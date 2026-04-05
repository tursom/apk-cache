package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	internalconfig "github.com/tursom/apk-cache/internal/config"
	"github.com/tursom/apk-cache/utils"
	aptutil "github.com/tursom/apk-cache/utils/apt"
)

var (
	ErrProxyDisabled       = errors.New("proxy adapter is disabled")
	ErrConnectNotAllowed   = errors.New("proxy CONNECT is disabled")
	ErrCacheCorrupted      = errors.New("cache validation failed")
	ErrAPKHashMismatch     = errors.New("apk hash mismatch")
	ErrAPKSignatureInvalid = errors.New("apk signature invalid")
	ErrAPKUnsigned         = errors.New("apk unsigned")
	ErrAPKIndexUnavailable = errors.New("apk index unavailable")
)

// CacheBypassError 表示“这次响应可以返回给客户端，但不能写入缓存”。
// APK 签名校验失败时会用它把控制权交回 pipeline，让 pipeline 改走透传分支。
type CacheBypassError struct {
	Err error
}

func (e *CacheBypassError) Error() string {
	return e.Err.Error()
}

func (e *CacheBypassError) Unwrap() error {
	return e.Err
}

// shouldBypassCache 用于区分致命校验错误和“允许透传、不允许缓存”的软失败。
func shouldBypassCache(err error) bool {
	var bypassErr *CacheBypassError
	return errors.As(err, &bypassErr)
}

// NormalizedRequest 是适配器对原始请求的统一表示。
// pipeline 只消费这个结构，从而避免把协议分支散落在主流程中。
type NormalizedRequest struct {
	AdapterName  string
	Request      *http.Request
	TargetURL    *url.URL
	UpstreamPath string
	PackageType  utils.PackageType
	CacheClass   string
	Cacheable    bool
}

// CacheDecision 描述当前请求的缓存策略。
type CacheDecision struct {
	Enabled       bool
	TTL           time.Duration
	StoreInMemory bool
}

// ProtocolAdapter 把 APK、APT 和通用代理接入统一流水线。
type ProtocolAdapter interface {
	Name() string
	Match(*http.Request) bool
	Normalize(*http.Request) (*NormalizedRequest, error)
	CachePolicy(*NormalizedRequest) CacheDecision
	CacheKey(*NormalizedRequest) (string, error)
	ValidateCached(context.Context, *App, *NormalizedRequest, string) error
	ValidateFetched(context.Context, *App, *NormalizedRequest, string, string) error
	CommitStored(context.Context, *App, *NormalizedRequest, string) error
	Fetch(context.Context, *App, *NormalizedRequest) (*http.Response, error)
}

// ConnectHandler 是支持 CONNECT 的适配器可选实现。
type ConnectHandler interface {
	HandleConnect(context.Context, *App, http.ResponseWriter, *http.Request, *NormalizedRequest) error
}

type APKAdapter struct {
	enabled bool
}

type APTAdapter struct {
	cfg internalconfig.APTConfig
}

type ProxyAdapter struct {
	cfg internalconfig.ProxyConfig
}

func NewAPKAdapter(enabled bool) *APKAdapter {
	return &APKAdapter{enabled: enabled}
}

func NewAPTAdapter(cfg internalconfig.APTConfig) *APTAdapter {
	return &APTAdapter{cfg: cfg}
}

func NewProxyAdapter(cfg internalconfig.ProxyConfig) *ProxyAdapter {
	return &ProxyAdapter{cfg: cfg}
}

func (a *APKAdapter) Name() string { return "apk" }

func (a *APKAdapter) Match(r *http.Request) bool {
	if !a.enabled || r.Method == http.MethodConnect {
		return false
	}
	return utils.DetectPackageTypeFast(requestPath(r)) == utils.PackageTypeAPK
}

// APK 请求的路径本身就是回源路径和缓存键的主要来源，因此归一化逻辑比较直接。
func (a *APKAdapter) Normalize(r *http.Request) (*NormalizedRequest, error) {
	path := requestPath(r)
	if path == "" || path[0] != '/' {
		return nil, fmt.Errorf("invalid apk path: %q", path)
	}
	cacheClass := "package"
	if utils.IsIndexFile(path) {
		cacheClass = "index"
	}
	return &NormalizedRequest{
		AdapterName:  a.Name(),
		Request:      r,
		UpstreamPath: path,
		PackageType:  utils.PackageTypeAPK,
		CacheClass:   cacheClass,
		Cacheable:    true,
	}, nil
}

