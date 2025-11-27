package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/tursom/apk-cache/utils/i18n"
)

// ComponentStatus 组件状态
type ComponentStatus struct {
	Status     string    `json:"status"`
	LastCheck  time.Time `json:"last_check"`
	Error      string    `json:"error,omitempty"`
	RetryCount int       `json:"retry_count"`
}

// SystemStatus 系统状态
type SystemStatus struct {
	mu     sync.RWMutex
	status map[string]ComponentStatus
}

// SetStatus 设置组件状态
func (s *SystemStatus) SetStatus(component string, status ComponentStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[component] = status
}

// GetStatus 获取组件状态
func (s *SystemStatus) GetStatus(component string) (ComponentStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status, exists := s.status[component]
	return status, exists
}

// GetAllStatus 获取所有组件状态
func (s *SystemStatus) GetAllStatus() map[string]ComponentStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]ComponentStatus)
	for k, v := range s.status {
		result[k] = v
	}
	return result
}

// HealthCheckManager 健康检查管理器
type HealthCheckManager struct {
	systemStatus *SystemStatus
}

// NewHealthCheckManager 创建健康检查管理器
func NewHealthCheckManager() *HealthCheckManager {
	return &HealthCheckManager{
		systemStatus: &SystemStatus{
			mu: sync.RWMutex{},
			status: map[string]ComponentStatus{
				"upstream":     {Status: "healthy", LastCheck: time.Now()},
				"filesystem":   {Status: "healthy", LastCheck: time.Now()},
				"memory_cache": {Status: "healthy", LastCheck: time.Now()},
				"cache_quota":  {Status: "healthy", LastCheck: time.Now()},
			},
		},
	}
}

// 健康检查相关的 Prometheus 指标
var (
	healthCheckStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "apk_cache_health_status",
		Help: "Health status of system components (1 = healthy, 0 = unhealthy)",
	}, []string{"component"})

	healthCheckDuration = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "apk_cache_health_check_duration_seconds",
		Help: "Duration of health checks in seconds",
	}, []string{"component"})

	healthCheckErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "apk_cache_health_check_errors_total",
		Help: "Total number of health check errors",
	}, []string{"component"})
)

// CheckUpstreamHealth 检查上游服务器健康状态
func (h *HealthCheckManager) CheckUpstreamHealth() {
	start := time.Now()
	status := ComponentStatus{
		Status:    "healthy",
		LastCheck: time.Now(),
	}

	// 使用上游管理器检查健康状态
	healthyCount := upstreamManager.GetHealthyCount()
	totalCount := upstreamManager.GetServerCount()

	if healthyCount == 0 {
		status.Status = "unhealthy"
		status.Error = "No healthy upstream servers available"
		healthCheckErrors.WithLabelValues("upstream").Inc()
	} else if healthyCount < totalCount {
		status.Status = "degraded"
		status.Error = fmt.Sprintf("Only %d/%d upstream servers healthy", healthyCount, totalCount)
	}

	h.systemStatus.SetStatus("upstream", status)
	duration := time.Since(start).Seconds()
	healthCheckDuration.WithLabelValues("upstream").Set(duration)

	if status.Status == "healthy" {
		healthCheckStatus.WithLabelValues("upstream").Set(1)
	} else {
		healthCheckStatus.WithLabelValues("upstream").Set(0)
	}
}

