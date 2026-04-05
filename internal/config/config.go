package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server    ServerConfig     `toml:"server"`
	Upstreams []UpstreamConfig `toml:"upstreams"`
	Cache     CacheConfig      `toml:"cache"`
	Transport TransportConfig  `toml:"transport"`
	APK       APKConfig        `toml:"apk"`
	APT       APTConfig        `toml:"apt"`
	Proxy     ProxyConfig      `toml:"proxy"`
}

type ServerConfig struct {
	Listen string `toml:"listen"`
}

type UpstreamConfig struct {
	Name  string `toml:"name"`
	URL   string `toml:"url"`
	Proxy string `toml:"proxy"`
	Kind  string `toml:"kind"`
}

type CacheConfig struct {
	Root       string            `toml:"root"`
	DataRoot   string            `toml:"data_root"`
	IndexTTL   string            `toml:"index_ttl"`
	PackageTTL string            `toml:"package_ttl"`
	Memory     MemoryCacheConfig `toml:"memory"`
}

type MemoryCacheConfig struct {
	Enabled     bool   `toml:"enabled"`
	MaxSize     string `toml:"max_size"`
	MaxItemSize string `toml:"max_item_size"`
	TTL         string `toml:"ttl"`
	MaxItems    int    `toml:"max_items"`
}

type TransportConfig struct {
	Timeout         string `toml:"timeout"`
	IdleConnTimeout string `toml:"idle_conn_timeout"`
	MaxIdleConns    int    `toml:"max_idle_conns"`
}

type APKConfig struct {
	Enabled bool `toml:"enabled"`
	// VerifyHash 控制是否使用 APKINDEX 中的记录校验 .apk 内容。
	VerifyHash bool `toml:"verify_hash"`
	// VerifySignature 控制是否要求 APK/APKINDEX 在写入缓存前通过签名校验。
	VerifySignature bool `toml:"verify_signature"`
	// KeysDir 允许额外加载一组受信任 RSA 公钥，与内置 keyring 合并使用。
	KeysDir string `toml:"keys_dir"`
}

type APTConfig struct {
	Enabled        bool `toml:"enabled"`
	VerifyHash     bool `toml:"verify_hash"`
	LoadIndexAsync bool `toml:"load_index_async"`
}

type ProxyConfig struct {
	Enabled         bool     `toml:"enabled"`
	AllowConnect    bool     `toml:"allow_connect"`
	CacheNonPackage bool     `toml:"cache_non_package_requests"`
	RequireAuth     bool     `toml:"require_auth"`
	UpstreamProxy   string   `toml:"upstream_proxy"`
	AllowedHosts    []string `toml:"allowed_hosts"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Listen: ":3142",
		},
		Upstreams: []UpstreamConfig{
			{
				Name: "Official Alpine CDN",
				URL:  "https://dl-cdn.alpinelinux.org",
				Kind: "apk",
			},
		},
		Cache: CacheConfig{
			Root:       "./cache",
			DataRoot:   "./data",
			IndexTTL:   "24h",
			PackageTTL: "720h",
			Memory: MemoryCacheConfig{
				Enabled:     true,
				MaxSize:     "256MB",
				MaxItemSize: "16MB",
				TTL:         "30m",
				MaxItems:    2048,
			},
		},
		Transport: TransportConfig{
			Timeout:         "30s",
			IdleConnTimeout: "90s",
			MaxIdleConns:    128,
		},
		APK: APKConfig{
			Enabled:         true,
			VerifyHash:      true,
			VerifySignature: true,
			KeysDir:         "",
		},
		APT: APTConfig{
			Enabled:        true,
			VerifyHash:     true,
			LoadIndexAsync: true,
		},
		Proxy: ProxyConfig{
			Enabled:         true,
			AllowConnect:    true,
			CacheNonPackage: false,
			RequireAuth:     false,
			UpstreamProxy:   "",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	if cfg.Server.Listen == "" || !strings.Contains(cfg.Server.Listen, ":") {
		return errors.New("server.listen must include host:port or :port")
	}

	if cfg.Cache.Root == "" {
		return errors.New("cache.root is required")
	}
	if cfg.Cache.DataRoot == "" {
		return errors.New("cache.data_root is required")
	}
	if strings.Contains(cfg.Cache.Root, "..") || strings.Contains(cfg.Cache.DataRoot, "..") {
		return errors.New("cache paths must not contain '..'")
	}

	if err := validateDuration("cache.index_ttl", cfg.Cache.IndexTTL); err != nil {
		return err
	}
	if err := validateDuration("cache.package_ttl", cfg.Cache.PackageTTL); err != nil {
		return err
	}
	if cfg.Cache.Memory.Enabled {
		if err := validateDuration("cache.memory.ttl", cfg.Cache.Memory.TTL); err != nil {
			return err
		}
	}
	if err := validateDuration("transport.timeout", cfg.Transport.Timeout); err != nil {
		return err
	}
	if err := validateDuration("transport.idle_conn_timeout", cfg.Transport.IdleConnTimeout); err != nil {
		return err
	}

	if cfg.APK.Enabled {
		hasAPKUpstream := false
		for _, upstream := range cfg.Upstreams {
			if upstream.URL == "" {
				return errors.New("upstream.url is required")
			}
			if !strings.HasPrefix(upstream.URL, "http://") && !strings.HasPrefix(upstream.URL, "https://") {
				return errors.New("upstream.url must start with http:// or https://")
			}
			kind := strings.ToLower(strings.TrimSpace(upstream.Kind))
			if kind == "" || kind == "apk" {
				hasAPKUpstream = true
			}
		}
		if !hasAPKUpstream {
			return errors.New("at least one APK upstream is required when apk.enabled=true")
		}
	}

	if err := validateProxyAddress("proxy.upstream_proxy", cfg.Proxy.UpstreamProxy); err != nil {
		return err
	}

	return nil
}

func validateDuration(name, value string) error {
	if value == "" {
		return errors.New(name + " is required")
	}
	if _, err := time.ParseDuration(value); err != nil {
		return errors.New(name + " is invalid: " + err.Error())
	}
	return nil
}

func validateProxyAddress(name, value string) error {
	if value == "" {
		return nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return errors.New(name + " is invalid: " + err.Error())
	}
	switch parsed.Scheme {
	case "http", "https", "socks5":
	default:
		return errors.New(name + " must start with socks5://, http://, or https://")
	}
	if parsed.Host == "" {
		return errors.New(name + " must include host:port")
	}
	return nil
}
