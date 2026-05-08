package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	internalconfig "github.com/tursom/apk-cache/internal/config"
	"github.com/tursom/apk-cache/internal/upstream"
	"github.com/tursom/apk-cache/utils"
)

const (
	routeHealth  = "/_health"
	routeMetrics = "/metrics"

	cacheHit       = "HIT"
	cacheMiss      = "MISS"
	cacheBypass    = "BYPASS"
	cacheMemoryHit = "MEMORY-HIT"
)

type App struct {
	cfg                    *internalconfig.Config
	server                 *http.Server
	httpClients            *HTTPClientFactory
	memoryCache            *MemoryCache
	memoryCacheMaxItemSize int64
	indexTTL               time.Duration
	packageTTL             time.Duration
	lockManager            *utils.FileLockManager
	apkUpstreams           *upstream.Manager
	apkFetcher             *upstream.DefaultFetcher
	apkIndex               *APKIndexService
	apkVerifier            *APKVerifier
	aptIndex               *APTIndexService
	proxyAdapter           *ProxyAdapter
	pipeline               *Pipeline

	bgWg       sync.WaitGroup
	connectSem chan struct{}
}

// HTTPClientFactory 按 proxy 地址复用 http.Client，避免重复创建 transport。
type HTTPClientFactory struct {
	timeout         time.Duration
	idleConnTimeout time.Duration
	maxIdleConns    int

	mu      sync.Mutex
	clients map[string]*http.Client
}

// NewApp 负责把配置组装成运行时依赖图。
// APK/APT 各自的索引服务都会在这里初始化，并尝试从已有缓存恢复内存态。
func NewApp(cfg *internalconfig.Config) (*App, error) {
	indexTTL, err := time.ParseDuration(cfg.Cache.IndexTTL)
	if err != nil {
		return nil, err
	}
	packageTTL, err := time.ParseDuration(cfg.Cache.PackageTTL)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.Cache.Root, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.Cache.DataRoot, 0o755); err != nil {
		return nil, err
	}

	clientFactory, err := NewHTTPClientFactory(cfg.Transport)
	if err != nil {
		return nil, err
	}

	var memoryCache *MemoryCache
	var maxMemoryItemSize int64
	if cfg.Cache.Memory.Enabled {
		maxMemorySize, err := utils.ParseSizeString(cfg.Cache.Memory.MaxSize)
		if err != nil {
			return nil, err
		}
		maxMemoryItemSize, err = utils.ParseSizeString(cfg.Cache.Memory.MaxItemSize)
		if err != nil {
			return nil, err
		}
		memoryTTL, err := time.ParseDuration(cfg.Cache.Memory.TTL)
		if err != nil {
			return nil, err
		}
		memoryCache = NewMemoryCache(maxMemorySize, cfg.Cache.Memory.MaxItems, memoryTTL)
	}

	apkUpstreams := upstream.NewManager()
	for _, candidate := range cfg.Upstreams {
		if candidate.Kind != "" && candidate.Kind != "apk" {
			continue
		}
		apkUpstreams.AddServer(upstream.NewServer(candidate.URL, candidate.Proxy, candidate.Name, 30*time.Second))
	}

	apkVerifier, err := NewAPKVerifier(cfg.APK.KeysDir)
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg:                    cfg,
		httpClients:            clientFactory,
		memoryCache:            memoryCache,
		memoryCacheMaxItemSize: maxMemoryItemSize,
		indexTTL:               indexTTL,
		packageTTL:             packageTTL,
		lockManager:            utils.NewFileLockManager(),
		apkUpstreams:           apkUpstreams,
		apkFetcher:             upstream.NewFetcher(apkUpstreams, clientFactory.Client),
		apkIndex:               NewAPKIndexService(cfg.Cache.Root),
		apkVerifier:            apkVerifier,
		aptIndex:               NewAPTIndexService(cfg.Cache.Root),
		connectSem:             make(chan struct{}, 500),
	}
	if err := app.apkIndex.LoadFromRoot(cfg.Cache.Root); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("load apk indexes", "err", err)
	}
	if err := app.aptIndex.LoadFromRoot(filepath.Join(cfg.Cache.Root, "apt")); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("load apt indexes", "err", err)
	}

	app.proxyAdapter = NewProxyAdapter(cfg.Proxy)
	adapters := []ProtocolAdapter{
		NewAPKAdapter(cfg.APK.Enabled),
		NewAPTAdapter(cfg.APT),
		app.proxyAdapter,
	}
	app.pipeline = NewPipeline(app, adapters)

	metricsHandler := promhttp.HandlerFor(utils.Monitoring.GetRegistry(), promhttp.HandlerOpts{})
	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		if r.Method == http.MethodConnect {
			app.pipeline.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case routeHealth:
			app.handleHealth(w, r)
		case routeMetrics:
			metricsHandler.ServeHTTP(w, r)
		default:
			app.pipeline.ServeHTTP(w, r)
		}
	})

	app.server = &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           timeoutExceptConnect(rootHandler, 120*time.Second),
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return app, nil
}

