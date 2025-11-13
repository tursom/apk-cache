package main

import (
	"sync"
	"time"
)

// AccessTimeTracker 跟踪文件的访问时间
type AccessTimeTracker struct {
	mu          sync.RWMutex
	accessTimes map[string]time.Time
}

// NewAccessTimeTracker 创建新的访问时间跟踪器
func NewAccessTimeTracker() *AccessTimeTracker {
	return &AccessTimeTracker{
		accessTimes: make(map[string]time.Time),
	}
}

// RecordAccess 记录文件访问
func (a *AccessTimeTracker) RecordAccess(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.accessTimes[path] = time.Now()
}

// GetAccessTime 获取文件的访问时间（如果有记录）
func (a *AccessTimeTracker) GetAccessTime(path string) (time.Time, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	t, ok := a.accessTimes[path]
	return t, ok
}

// Remove 移除文件的访问时间记录
func (a *AccessTimeTracker) Remove(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.accessTimes, path)
}

// Size 返回跟踪的文件数量
func (a *AccessTimeTracker) Size() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.accessTimes)
}
