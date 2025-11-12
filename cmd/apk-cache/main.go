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

	"golang.org/x/net/proxy"
)

var (
	listenAddr  = flag.String("addr", ":8080", "监听地址")
	cachePath   = flag.String("cache", "./cache", "缓存目录")
	upstreamURL = flag.String("upstream", "https://dl-cdn.alpinelinux.org", "上游服务器地址")
	socks5Proxy = flag.String("proxy", "", "SOCKS5 代理地址 (例如: socks5://127.0.0.1:1080)")
	httpClient  *http.Client
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
	if fileExists(cacheFile) {
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

	// 保存到缓存
	if err := saveToCacheFirst(cacheFile, upstreamResp.Body, w, upstreamResp.StatusCode, upstreamResp.Header); err != nil {
		log.Printf("保存缓存失败: %v", err)
	} else {
		log.Printf("缓存已保存: %s", r.URL.Path)
	}
}

func getCacheFilePath(urlPath string) string {
	safePath := filepath.Join(*cachePath, urlPath)
	return safePath
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func saveToCache(cacheFile string, body io.Reader, w http.ResponseWriter, statusCode int, headers http.Header) error {
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
	defer os.Remove(tmpFile.Name())

	// 复制响应头
	for key, values := range headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(statusCode)

	// 同时写入临时文件和响应
	writer := io.MultiWriter(tmpFile, w)
	if _, err := io.Copy(writer, body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("写入数据失败: %w", err)
	}

	tmpFile.Close()

	// 重命名临时文件为最终缓存文件
	if err := os.Rename(tmpFile.Name(), cacheFile); err != nil {
		return fmt.Errorf("重命名缓存文件失败: %w", err)
	}

	return nil
}

func saveToCacheFirst(cacheFile string, body io.Reader, w http.ResponseWriter, statusCode int, headers http.Header) error {
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

	// 先完整下载到临时文件
	if _, err := io.Copy(tmpFile, body); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	// 关闭并重命名为最终缓存文件
	tmpFile.Close()
	if err := os.Rename(tmpFileName, cacheFile); err != nil {
		return fmt.Errorf("重命名缓存文件失败: %w", err)
	}

	// 从缓存文件读取并发送给客户端
	cacheFileReader, err := os.Open(cacheFile)
	if err != nil {
		return fmt.Errorf("打开缓存文件失败: %w", err)
	}
	defer cacheFileReader.Close()

	// 复制响应头
	for key, values := range headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(statusCode)

	// 发送给客户端,忽略客户端断开连接的错误
	if _, err := io.Copy(w, cacheFileReader); err != nil {
		log.Printf("发送响应到客户端失败(可能客户端已断开): %v", err)
		// 不返回错误,因为缓存已经保存成功
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
