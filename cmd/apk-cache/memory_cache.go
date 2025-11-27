package main

import (
	"bytes"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MemoryCache 内存缓存管理器
type MemoryCache struct {
	mu          sync.RWMutex
	cache       map[string]*CacheItem
	maxSize     int64
	currentSize int64
	maxItems    int
	ttl         time.Duration
}

// CacheItem 缓存项
type CacheItem struct {
	Data        []byte
	Size        int64
	AccessTime  time.Time
	CreateTime  time.Time
	ModTime     time.Time
	AccessCount int64
	Headers     map[string][]string
	StatusCode  int
}

// 内存缓存相关的 Prometheus 指标
var (
	memCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_memory_hits_total",
		Help: "Total number of memory cache hits",
	})

	memCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_memory_misses_total",
		Help: "Total number of memory cache misses",
	})

	memCacheSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "apk_cache_memory_size_bytes",
		Help: "Memory cache size information",
	}, []string{"type"})

	memCacheItems = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "apk_cache_memory_items_total",
		Help: "Total number of items in memory cache",
	})

	memCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_memory_evictions_total",
		Help: "Total number of memory cache evictions",
	})
)

// NewMemoryCache 创建新的内存缓存
func NewMemoryCache(maxSize int64, maxItems int, ttl time.Duration) *MemoryCache {
	cache := &MemoryCache{
		cache:    make(map[string]*CacheItem),
		maxSize:  maxSize,
		maxItems: maxItems,
		ttl:      ttl,
	}

	// 初始化 Prometheus 指标
	memCacheSize.WithLabelValues("max").Set(float64(maxSize))
	memCacheSize.WithLabelValues("current").Set(0)
	memCacheItems.Set(0)

	// 启动定期清理过期项的 goroutine
	if ttl > 0 {
		go cache.startCleanup()
	}

	return cache
}

// Get 从内存缓存中获取数据
func (m *MemoryCache) Get(key string) (*CacheItem, bool) {
	m.mu.RLock()
	item, exists := m.cache[key]
	m.mu.RUnlock()

	if !exists {
		memCacheMisses.Inc()
		return nil, false
	}

	// 检查是否过期
	if m.ttl > 0 && time.Since(item.CreateTime) > m.ttl {
		m.mu.Lock()
		delete(m.cache, key)
		m.currentSize -= item.Size
		m.mu.Unlock()

		memCacheMisses.Inc()
		m.updateMetrics()
		return nil, false
	}

	// 更新访问时间和计数
	m.mu.Lock()
	item.AccessTime = time.Now()
	item.AccessCount++
	m.mu.Unlock()

	memCacheHits.Inc()
	return item, true
}

// Set 将数据存入内存缓存
func (m *MemoryCache) Set(key string, data []byte, headers map[string][]string, statusCode int, modTime time.Time) bool {
	size := int64(len(data))

	// 检查是否超过最大大小限制
	if m.maxSize > 0 && size > m.maxSize {
		log.Println(t("MemoryCacheItemTooLarge", map[string]any{
			"Key":  key,
			"Size": size,
			"Max":  m.maxSize,
		}))
		return false
	}

	item := &CacheItem{
		Data:        data,
		Size:        size,
		AccessTime:  time.Now(),
		CreateTime:  time.Now(),
		AccessCount: 1,
		Headers:     headers,
		StatusCode:  statusCode,
		ModTime:     modTime,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否需要清理空间
	if m.needCleanup(size) {
		if !m.cleanup(size) {
			return false
		}
	}

	// 如果键已存在，先移除旧项
	if existing, exists := m.cache[key]; exists {
		m.currentSize -= existing.Size
	}

	// 添加新项
	m.cache[key] = item
	m.currentSize += size

	m.updateMetrics()
	return true
}

// Delete 从内存缓存中删除项
func (m *MemoryCache) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if item, exists := m.cache[key]; exists {
		m.currentSize -= item.Size
		delete(m.cache, key)
		m.updateMetrics()
	}
}

// Clear 清空内存缓存
func (m *MemoryCache) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cache = make(map[string]*CacheItem)
	m.currentSize = 0
	m.updateMetrics()
}

// needCleanup 检查是否需要清理空间
func (m *MemoryCache) needCleanup(newItemSize int64) bool {
	if m.maxSize == 0 && m.maxItems == 0 {
		return false
	}

	if m.maxSize > 0 && m.currentSize+newItemSize > m.maxSize {
		return true
	}

	if m.maxItems > 0 && len(m.cache) >= m.maxItems {
		return true
	}

	return false
}

