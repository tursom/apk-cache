package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server    ServerConfig     `toml:"server"`
	Database  DatabaseConfig   `toml:"database"`
	Admin     AdminConfig      `toml:"admin"`
	HashStore HashStoreConfig  `toml:"hash_store"`
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

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type AdminConfig struct {
	BootstrapToken string `toml:"bootstrap_token"`
	SessionSecret  string `toml:"session_secret"`
}

type HashStoreConfig struct {
	Path                     string `toml:"path"`
	RebuildOnCorruption      bool   `toml:"rebuild_on_corruption"`
	TrustFileStat            bool   `toml:"trust_file_stat"`
	ActualRevalidateInterval string `toml:"actual_revalidate_interval"`
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
	Enabled         bool   `toml:"enabled"`
	VerifyHash      bool   `toml:"verify_hash"`
	VerifySignature bool   `toml:"verify_signature"`
	KeysDir         string `toml:"keys_dir"`
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
	UpstreamProxy   string   `toml:"upstream_proxy"`
	AllowedHosts    []string `toml:"allowed_hosts"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Listen: ":3142",
		},
		HashStore: HashStoreConfig{
			TrustFileStat:            true,
			ActualRevalidateInterval: "24h",
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
		},
		APT: APTConfig{
			Enabled:        true,
			VerifyHash:     true,
			LoadIndexAsync: true,
		},
		Proxy: ProxyConfig{
			Enabled:      true,
			AllowConnect: true,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, err
		}
	}
	ApplyEnvOverrides(cfg)
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func ApplyEnvOverrides(cfg *Config) {
	if v, ok := env("LISTEN", "ADDR"); ok {
		cfg.Server.Listen = v
	}
	if v, ok := env("DATABASE_PATH"); ok {
		cfg.Database.Path = v
	}
	if v, ok := env("ADMIN_BOOTSTRAP_TOKEN"); ok {
		cfg.Admin.BootstrapToken = v
	}
	if v, ok := env("ADMIN_SESSION_SECRET"); ok {
		cfg.Admin.SessionSecret = v
	}
	if v, ok := env("HASH_STORE_PATH"); ok {
		cfg.HashStore.Path = v
	}
	if v, ok := env("HASH_STORE_REBUILD_ON_CORRUPTION"); ok {
		cfg.HashStore.RebuildOnCorruption = parseBool(v)
	}
	if v, ok := env("HASH_STORE_TRUST_FILE_STAT"); ok {
		cfg.HashStore.TrustFileStat = parseBool(v)
	}
	if v, ok := env("HASH_STORE_ACTUAL_REVALIDATE_INTERVAL"); ok {
		cfg.HashStore.ActualRevalidateInterval = v
	}
	if v, ok := env("CACHE_ROOT", "CACHE_DIR"); ok {
		cfg.Cache.Root = v
	}
	if v, ok := env("DATA_ROOT", "CACHE_DATA_DIR"); ok {
		cfg.Cache.DataRoot = v
	}
	if v, ok := env("INDEX_TTL", "INDEX_CACHE"); ok {
		cfg.Cache.IndexTTL = v
	}
	if v, ok := env("PACKAGE_TTL", "PKG_CACHE"); ok {
		cfg.Cache.PackageTTL = v
	}
	if v, ok := env("MEMORY_CACHE_ENABLED"); ok {
		cfg.Cache.Memory.Enabled = parseBool(v)
	}
	if v, ok := env("MEMORY_CACHE_SIZE"); ok {
		cfg.Cache.Memory.MaxSize = v
	}
	if v, ok := env("MEMORY_CACHE_MAX_ITEM_SIZE"); ok {
		cfg.Cache.Memory.MaxItemSize = v
	}
	if v, ok := env("MEMORY_CACHE_TTL"); ok {
		cfg.Cache.Memory.TTL = v
	}
	if v, ok := env("MEMORY_CACHE_MAX_ITEMS"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Cache.Memory.MaxItems = n
		}
	}
	if v, ok := env("TRANSPORT_TIMEOUT"); ok {
		cfg.Transport.Timeout = v
	}
	if v, ok := env("TRANSPORT_IDLE_CONN_TIMEOUT"); ok {
		cfg.Transport.IdleConnTimeout = v
	}
	if v, ok := env("TRANSPORT_MAX_IDLE_CONNS"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Transport.MaxIdleConns = n
		}
	}
	if v, ok := env("APK_ENABLED"); ok {
		cfg.APK.Enabled = parseBool(v)
	}
	if v, ok := env("APK_VERIFY_HASH"); ok {
		cfg.APK.VerifyHash = parseBool(v)
	}
	if v, ok := env("APK_VERIFY_SIGNATURE"); ok {
		cfg.APK.VerifySignature = parseBool(v)
	}
	if v, ok := env("APT_ENABLED"); ok {
		cfg.APT.Enabled = parseBool(v)
	}
	if v, ok := env("APT_VERIFY_HASH"); ok {
		cfg.APT.VerifyHash = parseBool(v)
	}
	if v, ok := env("APT_LOAD_INDEX_ASYNC"); ok {
		cfg.APT.LoadIndexAsync = parseBool(v)
	}
	if v, ok := env("PROXY_ENABLED"); ok {
		cfg.Proxy.Enabled = parseBool(v)
	}
	if v, ok := env("PROXY_ALLOW_CONNECT"); ok {
		cfg.Proxy.AllowConnect = parseBool(v)
	}
	if v, ok := env("PROXY_CACHE_NON_PACKAGE_REQUESTS"); ok {
		cfg.Proxy.CacheNonPackage = parseBool(v)
	}
	if v, ok := env("UPSTREAM_PROXY"); ok {
		cfg.Proxy.UpstreamProxy = v
	}
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
	for name, value := range map[string]string{
		"cache.index_ttl":                       cfg.Cache.IndexTTL,
		"cache.package_ttl":                     cfg.Cache.PackageTTL,
		"cache.memory.ttl":                      cfg.Cache.Memory.TTL,
		"hash_store.actual_revalidate_interval": cfg.HashStore.ActualRevalidateInterval,
		"transport.timeout":                     cfg.Transport.Timeout,
		"transport.idle_conn_time":              cfg.Transport.IdleConnTimeout,
	} {
		if err := validateDuration(name, value); err != nil {
			return err
		}
	}
	if cfg.APK.Enabled {
		hasAPKUpstream := false
		for _, candidate := range cfg.Upstreams {
			kind := strings.ToLower(strings.TrimSpace(candidate.Kind))
			if kind != "" && kind != "apk" {
				continue
			}
			hasAPKUpstream = true
			if err := validateHTTPURL("upstream.url", candidate.URL); err != nil {
				return err
			}
			if err := validateProxyURL("upstream.proxy", candidate.Proxy); err != nil {
				return err
			}
		}
		if !hasAPKUpstream {
			return errors.New("at least one APK upstream is required when apk.enabled=true")
		}
	}
	if err := validateProxyURL("proxy.upstream_proxy", cfg.Proxy.UpstreamProxy); err != nil {
		return err
	}
	return nil
}

func env(keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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

func validateHTTPURL(name, value string) error {
	if value == "" {
		return errors.New(name + " is required")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return errors.New(name + " is invalid: " + err.Error())
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New(name + " must start with http:// or https://")
	}
	if parsed.Host == "" {
		return errors.New(name + " must include host")
	}
	return nil
}

func validateProxyURL(name, value string) error {
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
