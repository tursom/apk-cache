package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tursom/apk-cache/utils"
)

type Pipeline struct {
	app      *App
	adapters []ProtocolAdapter
}

func NewPipeline(app *App, adapters []ProtocolAdapter) *Pipeline {
	return &Pipeline{
		app:      app,
		adapters: adapters,
	}
}

// 统一请求入口：
// 1. 交给适配器识别和归一化请求
// 2. 根据缓存策略依次尝试内存缓存、磁盘缓存
// 3. 未命中时回源、校验并决定是否落盘
func (p *Pipeline) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	adapter := p.matchAdapter(r)
	if adapter == nil {
		http.Error(w, "unsupported request", http.StatusBadRequest)
		return
	}

	req, err := adapter.Normalize(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if connector, ok := adapter.(ConnectHandler); ok && r.Method == http.MethodConnect {
		if err := connector.HandleConnect(r.Context(), p.app, w, r, req); err != nil {
			writePipelineError(w, err)
		}
		return
	}

	decision := adapter.CachePolicy(req)
	if req.CacheClass == "index" {
		decision.TTL = p.app.indexTTL
	}
	if req.CacheClass == "package" {
		decision.TTL = p.app.packageTTL
	}

	if !decision.Enabled {
		resp, err := adapter.Fetch(r.Context(), p.app, req)
		if err != nil {
			writePipelineError(w, err)
			return
		}
		defer resp.Body.Close()
		p.writeResponse(w, resp, "BYPASS")
		return
	}

	cacheKey, err := adapter.CacheKey(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cachePath := filepath.Join(p.app.cfg.Cache.Root, cacheKey)

	if p.tryServeFromMemory(w, cachePath) {
		return
	}
	if p.tryServeFromDisk(w, r, adapter, req, decision, cachePath) {
		return
	}

	unlock := p.app.lockManager.Acquire(cachePath)
	defer unlock()

	if p.tryServeFromMemory(w, cachePath) {
		return
	}
	if p.tryServeFromDisk(w, r, adapter, req, decision, cachePath) {
		return
	}

	resp, err := adapter.Fetch(r.Context(), p.app, req)
	if err != nil {
		writePipelineError(w, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		p.writeResponse(w, resp, "BYPASS")
		return
	}

	if err := p.fetchAndStore(r.Context(), w, resp, adapter, req, decision, cachePath); err != nil {
		log.Printf("fetch and store %s: %v", cachePath, err)
		writePipelineError(w, err)
	}
}

// 按注册顺序选择首个匹配请求的适配器。
func (p *Pipeline) matchAdapter(r *http.Request) ProtocolAdapter {
	for _, adapter := range p.adapters {
		if adapter.Match(r) {
			return adapter
		}
	}
	return nil
}

// 保持主流程简洁，内存缓存细节全部封装在 MemoryCache 中。
func (p *Pipeline) tryServeFromMemory(w http.ResponseWriter, cachePath string) bool {
	if p.app.memoryCache == nil {
		return false
	}
	return p.app.memoryCache.ServeFromMemory(w, cachePath)
}

// 磁盘命中时除了 TTL，还会执行协议相关校验。
// 例如 APT 会做 by-hash/.deb 校验，APK 会做 APKINDEX 哈希和签名校验。
func (p *Pipeline) tryServeFromDisk(w http.ResponseWriter, r *http.Request, adapter ProtocolAdapter, req *NormalizedRequest, decision CacheDecision, cachePath string) bool {
	info, err := os.Stat(cachePath)
	if err != nil || info.IsDir() {
		return false
	}
	if isExpired(info.ModTime(), decision.TTL) {
		_ = os.Remove(cachePath)
		if p.app.memoryCache != nil {
			p.app.memoryCache.Delete(cachePath)
		}
		return false
	}
	if err := adapter.ValidateCached(r.Context(), p.app, req, cachePath); err != nil {
		utils.Monitoring.RecordValidationFailure()
		_ = os.Remove(cachePath)
		if p.app.memoryCache != nil {
			p.app.memoryCache.Delete(cachePath)
		}
		return false
	}

	file, err := os.Open(cachePath)
	if err != nil {
		return false
	}
	defer file.Close()

	w.Header().Set("X-Cache", "HIT")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeContent(w, r, filepath.Base(cachePath), info.ModTime(), file)
	utils.Monitoring.RecordCacheHit(info.Size())

	if decision.StoreInMemory && p.app.memoryCache != nil && (p.app.memoryCacheMaxItemSize == 0 || info.Size() <= p.app.memoryCacheMaxItemSize) {
		data, err := os.ReadFile(cachePath)
		if err == nil {
			headers := map[string][]string{
				"Last-Modified":  {info.ModTime().UTC().Format(http.TimeFormat)},
				"Content-Length": {strconv.FormatInt(info.Size(), 10)},
			}
			p.app.memoryCache.CacheToMemory(cachePath, data, headers, http.StatusOK, info.ModTime())
		}
	}

	return true
}

// 这里采用“先完整下载到临时文件，再校验，再响应”的顺序。
// 这样即便上游在传输中断开，也不会把半截文件返回给客户端或污染缓存。
func (p *Pipeline) fetchAndStore(ctx context.Context, w http.ResponseWriter, resp *http.Response, adapter ProtocolAdapter, req *NormalizedRequest, decision CacheDecision, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(cachePath), "cache-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	// Phase 1: 先完整下载到临时文件，避免客户端收到半截内容。
	var written int64
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			utils.Monitoring.RecordDownloadBytes(int64(n))
			if _, err := tmpFile.Write(buffer[:n]); err != nil {
				return err
			}
			written += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return readErr
		}
	}

	if err := tmpFile.Sync(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, cachePath); err != nil {
		return err
	}

	if err := adapter.ValidateFetched(ctx, p.app, req, cachePath); err != nil {
		utils.Monitoring.RecordValidationFailure()
		if shouldBypassCache(err) {
			// APK 签名失败属于“允许透传但不允许缓存”的场景：
			// 先用临时落盘文件响应客户端，再在函数退出时删除它。
			utils.Monitoring.RecordAPKBypassResponse()
			defer func() {
				_ = os.Remove(cachePath)
				if p.app.memoryCache != nil {
					p.app.memoryCache.Delete(cachePath)
				}
			}()
			_, writeErr := p.writeStoredResponse(w, resp, cachePath, "BYPASS")
			return writeErr
		}
		_ = os.Remove(cachePath)
		if p.app.memoryCache != nil {
			p.app.memoryCache.Delete(cachePath)
		}
		return err
	}

	n, err := p.writeStoredResponse(w, resp, cachePath, "MISS")
	if err == nil {
		utils.Monitoring.RecordCacheMiss(n)
	}

	info, statErr := os.Stat(cachePath)
	if statErr != nil {
		return statErr
	}

	// Phase 3: 只有通过校验的文件才有资格进入内存缓存。
	if decision.StoreInMemory && p.app.memoryCache != nil && (p.app.memoryCacheMaxItemSize == 0 || info.Size() <= p.app.memoryCacheMaxItemSize) {
		data, readErr := os.ReadFile(cachePath)
		if readErr == nil {
			headers := cloneHeaders(resp.Header)
			headers.Set("Content-Length", strconv.Itoa(len(data)))
			p.app.memoryCache.CacheToMemory(cachePath, data, headers, resp.StatusCode, info.ModTime())
		}
	}

	return nil
}

