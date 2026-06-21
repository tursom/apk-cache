package cache

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tursom/apk-cache/internal/metrics"
)

type Item struct {
	Data       []byte
	Headers    http.Header
	StatusCode int
	ModTime    time.Time

	size        int64
	createdAt   time.Time
	accessedAt  time.Time
	accessCount int64
}

type Memory struct {
	mu          sync.RWMutex
	items       map[string]*Item
	maxSize     int64
	maxItems    int
	ttl         time.Duration
	currentSize int64
	metrics     *metrics.Metrics

	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewMemory(maxSize int64, maxItems int, ttl time.Duration, m *metrics.Metrics) *Memory {
	c := &Memory{
		items:    make(map[string]*Item),
		maxSize:  maxSize,
		maxItems: maxItems,
		ttl:      ttl,
		metrics:  m,
		stopCh:   make(chan struct{}),
	}
	if m != nil {
		m.UpdateMemory(0, maxSize, 0)
	}
	if ttl > 0 {
		go c.expireLoop()
	}
	return c
}

func (c *Memory) Get(key string) (*Item, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.items[key]
	if !ok {
		if c.metrics != nil {
			c.metrics.MemoryMisses.Inc()
		}
		return nil, false
	}
	if c.ttl > 0 && time.Since(item.createdAt) > c.ttl {
		c.deleteLocked(key)
		if c.metrics != nil {
			c.metrics.MemoryMisses.Inc()
			c.metrics.UpdateMemory(c.currentSize, c.maxSize, len(c.items))
		}
		return nil, false
	}
	item.accessedAt = time.Now()
	item.accessCount++
	if c.metrics != nil {
		c.metrics.MemoryHits.Inc()
	}
	return cloneItem(item), true
}

func (c *Memory) Set(key string, data []byte, headers http.Header, statusCode int, modTime time.Time) bool {
	size := int64(len(data))
	if c.maxSize > 0 && size > c.maxSize {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.items[key]; ok {
		c.currentSize -= existing.size
	}
	for c.needsSpaceLocked(size) {
		if !c.evictOneLocked() {
			return false
		}
	}

	now := time.Now()
	c.items[key] = &Item{
		Data:        append([]byte(nil), data...),
		Headers:     headers.Clone(),
		StatusCode:  statusCode,
		ModTime:     modTime,
		size:        size,
		createdAt:   now,
		accessedAt:  now,
		accessCount: 1,
	}
	c.currentSize += size
	if c.metrics != nil {
		c.metrics.UpdateMemory(c.currentSize, c.maxSize, len(c.items))
	}
	return true
}

func (c *Memory) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteLocked(key)
	if c.metrics != nil {
		c.metrics.UpdateMemory(c.currentSize, c.maxSize, len(c.items))
	}
}

func (c *Memory) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
}

func (c *Memory) Stats() (currentSize, maxSize int64, items int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSize, c.maxSize, len(c.items)
}

func (c *Memory) needsSpaceLocked(newSize int64) bool {
	if c.maxSize > 0 && c.currentSize+newSize > c.maxSize {
		return true
	}
	return c.maxItems > 0 && len(c.items) >= c.maxItems
}

func (c *Memory) evictOneLocked() bool {
	if len(c.items) == 0 {
		return false
	}
	keys := make([]string, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return c.items[keys[i]].accessedAt.Before(c.items[keys[j]].accessedAt)
	})
	c.deleteLocked(keys[0])
	if c.metrics != nil {
		c.metrics.MemoryEvictions.Inc()
	}
	return true
}

func (c *Memory) deleteLocked(key string) {
	if item, ok := c.items[key]; ok {
		c.currentSize -= item.size
		delete(c.items, key)
	}
}

func (c *Memory) expireLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.deleteExpired()
		}
	}
}

func (c *Memory) deleteExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, item := range c.items {
		if time.Since(item.createdAt) > c.ttl {
			c.deleteLocked(key)
		}
	}
	if c.metrics != nil {
		c.metrics.UpdateMemory(c.currentSize, c.maxSize, len(c.items))
	}
}

type KeyLocks struct {
	mu    sync.Mutex
	locks map[string]*refLock
}

type refLock struct {
	mu   sync.Mutex
	refs int
}

func NewKeyLocks() *KeyLocks {
	return &KeyLocks{locks: make(map[string]*refLock)}
}

func (l *KeyLocks) Lock(key string) func() {
	l.mu.Lock()
	lock := l.locks[key]
	if lock == nil {
		lock = &refLock{}
		l.locks[key] = lock
	}
	lock.refs++
	l.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		l.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
}

func ParseSize(value string) (int64, error) {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" || value == "0" {
		return 0, nil
	}

	multiplier := int64(1)
	for _, unit := range []struct {
		suffix string
		factor int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	} {
		suffix, factor := unit.suffix, unit.factor
		if strings.HasSuffix(value, suffix) {
			multiplier = factor
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
			break
		}
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if number < 0 {
		return 0, errors.New("size must be non-negative")
	}
	return number * multiplier, nil
}

func cloneItem(item *Item) *Item {
	return &Item{
		Data:       append([]byte(nil), item.Data...),
		Headers:    item.Headers.Clone(),
		StatusCode: item.StatusCode,
		ModTime:    item.ModTime,
		size:       item.size,
	}
}
