package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestAdminDefaultLoginAccountAndConfigUpdate(t *testing.T) {
	cfg := testConfig(t, "http://example.invalid")
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.store.Close()
	defer a.hashStore.Close()

	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `id="root"`) || !strings.Contains(body, `/admin/assets/assets/index-`) {
		t.Fatalf("admin page code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(body, `href="/admin/assets/favicon.ico"`) || !strings.Contains(body, `href="/admin/assets/favicon.svg"`) {
		t.Fatalf("admin page missing favicon links: %s", body)
	}
	cssPath := regexp.MustCompile(`href="([^"]+\.css)"`).FindStringSubmatch(body)
	jsPath := regexp.MustCompile(`src="([^"]+\.js)"`).FindStringSubmatch(body)
	if len(cssPath) != 2 || len(jsPath) != 2 {
		t.Fatalf("missing vite assets in admin page: %s", body)
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, cssPath[1], nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), ":root") {
		t.Fatalf("admin css code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, jsPath[1], nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "APK Cache Admin") {
		t.Fatalf("admin js code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "image/x-icon") || !bytes.HasPrefix(rec.Body.Bytes(), []byte{0, 0, 1, 0}) {
		t.Fatalf("favicon ico code=%d content-type=%s len=%d", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/assets/favicon.ico", nil))
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte{0, 0, 1, 0}) {
		t.Fatalf("admin asset favicon ico code=%d len=%d", rec.Code, rec.Body.Len())
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "image/svg+xml") || !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("favicon svg code=%d content-type=%s body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"admin","password":"admin123456"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("login code=%d body=%s", rec.Code, rec.Body.String())
	}
	var login struct {
		OK   bool `json:"ok"`
		Data struct {
			Username            string `json:"username"`
			IsDefaultCredential bool   `json:"is_default_credential"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	if !login.OK || login.Data.Username != "admin" || !login.Data.IsDefaultCredential {
		t.Fatalf("unexpected login response: %s", rec.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		switch cookie.Name {
		case adminSessionCookie:
			sessionCookie = cookie
		case adminCSRFCookie:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("missing auth cookies: %#v", rec.Result().Cookies())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/proxy/host-rules", strings.NewReader(`{"host":"deb.example","enabled":true,"description":"test"}`))
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create host rule code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/admin/v1/apt/mirrors", strings.NewReader(`{"name":"Debian","public_prefix":"/debian","upstream_url":"http://example.invalid/debian","enabled":true}`))
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create apt mirror code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/account", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"is_default_credential":true`) {
		t.Fatalf("account code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/v1/config", strings.NewReader(`{"settings":{"cache.index_ttl":"48h"}}`))
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected csrf failure, code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/v1/config", strings.NewReader(`{"settings":{"cache.index_ttl":"48h"}}`))
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("config update code=%d body=%s", rec.Code, rec.Body.String())
	}
	if a.indexTTL != 48*time.Hour {
		t.Fatalf("indexTTL=%s", a.indexTTL)
	}

	oldCacheRoot := a.cfg.Cache.Root
	req = httptest.NewRequest(http.MethodPut, "/api/admin/v1/config", strings.NewReader(`{"settings":{"cache.root":"/tmp/apk-cache-next-root"}}`))
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cache.root"`) {
		t.Fatalf("cache.root update code=%d body=%s", rec.Code, rec.Body.String())
	}
	if a.cfg.Cache.Root != oldCacheRoot {
		t.Fatalf("cache root switched without restart: %s", a.cfg.Cache.Root)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/v1/account/username", strings.NewReader(`{"username":"rootadmin"}`))
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("username update code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/v1/account/password", strings.NewReader(`{"old_password":"admin123456","new_password":"password123"}`))
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("password update code=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/account", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"is_default_credential":false`) {
		t.Fatalf("account after update code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/dashboard/summary", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/system/info", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"go"`) {
		t.Fatalf("system info code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/logs/requests?limit=10", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items"`) {
		t.Fatalf("request logs code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/admin/v1/diagnostics/package", nil)
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("diagnostics code=%d content-type=%s body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestAdminPackageListsArePaginated(t *testing.T) {
	cfg := testConfig(t, "http://example.invalid")
	apkIndexPath := filepath.Join(cfg.Cache.Root, "alpine", "v3.23", "main", "x86_64", "APKINDEX.tar.gz")
	if err := os.MkdirAll(filepath.Dir(apkIndexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	apkIndex := []byte(strings.Join([]string{
		"P:alpha\nV:1.0-r0\nS:11\n",
		"P:beta\nV:1.0-r0\nS:22\n",
		"P:gamma\nV:1.0-r0\nS:33\n",
	}, "\n"))
	if err := os.WriteFile(apkIndexPath, testGzipTar(t, map[string][]byte{"APKINDEX": apkIndex}), 0o644); err != nil {
		t.Fatal(err)
	}

	aptPackagesPath := filepath.Join(cfg.Cache.Root, "apt", "deb.example", "debian", "dists", "bookworm", "main", "binary-amd64", "Packages")
	if err := os.MkdirAll(filepath.Dir(aptPackagesPath), 0o755); err != nil {
		t.Fatal(err)
	}
	aptPackages := strings.Join([]string{
		"Package: apt-alpha\nFilename: pool/main/a/apt-alpha.deb\nSize: 11\nSHA256: " + strings.Repeat("a", 64) + "\n",
		"Package: apt-beta\nFilename: pool/main/b/apt-beta.deb\nSize: 22\nSHA256: " + strings.Repeat("b", 64) + "\n",
		"Package: apt-gamma\nFilename: pool/main/g/apt-gamma.deb\nSize: 33\nSHA256: " + strings.Repeat("c", 64) + "\n",
	}, "\n")
	if err := os.WriteFile(aptPackagesPath, []byte(aptPackages), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.store.Close()
	defer a.hashStore.Close()
	sessionCookie, _ := adminLoginForTest(t, a)

	apkPage := adminGETForData[struct {
		Items []struct {
			Name string `json:"package_name"`
		} `json:"items"`
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}](t, a, "/api/admin/v1/apk/packages?page=2&page_size=2", sessionCookie)
	if apkPage.Total != 3 || apkPage.Page != 2 || apkPage.PageSize != 2 || len(apkPage.Items) != 1 || apkPage.Items[0].Name != "gamma" {
		t.Fatalf("unexpected apk page: %+v", apkPage)
	}

	apkSearch := adminGETForData[struct {
		Items []struct {
			Name string `json:"package_name"`
		} `json:"items"`
		Total int `json:"total"`
	}](t, a, "/api/admin/v1/apk/packages?q=beta&page=1&page_size=2", sessionCookie)
	if apkSearch.Total != 1 || len(apkSearch.Items) != 1 || apkSearch.Items[0].Name != "beta" {
		t.Fatalf("unexpected apk search page: %+v", apkSearch)
	}

	aptPage := adminGETForData[struct {
		Items []struct {
			PackageName string `json:"package_name"`
		} `json:"items"`
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}](t, a, "/api/admin/v1/apt/records?page=2&page_size=2", sessionCookie)
	if aptPage.Total != 3 || aptPage.Page != 2 || aptPage.PageSize != 2 || len(aptPage.Items) != 1 || aptPage.Items[0].PackageName != "apt-gamma" {
		t.Fatalf("unexpected apt page: %+v", aptPage)
	}

	aptSearch := adminGETForData[struct {
		Items []struct {
			PackageName string `json:"package_name"`
		} `json:"items"`
		Total int `json:"total"`
	}](t, a, "/api/admin/v1/apt/records?q=apt-beta&page=1&page_size=2", sessionCookie)
	if aptSearch.Total != 1 || len(aptSearch.Items) != 1 || aptSearch.Items[0].PackageName != "apt-beta" {
		t.Fatalf("unexpected apt search page: %+v", aptSearch)
	}
}

func adminLoginForTest(t *testing.T, a *App) (*http.Cookie, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"admin","password":"admin123456"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("login code=%d body=%s", rec.Code, rec.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		switch cookie.Name {
		case adminSessionCookie:
			sessionCookie = cookie
		case adminCSRFCookie:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("missing auth cookies: %#v", rec.Result().Cookies())
	}
	return sessionCookie, csrfCookie
}

func adminGETForData[T any](t *testing.T, a *App, path string, sessionCookie *http.Cookie) T {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s code=%d body=%s", path, rec.Code, rec.Body.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data T    `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK {
		t.Fatalf("GET %s returned not ok: %s", path, rec.Body.String())
	}
	return response.Data
}
