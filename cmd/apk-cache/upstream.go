package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/tursom/apk-cache/utils/i18n"
)

// UpstreamServer 上游服务器配置，集成健康检查功能
type UpstreamServer struct {
	URL   string
	Proxy string
	Name  string

	// 健康检查相关字段
	mu              sync.RWMutex
	lastHealthCheck time.Time
	isHealthy       bool
	healthCacheTTL  time.Duration
	lastError       string
	retryCount      int
	maxRetries      int
}

// UpstreamManager 上游服务器管理器
type UpstreamManager struct {
	servers []*UpstreamServer
	mu      sync.RWMutex
	current int
}

// NewUpstreamServer 创建新的上游服务器实例
func NewUpstreamServer(url, proxy, name string) *UpstreamServer {
	return &UpstreamServer{
		URL:            url,
		Proxy:          proxy,
		Name:           name,
		healthCacheTTL: max(0, *healthCheckInterval),
		maxRetries:     3,    // 默认最大重试次数
		isHealthy:      true, // 默认假设健康
	}
}

// SetHealthCacheTTL 设置健康检查缓存TTL
func (u *UpstreamServer) SetHealthCacheTTL(ttl time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.healthCacheTTL = ttl
}

// SetMaxRetries 设置最大重试次数
func (u *UpstreamServer) SetMaxRetries(maxRetries int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.maxRetries = maxRetries
}

// IsHealthy 检查上游服务器是否健康（使用缓存）
func (u *UpstreamServer) IsHealthy() bool {
	u.mu.RLock()

	// 如果健康检查被禁用（TTL <= 0），直接返回当前状态
	if u.healthCacheTTL <= 0 {
		healthy := u.isHealthy
		u.mu.RUnlock()
		return healthy
	}

	// 如果缓存未过期，直接返回缓存结果
	if time.Since(u.lastHealthCheck) < u.healthCacheTTL {
		healthy := u.isHealthy
		u.mu.RUnlock()
		return healthy
	}
	u.mu.RUnlock()

	// 缓存过期，执行健康检查
	return u.checkHealth()
}

// GetHealthStatus 获取详细的健康状态信息
func (u *UpstreamServer) GetHealthStatus() map[string]any {
	u.mu.RLock()
	defer u.mu.RUnlock()

	return map[string]any{
		"healthy":       u.isHealthy,
		"last_check":    u.lastHealthCheck,
		"last_error":    u.lastError,
		"retry_count":   u.retryCount,
		"url":           u.URL,
		"proxy":         u.Proxy,
		"name":          u.Name,
		"cache_ttl":     u.healthCacheTTL,
		"cache_expired": time.Since(u.lastHealthCheck) >= u.healthCacheTTL,
	}
}

// ForceHealthCheck 强制执行健康检查（忽略缓存）
func (u *UpstreamServer) ForceHealthCheck() bool {
	return u.checkHealth()
}

// checkHealth 执行实际的健康检查
func (u *UpstreamServer) checkHealth() bool {
	client := createHTTPClientForUpstream(u.Proxy)

	// 尝试多个可能的健康检查路径
	testPaths := []string{
		"/",                      // 根路径
		"/alpine/",               // Alpine 镜像根目录
		"/alpine/latest-stable/", // 最新稳定版目录
		"/alpine/latest-stable/main/x86_64/APKINDEX.tar.gz", // 具体的索引文件
	}

	var lastError error
	healthy := false

	for _, testPath := range testPaths {
		url := u.URL + testPath
		resp, err := client.Head(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			healthy = true
			resp.Body.Close()
			break
		}
		if err != nil {
			lastError = err
		} else {
			resp.Body.Close()
			lastError = fmt.Errorf("status code: %d", resp.StatusCode)
		}
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	u.lastHealthCheck = time.Now()

	if healthy {
		u.isHealthy = true
		u.lastError = ""
		u.retryCount = 0
		log.Println(i18n.T("HealthCheckUpstreamHealthy", map[string]any{
			"URL": u.URL,
		}))
	} else {
		u.retryCount++
		if u.retryCount >= u.maxRetries {
			u.isHealthy = false
		}
		u.lastError = lastError.Error()
		log.Println(i18n.T("HealthCheckUpstreamUnhealthy", map[string]any{
			"URL":   u.URL,
			"Error": lastError,
		}))
	}

	return u.isHealthy
}

// GetURL 获取服务器URL
func (u *UpstreamServer) GetURL() string {
	return u.URL
}

// GetProxy 获取代理配置
func (u *UpstreamServer) GetProxy() string {
	return u.Proxy
}

// GetName 获取服务器名称
func (u *UpstreamServer) GetName() string {
	return u.Name
}

// GetLastError 获取最后一次错误信息
func (u *UpstreamServer) GetLastError() string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.lastError
}

