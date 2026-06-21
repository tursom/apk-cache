package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	aptpkg "github.com/tursom/apk-cache/internal/apt"
)

func TestRealAPTFullChainCachesAndValidates(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "real", "apt")
	manifest := readManifest(t, filepath.Join(fixtureDir, "MANIFEST.txt"))

	releasePath := "/debian/dists/bookworm-updates/Release"
	packagesPath := "/debian/dists/bookworm-updates/main/binary-amd64/Packages.xz"
	byHashPath := manifest["packages_by_hash_path"]
	debPath := "/debian/" + manifest["deb_filename"]

	upstream := newFixtureServer(t, map[string]string{
		releasePath:  filepath.Join(fixtureDir, "Release"),
		packagesPath: filepath.Join(fixtureDir, "Packages.xz"),
		byHashPath:   filepath.Join(fixtureDir, "Packages.xz"),
		debPath:      filepath.Join(fixtureDir, "libwbclient-dev_4.17.12+dfsg-0+deb12u2_amd64.deb"),
	})

	cfg := testConfig(t, upstream.URL)
	cfg.Cache.Memory.Enabled = false
	cfg.APT.VerifyHash = true
	cfg.APT.LoadIndexAsync = false
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	rec := getThroughApp(t, a, upstream.URL+releasePath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "Release"))

	rec = getThroughApp(t, a, upstream.URL+packagesPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "Packages.xz"))

	packagesCachePath := aptCachePath(t, cfg.Cache.Root, upstream.URL, packagesPath)
	if err := os.WriteFile(packagesCachePath, []byte("corrupted packages"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforePackages := upstream.Count(packagesPath)
	rec = getThroughApp(t, a, upstream.URL+packagesPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "Packages.xz"))
	if got := upstream.Count(packagesPath); got != beforePackages+1 {
		t.Fatalf("corrupted Packages.xz should refetch once, before=%d after=%d", beforePackages, got)
	}

	rec = getThroughApp(t, a, upstream.URL+byHashPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "Packages.xz"))

	byHashCachePath := aptCachePath(t, cfg.Cache.Root, upstream.URL, byHashPath)
	if err := os.WriteFile(byHashCachePath, []byte("corrupted by-hash"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeByHash := upstream.Count(byHashPath)
	rec = getThroughApp(t, a, upstream.URL+byHashPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "Packages.xz"))
	if got := upstream.Count(byHashPath); got != beforeByHash+1 {
		t.Fatalf("corrupted by-hash file should refetch once, before=%d after=%d", beforeByHash, got)
	}

	rec = getThroughApp(t, a, upstream.URL+debPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "libwbclient-dev_4.17.12+dfsg-0+deb12u2_amd64.deb"))

	beforeDebHit := upstream.Count(debPath)
	rec = getThroughApp(t, a, upstream.URL+debPath)
	assertOKCache(t, rec, CacheHit)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "libwbclient-dev_4.17.12+dfsg-0+deb12u2_amd64.deb"))
	if got := upstream.Count(debPath); got != beforeDebHit {
		t.Fatalf("cached .deb should not refetch, before=%d after=%d", beforeDebHit, got)
	}

	debCachePath := aptCachePath(t, cfg.Cache.Root, upstream.URL, debPath)
	if err := os.WriteFile(debCachePath, []byte("corrupted deb"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = getThroughApp(t, a, upstream.URL+debPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "libwbclient-dev_4.17.12+dfsg-0+deb12u2_amd64.deb"))
	if got := upstream.Count(debPath); got != beforeDebHit+1 {
		t.Fatalf("corrupted .deb should refetch once, before=%d after=%d", beforeDebHit, got)
	}
}

