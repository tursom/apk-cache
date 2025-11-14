package main

import (
	"sync"
	"time"
)

// RateLimiter 基于令牌桶算法的限流器
type RateLimiter struct {
	mu sync.Mutex

	// 令牌桶参数
	rate     float64   // 每秒生成的令牌数
	capacity float64   // 令牌桶容量
	tokens   float64   // 当前令牌数量
	lastTime time.Time // 上次令牌更新时间

	// 统计信息
	allowedRequests  int64     // 允许的请求数
	rejectedRequests int64     // 拒绝的请求数
	totalRequests    int64     // 总请求数
	lastResetTime    time.Time // 上次统计重置时间
}

// NewRateLimiter 创建新的限流器
func NewRateLimiter(rate float64, capacity float64) *RateLimiter {
	return &RateLimiter{
		rate:          rate,
		capacity:      capacity,
		tokens:        capacity, // 初始时令牌桶是满的
		lastTime:      time.Now(),
		lastResetTime: time.Now(),
	}
}

// Allow 检查是否允许请求
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.totalRequests++

	// 更新令牌数量
	now := time.Now()
	elapsed := now.Sub(rl.lastTime).Seconds()
	rl.tokens += elapsed * rl.rate
	if rl.tokens > rl.capacity {
		rl.tokens = rl.capacity
	}
	rl.lastTime = now

	// 检查是否有足够的令牌
	if rl.tokens >= 1 {
		rl.tokens--
		rl.allowedRequests++
		return true
	}

	rl.rejectedRequests++
	return false
}

// AllowN 检查是否允许N个请求
func (rl *RateLimiter) AllowN(n int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.totalRequests += int64(n)

	// 更新令牌数量
	now := time.Now()
	elapsed := now.Sub(rl.lastTime).Seconds()
	rl.tokens += elapsed * rl.rate
	if rl.tokens > rl.capacity {
		rl.tokens = rl.capacity
	}
	rl.lastTime = now

	// 检查是否有足够的令牌
	if rl.tokens >= float64(n) {
		rl.tokens -= float64(n)
		rl.allowedRequests += int64(n)
		return true
	}

	rl.rejectedRequests += int64(n)
	return false
}

// GetStats 获取限流器统计信息
func (rl *RateLimiter) GetStats() map[string]any {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 如果超过1小时，重置统计信息
	if time.Since(rl.lastResetTime) > time.Hour {
		rl.allowedRequests = 0
		rl.rejectedRequests = 0
		rl.totalRequests = 0
		rl.lastResetTime = time.Now()
	}

	return map[string]any{
		"rate":              rl.rate,
		"capacity":          rl.capacity,
		"current_tokens":    rl.tokens,
		"allowed_requests":  rl.allowedRequests,
		"rejected_requests": rl.rejectedRequests,
		"total_requests":    rl.totalRequests,
		"rejection_rate":    float64(rl.rejectedRequests) / float64(rl.totalRequests),
		"last_reset_time":   rl.lastResetTime,
	}
}

// ResetStats 重置统计信息
func (rl *RateLimiter) ResetStats() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.allowedRequests = 0
	rl.rejectedRequests = 0
	rl.totalRequests = 0
	rl.lastResetTime = time.Now()
}

// SetRate 动态设置速率
func (rl *RateLimiter) SetRate(rate float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.rate = rate
}

// SetCapacity 动态设置容量
func (rl *RateLimiter) SetCapacity(capacity float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 调整当前令牌数量
	if capacity < rl.tokens {
		rl.tokens = capacity
	}
	rl.capacity = capacity
}

// updateRateLimitMetrics 定期更新限流器指标
func updateRateLimitMetrics() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if rateLimiter != nil {
			stats := rateLimiter.GetStats()
			if currentTokens, ok := stats["current_tokens"].(float64); ok {
				rateLimitCurrentTokens.Set(currentTokens)
			}
		}
	}
}