// APK 包允许进入内存缓存，APKINDEX 只落盘不进内存。
func (a *APKAdapter) CachePolicy(req *NormalizedRequest) CacheDecision {
	return CacheDecision{
		Enabled:       true,
		StoreInMemory: req.CacheClass != "index",
	}
}

func (a *APKAdapter) CacheKey(req *NormalizedRequest) (string, error) {
	return safeJoinPath(req.UpstreamPath)
}

// ValidateCached 校验磁盘中已经存在的 APK/APKINDEX，失败时上层会删除缓存并回源。
func (a *APKAdapter) ValidateCached(_ context.Context, app *App, req *NormalizedRequest, cachePath string) error {
	if req.CacheClass == "index" {
		if !app.cfg.APK.VerifySignature {
			return nil
		}
		err := app.apkVerifier.ValidateIndexSignature(cachePath)
		if err != nil {
			utils.Monitoring.RecordAPKSignatureFailure()
		}
		return err
	}

	if app.cfg.APK.VerifyHash {
		err := app.apkIndex.ValidatePackage(cachePath)
		switch {
		case err == nil:
		case errors.Is(err, ErrAPKIndexUnavailable):
		default:
			utils.Monitoring.RecordAPKHashFailure()
			return err
		}
	}

	if app.cfg.APK.VerifySignature {
		err := app.apkVerifier.ValidatePackageSignature(cachePath)
		if err != nil {
			utils.Monitoring.RecordAPKSignatureFailure()
		}
		return err
	}

	return nil
}

// ValidateFetched 校验本次刚下载完成的 APK/APKINDEX。
// 哈希失败是致命错误；签名失败则包装为 CacheBypassError，让上层改走“透传但不缓存”。
func (a *APKAdapter) ValidateFetched(_ context.Context, app *App, req *NormalizedRequest, cachePath string, filePath string) error {
	if req.CacheClass == "index" {
		if app.cfg.APK.VerifySignature {
			if err := app.apkVerifier.ValidateIndexSignature(filePath); err != nil {
				utils.Monitoring.RecordAPKSignatureFailure()
				return &CacheBypassError{Err: err}
			}
		}
		return nil
	}

	if app.cfg.APK.VerifyHash {
		err := app.apkIndex.ValidatePackageFile(cachePath, filePath)
		switch {
		case err == nil:
		case errors.Is(err, ErrAPKIndexUnavailable):
		default:
			utils.Monitoring.RecordAPKHashFailure()
			return err
		}
	}

	if app.cfg.APK.VerifySignature {
		if err := app.apkVerifier.ValidatePackageSignature(filePath); err != nil {
			utils.Monitoring.RecordAPKSignatureFailure()
			return &CacheBypassError{Err: err}
		}
	}

	return nil
}

func (a *APKAdapter) CommitStored(_ context.Context, app *App, req *NormalizedRequest, cachePath string) error {
	if req.CacheClass == "index" {
		return app.apkIndex.LoadFile(cachePath)
	}
	return nil
}

// APK 请求通过 apkFetcher 使用配置中的 APK upstream 与 failover 逻辑。
func (a *APKAdapter) Fetch(ctx context.Context, app *App, req *NormalizedRequest) (*http.Response, error) {
	return app.apkFetcher.Fetch(req.UpstreamPath, func(upstreamReq *http.Request) {
		upstreamReq = upstreamReq.WithContext(ctx)
		copyEndToEndHeaders(upstreamReq.Header, req.Request.Header)
	})
}

func (a *APTAdapter) Name() string { return "apt" }

func (a *APTAdapter) Match(r *http.Request) bool {
	if !a.cfg.Enabled || r.Method == http.MethodConnect {
		return false
	}
	return utils.DetectPackageTypeFast(requestPath(r)) == utils.PackageTypeAPT
}

