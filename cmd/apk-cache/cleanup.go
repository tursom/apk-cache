package main

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

// startAutoCleanup 启动自动清理协程
func startAutoCleanup() {
	ticker := time.NewTicker(*cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		cleanupExpiredCache()
	}
}

// cleanupExpiredCache 清理过期缓存
func cleanupExpiredCache() {
	log.Println(t("StartCleanupExpiredCache", nil))
	startTime := time.Now()

	var deletedCount int64
	var deletedSize int64

	err := filepath.Walk(*cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 检查文件是否过期
		isIndex := isIndexFile(path)

		var expired bool
		if isIndex {
			// APKINDEX 文件按修改时间过期
			expired = isCacheExpiredByModTime(path, *indexCacheDuration)
		} else {
			// 普通包文件按访问时间过期，0 表示永不过期
			if *pkgCacheDuration > 0 {
				expired = isCacheExpiredByAccessTime(path, *pkgCacheDuration)
			}
		}

		if expired {
			size := info.Size()
			if err := os.Remove(path); err != nil {
				log.Println(t("DeleteExpiredFileFailed", map[string]any{"Path": path, "Error": err}))
			} else {
				deletedCount++
				deletedSize += size
				// 从内存中移除访问时间记录
				accessTimeTracker.Remove(path)
				// 从数据完整性管理器中移除哈希记录
				if dataIntegrityManager != nil {
					dataIntegrityManager.mu.Lock()
					delete(dataIntegrityManager.fileHashes, path)
					delete(dataIntegrityManager.corruptedFiles, path)
					dataIntegrityManager.mu.Unlock()
				}
				// 更新缓存配额统计
				if cacheQuota != nil {
					cacheQuota.RemoveFile(size)
				}
				log.Println(t("DeletedExpiredFile", map[string]any{
					"Path": path,
					"Size": float64(size) / (1024 * 1024),
				}))
			}
		}

		return nil
	})

	if err != nil {
		log.Println(t("CleanupError", map[string]any{"Error": err}))
	}

	duration := time.Since(startTime)
	log.Println(t("CleanupComplete", map[string]any{
		"Count":    deletedCount,
		"Size":     float64(deletedSize) / (1024 * 1024),
		"Duration": duration,
	}))
}
