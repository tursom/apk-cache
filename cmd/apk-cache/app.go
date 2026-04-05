package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	internalconfig "github.com/tursom/apk-cache/internal/config"
	"github.com/tursom/apk-cache/internal/upstream"
	"github.com/tursom/apk-cache/utils"
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
	aptIndex               *APTIndexService
	proxyAdapter           *ProxyAdapter
	pipeline               *Pipeline
}

type HTTPClientFactory struct {
	timeout         time.Duration
	idleConnTimeout time.Duration
	maxIdleConns    int

	mu      sync.Mutex
	clients map[string]*http.Client
}

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
		aptIndex:               NewAPTIndexService(cfg.Cache.Root),
	}
	if err := app.aptIndex.LoadFromRoot(filepath.Join(cfg.Cache.Root, "apt")); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("load apt indexes: %v", err)
	}

	app.proxyAdapter = NewProxyAdapter(cfg.Proxy)
	adapters := []ProtocolAdapter{
		NewAPKAdapter(cfg.APK.Enabled),
		NewAPTAdapter(cfg.APT),
		app.proxyAdapter,
	}
	app.pipeline = NewPipeline(app, adapters)

	mux := http.NewServeMux()
	mux.HandleFunc("/_health", app.handleHealth)
	mux.Handle("/metrics", promhttp.HandlerFor(utils.Monitoring.GetRegistry(), promhttp.HandlerOpts{}))
	mux.Handle("/", app.pipeline)

	app.server = &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: mux,
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
	httpTransport, ok := transport.(*http.Transport)
	if !ok || httpTransport == nil {
		httpTransport = &http.Transport{}
	}
	httpTransport.ProxyConnectHeader = make(http.Header)
	httpTransport.MaxIdleConns = f.maxIdleConns
	httpTransport.IdleConnTimeout = f.idleConnTimeout

	client := &http.Client{
		Transport: httpTransport,
		Timeout:   f.timeout,
	}
	f.clients[proxyAddr] = client
	return client
}

func (f *HTTPClientFactory) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: f.timeout}
	return dialer.DialContext(ctx, network, address)
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		log.Printf("apk-cache listening on %s", a.cfg.Server.Listen)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.server.Shutdown(shutdownCtx)
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	state := "healthy"
	if a.cfg.APK.Enabled && a.apkUpstreams.GetServerCount() > 0 && a.apkUpstreams.GetHealthyCount() == 0 {
		status = http.StatusServiceUnavailable
		state = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(
		`{"status":"` + state + `","apk_upstreams":` + itoa(a.apkUpstreams.GetHealthyCount()) +
			`,"apk_upstreams_total":` + itoa(a.apkUpstreams.GetServerCount()) + `}`,
	))
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
