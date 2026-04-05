package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestPipelineCachesAPKIndexResponses(t *testing.T) {
	var upstreamHits atomic.Int32
	indexBody := buildSignedArchive(t, "ignored", nil, "DESCRIPTION", []byte(""), true, false)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		if r.URL.Path != "/alpine/v3.20/main/x86_64/APKINDEX.tar.gz" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		_, _ = w.Write(indexBody)
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
	})

	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)
	cachePath := mustCachePathForRequest(t, app, request)

	first := httptest.NewRecorder()
	app.pipeline.ServeHTTP(first, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first response status = %d", first.Code)
	}
	if got := first.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("first response X-Cache = %q", got)
	}

	second := httptest.NewRecorder()
	app.pipeline.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil))
	if second.Code != http.StatusOK {
		t.Fatalf("second response status = %d", second.Code)
	}
	if got := second.Header().Get("X-Cache"); got != "HIT" {
		t.Fatalf("second response X-Cache = %q", got)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("unexpected upstream hits: got %d want 1", upstreamHits.Load())
	}
	if got, err := os.ReadFile(cachePath); err != nil {
		t.Fatalf("read cache file: %v", err)
	} else if string(got) != string(indexBody) {
		t.Fatalf("cache file body = %q", string(got))
	}
	if app.memoryCache != nil {
		if _, ok := app.memoryCache.Get(cachePath); ok {
			t.Fatalf("apk index should not be cached in memory")
		}
	}
}

func TestPipelineUsesIndexTTLForAPKIndexRequests(t *testing.T) {
	var upstreamHits atomic.Int32
	indexBody := buildSignedArchive(t, "ignored", nil, "DESCRIPTION", []byte(""), true, false)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = w.Write(indexBody)
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.Cache.Memory.Enabled = false
		cfg.Cache.IndexTTL = "1s"
		cfg.Cache.PackageTTL = "1h"
	})

	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)
	cachePath := mustCachePathForRequest(t, app, request)

	first := httptest.NewRecorder()
	app.pipeline.ServeHTTP(first, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first response status = %d", first.Code)
	}

	expiredTime := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(cachePath, expiredTime, expiredTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	second := httptest.NewRecorder()
	app.pipeline.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil))
	if second.Code != http.StatusOK {
		t.Fatalf("second response status = %d", second.Code)
	}
	if got := second.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("second response X-Cache = %q", got)
	}
	if upstreamHits.Load() != 2 {
		t.Fatalf("unexpected upstream hits: got %d want 2", upstreamHits.Load())
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

func TestPipelineBypassesNonOKAPKResponses(t *testing.T) {
	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.Cache.Memory.Enabled = false
	})

	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test.apk", nil)
	cachePath := mustCachePathForRequest(t, app, request)

	first := httptest.NewRecorder()
	app.pipeline.ServeHTTP(first, request)
	if first.Code != http.StatusBadGateway {
		t.Fatalf("first response status = %d", first.Code)
	}
	if got := first.Header().Get("X-Cache"); got != "BYPASS" {
		t.Fatalf("first response X-Cache = %q", got)
	}
	if !strings.Contains(first.Body.String(), "boom") {
		t.Fatalf("first response body = %q", first.Body.String())
	}

	second := httptest.NewRecorder()
	app.pipeline.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test.apk", nil))
	if second.Code != http.StatusBadGateway {
		t.Fatalf("second response status = %d", second.Code)
	}
	if upstreamHits.Load() != 2 {
		t.Fatalf("unexpected upstream hits: got %d want 2", upstreamHits.Load())
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no cache file, stat err = %v", err)
	}
}

func TestPipelineReFetchesExpiredDiskCache(t *testing.T) {
	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = w.Write([]byte("apk-package"))
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.Cache.Memory.Enabled = false
	})

	request := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test.apk", nil)
	cachePath := mustCachePathForRequest(t, app, request)

	first := httptest.NewRecorder()
	app.pipeline.ServeHTTP(first, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first response status = %d", first.Code)
	}
	expiredTime := time.Now().Add(-2 * app.packageTTL)
	if err := os.Chtimes(cachePath, expiredTime, expiredTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	second := httptest.NewRecorder()
	app.pipeline.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test.apk", nil))
	if second.Code != http.StatusOK {
		t.Fatalf("second response status = %d", second.Code)
	}
	if got := second.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("second response X-Cache = %q", got)
	}
	if upstreamHits.Load() != 2 {
		t.Fatalf("unexpected upstream hits: got %d want 2", upstreamHits.Load())
	}
}

