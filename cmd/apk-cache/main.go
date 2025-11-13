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

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/net/proxy"
	"golang.org/x/text/language"
)

var (
	listenAddr         = flag.String("addr", ":8080", "Listen address")
	cachePath          = flag.String("cache", "./cache", "Cache directory path")
	upstreamURL        = flag.String("upstream", "https://dl-cdn.alpinelinux.org", "Upstream server URL")
	socks5Proxy        = flag.String("proxy", "", "SOCKS5 proxy address (e.g. socks5://127.0.0.1:1080)")
	indexCacheDuration = flag.Duration("index-cache", 24*time.Hour, "APKINDEX.tar.gz cache duration")
	locale             = flag.String("locale", "", "Language (en/zh), auto-detect if empty")
	httpClient         *http.Client

	// 文件锁管理器
	lockManager = NewFileLockManager()
	localizer   *i18n.Localizer
)

// detectLocale 自动检测系统语言
func detectLocale() string {
	// 如果命令行参数已指定，直接使用
	if *locale != "" {
		return *locale
	}

	// 按优先级检查环境变量
	envVars := []string{"LC_ALL", "LC_MESSAGES", "LANG"}
	for _, env := range envVars {
		if val := os.Getenv(env); val != "" {
			// 解析语言代码，如 "zh_CN.UTF-8" -> "zh"
			lang := strings.Split(val, ".")[0] // 去除编码部分
			lang = strings.Split(lang, "_")[0] // 去除地区部分
			lang = strings.ToLower(lang)

			// 支持的语言列表
			supported := map[string]bool{
				"zh": true,
				"en": true,
			}

			if supported[lang] {
				return lang
			}
		}
	}

	// 默认使用英语
	return "en"
}

func initI18n() {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// 加载翻译文件
	bundle.MustLoadMessageFile("locales/en.toml")
	bundle.MustLoadMessageFile("locales/zh.toml")
	// 自动检测语言
	detectedLocale := detectLocale()

	localizer = i18n.NewLocalizer(bundle, detectedLocale)

	log.Println(t("UsingLanguage", map[string]any{"Lang": detectedLocale}))
}

func t(messageID string, templateData map[string]any) string {
	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: templateData,
	})
	if err != nil {
		return messageID // 回退到 ID
	}
	return msg
}

func main() {
	flag.Parse()

	initI18n()

	// 创建缓存目录
	if err := os.MkdirAll(*cachePath, 0755); err != nil {
		log.Fatalln(t("CreateCacheDirFailed", map[string]any{"Error": err}))
	}

	// 配置 HTTP 客户端
	httpClient = createHTTPClient()

	http.HandleFunc("/", proxyHandler)

	log.Println(t("ServerStarted", map[string]any{"Addr": *listenAddr}))
	log.Println(t("UpstreamServer", map[string]any{"URL": *upstreamURL}))
	log.Println(t("CacheDirectory", map[string]any{"Path": *cachePath}))
	if *socks5Proxy != "" {
		log.Println(t("SOCKS5Proxy", map[string]any{"Proxy": *socks5Proxy}))
	}

	if err := http.ListenAndServe(*listenAddr, nil); err != nil {
		log.Fatalln(t("ServerStartFailed", map[string]any{"Error": err}))
	}
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	log.Println(t("RequestReceived", map[string]any{
		"Method": r.Method,
		"Path":   r.URL.Path,
	}))

	// 生成缓存文件路径
	cacheFile := getCacheFilePath(r.URL.Path)

	// 检查缓存是否存在
	if cacheValid(cacheFile) {
		log.Println(t("CacheHit", map[string]any{"Path": r.URL.Path}))
		serveFromCache(w, cacheFile)
		return
	}

	// 缓存未命中,从上游获取
	log.Println(t("CacheMiss", map[string]any{"Path": r.URL.Path}))
	upstreamResp, err := fetchFromUpstream(r.URL.Path)
	if err != nil {
		log.Println(t("FetchUpstreamFailed", map[string]any{"Error": err}))
		http.Error(w, t("FetchUpstreamFailed", map[string]any{"Error": err}), http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	// 保存到缓存（带文件锁）
	if err := updateCacheFile(cacheFile, upstreamResp.Body, w, upstreamResp.StatusCode, upstreamResp.Header); err != nil {
		log.Println(t("SaveCacheFailed", map[string]any{"Error": err}))
	} else {
		log.Println(t("CacheSaved", map[string]any{"Path": r.URL.Path}))
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
		log.Println(t("IndexExpired", map[string]any{"Path": path}))
		return false
	}

	return true
}

func serveFromCache(w http.ResponseWriter, cacheFile string) {
	file, err := os.Open(cacheFile)
	if err != nil {
		http.Error(w, t("ReadCacheFailed", nil), http.StatusInternalServerError)
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
		log.Println(t("CachedByOther", map[string]any{"Path": cacheFile}))
		// 文件已存在，直接从缓存读取并发送给客户端
		serveFromCache(w, cacheFile)
		return nil
	}

	// 创建缓存文件的目录
	dir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("%s", t("CreateCacheDirFailed", map[string]any{"Error": err}))
	}

	// 创建临时文件
	tmpFile, err := os.CreateTemp(dir, "tmp-")
	if err != nil {
		return fmt.Errorf("%s", t("CreateTempFileFailed", map[string]any{"Error": err}))
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
		}

		// 检查读取错误
		if readErr != nil {
			if readErr != io.EOF {
				return fmt.Errorf("%s", t("ReadUpstreamFailed", map[string]any{"Error": readErr}))
			}
			break // EOF，正常结束
		}
	}

	// 关闭临时文件
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("%s", t("CloseTempFileFailed", map[string]any{"Error": err}))
	}

	// 只有缓存写入成功才重命名
	if cacheErr != nil {
		return fmt.Errorf("%s", t("CacheWriteFailed", map[string]any{"Error": cacheErr}))
	}

	if err := os.Rename(tmpFileName, cacheFile); err != nil {
		return fmt.Errorf("%s", t("RenameCacheFileFailed", map[string]any{"Error": err}))
	}

	return nil
}

func createHTTPClient() *http.Client {
	if *socks5Proxy == "" {
		return http.DefaultClient
	}

	proxyURL, err := url.Parse(*socks5Proxy)
	if err != nil {
		log.Fatalln(t("ParseProxyFailed", map[string]any{"Error": err}))
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
		log.Fatalln(t("CreateDialerFailed", map[string]any{"Error": err}))
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
