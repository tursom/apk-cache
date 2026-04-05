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
	}
}

func (p *Pipeline) matchAdapter(r *http.Request) ProtocolAdapter {
	for _, adapter := range p.adapters {
		if adapter.Match(r) {
			return adapter
		}
	}
	return nil
}

func (p *Pipeline) tryServeFromMemory(w http.ResponseWriter, cachePath string) bool {
	if p.app.memoryCache == nil {
		return false
	}
	return p.app.memoryCache.ServeFromMemory(w, cachePath)
}

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

	responseData := make([]byte, 0)
	buffer := make([]byte, 32*1024)
	var written int64
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			utils.Monitoring.RecordDownloadBytes(int64(n))
			utils.Monitoring.RecordCacheMiss(int64(n))
			utils.Monitoring.RecordResponseBytes(int64(n))

			if _, err := tmpFile.Write(chunk); err != nil {
				return err
			}
			if _, err := w.Write(chunk); err != nil {
				return err
			}
			written += int64(n)

			if decision.StoreInMemory && p.app.memoryCache != nil && (p.app.memoryCacheMaxItemSize == 0 || written <= p.app.memoryCacheMaxItemSize) {
				responseData = append(responseData, chunk...)
			}
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
		_ = os.Remove(cachePath)
		if p.app.memoryCache != nil {
			p.app.memoryCache.Delete(cachePath)
		}
		return err
	}

	if decision.StoreInMemory && p.app.memoryCache != nil && len(responseData) > 0 {
		headers := cloneHeaders(resp.Header)
		headers.Set("Content-Length", strconv.Itoa(len(responseData)))
		p.app.memoryCache.CacheToMemory(cachePath, responseData, headers, resp.StatusCode, time.Now())
	}

	return nil
}

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

func cloneHeaders(source http.Header) http.Header {
	cloned := make(http.Header, len(source))
	for key, values := range source {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}

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