// CheckFilesystemHealth 检查文件系统健康状态
func (h *HealthCheckManager) CheckFilesystemHealth() {
	start := time.Now()
	status := ComponentStatus{
		Status:    "healthy",
		LastCheck: time.Now(),
	}

	// 检查缓存目录是否存在且可写
	if _, err := os.Stat(*cachePath); os.IsNotExist(err) {
		// 尝试创建目录
		if err := os.MkdirAll(*cachePath, 0755); err != nil {
			status.Status = "unhealthy"
			status.Error = fmt.Sprintf("Cache directory does not exist and cannot be created: %v", err)
			healthCheckErrors.WithLabelValues("filesystem").Inc()
		}
	}

	// 检查目录是否可写
	if status.Status == "healthy" {
		testFile := filepath.Join(*cachePath, ".health_check")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			status.Status = "unhealthy"
			status.Error = fmt.Sprintf("Cache directory is not writable: %v", err)
			healthCheckErrors.WithLabelValues("filesystem").Inc()
		} else {
			os.Remove(testFile) // 清理测试文件
		}
	}

	// 检查磁盘空间
	if status.Status == "healthy" {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(*cachePath, &stat); err != nil {
			status.Status = "degraded"
			status.Error = fmt.Sprintf("Cannot check disk space: %v", err)
		} else {
			// 计算可用空间百分比
			available := stat.Bavail * uint64(stat.Bsize)
			total := stat.Blocks * uint64(stat.Bsize)
			availablePercent := float64(available) / float64(total) * 100

			if availablePercent < 5 {
				status.Status = "degraded"
				status.Error = fmt.Sprintf("Low disk space: %.1f%% available", availablePercent)
			}
		}
	}

	h.systemStatus.SetStatus("filesystem", status)
	duration := time.Since(start).Seconds()
	healthCheckDuration.WithLabelValues("filesystem").Set(duration)

	if status.Status == "healthy" {
		healthCheckStatus.WithLabelValues("filesystem").Set(1)
	} else {
		healthCheckStatus.WithLabelValues("filesystem").Set(0)
	}
}

// CheckMemoryCacheHealth 检查内存缓存健康状态
func (h *HealthCheckManager) CheckMemoryCacheHealth() {
	start := time.Now()
	status := ComponentStatus{
		Status:    "healthy",
		LastCheck: time.Now(),
	}

	if memoryCache != nil {
		currentSize, maxSize, itemCount, _ := memoryCache.GetStats()
		if maxSize > 0 && float64(currentSize)/float64(maxSize) > 0.95 {
			status.Status = "degraded"
			status.Error = fmt.Sprintf("Memory cache nearly full: %d/%d bytes (%d items)", currentSize, maxSize, itemCount)
		}
	} else {
		status.Status = "disabled"
	}

	h.systemStatus.SetStatus("memory_cache", status)
	duration := time.Since(start).Seconds()
	healthCheckDuration.WithLabelValues("memory_cache").Set(duration)

	if status.Status == "healthy" || status.Status == "disabled" {
		healthCheckStatus.WithLabelValues("memory_cache").Set(1)
	} else {
		healthCheckStatus.WithLabelValues("memory_cache").Set(0)
	}
}

// CheckCacheQuotaHealth 检查缓存配额健康状态
func (h *HealthCheckManager) CheckCacheQuotaHealth() {
	start := time.Now()
	status := ComponentStatus{
		Status:    "healthy",
		LastCheck: time.Now(),
	}

	if cacheQuota != nil {
		_, maxSize, percentage := cacheQuota.GetUsage()
		if maxSize > 0 && percentage > 95 {
			status.Status = "degraded"
			status.Error = fmt.Sprintf("Cache quota nearly exceeded: %.1f%% used", percentage)
		}
	} else {
		status.Status = "disabled"
	}

	h.systemStatus.SetStatus("cache_quota", status)
	duration := time.Since(start).Seconds()
	healthCheckDuration.WithLabelValues("cache_quota").Set(duration)

	if status.Status == "healthy" || status.Status == "disabled" {
		healthCheckStatus.WithLabelValues("cache_quota").Set(1)
	} else {
		healthCheckStatus.WithLabelValues("cache_quota").Set(0)
	}
}

// PerformHealthChecks 执行所有健康检查
func (h *HealthCheckManager) PerformHealthChecks() {
	h.CheckUpstreamHealth()
	h.CheckFilesystemHealth()
	h.CheckMemoryCacheHealth()
	h.CheckCacheQuotaHealth()
}

// StartHealthCheckLoop 启动健康检查循环
func (h *HealthCheckManager) StartHealthCheckLoop() {
	if *healthCheckInterval <= 0 {
		log.Println(i18n.T("HealthCheckDisabled", nil))
		return
	}

	log.Println(i18n.T("HealthCheckEnabled", map[string]any{
		"Interval": *healthCheckInterval,
		"Timeout":  *healthCheckTimeout,
	}))

	ticker := time.NewTicker(*healthCheckInterval)
	defer ticker.Stop()

	// 立即执行一次健康检查
	h.PerformHealthChecks()

	for range ticker.C {
		h.PerformHealthChecks()
	}
}

