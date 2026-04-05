package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