func (a *APTAdapter) Normalize(r *http.Request) (*NormalizedRequest, error) {
	targetURL, err := parseForwardURL(r)
	if err != nil {
		return nil, err
	}
	cacheClass := "package"
	if utils.IsIndexFile(targetURL.Path) {
		cacheClass = "index"
	}
	return &NormalizedRequest{
		AdapterName: a.Name(),
		Request:     r,
		TargetURL:   targetURL,
		PackageType: utils.PackageTypeAPT,
		CacheClass:  cacheClass,
		Cacheable:   true,
	}, nil
}

func (a *APTAdapter) CachePolicy(req *NormalizedRequest) CacheDecision {
	return CacheDecision{
		Enabled:       true,
		StoreInMemory: req.CacheClass == "index",
	}
}

func (a *APTAdapter) CacheKey(req *NormalizedRequest) (string, error) {
	host := req.TargetURL.Host
	if host == "" {
		host = req.Request.Host
	}
	return aptutil.GetAPTCacheFilePath("", host, req.TargetURL.Path), nil
}

func (a *APTAdapter) ValidateCached(ctx context.Context, app *App, req *NormalizedRequest, cachePath string) error {
	if !a.cfg.VerifyHash {
		return nil
	}
	if utils.IsHashRequest(req.TargetURL.Path) {
		return app.aptIndex.ValidateByHash(cachePath, req.TargetURL.Path)
	}
	if strings.HasSuffix(cachePath, ".deb") {
		return app.aptIndex.ValidateDeb(cachePath)
	}
	return nil
}

func (a *APTAdapter) ValidateFetched(_ context.Context, app *App, req *NormalizedRequest, cachePath string, filePath string) error {
	if !a.cfg.VerifyHash {
		return nil
	}
	if utils.IsHashRequest(req.TargetURL.Path) {
		return app.aptIndex.ValidateByHash(filePath, req.TargetURL.Path)
	}
	if strings.HasSuffix(cachePath, ".deb") {
		return app.aptIndex.ValidateDebFile(cachePath, filePath)
	}
	return nil
}

func (a *APTAdapter) CommitStored(_ context.Context, app *App, req *NormalizedRequest, cachePath string) error {
	if req.CacheClass != "index" {
		return nil
	}
	if a.cfg.LoadIndexAsync {
		go func() {
			if err := app.aptIndex.LoadFile(cachePath); err != nil {
				log.Printf("load apt index %s: %v", cachePath, err)
			}
		}()
		return nil
	}
	return app.aptIndex.LoadFile(cachePath)
}

func (a *APTAdapter) Fetch(ctx context.Context, app *App, req *NormalizedRequest) (*http.Response, error) {
	upstreamReq, err := http.NewRequestWithContext(ctx, req.Request.Method, req.TargetURL.String(), nil)
	if err != nil {
		return nil, err
	}
	copyEndToEndHeaders(upstreamReq.Header, req.Request.Header)
	upstreamReq.Host = req.TargetURL.Host
	utils.Monitoring.RecordUpstreamRequest()
	return app.httpClients.Client("").Do(upstreamReq)
}

func (a *ProxyAdapter) Name() string { return "proxy" }

func (a *ProxyAdapter) Match(r *http.Request) bool {
	return r.Method == http.MethodConnect || r.URL.IsAbs() || looksLikeAbsoluteRequest(r)
}

func (a *ProxyAdapter) Normalize(r *http.Request) (*NormalizedRequest, error) {
	targetURL, err := parseForwardURL(r)
	if err != nil && r.Method != http.MethodConnect {
		return nil, err
	}
	return &NormalizedRequest{
		AdapterName: a.Name(),
		Request:     r,
		TargetURL:   targetURL,
		CacheClass:  "bypass",
		Cacheable:   a.cfg.CacheNonPackage,
	}, nil
}

func (a *ProxyAdapter) CachePolicy(*NormalizedRequest) CacheDecision {
	return CacheDecision{
		Enabled:       a.cfg.CacheNonPackage,
		TTL:           0,
		StoreInMemory: false,
	}
}

