package app

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tursom/apk-cache/internal/config"
	"github.com/tursom/apk-cache/internal/hashstore"
)

func testConfig(t *testing.T, upstreamURL string) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Cache.Root = filepath.Join(root, "cache")
	cfg.Cache.DataRoot = filepath.Join(root, "data")
	cfg.Cache.Memory.MaxSize = "1MB"
	cfg.Cache.Memory.MaxItemSize = "1MB"
	cfg.APK.VerifyHash = false
	cfg.APK.VerifySignature = false
	cfg.Upstreams = []config.UpstreamConfig{{Name: "test", URL: upstreamURL, Kind: "apk"}}
	return cfg
}

func TestAPKCacheMissThenMemoryHit(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("apk-body"))
	}))
	defer up.Close()

	a, err := New(testConfig(t, up.URL))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/alpine/v3.23/main/x86_64/hello-1.apk", nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get(HeaderCache) != CacheMiss {
		t.Fatalf("first response code=%d cache=%s body=%q", rec.Code, rec.Header().Get(HeaderCache), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get(HeaderCache) != CacheMemoryHit {
		t.Fatalf("second response code=%d cache=%s body=%q", rec.Code, rec.Header().Get(HeaderCache), rec.Body.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("upstream hits=%d", hits.Load())
	}
}

func TestAPKDiskHitWhenMemoryDisabled(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("apk-body"))
	}))
	defer up.Close()
	cfg := testConfig(t, up.URL)
	cfg.Cache.Memory.Enabled = false
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/alpine/v3.23/main/x86_64/hello-1.apk", nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Header().Get(HeaderCache) != CacheMiss {
		t.Fatalf("first cache=%s", rec.Header().Get(HeaderCache))
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Header().Get(HeaderCache) != CacheHit {
		t.Fatalf("second cache=%s", rec.Header().Get(HeaderCache))
	}
	if hits.Load() != 1 {
		t.Fatalf("upstream hits=%d", hits.Load())
	}
}

func TestAPTCacheIsHostScoped(t *testing.T) {
	var hitsA, hitsB atomic.Int32
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		_, _ = w.Write([]byte("a"))
	}))
	defer upA.Close()
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		_, _ = w.Write([]byte("b"))
	}))
	defer upB.Close()

	a, err := New(testConfig(t, upA.URL))
	if err != nil {
		t.Fatal(err)
	}

	path := "/debian/pool/main/h/hello/hello_1_amd64.deb"
	for _, target := range []string{upA.URL + path, upB.URL + path, upA.URL + path, upB.URL + path} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("target=%s code=%d body=%q", target, rec.Code, rec.Body.String())
		}
	}
	if hitsA.Load() != 1 || hitsB.Load() != 1 {
		t.Fatalf("host cache isolation failed: hitsA=%d hitsB=%d", hitsA.Load(), hitsB.Load())
	}
}

func TestAPTByHashFailureStreamsButDoesNotCache(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("wrong"))
	}))
	defer up.Close()
	a, err := New(testConfig(t, up.URL))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("expected"))
	requestPath := "/debian/dists/bookworm/main/binary-amd64/by-hash/SHA256/" + hex.EncodeToString(sum[:])
	target := up.URL + requestPath
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "wrong" {
			t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("failed validation response should not cache, hits=%d", hits.Load())
	}
	cachePath := aptCachePath(t, a.cfg.Cache.Root, up.URL, requestPath)
	if _, ok, err := a.hashStore.GetActual(cachePath, hashstore.HashSHA256); err != nil || ok {
		t.Fatalf("failed validation should delete actual hash metadata: ok=%v err=%v", ok, err)
	}
}

