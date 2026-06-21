package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	apkpkg "github.com/tursom/apk-cache/internal/apk"
	aptpkg "github.com/tursom/apk-cache/internal/apt"
	cachepkg "github.com/tursom/apk-cache/internal/cache"
	"github.com/tursom/apk-cache/internal/config"
	"github.com/tursom/apk-cache/internal/metrics"
	"github.com/tursom/apk-cache/internal/upstream"
)

const (
	HeaderCache       = "X-Cache"
	CacheHit          = "HIT"
	CacheMiss         = "MISS"
	CacheBypass       = "BYPASS"
	CacheMemoryHit    = "MEMORY-HIT"
	defaultConnectCap = 500
)

var (
	ErrUnsupported      = errors.New("unsupported request")
	ErrProxyDisabled    = errors.New("proxy adapter is disabled")
	ErrConnectDisabled  = errors.New("proxy CONNECT is disabled")
	ErrHostNotAllowed   = errors.New("proxy target host is not allowed")
	ErrTooManyConnects  = errors.New("too many concurrent CONNECT tunnels")
	ErrPathTraversal    = errors.New("path traversal is not allowed")
	ErrInvalidCachePath = errors.New("invalid cache path")
	ErrSoftCacheBypass  = errors.New("response may pass through but must not be cached")
)

type App struct {
	cfg *config.Config

	server    *http.Server
	metrics   *metrics.Metrics
	clients   *HTTPClientFactory
	mem       *cachepkg.Memory
	memMax    int64
	locks     *cachepkg.KeyLocks
	indexTTL  time.Duration
	pkgTTL    time.Duration
	bgWg      sync.WaitGroup
	connectCh chan struct{}

	apkUpstreams *upstream.Manager
	apkIndex     *apkpkg.Index
	apkVerifier  *apkpkg.Verifier
	aptIndex     *aptpkg.Index
}

type HTTPClientFactory struct {
	timeout         time.Duration
	idleConnTimeout time.Duration
	maxIdleConns    int

	mu      sync.Mutex
	clients map[string]*http.Client
}

func New(cfg *config.Config) (*App, error) {
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

	m := metrics.New()
	clients, err := NewHTTPClientFactory(cfg.Transport)
	if err != nil {
		return nil, err
	}

	var mem *cachepkg.Memory
	var maxItemSize int64
	if cfg.Cache.Memory.Enabled {
		maxSize, err := cachepkg.ParseSize(cfg.Cache.Memory.MaxSize)
		if err != nil {
			return nil, err
		}
		maxItemSize, err = cachepkg.ParseSize(cfg.Cache.Memory.MaxItemSize)
		if err != nil {
			return nil, err
		}
		ttl, err := time.ParseDuration(cfg.Cache.Memory.TTL)
		if err != nil {
			return nil, err
		}
		mem = cachepkg.NewMemory(maxSize, cfg.Cache.Memory.MaxItems, ttl, m)
	}

	apkManager := upstream.NewManager(clients)
	apkManager.SetMetricsHooks(func() { m.UpstreamRequests.Inc() }, func() { m.UpstreamFailovers.Inc() })
	for _, candidate := range cfg.Upstreams {
		kind := strings.ToLower(strings.TrimSpace(candidate.Kind))
		if kind != "" && kind != "apk" {
			continue
		}
		apkManager.Add(upstream.NewServer(candidate.URL, candidate.Proxy, candidate.Name))
	}

	verifier, err := apkpkg.NewVerifier(cfg.APK.KeysDir)
	if err != nil {
		return nil, err
	}

	a := &App{
		cfg:          cfg,
		metrics:      m,
		clients:      clients,
		mem:          mem,
		memMax:       maxItemSize,
		locks:        cachepkg.NewKeyLocks(),
		indexTTL:     indexTTL,
		pkgTTL:       packageTTL,
		connectCh:    make(chan struct{}, defaultConnectCap),
		apkUpstreams: apkManager,
		apkIndex:     apkpkg.NewIndex(cfg.Cache.Root),
		apkVerifier:  verifier,
		aptIndex:     aptpkg.NewIndex(cfg.Cache.Root),
	}
	if err := a.apkIndex.LoadFromRoot(cfg.Cache.Root); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("load apk indexes", "err", err)
	}
	if err := a.aptIndex.LoadFromRoot(filepath.Join(cfg.Cache.Root, "apt")); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("load apt indexes", "err", err)
	}

	a.server = &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           http.HandlerFunc(a.serveHTTP),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return a, nil
}

