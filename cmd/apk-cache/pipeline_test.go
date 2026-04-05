package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	internalconfig "github.com/tursom/apk-cache/internal/config"
)

func TestAPTAdapterCacheKeyIncludesHost(t *testing.T) {
	adapter := NewAPTAdapter(internalconfig.APTConfig{Enabled: true, VerifyHash: true})
	req := httptest.NewRequest(http.MethodGet, "http://mirror.example.com/debian/pool/main/h/hello.deb", nil)

	normalized, err := adapter.Normalize(req)
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}

	cacheKey, err := adapter.CacheKey(normalized)
	if err != nil {
		t.Fatalf("cache key: %v", err)
	}

	expected := filepath.Join("apt", "mirror.example.com", "debian/pool/main/h/hello.deb")
	if cacheKey != expected {
		t.Fatalf("unexpected cache key: got %q want %q", cacheKey, expected)
	}
}

func TestPipelineCachesAPKResponses(t *testing.T) {
	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = w.Write([]byte("apk-package"))
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.Proxy.Enabled = true
	})

	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test.apk", nil)
	first := httptest.NewRecorder()
	app.pipeline.ServeHTTP(first, request)

	if first.Code != http.StatusOK {
		t.Fatalf("first response status = %d", first.Code)
	}
	if got := first.Body.String(); got != "apk-package" {
		t.Fatalf("first response body = %q", got)
	}
	if first.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("first response X-Cache = %q", first.Header().Get("X-Cache"))
	}

	second := httptest.NewRecorder()
	app.pipeline.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test.apk", nil))
	if second.Code != http.StatusOK {
		t.Fatalf("second response status = %d", second.Code)
	}
	if got := second.Header().Get("X-Cache"); got != "HIT" && got != "MEMORY-HIT" {
		t.Fatalf("second response X-Cache = %q", got)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("unexpected upstream hits: got %d want 1", upstreamHits.Load())
	}
}

func TestPipelineCachesAPTResponses(t *testing.T) {
	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = w.Write([]byte("apt-index"))
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.Proxy.Enabled = true
	})

	requestURL := upstreamServer.URL + "/debian/dists/stable/InRelease"
	first := httptest.NewRecorder()
	app.pipeline.ServeHTTP(first, httptest.NewRequest(http.MethodGet, requestURL, nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first response status = %d", first.Code)
	}
	if first.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("first response X-Cache = %q", first.Header().Get("X-Cache"))
	}

	second := httptest.NewRecorder()
	app.pipeline.ServeHTTP(second, httptest.NewRequest(http.MethodGet, requestURL, nil))
	if second.Code != http.StatusOK {
		t.Fatalf("second response status = %d", second.Code)
	}
	if got := second.Header().Get("X-Cache"); got != "HIT" && got != "MEMORY-HIT" {
		t.Fatalf("second response X-Cache = %q", got)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("unexpected upstream hits: got %d want 1", upstreamHits.Load())
	}
}

func TestProxyDisabledReturnsForbidden(t *testing.T) {
	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.APT.Enabled = false
		cfg.Proxy.Enabled = false
	})

	recorder := httptest.NewRecorder()
	app.pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://example.com/plain.txt", nil))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestConnectDisabledReturnsMethodNotAllowed(t *testing.T) {
	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.APT.Enabled = false
		cfg.Proxy.Enabled = true
		cfg.Proxy.AllowConnect = false
	})

	request := httptest.NewRequest(http.MethodConnect, "http://example.com", nil)
	request.Host = "example.com:443"
	recorder := httptest.NewRecorder()
	app.pipeline.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPTIndexServiceRejectsInvalidByHashContent(t *testing.T) {
	root := t.TempDir()
	cachePath := filepath.Join(root, "apt", "example.com", "debian", "dists", "stable", "by-hash", "SHA256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("wrong"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	service := NewAPTIndexService(root)
	if err := service.ValidateByHash(cachePath, "/debian/dists/stable/by-hash/SHA256/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		t.Fatalf("expected by-hash validation to fail")
	}
}

func mustNewTestApp(t *testing.T, mutate func(*internalconfig.Config)) *App {
	t.Helper()

	root := t.TempDir()
	cfg := internalconfig.Default()
	cfg.Cache.Root = filepath.Join(root, "cache")
	cfg.Cache.DataRoot = filepath.Join(root, "data")
	cfg.Cache.Memory.MaxSize = "64MB"
	cfg.Cache.Memory.MaxItemSize = "4MB"
	cfg.Cache.Memory.TTL = "1h"
	cfg.Cache.Memory.MaxItems = 32
	if mutate != nil {
		mutate(cfg)
	}

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return app
}