func NewHTTPClientFactory(cfg internalconfig.TransportConfig) (*HTTPClientFactory, error) {
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		return nil, err
	}
	idleConnTimeout, err := time.ParseDuration(cfg.IdleConnTimeout)
	if err != nil {
		return nil, err
	}
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 128
	}

	return &HTTPClientFactory{
		timeout:         timeout,
		idleConnTimeout: idleConnTimeout,
		maxIdleConns:    maxIdleConns,
		clients:         make(map[string]*http.Client),
	}, nil
}

func (f *HTTPClientFactory) Client(proxyAddr string) *http.Client {
	f.mu.Lock()
	defer f.mu.Unlock()

	if client, ok := f.clients[proxyAddr]; ok {
		return client
	}

	transport := upstream.CreateTransport(proxyAddr)
	transport.MaxIdleConns = f.maxIdleConns
	transport.IdleConnTimeout = f.idleConnTimeout

	client := &http.Client{
		Transport: transport,
		Timeout:   f.timeout,
	}
	f.clients[proxyAddr] = client
	return client
}

func (f *HTTPClientFactory) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f.DialProxyContext(ctx, "", network, address)
}

func (f *HTTPClientFactory) DialProxyContext(ctx context.Context, proxyAddr, network, address string) (net.Conn, error) {
	return upstream.DialContextViaProxy(ctx, proxyAddr, network, address, f.timeout)
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("apk-cache listening", "addr", a.cfg.Server.Listen)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	}

	slog.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "err", err)
	}

	if a.memoryCache != nil {
		a.memoryCache.Stop()
	}
	a.bgWg.Wait()

	return nil
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	healthyCount := a.apkUpstreams.GetHealthyCount()
	totalCount := a.apkUpstreams.GetServerCount()

	resp := map[string]interface{}{
		"apk_upstreams_total": totalCount,
	}

	if a.cfg.APK.Enabled {
		apkStatus := "healthy"
		apkDetails := strconv.Itoa(healthyCount) + "/" + strconv.Itoa(totalCount) + " servers up"
		if totalCount > 0 && healthyCount == 0 {
			apkStatus = "unhealthy"
			apkDetails = "no healthy APK upstream servers"
		}
		resp["apk_upstreams"] = map[string]string{
			"status":  apkStatus,
			"details": apkDetails,
		}
	}

	if a.memoryCache != nil {
		cur, max, items, _ := a.memoryCache.GetStats()
		resp["memory_cache"] = map[string]interface{}{
			"items": items,
			"size":  cur,
			"max":   max,
		}
	}

	diskStatus := "healthy"
	if _, err := os.Stat(a.cfg.Cache.Root); err != nil {
		diskStatus = "unhealthy"
	}
	resp["disk_cache"] = map[string]string{
		"status": diskStatus,
	}

	state := "healthy"
	if a.cfg.APK.Enabled && totalCount > 0 && healthyCount == 0 {
		state = "degraded"
	}
	if diskStatus == "unhealthy" {
		state = "degraded"
	}
	resp["status"] = state

	statusCode := http.StatusOK
	if state != "healthy" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("health encode", "err", err)
	}
}

// timeoutExceptConnect wraps h with a TimeoutHandler but lets CONNECT
// requests through directly so the underlying handler can hijack the
// connection.
func timeoutExceptConnect(h http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			h.ServeHTTP(w, r)
			return
		}
		http.TimeoutHandler(h, timeout, "request timed out").ServeHTTP(w, r)
	})
}
