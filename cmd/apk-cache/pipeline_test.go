package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "github.com/tursom/apk-cache/internal/config"
)

type failAfterWriter struct {
	builder   strings.Builder
	maxWrites int
	writes    int
}

type chunkedReader struct {
	data      []byte
	chunkSize int
	offset    int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := r.chunkSize
	if n > len(r.data)-r.offset {
		n = len(r.data) - r.offset
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p[:n], r.data[r.offset:r.offset+n])
	r.offset += n
	return n, nil
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.writes >= w.maxWrites {
		return 0, fmt.Errorf("forced write failure")
	}
	w.writes++
	return w.builder.Write(p)
}

func TestStreamResponseToSinksContinuesCachingAfterClientWriteFailure(t *testing.T) {
	client := &failAfterWriter{maxWrites: 1}
	var cache strings.Builder

	result, err := streamResponseToSinks(&chunkedReader{data: []byte("abcdef"), chunkSize: 3}, client, &cache, nil)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("stream err = %v want EOF", err)
	}
	if !result.ClientFailed {
		t.Fatalf("expected client write failure")
	}
	if result.CacheFailed {
		t.Fatalf("did not expect cache write failure")
	}
	if cache.String() != "abcdef" {
		t.Fatalf("cache body = %q", cache.String())
	}
	if client.builder.String() != "abc" {
		t.Fatalf("client body = %q", client.builder.String())
	}
}

func TestStreamResponseToSinksContinuesClientWriteAfterCacheFailure(t *testing.T) {
	var client strings.Builder
	cache := &failAfterWriter{maxWrites: 1}

	result, err := streamResponseToSinks(&chunkedReader{data: []byte("abcdef"), chunkSize: 3}, &client, cache, nil)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("stream err = %v want EOF", err)
	}
	if result.ClientFailed {
		t.Fatalf("did not expect client write failure")
	}
	if !result.CacheFailed {
		t.Fatalf("expected cache write failure")
	}
	if client.String() != "abcdef" {
		t.Fatalf("client body = %q", client.String())
	}
	if cache.builder.String() != "abc" {
		t.Fatalf("cache prefix = %q", cache.builder.String())
	}
}

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

func TestPipelineStreamsFetchedAPKHashMismatchWithoutCaching(t *testing.T) {
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
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("X-Cache = %q", got)
	}
	if got := rec.Body.Bytes(); string(got) != string(invalidPackage) {
		t.Fatalf("unexpected streamed body")
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
	if got := rec.Header().Get("X-Cache"); got != "MISS" {
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

func TestProxyAdapterUsesHTTPUpstreamProxy(t *testing.T) {
	var targetPath string
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetPath = r.URL.Path
		_, _ = w.Write([]byte("via-http-proxy"))
	}))
	defer targetServer.Close()

	var proxiedURL string
	httpProxy := newHTTPTestProxy(t, func(method, target string) {
		if method == http.MethodConnect {
			return
		}
		proxiedURL = target
	})
	defer httpProxy.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.APT.Enabled = false
		cfg.Proxy.Enabled = true
		cfg.Proxy.UpstreamProxy = httpProxy.URL
	})

	requestURL := targetServer.URL + "/plain.txt"
	recorder := httptest.NewRecorder()
	app.pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestURL, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != "via-http-proxy" {
		t.Fatalf("body = %q", got)
	}
	if proxiedURL != requestURL {
		t.Fatalf("proxied URL = %q want %q", proxiedURL, requestURL)
	}
	if targetPath != "/plain.txt" {
		t.Fatalf("target path = %q want %q", targetPath, "/plain.txt")
	}
}

func TestProxyAdapterUsesSOCKS5UpstreamProxy(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("via-socks5-proxy"))
	}))
	defer targetServer.Close()

	socksProxy := newSOCKS5TestProxy(t)
	defer socksProxy.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.APT.Enabled = false
		cfg.Proxy.Enabled = true
		cfg.Proxy.UpstreamProxy = "socks5://" + socksProxy.Addr()
	})

	requestURL := targetServer.URL + "/plain.txt"
	recorder := httptest.NewRecorder()
	app.pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestURL, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != "via-socks5-proxy" {
		t.Fatalf("body = %q", got)
	}

	targetURL, err := url.Parse(targetServer.URL)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	if got := socksProxy.WaitForRequest(t); got != targetURL.Host {
		t.Fatalf("socks target = %q want %q", got, targetURL.Host)
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

func TestProxyAdapterConnectUsesHTTPUpstreamProxy(t *testing.T) {
	target := newTCPEchoServer(t)
	defer target.Close()

	var connectTarget string
	httpProxy := newHTTPTestProxy(t, func(method, target string) {
		if method == http.MethodConnect {
			connectTarget = target
		}
	})
	defer httpProxy.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.APT.Enabled = false
		cfg.Proxy.Enabled = true
		cfg.Proxy.AllowConnect = true
		cfg.Proxy.UpstreamProxy = httpProxy.URL
	})

	status, echoed := performConnectRoundTrip(t, app.pipeline, target.Addr(), "ping-over-http-proxy")
	if status != http.StatusOK {
		t.Fatalf("status = %d want %d", status, http.StatusOK)
	}
	if echoed != "ping-over-http-proxy" {
		t.Fatalf("echoed = %q", echoed)
	}
	if connectTarget != target.Addr() {
		t.Fatalf("proxy CONNECT target = %q want %q", connectTarget, target.Addr())
	}
}

