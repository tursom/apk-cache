package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tursom/apk-cache/utils/i18n"
)

// CacheQuota 缓存配额管理器
type CacheQuota struct {
	MaxSize     int64         // 最大缓存大小（字节）
	CurrentSize int64         // 当前缓存大小（字节）
	Strategy    CleanStrategy // 清理策略
	mu          sync.RWMutex
}

// fileInfo 文件信息结构
type fileInfo struct {
	path    string
	size    int64
	atime   time.Time
	modTime time.Time
}

// CleanStrategy 清理策略
type CleanStrategy int

const (
	LRU  CleanStrategy = iota // 最近最少使用（默认）
	LFU                       // 最不经常使用
	FIFO                      // 先进先出
)


// NewCacheQuota 创建新的缓存配额管理器
func NewCacheQuota(maxSize int64, strategy CleanStrategy) *CacheQuota {
	quota := &CacheQuota{
		MaxSize:  maxSize,
		Strategy: strategy,
	}

	// 初始化 Prometheus 指标
	monitoring.CacheQuotaSize.WithLabelValues("max").Set(float64(maxSize))
	monitoring.CacheQuotaSize.WithLabelValues("current").Set(0)

	return quota
}

// CheckAndUpdateQuota 检查配额并更新当前大小
func (q *CacheQuota) CheckAndUpdateQuota(fileSize int64) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// 如果配额为0，表示不限制大小
	if q.MaxSize == 0 {
		q.CurrentSize += fileSize
		q.updateMetrics()
		return true, nil
	}

	// 检查是否有足够空间
	if q.CurrentSize+fileSize <= q.MaxSize {
		q.CurrentSize += fileSize
		q.updateMetrics()
		return true, nil
	}

	// 空间不足，需要清理
	log.Println(i18n.T("CacheQuotaExceeded", map[string]any{
		"Current": q.CurrentSize,
		"Max":     q.MaxSize,
		"Need":    fileSize,
	}))

	// 尝试清理缓存
	freed, err := q.cleanupCache(fileSize)
	if err != nil {
		return false, err
	}

	// 检查清理后是否有足够空间
	if q.CurrentSize+fileSize <= q.MaxSize {
		q.CurrentSize += fileSize
		q.updateMetrics()
		return true, nil
	}

	return false, errors.New(i18n.T("CacheQuotaInsufficient", map[string]any{
		"Current": q.CurrentSize,
		"Max":     q.MaxSize,
		"Need":    fileSize,
		"Freed":   freed,
	}))
}

// RemoveFile 从配额中移除文件大小
func (q *CacheQuota) RemoveFile(fileSize int64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.CurrentSize -= fileSize
	if q.CurrentSize < 0 {
		q.CurrentSize = 0
	}
	q.updateMetrics()
}

// GetUsage 获取缓存使用情况
func (q *CacheQuota) GetUsage() (current int64, max int64, percentage float64) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	current = q.CurrentSize
	max = q.MaxSize
	if max > 0 {
		percentage = float64(current) / float64(max) * 100
	}
	return
}

// cleanupCache 清理缓存以释放空间
func (q *CacheQuota) cleanupCache(needSize int64) (int64, error) {
	log.Println(i18n.T("CacheQuotaCleanup", map[string]any{
		"NeedSize": needSize,
		"Strategy": q.Strategy.String(),
	}))

	monitoring.RecordCacheQuotaCleanup(0) // 字节数稍后记录

	var files []fileInfo
	var err error

	// 根据策略获取要清理的文件列表
	switch q.Strategy {
	case LRU:
		files, err = q.getLRUFiles()
	case LFU:
		files, err = q.getLFUFiles()
	case FIFO:
		files, err = q.getFIFOFiles()
	default:
		files, err = q.getLRUFiles()
	}

	if err != nil {
		return 0, err
	}

	freed := int64(0)
	filesDeleted := 0

	// 删除文件直到释放足够空间
	for _, file := range files {
		if freed >= needSize {
			break
		}

		if err := os.Remove(file.path); err != nil {
			log.Println(i18n.T("CacheQuotaDeleteFailed", map[string]any{
				"File":  file.path,
				"Error": err,
			}))
			continue
		}

		freed += file.size
		filesDeleted++
		q.CurrentSize -= file.size

		// 从访问时间跟踪器中移除
		accessTimeTracker.Remove(file.path)

		log.Println(i18n.T("CacheQuotaFileDeleted", map[string]any{
			"File": file.path,
			"Size": file.size,
		}))
	}

	monitoring.RecordCacheQuotaCleanup(freed)
	log.Println(i18n.T("CacheQuotaCleanupComplete", map[string]any{
		"Freed":        freed,
		"FilesDeleted": filesDeleted,
	}))

	return freed, nil
}

