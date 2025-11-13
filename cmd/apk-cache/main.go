package main

import (
	_ "embed"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/text/language"
)

//go:embed admin.html
var adminHTML string

//go:embed locales/en.toml
var enToml []byte

//go:embed locales/zh.toml
var zhToml []byte

var (
	// 创建自定义 Registry，不包含默认的 Go 运行时指标
	registry = prometheus.NewRegistry()

	cacheHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_hits_total",
		Help: "Total number of cache hits",
	})

	cacheMisses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_misses_total",
		Help: "Total number of cache misses",
	})

	downloadBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_download_bytes_total",
		Help: "Total bytes downloaded from upstream",
	})
)

var (
	listenAddr         = flag.String("addr", ":3142", "Listen address")
	cachePath          = flag.String("cache", "./cache", "Cache directory path")
	upstreamURL        = flag.String("upstream", "https://dl-cdn.alpinelinux.org", "Upstream server URL")
	socks5Proxy        = flag.String("proxy", "", "SOCKS5 proxy address (e.g. socks5://127.0.0.1:1080)")
	indexCacheDuration = flag.Duration("index-cache", 24*time.Hour, "APKINDEX.tar.gz cache duration")
	pkgCacheDuration   = flag.Duration("pkg-cache", 0, "Package cache duration (0 = never expire)")
	cleanupInterval    = flag.Duration("cleanup-interval", time.Hour, "Automatic cleanup interval (0 = disabled)")
	locale             = flag.String("locale", "", "Language (en/zh), auto-detect if empty")
	adminPassword      = flag.String("admin-password", "", "Admin dashboard password (empty = no auth)")
	configFile         = flag.String("config", "", "Config file path (optional)")
	httpClient         *http.Client

	// 进程启动时间
	processStartTime = time.Now()

	// 文件锁管理器
	lockManager = NewFileLockManager()
	// 访问时间跟踪器
	accessTimeTracker = NewAccessTimeTracker()
	localizer         *i18n.Localizer
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

	// 加载嵌入的翻译文件
	bundle.MustParseMessageFileBytes(enToml, "en.toml")
	bundle.MustParseMessageFileBytes(zhToml, "zh.toml")
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

	// 加载配置文件（如果指定）
	if *configFile != "" {
		config, err := LoadConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config file: %v\n", err)
		}
		if config != nil {
			if err := ApplyConfig(config); err != nil {
				log.Fatalf("Failed to apply config: %v\n", err)
			}
			log.Printf("Loaded config from: %s\n", *configFile)
		}
	}

	initI18n()

	// 注册 Prometheus 指标到自定义 Registry
	registry.MustRegister(cacheHits)
	registry.MustRegister(cacheMisses)
	registry.MustRegister(downloadBytes)

	// 创建缓存目录
	if err := os.MkdirAll(*cachePath, 0755); err != nil {
		log.Fatalln(t("CreateCacheDirFailed", map[string]any{"Error": err}))
	}

	// 配置 HTTP 客户端
	httpClient = createHTTPClient()

	// 启动自动清理
	if *cleanupInterval > 0 && *pkgCacheDuration != 0 {
		go startAutoCleanup()
		log.Printf("自动清理已启用，间隔：%v\n", *cleanupInterval)
	}

	// 使用自定义 Registry
	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	http.HandleFunc("/_admin/", authMiddleware(adminDashboardHandler))
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

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 如果没有设置密码，跳过认证
		if *adminPassword == "" {
			next(w, r)
			return
		}

		// Basic Auth
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != *adminPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
