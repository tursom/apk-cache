package main

import (
	"errors"
	"flag"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/tursom/apk-cache/utils/i18n"
)

// 命令行参数定义
var (
	listenAddr         = flag.String("addr", ":3142", "Listen address")
	cachePath          = flag.String("cache", "./cache", "Cache directory path")
	dataPath           = flag.String("data", "./data", "Data directory path for internal program data")
	upstreamURL        = flag.String("upstream", "https://dl-cdn.alpinelinux.org", "Upstream server URL")
	proxyURL           = flag.String("proxy", "", "Proxy address (e.g. socks5://127.0.0.1:1080 or http://127.0.0.1:8080)")
	indexCacheDuration = flag.Duration("index-cache", 24*time.Hour, "APKINDEX.tar.gz cache duration")
	pkgCacheDuration   = flag.Duration("pkg-cache", 0, "Package cache duration (0 = never expire)")
	cleanupInterval    = flag.Duration("cleanup-interval", time.Hour, "Automatic cleanup interval (0 = disabled)")
	locale             = flag.String("locale", "", "Language (en/zh), auto-detect if empty")
	adminUser          = flag.String("admin-user", "admin", "Admin dashboard username")
	adminPassword      = flag.String("admin-password", "", "Admin dashboard password (empty = no auth)")
	configFile         = flag.String("config", "", "Config file path (optional)")
	// 代理身份验证参数
	proxyAuthEnabled = flag.Bool("proxy-auth", false, "Enable proxy authentication")
	proxyUser        = flag.String("proxy-user", "proxy", "Proxy authentication username")
	proxyPassword    = flag.String("proxy-password", "", "Proxy authentication password (empty = no auth)")
	// 不需要验证的 IP 网段（CIDR格式，逗号分隔）
	proxyAuthExemptIPs = flag.String("proxy-auth-exempt-ips", "", "Comma-separated list of IP ranges exempt from proxy auth (CIDR format)")
	// 信任的 nginx 反向代理 IP（逗号分隔）
	trustedReverseProxyIPs = flag.String("trusted-reverse-proxy-ips", "", "Comma-separated list of trusted reverse proxy IPs")
	// 缓存配额相关参数
	cacheMaxSize       = flag.String("cache-max-size", "", "Maximum cache size (e.g. 10GB, 1TB, 0 = unlimited)")
	cacheCleanStrategy = flag.String("cache-clean-strategy", "LRU", "Cache cleanup strategy (LRU, LFU, FIFO)")

	// 内存缓存相关参数
	memoryCacheEnabled     = flag.Bool("memory-cache", false, "Enable memory cache")
	memoryCacheSize        = flag.String("memory-cache-size", "100MB", "Memory cache size (e.g. 100MB, 1GB)")
	memoryCacheMaxItems    = flag.Int("memory-cache-max-items", 1000, "Maximum number of items in memory cache")
	memoryCacheTTL         = flag.Duration("memory-cache-ttl", 30*time.Minute, "Memory cache TTL duration")
	memoryCacheMaxFileSize = flag.String("memory-cache-max-file-size", "10MB", "Maximum file size to cache in memory (e.g. 1MB, 10MB)")
	// 内存缓存最大文件大小
	memoryCacheMaxFileSizeBytes int64

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
	dataIntegrityCheckInterval = flag.Duration("data-integrity-check-interval", time.Hour, "Data integrity check interval (0 = disabled)")
	dataIntegrityAutoRepair    = flag.Bool("data-integrity-auto-repair", true, "Enable automatic repair of corrupted files")
	dataIntegrityPeriodicCheck = flag.Bool("data-integrity-periodic-check", true, "Enable periodic data integrity checks")
)

// 预处理后的限流豁免路径列表
var rateLimitExemptPathsList []string

// Config 配置文件结构
type Config struct {
	Server        ServerConfig        `toml:"server"`
	Upstreams     []UpstreamConfig    `toml:"upstreams"`
	Cache         CacheConfig         `toml:"cache"`
	Security      SecurityConfig      `toml:"security"`
	HealthCheck   HealthCheckConfig   `toml:"health_check"`
	RateLimit     RateLimitConfig     `toml:"rate_limit"`
	DataIntegrity DataIntegrityConfig `toml:"data_integrity"`
}

type ServerConfig struct {
	Addr   string `toml:"addr"`
	Locale string `toml:"locale"`
}

type UpstreamConfig struct {
	URL   string `toml:"url"`
	Proxy string `toml:"proxy"`
	Name  string `toml:"name"` // 可选的服务器名称
}

