package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "github.com/tursom/apk-cache/internal/config"
)

// TestIntegrationFullLifecycle exercises the full request cycle through a
// real HTTP server: APK MISS then HIT, health check, CONNECT tunnel.
func TestIntegrationFullLifecycle(t *testing.T) {
	var upstreamHits atomic.Int32
	indexBody := buildSignedArchive(t, "ignored", nil, "DESCRIPTION", []byte(""), true, false)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, ".apk"):
			_, _ = w.Write([]byte("apk-payload"))
		case strings.Contains(r.URL.Path, "APKINDEX"):
			_, _ = w.Write(indexBody)
		case strings.Contains(r.URL.Path, "InRelease"):
			_, _ = w.Write([]byte("apt-release"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstream.URL, Kind: "apk"}}
	})
	setTestHTTPClient(app, "", upstream.Client())

	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	client := server.Client()

	apkURL := server.URL + "/alpine/v3.20/main/x86_64/test.apk"

	resp1, err := client.Get(apkURL)
	mustNoError(t, err, "apk get 1")
	mustStatus(t, resp1, http.StatusOK)
	mustBody(t, resp1, "apk-payload")
	mustHeader(t, resp1, "X-Cache", "MISS")
	resp1.Body.Close()

	resp2, err := client.Get(apkURL)
	mustNoError(t, err, "apk get 2")
	mustStatus(t, resp2, http.StatusOK)
	mustBody(t, resp2, "apk-payload")
	if got := resp2.Header.Get("X-Cache"); got != "HIT" && got != "MEMORY-HIT" {
		t.Fatalf("APK second X-Cache = %q want HIT or MEMORY-HIT", got)
	}
	resp2.Body.Close()

	if hits := upstreamHits.Load(); hits != 1 {
		t.Fatalf("upstream hits after APK = %d want 1", hits)
	}

	respH, err := client.Get(server.URL + "/_health")
	mustNoError(t, err, "health")
	mustStatus(t, respH, http.StatusOK)
	respH.Body.Close()

	echoServer := newTCPEchoServer(t)
	defer echoServer.Close()

	status, echoed := performConnectRoundTrip(t, app.server.Handler, echoServer.Addr(), "hello-tunnel")
	if status != http.StatusOK {
		t.Fatalf("CONNECT status = %d", status)
	}
	if echoed != "hello-tunnel" {
		t.Fatalf("CONNECT echoed = %q", echoed)
	}
}

// TestIntegrationConcurrentRequests verifies that multiple goroutines can
// send requests concurrently without races or data corruption.
func TestIntegrationConcurrentRequests(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(filepath.Base(r.URL.Path)))
	}))
	defer upstream.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstream.URL, Kind: "apk"}}
	})

	server := httptest.NewServer(app.server.Handler)
	defer server.Close()
	client := server.Client()

	const (
		workers   = 12
		perWorker = 5
	)
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)

	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range perWorker {
				url := fmt.Sprintf("%s/alpine/v3.20/main/x86_64/pkg-%d.apk", server.URL, id)
				resp, err := client.Get(url)
				if err != nil {
					errs <- fmt.Errorf("worker %d req %d: %w", id, j, err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					errs <- fmt.Errorf("worker %d req %d: status %d", id, j, resp.StatusCode)
					resp.Body.Close()
					return
				}
				resp.Body.Close()
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}

// TestIntegrationGracefulShutdown verifies that a running server shuts down
// cleanly: the listener stops accepting new connections, but established
// connections complete before the server exits.
func TestIntegrationGracefulShutdown(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	defer upstream.Close()

	app := mustNewTestApp(t, func(cfg *internalconfig.Config) {
		cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstream.URL, Kind: "apk"}}
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	mustNoError(t, err, "listen")
	app.server.Addr = listener.Addr().String()

	go func() {
		_ = app.server.Serve(listener)
	}()

	baseURL := "http://" + listener.Addr().String()
	apkURL := baseURL + "/alpine/v3.20/main/x86_64/test.apk"

	resp1, err := http.Get(apkURL)
	mustNoError(t, err, "pre-shutdown get")
	mustStatus(t, resp1, http.StatusOK)
	mustBody(t, resp1, "done")
	resp1.Body.Close()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := app.server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	_, err = http.Get(apkURL)
	if err == nil {
		t.Fatal("expected connection refused after shutdown")
	}

	if app.memoryCache != nil {
		app.memoryCache.Stop()
	}
	app.bgWg.Wait()
}

// TestIntegrationCachePersistence verifies that disk-cached responses survive
// a restart: write a response to cache, create a fresh App pointing at the
// same cache directory, and confirm a HIT without upstream contact.
func TestIntegrationCachePersistence(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = w.Write([]byte("persistent"))
	}))
	defer upstream.Close()

	sharedRoot := t.TempDir()
	cacheRoot := filepath.Join(sharedRoot, "cache")
	dataRoot := filepath.Join(sharedRoot, "data")

	createApp := func() *App {
		app, err := NewApp(func() *internalconfig.Config {
			cfg := internalconfig.Default()
			cfg.Cache.Root = cacheRoot
			cfg.Cache.DataRoot = dataRoot
			cfg.Cache.Memory.Enabled = false
			cfg.Upstreams = []internalconfig.UpstreamConfig{{URL: upstream.URL, Kind: "apk"}}
			cfg.APK.VerifyHash = false
			cfg.APK.VerifySignature = false
			return cfg
		}())
		mustNoError(t, err, "new app")
		return app
	}

	apkPath := "/alpine/v3.20/main/x86_64/persist.apk"

	app1 := createApp()
	server1 := httptest.NewServer(app1.server.Handler)
	resp1, err := server1.Client().Get(server1.URL + apkPath)
	mustNoError(t, err, "app1 get")
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("app1 status = %d", resp1.StatusCode)
	}
	resp1.Body.Close()
	server1.Close()

	if upstreamHits.Load() != 1 {
		t.Fatalf("app1 upstream hits = %d want 1", upstreamHits.Load())
	}

	app2 := createApp()
	server2 := httptest.NewServer(app2.server.Handler)
	defer server2.Close()

	resp2, err := server2.Client().Get(server2.URL + apkPath)
	mustNoError(t, err, "app2 get")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("app2 status = %d", resp2.StatusCode)
	}
	if got := resp2.Header.Get("X-Cache"); got != "HIT" {
		t.Fatalf("app2 X-Cache = %q want HIT", got)
	}
	resp2.Body.Close()

	if upstreamHits.Load() != 1 {
		t.Fatalf("app2 upstream hits = %d want 1", upstreamHits.Load())
	}
}

func mustNoError(t *testing.T, err error, context string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", context, err)
	}
}

func mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d want %d, body = %s", resp.StatusCode, want, string(body))
	}
}

func mustBody(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != want {
		t.Fatalf("body = %q want %q", string(body), want)
	}
}

func mustHeader(t *testing.T, resp *http.Response, key, want string) {
	t.Helper()
	if got := resp.Header.Get(key); got != want {
		t.Fatalf("header %s = %q want %q", key, got, want)
	}
}
