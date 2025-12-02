package utils

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MonitoringManager 监控管理器
type MonitoringManager struct {
	registry *prometheus.Registry

	// 缓存相关指标
	CacheHits      prometheus.Counter
	CacheMisses    prometheus.Counter
	DownloadBytes  prometheus.Counter
	CacheHitBytes  prometheus.Counter
	CacheMissBytes prometheus.Counter

	// 限流相关指标
	RateLimitAllowed       prometheus.Counter
	RateLimitRejected      prometheus.Counter
	RateLimitCurrentTokens prometheus.Gauge

	// 数据完整性相关指标
	DataIntegrityChecks         prometheus.Counter
	DataIntegrityCorruptedFiles prometheus.Gauge
	DataIntegrityRepairedFiles  prometheus.Counter
	DataIntegrityCheckDuration  prometheus.Histogram

	// 缓存配额相关指标
	CacheQuotaSize       *prometheus.GaugeVec
	CacheQuotaFiles      prometheus.Gauge
	CacheQuotaCleanups   prometheus.Counter
	CacheQuotaBytesFreed prometheus.Counter

	// 内存缓存相关指标
	MemCacheHits      prometheus.Counter
	MemCacheMisses    prometheus.Counter
	MemCacheSize      *prometheus.GaugeVec
	MemCacheItems     prometheus.Gauge
	MemCacheEvictions prometheus.Counter
}

var Monitoring = NewMonitoring()

// NewMonitoring 创建新的监控管理器
func NewMonitoring() *MonitoringManager {
	registry := prometheus.NewRegistry()
	monitoring := &MonitoringManager{
		registry: registry,
	}

	// 初始化缓存相关指标
	monitoring.CacheHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_hits_total",
		Help: "Total number of cache hits",
	})

	monitoring.CacheMisses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_misses_total",
		Help: "Total number of cache misses",
	})

	monitoring.DownloadBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_download_bytes_total",
		Help: "Total bytes downloaded from upstream",
	})

	monitoring.CacheHitBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_hit_bytes_total",
		Help: "Total bytes served from cache hits",
	})

	monitoring.CacheMissBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_miss_bytes_total",
		Help: "Total bytes served from cache misses",
	})

	// 初始化限流相关指标
	monitoring.RateLimitAllowed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_rate_limit_allowed_total",
		Help: "Total number of requests allowed by rate limiter",
	})

	monitoring.RateLimitRejected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_rate_limit_rejected_total",
		Help: "Total number of requests rejected by rate limiter",
	})

	monitoring.RateLimitCurrentTokens = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "apk_cache_rate_limit_current_tokens",
		Help: "Current number of tokens in the rate limiter bucket",
	})

	// 初始化数据完整性相关指标
	monitoring.DataIntegrityChecks = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_data_integrity_checks_total",
		Help: "Total number of data integrity checks performed",
	})

	monitoring.DataIntegrityCorruptedFiles = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "apk_cache_data_integrity_corrupted_files_total",
		Help: "Total number of corrupted files detected",
	})

	monitoring.DataIntegrityRepairedFiles = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_data_integrity_repaired_files_total",
		Help: "Total number of corrupted files repaired",
	})

	monitoring.DataIntegrityCheckDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "apk_cache_data_integrity_check_duration_seconds",
		Help:    "Duration of data integrity checks",
		Buckets: prometheus.DefBuckets,
	})

	// 初始化缓存配额相关指标
	monitoring.CacheQuotaSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "apk_cache_quota_size_bytes",
		Help: "Cache quota size information",
	}, []string{"type"})

	monitoring.CacheQuotaFiles = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "apk_cache_quota_files_total",
		Help: "Total number of files in cache",
	})

	monitoring.CacheQuotaCleanups = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_quota_cleanups_total",
		Help: "Total number of cache quota cleanups performed",
	})

	monitoring.CacheQuotaBytesFreed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_quota_bytes_freed_total",
		Help: "Total bytes freed by cache quota cleanups",
	})

	// 初始化内存缓存相关指标
	monitoring.MemCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_memory_hits_total",
		Help: "Total number of memory cache hits",
	})

	monitoring.MemCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_memory_misses_total",
		Help: "Total number of memory cache misses",
	})

	monitoring.MemCacheSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "apk_cache_memory_size_bytes",
		Help: "Memory cache size information",
	}, []string{"type"})

	monitoring.MemCacheItems = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "apk_cache_memory_items_total",
		Help: "Total number of items in memory cache",
	})

	monitoring.MemCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_memory_evictions_total",
		Help: "Total number of memory cache evictions",
	})

	// 注册所有指标到自定义 Registry
	monitoring.registerMetrics()

	return monitoring
}