type CacheConfig struct {
	Dir             string `toml:"dir"`
	DataDir         string `toml:"data_dir"` // 新增：数据目录路径
	IndexDuration   string `toml:"index_duration"`
	PkgDuration     string `toml:"pkg_duration"`
	CleanupInterval string `toml:"cleanup_interval"`
	MaxSize         string `toml:"max_size"`       // 新增：最大缓存大小（如 "10GB", "1TB"）
	CleanStrategy   string `toml:"clean_strategy"` // 新增：清理策略（"LRU", "LFU", "FIFO"）
	// 新增：内存缓存配置
	MemoryCacheEnabled     bool   `toml:"memory_cache_enabled"`
	MemoryCacheSize        string `toml:"memory_cache_size"`
	MemoryCacheMaxItems    int    `toml:"memory_cache_max_items"`
	MemoryCacheTTL         string `toml:"memory_cache_ttl"`
	MemoryCacheMaxFileSize string `toml:"memory_cache_max_file_size"`
}

type SecurityConfig struct {
	AdminUser     string `toml:"admin_user"`
	AdminPassword string `toml:"admin_password"`
	// 代理身份验证配置
	ProxyAuthEnabled bool   `toml:"proxy_auth_enabled"`
	ProxyUser        string `toml:"proxy_user"`
	ProxyPassword    string `toml:"proxy_password"`
	// 不需要验证的 IP 网段（CIDR格式，逗号分隔）
	ProxyAuthExemptIPs string `toml:"proxy_auth_exempt_ips"`
	// 信任的 nginx 反向代理 IP（逗号分隔）
	TrustedReverseProxyIPs string `toml:"trusted_reverse_proxy_ips"`
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Interval          string `toml:"interval"`
	Timeout           string `toml:"timeout"`
	EnableSelfHealing bool   `toml:"enable_self_healing"`
}

// RateLimitConfig 请求限流配置
type RateLimitConfig struct {
	Enabled     bool    `toml:"enabled"`
	Rate        float64 `toml:"rate"`
	Burst       float64 `toml:"burst"`
	ExemptPaths string  `toml:"exempt_paths"`
}

// DataIntegrityConfig 数据完整性校验配置
type DataIntegrityConfig struct {
	CheckInterval string `toml:"check_interval"`
	AutoRepair    bool   `toml:"auto_repair"`
	PeriodicCheck bool   `toml:"periodic_check"`
}

