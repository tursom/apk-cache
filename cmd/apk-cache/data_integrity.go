package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tursom/apk-cache/utils/i18n"
	bolt "go.etcd.io/bbolt"
)

// DataIntegrityManager 数据完整性管理器
type DataIntegrityManager struct {
	mu                  sync.RWMutex
	fileHashes          map[string]string // 文件路径 -> SHA256哈希（数据库的缓存，数据库不可用时作为替代）
	corruptedFiles      map[string]bool   // 损坏文件列表
	lastCheckTime       time.Time
	checkInterval       time.Duration
	enableAutoRepair    bool
	enablePeriodicCheck bool
	db                  *bolt.DB // BoltDB 数据库实例（主存储）
	dbPath              string   // 数据库文件路径
}

// NewDataIntegrityManager 创建新的数据完整性管理器
func NewDataIntegrityManager(checkInterval time.Duration, enableAutoRepair bool, enablePeriodicCheck bool) *DataIntegrityManager {
	dbPath := filepath.Join(*dataPath, "file_hashes.db")

	// 打开 BoltDB 数据库
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		log.Println(i18n.T("OpenDatabaseFailed", map[string]any{"Error": err}))
		// 如果数据库打开失败，回退到内存模式
		return &DataIntegrityManager{
			fileHashes:          make(map[string]string),
			corruptedFiles:      make(map[string]bool),
			checkInterval:       checkInterval,
			enableAutoRepair:    enableAutoRepair,
			enablePeriodicCheck: enablePeriodicCheck,
			db:                  nil,
			dbPath:              dbPath,
		}
	}

	manager := &DataIntegrityManager{
		fileHashes:          make(map[string]string),
		corruptedFiles:      make(map[string]bool),
		checkInterval:       checkInterval,
		enableAutoRepair:    enableAutoRepair,
		enablePeriodicCheck: enablePeriodicCheck,
		db:                  db,
		dbPath:              dbPath,
	}

	// 自动加载持久化的哈希数据
	if err := manager.LoadHashes(); err != nil {
		log.Println(i18n.T("LoadFileHashesFailed", map[string]any{"Error": err}))
	}

	return manager
}

// RecordFileHash 记录文件的哈希值
func (d *DataIntegrityManager) RecordFileHash(filePath string, data []byte) error {
	hash := d.calculateHash(data)

	// 先更新内存缓存
	d.setFileHash(filePath, hash)

	// 如果数据库可用，保存到数据库
	if d.db != nil {
		if err := d.saveHashToDB(filePath, hash); err != nil {
			log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
		}
	}

	return nil
}

// getFileHash 获取文件哈希（线程安全）
func (d *DataIntegrityManager) getFileHash(filePath string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	hash, exists := d.fileHashes[filePath]
	return hash, exists
}

// setFileHash 设置文件哈希（线程安全）
func (d *DataIntegrityManager) setFileHash(filePath, hash string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fileHashes[filePath] = hash
}

// markFileAsCorrupted 标记文件为损坏（线程安全）
func (d *DataIntegrityManager) markFileAsCorrupted(filePath string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.corruptedFiles[filePath] = true
}

// removeFileHash 移除文件哈希（线程安全）
func (d *DataIntegrityManager) removeFileHash(filePath string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.fileHashes, filePath)
	delete(d.corruptedFiles, filePath)
}

// updateLastCheckTime 更新最后检查时间（线程安全）
func (d *DataIntegrityManager) updateLastCheckTime() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastCheckTime = time.Now()
}

