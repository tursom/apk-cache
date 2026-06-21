package app

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/tursom/apk-cache/internal/store"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}

func (w *loggingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return hijacker.Hijack()
}

func (a *App) recordRequest(r *http.Request, w *loggingResponseWriter, duration time.Duration, errText string) {
	if a.store == nil {
		return
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	path := r.URL.RequestURI()
	if r.Method == http.MethodConnect {
		path = r.Host
	} else if r.URL != nil && r.URL.IsAbs() {
		path = r.URL.String()
	}
	host := r.Host
	if r.URL != nil && r.URL.Host != "" {
		host = r.URL.Host
	}
	log := store.RequestLog{
		TS:          time.Now().UTC().Format(time.RFC3339Nano),
		Method:      r.Method,
		Protocol:    protocolForRequest(r),
		Host:        host,
		Path:        path,
		StatusCode:  status,
		CacheStatus: w.Header().Get(HeaderCache),
		DurationMS:  duration.Milliseconds(),
		BytesSent:   w.bytes,
		Error:       errText,
	}
	if err := a.store.AddRequestLog(context.Background(), log); err != nil {
		slog.Debug("record request log", "err", err)
	}
}

func (a *App) recordCacheObject(ctx context.Context, req cacheRequest, size int64, contentType, cacheStatus, validationStatus string) {
	if a.store == nil || req.cachePath == "" {
		return
	}
	obj := store.CacheObject{
		Protocol:         req.protocol,
		Class:            req.cacheClass,
		Host:             req.host,
		RequestPath:      req.requestPath,
		CachePath:        req.cachePath,
		SizeBytes:        size,
		ContentType:      contentType,
		CacheStatus:      cacheStatus,
		ValidationStatus: validationStatus,
		LastAccessedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	if obj.Protocol == "" {
		obj.Protocol = "unknown"
	}
	if obj.Class == "" {
		obj.Class = "other"
	}
	if obj.RequestPath == "" {
		obj.RequestPath = req.cachePath
	}
	if obj.Host == "" {
		obj.Host = "unknown"
	}
	if err := a.store.UpsertCacheObject(ctx, obj); err != nil {
		slog.Debug("record cache object", "err", err)
	}
}
