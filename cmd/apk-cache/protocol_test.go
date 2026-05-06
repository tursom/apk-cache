package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	internalconfig "github.com/tursom/apk-cache/internal/config"
	"github.com/tursom/apk-cache/utils"
)

func TestAPKAdapterMatchesAPKIndexRequests(t *testing.T) {
	adapter := NewAPKAdapter(true)
	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)

	if !adapter.Match(request) {
		t.Fatalf("expected APK adapter to match APKINDEX request")
	}
}

func TestAPKAdapterDoesNotMatchAPKIndexWhenDisabledOrConnect(t *testing.T) {
	disabled := NewAPKAdapter(false)
	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)
	if disabled.Match(request) {
		t.Fatalf("disabled APK adapter should not match")
	}

	enabled := NewAPKAdapter(true)
	connectReq := httptest.NewRequest(http.MethodConnect, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)
	if enabled.Match(connectReq) {
		t.Fatalf("APK adapter should not match CONNECT requests")
	}
}

func TestAPKAdapterNormalizesAPKIndexRequests(t *testing.T) {
	adapter := NewAPKAdapter(true)
	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)

	normalized, err := adapter.Normalize(request)
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}
	if normalized.AdapterName != "apk" {
		t.Fatalf("adapter name = %q", normalized.AdapterName)
	}
	if normalized.PackageType != utils.PackageTypeAPK {
		t.Fatalf("package type = %v", normalized.PackageType)
	}
	if normalized.CacheClass != "index" {
		t.Fatalf("cache class = %q want %q", normalized.CacheClass, "index")
	}
	if normalized.UpstreamPath != "/alpine/v3.20/main/x86_64/APKINDEX.tar.gz" {
		t.Fatalf("upstream path = %q", normalized.UpstreamPath)
	}
	if !normalized.Cacheable {
		t.Fatalf("normalized request should be cacheable")
	}
}

func TestAPKAdapterCachePolicyForAPKIndexRequests(t *testing.T) {
	adapter := NewAPKAdapter(true)
	normalized, err := adapter.Normalize(httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil))
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}

	decision := adapter.CachePolicy(normalized)
	if !decision.Enabled {
		t.Fatalf("cache policy should be enabled")
	}
	if decision.StoreInMemory {
		t.Fatalf("APK index should not be stored in memory")
	}
}

func TestAPKAdapterCacheKeyForAPKIndexRequests(t *testing.T) {
	adapter := NewAPKAdapter(true)
	normalized, err := adapter.Normalize(httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil))
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}

	cacheKey, err := adapter.CacheKey(normalized)
	if err != nil {
		t.Fatalf("cache key: %v", err)
	}
	if cacheKey != filepath.Join("alpine", "v3.20", "main", "x86_64", "APKINDEX.tar.gz") {
		t.Fatalf("cache key = %q", cacheKey)
	}
}

func TestAPKAdapterRejectsInvalidPath(t *testing.T) {
	adapter := NewAPKAdapter(true)
	request := &http.Request{URL: nil}

	if _, err := adapter.Normalize(request); err == nil {
		t.Fatalf("expected invalid path error")
	}
}

func TestAPKAdapterFetchPropagatesHeaders(t *testing.T) {
	var gotHeader string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.Cache.Memory.Enabled = false
	})

	adapter := NewAPKAdapter(true)
	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)
	request.Header.Set("X-Custom-Header", "test-value")
	normalized, err := adapter.Normalize(request)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	ctx := context.Background()
	resp, err := adapter.Fetch(ctx, app, normalized)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()

	if gotHeader != "test-value" {
		t.Fatalf("upstream X-Custom-Header = %q, want %q", gotHeader, "test-value")
	}
}

func TestAPKAdapterFetchPropagatesContext(t *testing.T) {
	handlerCh := make(chan struct{})
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-handlerCh
	}))
	defer func() {
		close(handlerCh) // unblock any remaining handler goroutines
		upstreamServer.Close()
	}()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.Cache.Memory.Enabled = false
	})

	adapter := NewAPKAdapter(true)
	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)
	normalized, err := adapter.Normalize(request)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = adapter.Fetch(ctx, app, normalized)
	if err == nil {
		t.Fatalf("expected error from expired context")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}