// HealthCheckHandler 健康检查 HTTP 处理器
func (h *HealthCheckManager) HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 执行快速健康检查
	h.PerformHealthChecks()

	allStatus := h.systemStatus.GetAllStatus()

	// 确定整体健康状态
	overallHealthy := true
	for _, status := range allStatus {
		if status.Status == "unhealthy" {
			overallHealthy = false
			break
		}
	}

	response := map[string]any{
		"status":     "healthy",
		"timestamp":  time.Now(),
		"components": allStatus,
	}

	if !overallHealthy {
		response["status"] = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(response)
}

// SelfHeal 自愈机制
func (h *HealthCheckManager) SelfHeal() {
	if !*enableSelfHealing {
		return
	}

	log.Println(i18n.T("HealthCheckSelfHealingTriggered", map[string]any{
		"Component": "system",
		"Action":    "starting self-healing process",
	}))

	// 检查上游服务器自愈
	if status, exists := h.systemStatus.GetStatus("upstream"); exists && status.Status == "unhealthy" {
		h.healUpstream()
	}

	// 检查文件系统自愈
	if status, exists := h.systemStatus.GetStatus("filesystem"); exists && status.Status == "unhealthy" {
		h.healFilesystem()
	}

	// 检查内存缓存自愈
	if status, exists := h.systemStatus.GetStatus("memory_cache"); exists && status.Status == "degraded" {
		h.healMemoryCache()
	}

	log.Println(i18n.T("HealthCheckSelfHealingCompleted", map[string]any{
		"Component": "system",
	}))
}

// healUpstream 上游服务器自愈
func (h *HealthCheckManager) healUpstream() {
	log.Println(i18n.T("HealthCheckSelfHealingTriggered", map[string]any{
		"Component": "upstream",
		"Action":    "attempting to recover upstream servers",
	}))
	// 这里可以实现更复杂的自愈逻辑，比如：
	// - 重新尝试连接失败的上游服务器
	// - 调整上游服务器优先级
	// - 清除可能损坏的连接池
}

// healFilesystem 文件系统自愈
func (h *HealthCheckManager) healFilesystem() {
	log.Println(i18n.T("HealthCheckSelfHealingTriggered", map[string]any{
		"Component": "filesystem",
		"Action":    "repairing cache directory permissions and structure",
	}))

	// 尝试修复缓存目录权限
	if err := os.Chmod(*cachePath, 0755); err != nil {
		log.Println(i18n.T("HealthCheckSelfHealingFailed", map[string]any{
			"Component": "filesystem",
			"Error":     err,
		}))
		return
	}

	// 检查并重新创建必要的子目录
	subdirs := []string{"x86_64", "aarch64", "x86", "armhf"}
	for _, subdir := range subdirs {
		dir := filepath.Join(*cachePath, subdir)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Println(i18n.T("HealthCheckSelfHealingFailed", map[string]any{
				"Component": "filesystem",
				"Error":     fmt.Sprintf("failed to create subdirectory %s: %v", dir, err),
			}))
		}
	}

	log.Println(i18n.T("HealthCheckSelfHealingCompleted", map[string]any{
		"Component": "filesystem",
	}))
}

// healMemoryCache 内存缓存自愈
func (h *HealthCheckManager) healMemoryCache() {
	log.Println(i18n.T("HealthCheckSelfHealingTriggered", map[string]any{
		"Component": "memory_cache",
		"Action":    "cleaning up expired memory cache items",
	}))

	if memoryCache != nil {
		// 清理过期的内存缓存项
		memoryCache.cleanupExpired()
		log.Println(i18n.T("HealthCheckSelfHealingCompleted", map[string]any{
			"Component": "memory_cache",
		}))
	}
}

// GetSystemStatus 获取系统状态（用于其他模块）
func (h *HealthCheckManager) GetSystemStatus() *SystemStatus {
	return h.systemStatus
}
