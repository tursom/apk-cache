package main

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
)

// ClientCacheHeaders 存储客户端的缓存头信息
type ClientCacheHeaders struct {
	mu      sync.RWMutex
	headers map[string]*cacheHeaderEntry // key: cacheFile, value: cache header entry
}

// cacheHeaderEntry 存储缓存头信息和创建时间
type cacheHeaderEntry struct {
	ifModifiedSince string
	createdAt       time.Time
}

// 全局客户端缓存头管理器
var clientCacheHeaders = NewClientCacheHeaders()

// NewClientCacheHeaders 创建新的客户端缓存头管理器
func NewClientCacheHeaders() *ClientCacheHeaders {
	return &ClientCacheHeaders{
		headers: make(map[string]*cacheHeaderEntry),
	}
}

// Save 设置缓存文件的 If-Modified-Since 头，只保存更早的时间
func (c *ClientCacheHeaders) Save(cacheFile, ifModifiedSince string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果新的 If-Modified-Since 为空，不保存
	if ifModifiedSince == "" {
		return
	}

	// 解析新的时间
	newTime, err := time.Parse(http.TimeFormat, ifModifiedSince)
	if err != nil {
		// 如果解析失败，不保存
		return
	}

	// 检查是否已存在该缓存文件的头信息
	if existingEntry, exists := c.headers[cacheFile]; exists {
		// 解析已存在的时间
		existingTime, err := time.Parse(http.TimeFormat, existingEntry.ifModifiedSince)
		if err != nil {
			// 如果已存在的时间解析失败，用新的时间替换
			c.headers[cacheFile] = &cacheHeaderEntry{
				ifModifiedSince: ifModifiedSince,
				createdAt:       time.Now(),
			}
			return
		}

		// 只保存更早的时间
		if newTime.Before(existingTime) {
			c.headers[cacheFile] = &cacheHeaderEntry{
				ifModifiedSince: ifModifiedSince,
				createdAt:       time.Now(),
			}
		}
	} else {
		// 如果不存在，直接保存
		c.headers[cacheFile] = &cacheHeaderEntry{
			ifModifiedSince: ifModifiedSince,
			createdAt:       time.Now(),
		}
	}
}

// Get 获取缓存文件的 If-Modified-Since 头，同时检查是否过期
func (c *ClientCacheHeaders) Get(cacheFile string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.headers[cacheFile]
	if !exists {
		return "", false
	}

	// 检查是否过期（使用索引文件过期时间）
	if time.Since(entry.createdAt) > *indexCacheDuration {
		return "", false
	}

	return entry.ifModifiedSince, true
}

// CleanupExpired 清理过期的缓存头
func (c *ClientCacheHeaders) CleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for cacheFile, entry := range c.headers {
		if time.Since(entry.createdAt) > *indexCacheDuration {
			delete(c.headers, cacheFile)
		}
	}
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	log.Println(t("RequestReceived", map[string]any{
		"Method": r.Method,
		"Path":   r.URL.Path,
	}))

	// 生成缓存文件路径
	cacheFile := getCacheFilePath(r.URL.Path)

	// 首先检查内存缓存
	if memoryCache != nil {
		if memoryCache.ServeFromMemory(w, cacheFile) {
			return
		}
	}

	// 检查文件缓存是否存在
	if cacheValid(cacheFile) {
		// 如果数据完整性校验启用，验证文件完整性
		if dataIntegrityManager != nil {
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
		} else {
			// 数据完整性校验未启用，直接从缓存提供
			serveFromCache(w, r, cacheFile)
			return
		}
	}

	cacheMisses.Add(1)

	// 缓存未命中,从上游获取
	log.Println(t("CacheMiss", map[string]any{"Path": cacheFile}))
	upstreamResp, err := fetchFromUpstream(r.URL.Path)
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
	} else {
		log.Println(t("CacheSaved", map[string]any{"Path": cacheFile}))
	}
}

func getCacheFilePath(urlPath string) string {
	safePath := filepath.Join(*cachePath, urlPath)
	return safePath
}