func NewHTTPClientFactory(cfg config.TransportConfig) (*HTTPClientFactory, error) {
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		return nil, err
	}
	idleTimeout, err := time.ParseDuration(cfg.IdleConnTimeout)
	if err != nil {
		return nil, err
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 128
	}
	return &HTTPClientFactory{
		timeout:         timeout,
		idleConnTimeout: idleTimeout,
		maxIdleConns:    maxIdle,
		clients:         make(map[string]*http.Client),
	}, nil
}

func (f *HTTPClientFactory) Client(proxyAddr string) *http.Client {
	f.mu.Lock()
	defer f.mu.Unlock()
	if client := f.clients[proxyAddr]; client != nil {
		return client
	}
	transport := upstream.CreateTransport(proxyAddr)
	transport.MaxIdleConns = f.maxIdleConns
	transport.IdleConnTimeout = f.idleConnTimeout
	client := &http.Client{Transport: transport, Timeout: f.timeout}
	f.clients[proxyAddr] = client
	return client
}

func (f *HTTPClientFactory) DialProxy(ctx context.Context, proxyAddr, network, address string) (net.Conn, error) {
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
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("server shutdown", "err", err)
	}
	if a.mem != nil {
		a.mem.Stop()
	}
	a.bgWg.Wait()
	return nil
}

func (a *App) Handler() http.Handler {
	return a.server.Handler
}

func (a *App) serveHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}()

	switch {
	case r.URL.Path == "/_health" && r.Method == http.MethodGet:
		a.handleHealth(w)
	case r.URL.Path == "/metrics" && r.Method == http.MethodGet:
		promhttp.HandlerFor(a.metrics.Registry(), promhttp.HandlerOpts{}).ServeHTTP(w, r)
	case r.Method == http.MethodConnect:
		if err := a.handleConnect(w, r); err != nil {
			writeError(w, err)
		}
	default:
		if err := a.routeHTTP(w, r); err != nil {
			writeError(w, err)
		}
	}
}

func (a *App) routeHTTP(w http.ResponseWriter, r *http.Request) error {
	path := requestPath(r)
	switch {
	case a.cfg.APK.Enabled && isAPKRequest(path):
		return a.handleAPK(w, r, path)
	case a.cfg.APT.Enabled && isAPTRequest(r, path):
		return a.handleAPT(w, r)
	case isProxyRequest(r):
		return a.handleProxyHTTP(w, r)
	default:
		return ErrUnsupported
	}
}

type cacheRequest struct {
	cachePath     string
	cacheClass    string
	storeInMemory bool
	fetch         func(context.Context) (*http.Response, error)
	validateCache func(context.Context, string) error
	validateFetch func(context.Context, string, string) error
	commit        func(context.Context, string) error
}

func (a *App) serveCached(w http.ResponseWriter, r *http.Request, req cacheRequest) error {
	ttl := a.pkgTTL
	if req.cacheClass == "index" {
		ttl = a.indexTTL
	}
	if a.tryMemory(w, req.cachePath) {
		return nil
	}
	if a.tryDisk(w, r, req, ttl) {
		return nil
	}

	unlock := a.locks.Lock(req.cachePath)
	defer unlock()

	if a.tryMemory(w, req.cachePath) {
		return nil
	}
	if a.tryDisk(w, r, req, ttl) {
		return nil
	}

	resp, err := req.fetch(r.Context())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		a.writeResponse(w, resp, CacheBypass)
		return nil
	}
	return a.fetchAndStore(r.Context(), w, resp, req)
}

func (a *App) tryMemory(w http.ResponseWriter, cachePath string) bool {
	if a.mem == nil {
		return false
	}
	item, ok := a.mem.Get(cachePath)
	if !ok {
		return false
	}
	copyEndToEndHeaders(w.Header(), item.Headers)
	w.Header().Set(HeaderCache, CacheMemoryHit)
	w.Header().Set("Content-Length", strconv.Itoa(len(item.Data)))
	w.WriteHeader(item.StatusCode)
	if _, err := w.Write(item.Data); err == nil {
		a.metrics.RecordCacheHit(int64(len(item.Data)))
	}
	return true
}

