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
	"sync"
	"time"

	"github.com/tursom/apk-cache/utils"
)

var streamCopyBufferPool = sync.Pool{
	New: func() any {
		buffer := make([]byte, 32*1024)
		return &buffer
	},
}

type streamCopyResult struct {
	Downloaded   int64
	Responded    int64
	CacheFailed  bool
	ClientFailed bool
}

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

// 这里采用“边下载边响应，同时写入临时文件”的顺序。
// 下载完成后再校验临时文件，并决定是否提升为正式缓存。
// 这样可以降低首字节延迟，但代价是校验失败时客户端可能已经收到上游内容。
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

	copyEndToEndHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(resp.StatusCode)

	var flush func()
	if flusher, ok := w.(http.Flusher); ok {
		flush = flusher.Flush
	}
	copyResult, readErr := streamResponseToSinks(resp.Body, w, tmpFile, flush)
	utils.Monitoring.RecordResponseBytes(copyResult.Responded)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		if p.app.memoryCache != nil {
			p.app.memoryCache.Delete(cachePath)
		}
		return nil
	}

	cacheReady := !copyResult.CacheFailed
	if cacheReady {
		if err := tmpFile.Sync(); err != nil {
			cacheReady = false
		}
	}
	if err := tmpFile.Close(); err != nil {
		cacheReady = false
	}
	if !cacheReady {
		if p.app.memoryCache != nil {
			p.app.memoryCache.Delete(cachePath)
		}
		return nil
	}

	if err := adapter.ValidateFetched(ctx, p.app, req, cachePath, tmpName); err != nil {
		utils.Monitoring.RecordValidationFailure()
		if shouldBypassCache(err) {
			// 流式响应已经发出，这里只需放弃缓存即可。
			utils.Monitoring.RecordAPKBypassResponse()
			if p.app.memoryCache != nil {
				p.app.memoryCache.Delete(cachePath)
			}
			return nil
		}
		if p.app.memoryCache != nil {
			p.app.memoryCache.Delete(cachePath)
		}
		return nil
	}

	if err := os.Rename(tmpName, cachePath); err != nil {
		return err
	}
	if err := adapter.CommitStored(ctx, p.app, req, cachePath); err != nil {
		_ = os.Remove(cachePath)
		if p.app.memoryCache != nil {
			p.app.memoryCache.Delete(cachePath)
		}
		return err
	}

	utils.Monitoring.RecordCacheMiss(copyResult.Downloaded)

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

// streamResponseToSinks 同时把上游响应写到客户端和缓存临时文件。
// 任一写入方失败时，只停止该写入方，另一方继续工作。
func streamResponseToSinks(src io.Reader, client io.Writer, cache io.Writer, flush func()) (streamCopyResult, error) {
	var result streamCopyResult
	clientEnabled := client != nil
	cacheEnabled := cache != nil
	bufferPtr := streamCopyBufferPool.Get().(*[]byte)
	buffer := *bufferPtr
	defer streamCopyBufferPool.Put(bufferPtr)

	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			utils.Monitoring.RecordDownloadBytes(int64(n))
			result.Downloaded += int64(n)

			if cacheEnabled {
				if _, err := cache.Write(chunk); err != nil {
					cacheEnabled = false
					result.CacheFailed = true
				}
			}
			if clientEnabled {
				written, err := client.Write(chunk)
				result.Responded += int64(written)
				if err != nil {
					clientEnabled = false
					result.ClientFailed = true
				} else if flush != nil {
					flush()
				}
			}
		}
		if readErr != nil {
			return result, readErr
		}
	}
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