// registerMetrics 注册所有指标到 Registry
func (m *MonitoringManager) registerMetrics() {
	// 注册缓存相关指标
	m.registry.MustRegister(m.CacheHits)
	m.registry.MustRegister(m.CacheMisses)
	m.registry.MustRegister(m.DownloadBytes)
	m.registry.MustRegister(m.CacheHitBytes)
	m.registry.MustRegister(m.CacheMissBytes)

	// 注册限流相关指标
	m.registry.MustRegister(m.RateLimitAllowed)
	m.registry.MustRegister(m.RateLimitRejected)
	m.registry.MustRegister(m.RateLimitCurrentTokens)

	// 数据完整性、缓存配额和内存缓存指标使用 promauto 自动注册
}

// GetRegistry 获取 Prometheus Registry
func (m *MonitoringManager) GetRegistry() *prometheus.Registry {
	return m.registry
}

// UpdateRateLimitMetrics 更新限流器指标
func (m *MonitoringManager) UpdateRateLimitMetrics(currentTokens float64) {
	m.RateLimitCurrentTokens.Set(currentTokens)
}

// UpdateCacheQuotaMetrics 更新缓存配额指标
func (m *MonitoringManager) UpdateCacheQuotaMetrics(currentSize int64, fileCount int) {
	m.CacheQuotaSize.WithLabelValues("current").Set(float64(currentSize))
	m.CacheQuotaFiles.Set(float64(fileCount))
}

// UpdateMemoryCacheMetrics 更新内存缓存指标
func (m *MonitoringManager) UpdateMemoryCacheMetrics(currentSize int64, itemCount int) {
	m.MemCacheSize.WithLabelValues("current").Set(float64(currentSize))
	m.MemCacheItems.Set(float64(itemCount))
}

// RecordCacheHit 记录缓存命中
func (m *MonitoringManager) RecordCacheHit(bytes int64) {
	m.CacheHits.Add(1)
	m.CacheHitBytes.Add(float64(bytes))
}

// RecordCacheMiss 记录缓存未命中
func (m *MonitoringManager) RecordCacheMiss(bytes int64) {
	m.CacheMisses.Add(1)
	m.CacheMissBytes.Add(float64(bytes))
}

// RecordDownloadBytes 记录下载字节数
func (m *MonitoringManager) RecordDownloadBytes(bytes int64) {
	m.DownloadBytes.Add(float64(bytes))
}

// RecordRateLimitAllowed 记录限流允许的请求
func (m *MonitoringManager) RecordRateLimitAllowed() {
	m.RateLimitAllowed.Add(1)
}

// RecordRateLimitRejected 记录限流拒绝的请求
func (m *MonitoringManager) RecordRateLimitRejected() {
	m.RateLimitRejected.Add(1)
}

// RecordDataIntegrityCheck 记录数据完整性检查
func (m *MonitoringManager) RecordDataIntegrityCheck(duration time.Duration) {
	m.DataIntegrityChecks.Add(1)
	m.DataIntegrityCheckDuration.Observe(duration.Seconds())
}

// RecordDataIntegrityCorrupted 记录数据完整性损坏文件
func (m *MonitoringManager) RecordDataIntegrityCorrupted() {
	m.DataIntegrityCorruptedFiles.Inc()
}

// RecordDataIntegrityRepaired 记录数据完整性修复文件
func (m *MonitoringManager) RecordDataIntegrityRepaired() {
	m.DataIntegrityRepairedFiles.Inc()
	m.DataIntegrityCorruptedFiles.Dec()
}

// RecordCacheQuotaCleanup 记录缓存配额清理
func (m *MonitoringManager) RecordCacheQuotaCleanup(bytesFreed int64) {
	m.CacheQuotaCleanups.Add(1)
	m.CacheQuotaBytesFreed.Add(float64(bytesFreed))
}

// RecordMemoryCacheHit 记录内存缓存命中
func (m *MonitoringManager) RecordMemoryCacheHit() {
	m.MemCacheHits.Add(1)
}

// RecordMemoryCacheMiss 记录内存缓存未命中
func (m *MonitoringManager) RecordMemoryCacheMiss() {
	m.MemCacheMisses.Add(1)
}

// RecordMemoryCacheEviction 记录内存缓存驱逐
func (m *MonitoringManager) RecordMemoryCacheEviction() {
	m.MemCacheEvictions.Add(1)
}
