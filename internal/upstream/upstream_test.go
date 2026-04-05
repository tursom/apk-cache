package upstream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetcherFallsBackToHealthyServer(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer failing.Close()

	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer healthy.Close()

	manager := NewManager()
	manager.AddServer(NewServer(failing.URL, "", "bad", time.Second))
	manager.AddServer(NewServer(healthy.URL, "", "good", time.Second))

	fetcher := NewFetcher(manager, func(proxy string) *http.Client {
		return &http.Client{Timeout: 2 * time.Second}
	})

	resp, err := fetcher.Fetch("/alpine/v3.20/main/x86_64/test.apk", nil)
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
}
