package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
