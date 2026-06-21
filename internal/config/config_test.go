package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	if err := Validate(Default()); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestLoadConfigAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[server]
listen = ":8080"

[[upstreams]]
url = "https://example.invalid/alpine"
kind = "apk"

[cache]
root = "` + filepath.ToSlash(filepath.Join(dir, "cache")) + `"
data_root = "` + filepath.ToSlash(filepath.Join(dir, "data")) + `"
index_ttl = "1h"
package_ttl = "2h"

[cache.memory]
enabled = true
max_size = "1MB"
max_item_size = "64KB"
ttl = "5m"
max_items = 8

[transport]
timeout = "3s"
idle_conn_timeout = "4s"
max_idle_conns = 16

[apk]
enabled = true
verify_hash = false
verify_signature = false

[apt]
enabled = true
verify_hash = true
load_index_async = false

[proxy]
enabled = true
allow_connect = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADDR", ":9090")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Listen != ":9090" {
		t.Fatalf("env override not applied: %s", cfg.Server.Listen)
	}
	if cfg.Cache.Memory.MaxItems != 8 {
		t.Fatalf("memory max items changed: %d", cfg.Cache.Memory.MaxItems)
	}
}

func TestValidateRejectsBadProxy(t *testing.T) {
	cfg := Default()
	cfg.Proxy.UpstreamProxy = "ftp://127.0.0.1:21"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected invalid proxy error")
	}
}

func TestApplyEnvOverridesFullSet(t *testing.T) {
	cfg := Default()
	t.Setenv("LISTEN", ":10000")
	t.Setenv("CACHE_ROOT", "/tmp/cache")
	t.Setenv("DATA_ROOT", "/tmp/data")
	t.Setenv("INDEX_TTL", "2h")
	t.Setenv("PACKAGE_TTL", "3h")
	t.Setenv("MEMORY_CACHE_ENABLED", "false")
	t.Setenv("MEMORY_CACHE_SIZE", "2MB")
	t.Setenv("MEMORY_CACHE_MAX_ITEM_SIZE", "3KB")
	t.Setenv("MEMORY_CACHE_TTL", "4m")
	t.Setenv("MEMORY_CACHE_MAX_ITEMS", "9")
	t.Setenv("TRANSPORT_TIMEOUT", "5s")
	t.Setenv("TRANSPORT_IDLE_CONN_TIMEOUT", "6s")
	t.Setenv("TRANSPORT_MAX_IDLE_CONNS", "7")
	t.Setenv("APK_ENABLED", "false")
	t.Setenv("APK_VERIFY_HASH", "false")
	t.Setenv("APK_VERIFY_SIGNATURE", "false")
	t.Setenv("APT_ENABLED", "false")
	t.Setenv("APT_VERIFY_HASH", "false")
	t.Setenv("APT_LOAD_INDEX_ASYNC", "false")
	t.Setenv("PROXY_ENABLED", "false")
	t.Setenv("PROXY_ALLOW_CONNECT", "false")
	t.Setenv("PROXY_CACHE_NON_PACKAGE_REQUESTS", "true")
	t.Setenv("UPSTREAM_PROXY", "http://127.0.0.1:8080")

	ApplyEnvOverrides(cfg)
	if cfg.Server.Listen != ":10000" || cfg.Cache.Root != "/tmp/cache" || cfg.Cache.DataRoot != "/tmp/data" {
		t.Fatalf("basic overrides failed: %#v", cfg)
	}
	if cfg.Cache.Memory.Enabled || cfg.Cache.Memory.MaxItems != 9 || cfg.Transport.MaxIdleConns != 7 {
		t.Fatalf("numeric/bool overrides failed: %#v", cfg.Cache.Memory)
	}
	if cfg.APK.Enabled || cfg.APT.Enabled || cfg.Proxy.Enabled || cfg.Proxy.AllowConnect {
		t.Fatal("boolean false overrides failed")
	}
	if !cfg.Proxy.CacheNonPackage || cfg.Proxy.UpstreamProxy != "http://127.0.0.1:8080" {
		t.Fatal("proxy overrides failed")
	}
}

func TestValidateRejectsInvalidConfigMatrix(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"nil", func(*Config) {}},
		{"listen", func(c *Config) { c.Server.Listen = "3142" }},
		{"cache root", func(c *Config) { c.Cache.Root = "" }},
		{"data root", func(c *Config) { c.Cache.DataRoot = "" }},
		{"cache traversal", func(c *Config) { c.Cache.Root = "../cache" }},
		{"bad index ttl", func(c *Config) { c.Cache.IndexTTL = "bad" }},
		{"bad package ttl", func(c *Config) { c.Cache.PackageTTL = "bad" }},
		{"bad memory ttl", func(c *Config) { c.Cache.Memory.TTL = "bad" }},
		{"bad transport timeout", func(c *Config) { c.Transport.Timeout = "bad" }},
		{"bad idle timeout", func(c *Config) { c.Transport.IdleConnTimeout = "bad" }},
		{"no apk upstream", func(c *Config) { c.Upstreams = []UpstreamConfig{{Kind: "apt", URL: "https://example.com"}} }},
		{"bad upstream url", func(c *Config) { c.Upstreams[0].URL = "ftp://example.com" }},
		{"bad upstream proxy", func(c *Config) { c.Upstreams[0].Proxy = "ftp://proxy" }},
		{"bad proxy url", func(c *Config) { c.Proxy.UpstreamProxy = "http://" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "nil" {
				if err := Validate(nil); err == nil {
					t.Fatal("expected nil config error")
				}
				return
			}
			cfg := Default()
			tc.edit(cfg)
			if err := Validate(cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	for _, value := range []string{"1", "true", "yes", "on", "TRUE"} {
		if !parseBool(value) {
			t.Fatalf("%q should parse true", value)
		}
	}
	for _, value := range []string{"", "0", "false", "off", "no"} {
		if parseBool(value) {
			t.Fatalf("%q should parse false", value)
		}
	}
}