// 该 helper 同时服务于正常 MISS 返回和“校验失败但允许透传”的 BYPASS 返回。
func (p *Pipeline) writeStoredResponse(w http.ResponseWriter, resp *http.Response, cachePath, cacheStatus string) (int64, error) {
	info, err := os.Stat(cachePath)
	if err != nil {
		return 0, err
	}
	file, err := os.Open(cachePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	copyEndToEndHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Cache", cacheStatus)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(resp.StatusCode)

	n, err := io.Copy(w, file)
	if err == nil {
		utils.Monitoring.RecordResponseBytes(n)
	}
	return n, err
}

// 对不进入缓存的响应做直接透传。
func (p *Pipeline) writeResponse(w http.ResponseWriter, resp *http.Response, cacheHeader string) {
	copyEndToEndHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Cache", cacheHeader)
	w.WriteHeader(resp.StatusCode)

	n, err := io.Copy(w, resp.Body)
	if err == nil {
		utils.Monitoring.RecordResponseBytes(n)
	}
}

func isExpired(modTime time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	return time.Since(modTime) > ttl
}

// 内存缓存需要一份可安全修改的 header 副本，因此这里做深拷贝。
func cloneHeaders(source http.Header) http.Header {
	cloned := make(http.Header, len(source))
	for key, values := range source {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}

// 把内部错误映射到更合适的外部 HTTP 状态码。
func writePipelineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrProxyDisabled):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrConnectNotAllowed):
		http.Error(w, err.Error(), http.StatusMethodNotAllowed)
	default:
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}