func (a *App) tryDisk(w http.ResponseWriter, r *http.Request, req cacheRequest, ttl time.Duration) bool {
	info, err := os.Stat(req.cachePath)
	if err != nil || info.IsDir() {
		return false
	}
	if ttl > 0 && time.Since(info.ModTime()) > ttl {
		_ = os.Remove(req.cachePath)
		if a.mem != nil {
			a.mem.Delete(req.cachePath)
		}
		return false
	}
	if req.validateCache != nil {
		if err := req.validateCache(r.Context(), req.cachePath); err != nil {
			a.metrics.ValidationFailures.Inc()
			_ = os.Remove(req.cachePath)
			if a.mem != nil {
				a.mem.Delete(req.cachePath)
			}
			return false
		}
	}

	file, err := os.Open(req.cachePath)
	if err != nil {
		return false
	}
	defer file.Close()

	w.Header().Set(HeaderCache, CacheHit)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeContent(w, r, filepath.Base(req.cachePath), info.ModTime(), file)
	a.metrics.RecordCacheHit(info.Size())

	if req.storeInMemory {
		a.cacheDiskFileInMemory(req.cachePath, info, http.Header{
			"Content-Length": []string{strconv.FormatInt(info.Size(), 10)},
			"Last-Modified":  []string{info.ModTime().UTC().Format(http.TimeFormat)},
		}, http.StatusOK)
	}
	return true
}

func (a *App) fetchAndStore(ctx context.Context, w http.ResponseWriter, resp *http.Response, req cacheRequest) error {
	if err := os.MkdirAll(filepath.Dir(req.cachePath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(req.cachePath), "cache-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()

	copyEndToEndHeaders(w.Header(), resp.Header)
	w.Header().Set(HeaderCache, CacheMiss)
	w.WriteHeader(resp.StatusCode)
	flush := func() {}
	if flusher, ok := w.(http.Flusher); ok {
		flush = flusher.Flush
	}

	result, readErr := streamToClientAndCache(resp.Body, w, tmp, flush, a.metrics)
	a.metrics.RecordResponseBytes(result.responded)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		if a.mem != nil {
			a.mem.Delete(req.cachePath)
		}
		slog.Warn("upstream stream ended with error", "path", req.cachePath, "err", readErr)
		return nil
	}
	if result.cacheFailed {
		if a.mem != nil {
			a.mem.Delete(req.cachePath)
		}
		return nil
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	closed = true

	if req.validateFetch != nil {
		if err := req.validateFetch(ctx, req.cachePath, tmpName); err != nil {
			a.metrics.ValidationFailures.Inc()
			if errors.Is(err, ErrSoftCacheBypass) {
				a.metrics.APKBypassResponses.Inc()
			}
			if a.mem != nil {
				a.mem.Delete(req.cachePath)
			}
			return nil
		}
	}
	if err := os.Rename(tmpName, req.cachePath); err != nil {
		return err
	}
	if req.commit != nil {
		if err := req.commit(ctx, req.cachePath); err != nil {
			_ = os.Remove(req.cachePath)
			if a.mem != nil {
				a.mem.Delete(req.cachePath)
			}
			return err
		}
	}
	a.metrics.RecordCacheMiss(result.downloaded)

	if req.storeInMemory {
		if info, err := os.Stat(req.cachePath); err == nil {
			headers := resp.Header.Clone()
			headers.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
			a.cacheDiskFileInMemory(req.cachePath, info, headers, resp.StatusCode)
		}
	}
	return nil
}

func (a *App) cacheDiskFileInMemory(cachePath string, info os.FileInfo, headers http.Header, status int) {
	if a.mem == nil {
		return
	}
	if a.memMax > 0 && info.Size() > a.memMax {
		return
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return
	}
	a.mem.Set(cachePath, data, headers, status, info.ModTime())
}

func (a *App) writeResponse(w http.ResponseWriter, resp *http.Response, cacheHeader string) {
	copyEndToEndHeaders(w.Header(), resp.Header)
	w.Header().Set(HeaderCache, cacheHeader)
	w.WriteHeader(resp.StatusCode)
	if n, err := io.Copy(w, resp.Body); err == nil {
		a.metrics.RecordResponseBytes(n)
	}
}

type streamResult struct {
	downloaded   int64
	responded    int64
	cacheFailed  bool
	clientFailed bool
}

func streamToClientAndCache(src io.Reader, client io.Writer, cache io.Writer, flush func(), m *metrics.Metrics) (streamResult, error) {
	var result streamResult
	buffer := make([]byte, 32*1024)
	clientEnabled := client != nil
	cacheEnabled := cache != nil
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			result.downloaded += int64(n)
			if m != nil {
				m.DownloadBytes.Add(float64(n))
			}
			if cacheEnabled {
				if _, err := cache.Write(chunk); err != nil {
					cacheEnabled = false
					result.cacheFailed = true
				}
			}
			if clientEnabled {
				written, err := client.Write(chunk)
				result.responded += int64(written)
				if err != nil {
					clientEnabled = false
					result.clientFailed = true
				} else {
					flush()
				}
			}
		}
		if readErr != nil {
			return result, readErr
		}
	}
}

