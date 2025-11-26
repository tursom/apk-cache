package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// handleAPTProxy 处理APT协议代理请求
func handleAPTProxy(w http.ResponseWriter, r *http.Request) {
	log.Println(t("APTProxyRequest", map[string]any{
		"Method": r.Method,
		"URL":    r.URL.String(),
		"Host":   r.Host,
	}))

	// 生成包含host的APT缓存文件路径
	cacheFile := getAPTCacheFilePath(r)

	// 检查客户端条件请求头
	if handleAPTClientConditionalRequest(w, r, cacheFile) {
		return
	}

	// 首先检查内存缓存
	if memoryCache != nil {
		// 在提供内存缓存之前检查客户端缓存头
		if handleMemoryCacheConditionalRequest(w, r, memoryCache, cacheFile) ||
			memoryCache.ServeFromMemory(w, cacheFile) {
			return
		}
	}

	// 检查文件缓存是否存在
	if cacheValid(cacheFile) {
		// 非 hash 请求，使用原有逻辑
		// 检查客户端缓存头，如果缓存未过期则返回304
		if handleFileCacheConditionalRequest(w, r, cacheFile) {
			return
		}

		if dataIntegrityManager == nil {
			// 数据完整性校验未启用，从缓存提供
			serveFromCache(w, r, cacheFile)
			return
		}
		// 如果数据完整性校验启用，验证文件完整性

		// 对于 hash 请求，必须使用 URL 中的哈希值进行完整性校验
		if isHashRequest(r.URL.Path) {
			// 使用统一的哈希请求验证函数
			valid, err := verifyHashRequest(cacheFile, r.URL.Path)
			if err != nil || !valid {
				// 校验出错，视为缓存未命中
				// 哈希不匹配，视为缓存未命中
			} else {
				// 哈希验证通过，从缓存提供
				serveFromCache(w, r, cacheFile)
				return
			}
		} else {
			valid, err := dataIntegrityManager.VerifyFileIntegrity(cacheFile)
			if err != nil {
				log.Println(t("FileIntegrityCheckError", map[string]any{
					"File":  cacheFile,
					"Error": err,
				}))
			} else if !valid {
				log.Println(t("CacheFileCorrupted", map[string]any{"Path": cacheFile}))
				// 文件损坏，视为缓存未命中
				// 继续从上游获取
			} else {
				// 文件完整，从缓存提供
				serveFromCache(w, r, cacheFile)
				return
			}
		}
	}

	cacheMisses.Add(1)

	// 缓存未命中,从上游获取
	log.Println(t("CacheMiss", map[string]any{"Path": cacheFile}))

	upstreamResp, err := fetchAPTFromUpstream(r)
	if err != nil {
		log.Println(t("FetchUpstreamFailed", map[string]any{"Error": err}))
		http.Error(w, t("FetchUpstreamFailed", map[string]any{"Error": err}), http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	// 使用统一的函数处理上游响应状态码
	if !handleUpstreamResponse(w, r, upstreamResp, cacheFile) {
		return
	}

	// 保存到缓存（带文件锁）
	if err := updateCacheFile(cacheFile, upstreamResp.Body, r, w, upstreamResp.StatusCode, upstreamResp.Header); err != nil {
		log.Println(t("SaveCacheFailed", map[string]any{"Error": err}))
		return
	}
	log.Println(t("CacheSaved", map[string]any{"Path": cacheFile}))

	// 对于 hash 请求，必须在 updateCacheFile 调用后校验 hash，不论校验器有没有开启
	if isHashRequest(r.URL.Path) {
		// 使用统一的哈希请求验证函数
		valid, err := verifyHashRequest(cacheFile, r.URL.Path)
		if err != nil {
			// 校验出错，记录错误但保留文件
			log.Println(t("HashVerificationFailed", map[string]any{
				"File":  cacheFile,
				"Error": err,
			}))
		} else if !valid {
			// 哈希不匹配，删除损坏的缓存文件
			if err := os.Remove(cacheFile); err != nil {
				log.Println(t("RemoveCorruptedCacheFailed", map[string]any{
					"File":  cacheFile,
					"Error": err,
				}))
			}
		}
		// 如果验证通过，verifyHashRequest 内部已经记录了成功日志
	}
}

// fetchAPTFromUpstream 从上游获取APT协议响应
func fetchAPTFromUpstream(r *http.Request) (*http.Response, error) {
	// 转发请求
	resp, err := proxyForwardRequest(r)
	if err != nil {
		return nil, errors.New(t("HTTPProxyForwardFailed", map[string]any{
			"Error": err,
		}))
	}

	return resp, nil
}

// getAPTCacheFilePath 为APT协议请求生成包含host的缓存文件路径
func getAPTCacheFilePath(r *http.Request) string {
	// 获取host信息，如果没有则使用默认值
	host := r.Host
	if host == "" {
		host = "default"
	}

	// 清理host中的特殊字符，使其适合作为目录名
	host = sanitizeHostForPath(host)

	// 处理URL路径，如果是代理请求且包含完整URL，提取路径部分
	urlPath := r.URL.Path
	if proxyIsProxyRequest(r) {
		// 检查是否是绝对URL路径（HTTP代理的常见特征）
		if strings.HasPrefix(urlPath, "http://") || strings.HasPrefix(urlPath, "https://") {
			// 解析URL以提取路径部分
			if parsedURL, err := url.Parse(urlPath); err == nil {
				urlPath = parsedURL.Path
			}
		}
	}

	// 构建包含host的缓存路径
	safePath := filepath.Join(*cachePath, "apt", host, urlPath)
	return safePath
}

// sanitizeHostForPath 清理host字符串，使其适合作为文件路径
func sanitizeHostForPath(host string) string {
	// 替换不安全的字符
	host = strings.ReplaceAll(host, ":", "_")
	host = strings.ReplaceAll(host, "/", "_")
	host = strings.ReplaceAll(host, "\\", "_")
	host = strings.ReplaceAll(host, "..", "_")
	host = strings.ReplaceAll(host, "*", "_")
	host = strings.ReplaceAll(host, "?", "_")
	host = strings.ReplaceAll(host, "\"", "_")
	host = strings.ReplaceAll(host, "<", "_")
	host = strings.ReplaceAll(host, ">", "_")
	host = strings.ReplaceAll(host, "|", "_")

	// 如果host为空，使用默认值
	if host == "" {
		host = "default"
	}

	return host
}

// handleAPTClientConditionalRequest 处理APT协议客户端的条件请求
// 比较客户端提供的If-Modified-Since与之前保存的值，如果缓存未过期则返回304
func handleAPTClientConditionalRequest(w http.ResponseWriter, r *http.Request, cacheFile string) bool {
	// 检查当前请求的 If-Modified-Since 头
	currentIfModifiedSince := r.Header.Get("If-Modified-Since")
	if currentIfModifiedSince == "" {
		return false
	}

	// 获取之前保存的 If-Modified-Since 头（当上游返回304时保存的）
	savedIfModifiedSince, savedExists := clientCacheHeaders.Get(cacheFile)
	if !savedExists {
		return false
	}

	// 如果之前保存过值，比较当前值和保存值
	currentTime, err1 := time.Parse(http.TimeFormat, currentIfModifiedSince)
	savedTime, err2 := time.Parse(http.TimeFormat, savedIfModifiedSince)

	// 如果当前值比保存值更早，说明客户端缓存可能已过期，需要重新获取
	if err1 != nil || err2 != nil || currentTime.Before(savedTime) {
		return false
	}

	// 客户端缓存未过期，返回304 Not Modified
	w.WriteHeader(http.StatusNotModified)
	cacheHits.Add(1)
	log.Println(t("ClientCacheValid", map[string]any{"Path": cacheFile}))
	return true
}

// handleMemoryCacheConditionalRequest 处理内存缓存的条件请求
// 基于内存缓存项的修改时间检查If-Modified-Since头，如果缓存未过期则返回304
func handleMemoryCacheConditionalRequest(w http.ResponseWriter, r *http.Request, memoryCache *MemoryCache, cacheFile string) bool {
	// 检查 If-Modified-Since 头
	ifModifiedSince := r.Header.Get("If-Modified-Since")
	if ifModifiedSince == "" {
		return false
	}

	// 解析客户端提供的修改时间
	clientTime, err := time.Parse(http.TimeFormat, ifModifiedSince)
	if err != nil {
		log.Println(t("ParseIfModifiedSinceFailed", map[string]any{"Error": err}))
		return false
	}

	if modTime, found := memoryCache.GetModTime(cacheFile); found {
		// 如果内存缓存项的修改时间晚于客户端提供的修改时间，说明内容已更新
		// 需要返回完整内容
		if modTime.After(clientTime) {
			return false
		}

		// 内存缓存项未修改，返回304 Not Modified
		w.WriteHeader(http.StatusNotModified)
		cacheHits.Add(1)
		log.Println(t("ClientCacheValid", map[string]any{"Path": cacheFile}))
		return true
	}

	return false
}

// handleFileCacheConditionalRequest 处理文件缓存的条件请求
// 基于文件系统修改时间检查If-Modified-Since头，如果缓存未过期则返回304
func handleFileCacheConditionalRequest(w http.ResponseWriter, r *http.Request, cacheFile string) bool {
	// 检查 If-Modified-Since 头
	ifModifiedSince := r.Header.Get("If-Modified-Since")
	if ifModifiedSince == "" {
		return false
	}

	// 解析客户端提供的修改时间
	clientTime, err := time.Parse(http.TimeFormat, ifModifiedSince)
	if err != nil {
		log.Println(t("ParseIfModifiedSinceFailed", map[string]any{"Error": err}))
		return false
	}

	// 内存缓存未命中，检查文件缓存的修改时间
	fileInfo, err := os.Stat(cacheFile)
	if err != nil {
		log.Println(t("GetFileInfoFailed", map[string]any{"File": cacheFile, "Error": err}))
		return false
	}

	// 如果缓存文件的修改时间晚于客户端提供的修改时间，说明内容已更新
	// 需要返回完整内容
	if fileInfo.ModTime().After(clientTime) {
		return false
	}

	// 缓存文件未修改，返回304 Not Modified
	w.WriteHeader(http.StatusNotModified)
	cacheHits.Add(1)
	log.Println(t("ClientCacheValid", map[string]any{"Path": cacheFile}))
	return true
}

// isHashRequest 检查是否是 hash 请求
func isHashRequest(path string) bool {
	// 检查路径是否包含常见的 hash 请求模式
	return strings.Contains(path, "/by-hash/SHA256/") ||
		strings.Contains(path, "/by-hash/SHA1/") ||
		strings.Contains(path, "/by-hash/MD5Sum/") ||
		strings.Contains(path, "/by-hash/MD5/")
}

// parseHashFromURL 从 URL 中解析哈希算法和哈希值
func parseHashFromURL(path string) (string, string, error) {
	// 解析 URL 路径，提取哈希算法和哈希值
	// 格式: /by-hash/ALGORITHM/HASH_VALUE
	parts := strings.Split(path, "/")

	// 查找 "by-hash" 的位置
	byHashIndex := -1
	for i, part := range parts {
		if part == "by-hash" {
			byHashIndex = i
			break
		}
	}

	if byHashIndex == -1 || byHashIndex+2 >= len(parts) {
		return "", "", errors.New(t("InvalidHashURLFormat", nil))
	}

	algorithm := parts[byHashIndex+1]
	hashValue := parts[byHashIndex+2]

	// 验证算法是否支持
	supportedAlgorithms := map[string]bool{
		"SHA256": true,
		"SHA1":   true,
		"MD5Sum": true,
		"MD5":    true,
	}

	if !supportedAlgorithms[algorithm] {
		return "", "", errors.New(t("UnsupportedHashAlgorithm", map[string]any{"Algorithm": algorithm}))
	}

	// 验证哈希值格式（基本格式检查）
	if len(hashValue) == 0 {
		return "", "", errors.New(t("EmptyHashValue", nil))
	}

	return algorithm, hashValue, nil
}

// verifyFileWithHash 使用指定的哈希算法验证文件完整性
func verifyFileWithHash(filePath, algorithm, expectedHash string) (bool, error) {
	// 读取文件内容
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, errors.New(t("ReadFileFailed", map[string]any{"Error": err}))
	}

	// 根据算法计算哈希值
	var actualHash string
	switch algorithm {
	case "SHA256":
		hash := sha256.Sum256(data)
		actualHash = hex.EncodeToString(hash[:])
	case "SHA1":
		hash := sha1.Sum(data)
		actualHash = hex.EncodeToString(hash[:])
	case "MD5Sum", "MD5":
		hash := md5.Sum(data)
		actualHash = hex.EncodeToString(hash[:])
	default:
		return false, errors.New(t("UnsupportedHashAlgorithm", map[string]any{"Algorithm": algorithm}))
	}

	// 比较哈希值（不区分大小写）
	return strings.EqualFold(actualHash, expectedHash), nil
}

// verifyHashRequest 验证哈希请求的文件完整性
func verifyHashRequest(cacheFile, path string) (bool, error) {
	// 解析 URL 中的哈希算法和哈希值
	algorithm, expectedHash, err := parseHashFromURL(path)
	if err != nil {
		log.Println(t("ParseHashFromURLFailed", map[string]any{
			"Path":  path,
			"Error": err,
		}))
		return false, err
	}

	// 使用 URL 中的哈希值验证文件完整性
	valid, err := verifyFileWithHash(cacheFile, algorithm, expectedHash)
	if err != nil {
		log.Println(t("HashVerificationFailed", map[string]any{
			"File":  cacheFile,
			"Error": err,
		}))
		return false, err
	}

	if !valid {
		log.Println(t("HashVerificationMismatch", map[string]any{
			"Path":      path,
			"File":      cacheFile,
			"Algorithm": algorithm,
			"Expected":  expectedHash,
		}))
		return false, nil
	}

	log.Println(t("HashVerificationPassed", map[string]any{
		"Path":      path,
		"Algorithm": algorithm,
	}))
	return true, nil
}