func (a *ProxyAdapter) CacheKey(req *NormalizedRequest) (string, error) {
	if req.TargetURL == nil {
		return "", ErrProxyDisabled
	}
	return filepath.Join("proxy", req.TargetURL.Host, req.TargetURL.Path), nil
}

func (a *ProxyAdapter) ValidateCached(context.Context, *App, *NormalizedRequest, string) error {
	return nil
}

func (a *ProxyAdapter) ValidateFetched(context.Context, *App, *NormalizedRequest, string, string) error {
	return nil
}

func (a *ProxyAdapter) CommitStored(context.Context, *App, *NormalizedRequest, string) error {
	return nil
}

func (a *ProxyAdapter) Fetch(ctx context.Context, app *App, req *NormalizedRequest) (*http.Response, error) {
	if !a.cfg.Enabled {
		return nil, ErrProxyDisabled
	}
	if err := a.validateAllowedHost(req.Request); err != nil {
		return nil, err
	}
	upstreamReq, err := http.NewRequestWithContext(ctx, req.Request.Method, req.TargetURL.String(), req.Request.Body)
	if err != nil {
		return nil, err
	}
	copyEndToEndHeaders(upstreamReq.Header, req.Request.Header)
	upstreamReq.Host = req.TargetURL.Host
	utils.Monitoring.RecordUpstreamRequest()
	return app.httpClients.Client("").Do(upstreamReq)
}

func (a *ProxyAdapter) HandleConnect(ctx context.Context, app *App, w http.ResponseWriter, r *http.Request, _ *NormalizedRequest) error {
	if !a.cfg.Enabled {
		return ErrProxyDisabled
	}
	if !a.cfg.AllowConnect {
		return ErrConnectNotAllowed
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

	targetConn, err := app.httpClients.DialContext(ctx, "tcp", ensurePort(r.Host, "443"))
	if err != nil {
		clientConn.Close()
		return err
	}

	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\nProxy-Agent: apk-cache\r\n\r\n")); err != nil {
		clientConn.Close()
		targetConn.Close()
		return err
	}

	go tunnelCopy(targetConn, clientConn)
	go tunnelCopy(clientConn, targetConn)
	return nil
}

func (a *ProxyAdapter) validateAllowedHost(r *http.Request) error {
	if len(a.cfg.AllowedHosts) == 0 {
		return nil
	}

	host := r.Host
	if host == "" && r.URL != nil {
		host = r.URL.Host
	}
	host = stripPort(host)
	for _, allowed := range a.cfg.AllowedHosts {
		if host == stripPort(allowed) {
			return nil
		}
	}
	return fmt.Errorf("host %q is not allowed", host)
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

func parseForwardURL(r *http.Request) (*url.URL, error) {
	if r.Method == http.MethodConnect {
		return &url.URL{Scheme: "https", Host: ensurePort(r.Host, "443")}, nil
	}
	if r.URL != nil && r.URL.IsAbs() {
		return cloneURL(r.URL), nil
	}
	if looksLikeAbsoluteRequest(r) {
		return url.Parse(r.RequestURI)
	}
	if r.Host == "" {
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

func looksLikeAbsoluteRequest(r *http.Request) bool {
	return strings.HasPrefix(r.RequestURI, "http://") || strings.HasPrefix(r.RequestURI, "https://")
}

func cloneURL(source *url.URL) *url.URL {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}

func safeJoinPath(path string) (string, error) {
	clean := filepath.Clean(path)
	if clean == "." {
		return "", errors.New("empty cache path")
	}
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("path traversal is not allowed: %s", path)
	}
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(filepath.Separator))
	}
	return clean, nil
}

func ensurePort(host, defaultPort string) string {
	if strings.Contains(host, ":") {
		return host
	}
	return host + ":" + defaultPort
}

func stripPort(host string) string {
	if !strings.Contains(host, ":") {
		return host
	}
	value, _, err := net.SplitHostPort(host)
	if err == nil {
		return value
	}
	return host
}

func copyEndToEndHeaders(dst, src http.Header) {
	for key, values := range src {
		switch strings.ToLower(key) {
		case "connection", "proxy-connection", "keep-alive", "proxy-authenticate",
			"proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