func (a *App) handleAPK(w http.ResponseWriter, r *http.Request, path string) error {
	key, err := safeCacheKey(path)
	if err != nil {
		return err
	}
	cachePath := filepath.Join(a.cfg.Cache.Root, key)
	cacheClass := "package"
	storeMemory := true
	if apkpkg.IsIndexFile(path) {
		cacheClass = "index"
		storeMemory = false
	}

	req := cacheRequest{
		cachePath:     cachePath,
		cacheClass:    cacheClass,
		storeInMemory: storeMemory,
		fetch: func(ctx context.Context) (*http.Response, error) {
			return a.apkUpstreams.Fetch(ctx, path, r.Header)
		},
		validateCache: func(_ context.Context, cachePath string) error {
			return a.validateAPK(cachePath, cachePath, cacheClass, false)
		},
		validateFetch: func(_ context.Context, cachePath, filePath string) error {
			return a.validateAPK(cachePath, filePath, cacheClass, true)
		},
		commit: func(_ context.Context, cachePath string) error {
			if cacheClass == "index" {
				return a.apkIndex.LoadFile(cachePath)
			}
			return nil
		},
	}
	return a.serveCached(w, r, req)
}

func (a *App) validateAPK(cachePath, filePath, cacheClass string, fetched bool) error {
	if cacheClass == "index" {
		if !a.cfg.APK.VerifySignature {
			return nil
		}
		if err := a.apkVerifier.ValidateArchive(filePath); err != nil {
			a.metrics.APKSignFailures.Inc()
			if fetched {
				return fmt.Errorf("%w: %v", ErrSoftCacheBypass, err)
			}
			return err
		}
		return nil
	}

	if a.cfg.APK.VerifyHash {
		err := a.apkIndex.ValidatePackage(cachePath, filePath)
		switch {
		case err == nil:
		case errors.Is(err, apkpkg.ErrIndexUnavailable):
		default:
			a.metrics.APKHashFailures.Inc()
			return err
		}
	}
	if a.cfg.APK.VerifySignature {
		if err := a.apkVerifier.ValidateArchive(filePath); err != nil {
			a.metrics.APKSignFailures.Inc()
			if fetched {
				return fmt.Errorf("%w: %v", ErrSoftCacheBypass, err)
			}
			return err
		}
	}
	return nil
}

func (a *App) handleAPT(w http.ResponseWriter, r *http.Request) error {
	target, err := forwardURL(r)
	if err != nil {
		return err
	}
	keyPath, err := safeCacheKey(target.Path)
	if err != nil {
		return err
	}
	cachePath := aptpkg.CachePath(a.cfg.Cache.Root, target.Host, keyPath)
	cacheClass := "package"
	storeMemory := false
	isIndexRequest := aptpkg.IsIndexFile(target.Path) || aptpkg.IsHashRequest(target.Path)
	if isIndexRequest {
		cacheClass = "index"
		storeMemory = true
	}

	req := cacheRequest{
		cachePath:     cachePath,
		cacheClass:    cacheClass,
		storeInMemory: storeMemory,
		fetch: func(ctx context.Context) (*http.Response, error) {
			upstreamReq, err := http.NewRequestWithContext(ctx, r.Method, target.String(), nil)
			if err != nil {
				return nil, err
			}
			copyEndToEndHeaders(upstreamReq.Header, r.Header)
			upstreamReq.Host = target.Host
			a.metrics.UpstreamRequests.Inc()
			return a.clients.Client(a.cfg.Proxy.UpstreamProxy).Do(upstreamReq)
		},
		validateCache: func(_ context.Context, cachePath string) error {
			return a.validateAPT(cachePath, cachePath, target.Path)
		},
		validateFetch: func(_ context.Context, cachePath, filePath string) error {
			return a.validateAPT(cachePath, filePath, target.Path)
		},
		commit: func(_ context.Context, cachePath string) error {
			if !isIndexRequest {
				return nil
			}
			if a.cfg.APT.LoadIndexAsync {
				a.bgWg.Add(1)
				go func() {
					defer a.bgWg.Done()
					if err := a.loadAPTIndex(cachePath, target.Path); err != nil {
						slog.Warn("load apt index", "path", cachePath, "err", err)
					}
				}()
				return nil
			}
			return a.loadAPTIndex(cachePath, target.Path)
		},
	}
	return a.serveCached(w, r, req)
}