func cacheValid(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		return false
	}

	// 检查是否是索引文件
	isIndex := isIndexFile(path)
	if isIndex {
		// index 文件按修改时间过期
		if isCacheExpiredByModTime(path, *indexCacheDuration) {
			log.Println(t("IndexExpired", map[string]any{"Path": path}))
			return false
		}
	} else {
		// 普通包文件按访问时间过期（pkgCacheDuration > 0 时才检查）
		if *pkgCacheDuration > 0 && isCacheExpiredByAccessTime(path, *pkgCacheDuration) {
			log.Println(t("CacheExpired", map[string]any{"Path": path}))
			return false
		}
	}

	return true
}

func serveFromCache(w http.ResponseWriter, r *http.Request, cacheFile string) {
	log.Println(t("CacheHit", map[string]any{"Path": cacheFile}))

	// 首先尝试从内存缓存提供
	if memoryCache != nil {
		if memoryCache.ServeFromMemory(w, cacheFile) {
			return
		}
	}

	file, err := os.Open(cacheFile)
	if err != nil {
		http.Error(w, t("ReadCacheFailed", nil), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// 记录访问时间（只记录非索引文件）
	if !isIndexFile(cacheFile) {
		accessTimeTracker.RecordAccess(cacheFile)
	}

	stat, _ := file.Stat()
	w.Header().Set("X-Cache", "HIT")

	// 如果内存缓存启用，将文件内容加载到内存缓存
	if memoryCache != nil && !isIndexFile(cacheFile) {
		// 检查文件大小是否超过内存缓存限制
		if memoryCacheMaxFileSizeBytes > 0 && stat.Size() > memoryCacheMaxFileSizeBytes {
			log.Println(t("MemoryCacheFileTooLarge", map[string]any{
				"Path": cacheFile,
				"Size": stat.Size(),
				"Max":  memoryCacheMaxFileSizeBytes,
			}))
		} else {
			// 读取文件内容
			data, err := os.ReadFile(cacheFile)
			if err == nil {
				// 获取文件信息以获取响应头
				headers := make(map[string][]string)
				headers["Content-Type"] = []string{"application/octet-stream"}
				headers["Last-Modified"] = []string{stat.ModTime().Format(http.TimeFormat)}
				headers["Content-Length"] = []string{strconv.FormatInt(stat.Size(), 10)}

				// 将文件内容缓存到内存，包括修改时间
				memoryCache.CacheToMemory(cacheFile, data, headers, http.StatusOK, stat.ModTime())
			}
		}
	}

	http.ServeContent(w, r, filepath.Base(cacheFile), stat.ModTime(), file)
	cacheHits.Add(1)
	cacheHitBytes.Add(float64(stat.Size()))
}

func fetchFromUpstream(urlPath string) (*http.Response, error) {
	// 使用上游管理器获取响应
	resp, err := upstreamManager.FetchFromUpstream(urlPath)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// createHTTPClientForUpstream 为指定的代理创建 HTTP 客户端
func createHTTPClientForUpstream(proxyAddr string) *http.Client {
	if proxyAddr == "" {
		return &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		log.Println(t("InvalidProxy", map[string]any{
			"Proxy": proxyAddr,
			"Error": err,
		}))
		return &http.Client{Timeout: 30 * time.Second}
	}

	var transport *http.Transport

	if proxyURL.Scheme == "socks5" {
		// SOCKS5 代理
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		if err != nil {
			log.Println(t("CreateProxyFailed", map[string]any{
				"Error": err,
			}))
			return &http.Client{Timeout: 30 * time.Second}
		}
		transport = &http.Transport{
			Dial: dialer.Dial,
		}
	} else {
		// HTTP/HTTPS 代理
		transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

func updateCacheFile(cacheFile string, body io.Reader, r *http.Request, w http.ResponseWriter, statusCode int, headers http.Header) error {
	// 获取该文件的锁
	unlock := lockManager.Acquire(cacheFile)
	defer unlock()

	// 再次检查文件是否已存在（可能在等待锁期间已被其他 goroutine 创建）
	if cacheValid(cacheFile) {
		// 文件已存在，直接从缓存读取并发送给客户端
		serveFromCache(w, r, cacheFile)
		return nil
	}

	// 创建缓存文件的目录
	dir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.New(t("CreateCacheDirFailed", map[string]any{"Error": err}))
	}

	// 创建临时文件
	tmpFile, err := os.CreateTemp(dir, "tmp-")
	if err != nil {
		return errors.New(t("CreateTempFileFailed", map[string]any{"Error": err}))
	}
	tmpFileName := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFileName)
	}()

	// 复制响应头
	for key, values := range headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(statusCode)

	buf := make([]byte, 32*1024)
	var cacheErr, clientErr error
	var totalBytes int64 = 0
	var responseData []byte

	for {
		// 读取数据
		n, readErr := body.Read(buf)
		if n > 0 {
			downloadBytes.Add(float64(n))
			totalBytes += int64(n)

			// 写入缓存文件（只要缓存没出错就继续写）
			if cacheErr == nil {
				if _, err := tmpFile.Write(buf[:n]); err != nil {
					cacheErr = err
					log.Println(t("WriteCacheFailed", map[string]any{"Error": err}))
				}
			}

			// 写入客户端（只要客户端没出错就继续写）
			if clientErr == nil {
				if _, err := w.Write(buf[:n]); err != nil {
					clientErr = err
					log.Println(t("WriteClientFailed", map[string]any{"Error": err}))
				}
			}

			// 收集数据用于内存缓存（仅在内存缓存启用且没有错误时）
			if memoryCache != nil && cacheErr == nil && clientErr == nil && len(responseData) < int(memoryCacheMaxFileSizeBytes) {
				responseData = append(responseData, buf[:n]...)
			}
		}

		// 检查读取错误
		if readErr != nil {
			if readErr != io.EOF {
				return errors.New(t("ReadUpstreamFailed", map[string]any{"Error": readErr}))
			}
			break // EOF，正常结束
		}
	}

	// 关闭临时文件
	if err := tmpFile.Close(); err != nil {
		return errors.New(t("CloseTempFileFailed", map[string]any{"Error": err}))
	}

	// 检查缓存配额（仅在缓存写入成功时）
	if cacheErr == nil && cacheQuota != nil {
		// 获取临时文件大小
		fileInfo, err := os.Stat(tmpFileName)
		if err != nil {
			return errors.New(t("GetFileSizeFailed", map[string]any{"Error": err}))
		}

		// 检查配额
		allowed, err := cacheQuota.CheckAndUpdateQuota(fileInfo.Size())
		if !allowed {
			log.Println(t("CacheQuotaRejected", map[string]any{
				"File": cacheFile,
				"Size": fileInfo.Size(),
			}))
			// 配额不足，不保存缓存文件，但已成功服务客户端
			return nil
		}
	}

	// 只有缓存写入成功才重命名
	if cacheErr != nil {
		return errors.New(t("CacheWriteFailed", map[string]any{"Error": cacheErr}))
	}

	// 检查临时文件大小
	ostInfo, err := os.Stat(tmpFileName)
	if err != nil {
		return errors.New(t("GetTempFileSizeFailed", map[string]any{"Error": err}))
	}
	if ostInfo.Size() == 0 {
		return errors.New(t("TempFileZeroSize", map[string]any{"File": tmpFileName}))
	}

	if err := os.Rename(tmpFileName, cacheFile); err != nil {
		return errors.New(t("RenameCacheFileFailed", map[string]any{"Error": err}))
	}

	// 将数据存入内存缓存（仅在成功写入文件缓存且内存缓存启用时）
	if memoryCache != nil && len(responseData) > 0 {
		// 检查文件大小是否超过内存缓存限制
		if memoryCacheMaxFileSizeBytes > 0 && int64(len(responseData)) > memoryCacheMaxFileSizeBytes {
			log.Println(t("MemoryCacheFileTooLarge", map[string]any{
				"Path": cacheFile,
				"Size": len(responseData),
				"Max":  memoryCacheMaxFileSizeBytes,
			}))
		} else {
			memoryCache.CacheToMemory(cacheFile, responseData, headers, statusCode, time.Now())
		}
	}

	// 记录文件哈希（如果数据完整性校验启用）
	if dataIntegrityManager != nil && len(responseData) > 0 {
		if err := dataIntegrityManager.RecordFileHash(cacheFile, responseData); err != nil {
			log.Println(t("RecordFileHashFailed", map[string]any{
				"File":  cacheFile,
				"Error": err,
			}))
		}
	}

	// 记录缓存未命中时的大小
	cacheMissBytes.Add(float64(totalBytes))
	return nil
}

// isCacheExpiredByModTime 检查文件是否按修改时间过期（用于 index 文件）
func isCacheExpiredByModTime(path string, duration time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > duration
}

// isCacheExpiredByAccessTime 检查文件是否按访问时间过期（用于普通包文件）
func isCacheExpiredByAccessTime(path string, duration time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}

	// 优先使用内存中记录的访问时间
	if memAccessTime, ok := accessTimeTracker.GetAccessTime(path); ok {
		return time.Since(memAccessTime) > duration
	}

	// 如果内存中没有记录，从文件系统获取访问时间
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// 如果无法获取访问时间，回退到进程启动时间
		// 这样可以避免程序启动后立即清理旧缓存
		return time.Since(processStartTime) > duration
	}

	// 访问时间
	atime := time.Unix(stat.Atim.Sec, stat.Atim.Nsec)

	// 如果文件的访问时间早于进程启动时间，使用进程启动时间
	// 这表示文件在程序启动前就存在，我们假设它在启动时被"访问"
	if atime.Before(processStartTime) {
		return time.Since(processStartTime) > duration
	}

	return time.Since(atime) > duration
}

