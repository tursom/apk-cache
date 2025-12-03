package data_integrity

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tursom/apk-cache/utils"
	"github.com/tursom/apk-cache/utils/i18n"
	bolt "go.etcd.io/bbolt"
)

// persistentManager 持久化实现（使用 BoltDB）
type persistentManager struct {
	baseManager
	db *bolt.DB
}

// NewPersistentDataIntegrityManager 创建持久化数据完整性管理器
func NewPersistentDataIntegrityManager(cachePath string, db *bolt.DB, checkInterval time.Duration, enableAutoRepair bool, enablePeriodicCheck bool) Manager {
	manager := &persistentManager{
		baseManager: baseManager{
			checkInterval:       checkInterval,
			enableAutoRepair:    enableAutoRepair,
			enablePeriodicCheck: enablePeriodicCheck,
			cachePath:           cachePath,
		},
		db: db,
	}

	// 自动加载持久化的哈希数据
	if err := manager.LoadHashes(); err != nil {
		log.Println(i18n.T("LoadFileHashesFailed", map[string]any{"Error": err}))
	}

	return manager
}

// 数据库辅助方法
func (p *persistentManager) getHashFromDB(filePath string) (string, error) {
	var hash string
	err := p.db.View(func(tx *bolt.Tx) error {
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

func (p *persistentManager) saveHashToDB(filePath string, hash string) error {
	return p.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("file_hashes"))
		if err != nil {
			return errors.New(i18n.T("CreateDatabaseBucketFailed", map[string]any{"Error": err}))
		}
		return bucket.Put([]byte(filePath), []byte(hash))
	})
}

// markFileAsCorrupted 将文件标记为损坏（存储到数据库）
func (p *persistentManager) markFileAsCorrupted(filePath string) error {
	return p.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("corrupted_files"))
		if err != nil {
			return errors.New(i18n.T("CreateDatabaseBucketFailed", map[string]any{"Error": err}))
		}
		// 存储空值（布尔值用存在性表示）
		return bucket.Put([]byte(filePath), []byte{})
	})
}

// removeFileHash 删除文件的哈希记录（从两个桶中删除）
func (p *persistentManager) removeFileHash(filePath string) error {
	return p.db.Update(func(tx *bolt.Tx) error {
		// 从 file_hashes 桶删除
		bucket := tx.Bucket([]byte("file_hashes"))
		if bucket != nil {
			if err := bucket.Delete([]byte(filePath)); err != nil {
				return err
			}
		}
		// 从 corrupted_files 桶删除
		corruptedBucket := tx.Bucket([]byte("corrupted_files"))
		if corruptedBucket != nil {
			if err := corruptedBucket.Delete([]byte(filePath)); err != nil {
				return err
			}
		}
		return nil
	})
}

// getCorruptedFilesFromDB 从数据库获取损坏文件列表
func (p *persistentManager) getCorruptedFilesFromDB() ([]string, error) {
	var files []string
	err := p.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("corrupted_files"))
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(k, v []byte) error {
			files = append(files, string(k))
			return nil
		})
	})
	return files, err
}

// countCorruptedFilesFromDB 从数据库统计损坏文件数量
func (p *persistentManager) countCorruptedFilesFromDB() (int, error) {
	count := 0
	err := p.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("corrupted_files"))
		if bucket == nil {
			return nil
		}
		stats := bucket.Stats()
		count = stats.KeyN
		return nil
	})
	return count, err
}

func (p *persistentManager) LoadHashes() error {
	return p.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("file_hashes"))
		if bucket == nil {
			return nil // 桶不存在，没有数据可加载
		}

		count := 0
		err := bucket.ForEach(func(k, v []byte) error {
			count++
			return nil
		})

		if err != nil {
			return errors.New(i18n.T("LoadDatabaseFailed", map[string]any{"Error": err}))
		}

		log.Println(i18n.T("FileHashesLoaded", map[string]any{"Count": count}))
		return nil
	})
}

// RecordFileHash 记录文件的哈希值（持久化到数据库）
func (p *persistentManager) RecordFileHash(filePath string, data []byte) error {
	hash := p.calculateHash(data)

	// 保存到数据库
	if err := p.saveHashToDB(filePath, hash); err != nil {
		log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
	}

	return nil
}

// VerifyFileIntegrity 验证文件完整性（直接读取数据库）
func (p *persistentManager) VerifyFileIntegrity(filePath string) (bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, errors.New(i18n.T("ReadFileFailed", map[string]any{"Error": err}))
	}

	currentHash := p.calculateHash(data)
	// 直接从数据库查询哈希
	recordedHash, err := p.getHashFromDB(filePath)
	if err != nil {
		return false, err
	}

	if recordedHash == "" {
		// 如果没有记录哈希，记录当前哈希到数据库
		if err := p.saveHashToDB(filePath, currentHash); err != nil {
			log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
		}
		return true, nil
	}

	if currentHash != recordedHash {
		if err := p.markFileAsCorrupted(filePath); err != nil {
			log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
		}
		utils.Monitoring.RecordDataIntegrityCorrupted()

		log.Println(i18n.T("FileIntegrityCheckFailed", map[string]any{
			"File":     filePath,
			"Expected": recordedHash[:16] + "...",
			"Actual":   currentHash[:16] + "...",
		}))
		return false, nil
	}

	return true, nil
}

