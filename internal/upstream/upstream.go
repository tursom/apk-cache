package upstream

import (
	"errors"
	"fmt"
	"iter"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// Server 上游服务器配置，集成健康检查功能
type Server struct {
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

// Manager 上游服务器管理器
type Manager struct {
	servers atomic.Pointer[[]*Server] // Copy On Write 切片
	current int32                     // 使用原子操作保证并发安全
}

// NewServer 创建新的上游服务器实例
func NewServer(url, proxy, name string, healthCheckTTL time.Duration) *Server {
	return &Server{
		URL:            url,
		Proxy:          proxy,
		Name:           name,
		healthCacheTTL: healthCheckTTL,
		maxRetries:     3,
		isHealthy:      true,
	}
}

// SetHealthCacheTTL 设置健康检查缓存TTL
func (u *Server) SetHealthCacheTTL(ttl time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.healthCacheTTL = ttl
}

// SetMaxRetries 设置最大重试次数
func (u *Server) SetMaxRetries(maxRetries int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.maxRetries = maxRetries
}

// IsHealthy 检查上游服务器是否健康（使用缓存）
func (u *Server) IsHealthy() bool {
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
func (u *Server) getHealthyStatus() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.isHealthy
}

// isCacheValid 检查缓存是否有效（线程安全）
func (u *Server) isCacheValid() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return time.Since(u.lastHealthCheck) < u.healthCacheTTL
}

// GetHealthStatus 获取详细的健康状态信息
func (u *Server) GetHealthStatus() map[string]any {
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
func (u *Server) checkHealth() bool {
	key := u.URL + "|" + u.Proxy // 唯一标识符
	result, _, _ := u.healthCheckGroup.Do(key, func() (interface{}, error) {
		return u.doHealthCheck(), nil
	})
	return result.(bool)
}

// doHealthCheck 执行实际的健康检查逻辑（无并发合并）
func (u *Server) doHealthCheck() bool {
	timeout := u.healthCacheTTL
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: CreateTransport(u.Proxy),
	}
	if client.Transport == nil {
		client.Transport = http.DefaultTransport
	}

	testPaths := []string{
		"/",
		"/alpine/",
		"/alpine/latest-stable/",
		"/alpine/latest-stable/main/x86_64/APKINDEX.tar.gz",
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
		log.Printf("[upstream] Server %s is healthy", u.URL)
	} else {
		u.retryCount++
		if u.retryCount >= u.maxRetries {
			u.isHealthy = false
		}
		u.lastError = lastError.Error()
		log.Printf("[upstream] Server %s is unhealthy: %v", u.URL, lastError)
	}

	return u.isHealthy
}

// GetURL 获取服务器URL
func (u *Server) GetURL() string {
	return u.URL
}

// GetProxy 获取代理配置
func (u *Server) GetProxy() string {
	return u.Proxy
}

// GetName 获取服务器名称
func (u *Server) GetName() string {
	return u.Name
}

// GetLastError 获取最后一次错误信息
func (u *Server) GetLastError() string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.lastError
}

// GetRetryCount 获取重试次数
func (u *Server) GetRetryCount() int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.retryCount
}

// ResetHealth 重置健康状态
func (u *Server) ResetHealth() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.retryCount = 0
	u.isHealthy = true
	u.lastError = ""
	u.lastHealthCheck = time.Time{}
}

// NewManager 创建上游服务器管理器
func NewManager() *Manager {
	m := &Manager{
		current: 0,
	}
	empty := make([]*Server, 0)
	m.servers.Store(&empty)
	return m
}

// AddServer 添加上游服务器
func (m *Manager) AddServer(server *Server) {
	for {
		oldPtr := m.servers.Load()
		var newServers []*Server
		if oldPtr == nil {
			newServers = []*Server{server}
		} else {
			newServers = make([]*Server, len(*oldPtr)+1)
			copy(newServers, *oldPtr)
			newServers[len(*oldPtr)] = server
		}
		if m.servers.CompareAndSwap(oldPtr, &newServers) {
			break
		}
	}
}

// GetHealthyServer 获取健康的上游服务器（轮询）
func (m *Manager) GetHealthyServer() *Server {
	servers := m.getServers()
	if len(servers) == 0 {
		return nil
	}

	start := int(atomic.LoadInt32(&m.current))
	for i := range servers {
		index := (start + i) % len(servers)
		server := servers[index]
		if server.IsHealthy() {
			next := (index + 1) % len(servers)
			atomic.StoreInt32(&m.current, int32(next))
			return server
		}
	}

	if len(servers) > 0 {
		log.Printf("[upstream] No healthy server available, using fallback: %s", servers[0].URL)
		return servers[0]
	}
	return nil
}

// GetAllServers 获取所有服务器
func (m *Manager) GetAllServers() iter.Seq[*Server] {
	servers := m.getServers()
	if len(servers) == 0 {
		return func(yield func(*Server) bool) {}
	}

	return func(yield func(*Server) bool) {
		for _, s := range servers {
			if !yield(s) {
				return
			}
		}
	}
}

// GetServerCount 获取服务器数量
func (m *Manager) GetServerCount() int {
	ptr := m.servers.Load()
	if ptr == nil {
		return 0
	}
	return len(*ptr)
}

// GetHealthyCount 获取健康服务器数量
func (m *Manager) GetHealthyCount() int {
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

// getServers 返回服务器切片的引用
func (m *Manager) getServers() []*Server {
	ptr := m.servers.Load()
	if ptr == nil {
		return nil
	}
	return *ptr
}

// Fetcher 上游数据获取器接口
type Fetcher interface {
	Fetch(urlPath string, requestModifier func(*http.Request)) (*http.Response, error)
}

// DefaultFetcher 默认的上游数据获取实现
type DefaultFetcher struct {
	manager *Manager
	client  func(proxy string) *http.Client
}

// NewFetcher 创建新的上游数据获取器
func NewFetcher(manager *Manager, clientFn func(proxy string) *http.Client) *DefaultFetcher {
	return &DefaultFetcher{
		manager: manager,
		client:  clientFn,
	}
}

// Fetch 从上游服务器获取数据，支持故障转移
func (f *DefaultFetcher) Fetch(urlPath string, requestModifier func(*http.Request)) (*http.Response, error) {
	var lastErr error

	servers := f.manager.getServers()
	log.Printf("[upstream] Fetching from upstream, path: %s, server count: %d", urlPath, len(servers))

	for i, server := range servers {
		client := f.client(server.Proxy)
		url := server.URL + urlPath

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			lastErr = err
			continue
		}

		if requestModifier != nil {
			requestModifier(req)
		}

		resp, err := client.Do(req)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) {
			if i > 0 {
				serverName := server.Name
				if serverName == "" {
					serverName = server.URL
				}
				log.Printf("[upstream] Fallback to server %d: %s", i+1, serverName)
			}
			return resp, nil
		}

		if err != nil {
			lastErr = err
			log.Printf("[upstream] Server %s failed: %v", server.URL, err)
		} else {
			lastErr = errors.New(fmt.Sprintf("upstream returned status: %d", resp.StatusCode))
			log.Printf("[upstream] Server %s returned error status: %d", server.URL, resp.StatusCode)
			resp.Body.Close()
		}
	}

	if lastErr == nil {
		lastErr = errors.New("no available upstream server")
	}
	return nil, lastErr
}
