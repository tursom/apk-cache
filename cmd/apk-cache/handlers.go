package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tursom/apk-cache/utils/i18n"
)

type webHandler struct {
	metricsHandler http.HandlerFunc
	adminHandler   http.HandlerFunc
	healthHandler  http.HandlerFunc
	rootHandler    http.HandlerFunc
}

func newWebHandler() *webHandler {
	return &webHandler{
		metricsHandler: rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
			promhttp.HandlerFor(monitoring.GetRegistry(), promhttp.HandlerOpts{}).ServeHTTP(w, r)
		}),
		adminHandler:  authMiddleware(rateLimitAdminMiddleware(adminDashboardHandler)),
		healthHandler: authMiddleware(healthCheckHandler),
		rootHandler: rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
			// 检查是否是CONNECT方法（HTTPS代理）或代理请求
			if proxyIsProxyRequest(r) {
				// 代理请求需要身份验证
				proxyAuth(handleProxyRequest, w, r)
			} else {
				// 非代理请求使用原有的APK缓存逻辑，不需要代理身份验证
				proxyHandler(w, r)
			}
		}),
	}
}

// ServeHTTP implements http.Handler.
func (h *webHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/metrics" {
		h.metricsHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/_admin") {
		h.adminHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/_health") {
		h.healthHandler.ServeHTTP(w, r)
		return
	}

	h.rootHandler.ServeHTTP(w, r)
}

// healthCheckHandler 简单的健康检查处理器
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 检查上游服务器健康状态
	healthyCount := upstreamManager.GetHealthyCount()
	totalCount := upstreamManager.GetServerCount()

	status := "healthy"
	if healthyCount == 0 {
		status = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else if healthyCount < totalCount {
		status = "degraded"
	} else {
		w.WriteHeader(http.StatusOK)
	}

	response := map[string]any{
		"status":    status,
		"timestamp": time.Now(),
		"upstream": map[string]any{
			"healthy_servers": healthyCount,
			"total_servers":   totalCount,
		},
	}

	json.NewEncoder(w).Encode(response)
}

// authMiddleware 认证中间件
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 如果没有设置密码，跳过认证
		if *adminPassword == "" {
			next(w, r)
			return
		}

		// Basic Auth
		username, password, ok := r.BasicAuth()
		if !ok || username != *adminUser || password != *adminPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// proxyAuth 代理身份验证
func proxyAuth(next http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	// 如果没有启用代理身份验证，直接返回next
	if !*proxyAuthEnabled {
		next(w, r)
		return
	}

	// 如果IP匹配器初始化失败，使用简单的认证逻辑
	if proxyIPMatcher == nil {
		// Basic Auth for proxy
		username, password, ok := r.BasicAuth()
		if !ok || username != *proxyUser || password != *proxyPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
			http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
			return
		}
		next(w, r)
		return
	}

	// 获取真实的客户端 IP
	clientIP := proxyIPMatcher.GetRealClientIP(r)

	// 检查 IP 是否在不需要验证的网段中
	if proxyIPMatcher.IsExemptIP(clientIP) {
		next(w, r)
		return
	}

	// Basic Auth for proxy
	username, password, ok := r.BasicAuth()
	if !ok || username != *proxyUser || password != *proxyPassword {
		w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		return
	}
	next(w, r)
}

// rateLimitMiddleware 限流中间件
func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	// 如果限流器未启用，直接调用下一个处理器
	if rateLimiter == nil || !*rateLimitEnabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// 检查路径是否在豁免列表中
		if isExemptPath(r.URL.Path) {
			next(w, r)
			return
		}

		// 检查是否允许请求
		if !rateLimiter.Allow() {
			monitoring.RecordRateLimitRejected()
			w.Header().Set("Retry-After", "1")
			http.Error(w, i18n.T("RateLimitExceeded", nil), http.StatusTooManyRequests)
			return
		}

		// 请求被允许，继续处理
		monitoring.RecordRateLimitAllowed()
		next(w, r)
	}
}

// isExemptPath 检查路径是否在豁免列表中
func isExemptPath(path string) bool {
	for _, exemptPath := range rateLimitExemptPathsList {
		if strings.HasPrefix(path, exemptPath) {
			return true
		}
	}

	return false
}

// rateLimitAdminMiddleware 管理员接口限流中间件（更宽松的限制）
func rateLimitAdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	// 如果限流器未启用，直接调用下一个处理器
	if rateLimiter == nil || !*rateLimitEnabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// 管理员接口使用更宽松的限制
		if !rateLimiter.Allow() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, i18n.T("RateLimitExceeded", nil), http.StatusTooManyRequests)
			return
		}

		// 请求被允许，继续处理
		next(w, r)
	}
}

// updateRateLimitMetrics 定期更新限流器指标
func updateRateLimitMetrics() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if rateLimiter != nil {
			stats := rateLimiter.GetStats()
			if currentTokens, ok := stats["current_tokens"].(float64); ok {
				monitoring.UpdateRateLimitMetrics(currentTokens)
			}
		}
	}
}