// LoadConfig 加载配置文件
func LoadConfig(path string) (*Config, error) {
	// 如果配置文件不存在，返回默认配置
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	var config Config
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, errors.New(i18n.T("ParseConfigFailed", map[string]any{"Error": err}))
	}

	// 验证配置
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// ApplyConfig 应用配置到命令行参数
func ApplyConfig(config *Config) error {
	if config == nil {
		return nil
	}

	// Server 配置
	if config.Server.Addr != "" && !isFlagSet("addr") {
		*listenAddr = config.Server.Addr
	}
	if config.Server.Locale != "" && !isFlagSet("locale") {
		*locale = config.Server.Locale
	}

	// Upstreams 配置
	if len(config.Upstreams) > 0 && !isFlagSet("upstream") {
		// 从配置文件加载上游服务器列表（仅在命令行未指定时）
		for _, upstream := range config.Upstreams {
			server := NewUpstreamServer(upstream.URL, upstream.Proxy, upstream.Name)
			upstreamManager.AddServer(server)
		}
	} else if upstreamManager.GetServerCount() == 0 && !isFlagSet("upstream") {
		// 如果配置文件中没有 upstreams，也没有命令行参数，使用默认值
		server := NewUpstreamServer(*upstreamURL, *proxyURL, "default")
		upstreamManager.AddServer(server)
	}

	// Cache 配置
	if config.Cache.Dir != "" && !isFlagSet("cache") {
		*cachePath = config.Cache.Dir
	}
	if config.Cache.DataDir != "" && !isFlagSet("data") {
		*dataPath = config.Cache.DataDir
	}
	if config.Cache.IndexDuration != "" && !isFlagSet("index-cache") {
		duration, err := time.ParseDuration(config.Cache.IndexDuration)
		if err != nil {
			return errors.New(i18n.T("InvalidIndexDuration", map[string]any{"Error": err}))
		}
		*indexCacheDuration = duration
	}
	if config.Cache.PkgDuration != "" && !isFlagSet("pkg-cache") {
		duration, err := time.ParseDuration(config.Cache.PkgDuration)
		if err != nil {
			return errors.New(i18n.T("InvalidPkgDuration", map[string]any{"Error": err}))
		}
		*pkgCacheDuration = duration
	}
	if config.Cache.CleanupInterval != "" && !isFlagSet("cleanup-interval") {
		duration, err := time.ParseDuration(config.Cache.CleanupInterval)
		if err != nil {
			return errors.New(i18n.T("InvalidCleanupInterval", map[string]any{"Error": err}))
		}
		*cleanupInterval = duration
	}
	// 新增：缓存配额配置
	if config.Cache.MaxSize != "" && !isFlagSet("cache-max-size") {
		*cacheMaxSize = config.Cache.MaxSize
	}
	if config.Cache.CleanStrategy != "" && !isFlagSet("cache-clean-strategy") {
		*cacheCleanStrategy = config.Cache.CleanStrategy
	}

	// 新增：内存缓存配置
	if !isFlagSet("memory-cache") {
		*memoryCacheEnabled = config.Cache.MemoryCacheEnabled
	}
	if config.Cache.MemoryCacheSize != "" && !isFlagSet("memory-cache-size") {
		*memoryCacheSize = config.Cache.MemoryCacheSize
	}
	if config.Cache.MemoryCacheMaxItems > 0 && !isFlagSet("memory-cache-max-items") {
		*memoryCacheMaxItems = config.Cache.MemoryCacheMaxItems
	}
	if config.Cache.MemoryCacheTTL != "" && !isFlagSet("memory-cache-ttl") {
		duration, err := time.ParseDuration(config.Cache.MemoryCacheTTL)
		if err != nil {
			return errors.New(i18n.T("InvalidMemoryCacheTTL", map[string]any{"Error": err}))
		}
		*memoryCacheTTL = duration
	}
	if config.Cache.MemoryCacheMaxFileSize != "" && !isFlagSet("memory-cache-max-file-size") {
		*memoryCacheMaxFileSize = config.Cache.MemoryCacheMaxFileSize
	}

	// Security 配置
	if config.Security.AdminUser != "" && !isFlagSet("admin-user") {
		*adminUser = config.Security.AdminUser
	}
	if config.Security.AdminPassword != "" && !isFlagSet("admin-password") {
		*adminPassword = config.Security.AdminPassword
	}
	// 代理身份验证配置
	if !isFlagSet("proxy-auth") {
		*proxyAuthEnabled = config.Security.ProxyAuthEnabled
	}
	if config.Security.ProxyUser != "" && !isFlagSet("proxy-user") {
		*proxyUser = config.Security.ProxyUser
	}
	if config.Security.ProxyPassword != "" && !isFlagSet("proxy-password") {
		*proxyPassword = config.Security.ProxyPassword
	}
	// 不需要验证的 IP 网段配置
	if config.Security.ProxyAuthExemptIPs != "" && !isFlagSet("proxy-auth-exempt-ips") {
		*proxyAuthExemptIPs = config.Security.ProxyAuthExemptIPs
	}
	// 信任的反向代理 IP 配置
	if config.Security.TrustedReverseProxyIPs != "" && !isFlagSet("trusted-reverse-proxy-ips") {
		*trustedReverseProxyIPs = config.Security.TrustedReverseProxyIPs
	}

	// HealthCheck 配置
	if config.HealthCheck.Interval != "" && !isFlagSet("health-check-interval") {
		duration, err := time.ParseDuration(config.HealthCheck.Interval)
		if err != nil {
			return errors.New(i18n.T("InvalidHealthCheckInterval", map[string]any{"Error": err}))
		}
		*healthCheckInterval = duration
	}
	if config.HealthCheck.Timeout != "" && !isFlagSet("health-check-timeout") {
		duration, err := time.ParseDuration(config.HealthCheck.Timeout)
		if err != nil {
			return errors.New(i18n.T("InvalidHealthCheckTimeout", map[string]any{"Error": err}))
		}
		*healthCheckTimeout = duration
	}
	if !isFlagSet("enable-self-healing") {
		*enableSelfHealing = config.HealthCheck.EnableSelfHealing
	}

	// RateLimit 配置
	if !isFlagSet("rate-limit") {
		*rateLimitEnabled = config.RateLimit.Enabled
	}
	if config.RateLimit.Rate > 0 && !isFlagSet("rate-limit-rate") {
		*rateLimitRate = config.RateLimit.Rate
	}
	if config.RateLimit.Burst > 0 && !isFlagSet("rate-limit-burst") {
		*rateLimitBurst = config.RateLimit.Burst
	}
	if config.RateLimit.ExemptPaths != "" && !isFlagSet("rate-limit-exempt-paths") {
		*rateLimitExemptPaths = config.RateLimit.ExemptPaths
	}

	// 预处理限流豁免路径
	preprocessRateLimitExemptPaths()

	// DataIntegrity 配置
	if config.DataIntegrity.CheckInterval != "" && !isFlagSet("data-integrity-check-interval") {
		duration, err := time.ParseDuration(config.DataIntegrity.CheckInterval)
		if err != nil {
			return errors.New(i18n.T("InvalidDataIntegrityCheckInterval", map[string]any{"Error": err}))
		}
		*dataIntegrityCheckInterval = duration
	}
	if !isFlagSet("data-integrity-auto-repair") {
		*dataIntegrityAutoRepair = config.DataIntegrity.AutoRepair
	}
	if !isFlagSet("data-integrity-periodic-check") {
		*dataIntegrityPeriodicCheck = config.DataIntegrity.PeriodicCheck
	}

	return nil
}

