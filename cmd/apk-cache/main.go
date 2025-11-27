package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/text/language"
)

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

	// 缓存命中大小统计
	cacheHitBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_hit_bytes_total",
		Help: "Total bytes served from cache hits",
	})

	// 缓存未命中大小统计
	cacheMissBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_miss_bytes_total",
		Help: "Total bytes served from cache misses",
	})

	// 限流相关指标
	rateLimitAllowed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_rate_limit_allowed_total",
		Help: "Total number of requests allowed by rate limiter",
	})

	rateLimitRejected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_rate_limit_rejected_total",
		Help: "Total number of requests rejected by rate limiter",
	})

	rateLimitCurrentTokens = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "apk_cache_rate_limit_current_tokens",
		Help: "Current number of tokens in the rate limiter bucket",
	})
)

var (
	listenAddr         = flag.String("addr", ":3142", "Listen address")
	cachePath          = flag.String("cache", "./cache", "Cache directory path")
	upstreamURL        = flag.String("upstream", "https://dl-cdn.alpinelinux.org", "Upstream server URL")
	proxyURL           = flag.String("proxy", "", "Proxy address (e.g. socks5://127.0.0.1:1080 or http://127.0.0.1:8080)")
	indexCacheDuration = flag.Duration("index-cache", 24*time.Hour, "APKINDEX.tar.gz cache duration")
	pkgCacheDuration   = flag.Duration("pkg-cache", 0, "Package cache duration (0 = never expire)")
	cleanupInterval    = flag.Duration("cleanup-interval", time.Hour, "Automatic cleanup interval (0 = disabled)")
	locale             = flag.String("locale", "", "Language (en/zh), auto-detect if empty")
	adminUser          = flag.String("admin-user", "admin", "Admin dashboard username")
	adminPassword      = flag.String("admin-password", "", "Admin dashboard password (empty = no auth)")
	configFile         = flag.String("config", "", "Config file path (optional)")
	// 缓存配额相关参数
	cacheMaxSize       = flag.String("cache-max-size", "", "Maximum cache size (e.g. 10GB, 1TB, 0 = unlimited)")
	cacheCleanStrategy = flag.String("cache-clean-strategy", "LRU", "Cache cleanup strategy (LRU, LFU, FIFO)")

	// 内存缓存相关参数
	memoryCacheEnabled     = flag.Bool("memory-cache", false, "Enable memory cache")
	memoryCacheSize        = flag.String("memory-cache-size", "100MB", "Memory cache size (e.g. 100MB, 1GB)")
	memoryCacheMaxItems    = flag.Int("memory-cache-max-items", 1000, "Maximum number of items in memory cache")
	memoryCacheTTL         = flag.Duration("memory-cache-ttl", 30*time.Minute, "Memory cache TTL duration")
	memoryCacheMaxFileSize = flag.String("memory-cache-max-file-size", "10MB", "Maximum file size to cache in memory (e.g. 1MB, 10MB)")

	// 请求限流相关参数
	rateLimitEnabled     = flag.Bool("rate-limit", false, "Enable request rate limiting")
	rateLimitRate        = flag.Float64("rate-limit-rate", 100, "Rate limit requests per second")
	rateLimitBurst       = flag.Float64("rate-limit-burst", 200, "Rate limit burst capacity")
	rateLimitExemptPaths = flag.String("rate-limit-exempt-paths", "/_health", "Comma-separated list of paths exempt from rate limiting")

	// 健康检查相关变量
	healthCheckInterval = flag.Duration("health-check-interval", 30*time.Second, "Health check interval")
	healthCheckTimeout  = flag.Duration("health-check-timeout", 10*time.Second, "Health check timeout")
	enableSelfHealing   = flag.Bool("enable-self-healing", true, "Enable self-healing mechanisms")

	// 数据完整性校验相关参数
	dataIntegrityCheckInterval           = flag.Duration("data-integrity-check-interval", time.Hour, "Data integrity check interval (0 = disabled)")
	dataIntegrityAutoRepair              = flag.Bool("data-integrity-auto-repair", true, "Enable automatic repair of corrupted files")
	dataIntegrityPeriodicCheck           = flag.Bool("data-integrity-periodic-check", true, "Enable periodic data integrity checks")
	dataIntegrityInitializeExistingFiles = flag.Bool("data-integrity-initialize-existing-files", false, "Initialize existing files hash records on startup")

	// 进程启动时间
	processStartTime = time.Now()

	// 上游服务器管理器（支持故障转移和健康检查）
	upstreamManager *UpstreamManager

	// 文件锁管理器
	lockManager = NewFileLockManager()
	// 访问时间跟踪器
	accessTimeTracker = NewAccessTimeTracker()
	// 缓存配额管理器
	cacheQuota *CacheQuota
	// 内存缓存管理器
	memoryCache *MemoryCache
	// 内存缓存最大文件大小
	memoryCacheMaxFileSizeBytes int64
	localizer                   *i18n.Localizer

	// 健康检查管理器
	healthCheckManager = NewHealthCheckManager()

	// 请求限流器
	rateLimiter *RateLimiter

	// 数据完整性管理器
	dataIntegrityManager *DataIntegrityManager
)