func isIndexFile(path string) bool {
	return strings.HasSuffix(path, "/APKINDEX.tar.gz") ||
		strings.HasSuffix(path, "/InRelease") ||
		strings.HasSuffix(path, "/Release") ||
		strings.HasSuffix(path, "/Packages") ||
		strings.HasSuffix(path, "/Packages.gz") ||
		strings.HasSuffix(path, "/Sources") ||
		strings.HasSuffix(path, "/Sources.gz")
}

// handleUpstreamResponse 统一处理上游响应状态码
// 返回 true 表示继续处理，false 表示已处理完成
func handleUpstreamResponse(w http.ResponseWriter, r *http.Request, upstreamResp *http.Response, cacheFile string) bool {
	// 细化处理上游响应状态码
	switch upstreamResp.StatusCode {
	case http.StatusOK:
		// 正常响应，继续处理
		return true
	case http.StatusNotModified:
		// 304 Not Modified - 内容未修改，可以继续使用缓存
		log.Println(t("UpstreamNotModified", map[string]any{"Path": cacheFile}))

		// 保存客户端的 If-Modified-Since 头信息
		if ifModifiedSince := r.Header.Get("If-Modified-Since"); ifModifiedSince != "" {
			clientCacheHeaders.Save(cacheFile, ifModifiedSince)
			log.Println(t("ClientCacheHeaderSaved", map[string]any{
				"Path":   cacheFile,
				"Header": ifModifiedSince,
			}))
		}

		// 如果缓存存在，直接使用缓存
		if cacheValid(cacheFile) {
			serveFromCache(w, r, cacheFile)
			return false
		}
		// 如果缓存不存在，返回304状态
		w.WriteHeader(http.StatusNotModified)
		return false
	default:
		// 其他错误状态码
		log.Println(t("UpstreamReturnedError", map[string]any{
			"Status":     upstreamResp.Status,
			"StatusCode": upstreamResp.StatusCode,
		}))
		http.Error(w, t("UpstreamReturnedError", map[string]any{
			"Status":     upstreamResp.Status,
			"StatusCode": upstreamResp.StatusCode,
		}), upstreamResp.StatusCode)
		return false
	}
}
