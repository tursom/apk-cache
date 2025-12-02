package main

import (
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tursom/apk-cache/utils/i18n"
	bolt "go.etcd.io/bbolt"
)

// AccessTimeTracker 接口定义访问时间跟踪器的行为
type AccessTimeTracker interface {
	RecordAccess(path string)
	GetAccessTime(path string) (time.Time, bool)
	Remove(path string)
	Size() int
	Close() error
	CleanupOrphaned() int
}

// BoltAccessTimeTracker 使用 BoltDB 持久化存储
type BoltAccessTimeTracker struct {
	db         *bolt.DB
	bucketName []byte
}

// MemoryAccessTimeTracker 使用内存 map 存储（回退方案）
type MemoryAccessTimeTracker struct {
	mu          sync.RWMutex
	accessTimes map[string]time.Time
}

// NewAccessTimeTracker 创建新的访问时间跟踪器
// 尝试使用 BoltDB，如果失败则回退到内存实现
func NewAccessTimeTracker() AccessTimeTracker {
	dbPath := filepath.Join(*dataPath, "access_times.db")
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		log.Println(i18n.T("OpenAccessTimeDBFailed", map[string]any{"Error": err}))
		log.Println(i18n.T("FallingBackToMemoryTracker", nil))
		return NewMemoryAccessTimeTracker()
	}

	// 确保 bucket 存在
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("access_times"))
		return err
	})
	if err != nil {
		log.Println(i18n.T("CreateBucketFailed", map[string]any{"Error": err}))
		db.Close()
		return NewMemoryAccessTimeTracker()
	}

	return &BoltAccessTimeTracker{
		db:         db,
		bucketName: []byte("access_times"),
	}
}

// NewMemoryAccessTimeTracker 创建纯内存的访问时间跟踪器
func NewMemoryAccessTimeTracker() AccessTimeTracker {
	return &MemoryAccessTimeTracker{
		accessTimes: make(map[string]time.Time),
	}
}

// --- BoltAccessTimeTracker 方法实现 ---

func (b *BoltAccessTimeTracker) RecordAccess(path string) {
	now := time.Now()
	unix := now.Unix()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(unix))

	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b.bucketName)
		if bucket == nil {
			return nil // 不应该发生
		}
		return bucket.Put([]byte(path), buf[:])
	})
	if err != nil {
		log.Println(i18n.T("SaveAccessTimeFailed", map[string]any{"Path": path, "Error": err}))
	}
}

func (b *BoltAccessTimeTracker) GetAccessTime(path string) (time.Time, bool) {
	var buf []byte
	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b.bucketName)
		if bucket == nil {
			return nil
		}
		buf = bucket.Get([]byte(path))
		return nil
	})
	if err != nil || buf == nil || len(buf) != 8 {
		return time.Time{}, false
	}
	unix := int64(binary.BigEndian.Uint64(buf))
	return time.Unix(unix, 0), true
}

func (b *BoltAccessTimeTracker) Remove(path string) {
	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b.bucketName)
		if bucket == nil {
			return nil
		}
		return bucket.Delete([]byte(path))
	})
	if err != nil {
		log.Println(i18n.T("DeleteAccessTimeFailed", map[string]any{"Path": path, "Error": err}))
	}
}

func (b *BoltAccessTimeTracker) Size() int {
	var count int
	b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b.bucketName)
		if bucket == nil {
			return nil
		}
		stats := bucket.Stats()
		count = stats.KeyN
		return nil
	})
	return count
}

func (b *BoltAccessTimeTracker) Close() error {
	return b.db.Close()
}

func (b *BoltAccessTimeTracker) CleanupOrphaned() int {
	// 由于 BoltDB 不存储文件系统状态，我们需要遍历所有键并检查文件是否存在
	// 这可能会很慢，所以我们可以选择不实现，或者定期运行
	// 这里简单返回 0，如果需要可以扩展
	return 0
}

// --- MemoryAccessTimeTracker 方法实现 ---

func (m *MemoryAccessTimeTracker) RecordAccess(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accessTimes[path] = time.Now()
}

func (m *MemoryAccessTimeTracker) GetAccessTime(path string) (time.Time, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.accessTimes[path]
	return t, ok
}

func (m *MemoryAccessTimeTracker) Remove(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.accessTimes, path)
}

func (m *MemoryAccessTimeTracker) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.accessTimes)
}

func (m *MemoryAccessTimeTracker) Close() error {
	// 无资源需要释放
	return nil
}

func (m *MemoryAccessTimeTracker) CleanupOrphaned() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	var cleaned int
	for path := range m.accessTimes {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			delete(m.accessTimes, path)
			cleaned++
		}
	}
	return cleaned
}
