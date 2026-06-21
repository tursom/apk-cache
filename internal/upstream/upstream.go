package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

type Server struct {
	Name  string
	URL   string
	Proxy string

	healthy atomic.Bool
	lastErr atomic.Value
}

func NewServer(url, proxyAddr, name string) *Server {
	s := &Server{Name: name, URL: url, Proxy: proxyAddr}
	s.healthy.Store(true)
	return s
}

func (s *Server) Healthy() bool {
	return s.healthy.Load()
}

func (s *Server) LastError() string {
	value := s.lastErr.Load()
	if value == nil {
		return ""
	}
	return value.(string)
}

func (s *Server) mark(ok bool, err error) {
	s.healthy.Store(ok)
	if err == nil {
		s.lastErr.Store("")
		return
	}
	s.lastErr.Store(err.Error())
}

type ClientFactory interface {
	Client(proxyAddr string) *http.Client
}

type Manager struct {
	mu      sync.RWMutex
	servers []*Server
	next    uint64
	clients ClientFactory

	onRequest  func()
	onFailover func()
}

func NewManager(clients ClientFactory) *Manager {
	return &Manager{clients: clients}
}

func (m *Manager) SetMetricsHooks(onRequest, onFailover func()) {
	m.onRequest = onRequest
	m.onFailover = onFailover
}

func (m *Manager) Add(server *Server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = append(m.servers, server)
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.servers)
}

func (m *Manager) HealthyCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, server := range m.servers {
		if server.Healthy() {
			count++
		}
	}
	return count
}

func (m *Manager) Servers() []*Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Server, len(m.servers))
	copy(out, m.servers)
	return out
}

func (m *Manager) Fetch(ctx context.Context, path string, headers http.Header) (*http.Response, error) {
	servers := m.orderedServers()
	if len(servers) == 0 {
		return nil, errors.New("no configured upstream servers")
	}
	if m.onRequest != nil {
		m.onRequest()
	}

	var lastErr error
	var lastResp *http.Response
	for idx, server := range servers {
		target, err := BuildURL(server.URL, path)
		if err != nil {
			lastErr = err
			server.mark(false, err)
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			lastErr = err
			server.mark(false, err)
			continue
		}
		copyEndToEndHeaders(req.Header, headers)

		resp, err := m.clients.Client(server.Proxy).Do(req)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) {
			server.mark(true, nil)
			if idx > 0 && m.onFailover != nil {
				m.onFailover()
			}
			return resp, nil
		}
		if err != nil {
			lastErr = err
			server.mark(false, err)
			slog.Warn("apk upstream request failed", "url", server.URL, "err", err)
			continue
		}

		server.mark(false, fmt.Errorf("status %d", resp.StatusCode))
		if lastResp != nil {
			_, _ = io.Copy(io.Discard, lastResp.Body)
			_ = lastResp.Body.Close()
		}
		lastResp = resp
		lastErr = fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	if lastResp != nil {
		return lastResp, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no available upstream server")
	}
	return nil, lastErr
}

func (m *Manager) orderedServers() []*Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.servers) == 0 {
		return nil
	}
	start := int(atomic.AddUint64(&m.next, 1)-1) % len(m.servers)
	out := make([]*Server, 0, len(m.servers))
	for i := 0; i < len(m.servers); i++ {
		server := m.servers[(start+i)%len(m.servers)]
		if server.Healthy() {
			out = append(out, server)
		}
	}
	for i := 0; i < len(m.servers); i++ {
		server := m.servers[(start+i)%len(m.servers)]
		if !server.Healthy() {
			out = append(out, server)
		}
	}
	return out
}

func BuildURL(baseURL, requestPath string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = joinPath(parsed.Path, requestPath)
	parsed.RawPath = ""
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func joinPath(basePath, requestPath string) string {
	base := splitSegments(basePath)
	request := splitSegments(requestPath)
	overlap := 0
	limit := min(len(base), len(request))
	for size := limit; size > 0; size-- {
		matched := true
		for i := 0; i < size; i++ {
			if base[len(base)-size+i] != request[i] {
				matched = false
				break
			}
		}
		if matched {
			overlap = size
			break
		}
	}
	combined := append(append([]string{}, base...), request[overlap:]...)
	if len(combined) == 0 {
		return "/"
	}
	joined := "/" + strings.Join(combined, "/")
	if strings.HasSuffix(requestPath, "/") && joined != "/" {
		return joined + "/"
	}
	return joined
}

func splitSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func copyEndToEndHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate",
		"proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
