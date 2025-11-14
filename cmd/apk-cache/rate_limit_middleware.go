package main

import (
	"net/http"
	"strings"
)

// rateLimitMiddleware 限流中间件
func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 如果限流器未启用，直接调用下一个处理器
		if rateLimiter == nil || !*rateLimitEnabled {
			next(w, r)
			return
		}

		// 检查路径是否在豁免列表中
		if isExemptPath(r.URL.Path) {
			next(w, r)
			return
		}

		// 检查是否允许请求
		if !rateLimiter.Allow() {
			rateLimitRejected.Add(1)
			w.Header().Set("Retry-After", "1")
			http.Error(w, t("RateLimitExceeded", nil), http.StatusTooManyRequests)
			return
		}

		// 请求被允许，继续处理
		rateLimitAllowed.Add(1)
		next(w, r)
	}
}

// isExemptPath 检查路径是否在豁免列表中
func isExemptPath(path string) bool {
	if *rateLimitExemptPaths == "" {
		return false
	}

	exemptPaths := strings.Split(*rateLimitExemptPaths, ",")
	for _, exemptPath := range exemptPaths {
		exemptPath = strings.TrimSpace(exemptPath)
		if exemptPath != "" && strings.HasPrefix(path, exemptPath) {
			return true
		}
	}

	return false
}

// rateLimitAdminMiddleware 管理员接口限流中间件（更宽松的限制）
func rateLimitAdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 如果限流器未启用，直接调用下一个处理器
		if rateLimiter == nil || !*rateLimitEnabled {
			next(w, r)
			return
		}

		// 管理员接口使用更宽松的限制
		if !rateLimiter.Allow() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, t("RateLimitExceeded", nil), http.StatusTooManyRequests)
			return
		}

		// 请求被允许，继续处理
		next(w, r)
	}
}
