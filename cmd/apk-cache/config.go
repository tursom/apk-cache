package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config 配置文件结构
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Upstream UpstreamConfig `toml:"upstream"`
	Cache    CacheConfig    `toml:"cache"`
	Security SecurityConfig `toml:"security"`
}

type ServerConfig struct {
	Addr   string `toml:"addr"`
	Locale string `toml:"locale"`
}

type UpstreamConfig struct {
	URL   string `toml:"url"`
	Proxy string `toml:"proxy"`
}

type CacheConfig struct {
	Dir             string `toml:"dir"`
	IndexDuration   string `toml:"index_duration"`
	PkgDuration     string `toml:"pkg_duration"`
	CleanupInterval string `toml:"cleanup_interval"`
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
		return nil, fmt.Errorf("failed to parse config file: %w", err)
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

	// Upstream 配置
	if config.Upstream.URL != "" && !isFlagSet("upstream") {
		*upstreamURL = config.Upstream.URL
	}
	if config.Upstream.Proxy != "" && !isFlagSet("proxy") {
		*socks5Proxy = config.Upstream.Proxy
	}

	// Cache 配置
	if config.Cache.Dir != "" && !isFlagSet("cache") {
		*cachePath = config.Cache.Dir
	}
	if config.Cache.IndexDuration != "" && !isFlagSet("index-cache") {
		duration, err := time.ParseDuration(config.Cache.IndexDuration)
		if err != nil {
			return fmt.Errorf("invalid index_duration: %w", err)
		}
		*indexCacheDuration = duration
	}
	if config.Cache.PkgDuration != "" && !isFlagSet("pkg-cache") {
		duration, err := time.ParseDuration(config.Cache.PkgDuration)
		if err != nil {
			return fmt.Errorf("invalid pkg_duration: %w", err)
		}
		*pkgCacheDuration = duration
	}
	if config.Cache.CleanupInterval != "" && !isFlagSet("cleanup-interval") {
		duration, err := time.ParseDuration(config.Cache.CleanupInterval)
		if err != nil {
			return fmt.Errorf("invalid cleanup_interval: %w", err)
		}
		*cleanupInterval = duration
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