// GetRetryCount 获取重试次数
func (u *UpstreamServer) GetRetryCount() int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.retryCount
}

// ResetHealth 重置健康状态（用于自愈机制）
func (u *UpstreamServer) ResetHealth() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.retryCount = 0
	u.isHealthy = true
	u.lastError = ""
	u.lastHealthCheck = time.Time{} // 强制下次检查
}

// NewUpstreamManager 创建上游服务器管理器
func NewUpstreamManager() *UpstreamManager {
	return &UpstreamManager{
		servers: make([]*UpstreamServer, 0),
		current: 0,
	}
}

// AddServer 添加上游服务器
func (m *UpstreamManager) AddServer(server *UpstreamServer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = append(m.servers, server)
}

// GetHealthyServer 获取健康的上游服务器（轮询）
func (m *UpstreamManager) GetHealthyServer() *UpstreamServer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.servers) == 0 {
		return nil
	}

	// 尝试从当前位置开始查找健康服务器
	start := m.current
	for i := 0; i < len(m.servers); i++ {
		index := (start + i) % len(m.servers)
		server := m.servers[index]
		if server.IsHealthy() {
			m.current = (index + 1) % len(m.servers)
			return server
		}
	}

	// 如果没有健康服务器，返回第一个（降级使用）
	return m.servers[0]
}

// GetAllServers 获取所有服务器
func (m *UpstreamManager) GetAllServers() []*UpstreamServer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.servers
}

// GetServerCount 获取服务器数量
func (m *UpstreamManager) GetServerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.servers)
}

// GetHealthyCount 获取健康服务器数量
func (m *UpstreamManager) GetHealthyCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, server := range m.servers {
		if server.IsHealthy() {
			count++
		}
	}
	return count
}

// ForceHealthCheckAll 强制检查所有服务器的健康状态
func (m *UpstreamManager) ForceHealthCheckAll() {
	m.mu.RLock()
	servers := make([]*UpstreamServer, len(m.servers))
	copy(servers, m.servers)
	m.mu.RUnlock()

	for _, server := range servers {
		server.ForceHealthCheck()
	}
}

// FetchFromUpstream 从上游服务器获取数据，支持故障转移
func (m *UpstreamManager) FetchFromUpstream(urlPath string) (*http.Response, error) {
	m.mu.RLock()
	servers := make([]*UpstreamServer, len(m.servers))
	copy(servers, m.servers)
	m.mu.RUnlock()

	var lastErr error

	// 尝试所有上游服务器，直到成功或全部失败
	for i, server := range servers {
		client := createHTTPClientForUpstream(server.Proxy)
		url := server.URL + urlPath

		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			if i > 0 {
				serverName := server.Name
				if serverName == "" {
					serverName = server.URL
				}
				log.Println(i18n.T("FallbackUpstream", map[string]any{
					"Index": i + 1,
					"Name":  serverName,
				}))
			}
			return resp, nil
		}

		// 记录错误并尝试下一个服务器
		if err != nil {
			lastErr = err
			log.Println(i18n.T("UpstreamFailed", map[string]any{
				"URL":   server.URL,
				"Error": err,
			}))
		} else {
			lastErr = errors.New(i18n.T("UpstreamReturnedStatusCode", map[string]any{"Status": resp.StatusCode}))
			log.Println(i18n.T("UpstreamStatusError", map[string]any{
				"URL":    server.URL,
				"Status": resp.StatusCode,
			}))
			resp.Body.Close()
		}
	}

	if lastErr == nil {
		lastErr = errors.New(i18n.T("NoAvailableUpstream", nil))
	}
	return nil, lastErr
}