// RepairCorruptedFile 修复损坏的文件（从数据库删除）
func (p *persistentManager) RepairCorruptedFile(filePath string) error {
	log.Println(i18n.T("RepairingCorruptedFile", map[string]any{"File": filePath}))

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return errors.New(i18n.T("DeleteCorruptedFileFailed", map[string]any{"Error": err}))
	}

	// 从数据库中删除哈希和损坏标记
	if err := p.removeFileHash(filePath); err != nil {
		log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
	}

	utils.Monitoring.RecordDataIntegrityRepaired()

	log.Println(i18n.T("FileRepaired", map[string]any{"File": filePath}))
	return nil
}

// CheckAllFilesIntegrity 检查所有文件的完整性（使用持久化存储）
func (p *persistentManager) CheckAllFilesIntegrity() (int, int, error) {
	startTime := time.Now()

	var checkedCount, corruptedCount int

	err := filepath.Walk(p.cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if utils.IsIndexFile(path) {
			return nil
		}

		valid, err := p.VerifyFileIntegrity(path)
		if err != nil {
			log.Println(i18n.T("FileIntegrityCheckError", map[string]any{
				"File":  path,
				"Error": err,
			}))
			return nil
		}

		checkedCount++

		if !valid {
			corruptedCount++

			if p.enableAutoRepair {
				if err := p.RepairCorruptedFile(path); err != nil {
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
	utils.Monitoring.RecordDataIntegrityCheck(duration)

	p.updateLastCheckTime()

	log.Println(i18n.T("DataIntegrityCheckComplete", map[string]any{
		"Checked":   checkedCount,
		"Corrupted": corruptedCount,
		"Duration":  duration,
	}))

	return checkedCount, corruptedCount, nil
}

// GetCorruptedFiles 获取损坏文件列表
func (p *persistentManager) GetCorruptedFiles() []string {
	files, err := p.getCorruptedFilesFromDB()
	if err != nil {
		log.Println(i18n.T("LoadDatabaseFailed", map[string]any{"Error": err}))
		return []string{}
	}
	return files
}

// GetStats 获取统计信息
func (p *persistentManager) GetStats() (totalFiles int, corruptedFiles int, lastCheck time.Time) {
	// 从数据库计算总文件数
	totalFiles = 0
	err := p.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("file_hashes"))
		if bucket == nil {
			return nil
		}
		stats := bucket.Stats()
		totalFiles = stats.KeyN
		return nil
	})
	if err != nil {
		totalFiles = 0
	}

	// 从数据库计算损坏文件数
	corruptedFiles, err = p.countCorruptedFilesFromDB()
	if err != nil {
		corruptedFiles = 0
	}
	lastCheck = p.lastCheck()
	return
}

// StartPeriodicCheck 启动定期检查
func (p *persistentManager) StartPeriodicCheck() {
	if !p.enablePeriodicCheck || p.checkInterval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(p.checkInterval)
		defer ticker.Stop()

		for range ticker.C {
			checked, corrupted, err := p.CheckAllFilesIntegrity()
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

// CleanupOrphanedHashes 清理孤立的哈希记录（同时从数据库删除）
func (p *persistentManager) CleanupOrphanedHashes() int {
	var orphanedPaths []string

	// 第一次遍历：收集孤立的文件路径
	err := p.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("file_hashes"))
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(k, v []byte) error {
			filePath := string(k)
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				orphanedPaths = append(orphanedPaths, filePath)
			}
			return nil
		})
	})
	if err != nil {
		log.Println(i18n.T("LoadDatabaseFailed", map[string]any{"Error": err}))
		return 0
	}

	if len(orphanedPaths) == 0 {
		return 0
	}

	// 第二次遍历：从两个桶中删除
	err = p.db.Update(func(tx *bolt.Tx) error {
		fileHashesBucket := tx.Bucket([]byte("file_hashes"))
		corruptedBucket := tx.Bucket([]byte("corrupted_files"))
		// 如果桶不存在，则忽略
		for _, filePath := range orphanedPaths {
			if fileHashesBucket != nil {
				if err := fileHashesBucket.Delete([]byte(filePath)); err != nil {
					return err
				}
			}
			if corruptedBucket != nil {
				if err := corruptedBucket.Delete([]byte(filePath)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
		return 0
	}

	log.Println(i18n.T("OrphanedHashesCleaned", map[string]any{"Count": len(orphanedPaths)}))
	return len(orphanedPaths)
}

// RemoveFileHash 删除文件的哈希记录（同时从数据库删除）
func (p *persistentManager) RemoveFileHash(filePath string) error {
	if err := p.removeFileHash(filePath); err != nil {
		log.Println(i18n.T("SaveHashesFailed", map[string]any{"Error": err}))
	}
	return nil
}

// Close 关闭数据库连接
func (p *persistentManager) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}