func TestValidateAPKHashAndSignatureBranches(t *testing.T) {
	cfg := testConfig(t, "http://example.invalid")
	cfg.APK.VerifyHash = true
	cfg.APK.VerifySignature = false
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	packageBody := []byte("apk payload")
	sum := sha256.Sum256(packageBody)
	indexPath := filepath.Join(cfg.Cache.Root, "alpine", "v3.23", "main", "x86_64", "APKINDEX.tar.gz")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	indexBody := []byte("P:hello\nV:1.0-r0\nS:" + strconv.Itoa(len(packageBody)) + "\nC:" + hex.EncodeToString(sum[:]) + "\n\n")
	if err := os.WriteFile(indexPath, testGzipTar(t, map[string][]byte{"APKINDEX": indexBody}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.apkIndex.LoadFile(indexPath); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(filepath.Dir(indexPath), "hello-1.0-r0.apk")
	if err := os.WriteFile(packagePath, packageBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.validateAPK(packagePath, packagePath, "package", false); err != nil {
		t.Fatalf("hash validate: %v", err)
	}
	if err := os.WriteFile(packagePath, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.validateAPK(packagePath, packagePath, "package", false); err == nil {
		t.Fatal("expected hash validation failure")
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(keyDir, "test.rsa.pub"), pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg = testConfig(t, "http://example.invalid")
	cfg.APK.VerifyHash = false
	cfg.APK.VerifySignature = true
	cfg.APK.KeysDir = keyDir
	a, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	signedPath := filepath.Join(cfg.Cache.Root, "signed.apk")
	if err := os.WriteFile(signedPath, testSignedArchive(t, key, "test.rsa.pub", map[string][]byte{"DESCRIPTION": []byte("payload")}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.validateAPK(signedPath, signedPath, "package", false); err != nil {
		t.Fatalf("signature validate: %v", err)
	}
	unsignedPath := filepath.Join(cfg.Cache.Root, "unsigned.apk")
	if err := os.WriteFile(unsignedPath, testGzipTar(t, map[string][]byte{"DESCRIPTION": []byte("payload")}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.validateAPK(unsignedPath, unsignedPath, "package", true); !errors.Is(err, ErrSoftCacheBypass) {
		t.Fatalf("expected soft bypass, got %v", err)
	}
}

func TestAPKHashStorePersistsAcrossRestartWithoutIndexScan(t *testing.T) {
	cfg := testConfig(t, "http://example.invalid")
	cfg.APK.VerifyHash = true
	cfg.APK.VerifySignature = false
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	packageBody := []byte("apk payload after restart")
	sum := sha256.Sum256(packageBody)
	indexPath := filepath.Join(cfg.Cache.Root, "alpine", "v3.23", "main", "x86_64", "APKINDEX.tar.gz")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	indexBody := []byte("P:hello\nV:1.0-r0\nS:" + strconv.Itoa(len(packageBody)) + "\nC:" + hex.EncodeToString(sum[:]) + "\n\n")
	if err := os.WriteFile(indexPath, testGzipTar(t, map[string][]byte{"APKINDEX": indexBody}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.apkIndex.LoadFile(indexPath); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(filepath.Dir(indexPath), "hello-1.0-r0.apk")
	if err := os.WriteFile(packagePath, packageBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.validateAPK(packagePath, packagePath, "package", false); err != nil {
		t.Fatalf("first validate: %v", err)
	}
	if err := first.hashStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}

	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.store.Close()
	defer second.hashStore.Close()
	second.apkIndex.SetHashStore(nil)
	if err := second.validateAPK(packagePath, packagePath, "package", false); err != nil {
		t.Fatalf("validate after restart without APKINDEX scan: %v", err)
	}
}

func TestAPKHashStoreRebuildsFromDiskIndexesWhenDeleted(t *testing.T) {
	cfg := testConfig(t, "http://example.invalid")
	cfg.APK.VerifyHash = true
	cfg.APK.VerifySignature = false
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	packageBody := []byte("apk payload after hash store rebuild")
	sum := sha256.Sum256(packageBody)
	indexPath := filepath.Join(cfg.Cache.Root, "alpine", "v3.23", "main", "x86_64", "APKINDEX.tar.gz")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	indexBody := []byte("P:hello\nV:1.0-r0\nS:" + strconv.Itoa(len(packageBody)) + "\nC:" + hex.EncodeToString(sum[:]) + "\n\n")
	if err := os.WriteFile(indexPath, testGzipTar(t, map[string][]byte{"APKINDEX": indexBody}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.apkIndex.LoadFile(indexPath); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(filepath.Dir(indexPath), "hello-1.0-r0.apk")
	if err := os.WriteFile(packagePath, packageBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.validateAPK(packagePath, packagePath, "package", false); err != nil {
		t.Fatalf("first validate: %v", err)
	}
	hashStorePath := cfg.HashStore.Path
	if err := first.hashStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(hashStorePath); err != nil {
		t.Fatal(err)
	}

	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.store.Close()
	defer second.hashStore.Close()
	stats, err := second.hashStore.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.LastRebuildUnixNS == 0 || stats.LastRebuildReason != "empty_or_incompatible" || stats.CorruptionStatus != "ok" {
		t.Fatalf("unexpected rebuild stats: %#v", stats)
	}
	second.apkIndex.SetHashStore(nil)
	if err := second.validateAPK(packagePath, packagePath, "package", false); err != nil {
		t.Fatalf("validate after hash store rebuild: %v", err)
	}
}

func TestProxyForwardAbsoluteURL(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain"))
	}))
	defer up.Close()

	a, err := New(testConfig(t, up.URL))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, up.URL+"/plain.txt", nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "plain" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get(HeaderCache) != CacheBypass {
		t.Fatalf("cache=%s", rec.Header().Get(HeaderCache))
	}
}

func TestConnectDisabled(t *testing.T) {
	cfg := testConfig(t, "http://example.invalid")
	cfg.Proxy.AllowConnect = false
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodConnect, "http://cache.local", nil)
	req.Host = "example.com:443"
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	a, err := New(testConfig(t, "http://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/_health", nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "healthy" {
		t.Fatalf("status=%v", body["status"])
	}
}

func TestUnsupportedAndPathTraversalRequests(t *testing.T) {
	a, err := New(testConfig(t, "http://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plain.txt", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported code=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/alpine/../secret.apk", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("path traversal code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestProxyDisabledAndAllowedHosts(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer up.Close()

	cfg := testConfig(t, up.URL)
	cfg.Proxy.Enabled = false
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, up.URL+"/plain.txt", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("proxy disabled code=%d", rec.Code)
	}

	cfg = testConfig(t, up.URL)
	cfg.Proxy.AllowedHosts = []string{"other.example"}
	a, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, up.URL+"/plain.txt", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("allowed host code=%d", rec.Code)
	}
}

func TestAPKNonOKBypassesCache(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer up.Close()
	a, err := New(testConfig(t, up.URL))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/alpine/v3.23/main/x86_64/missing.apk", nil)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req.Clone(req.Context()))
		if rec.Code != http.StatusNotFound || rec.Header().Get(HeaderCache) != CacheBypass {
			t.Fatalf("code=%d cache=%s", rec.Code, rec.Header().Get(HeaderCache))
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("non-ok response was cached, hits=%d", hits.Load())
	}
}

func TestProxyCachesNonPackageWhenEnabled(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("plain"))
	}))
	defer up.Close()
	cfg := testConfig(t, up.URL)
	cfg.Proxy.CacheNonPackage = true
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, up.URL+"/plain.txt", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("proxy cache miss count=%d", hits.Load())
	}
}

func TestHealthDegradedWhenCacheMissing(t *testing.T) {
	cfg := testConfig(t, "http://example.invalid")
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cfg.Cache.Root); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestConnectSuccessOverRealHTTPServer(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		conn, err := target.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	a, err := New(testConfig(t, "http://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", serverURL.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "CONNECT "+target.Addr().String()+" HTTP/1.1\r\nHost: "+target.Addr().String()+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect status=%s", resp.Status)
	}
}

func TestRunStartsAndStops(t *testing.T) {
	cfg := testConfig(t, "http://example.invalid")
	cfg.Server.Listen = "127.0.0.1:0"
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop")
	}
}

func TestNewRejectsInvalidRuntimeConfig(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.Config)
	}{
		{"index ttl", func(c *config.Config) { c.Cache.IndexTTL = "bad" }},
		{"package ttl", func(c *config.Config) { c.Cache.PackageTTL = "bad" }},
		{"memory max size", func(c *config.Config) { c.Cache.Memory.MaxSize = "bad" }},
		{"memory item size", func(c *config.Config) { c.Cache.Memory.MaxItemSize = "bad" }},
		{"memory ttl", func(c *config.Config) { c.Cache.Memory.TTL = "bad" }},
		{"transport timeout", func(c *config.Config) { c.Transport.Timeout = "bad" }},
		{"keys dir", func(c *config.Config) { c.APK.KeysDir = filepath.Join(t.TempDir(), "missing") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t, "http://example.invalid")
			tc.edit(cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("expected New error")
			}
		})
	}
}

func TestRequestHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/path?q=1", nil)
	req.Host = "example.com"
	target, err := forwardURL(req)
	if err != nil {
		t.Fatal(err)
	}
	if target.String() != "http://example.com/path?q=1" {
		t.Fatalf("target=%s", target)
	}
	if _, err := safeCacheKey("/../bad"); err == nil {
		t.Fatal("expected traversal error")
	}
	if sanitizeHost("[::1]:443") != "___1__443" {
		t.Fatalf("sanitize=%s", sanitizeHost("[::1]:443"))
	}
	if stripPort("[::1]:443") != "::1" || ensurePort("example.com", "443") != "example.com:443" {
		t.Fatal("host helpers failed")
	}
}

func testGzipTar(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var gz bytes.Buffer
	gzw := gzip.NewWriter(&gz)
	tw := tar.NewWriter(gzw)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return gz.Bytes()
}

func testSignedArchive(t *testing.T, key *rsa.PrivateKey, keyName string, entries map[string][]byte) []byte {
	t.Helper()
	signedMember := testGzipTar(t, entries)
	sum := sha256.Sum256(signedMember)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	signatureMember := testGzipTar(t, map[string][]byte{".SIGN.RSA256." + keyName: signature})
	return append(signatureMember, signedMember...)
}
