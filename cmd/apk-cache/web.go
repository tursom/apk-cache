package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func adminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/_admin/" || path == "/_admin":
		serveAdminDashboard(w, r)
	case path == "/_admin/stats":
		serveAdminStats(w, r)
	case path == "/_admin/clear":
		handleCacheClear(w, r)
	default:
		http.NotFound(w, r)
	}
}

func serveAdminDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(adminHTML))
}

func serveAdminStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 获取 Prometheus metrics
	cacheHitsVal := getMetricValue(cacheHits)
	cacheMissesVal := getMetricValue(cacheMisses)
	downloadBytesVal := getMetricValue(downloadBytes)

	// 计算缓存大小
	cacheSize, _ := getDirSize(*cachePath)

	stats := map[string]interface{}{
		"cache_hits":           int64(cacheHitsVal),
		"cache_misses":         int64(cacheMissesVal),
		"download_bytes":       int64(downloadBytesVal),
		"active_locks":         lockManager.Size(),
		"tracked_files":        accessTimeTracker.Size(),
		"cache_size":           cacheSize,
		"listen_addr":          *listenAddr,
		"cache_dir":            *cachePath,
		"upstream":             *upstreamURL,
		"index_cache_duration": indexCacheDuration.String(),
		"pkg_cache_duration":   pkgCacheDuration.String(),
		"cleanup_interval":     cleanupInterval.String(),
		"proxy":                *proxyURL,
	}

	// 添加缓存配额信息
	if cacheQuota != nil {
		currentSize, maxSize, percentage := cacheQuota.GetUsage()
		stats["quota_current_size"] = currentSize
		stats["quota_max_size"] = maxSize
		stats["quota_usage_percentage"] = percentage
		stats["quota_clean_strategy"] = cacheQuota.Strategy.String()
	}

	// 添加健康检查信息
	if healthCheckManager != nil {
		healthStatus := healthCheckManager.GetSystemStatus().GetAllStatus()
		stats["health_status"] = healthStatus

		// 计算整体健康状态
		overallHealthy := true
		for _, status := range healthStatus {
			if status.Status == "unhealthy" {
				overallHealthy = false
				break
			}
		}
		stats["overall_health"] = overallHealthy

		// 添加上游服务器健康状态
		upstreamHealth := make(map[string]interface{})
		servers := upstreamManager.GetAllServers()
		for i, server := range servers {
			upstreamHealth[fmt.Sprintf("upstream_%d", i)] = map[string]interface{}{
				"url":    server.GetURL(),
				"proxy":  server.GetProxy(),
				"name":   server.GetName(),
				"health": server.IsHealthy(),
			}
		}
		stats["upstream_health"] = upstreamHealth
	}

	json.NewEncoder(w).Encode(stats)
}

func handleCacheClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// 删除缓存目录中的所有文件
	err := os.RemoveAll(*cachePath)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("Failed to clear cache: %v", err),
		})
		return
	}

	// 清空访问时间跟踪器
	accessTimeTracker = NewAccessTimeTracker()

	// 重置缓存配额统计
	if cacheQuota != nil {
		cacheQuota.RemoveFile(cacheQuota.CurrentSize)
	}

	// 重新创建缓存目录
	err = os.MkdirAll(*cachePath, 0755)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("Failed to recreate cache directory: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Cache cleared successfully",
	})
}

func getMetricValue(counter prometheus.Counter) float64 {
	// 使用 prometheus 的 Write 方法获取值
	metric := &dto.Metric{}
	counter.Write(metric)
	return metric.GetCounter().GetValue()
}

func getDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}