// VerifyFileIntegrity 验证文件完整性
func (d *DataIntegrityManager) VerifyFileIntegrity(filePath string) (bool, error) {
	// 读取文件内容
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, errors.New(i18n.T("ReadFileFailed", map[string]any{"Error": err}))
	}

	// 计算当前哈希
	currentHash := d.calculateHash(data)

	// 获取记录的哈希（先从内存缓存中查找）
	recordedHash, exists := d.getFileHash(filePath)

	// 如果内存中没有找到，尝试从数据库查询（如果数据库可用）
	if !exists && d.db != nil {
		if dbHash, err := d.getHashFromDB(filePath); err == nil && dbHash != "" {
			recordedHash = dbHash
			exists = true
			// 将数据库中的哈希加载到内存缓存
			d.setFileHash(filePath, dbHash)
		}
	}

	if !exists {
		// 如果没有记录哈希，记录当前哈希到内存缓存
		d.setFileHash(filePath, currentHash)
		// 如果数据库可用，同时保存到数据库
		if d.db != nil {
			if err := d.saveHashToDB(filePath, currentHash); err != nil {
				log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
			}
		}
		return true, nil
	}

	// 比较哈希值
	if currentHash != recordedHash {
		d.markFileAsCorrupted(filePath)
		monitoring.RecordDataIntegrityCorrupted()

		log.Println(i18n.T("FileIntegrityCheckFailed", map[string]any{
			"File":     filePath,
			"Expected": recordedHash[:16] + "...",
			"Actual":   currentHash[:16] + "...",
		}))
		return false, nil
	}

	return true, nil
}

// RepairCorruptedFile 修复损坏的文件
func (d *DataIntegrityManager) RepairCorruptedFile(filePath string) error {
	log.Println(i18n.T("RepairingCorruptedFile", map[string]any{"File": filePath}))

	// 删除损坏的文件
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return errors.New(i18n.T("DeleteCorruptedFileFailed", map[string]any{"Error": err}))
	}

	// 从内存缓存中移除
	d.removeFileHash(filePath)

	// 如果数据库可用，从数据库中删除
	if d.db != nil {
		if err := d.deleteHashFromDB(filePath); err != nil {
			log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
		}
	}

	monitoring.RecordDataIntegrityRepaired()

	log.Println(i18n.T("FileRepaired", map[string]any{"File": filePath}))
	return nil
}

// CheckAllFilesIntegrity 检查所有文件的完整性
func (d *DataIntegrityManager) CheckAllFilesIntegrity() (int, int, error) {
	startTime := time.Now()

	var checkedCount, corruptedCount int

	err := filepath.Walk(*cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 跳过索引文件（它们会定期更新）
		if isIndexFile(path) {
			return nil
		}

		// 验证文件完整性
		valid, err := d.VerifyFileIntegrity(path)
		if err != nil {
			log.Println(i18n.T("FileIntegrityCheckError", map[string]any{
				"File":  path,
				"Error": err,
			}))
			return nil // 继续检查其他文件
		}

		checkedCount++

		if !valid {
			corruptedCount++

			// 如果启用自动修复，立即修复
			if d.enableAutoRepair {
				if err := d.RepairCorruptedFile(path); err != nil {
					log.Println(i18n.T("AutoRepairFailed", map[string]any{
						"File":  path,
						"Error": err,
					}))
				}
			}
		}

		return nil
	})

	if err != nil {
		return checkedCount, corruptedCount, err
	}

	duration := time.Since(startTime)
	monitoring.RecordDataIntegrityCheck(duration)

	d.updateLastCheckTime()

	log.Println(i18n.T("DataIntegrityCheckComplete", map[string]any{
		"Checked":   checkedCount,
		"Corrupted": corruptedCount,
		"Duration":  duration,
	}))

	return checkedCount, corruptedCount, nil
}

// GetCorruptedFiles 获取损坏文件列表
func (d *DataIntegrityManager) GetCorruptedFiles() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	files := make([]string, 0, len(d.corruptedFiles))
	for file := range d.corruptedFiles {
		files = append(files, file)
	}
	return files
}

// GetStats 获取统计信息
func (d *DataIntegrityManager) GetStats() (totalFiles int, corruptedFiles int, lastCheck time.Time) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	totalFiles = len(d.fileHashes)
	corruptedFiles = len(d.corruptedFiles)
	lastCheck = d.lastCheckTime
	return
}

