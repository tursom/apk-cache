package main

import (
	"encoding/json"
	"net/http"
	"time"
)

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