func (a *App) loadAPTIndex(cachePath, requestPath string) error {
	if aptpkg.IsHashRequest(requestPath) {
		return a.aptIndex.LoadFileByHash(cachePath, requestPath)
	}
	return a.aptIndex.LoadFile(cachePath)
}

func (a *App) validateAPT(cachePath, filePath, requestPath string) error {
	if !a.cfg.APT.VerifyHash {
		return nil
	}
	if aptpkg.IsHashRequest(requestPath) {
		return a.aptIndex.ValidateByHash(filePath, requestPath)
	}
	return a.aptIndex.ValidateFile(cachePath, filePath)
}

func (a *App) handleProxyHTTP(w http.ResponseWriter, r *http.Request) error {
	if !a.cfg.Proxy.Enabled {
		return ErrProxyDisabled
	}
	if err := a.validateAllowedHost(r); err != nil {
		return err
	}
	target, err := forwardURL(r)
	if err != nil {
		return err
	}

	if a.cfg.Proxy.CacheNonPackage && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		keyPath, err := safeCacheKey(target.Path)
		if err != nil {
			return err
		}
		scheme := target.Scheme
		if scheme == "" {
			scheme = "http"
		}
		cachePath := filepath.Join(a.cfg.Cache.Root, "proxy", scheme, sanitizeHost(target.Host), keyPath)
		return a.serveCached(w, r, cacheRequest{
			cachePath:     cachePath,
			cacheClass:    "package",
			storeInMemory: false,
			fetch: func(ctx context.Context) (*http.Response, error) {
				return a.fetchProxyHTTP(ctx, r, target)
			},
		})
	}

	resp, err := a.fetchProxyHTTP(r.Context(), r, target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	a.writeResponse(w, resp, CacheBypass)
	return nil
}

func (a *App) fetchProxyHTTP(ctx context.Context, r *http.Request, target *url.URL) (*http.Response, error) {
	upstreamReq, err := http.NewRequestWithContext(ctx, r.Method, target.String(), r.Body)
	if err != nil {
		return nil, err
	}
	copyEndToEndHeaders(upstreamReq.Header, r.Header)
	upstreamReq.Host = target.Host
	a.metrics.UpstreamRequests.Inc()
	return a.clients.Client(a.cfg.Proxy.UpstreamProxy).Do(upstreamReq)
}

func (a *App) handleConnect(w http.ResponseWriter, r *http.Request) error {
	if !a.cfg.Proxy.Enabled {
		return ErrProxyDisabled
	}
	if !a.cfg.Proxy.AllowConnect {
		return ErrConnectDisabled
	}
	if err := a.validateAllowedHost(r); err != nil {
		return err
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return errors.New("response writer does not support hijacking")
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		return err
	}

	select {
	case a.connectCh <- struct{}{}:
	default:
		_ = clientConn.Close()
		return ErrTooManyConnects
	}

	target := ensurePort(r.Host, "443")
	target = strings.ReplaceAll(strings.ReplaceAll(target, "\r", ""), "\n", "")
	targetConn, err := a.clients.DialProxy(r.Context(), a.cfg.Proxy.UpstreamProxy, "tcp", target)
	if err != nil {
		_ = clientConn.Close()
		<-a.connectCh
		return err
	}
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\nProxy-Agent: apk-cache\r\n\r\n")); err != nil {
		_ = clientConn.Close()
		_ = targetConn.Close()
		<-a.connectCh
		return err
	}

	go func() {
		defer func() { <-a.connectCh }()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			copyAndClose(targetConn, clientConn)
		}()
		go func() {
			defer wg.Done()
			copyAndClose(clientConn, targetConn)
		}()
		wg.Wait()
	}()
	return nil
}

