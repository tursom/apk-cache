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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/tursom/apk-cache/utils/i18n"
)

// DataIntegrityManager 数据完整性管理器
type DataIntegrityManager struct {
	mu                  sync.RWMutex
	fileHashes          map[string]string // 文件路径 -> SHA256哈希
	corruptedFiles      map[string]bool   // 损坏文件列表
	lastCheckTime       time.Time
	checkInterval       time.Duration
	enableAutoRepair    bool
	enablePeriodicCheck bool
}

// 数据完整性相关的 Prometheus 指标
var (
	dataIntegrityChecks = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_data_integrity_checks_total",
		Help: "Total number of data integrity checks performed",
	})

	dataIntegrityCorruptedFiles = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "apk_cache_data_integrity_corrupted_files_total",
		Help: "Total number of corrupted files detected",
	})

	dataIntegrityRepairedFiles = promauto.NewCounter(prometheus.CounterOpts{
		Name: "apk_cache_data_integrity_repaired_files_total",
		Help: "Total number of corrupted files repaired",
	})

	dataIntegrityCheckDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "apk_cache_data_integrity_check_duration_seconds",
		Help:    "Duration of data integrity checks",
		Buckets: prometheus.DefBuckets,
	})
)

// NewDataIntegrityManager 创建新的数据完整性管理器
func NewDataIntegrityManager(checkInterval time.Duration, enableAutoRepair bool, enablePeriodicCheck bool) *DataIntegrityManager {
	return &DataIntegrityManager{
		fileHashes:          make(map[string]string),
		corruptedFiles:      make(map[string]bool),
		checkInterval:       checkInterval,
		enableAutoRepair:    enableAutoRepair,
		enablePeriodicCheck: enablePeriodicCheck,
	}
}

// RecordFileHash 记录文件的哈希值
func (d *DataIntegrityManager) RecordFileHash(filePath string, data []byte) error {
	hash := d.calculateHash(data)

	d.mu.Lock()
	defer d.mu.Unlock()

	d.fileHashes[filePath] = hash
	return nil
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

	// 获取记录的哈希
	d.mu.RLock()
	recordedHash, exists := d.fileHashes[filePath]
	d.mu.RUnlock()

	if !exists {
		// 如果没有记录哈希，记录当前哈希
		d.mu.Lock()
		d.fileHashes[filePath] = currentHash
		d.mu.Unlock()
		return true, nil
	}

	// 比较哈希值
	if currentHash != recordedHash {
		d.mu.Lock()
		d.corruptedFiles[filePath] = true
		dataIntegrityCorruptedFiles.Inc()
		d.mu.Unlock()

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

	// 从哈希记录中移除
	d.mu.Lock()
	delete(d.fileHashes, filePath)
	delete(d.corruptedFiles, filePath)
	d.mu.Unlock()

	dataIntegrityRepairedFiles.Inc()
	dataIntegrityCorruptedFiles.Dec()

	log.Println(i18n.T("FileRepaired", map[string]any{"File": filePath}))
	return nil
}

// CheckAllFilesIntegrity 检查所有文件的完整性
func (d *DataIntegrityManager) CheckAllFilesIntegrity() (int, int, error) {
	startTime := time.Now()
	dataIntegrityChecks.Inc()

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
	dataIntegrityCheckDuration.Observe(duration.Seconds())

	d.mu.Lock()
	d.lastCheckTime = time.Now()
	d.mu.Unlock()

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

// InitializeExistingFiles 初始化现有文件的哈希记录
func (d *DataIntegrityManager) InitializeExistingFiles() error {
	log.Println(i18n.T("InitializingFileHashes", nil))

	var initializedCount int

	err := filepath.Walk(*cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || isIndexFile(path) {
			return nil
		}

		hash, err := d.calculateFileHash(path)
		if err != nil {
			log.Println(i18n.T("CalculateFileHashFailed", map[string]any{
				"File":  path,
				"Error": err,
			}))
			return nil // 继续处理其他文件
		}

		d.mu.Lock()
		defer d.mu.Unlock()
		d.fileHashes[path] = hash

		initializedCount++

		return nil
	})

	if err != nil {
		return err
	}

	log.Println(i18n.T("FileHashesInitialized", map[string]any{"Count": initializedCount}))
	return nil
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
		}
	}

	if cleanedCount > 0 {
		log.Println(i18n.T("OrphanedHashesCleaned", map[string]any{"Count": cleanedCount}))
	}

	return cleanedCount
}
