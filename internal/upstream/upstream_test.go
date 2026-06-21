package upstream

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildURLAvoidsDuplicateAlpineSegment(t *testing.T) {
	got, err := BuildURL("https://mirror.example/alpine", "/alpine/v3.23/main/x86_64/APKINDEX.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://mirror.example/alpine/v3.23/main/x86_64/APKINDEX.tar.gz"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestManagerFetchFailoverAndHealth(t *testing.T) {
	var firstHits, secondHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		if r.URL.Path != "/alpine/pkg.apk" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer second.Close()

	var requests, failovers atomic.Int32
	manager := NewManager(realClientFactory{})
	manager.SetMetricsHooks(func() { requests.Add(1) }, func() { failovers.Add(1) })
	bad := NewServer(first.URL, "", "bad")
	good := NewServer(second.URL, "", "good")
	manager.Add(bad)
	manager.Add(good)

	resp, err := manager.Fetch(context.Background(), "/alpine/pkg.apk", http.Header{"X-Test": []string{"1"}})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body=%q", body)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 || requests.Load() != 1 || failovers.Load() != 1 {
		t.Fatalf("hits/metrics first=%d second=%d req=%d fail=%d", firstHits.Load(), secondHits.Load(), requests.Load(), failovers.Load())
	}
	if bad.Healthy() || bad.LastError() == "" {
		t.Fatal("failed server health not updated")
	}
	if manager.Count() != 2 || manager.HealthyCount() != 1 || len(manager.Servers()) != 2 {
		t.Fatal("manager counts are wrong")
	}
}

func TestManagerFetchReturnsLastNonOKResponse(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer up.Close()
	manager := NewManager(realClientFactory{})
	manager.Add(NewServer(up.URL, "", "only"))
	resp, err := manager.Fetch(context.Background(), "/missing.apk", nil)
	if err != nil {
		t.Fatalf("fetch returned error instead of last response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestManagerFetchNoServers(t *testing.T) {
	manager := NewManager(realClientFactory{})
	if _, err := manager.Fetch(context.Background(), "/pkg.apk", nil); err == nil {
		t.Fatal("expected no upstream error")
	}
}

func TestTransportProxyHelpers(t *testing.T) {
	parsed, err := url.Parse("http://user:pass@proxy.local:8080")
	if err != nil {
		t.Fatal(err)
	}
	if got := ProxyAuthorizationValue(parsed); got != "Basic dXNlcjpwYXNz" {
		t.Fatalf("auth=%s", got)
	}

	transport := CreateTransport(parsed.String())
	if transport.Proxy == nil {
		t.Fatal("http proxy function not configured")
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.Host != "proxy.local:8080" {
		t.Fatalf("proxy url=%s", proxyURL)
	}

	if direct := CreateTransport(""); direct.Proxy != nil {
		t.Fatal("empty proxy should not use environment proxy")
	}
	if socks := CreateTransport("socks5://127.0.0.1:1080"); socks.DialContext == nil {
		t.Fatal("socks5 proxy should configure DialContext")
	}
}

func TestDialContextViaProxyDirectAndHTTPProxy(t *testing.T) {
	target := newEchoListener(t)
	defer target.Close()
	conn, err := DialContextViaProxy(context.Background(), "", "tcp", target.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("direct dial: %v", err)
	}
	_ = conn.Close()

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyListener.Close()
	go func() {
		conn, err := proxyListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		if req.Method != http.MethodConnect || !strings.Contains(req.Host, target.Addr().String()) {
			t.Errorf("unexpected CONNECT request: %s %s", req.Method, req.Host)
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	}()
	proxyURL := "http://" + proxyListener.Addr().String()
	proxied, err := DialContextViaProxy(context.Background(), proxyURL, "tcp", target.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("http proxy dial: %v", err)
	}
	_ = proxied.Close()
}

func TestDialContextViaProxyErrors(t *testing.T) {
	if _, err := DialContextViaProxy(context.Background(), "ftp://127.0.0.1:1", "tcp", "example.com:443", time.Second); err == nil {
		t.Fatal("expected unsupported proxy scheme")
	}
	if _, err := DialContextViaProxy(context.Background(), "http://127.0.0.1:1", "udp", "example.com:443", time.Second); err == nil {
		t.Fatal("expected non-tcp error")
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyListener.Close()
	go func() {
		conn, err := proxyListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = http.ReadRequest(bufio.NewReader(conn))
		_, _ = conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
	}()
	_, err = DialContextViaProxy(context.Background(), "http://"+proxyListener.Addr().String(), "tcp", "example.com:443", time.Second)
	if err == nil {
		t.Fatal("expected proxy rejection error")
	}
}

type realClientFactory struct{}

func (realClientFactory) Client(proxyAddr string) *http.Client {
	return &http.Client{Transport: CreateTransport(proxyAddr), Timeout: 2 * time.Second}
}

func newEchoListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return listener
}
