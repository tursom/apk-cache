package upstream

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetcherFallbackScenarios(t *testing.T) {
	t.Run("fallback on 5xx", func(t *testing.T) {
		failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusBadGateway)
		}))
		defer failing.Close()

		healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		defer healthy.Close()

		manager := newTestManager(
			NewServer(failing.URL, "", "bad", time.Second),
			NewServer(healthy.URL, "", "good", time.Second),
		)

		resp, err := newTestFetcher(manager).Fetch("/alpine/v3.20/main/x86_64/test.apk", nil)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != "ok" {
			t.Fatalf("body = %q want %q", got, "ok")
		}
	})

	t.Run("fallback on network error", func(t *testing.T) {
		unavailableURL := newUnavailableURL(t)

		healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		defer healthy.Close()

		manager := newTestManager(
			NewServer(unavailableURL, "", "offline", time.Second),
			NewServer(healthy.URL, "", "good", time.Second),
		)

		resp, err := newTestFetcher(manager).Fetch("/alpine/v3.20/main/x86_64/test.apk", nil)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != "ok" {
			t.Fatalf("body = %q want %q", got, "ok")
		}
	})

	t.Run("returns last error when all upstreams fail", func(t *testing.T) {
		manager := newTestManager(
			NewServer(newUnavailableURL(t), "", "offline-1", time.Second),
			NewServer(newUnavailableURL(t), "", "offline-2", time.Second),
		)

		resp, err := newTestFetcher(manager).Fetch("/alpine/v3.20/main/x86_64/test.apk", nil)
		if err == nil {
			if resp != nil {
				resp.Body.Close()
			}
			t.Fatalf("expected fetch to fail")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "connect") && !strings.Contains(strings.ToLower(err.Error()), "refused") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestFetcherReturnsLastNonOKResponse(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer first.Close()

	last := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer last.Close()

	manager := newTestManager(
		NewServer(first.URL, "", "bad", time.Second),
		NewServer(last.URL, "", "missing", time.Second),
	)

	resp, err := newTestFetcher(manager).Fetch("/alpine/v3.20/main/x86_64/test.apk", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d want %d", resp.StatusCode, http.StatusNotFound)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "missing") {
		t.Fatalf("body = %q, want missing response", string(body))
	}
}

func TestBuildUpstreamURLCompatibility(t *testing.T) {
	testCases := []struct {
		name        string
		baseSuffix  string
		requestPath string
		wantPath    string
	}{
		{
			name:        "official alpine base",
			baseSuffix:  "",
			requestPath: "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz",
			wantPath:    "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz",
		},
		{
			name:        "mirror base already ends with alpine",
			baseSuffix:  "/alpine",
			requestPath: "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz",
			wantPath:    "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz",
		},
		{
			name:        "extra slashes remain stable",
			baseSuffix:  "/alpine//",
			requestPath: "/alpine/v3.22/main/",
			wantPath:    "/alpine/v3.22/main/",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte("ok"))
			}))
			defer upstreamServer.Close()

			manager := newTestManager(NewServer(upstreamServer.URL+tc.baseSuffix, "", tc.name, time.Second))
			resp, err := newTestFetcher(manager).Fetch(tc.requestPath, nil)
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			resp.Body.Close()

			if gotPath != tc.wantPath {
				t.Fatalf("requested path = %q want %q", gotPath, tc.wantPath)
			}
		})
	}
}

func newTestFetcher(manager *Manager) *DefaultFetcher {
	return NewFetcher(manager, func(proxy string) *http.Client {
		return &http.Client{Timeout: 2 * time.Second}
	})
}

func newTestManager(servers ...*Server) *Manager {
	manager := NewManager()
	for _, server := range servers {
		manager.AddServer(server)
	}
	return manager
}

func newUnavailableURL(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return "http://" + address
}
