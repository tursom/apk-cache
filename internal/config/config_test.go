package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := Default()
	if err := Validate(cfg); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestValidateRejectsInvalidConfig(t *testing.T) {
	testCases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "invalid listen",
			mutate: func(cfg *Config) {
				cfg.Server.Listen = "3142"
			},
			wantErr: "server.listen",
		},
		{
			name: "empty cache root",
			mutate: func(cfg *Config) {
				cfg.Cache.Root = ""
			},
			wantErr: "cache.root",
		},
		{
			name: "empty cache data root",
			mutate: func(cfg *Config) {
				cfg.Cache.DataRoot = ""
			},
			wantErr: "cache.data_root",
		},
		{
			name: "cache root path traversal",
			mutate: func(cfg *Config) {
				cfg.Cache.Root = "../cache"
			},
			wantErr: "must not contain '..'",
		},
		{
			name: "invalid index ttl",
			mutate: func(cfg *Config) {
				cfg.Cache.IndexTTL = "tomorrow"
			},
			wantErr: "cache.index_ttl",
		},
		{
			name: "invalid memory ttl",
			mutate: func(cfg *Config) {
				cfg.Cache.Memory.TTL = "later"
			},
			wantErr: "cache.memory.ttl",
		},
		{
			name: "invalid transport timeout",
			mutate: func(cfg *Config) {
				cfg.Transport.Timeout = "soon"
			},
			wantErr: "transport.timeout",
		},
		{
			name: "missing upstream url",
			mutate: func(cfg *Config) {
				cfg.Upstreams = []UpstreamConfig{{Kind: "apk"}}
			},
			wantErr: "upstream.url is required",
		},
		{
			name: "invalid upstream scheme",
			mutate: func(cfg *Config) {
				cfg.Upstreams = []UpstreamConfig{{URL: "ftp://mirror.example.com/alpine", Kind: "apk"}}
			},
			wantErr: "upstream.url must start with http:// or https://",
		},
		{
			name: "apk enabled without apk upstream",
			mutate: func(cfg *Config) {
				cfg.Upstreams = []UpstreamConfig{{URL: "https://mirror.example.com/debian", Kind: "apt"}}
			},
			wantErr: "at least one APK upstream",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(cfg)

			err := Validate(cfg)
			if err == nil {
				t.Fatalf("expected validation to fail")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateAllowsMixedKindsWhenAPKUpstreamExists(t *testing.T) {
	testCases := []struct {
		name      string
		upstreams []UpstreamConfig
	}{
		{
			name: "explicit apk upstream",
			upstreams: []UpstreamConfig{
				{URL: "https://mirror.example.com/debian", Kind: "apt"},
				{URL: "https://mirror.example.com/alpine", Kind: "apk"},
			},
		},
		{
			name: "empty kind counts as apk",
			upstreams: []UpstreamConfig{
				{URL: "https://mirror.example.com/debian", Kind: "apt"},
				{URL: "https://mirror.example.com/alpine"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Upstreams = tc.upstreams

			if err := Validate(cfg); err != nil {
				t.Fatalf("validation should succeed: %v", err)
			}
		})
	}
}

func TestValidateAllowsMirrorBasePath(t *testing.T) {
	cfg := Default()
	cfg.Upstreams = []UpstreamConfig{
		{URL: "https://mirrors.tuna.tsinghua.edu.cn/alpine", Kind: "apk"},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("mirror base path should be valid: %v", err)
	}
}

func TestLoadValidatesConfigFile(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	content := `
[server]
listen = ":3142"

[[upstreams]]
name = "Mirror"
url = "https://mirrors.tuna.tsinghua.edu.cn/alpine"
kind = "apk"

[cache]
root = "./cache"
data_root = "./data"
index_ttl = "24h"
package_ttl = "720h"

[cache.memory]
enabled = true
max_size = "256MB"
max_item_size = "16MB"
ttl = "30m"
max_items = 2048

[transport]
timeout = "30s"
idle_conn_timeout = "90s"
max_idle_conns = 128

[apk]
enabled = true

[apt]
enabled = true
verify_hash = true
load_index_async = true

[proxy]
enabled = true
allow_connect = true
cache_non_package_requests = false
require_auth = false
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Upstreams[0].URL; got != "https://mirrors.tuna.tsinghua.edu.cn/alpine" {
		t.Fatalf("upstream url = %q", got)
	}
}