func TestProxyAdapterConnectUsesSOCKS5UpstreamProxy(t *testing.T) {
	target := newTCPEchoServer(t)
	defer target.Close()

	socksProxy := newSOCKS5TestProxy(t)
	defer socksProxy.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.APK.Enabled = false
		cfg.APT.Enabled = false
		cfg.Proxy.Enabled = true
		cfg.Proxy.AllowConnect = true
		cfg.Proxy.UpstreamProxy = "socks5://" + socksProxy.Addr()
	})

	status, echoed := performConnectRoundTrip(t, app.pipeline, target.Addr(), "ping-over-socks5-proxy")
	if status != http.StatusOK {
		t.Fatalf("status = %d want %d", status, http.StatusOK)
	}
	if echoed != "ping-over-socks5-proxy" {
		t.Fatalf("echoed = %q", echoed)
	}
	if got := socksProxy.WaitForRequest(t); got != target.Addr() {
		t.Fatalf("socks CONNECT target = %q want %q", got, target.Addr())
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

func performConnectRoundTrip(t *testing.T, handler http.Handler, targetAddr, payload string) (int, string) {
	t.Helper()

	server := httptest.NewServer(handler)
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	conn, err := net.Dial("tcp", serverURL.Host)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr); err != nil {
		t.Fatalf("write CONNECT request: %v", err)
	}

	reader := bufio.NewReader(conn)
	request, err := http.NewRequest(http.MethodConnect, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return response.StatusCode, ""
	}

	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}

	echoed := make([]byte, len(payload))
	if _, err := io.ReadFull(reader, echoed); err != nil {
		t.Fatalf("read tunnel payload: %v", err)
	}
	return response.StatusCode, string(echoed)
}

type tcpEchoServer struct {
	listener net.Listener
}

func newTCPEchoServer(t *testing.T) *tcpEchoServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo server: %v", err)
	}

	server := &tcpEchoServer{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buffer := make([]byte, 4096)
				for {
					n, err := conn.Read(buffer)
					if n > 0 {
						if _, writeErr := conn.Write(buffer[:n]); writeErr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return server
}

func (s *tcpEchoServer) Addr() string {
	return s.listener.Addr().String()
}

func (s *tcpEchoServer) Close() {
	_ = s.listener.Close()
}

type socks5TestProxy struct {
	listener net.Listener
	requests chan string
}

func newSOCKS5TestProxy(t *testing.T) *socks5TestProxy {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen socks5 proxy: %v", err)
	}

	proxy := &socks5TestProxy{
		listener: listener,
		requests: make(chan string, 8),
	}
	go proxy.serve(t)
	return proxy
}

func (p *socks5TestProxy) Addr() string {
	return p.listener.Addr().String()
}

func (p *socks5TestProxy) Close() {
	_ = p.listener.Close()
}

func (p *socks5TestProxy) WaitForRequest(t *testing.T) string {
	t.Helper()

	select {
	case target := <-p.requests:
		return target
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for socks5 request")
		return ""
	}
}

func (p *socks5TestProxy) serve(t *testing.T) {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		go p.handleConn(t, conn)
	}
}

func (p *socks5TestProxy) handleConn(t *testing.T, conn net.Conn) {
	t.Helper()
	defer conn.Close()

	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	requestHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, requestHeader); err != nil {
		return
	}
	if requestHeader[0] != 0x05 || requestHeader[1] != 0x01 {
		_, _ = conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	targetAddr, err := readSOCKS5Address(conn, requestHeader[3])
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	p.requests <- targetAddr

	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer targetConn.Close()

	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	go tunnelCopy(targetConn, conn)
	tunnelCopy(conn, targetConn)
}

func readSOCKS5Address(conn net.Conn, atyp byte) (string, error) {
	switch atyp {
	case 0x01:
		host := make([]byte, 4)
		if _, err := io.ReadFull(conn, host); err != nil {
			return "", err
		}
		port := make([]byte, 2)
		if _, err := io.ReadFull(conn, port); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s:%d", net.IP(host).String(), int(port[0])<<8|int(port[1])), nil
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", err
		}
		host := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, host); err != nil {
			return "", err
		}
		port := make([]byte, 2)
		if _, err := io.ReadFull(conn, port); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s:%d", string(host), int(port[0])<<8|int(port[1])), nil
	case 0x04:
		host := make([]byte, 16)
		if _, err := io.ReadFull(conn, host); err != nil {
			return "", err
		}
		port := make([]byte, 2)
		if _, err := io.ReadFull(conn, port); err != nil {
			return "", err
		}
		return fmt.Sprintf("[%s]:%d", net.IP(host).String(), int(port[0])<<8|int(port[1])), nil
	default:
		return "", fmt.Errorf("unsupported atyp %d", atyp)
	}
}

func newHTTPTestProxy(t *testing.T, record func(method, target string)) *httptest.Server {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.String()
		if r.Method == http.MethodConnect {
			target = r.Host
		}
		record(r.Method, target)
		if r.Method == http.MethodConnect {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Fatalf("proxy response writer does not support hijacking")
			}
			clientConn, _, err := hijacker.Hijack()
			if err != nil {
				t.Fatalf("proxy hijack: %v", err)
			}

			targetConn, err := net.Dial("tcp", r.Host)
			if err != nil {
				_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
				_ = clientConn.Close()
				return
			}

			if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
				_ = clientConn.Close()
				_ = targetConn.Close()
				return
			}

			go tunnelCopy(targetConn, clientConn)
			go tunnelCopy(clientConn, targetConn)
			return
		}

		upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
		if err != nil {
			t.Fatalf("proxy new request: %v", err)
		}
		upstreamReq.Header = r.Header.Clone()
		response, err := http.DefaultTransport.RoundTrip(upstreamReq)
		if err != nil {
			t.Fatalf("proxy round trip: %v", err)
		}
		defer response.Body.Close()

		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		if _, err := io.Copy(w, response.Body); err != nil {
			t.Fatalf("proxy copy body: %v", err)
		}
	})

	return httptest.NewServer(handler)
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
