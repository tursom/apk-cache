package config

import (
	"errors"
	"time"

	"github.com/BurntSushi/toml"
)

// Config 配置文件结构
type Config struct {
	Server            ServerConfig              `toml:"server"`
	Upstreams        []UpstreamConfig           `toml:"upstreams"`
	Cache            CacheConfig                `toml:"cache"`
	Security         SecurityConfig             `toml:"security"`
	HealthCheck      HealthCheckConfig          `toml:"health_check"`
	RateLimit        RateLimitConfig            `toml:"rate_limit"`
	DataIntegrity    DataIntegrityConfig        `toml:"data_integrity"`
	FineGrainedPolicy FineGrainedPolicyConfig   `toml:"fine_grained_policy"`
}

type ServerConfig struct {
	Addr   string `toml:"addr"`
	Locale string `toml:"locale"`
}

type UpstreamConfig struct {
	URL   string `toml:"url"`
	Proxy string `toml:"proxy"`
	Name  string `toml:"name"`
}

type CacheConfig struct {
	Dir                  string `toml:"dir"`
	DataDir              string `toml:"data_dir"`
	IndexDuration        string `toml:"index_duration"`
	PkgDuration          string `toml:"pkg_duration"`
	CleanupInterval      string `toml:"cleanup_interval"`
	MaxSize              string `toml:"max_size"`
	CleanStrategy        string `toml:"clean_strategy"`
	MemoryCacheEnabled   bool   `toml:"memory_cache_enabled"`
	MemoryCacheSize     string `toml:"memory_cache_size"`
	MemoryCacheMaxItems int    `toml:"memory_cache_max_items"`
	MemoryCacheTTL      string `toml:"memory_cache_ttl"`
	MemoryCacheMaxFileSize string `toml:"memory_cache_max_file_size"`
}

type SecurityConfig struct {
	AdminUser              string `toml:"admin_user"`
	AdminPassword          string `toml:"admin_password"`
	ProxyAuthEnabled       bool   `toml:"proxy_auth_enabled"`
	ProxyUser              string `toml:"proxy_user"`
	ProxyPassword          string `toml:"proxy_password"`
	ProxyAuthExemptIPs     string `toml:"proxy_auth_exempt_ips"`
	TrustedReverseProxyIPs string `toml:"trusted_reverse_proxy_ips"`
}

type HealthCheckConfig struct {
	Interval          string `toml:"interval"`
	Timeout           string `toml:"timeout"`
	EnableSelfHealing bool   `toml:"enable_self_healing"`
}

type RateLimitConfig struct {
	Enabled     bool    `toml:"enabled"`
	Rate        float64 `toml:"rate"`
	Burst       float64 `toml:"burst"`
	ExemptPaths string  `toml:"exempt_paths"`
}

type DataIntegrityConfig struct {
	CheckInterval string `toml:"check_interval"`
	AutoRepair    bool   `toml:"auto_repair"`
	PeriodicCheck bool   `toml:"periodic_check"`
}

type FineGrainedPolicyConfig struct {
	Policy         string              `toml:"policy"`
	SizeSmall      string              `toml:"size_small"`
	SizeMedium     string              `toml:"size_medium"`
	SizeLarge      string              `toml:"size_large"`
	HotThreshold   int                 `toml:"hot_threshold"`
	ColdThreshold  int                 `toml:"cold_threshold"`
	Adaptive       bool                `toml:"adaptive"`
	AdjustInterval string              `toml:"adjust_interval"`
	TypeRules      []TypeRuleConfig    `toml:"type_rules"`
}

type TypeRuleConfig struct {
	Pattern     string `toml:"pattern"`
	Priority    string `toml:"priority"`
	TTL         string `toml:"ttl"`
	MemoryCache bool   `toml:"memory_cache"`
	Preload     bool   `toml:"preload"`
}

// Load 加载配置文件
func Load(path string) (*Config, error) {
	var config Config
	_, err := toml.DecodeFile(path, &config)
	if err != nil {
		return nil, err
	}

	if err := Validate(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate 验证配置有效性
func Validate(config *Config) error {
	if config.Server.Addr != "" && !contains(config.Server.Addr, ":") {
		return errors.New("invalid server address: must contain ':'")
	}

	if config.Server.Locale != "" {
		supported := map[string]bool{"en": true, "zh": true, "": true}
		if !supported[config.Server.Locale] {
			return errors.New("unsupported locale: " + config.Server.Locale)
		}
	}

	for i, upstream := range config.Upstreams {
		if upstream.URL == "" {
			return errors.New("upstream URL required at index " + string(rune(i)))
		}
		if !hasPrefix(upstream.URL, "http://") && !hasPrefix(upstream.URL, "https://") {
			return errors.New("invalid upstream URL: " + upstream.URL)
		}
	}

	if config.Cache.Dir != "" && contains(config.Cache.Dir, "..") {
		return errors.New("invalid cache directory: path traversal not allowed")
	}

	if config.Cache.CleanStrategy != "" {
		supported := map[string]bool{"LRU": true, "LFU": true, "FIFO": true, "": true}
		if !supported[upper(config.Cache.CleanStrategy)] {
			return errors.New("unsupported clean strategy: " + config.Cache.CleanStrategy)
		}
	}

	return nil
}

// Helper functions to avoid importing strings package
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[0:len(prefix)] == prefix
}

func upper(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

// ParseDuration 解析持续时间字符串
func ParseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}
