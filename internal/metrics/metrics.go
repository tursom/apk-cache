package metrics

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	registry *prometheus.Registry

	CacheHits          prometheus.Counter
	CacheMisses        prometheus.Counter
	DownloadBytes      prometheus.Counter
	ResponseBytes      prometheus.Counter
	UpstreamRequests   prometheus.Counter
	UpstreamFailovers  prometheus.Counter
	ValidationFailures prometheus.Counter
	APKHashFailures    prometheus.Counter
	APKSignFailures    prometheus.Counter
	APKBypassResponses prometheus.Counter

	MemoryHits      prometheus.Counter
	MemoryMisses    prometheus.Counter
	MemoryEvictions prometheus.Counter
	MemorySize      *prometheus.GaugeVec
	MemoryItems     prometheus.Gauge
}

func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		CacheHits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_hits_total",
			Help: "Total number of cache hits.",
		}),
		CacheMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_misses_total",
			Help: "Total number of cache misses.",
		}),
		DownloadBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_download_bytes_total",
			Help: "Total bytes downloaded from upstream.",
		}),
		ResponseBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_response_bytes_total",
			Help: "Total bytes written to clients.",
		}),
		UpstreamRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_upstream_requests_total",
			Help: "Total upstream requests.",
		}),
		UpstreamFailovers: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_upstream_failovers_total",
			Help: "Total upstream failovers.",
		}),
		ValidationFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_validation_failures_total",
			Help: "Total cache validation failures.",
		}),
		APKHashFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_apk_hash_failures_total",
			Help: "Total APK hash validation failures.",
		}),
		APKSignFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_apk_signature_failures_total",
			Help: "Total APK signature validation failures.",
		}),
		APKBypassResponses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_apk_bypass_responses_total",
			Help: "Total APK responses bypassed from cache after signature validation failure.",
		}),
		MemoryHits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_memory_hits_total",
			Help: "Total memory cache hits.",
		}),
		MemoryMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_memory_misses_total",
			Help: "Total memory cache misses.",
		}),
		MemoryEvictions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "apk_cache_memory_evictions_total",
			Help: "Total memory cache evictions.",
		}),
		MemorySize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "apk_cache_memory_size_bytes",
			Help: "Memory cache size by type.",
		}, []string{"type"}),
		MemoryItems: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "apk_cache_memory_items_total",
			Help: "Total memory cache items.",
		}),
	}
	m.register()
	return m
}

func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

func (m *Metrics) register() {
	m.registry.MustRegister(
		m.CacheHits,
		m.CacheMisses,
		m.DownloadBytes,
		m.ResponseBytes,
		m.UpstreamRequests,
		m.UpstreamFailovers,
		m.ValidationFailures,
		m.APKHashFailures,
		m.APKSignFailures,
		m.APKBypassResponses,
		m.MemoryHits,
		m.MemoryMisses,
		m.MemoryEvictions,
		m.MemorySize,
		m.MemoryItems,
	)
}

func (m *Metrics) RecordCacheHit(size int64) {
	m.CacheHits.Inc()
	if size > 0 {
		m.ResponseBytes.Add(float64(size))
	}
}

func (m *Metrics) RecordCacheMiss(size int64) {
	m.CacheMisses.Inc()
	if size > 0 {
		m.DownloadBytes.Add(float64(size))
	}
}

func (m *Metrics) RecordResponseBytes(size int64) {
	if size > 0 {
		m.ResponseBytes.Add(float64(size))
	}
}

func (m *Metrics) UpdateMemory(current, max int64, items int) {
	m.MemorySize.WithLabelValues("current").Set(float64(current))
	m.MemorySize.WithLabelValues("max").Set(float64(max))
	m.MemoryItems.Set(float64(items))
}