// StartPeriodicCheck 启动定期检查
func (d *DataIntegrityManager) StartPeriodicCheck() {
	if !d.enablePeriodicCheck || d.checkInterval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(d.checkInterval)
		defer ticker.Stop()

		for range ticker.C {
			checked, corrupted, err := d.CheckAllFilesIntegrity()
			if err != nil {
				log.Println(i18n.T("PeriodicIntegrityCheckFailed", map[string]any{"Error": err}))
			} else {
				log.Println(i18n.T("PeriodicIntegrityCheckComplete", map[string]any{
					"Checked":   checked,
					"Corrupted": corrupted,
				}))
			}
		}
	}()
}

// calculateHash 计算数据的SHA256哈希
func (d *DataIntegrityManager) calculateHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// calculateFileHash 计算文件的SHA256哈希
func (d *DataIntegrityManager) calculateFileHash(filePath string) (string, error) {
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

// CleanupOrphanedHashes 清理孤立的哈希记录（文件已不存在但哈希记录还在）
func (d *DataIntegrityManager) CleanupOrphanedHashes() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	var cleanedCount int

	for filePath := range d.fileHashes {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			delete(d.fileHashes, filePath)
			delete(d.corruptedFiles, filePath)
			cleanedCount++

			// 如果数据库可用，从数据库中删除
			if d.db != nil {
				if err := d.deleteHashFromDB(filePath); err != nil {
					log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
				}
			}
		}
	}

	if cleanedCount > 0 {
		log.Println(i18n.T("OrphanedHashesCleaned", map[string]any{"Count": cleanedCount}))
	}

	return cleanedCount
}

// getHashFromDB 从数据库获取单个哈希
func (d *DataIntegrityManager) getHashFromDB(filePath string) (string, error) {
	if d.db == nil {
		return "", nil // 数据库不可用，回退到内存模式
	}

	var hash string
	err := d.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("file_hashes"))
		if bucket == nil {
			return nil // 桶不存在
		}
		data := bucket.Get([]byte(filePath))
		if data != nil {
			hash = string(data)
		}
		return nil
	})
	return hash, err
}

// saveHashToDB 保存单个哈希到数据库
func (d *DataIntegrityManager) saveHashToDB(filePath string, hash string) error {
	if d.db == nil {
		return nil // 数据库不可用，回退到内存模式
	}

	return d.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("file_hashes"))
		if err != nil {
			return errors.New(i18n.T("CreateDatabaseBucketFailed", map[string]any{"Error": err}))
		}
		return bucket.Put([]byte(filePath), []byte(hash))
	})
}

// deleteHashFromDB 从数据库中删除哈希
func (d *DataIntegrityManager) deleteHashFromDB(filePath string) error {
	if d.db == nil {
		return nil // 数据库不可用，回退到内存模式
	}

	return d.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("file_hashes"))
		if bucket == nil {
			return nil // 桶不存在，无需删除
		}
		return bucket.Delete([]byte(filePath))
	})
}

// LoadHashes 从数据库加载所有文件哈希
func (d *DataIntegrityManager) LoadHashes() error {
	if d.db == nil {
		return nil // 数据库不可用，回退到内存模式
	}

	return d.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("file_hashes"))
		if bucket == nil {
			return nil // 桶不存在，没有数据可加载
		}

		fileHashes := make(map[string]string)

		err := bucket.ForEach(func(k, v []byte) error {
			fileHashes[string(k)] = string(v)
			return nil
		})

		if err != nil {
			return errors.New(i18n.T("LoadDatabaseFailed", map[string]any{"Error": err}))
		}

		// 更新管理器中的哈希数据
		d.mu.Lock()
		defer d.mu.Unlock()
		d.fileHashes = fileHashes

		log.Println(i18n.T("FileHashesLoaded", map[string]any{"Count": len(fileHashes)}))
		return nil
	})
}

// Close 关闭数据库连接
func (d *DataIntegrityManager) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}
