package main

import (
	"errors"
	"fmt"
	"iter"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tursom/apk-cache/utils/i18n"
	"golang.org/x/sync/singleflight"
)

// UpstreamServer 上游服务器配置，集成健康检查功能
type UpstreamServer struct {
	URL   string
	Proxy string
	Name  string

	// 健康检查相关字段
	mu               sync.RWMutex
	lastHealthCheck  time.Time
	isHealthy        bool
	healthCacheTTL   time.Duration
	lastError        string
	retryCount       int
	maxRetries       int
	healthCheckGroup singleflight.Group // 用于合并并发健康检查
}

// UpstreamManager 上游服务器管理器
type UpstreamManager struct {
	servers atomic.Pointer[[]*UpstreamServer] // Copy On Write 切片
	current int32                             // 使用原子操作保证并发安全
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
	// 如果健康检查被禁用（TTL <= 0），直接返回当前状态
	if u.healthCacheTTL <= 0 {
		return u.getHealthyStatus()
	}

	// 如果缓存未过期，直接返回缓存结果
	if u.isCacheValid() {
		return u.getHealthyStatus()
	}

	// 缓存过期，执行健康检查
	return u.checkHealth()
}

// getHealthyStatus 获取健康状态（线程安全）
func (u *UpstreamServer) getHealthyStatus() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.isHealthy
}

// isCacheValid 检查缓存是否有效（线程安全）
func (u *UpstreamServer) isCacheValid() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return time.Since(u.lastHealthCheck) < u.healthCacheTTL
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

// checkHealth 执行实际的健康检查（使用 singleflight 合并并发请求）
func (u *UpstreamServer) checkHealth() bool {
	// 使用 singleflight 确保对同一服务器的并发健康检查只执行一次
	key := u.URL + "|" + u.Proxy // 唯一标识符
	result, _, _ := u.healthCheckGroup.Do(key, func() (interface{}, error) {
		return u.doHealthCheck(), nil
	})
	return result.(bool)
}

// doHealthCheck 执行实际的健康检查逻辑（无并发合并）
func (u *UpstreamServer) doHealthCheck() bool {
	// 打印上次更新时间
	log.Println(i18n.T("LastHealthCheckUpdateTime", map[string]any{"Time": u.lastHealthCheck}))

	// 使用健康检查专用超时
	timeout := *healthCheckTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second // 默认超时
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: createHTTPClientForUpstream(u.Proxy).Transport,
	}
	if client.Transport == nil {
		client.Transport = http.DefaultTransport
	}

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
	m := &UpstreamManager{
		current: 0,
	}
	// 初始化空切片指针
	empty := make([]*UpstreamServer, 0)
	m.servers.Store(&empty)
	return m
}

// AddServer 添加上游服务器
func (m *UpstreamManager) AddServer(server *UpstreamServer) {
	for {
		oldPtr := m.servers.Load()
		var newServers []*UpstreamServer
		if oldPtr == nil {
			newServers = []*UpstreamServer{server}
		} else {
			newServers = make([]*UpstreamServer, len(*oldPtr)+1)
			copy(newServers, *oldPtr)
			newServers[len(*oldPtr)] = server
		}
		if m.servers.CompareAndSwap(oldPtr, &newServers) {
			break
		}
		// CAS 失败，重试
	}
}

// GetHealthyServer 获取健康的上游服务器（轮询）
func (m *UpstreamManager) GetHealthyServer() *UpstreamServer {
	servers := m.getServers()
	if len(servers) == 0 {
		return nil
	}

	// 原子加载当前索引
	start := int(atomic.LoadInt32(&m.current))
	for i := range servers {
		index := (start + i) % len(servers)
		server := servers[index]
		if server.IsHealthy() {
			// 原子存储下一个索引
			next := (index + 1) % len(servers)
			atomic.StoreInt32(&m.current, int32(next))
			return server
		}
	}

	// 如果没有健康服务器，返回第一个（降级使用）
	return servers[0]
}

// GetAllServers 获取所有服务器
func (m *UpstreamManager) GetAllServers() iter.Seq[*UpstreamServer] {
	servers := m.getServers()
	if len(servers) == 0 {
		return func(yield func(*UpstreamServer) bool) {}
	}

	return func(yield func(*UpstreamServer) bool) {
		for _, s := range servers {
			if !yield(s) {
				return
			}
		}
	}
}

// GetServerCount 获取服务器数量
func (m *UpstreamManager) GetServerCount() int {
	ptr := m.servers.Load()
	if ptr == nil {
		return 0
	}
	return len(*ptr)
}

// GetHealthyCount 获取健康服务器数量
func (m *UpstreamManager) GetHealthyCount() int {
	ptr := m.servers.Load()
	if ptr == nil {
		return 0
	}
	servers := *ptr
	count := 0
	for _, server := range servers {
		if server.IsHealthy() {
			count++
		}
	}
	return count
}

// getServers 返回服务器切片的引用（线程安全，基于 COW 无需拷贝）
func (m *UpstreamManager) getServers() []*UpstreamServer {
	ptr := m.servers.Load()
	if ptr == nil {
		return nil
	}
	return *ptr
}

// FetchFromUpstream 从上游服务器获取数据，支持故障转移
func (m *UpstreamManager) FetchFromUpstream(urlPath string) (*http.Response, error) {
	var lastErr error

	// 尝试所有上游服务器，直到成功或全部失败
	for i, server := range m.getServers() {
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
