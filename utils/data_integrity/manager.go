package data_integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"sync/atomic"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Manager 数据完整性管理器接口
type Manager interface {
	RecordFileHash(filePath string, data []byte) error
	VerifyFileIntegrity(filePath string) (bool, error)
	RepairCorruptedFile(filePath string) error
	CheckAllFilesIntegrity() (int, int, error)
	GetCorruptedFiles() []string
	GetStats() (totalFiles int, corruptedFiles int, lastCheck time.Time)
	StartPeriodicCheck()
	CleanupOrphanedHashes() int
	RemoveFileHash(filePath string) error
	Close() error
}

// baseManager 基础实现（用于共享通用逻辑）
type baseManager struct {
	lastCheckTime       atomic.Int64 // 存储 Unix 纳秒时间戳
	checkInterval       time.Duration
	enableAutoRepair    bool
	enablePeriodicCheck bool
	cachePath           string
}

func (b *baseManager) updateLastCheckTime() {
	b.lastCheckTime.Store(time.Now().UnixNano())
}

// lastCheck 返回最近一次检查的时间（线程安全）
func (b *baseManager) lastCheck() time.Time {
	ns := b.lastCheckTime.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (b *baseManager) calculateHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// NewDataIntegrityManager 创建新的数据完整性管理器（尝试持久化，失败则回退到内存）
func NewManager(
	cachePath string, db *bolt.DB,
	checkInterval time.Duration, enableAutoRepair bool, enablePeriodicCheck bool,
) Manager {
	return NewPersistentDataIntegrityManager(cachePath, db, checkInterval, enableAutoRepair, enablePeriodicCheck)
}
