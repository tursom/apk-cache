package main

import (
	"context"
	_ "embed"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tursom/apk-cache/utils"
	"github.com/tursom/apk-cache/utils/data_integrity"
	"github.com/tursom/apk-cache/utils/i18n"
)

var (
	// 进程启动时间
	processStartTime = time.Now()

	// 监控管理器
	monitoring = utils.Monitoring
	// 上游服务器管理器（支持故障转移和健康检查）
	upstreamManager = NewUpstreamManager()
	// 文件锁管理器
	lockManager = utils.NewFileLockManager()
	// 访问时间跟踪器（在 main 中初始化，以便 ApplyConfig 后使用正确的 dataPath）
	accessTimeTracker AccessTimeTracker
	// 缓存配额管理器
	cacheQuota *CacheQuota
	// 内存缓存管理器
	memoryCache *MemoryCache
	// 请求限流器
	rateLimiter *utils.RateLimiter
	// 数据完整性管理器
	dataIntegrityManager data_integrity.Manager
	// IP匹配器（用于代理身份验证）
	proxyIPMatcher *utils.IPMatcher
)

func main() {
	flag.Parse()

	// 加载配置文件（如果指定）
	if *configFile != "" {
		config, err := LoadConfig(*configFile)
		if err != nil {
			log.Fatalln(i18n.T("FailedToLoadConfigFile", map[string]any{"Error": err}))
		}
		if config != nil {
			if err := ApplyConfig(config); err != nil {
				log.Fatalln(i18n.T("FailedToApplyConfig", map[string]any{"Error": err}))
			}
			log.Println(i18n.T("LoadedConfigFrom", map[string]any{"Path": *configFile}))
		}
	}

	// 如果没有从配置文件加载上游服务器，使用命令行参数
	if upstreamManager.GetServerCount() == 0 {
		server := NewUpstreamServer(*upstreamURL, *proxyURL, "default")
		upstreamManager.AddServer(server)
	}

	// 初始化 i18n
	i18n.Init(*locale)

	// 初始化缓存配额管理器
	if *cacheMaxSize != "" {
		maxSize, err := utils.ParseSizeString(*cacheMaxSize)
		if err != nil {
			log.Fatalln(i18n.T("InvalidCacheMaxSize", map[string]any{"Error": err}))
		}

		strategy := parseCleanStrategy(*cacheCleanStrategy)
		cacheQuota = NewCacheQuota(maxSize, strategy)

		// 初始化当前缓存大小
		if err := cacheQuota.InitializeCacheSize(); err != nil {
			log.Println(i18n.T("CacheSizeInitFailed", map[string]any{"Error": err}))
		}

		if maxSize > 0 {
			log.Println(i18n.T("CacheQuotaEnabled", map[string]any{
				"MaxSize":  maxSize,
				"Strategy": strategy.String(),
			}))
		} else {
			log.Println(i18n.T("CacheQuotaDisabled", nil))
		}
	}

	// 初始化内存缓存
	if *memoryCacheEnabled {
		memoryMaxSize, err := utils.ParseSizeString(*memoryCacheSize)
		if err != nil {
			log.Fatalln(i18n.T("InvalidMemoryCacheSize", map[string]any{"Error": err}))
		}

		memoryCacheMaxFileSizeBytes, err = utils.ParseSizeString(*memoryCacheMaxFileSize)
		if err != nil {
			log.Fatalln(i18n.T("InvalidMemoryCacheMaxFileSize", map[string]any{"Error": err}))
		}

		memoryCache = NewMemoryCache(memoryMaxSize, *memoryCacheMaxItems, *memoryCacheTTL)
		log.Println(i18n.T("MemoryCacheEnabled", map[string]any{
			"MaxSize":     memoryMaxSize,
			"MaxItems":    *memoryCacheMaxItems,
			"TTL":         *memoryCacheTTL,
			"MaxFileSize": memoryCacheMaxFileSizeBytes,
		}))
	} else {
		log.Println(i18n.T("MemoryCacheDisabled", nil))
	}

	// 初始化请求限流器
	if *rateLimitEnabled {
		rateLimiter = utils.NewRateLimiter(*rateLimitRate, *rateLimitBurst)
		// 预处理限流豁免路径
		preprocessRateLimitExemptPaths()
		log.Println(i18n.T("RateLimitEnabled", map[string]any{
			"Rate":  *rateLimitRate,
			"Burst": *rateLimitBurst,
		}))
	} else {
		log.Println(i18n.T("RateLimitDisabled", nil))
	}

	// 创建缓存目录
	if err := os.MkdirAll(*cachePath, 0755); err != nil {
		log.Fatalln(i18n.T("CreateCacheDirFailed", map[string]any{"Error": err}))
	}

	// 创建数据目录
	if err := os.MkdirAll(*dataPath, 0755); err != nil {
		log.Fatalln(i18n.T("CreateDataDirFailed", map[string]any{"Error": err}))
	}

	// 初始化访问时间跟踪器（必须在 ApplyConfig 后，以便使用正确的 dataPath）
	accessTimeTracker = NewAccessTimeTracker()

	// 启动自动清理
	if *cleanupInterval > 0 && *pkgCacheDuration != 0 {
		go startAutoCleanup()
		log.Println(i18n.T("AutoCleanupEnabled", map[string]any{"Interval": *cleanupInterval}))
	}

	// 启动限流指标更新循环
	if *rateLimitEnabled {
		go updateRateLimitMetrics()
	}

	// 初始化数据完整性管理器
	if *dataIntegrityCheckInterval > 0 {
		dataIntegrityManager = data_integrity.NewManager(
			*cachePath,
			*dataPath,
			*dataIntegrityCheckInterval,
			*dataIntegrityAutoRepair,
			*dataIntegrityPeriodicCheck,
		)

		log.Println(i18n.T("DataIntegrityEnabled", map[string]any{
			"Interval":      *dataIntegrityCheckInterval,
			"AutoRepair":    *dataIntegrityAutoRepair,
			"PeriodicCheck": *dataIntegrityPeriodicCheck,
		}))

		// 启动定期检查
		dataIntegrityManager.StartPeriodicCheck()
	} else {
		log.Println(i18n.T("DataIntegrityDisabled", nil))
	}

	// 初始化代理IP匹配器（如果启用了代理身份验证）
	if *proxyAuthEnabled {
		var err error
		proxyIPMatcher, err = utils.NewIPMatcher(*proxyAuthExemptIPs, *trustedReverseProxyIPs)
		if err != nil {
			log.Printf("Failed to create proxy IP matcher: %v", err)
		}
	}

	log.Println(i18n.T("UpstreamServer", map[string]any{"URL": *upstreamURL}))
	log.Println(i18n.T("CacheDirectory", map[string]any{"Path": *cachePath}))
	if *proxyURL != "" {
		log.Println(i18n.T("ProxyServer", map[string]any{"Proxy": *proxyURL}))
	}

	log.Println(i18n.T("ServerStarted", map[string]any{"Addr": *listenAddr}))

	// 设置优雅关闭
	server := &http.Server{
		Addr:    *listenAddr,
		Handler: newWebHandler(),
	}

	// 启动服务器
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalln(i18n.T("ServerStartFailed", map[string]any{"Error": err}))
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println(i18n.T("ShuttingDownServer", nil))

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Println(i18n.T("ServerShutdownFailed", map[string]any{"Error": err}))
	}

	// 关闭数据完整性管理器
	if dataIntegrityManager != nil {
		if err := dataIntegrityManager.Close(); err != nil {
			log.Println(i18n.T("CloseDataIntegrityManagerFailed", map[string]any{"Error": err}))
		}
	}

	// 关闭访问时间跟踪器
	if err := accessTimeTracker.Close(); err != nil {
		log.Println(i18n.T("CloseAccessTimeTrackerFailed", map[string]any{"Error": err}))
	}

	log.Println(i18n.T("ServerStopped", nil))
}
