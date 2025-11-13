package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
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
	log.Println("开始清理过期缓存...")
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
		isIndex := strings.HasSuffix(path, "/APKINDEX.tar.gz")

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
				log.Printf("删除过期文件失败 %s: %v\n", path, err)
			} else {
				deletedCount++
				deletedSize += size
				// 从内存中移除访问时间记录
				accessTimeTracker.Remove(path)
				log.Printf("已删除过期文件: %s (%.2f MB)\n", path, float64(size)/(1024*1024))
			}
		}

		return nil
	})

	if err != nil {
		log.Printf("清理过程出错: %v\n", err)
	}

	duration := time.Since(startTime)
	log.Printf("清理完成: 删除 %d 个文件，释放 %.2f MB，耗时 %v\n",
		deletedCount, float64(deletedSize)/(1024*1024), duration)
}
