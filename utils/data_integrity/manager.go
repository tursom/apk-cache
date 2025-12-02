package data_integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/tursom/apk-cache/utils/i18n"
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
	mu                  sync.RWMutex
	lastCheckTime       time.Time
	checkInterval       time.Duration
	enableAutoRepair    bool
	enablePeriodicCheck bool
	cachePath           string
}

func (b *baseManager) updateLastCheckTime() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastCheckTime = time.Now()
}

func (b *baseManager) calculateHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (b *baseManager) calculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// NewDataIntegrityManager 创建新的数据完整性管理器（尝试持久化，失败则回退到内存）
func NewManager(
	cachePath, dataPath string,
	checkInterval time.Duration, enableAutoRepair bool, enablePeriodicCheck bool,
) Manager {
	manager, err := NewPersistentDataIntegrityManager(cachePath, dataPath, checkInterval, enableAutoRepair, enablePeriodicCheck)
	if err != nil {
		log.Println(i18n.T("OpenDatabaseFailed", map[string]any{"Error": err}))
		log.Println(i18n.T("FallbackToMemoryMode", nil))
		return NewMemoryDataIntegrityManager(cachePath, checkInterval, enableAutoRepair, enablePeriodicCheck)
	}
	return manager
}