func (a *App) validateAllowedHost(r *http.Request) error {
	if len(a.cfg.Proxy.AllowedHosts) == 0 {
		return nil
	}
	host := r.Host
	if host == "" && r.URL != nil {
		host = r.URL.Host
	}
	host = stripPort(host)
	for _, allowed := range a.cfg.Proxy.AllowedHosts {
		if host == stripPort(allowed) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrHostNotAllowed, host)
}

func (a *App) handleHealth(w http.ResponseWriter) {
	resp := map[string]any{
		"status":              "healthy",
		"apk_upstreams_total": a.apkUpstreams.Count(),
		"apk_upstreams": map[string]any{
			"healthy": a.apkUpstreams.HealthyCount(),
			"total":   a.apkUpstreams.Count(),
		},
	}
	if a.mem != nil {
		current, max, items := a.mem.Stats()
		resp["memory_cache"] = map[string]any{
			"items": items,
			"size":  current,
			"max":   max,
		}
	}
	diskStatus := "healthy"
	if _, err := os.Stat(a.cfg.Cache.Root); err != nil {
		diskStatus = "unhealthy"
		resp["status"] = "degraded"
	}
	if a.cfg.APK.Enabled && a.apkUpstreams.Count() > 0 && a.apkUpstreams.HealthyCount() == 0 {
		resp["status"] = "degraded"
	}
	resp["disk_cache"] = map[string]string{"status": diskStatus}

	statusCode := http.StatusOK
	if resp["status"] != "healthy" {
		statusCode = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Warn("encode health response", "err", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnsupported), errors.Is(err, ErrInvalidCachePath), errors.Is(err, ErrPathTraversal):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrConnectDisabled), errors.Is(err, ErrHostNotAllowed):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrProxyDisabled), errors.Is(err, ErrTooManyConnects):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	default:
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

func isProxyRequest(r *http.Request) bool {
	return r.URL.IsAbs() || strings.HasPrefix(r.RequestURI, "http://") || strings.HasPrefix(r.RequestURI, "https://")
}

func isAPKRequest(path string) bool {
	return apkpkg.IsPackageFile(path) || apkpkg.IsIndexFile(path) || strings.Contains(path, "/alpine/")
}

func isAPTRequest(r *http.Request, path string) bool {
	if r.Method == http.MethodConnect {
		return false
	}
	if strings.HasSuffix(path, ".deb") || aptpkg.IsIndexFile(path) || aptpkg.IsHashRequest(path) {
		return true
	}
	return strings.Contains(path, "/dists/") || strings.Contains(path, "/pool/") ||
		strings.HasPrefix(path, "/debian/") || strings.HasPrefix(path, "/ubuntu/")
}

func requestPath(r *http.Request) string {
	if r.URL == nil {
		return ""
	}
	if r.URL.Path != "" {
		return r.URL.Path
	}
	if r.URL.IsAbs() {
		return r.URL.EscapedPath()
	}
	return ""
}

func forwardURL(r *http.Request) (*url.URL, error) {
	if r.URL != nil && r.URL.IsAbs() {
		clone := *r.URL
		return &clone, nil
	}
	if strings.HasPrefix(r.RequestURI, "http://") || strings.HasPrefix(r.RequestURI, "https://") {
		return url.Parse(r.RequestURI)
	}
	if r.Host == "" || r.URL == nil {
		return nil, errors.New("request host is empty")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return &url.URL{
		Scheme:   scheme,
		Host:     r.Host,
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
	}, nil
}

func safeCacheKey(path string) (string, error) {
	if path == "" {
		return "", ErrInvalidCachePath
	}
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, path)
	}
	clean := filepath.Clean("/" + path)
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	if clean == "." || clean == "" {
		return "", ErrInvalidCachePath
	}
	return clean, nil
}

func sanitizeHost(host string) string {
	return strings.NewReplacer(":", "_", "/", "_", "\\", "_", "[", "_", "]", "_").Replace(host)
}

func stripPort(host string) string {
	if host == "" {
		return host
	}
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

func ensurePort(host, defaultPort string) string {
	if strings.Contains(host, ":") {
		return host
	}
	return host + ":" + defaultPort
}

func copyAndClose(dst net.Conn, src net.Conn) {
	defer dst.Close()
	_, _ = io.Copy(dst, src)
}

func copyEndToEndHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate",
		"proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