type webHandler struct {
	metricsHandler http.HandlerFunc
	adminHandler   http.HandlerFunc
	healthHandler  http.HandlerFunc
	rootHandler    http.HandlerFunc
}

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
		// 在未命中时将 templateData 以 JSON 格式添加到返回值中以便调试
		if len(templateData) > 0 {
			jsonData, err := json.Marshal(templateData)
			if err == nil {
				return messageID + " " + string(jsonData)
			}
		}
		return messageID // 回退到 ID
	}
	return msg
}

// parseSizeString 解析大小字符串（如 "10GB", "1TB"）
func parseSizeString(sizeStr string) (int64, error) {
	if sizeStr == "" || sizeStr == "0" {
		return 0, nil // 0 表示无限制
	}

	sizeStr = strings.ToUpper(sizeStr)
	multiplier := int64(1)

	// 检查单位
	if strings.HasSuffix(sizeStr, "TB") {
		multiplier = 1024 * 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "TB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "B") {
		sizeStr = strings.TrimSuffix(sizeStr, "B")
	}

	// 解析数字部分
	value, err := strconv.ParseInt(strings.TrimSpace(sizeStr), 10, 64)
	if err != nil {
		return 0, err
	}

	return value * multiplier, nil
}

// parseCleanStrategy 解析清理策略字符串
func parseCleanStrategy(strategy string) CleanStrategy {
	switch strings.ToUpper(strategy) {
	case "LFU":
		return LFU
	case "FIFO":
		return FIFO
	default:
		return LRU
	}
}

