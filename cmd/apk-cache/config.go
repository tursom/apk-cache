package main

import (
	"errors"
	"flag"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config 配置文件结构
type Config struct {
	Server    ServerConfig     `toml:"server"`
	Upstreams []UpstreamConfig `toml:"upstreams"`
	Cache     CacheConfig      `toml:"cache"`
	Security  SecurityConfig   `toml:"security"`
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
	IndexDuration   string `toml:"index_duration"`
	PkgDuration     string `toml:"pkg_duration"`
	CleanupInterval string `toml:"cleanup_interval"`
	MaxSize         string `toml:"max_size"`       // 新增：最大缓存大小（如 "10GB", "1TB"）
	CleanStrategy   string `toml:"clean_strategy"` // 新增：清理策略（"LRU", "LFU", "FIFO"）
}

type SecurityConfig struct {
	AdminPassword string `toml:"admin_password"`
}

// LoadConfig 加载配置文件
func LoadConfig(path string) (*Config, error) {
	// 如果配置文件不存在，返回默认配置
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	var config Config
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, errors.New(t("ParseConfigFailed", map[string]any{"Error": err}))
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
	if len(config.Upstreams) > 0 {
		// 从配置文件加载上游服务器列表
		upstreamServers = make([]UpstreamServer, len(config.Upstreams))
		for i, upstream := range config.Upstreams {
			upstreamServers[i] = UpstreamServer{
				URL:   upstream.URL,
				Proxy: upstream.Proxy,
				Name:  upstream.Name,
			}
		}
	} else if !isFlagSet("upstream") {
		// 如果配置文件中没有 upstreams，但有旧的 upstream 字段（向后兼容）
		// 使用默认的命令行参数值
	}

	// Cache 配置
	if config.Cache.Dir != "" && !isFlagSet("cache") {
		*cachePath = config.Cache.Dir
	}
	if config.Cache.IndexDuration != "" && !isFlagSet("index-cache") {
		duration, err := time.ParseDuration(config.Cache.IndexDuration)
		if err != nil {
			return errors.New(t("InvalidIndexDuration", map[string]any{"Error": err}))
		}
		*indexCacheDuration = duration
	}
	if config.Cache.PkgDuration != "" && !isFlagSet("pkg-cache") {
		duration, err := time.ParseDuration(config.Cache.PkgDuration)
		if err != nil {
			return errors.New(t("InvalidPkgDuration", map[string]any{"Error": err}))
		}
		*pkgCacheDuration = duration
	}
	if config.Cache.CleanupInterval != "" && !isFlagSet("cleanup-interval") {
		duration, err := time.ParseDuration(config.Cache.CleanupInterval)
		if err != nil {
			return errors.New(t("InvalidCleanupInterval", map[string]any{"Error": err}))
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

	// Security 配置
	if config.Security.AdminPassword != "" && !isFlagSet("admin-password") {
		*adminPassword = config.Security.AdminPassword
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
