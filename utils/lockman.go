package utils

import (
	"log"
	"sync"
	"time"

	"github.com/tursom/apk-cache/utils/i18n"
)

// fileLockInfo 文件锁信息
type fileLockInfo struct {
	man      *FileLockManager
	path     string
	mu       sync.Mutex
	refCount int
}

// FileLockManager 文件锁管理器
type FileLockManager struct {
	locks map[string]*fileLockInfo
	mu    sync.Mutex
}

// NewFileLockManager 创建新的文件锁管理器
func NewFileLockManager() *FileLockManager {
	return &FileLockManager{
		locks: make(map[string]*fileLockInfo),
	}
}

// Acquire 获取文件锁并立即锁定
// 返回一个 unlock 函数，调用时会释放锁并减少引用计数
//
// 用法:
//
//	unlock := manager.Acquire(path)
//	defer unlock()
//	// 临界区代码
func (m *FileLockManager) Acquire(path string) (unlock func()) {
	lock := m.acquire(path)
	lock.mu.Lock()
	return lock.Unlock
}

func (m *FileLockManager) acquire(path string) *fileLockInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, exists := m.locks[path]
	if !exists {
		info = &fileLockInfo{
			man:  m,
			path: path,
		}
		m.locks[path] = info
	}
	info.refCount++
	return info
}

// Release 释放文件锁
func (m *FileLockManager) release(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, exists := m.locks[path]
	if !exists {
		return
	}

	info.refCount--
	// 如果引用计数为 0，删除锁
	if info.refCount <= 0 {
		delete(m.locks, path)
	}
}

// Size 获取当前锁的数量（用于监控）
func (m *FileLockManager) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.locks)
}

// StartMonitor 启动锁监控 goroutine
func (m *FileLockManager) StartMonitor(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			size := m.Size()
			if size > 0 {
				log.Println(i18n.T("ActiveFileLockCount", map[string]any{"Count": size}))
			}
		}
	}()
}

func (l *fileLockInfo) Unlock() {
	l.mu.Unlock()
	l.man.release(l.path)
}