// isFlagSet 检查命令行参数是否被设置
func isFlagSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// validateConfig 验证配置的有效性
func validateConfig(config *Config) error {
	// 验证服务器配置
	if config.Server.Addr != "" {
		if !strings.Contains(config.Server.Addr, ":") {
			return errors.New(i18n.T("InvalidServerAddr", map[string]any{"Addr": config.Server.Addr}))
		}
	}

	// 验证语言设置
	if config.Server.Locale != "" {
		supportedLocales := map[string]bool{
			"en": true,
			"zh": true,
			"":   true,
		}
		if !supportedLocales[config.Server.Locale] {
			return errors.New(i18n.T("UnsupportedLocale", map[string]any{"Locale": config.Server.Locale}))
		}
	}

	// 验证上游服务器配置
	for i, upstream := range config.Upstreams {
		if upstream.URL == "" {
			return errors.New(i18n.T("UpstreamURLRequired", map[string]any{"Index": i}))
		}
		if !strings.HasPrefix(upstream.URL, "http://") && !strings.HasPrefix(upstream.URL, "https://") {
			return errors.New(i18n.T("InvalidUpstreamURL", map[string]any{"URL": upstream.URL}))
		}
	}

	// 验证缓存配置
	if config.Cache.Dir != "" {
		if strings.Contains(config.Cache.Dir, "..") {
			return errors.New(i18n.T("InvalidCacheDir", map[string]any{"Dir": config.Cache.Dir}))
		}
	}

	// 验证缓存持续时间
	if config.Cache.IndexDuration != "" {
		if _, err := time.ParseDuration(config.Cache.IndexDuration); err != nil {
			return errors.New(i18n.T("InvalidIndexDuration", map[string]any{"Error": err}))
		}
	}
	if config.Cache.PkgDuration != "" {
		if _, err := time.ParseDuration(config.Cache.PkgDuration); err != nil {
			return errors.New(i18n.T("InvalidPkgDuration", map[string]any{"Error": err}))
		}
	}
	if config.Cache.CleanupInterval != "" {
		if _, err := time.ParseDuration(config.Cache.CleanupInterval); err != nil {
			return errors.New(i18n.T("InvalidCleanupInterval", map[string]any{"Error": err}))
		}
	}

	// 验证缓存清理策略
	if config.Cache.CleanStrategy != "" {
		supportedStrategies := map[string]bool{
			"LRU":  true,
			"LFU":  true,
			"FIFO": true,
			"":     true,
		}
		if !supportedStrategies[strings.ToUpper(config.Cache.CleanStrategy)] {
			return errors.New(i18n.T("UnsupportedCleanStrategy", map[string]any{"Strategy": config.Cache.CleanStrategy}))
		}
	}

	return nil
}

// preprocessRateLimitExemptPaths 预处理限流豁免路径
func preprocessRateLimitExemptPaths() {
	if *rateLimitExemptPaths == "" {
		rateLimitExemptPathsList = []string{}
		return
	}

	// 分割并清理路径
	paths := strings.Split(*rateLimitExemptPaths, ",")
	rateLimitExemptPathsList = make([]string, 0, len(paths))

	for _, path := range paths {
		trimmedPath := strings.TrimSpace(path)
		if trimmedPath != "" {
			rateLimitExemptPathsList = append(rateLimitExemptPathsList, trimmedPath)
		}
	}
}
