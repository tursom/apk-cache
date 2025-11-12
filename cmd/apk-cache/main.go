package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

var (
	listenAddr         = flag.String("addr", ":8080", "监听地址")
	cachePath          = flag.String("cache", "./cache", "缓存目录")
	upstreamURL        = flag.String("upstream", "https://dl-cdn.alpinelinux.org", "上游服务器地址")
	socks5Proxy        = flag.String("proxy", "", "SOCKS5 代理地址 (例如: socks5://127.0.0.1:1080)")
	indexCacheDuration = flag.Duration("index-cache", 1*time.Hour, "APKINDEX.tar.gz 缓存时间")
	httpClient         *http.Client

	// 文件锁管理器
	lockManager = NewFileLockManager()
)

func main() {
	flag.Parse()

	// 创建缓存目录
	if err := os.MkdirAll(*cachePath, 0755); err != nil {
		log.Fatalf("创建缓存目录失败: %v", err)
	}

	// 配置 HTTP 客户端
	httpClient = createHTTPClient()

	http.HandleFunc("/", proxyHandler)

	log.Printf("APK 缓存服务器启动在 %s", *listenAddr)
	log.Printf("上游服务器: %s", *upstreamURL)
	log.Printf("缓存目录: %s", *cachePath)
	if *socks5Proxy != "" {
		log.Printf("SOCKS5 代理: %s", *socks5Proxy)
	}

	if err := http.ListenAndServe(*listenAddr, nil); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("请求: %s %s", r.Method, r.URL.Path)

	// 生成缓存文件路径
	cacheFile := getCacheFilePath(r.URL.Path)

	// 检查缓存是否存在
	if cacheValid(cacheFile) {
		log.Printf("缓存命中: %s", r.URL.Path)
		serveFromCache(w, cacheFile)
		return
	}

	// 缓存未命中,从上游获取
	log.Printf("缓存未命中,从上游获取: %s", r.URL.Path)
	upstreamResp, err := fetchFromUpstream(r.URL.Path)
	if err != nil {
		log.Printf("获取上游数据失败: %v", err)
		http.Error(w, "获取上游数据失败", http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	// 保存到缓存（带文件锁）
	if err := updateCacheFile(cacheFile, upstreamResp.Body, w, upstreamResp.StatusCode, upstreamResp.Header); err != nil {
		log.Printf("保存缓存失败: %v", err)
	} else {
		log.Printf("缓存已保存: %s", r.URL.Path)
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
	isIndex := strings.HasSuffix(path, "/APKINDEX.tar.gz")
	if isIndex && isCacheExpired(path, *indexCacheDuration) {
		log.Printf("索引缓存已过期: %s", path)
		return false
	}

	return true
}

func serveFromCache(w http.ResponseWriter, cacheFile string) {
	file, err := os.Open(cacheFile)
	if err != nil {
		http.Error(w, "读取缓存失败", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("X-Cache", "HIT")
	io.Copy(w, file)
}

func fetchFromUpstream(urlPath string) (*http.Response, error) {
	url := *upstreamURL + urlPath
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func updateCacheFile(cacheFile string, body io.Reader, w http.ResponseWriter, statusCode int, headers http.Header) error {
	// 获取该文件的锁
	unlock := lockManager.Acquire(cacheFile)
	defer unlock()

	// 再次检查文件是否已存在（可能在等待锁期间已被其他 goroutine 创建）
	if cacheValid(cacheFile) {
		log.Printf("文件已被其他请求缓存: %s", cacheFile)
		// 文件已存在，直接从缓存读取并发送给客户端
		serveFromCache(w, cacheFile)
		return nil
	}

	// 创建缓存文件的目录
	dir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建缓存目录失败: %w", err)
	}

	// 创建临时文件
	tmpFile, err := os.CreateTemp(dir, "tmp-")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
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

	for {
		// 读取数据
		n, readErr := body.Read(buf)
		if n > 0 {
			// 写入缓存文件（只要缓存没出错就继续写）
			if cacheErr == nil {
				if _, err := tmpFile.Write(buf[:n]); err != nil {
					cacheErr = err
					log.Printf("写入缓存失败: %v", err)
				}
			}

			// 写入客户端（只要客户端没出错就继续写）
			if clientErr == nil {
				if _, err := w.Write(buf[:n]); err != nil {
					clientErr = err
					log.Printf("写入客户端失败(可能客户端已断开): %v", err)
				}
			}
		}

		// 检查读取错误
		if readErr != nil {
			if readErr != io.EOF {
				return fmt.Errorf("读取上游响应失败: %w", readErr)
			}
			break // EOF，正常结束
		}
	}

	// 关闭临时文件
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}

	// 只有缓存写入成功才重命名
	if cacheErr != nil {
		return fmt.Errorf("缓存写入失败: %w", cacheErr)
	}

	if err := os.Rename(tmpFileName, cacheFile); err != nil {
		return fmt.Errorf("重命名缓存文件失败: %w", err)
	}

	return nil
}

func createHTTPClient() *http.Client {
	if *socks5Proxy == "" {
		return http.DefaultClient
	}

	proxyURL, err := url.Parse(*socks5Proxy)
	if err != nil {
		log.Fatalf("解析代理地址失败: %v", err)
	}

	// 创建 SOCKS5 dialer
	var auth *proxy.Auth
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		auth = &proxy.Auth{
			User:     proxyURL.User.Username(),
			Password: password,
		}
	}

	dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, auth, proxy.Direct)
	if err != nil {
		log.Fatalf("创建 SOCKS5 dialer 失败: %v", err)
	}

	// 创建带代理的 HTTP Transport
	transport := &http.Transport{
		Dial: dialer.Dial,
	}

	return &http.Client{
		Transport: transport,
	}
}

func isCacheExpired(path string, duration time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > duration
}