// cleanup 清理缓存以释放空间
func (m *MemoryCache) cleanup(needSize int64) bool {
	// 按访问时间排序（LRU策略）
	var items []struct {
		key  string
		item *CacheItem
	}

	for key, item := range m.cache {
		items = append(items, struct {
			key  string
			item *CacheItem
		}{key, item})
	}

	// 按访问时间排序（最早的在前）
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].item.AccessTime.After(items[j].item.AccessTime) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	freed := int64(0)
	evicted := 0

	// 删除最旧的项直到释放足够空间
	for _, entry := range items {
		if freed >= needSize && len(m.cache)-evicted < m.maxItems {
			break
		}

		m.currentSize -= entry.item.Size
		delete(m.cache, entry.key)
		freed += entry.item.Size
		evicted++

		memCacheEvictions.Inc()
		log.Println(t("MemoryCacheEvicted", map[string]any{
			"Key":  entry.key,
			"Size": entry.item.Size,
		}))
	}

	if evicted > 0 {
		log.Println(t("MemoryCacheCleanupComplete", map[string]any{
			"Evicted": evicted,
			"Freed":   freed,
		}))
	}

	return freed >= needSize || (m.maxItems > 0 && len(m.cache) < m.maxItems)
}

// startCleanup 启动定期清理过期项的 goroutine
func (m *MemoryCache) startCleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanupExpired()
	}
}

// cleanupExpired 清理过期项
func (m *MemoryCache) cleanupExpired() {
	if m.ttl == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	expiredCount := 0
	now := time.Now()

	for key, item := range m.cache {
		if now.Sub(item.CreateTime) > m.ttl {
			m.currentSize -= item.Size
			delete(m.cache, key)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		log.Println(t("MemoryCacheExpiredCleaned", map[string]any{
			"Count": expiredCount,
		}))
		m.updateMetrics()
	}
}

// updateMetrics 更新 Prometheus 指标
func (m *MemoryCache) updateMetrics() {
	memCacheSize.WithLabelValues("current").Set(float64(m.currentSize))
	memCacheItems.Set(float64(len(m.cache)))
}

// GetStats 获取内存缓存统计信息
func (m *MemoryCache) GetStats() (currentSize int64, maxSize int64, itemCount int, hitRate float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	currentSize = m.currentSize
	maxSize = m.maxSize
	itemCount = len(m.cache)

	// 由于 Prometheus 计数器是接口类型，我们无法直接计算命中率
	// 在实际使用中，可以通过 Prometheus 查询来获取命中率
	hitRate = 0.0

	return
}

// ServeFromMemory 从内存缓存提供数据
func (m *MemoryCache) ServeFromMemory(w http.ResponseWriter, key string) bool {
	item, found := m.Get(key)
	if !found {
		return false
	}

	log.Println(t("MemoryCacheHit", map[string]any{"Path": key}))

	// 复制响应头
	for key, values := range item.Headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.Header().Set("X-Cache", "MEMORY-HIT")
	w.WriteHeader(item.StatusCode)

	// 写入响应体
	if _, err := w.Write(item.Data); err != nil {
		log.Println(t("MemoryCacheWriteFailed", map[string]any{"Error": err}))
		return false
	}

	cacheHits.Add(1)
	cacheHitBytes.Add(float64(len(item.Data)))
	return true
}

// CacheToMemory 将数据缓存到内存
func (m *MemoryCache) CacheToMemory(key string, data []byte, headers map[string][]string, statusCode int, modTime time.Time) {
	if !m.Set(key, data, headers, statusCode, modTime) {
		log.Println(t("MemoryCacheStoreFailed", map[string]any{"Key": key}))
	}
}

// GetModTime 获取内存缓存项的修改时间
func (m *MemoryCache) GetModTime(key string) (time.Time, bool) {
	item, found := m.Get(key)
	if !found {
		return time.Time{}, false
	}
	return item.ModTime, true
}

// CreateReaderFromMemory 从内存缓存创建读取器
func (m *MemoryCache) CreateReaderFromMemory(key string) (*bytes.Reader, map[string][]string, int, bool) {
	item, found := m.Get(key)
	if !found {
		return nil, nil, 0, false
	}

	return bytes.NewReader(item.Data), item.Headers, item.StatusCode, true
}