func TestRealAPTByHashIndexLoadsPackageRecords(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "real", "apt")
	manifest := readManifest(t, filepath.Join(fixtureDir, "MANIFEST.txt"))

	releasePath := "/debian/dists/bookworm-updates/Release"
	byHashPath := manifest["packages_by_hash_path"]
	debPath := "/debian/" + manifest["deb_filename"]

	upstream := newFixtureServer(t, map[string]string{
		releasePath: filepath.Join(fixtureDir, "Release"),
		byHashPath:  filepath.Join(fixtureDir, "Packages.xz"),
		debPath:     filepath.Join(fixtureDir, "libwbclient-dev_4.17.12+dfsg-0+deb12u2_amd64.deb"),
	})

	cfg := testConfig(t, upstream.URL)
	cfg.Cache.Memory.Enabled = false
	cfg.APT.VerifyHash = true
	cfg.APT.LoadIndexAsync = false
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	rec := getThroughApp(t, a, upstream.URL+releasePath)
	assertOKCache(t, rec, CacheMiss)
	rec = getThroughApp(t, a, upstream.URL+byHashPath)
	assertOKCache(t, rec, CacheMiss)
	rec = getThroughApp(t, a, upstream.URL+debPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "libwbclient-dev_4.17.12+dfsg-0+deb12u2_amd64.deb"))

	beforeDeb := upstream.Count(debPath)
	debCachePath := aptCachePath(t, cfg.Cache.Root, upstream.URL, debPath)
	if err := os.WriteFile(debCachePath, []byte("corrupted deb after by-hash index"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = getThroughApp(t, a, upstream.URL+debPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "libwbclient-dev_4.17.12+dfsg-0+deb12u2_amd64.deb"))
	if got := upstream.Count(debPath); got != beforeDeb+1 {
		t.Fatalf("corrupted .deb should refetch when package records came from by-hash index, before=%d after=%d", beforeDeb, got)
	}
}

func TestRealAPKFullChainCachesAndValidates(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "real", "apk")
	manifest := readManifest(t, filepath.Join(fixtureDir, "MANIFEST.txt"))

	indexPath := "/alpine/v3.23/main/x86_64/APKINDEX.tar.gz"
	packagePath := "/alpine/v3.23/main/x86_64/" + manifest["package_filename"]

	upstream := newFixtureServer(t, map[string]string{
		indexPath:   filepath.Join(fixtureDir, "APKINDEX.tar.gz"),
		packagePath: filepath.Join(fixtureDir, manifest["package_filename"]),
	})

	cfg := testConfig(t, upstream.URL)
	cfg.Cache.Memory.Enabled = false
	cfg.APK.VerifyHash = true
	cfg.APK.VerifySignature = true
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	rec := getThroughApp(t, a, indexPath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, "APKINDEX.tar.gz"))

	rec = getThroughApp(t, a, packagePath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, manifest["package_filename"]))

	beforeHit := upstream.Count(packagePath)
	rec = getThroughApp(t, a, packagePath)
	assertOKCache(t, rec, CacheHit)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, manifest["package_filename"]))
	if got := upstream.Count(packagePath); got != beforeHit {
		t.Fatalf("cached APK should not refetch, before=%d after=%d", beforeHit, got)
	}

	packageCachePath := filepath.Join(cfg.Cache.Root, strings.TrimPrefix(packagePath, "/"))
	if err := os.WriteFile(packageCachePath, []byte("corrupted apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = getThroughApp(t, a, packagePath)
	assertOKCache(t, rec, CacheMiss)
	assertBodyFile(t, rec, filepath.Join(fixtureDir, manifest["package_filename"]))
	if got := upstream.Count(packagePath); got != beforeHit+1 {
		t.Fatalf("corrupted APK should refetch once, before=%d after=%d", beforeHit, got)
	}
}

type fixtureServer struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
}

func newFixtureServer(t *testing.T, files map[string]string) *fixtureServer {
	t.Helper()
	server := &fixtureServer{counts: make(map[string]int)}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.mu.Lock()
		server.counts[r.URL.Path]++
		server.mu.Unlock()

		path, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *fixtureServer) Count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[path]
}

func getThroughApp(t *testing.T, a *App, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func assertOKCache(t *testing.T, rec *httptest.ResponseRecorder, cache string) {
	t.Helper()
	if rec.Code != http.StatusOK || rec.Header().Get(HeaderCache) != cache {
		t.Fatalf("code=%d cache=%s body=%q", rec.Code, rec.Header().Get(HeaderCache), rec.Body.String())
	}
}

func assertBodyFile(t *testing.T, rec *httptest.ResponseRecorder, path string) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("body does not match %s: got=%d want=%d", path, rec.Body.Len(), len(want))
	}
}

func aptCachePath(t *testing.T, cacheRoot, upstreamURL, requestPath string) string {
	t.Helper()
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatal(err)
	}
	return aptpkg.CachePath(cacheRoot, parsed.Host, strings.TrimPrefix(requestPath, "/"))
}

func readManifest(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("invalid manifest line %q in %s", line, path)
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}
