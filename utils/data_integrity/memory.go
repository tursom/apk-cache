package data_integrity

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tursom/apk-cache/utils"
	"github.com/tursom/apk-cache/utils/i18n"
)

// memoryDataIntegrityManager 纯内存实现
type memoryDataIntegrityManager struct {
	baseManager
	fileHashes     map[string]string // 文件路径 -> SHA256哈希
	corruptedFiles map[string]bool   // 损坏文件列表
}

// 辅助方法（线程安全） - 仅适用于 memoryDataIntegrityManager
func (m *memoryDataIntegrityManager) getFileHash(filePath string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hash, exists := m.fileHashes[filePath]
	return hash, exists
}

func (m *memoryDataIntegrityManager) setFileHash(filePath, hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fileHashes[filePath] = hash
}

func (m *memoryDataIntegrityManager) markFileAsCorrupted(filePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.corruptedFiles[filePath] = true
}

func (m *memoryDataIntegrityManager) removeFileHash(filePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.fileHashes, filePath)
	delete(m.corruptedFiles, filePath)
}

// NewMemoryDataIntegrityManager 创建纯内存数据完整性管理器
func NewMemoryDataIntegrityManager(
	cachePath string, checkInterval time.Duration, enableAutoRepair bool, enablePeriodicCheck bool,
) Manager {
	return &memoryDataIntegrityManager{
		baseManager: baseManager{
			checkInterval:       checkInterval,
			enableAutoRepair:    enableAutoRepair,
			enablePeriodicCheck: enablePeriodicCheck,
			cachePath:           cachePath,
		},
		fileHashes:     make(map[string]string),
		corruptedFiles: make(map[string]bool),
	}
}

// RecordFileHash 记录文件的哈希值（仅内存）
func (m *memoryDataIntegrityManager) RecordFileHash(filePath string, data []byte) error {
	hash := m.calculateHash(data)
	m.setFileHash(filePath, hash)
	return nil
}

// VerifyFileIntegrity 验证文件完整性（仅内存）
func (m *memoryDataIntegrityManager) VerifyFileIntegrity(filePath string) (bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, errors.New(i18n.T("ReadFileFailed", map[string]any{"Error": err}))
	}

	currentHash := m.calculateHash(data)
	recordedHash, exists := m.getFileHash(filePath)

	if !exists {
		// 如果没有记录哈希，记录当前哈希到内存缓存
		m.setFileHash(filePath, currentHash)
		return true, nil
	}

	if currentHash != recordedHash {
		m.markFileAsCorrupted(filePath)
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

// RepairCorruptedFile 修复损坏的文件（仅内存）
func (m *memoryDataIntegrityManager) RepairCorruptedFile(filePath string) error {
	log.Println(i18n.T("RepairingCorruptedFile", map[string]any{"File": filePath}))

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return errors.New(i18n.T("DeleteCorruptedFileFailed", map[string]any{"Error": err}))
	}

	m.removeFileHash(filePath)
	utils.Monitoring.RecordDataIntegrityRepaired()

	log.Println(i18n.T("FileRepaired", map[string]any{"File": filePath}))
	return nil
}

// CheckAllFilesIntegrity 检查所有文件的完整性（仅内存）
func (m *memoryDataIntegrityManager) CheckAllFilesIntegrity() (int, int, error) {
	startTime := time.Now()

	var checkedCount, corruptedCount int

	err := filepath.Walk(m.cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if utils.IsIndexFile(path) {
			return nil
		}

		valid, err := m.VerifyFileIntegrity(path)
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

			if m.enableAutoRepair {
				if err := m.RepairCorruptedFile(path); err != nil {
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

	m.updateLastCheckTime()

	log.Println(i18n.T("DataIntegrityCheckComplete", map[string]any{
		"Checked":   checkedCount,
		"Corrupted": corruptedCount,
		"Duration":  duration,
	}))

	return checkedCount, corruptedCount, nil
}

// GetCorruptedFiles 获取损坏文件列表
func (m *memoryDataIntegrityManager) GetCorruptedFiles() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	files := make([]string, 0, len(m.corruptedFiles))
	for file := range m.corruptedFiles {
		files = append(files, file)
	}
	return files
}

// GetStats 获取统计信息
func (m *memoryDataIntegrityManager) GetStats() (totalFiles int, corruptedFiles int, lastCheck time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalFiles = len(m.fileHashes)
	corruptedFiles = len(m.corruptedFiles)
	lastCheck = m.lastCheckTime
	return
}

// StartPeriodicCheck 启动定期检查
func (m *memoryDataIntegrityManager) StartPeriodicCheck() {
	if !m.enablePeriodicCheck || m.checkInterval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(m.checkInterval)
		defer ticker.Stop()

		for range ticker.C {
			checked, corrupted, err := m.CheckAllFilesIntegrity()
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

// CleanupOrphanedHashes 清理孤立的哈希记录
func (m *memoryDataIntegrityManager) CleanupOrphanedHashes() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	var cleanedCount int

	for filePath := range m.fileHashes {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			delete(m.fileHashes, filePath)
			delete(m.corruptedFiles, filePath)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		log.Println(i18n.T("OrphanedHashesCleaned", map[string]any{"Count": cleanedCount}))
	}

	return cleanedCount
}

// RemoveFileHash 删除文件的哈希记录
func (m *memoryDataIntegrityManager) RemoveFileHash(filePath string) error {
	m.removeFileHash(filePath)
	return nil
}

// Close 关闭（无操作）
func (m *memoryDataIntegrityManager) Close() error {
	return nil
}