func TestPipelineReFetchesAfterCachedValidationFailure(t *testing.T) {
	var upstreamHits atomic.Int32
	body := []byte("valid-by-hash-content")
	hash := sha256.Sum256(body)
	hashHex := hex.EncodeToString(hash[:])

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = w.Write(body)
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.Cache.Memory.Enabled = false
	})

	requestURL := upstreamServer.URL + "/debian/dists/stable/by-hash/SHA256/" + hashHex
	request := httptest.NewRequest(http.MethodGet, requestURL, nil)
	cachePath := mustCachePathForRequest(t, app, request)

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("wrong"), 0o644); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}

	first := httptest.NewRecorder()
	app.pipeline.ServeHTTP(first, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first response status = %d", first.Code)
	}
	if got := first.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("first response X-Cache = %q", got)
	}
	if got := first.Body.Bytes(); string(got) != string(body) {
		t.Fatalf("first response body = %q", string(got))
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("unexpected upstream hits after refetch: got %d want 1", upstreamHits.Load())
	}
	if got, err := os.ReadFile(cachePath); err != nil {
		t.Fatalf("read cache file: %v", err)
	} else if string(got) != string(body) {
		t.Fatalf("cache file body = %q", string(got))
	}

	second := httptest.NewRecorder()
	app.pipeline.ServeHTTP(second, httptest.NewRequest(http.MethodGet, requestURL, nil))
	if second.Code != http.StatusOK {
		t.Fatalf("second response status = %d", second.Code)
	}
	if got := second.Header().Get("X-Cache"); got != "HIT" {
		t.Fatalf("second response X-Cache = %q", got)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("unexpected upstream hits after cache hit: got %d want 1", upstreamHits.Load())
	}
}

func TestPipelineReFetchesAPKAfterCachedHashValidationFailure(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keysDir := t.TempDir()
	writeTrustedKey(t, keysDir, "test.rsa.pub", privateKey)

	validPackage := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("valid package"), false, false)
	indexBody := buildSignedAPKIndex(t, "test.rsa.pub", privateKey, map[string][]byte{
		"test-1.apk": validPackage,
	})

	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		switch r.URL.Path {
		case "/alpine/v3.20/main/x86_64/APKINDEX.tar.gz":
			_, _ = w.Write(indexBody)
		case "/alpine/v3.20/main/x86_64/test-1.apk":
			_, _ = w.Write(validPackage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.APK.VerifyHash = true
		cfg.APK.VerifySignature = true
		cfg.APK.KeysDir = keysDir
		cfg.Cache.Memory.Enabled = false
	})

	indexReq := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)
	indexRec := httptest.NewRecorder()
	app.pipeline.ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index response status = %d", indexRec.Code)
	}

	packageReq := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test-1.apk", nil)
	cachePath := mustCachePathForRequest(t, app, packageReq)
	if err := os.WriteFile(cachePath, buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("stale"), false, false), 0o644); err != nil {
		t.Fatalf("write stale package: %v", err)
	}

	rec := httptest.NewRecorder()
	app.pipeline.ServeHTTP(rec, packageReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("package response status = %d", rec.Code)
	}
	if got := rec.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("X-Cache = %q", got)
	}
	if got := rec.Body.Bytes(); string(got) != string(validPackage) {
		t.Fatalf("package body mismatch")
	}
	if got, err := os.ReadFile(cachePath); err != nil {
		t.Fatalf("read package cache: %v", err)
	} else if string(got) != string(validPackage) {
		t.Fatalf("cached package mismatch")
	}
	if upstreamHits.Load() != 2 {
		t.Fatalf("unexpected upstream hits: got %d want 2", upstreamHits.Load())
	}
}

func TestPipelineRejectsFetchedAPKHashMismatch(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keysDir := t.TempDir()
	writeTrustedKey(t, keysDir, "test.rsa.pub", privateKey)

	validPackage := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("valid package"), false, false)
	indexBody := buildSignedAPKIndex(t, "test.rsa.pub", privateKey, map[string][]byte{
		"test-1.apk": validPackage,
	})
	invalidPackage := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("different package"), false, false)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alpine/v3.20/main/x86_64/APKINDEX.tar.gz":
			_, _ = w.Write(indexBody)
		case "/alpine/v3.20/main/x86_64/test-1.apk":
			_, _ = w.Write(invalidPackage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.APK.VerifyHash = true
		cfg.APK.VerifySignature = true
		cfg.APK.KeysDir = keysDir
		cfg.Cache.Memory.Enabled = false
	})

	indexReq := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil)
	app.pipeline.ServeHTTP(httptest.NewRecorder(), indexReq)

	packageReq := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test-1.apk", nil)
	cachePath := mustCachePathForRequest(t, app, packageReq)
	rec := httptest.NewRecorder()
	app.pipeline.ServeHTTP(rec, packageReq)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d want %d", rec.Code, http.StatusBadGateway)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected invalid package to be absent from cache, stat err = %v", err)
	}
}