func main() {
	flag.Parse()

	// 加载配置文件（如果指定）
	if *configFile != "" {
		config, err := LoadConfig(*configFile)
		if err != nil {
			log.Fatalln(t("FailedToLoadConfigFile", map[string]any{"Error": err}))
		}
		if config != nil {
			if err := ApplyConfig(config); err != nil {
				log.Fatalln(t("FailedToApplyConfig", map[string]any{"Error": err}))
			}
			log.Println(t("LoadedConfigFrom", map[string]any{"Path": *configFile}))
		}
	}

	// 初始化上游服务器管理器
	upstreamManager = NewUpstreamManager()

	// 如果没有从配置文件加载上游服务器，使用命令行参数
	if upstreamManager.GetServerCount() == 0 {
		server := NewUpstreamServer(*upstreamURL, *proxyURL, "default")
		upstreamManager.AddServer(server)
	}

	initI18n()

	// 初始化缓存配额管理器
	if *cacheMaxSize != "" {
		maxSize, err := parseSizeString(*cacheMaxSize)
		if err != nil {
			log.Fatalln(t("InvalidCacheMaxSize", map[string]any{"Error": err}))
		}

		strategy := parseCleanStrategy(*cacheCleanStrategy)
		cacheQuota = NewCacheQuota(maxSize, strategy)

		// 初始化当前缓存大小
		if err := cacheQuota.InitializeCacheSize(); err != nil {
			log.Println(t("CacheSizeInitFailed", map[string]any{"Error": err}))
		}

		if maxSize > 0 {
			log.Println(t("CacheQuotaEnabled", map[string]any{
				"MaxSize":  maxSize,
				"Strategy": strategy.String(),
			}))
		} else {
			log.Println(t("CacheQuotaDisabled", nil))
		}
	}

	// 初始化内存缓存
	if *memoryCacheEnabled {
		memoryMaxSize, err := parseSizeString(*memoryCacheSize)
		if err != nil {
			log.Fatalln(t("InvalidMemoryCacheSize", map[string]any{"Error": err}))
		}

		memoryCacheMaxFileSizeBytes, err = parseSizeString(*memoryCacheMaxFileSize)
		if err != nil {
			log.Fatalln(t("InvalidMemoryCacheMaxFileSize", map[string]any{"Error": err}))
		}

		memoryCache = NewMemoryCache(memoryMaxSize, *memoryCacheMaxItems, *memoryCacheTTL)
		log.Println(t("MemoryCacheEnabled", map[string]any{
			"MaxSize":     memoryMaxSize,
			"MaxItems":    *memoryCacheMaxItems,
			"TTL":         *memoryCacheTTL,
			"MaxFileSize": memoryCacheMaxFileSizeBytes,
		}))
	} else {
		log.Println(t("MemoryCacheDisabled", nil))
	}

	// 注册 Prometheus 指标到自定义 Registry
	registry.MustRegister(cacheHits)
	registry.MustRegister(cacheMisses)
	registry.MustRegister(downloadBytes)
	registry.MustRegister(cacheHitBytes)
	registry.MustRegister(cacheMissBytes)
	registry.MustRegister(rateLimitAllowed)
	registry.MustRegister(rateLimitRejected)
	registry.MustRegister(rateLimitCurrentTokens)

	// 初始化请求限流器
	if *rateLimitEnabled {
		rateLimiter = NewRateLimiter(*rateLimitRate, *rateLimitBurst)
		log.Println(t("RateLimitEnabled", map[string]any{
			"Rate":  *rateLimitRate,
			"Burst": *rateLimitBurst,
		}))
	} else {
		log.Println(t("RateLimitDisabled", nil))
	}

	// 创建缓存目录
	if err := os.MkdirAll(*cachePath, 0755); err != nil {
		log.Fatalln(t("CreateCacheDirFailed", map[string]any{"Error": err}))
	}

	// 启动自动清理
	if *cleanupInterval > 0 && *pkgCacheDuration != 0 {
		go startAutoCleanup()
		log.Println(t("AutoCleanupEnabled", map[string]any{"Interval": *cleanupInterval}))
	}

	// 启动健康检查循环
	go healthCheckManager.StartHealthCheckLoop()

	// 启动限流指标更新循环
	if *rateLimitEnabled {
		go updateRateLimitMetrics()
	}

	// 初始化数据完整性管理器
	if *dataIntegrityCheckInterval > 0 {
		dataIntegrityManager = NewDataIntegrityManager(
			*dataIntegrityCheckInterval,
			*dataIntegrityAutoRepair,
			*dataIntegrityPeriodicCheck,
		)

		// 初始化现有文件的哈希记录（仅在配置启用时）
		if *dataIntegrityInitializeExistingFiles {
			if err := dataIntegrityManager.InitializeExistingFiles(); err != nil {
				log.Println(t("DataIntegrityInitFailed", map[string]any{"Error": err}))
			} else {
				log.Println(t("DataIntegrityEnabled", map[string]any{
					"Interval":                *dataIntegrityCheckInterval,
					"AutoRepair":              *dataIntegrityAutoRepair,
					"PeriodicCheck":           *dataIntegrityPeriodicCheck,
					"InitializeExistingFiles": *dataIntegrityInitializeExistingFiles,
				}))
			}
		} else {
			log.Println(t("DataIntegrityEnabled", map[string]any{
				"Interval":                *dataIntegrityCheckInterval,
				"AutoRepair":              *dataIntegrityAutoRepair,
				"PeriodicCheck":           *dataIntegrityPeriodicCheck,
				"InitializeExistingFiles": *dataIntegrityInitializeExistingFiles,
			}))
		}

		// 启动定期检查
		dataIntegrityManager.StartPeriodicCheck()
	} else {
		log.Println(t("DataIntegrityDisabled", nil))
	}

	var handler webHandler

	handler.metricsHandler = rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})
	handler.adminHandler = authMiddleware(rateLimitAdminMiddleware(adminDashboardHandler))
	handler.healthHandler = authMiddleware(healthCheckManager.HealthCheckHandler)

	handler.rootHandler = rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// 检查是否是CONNECT方法（HTTPS代理）或代理请求
		if proxyIsProxyRequest(r) {
			handleProxyRequest(w, r)
		} else {
			// 非代理请求使用原有的APK缓存逻辑
			proxyHandler(w, r)
		}
	})

	log.Println(t("ServerStarted", map[string]any{"Addr": *listenAddr}))
	log.Println(t("UpstreamServer", map[string]any{"URL": *upstreamURL}))
	log.Println(t("CacheDirectory", map[string]any{"Path": *cachePath}))
	if *proxyURL != "" {
		log.Println(t("ProxyServer", map[string]any{"Proxy": *proxyURL}))
	}

	if err := http.ListenAndServe(*listenAddr, &handler); err != nil {
		log.Fatalln(t("ServerStartFailed", map[string]any{"Error": err}))
	}
}

// ServeHTTP implements http.Handler.
func (h *webHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/metrics" {
		h.metricsHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/_admin") {
		h.adminHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/_health") {
		h.healthHandler.ServeHTTP(w, r)
		return
	}

	h.rootHandler.ServeHTTP(w, r)
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
		if !ok || username != *adminUser || password != *adminPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