// parseCleanStrategy 解析清理策略字符串
func parseCleanStrategy(strategy string) CleanStrategy {
	switch strings.ToUpper(strategy) {
	case "LFU":
		return LFU
	case "FIFO":
		return FIFO
	default:
		return LRU
	}
}

// getLRUFiles 获取最近最少使用的文件列表（按访问时间排序）
func (q *CacheQuota) getLRUFiles() ([]fileInfo, error) {
	var files []fileInfo

	err := filepath.Walk(*cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录和索引文件（优先保留索引文件）
		if info.IsDir() || isIndexFile(path) {
			return nil
		}

		// 获取访问时间
		var atime time.Time
		if memAtime, ok := accessTimeTracker.GetAccessTime(path); ok {
			atime = memAtime
		} else {
			// 从文件系统获取
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				atime = time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
			} else {
				atime = info.ModTime() // 回退到修改时间
			}
		}

		files = append(files, fileInfo{
			path:    path,
			size:    info.Size(),
			atime:   atime,
			modTime: info.ModTime(),
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	// 按访问时间排序（最早的在前）
	sortFilesByAccessTime(files)

	return files, nil
}

// getLFUFiles 获取最不经常使用的文件列表（需要额外的使用计数跟踪）
func (q *CacheQuota) getLFUFiles() ([]fileInfo, error) {
	// 简化实现：使用 LRU 策略
	// 在实际实现中，需要维护文件访问计数
	return q.getLRUFiles()
}

// getFIFOFiles 获取先进先出的文件列表（按修改时间排序）
func (q *CacheQuota) getFIFOFiles() ([]fileInfo, error) {
	var files []fileInfo

	err := filepath.Walk(*cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || isIndexFile(path) {
			return nil
		}

		files = append(files, fileInfo{
			path:    path,
			size:    info.Size(),
			modTime: info.ModTime(),
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	// 按修改时间排序（最早的在前）
	sortFilesByModTime(files)

	return files, nil
}

// sortFilesByAccessTime 按访问时间排序
func sortFilesByAccessTime(files []fileInfo) {
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[i].atime.After(files[j].atime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
}

// sortFilesByModTime 按修改时间排序
func sortFilesByModTime(files []fileInfo) {
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[i].modTime.After(files[j].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
}

// updateMetrics 更新 Prometheus 指标
func (q *CacheQuota) updateMetrics() {
	monitoring.UpdateCacheQuotaMetrics(q.CurrentSize, 0) // 文件数稍后更新
}

// String 方法返回清理策略的字符串表示
func (s CleanStrategy) String() string {
	switch s {
	case LRU:
		return "LRU"
	case LFU:
		return "LFU"
	case FIFO:
		return "FIFO"
	default:
		return "Unknown"
	}
}

// InitializeCacheSize 初始化缓存大小统计
func (q *CacheQuota) InitializeCacheSize() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	totalSize := int64(0)
	fileCount := 0

	err := filepath.Walk(*cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			totalSize += info.Size()
			fileCount++
		}

		return nil
	})

	if err != nil {
		return err
	}

	q.CurrentSize = totalSize
	monitoring.UpdateCacheQuotaMetrics(totalSize, fileCount)
	q.updateMetrics()

	log.Println(i18n.T("CacheSizeInitialized", map[string]any{
		"Size":  totalSize,
		"Files": fileCount,
	}))

	return nil
}