func TestPipelineBypassesUnsignedFetchedAPKWithoutCaching(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keysDir := t.TempDir()
	writeTrustedKey(t, keysDir, "test.rsa.pub", privateKey)

	unsignedPackage := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("valid package"), true, false)
	indexBody := buildSignedAPKIndex(t, "test.rsa.pub", privateKey, map[string][]byte{
		"test-1.apk": unsignedPackage,
	})

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alpine/v3.20/main/x86_64/APKINDEX.tar.gz":
			_, _ = w.Write(indexBody)
		case "/alpine/v3.20/main/x86_64/test-1.apk":
			_, _ = w.Write(unsignedPackage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.APK.VerifyHash = true
		cfg.APK.VerifySignature = true
		cfg.APK.KeysDir = keysDir
		cfg.Cache.Memory.Enabled = false
	})

	app.pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil))

	packageReq := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test-1.apk", nil)
	cachePath := mustCachePathForRequest(t, app, packageReq)
	rec := httptest.NewRecorder()
	app.pipeline.ServeHTTP(rec, packageReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Cache"); got != "BYPASS" {
		t.Fatalf("X-Cache = %q", got)
	}
	if got := rec.Body.Bytes(); string(got) != string(unsignedPackage) {
		t.Fatalf("unexpected bypass body")
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unsigned package to bypass cache, stat err = %v", err)
	}
}

func TestPipelineAllowsUnsignedAPKWhenSignatureVerificationDisabled(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	unsignedPackage := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("valid package"), true, false)
	indexBody := buildSignedAPKIndex(t, "test.rsa.pub", privateKey, map[string][]byte{
		"test-1.apk": unsignedPackage,
	})

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alpine/v3.20/main/x86_64/APKINDEX.tar.gz":
			_, _ = w.Write(indexBody)
		case "/alpine/v3.20/main/x86_64/test-1.apk":
			_, _ = w.Write(unsignedPackage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstreamServer.URL, Kind: "apk"}}
		cfg.APK.VerifyHash = true
		cfg.APK.VerifySignature = false
		cfg.Cache.Memory.Enabled = false
	})

	app.pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/APKINDEX.tar.gz", nil))

	packageReq := httptest.NewRequest(http.MethodGet, "http://cache.local/alpine/v3.20/main/x86_64/test-1.apk", nil)
	cachePath := mustCachePathForRequest(t, app, packageReq)
	rec := httptest.NewRecorder()
	app.pipeline.ServeHTTP(rec, packageReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("X-Cache = %q", got)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected unsigned package to be cached when signature verification is disabled: %v", err)
	}
}

func TestProxyAdapterForwardsAbsoluteURLRequests(t *testing.T) {
	var gotPath string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("plain"))
	}))
	defer upstreamServer.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.APT.Enabled = false
		cfg.Proxy.Enabled = true
	})

	requestURL := upstreamServer.URL + "/plain.txt"
	recorder := httptest.NewRecorder()
	app.pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestURL, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != "plain" {
		t.Fatalf("body = %q", got)
	}
	if got := recorder.Header().Get("X-Cache"); got != "BYPASS" {
		t.Fatalf("X-Cache = %q", got)
	}
	if gotPath != "/plain.txt" {
		t.Fatalf("upstream path = %q", gotPath)
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
	cfg.APK.VerifyHash = false
	cfg.APK.VerifySignature = false
	if mutate != nil {
		mutate(cfg)
	}

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return app
}

func mustCachePathForRequest(t *testing.T, app *App, request *http.Request) string {
	t.Helper()

	adapter := app.pipeline.matchAdapter(request)
	if adapter == nil {
		t.Fatalf("no adapter matched request %s", request.URL)
	}
	normalized, err := adapter.Normalize(request)
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}
	cacheKey, err := adapter.CacheKey(normalized)
	if err != nil {
		t.Fatalf("cache key: %v", err)
	}
	return filepath.Join(app.cfg.Cache.Root, cacheKey)
}
