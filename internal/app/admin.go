package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	adminui "github.com/tursom/apk-cache/internal/admin"
	apkpkg "github.com/tursom/apk-cache/internal/apk"
	aptpkg "github.com/tursom/apk-cache/internal/apt"
	cachepkg "github.com/tursom/apk-cache/internal/cache"
	"github.com/tursom/apk-cache/internal/config"
	"github.com/tursom/apk-cache/internal/hashstore"
	"github.com/tursom/apk-cache/internal/store"
	"github.com/tursom/apk-cache/internal/upstream"
	"golang.org/x/crypto/argon2"
)

const (
	adminSessionCookie = "apk_cache_admin_session"
	adminCSRFCookie    = "apk_cache_admin_csrf"
	adminSessionTTL    = 24 * time.Hour
	defaultAdminUser   = "admin"
	defaultAdminPass   = "admin123456"
)

type loginFailure struct {
	count     int
	blockedTo time.Time
}

type adminResponse struct {
	OK    bool        `json:"ok"`
	Data  any         `json:"data"`
	Error *adminError `json:"error"`
}

type adminError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (a *App) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	adminui.ServePage(w, r)
}

func (a *App) handleAdminAsset(w http.ResponseWriter, r *http.Request) {
	adminui.ServeAsset(w, r)
}

func (a *App) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/v1")
	if path == "" {
		path = "/"
	}

	if path == "/auth/login" && r.Method == http.MethodPost {
		a.adminLogin(w, r)
		return
	}

	user, session, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	switch {
	case path == "/auth/me" && r.Method == http.MethodGet:
		a.writeAdminData(w, adminUserPayload(user, csrfFromRequest(r)))
	case path == "/auth/logout" && r.Method == http.MethodPost:
		a.adminLogout(w, r, session)
	case path == "/auth/change-password" && r.Method == http.MethodPost:
		a.adminChangePassword(w, r, user)
	case path == "/account" && r.Method == http.MethodGet:
		a.adminAccount(w, r, user)
	case path == "/account/username" && r.Method == http.MethodPut:
		a.adminUpdateUsername(w, r, user)
	case path == "/account/password" && r.Method == http.MethodPut:
		a.adminChangePassword(w, r, user)
	case path == "/dashboard/summary" && r.Method == http.MethodGet:
		a.adminDashboard(w, r)
	case path == "/dashboard/series" && r.Method == http.MethodGet:
		a.adminDashboardSeries(w, r)
	case path == "/config" && r.Method == http.MethodGet:
		a.adminConfig(w, r)
	case path == "/config" && r.Method == http.MethodPut:
		a.adminUpdateConfig(w, r)
	case path == "/config/schema" && r.Method == http.MethodGet:
		a.writeAdminData(w, map[string]any{"items": store.ListSettingSchema()})
	case path == "/config/validate" && r.Method == http.MethodPost:
		a.adminValidateConfig(w, r)
	case path == "/config/reload" && r.Method == http.MethodPost:
		a.adminReloadConfig(w, r)
	case path == "/upstreams" && r.Method == http.MethodGet:
		a.adminListUpstreams(w, r)
	case path == "/upstreams" && r.Method == http.MethodPost:
		a.adminCreateUpstream(w, r)
	case strings.HasPrefix(path, "/upstreams/"):
		a.adminUpstreamAction(w, r, path)
	case path == "/proxy/status" && r.Method == http.MethodGet:
		a.adminProxyStatus(w, r)
	case path == "/proxy/config" && r.Method == http.MethodPut:
		a.adminProxyConfig(w, r)
	case path == "/proxy/host-rules" && r.Method == http.MethodGet:
		a.adminListProxyHostRules(w, r)
	case path == "/proxy/host-rules" && r.Method == http.MethodPost:
		a.adminCreateProxyHostRule(w, r)
	case strings.HasPrefix(path, "/proxy/host-rules/"):
		a.adminProxyHostRuleAction(w, r, path)
	case path == "/cache/objects" && r.Method == http.MethodGet:
		a.adminListCacheObjects(w, r)
	case strings.HasPrefix(path, "/cache/objects/"):
		a.adminCacheObjectAction(w, r, path)
	case path == "/cache/delete" && r.Method == http.MethodPost:
		a.adminBatchDeleteCache(w, r)
	case path == "/cache/prewarm" && r.Method == http.MethodPost:
		a.adminPrewarm(w, r)
	case path == "/cache/reconcile" && r.Method == http.MethodPost:
		a.adminReconcileCache(w, r)
	case path == "/cache/memory/clear" && r.Method == http.MethodPost:
		a.adminClearMemory(w, r)
	case path == "/apk/indexes" && r.Method == http.MethodGet:
		a.adminAPKIndexes(w, r)
	case path == "/apk/packages" && r.Method == http.MethodGet:
		a.adminAPKPackages(w, r)
	case path == "/apk/indexes/reload" && r.Method == http.MethodPost:
		a.adminReloadAPKIndexes(w, r)
	case path == "/apk/keys" && r.Method == http.MethodGet:
		a.adminAPKKeys(w, r)
	case path == "/apk/keys/reload" && r.Method == http.MethodPost:
		a.adminReloadAPKKeys(w, r)
	case path == "/apt/indexes" && r.Method == http.MethodGet:
		a.adminAPTIndexes(w, r)
	case path == "/apt/records" && r.Method == http.MethodGet:
		a.adminAPTRecords(w, r)
	case path == "/apt/mirrors" && r.Method == http.MethodGet:
		a.adminListAPTMirrors(w, r)
	case path == "/apt/mirrors" && r.Method == http.MethodPost:
		a.adminCreateAPTMirror(w, r)
	case strings.HasPrefix(path, "/apt/mirrors/"):
		a.adminAPTMirrorAction(w, r, path)
	case path == "/apt/indexes/reload" && r.Method == http.MethodPost:
		a.adminReloadAPTIndexes(w, r)
	case path == "/apt/validate" && r.Method == http.MethodPost:
		a.adminValidateAPT(w, r)
	case (path == "/logs" || path == "/logs/requests") && r.Method == http.MethodGet:
		a.adminRequestLogs(w, r)
	case path == "/logs/errors" && r.Method == http.MethodGet:
		a.adminErrorLogs(w, r)
	case path == "/diagnostics/package" && r.Method == http.MethodPost:
		a.adminDiagnosticsPackage(w, r, user)
	case path == "/system/info" && r.Method == http.MethodGet:
		a.adminSystemInfo(w, r, user)
	case path == "/hash/status" && r.Method == http.MethodGet:
		a.adminHashStatus(w, r)
	default:
		a.writeAdminError(w, http.StatusNotFound, "not_found", "admin endpoint not found")
	}
}

func (a *App) adminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	if blocked := a.loginBlocked(r.RemoteAddr); blocked {
		a.writeAdminError(w, http.StatusTooManyRequests, "too_many_attempts", "too many login attempts")
		return
	}
	user, err := a.store.GetAdminByUsername(r.Context(), strings.TrimSpace(req.Username))
	if err != nil {
		a.recordLoginFailure(r.RemoteAddr)
		a.writeAdminError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	if !verifyPassword(req.Password, user.PasswordHash) {
		a.recordLoginFailure(r.RemoteAddr)
		a.writeAdminError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	token, tokenHash, err := a.newToken()
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "token_failed", err.Error())
		return
	}
	csrf, csrfHash, err := a.newToken()
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "token_failed", err.Error())
		return
	}
	now := time.Now().UTC()
	session := store.AdminSession{
		ID:            randomID(),
		UserID:        user.ID,
		TokenHash:     tokenHash,
		CSRFTokenHash: csrfHash,
		UserAgent:     r.UserAgent(),
		RemoteAddr:    r.RemoteAddr,
		ExpiresAt:     now.Add(adminSessionTTL).Format(time.RFC3339Nano),
		CreatedAt:     now.Format(time.RFC3339Nano),
		LastSeenAt:    now.Format(time.RFC3339Nano),
	}
	if err := a.store.CreateSession(r.Context(), session); err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	_ = a.store.MarkAdminLogin(r.Context(), user.ID)
	a.clearLoginFailures(r.RemoteAddr)
	a.setSessionCookies(w, r, token, csrf, now.Add(adminSessionTTL))
	user.LastLoginAt.String = now.Format(time.RFC3339Nano)
	user.LastLoginAt.Valid = true
	a.writeAdminData(w, adminUserPayload(user, csrf))
}

func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) (store.AdminUser, store.AdminSession, bool) {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil || cookie.Value == "" {
		a.writeAdminError(w, http.StatusUnauthorized, "unauthorized", "login required")
		return store.AdminUser{}, store.AdminSession{}, false
	}
	session, err := a.store.GetSessionByTokenHash(r.Context(), a.hashToken(cookie.Value))
	if err != nil {
		a.writeAdminError(w, http.StatusUnauthorized, "unauthorized", "login required")
		return store.AdminUser{}, store.AdminSession{}, false
	}
	expires, err := time.Parse(time.RFC3339Nano, session.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		_ = a.store.DeleteSession(r.Context(), session.ID)
		a.writeAdminError(w, http.StatusUnauthorized, "session_expired", "session expired")
		return store.AdminUser{}, store.AdminSession{}, false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		csrf := r.Header.Get("X-CSRF-Token")
		if csrf == "" || subtle.ConstantTimeCompare([]byte(a.hashToken(csrf)), []byte(session.CSRFTokenHash)) != 1 {
			a.writeAdminError(w, http.StatusForbidden, "csrf_failed", "invalid csrf token")
			return store.AdminUser{}, store.AdminSession{}, false
		}
	}
	user, err := a.store.GetAdminByID(r.Context(), session.UserID)
	if err != nil {
		a.writeAdminError(w, http.StatusUnauthorized, "unauthorized", "login required")
		return store.AdminUser{}, store.AdminSession{}, false
	}
	_ = a.store.TouchSession(r.Context(), session.ID)
	return user, session, true
}

func (a *App) adminLogout(w http.ResponseWriter, r *http.Request, session store.AdminSession) {
	_ = a.store.DeleteSession(r.Context(), session.ID)
	a.clearSessionCookies(w, r)
	a.writeAdminData(w, map[string]any{"logged_out": true})
}

func (a *App) adminChangePassword(w http.ResponseWriter, r *http.Request, user store.AdminUser) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	if !verifyPassword(req.OldPassword, user.PasswordHash) {
		a.writeAdminError(w, http.StatusForbidden, "invalid_password", "old password is invalid")
		return
	}
	if len(req.NewPassword) < 8 {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", "new password must be at least 8 characters")
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "hash_failed", err.Error())
		return
	}
	if err := a.store.UpdateAdminPassword(r.Context(), user.ID, hash, "argon2id"); err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"changed": true})
}

func (a *App) adminAccount(w http.ResponseWriter, r *http.Request, user store.AdminUser) {
	a.writeAdminData(w, map[string]any{
		"username":              user.Username,
		"is_default_credential": user.IsDefaultCredential,
		"last_login_at":         nullableString(user.LastLoginAt),
	})
}

func (a *App) adminUpdateUsername(w http.ResponseWriter, r *http.Request, user store.AdminUser) {
	var req struct {
		Username string `json:"username"`
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if len(username) < 3 || len(username) > 64 {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", "username must be 3-64 characters")
		return
	}
	if strings.ContainsAny(username, " \t\r\n") {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", "username must not contain whitespace")
		return
	}
	if err := a.store.UpdateAdminUsername(r.Context(), user.ID, username); err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"username": username, "is_default_credential": false})
}

func (a *App) adminDashboard(w http.ResponseWriter, r *http.Request) {
	cacheObjects, total, _ := a.store.ListCacheObjects(r.Context(), store.CacheObjectFilter{Page: 1, PageSize: 1})
	_ = cacheObjects
	hashStats, _ := a.hashStore.Stats()
	diskSummary, _ := a.cacheDiskSummary()
	logs, _ := a.store.ListRequestLogs(r.Context(), 500)
	requestStats, recentRequests, recentErrors := summarizeRequestLogs(logs, 10)
	var mem any
	if a.mem != nil {
		current, max, items := a.mem.Stats()
		mem = map[string]any{"size": current, "max": max, "items": items}
	}
	a.writeAdminData(w, map[string]any{
		"status":          "healthy",
		"cache_objects":   total,
		"apk_upstreams":   map[string]any{"healthy": a.apkUpstreams.HealthyCount(), "total": a.apkUpstreams.Count()},
		"memory_cache":    mem,
		"disk_cache":      diskSummary,
		"connect":         map[string]any{"active": len(a.connectCh), "limit": cap(a.connectCh), "rejected": 0},
		"hash_store":      hashStats,
		"database":        map[string]any{"path": a.store.Path()},
		"requests":        requestStats,
		"recent_requests": recentRequests,
		"recent_errors":   recentErrors,
	})
}

func (a *App) adminDashboardSeries(w http.ResponseWriter, r *http.Request) {
	logs, err := a.store.ListRequestLogs(r.Context(), 1000)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	buckets := map[string]map[string]int{}
	for _, item := range logs {
		ts, err := time.Parse(time.RFC3339Nano, item.TS)
		if err != nil {
			continue
		}
		key := ts.Truncate(time.Minute).Format(time.RFC3339)
		if buckets[key] == nil {
			buckets[key] = map[string]int{"requests": 0, "errors": 0}
		}
		buckets[key]["requests"]++
		if item.StatusCode >= 500 {
			buckets[key]["errors"]++
		}
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	points := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		points = append(points, map[string]any{
			"ts":       key,
			"requests": buckets[key]["requests"],
			"errors":   buckets[key]["errors"],
		})
	}
	a.writeAdminData(w, map[string]any{"points": points})
}

func (a *App) adminConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := a.store.ListSettings(r.Context())
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	upstreams, _ := a.store.ListUpstreams(r.Context(), false)
	a.writeAdminData(w, map[string]any{"settings": settings, "upstreams": upstreams})
}

func (a *App) adminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	next, restartKeys, err := a.store.UpdateSettings(r.Context(), a.cfg, req.Settings)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if err := a.applyRuntimeConfig(next); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "apply_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"saved": true, "restart_required": restartKeys})
}

func (a *App) adminValidateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	_, restartKeys, err := a.store.ValidateSettings(r.Context(), a.cfg, req.Settings)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"valid": true, "restart_required": restartKeys})
}

func (a *App) adminReloadConfig(w http.ResponseWriter, r *http.Request) {
	if err := a.reloadRuntimeFromStore(r.Context()); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "reload_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"reloaded": true})
}

func (a *App) adminListUpstreams(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListUpstreams(r.Context(), false)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"items": items})
}

func (a *App) adminCreateUpstream(w http.ResponseWriter, r *http.Request) {
	var req store.Upstream
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	if _, err := url.ParseRequestURI(req.URL); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	created, err := a.store.CreateUpstream(r.Context(), req)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := a.reloadRuntimeFromStore(r.Context()); err != nil {
		_ = a.store.DeleteUpstream(r.Context(), created.ID)
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	a.writeAdminData(w, created)
}

func (a *App) adminUpstreamAction(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		a.writeAdminError(w, http.StatusNotFound, "not_found", "upstream not found")
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", "invalid upstream id")
		return
	}
	switch {
	case len(parts) == 2 && r.Method == http.MethodPut:
		var req store.Upstream
		if !a.decodeAdminJSON(w, r, &req) {
			return
		}
		req.ID = id
		if req.Kind == "" {
			req.Kind = "apk"
		}
		if req.Priority == 0 {
			req.Priority = 100
		}
		if _, parseErr := url.ParseRequestURI(req.URL); parseErr != nil {
			a.writeAdminError(w, http.StatusBadRequest, "validation_failed", parseErr.Error())
			return
		}
		err = a.store.UpdateUpstream(r.Context(), req)
	case len(parts) == 2 && r.Method == http.MethodDelete:
		err = a.store.DeleteUpstream(r.Context(), id)
	case len(parts) == 3 && parts[2] == "enable" && r.Method == http.MethodPost:
		err = a.store.SetUpstreamEnabled(r.Context(), id, true)
	case len(parts) == 3 && parts[2] == "disable" && r.Method == http.MethodPost:
		err = a.store.SetUpstreamEnabled(r.Context(), id, false)
	case len(parts) == 3 && parts[2] == "test" && r.Method == http.MethodPost:
		a.adminTestUpstream(w, r, id)
		return
	default:
		a.writeAdminError(w, http.StatusNotFound, "not_found", "upstream action not found")
		return
	}
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := a.reloadRuntimeFromStore(r.Context()); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "reload_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"updated": true})
}

func (a *App) adminTestUpstream(w http.ResponseWriter, r *http.Request, id int64) {
	items, err := a.store.ListUpstreams(r.Context(), false)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	var target *store.Upstream
	for idx := range items {
		if items[idx].ID == id {
			target = &items[idx]
			break
		}
	}
	if target == nil {
		a.writeAdminError(w, http.StatusNotFound, "not_found", "upstream not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target.URL, nil)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	start := time.Now()
	resp, err := a.clients.Client(target.Proxy).Do(req)
	if err != nil {
		a.writeAdminData(w, map[string]any{"ok": false, "error": err.Error(), "duration_ms": time.Since(start).Milliseconds()})
		return
	}
	defer resp.Body.Close()
	a.writeAdminData(w, map[string]any{"ok": resp.StatusCode < 500, "status_code": resp.StatusCode, "duration_ms": time.Since(start).Milliseconds()})
}

func (a *App) adminProxyStatus(w http.ResponseWriter, r *http.Request) {
	hostRules, _ := a.store.ListProxyHostRules(r.Context(), false)
	a.writeAdminData(w, map[string]any{
		"enabled":                    a.cfg.Proxy.Enabled,
		"allow_connect":              a.cfg.Proxy.AllowConnect,
		"cache_non_package_requests": a.cfg.Proxy.CacheNonPackage,
		"upstream_proxy":             a.cfg.Proxy.UpstreamProxy,
		"allowed_hosts":              a.cfg.Proxy.AllowedHosts,
		"host_rules":                 hostRules,
		"host_rules_configured":      a.proxyHostRulesConfigured,
		"connect":                    map[string]any{"active": len(a.connectCh), "limit": cap(a.connectCh)},
	})
}

func (a *App) adminProxyConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled                 *bool    `json:"enabled"`
		AllowConnect            *bool    `json:"allow_connect"`
		CacheNonPackageRequests *bool    `json:"cache_non_package_requests"`
		UpstreamProxy           *string  `json:"upstream_proxy"`
		AllowedHosts            []string `json:"allowed_hosts"`
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	settings := map[string]json.RawMessage{}
	put := func(key string, value any) {
		raw, _ := json.Marshal(value)
		settings[key] = raw
	}
	if req.Enabled != nil {
		put("proxy.enabled", *req.Enabled)
	}
	if req.AllowConnect != nil {
		put("proxy.allow_connect", *req.AllowConnect)
	}
	if req.CacheNonPackageRequests != nil {
		put("proxy.cache_non_package_requests", *req.CacheNonPackageRequests)
	}
	if req.UpstreamProxy != nil {
		put("proxy.upstream_proxy", *req.UpstreamProxy)
	}
	next, restartKeys, err := a.store.UpdateSettings(r.Context(), a.cfg, settings)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if req.AllowedHosts != nil {
		if err := a.store.ReplaceProxyHostRules(r.Context(), req.AllowedHosts); err != nil {
			a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		if err := a.reloadRuntimeFromStore(r.Context()); err != nil {
			a.writeAdminError(w, http.StatusBadRequest, "reload_failed", err.Error())
			return
		}
	} else {
		if err := a.applyRuntimeConfig(next); err != nil {
			a.writeAdminError(w, http.StatusBadRequest, "apply_failed", err.Error())
			return
		}
	}
	a.writeAdminData(w, map[string]any{"saved": true, "restart_required": restartKeys})
}

func (a *App) adminListProxyHostRules(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListProxyHostRules(r.Context(), false)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"items": items})
}

func (a *App) adminCreateProxyHostRule(w http.ResponseWriter, r *http.Request) {
	var req store.ProxyHostRule
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	if err := validateProxyHostRule(&req); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	created, err := a.store.CreateProxyHostRule(r.Context(), req)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := a.store.SyncProxyAllowedHostsSetting(r.Context()); err != nil {
		_ = a.store.DeleteProxyHostRule(r.Context(), created.ID)
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := a.reloadRuntimeFromStore(r.Context()); err != nil {
		_ = a.store.DeleteProxyHostRule(r.Context(), created.ID)
		a.writeAdminError(w, http.StatusBadRequest, "reload_failed", err.Error())
		return
	}
	a.writeAdminData(w, created)
}

func (a *App) adminProxyHostRuleAction(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		a.writeAdminError(w, http.StatusNotFound, "not_found", "host rule not found")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", "invalid host rule id")
		return
	}
	switch {
	case len(parts) == 3 && r.Method == http.MethodPut:
		var req store.ProxyHostRule
		if !a.decodeAdminJSON(w, r, &req) {
			return
		}
		req.ID = id
		if err := validateProxyHostRule(&req); err != nil {
			a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		err = a.store.UpdateProxyHostRule(r.Context(), req)
	case len(parts) == 3 && r.Method == http.MethodDelete:
		err = a.store.DeleteProxyHostRule(r.Context(), id)
	case len(parts) == 4 && parts[3] == "enable" && r.Method == http.MethodPost:
		err = a.store.SetProxyHostRuleEnabled(r.Context(), id, true)
	case len(parts) == 4 && parts[3] == "disable" && r.Method == http.MethodPost:
		err = a.store.SetProxyHostRuleEnabled(r.Context(), id, false)
	default:
		a.writeAdminError(w, http.StatusNotFound, "not_found", "host rule action not found")
		return
	}
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := a.store.SyncProxyAllowedHostsSetting(r.Context()); err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := a.reloadRuntimeFromStore(r.Context()); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "reload_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"updated": true})
}

func (a *App) adminListCacheObjects(w http.ResponseWriter, r *http.Request) {
	filter := cacheFilterFromQuery(r)
	items, total, err := a.store.ListCacheObjects(r.Context(), filter)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"items": items, "total": total, "page": filter.Page, "page_size": filter.PageSize})
}

func (a *App) adminCacheObjectAction(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method != http.MethodDelete && r.Method != http.MethodGet {
		a.writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	idText := strings.TrimPrefix(path, "/cache/objects/")
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", "invalid cache object id")
		return
	}
	obj, err := a.store.GetCacheObject(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.writeAdminError(w, http.StatusNotFound, "not_found", "cache object not found")
			return
		}
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if r.Method == http.MethodGet {
		a.writeAdminData(w, map[string]any{"object": obj, "hash": a.cacheObjectHashDetail(obj)})
		return
	}
	if err := a.deleteCacheObject(r.Context(), obj); err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"deleted": true})
}

func (a *App) cacheObjectHashDetail(obj store.CacheObject) map[string]any {
	detail := map[string]any{
		"expected": []map[string]any{},
		"actual":   []map[string]any{},
	}
	if a.hashStore == nil || obj.CachePath == "" {
		return detail
	}
	expectedItems := make([]map[string]any, 0, 2)
	actualItems := make([]map[string]any, 0, 2)
	for _, kind := range []hashstore.HashKind{hashstore.HashSHA256, hashstore.HashSHA1} {
		if expected, err := a.hashStore.GetExpected(obj.CachePath, kind); err == nil {
			expectedItems = append(expectedItems, map[string]any{
				"hash_kind":         kind.Algorithm(),
				"record_type":       expected.RecordType,
				"expected_hash_hex": hex.EncodeToString(expected.ExpectedHash),
				"expected_size":     expected.ExpectedSize,
				"source_path":       expected.SourcePath,
				"updated_unix_nano": expected.UpdatedUnixNS,
			})
		}
		if actual, ok, err := a.hashStore.GetActual(obj.CachePath, kind); err == nil && ok {
			actualItems = append(actualItems, map[string]any{
				"hash_kind":          kind.Algorithm(),
				"actual_hash_hex":    hex.EncodeToString(actual.ActualHash),
				"size_bytes":         actual.SizeBytes,
				"mtime_unix_nano":    actual.MTimeUnixNS,
				"computed_unix_nano": actual.ComputedUnixNS,
			})
		}
	}
	detail["expected"] = expectedItems
	detail["actual"] = actualItems
	return detail
}

func (a *App) adminBatchDeleteCache(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DryRun bool `json:"dry_run"`
		store.CacheObjectFilter
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	filter := req.CacheObjectFilter
	filter.Page = 1
	filter.PageSize = 200
	items, total, err := a.store.ListCacheObjects(r.Context(), filter)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if req.DryRun {
		a.writeAdminData(w, map[string]any{"dry_run": true, "total": total, "sample": items})
		return
	}
	deleted := 0
	for {
		items, _, err := a.store.ListCacheObjects(r.Context(), filter)
		if err != nil {
			a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		if len(items) == 0 {
			break
		}
		deletedInBatch := 0
		for _, obj := range items {
			if err := a.deleteCacheObject(r.Context(), obj); err == nil {
				deleted++
				deletedInBatch++
			}
		}
		if deletedInBatch == 0 {
			break
		}
	}
	a.writeAdminData(w, map[string]any{"deleted": deleted, "matched": total})
}

func (a *App) adminPrewarm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URLs []string `json:"urls"`
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	results := make([]map[string]any, 0, len(req.URLs))
	for _, target := range req.URLs {
		rec := httptest.NewRecorder()
		preq := httptest.NewRequest(http.MethodGet, target, nil)
		a.Handler().ServeHTTP(rec, preq)
		results = append(results, map[string]any{"url": target, "status_code": rec.Code, "cache": rec.Header().Get(HeaderCache)})
	}
	a.writeAdminData(w, map[string]any{"items": results})
}

func (a *App) adminReconcileCache(w http.ResponseWriter, r *http.Request) {
	count := 0
	err := filepath.WalkDir(a.cfg.Cache.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		obj := a.cacheObjectFromPath(path, info.Size())
		if obj.CachePath == "" {
			return nil
		}
		if err := a.store.UpsertCacheObject(r.Context(), obj); err == nil {
			count++
		}
		return nil
	})
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "reconcile_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"scanned": count})
}

func (a *App) adminClearMemory(w http.ResponseWriter, r *http.Request) {
	if a.mem != nil {
		a.mem.Clear()
	}
	a.writeAdminData(w, map[string]any{"cleared": true})
}

func (a *App) adminAPKIndexes(w http.ResponseWriter, r *http.Request) {
	items, err := a.findFiles(func(path string) bool { return apkpkg.IsIndexFile(path) })
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "scan_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"items": items})
}

func (a *App) adminAPKPackages(w http.ResponseWriter, r *http.Request) {
	type item struct {
		IndexPath string `json:"index_cache_path"`
		Name      string `json:"package_name"`
		Version   string `json:"version"`
		Algorithm string `json:"checksum_algorithm"`
		Size      int64  `json:"size_bytes"`
	}
	page, pageSize := adminPageFromQuery(r)
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out := make([]item, 0, pageSize)
	total := 0
	indexes, err := a.findFiles(func(path string) bool { return apkpkg.IsIndexFile(path) })
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "scan_failed", err.Error())
		return
	}
	for _, indexPath := range indexes {
		members, err := apkpkg.ReadArchiveFile(indexPath)
		if err != nil {
			continue
		}
		for _, member := range members {
			for _, entry := range member.Entries {
				if entry.Name != "APKINDEX" && entry.Name != "DESCRIPTION" {
					continue
				}
				for _, pkg := range apkpkg.ParseIndex(entry.Body) {
					adminItem := item{IndexPath: indexPath, Name: pkg.Name, Version: pkg.Version, Algorithm: pkg.Algorithm, Size: pkg.Size}
					if query != "" && !strings.Contains(strings.ToLower(strings.Join([]string{adminItem.IndexPath, adminItem.Name, adminItem.Version, adminItem.Algorithm}, " ")), query) {
						continue
					}
					if total >= (page-1)*pageSize && len(out) < pageSize {
						out = append(out, adminItem)
					}
					total++
				}
			}
		}
	}
	a.writeAdminData(w, map[string]any{"items": out, "total": total, "page": page, "page_size": pageSize})
}

func (a *App) adminReloadAPKIndexes(w http.ResponseWriter, r *http.Request) {
	err := a.apkIndex.LoadFromRoot(a.cfg.Cache.Root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		a.writeAdminError(w, http.StatusInternalServerError, "reload_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"reloaded": true})
}

func (a *App) adminAPKKeys(w http.ResponseWriter, r *http.Request) {
	a.writeAdminData(w, map[string]any{"keys_dir": a.cfg.APK.KeysDir, "verify_signature": a.cfg.APK.VerifySignature})
}

func (a *App) adminReloadAPKKeys(w http.ResponseWriter, r *http.Request) {
	verifier, err := apkpkg.NewVerifier(a.cfg.APK.KeysDir)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "reload_failed", err.Error())
		return
	}
	a.apkVerifier = verifier
	a.writeAdminData(w, map[string]any{"reloaded": true})
}

func (a *App) adminAPTIndexes(w http.ResponseWriter, r *http.Request) {
	items, err := a.findFiles(func(path string) bool { return aptpkg.IsIndexFile(path) })
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "scan_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"items": items})
}

func (a *App) adminAPTRecords(w http.ResponseWriter, r *http.Request) {
	type item struct {
		SourceIndexPath string `json:"source_index_cache_path"`
		RecordType      string `json:"record_type"`
		TargetPath      string `json:"target_cache_path"`
		Filename        string `json:"filename"`
		PackageName     string `json:"package_name,omitempty"`
		Size            int64  `json:"size_bytes"`
		SHA256          string `json:"sha256"`
	}
	page, pageSize := adminPageFromQuery(r)
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	byHashOnly := parseBoolQuery(r.URL.Query().Get("by_hash"))
	out := make([]item, 0, pageSize)
	total := 0
	appendRecord := func(adminItem item) {
		haystack := strings.ToLower(strings.Join([]string{adminItem.SourceIndexPath, adminItem.RecordType, adminItem.TargetPath, adminItem.Filename, adminItem.PackageName, adminItem.SHA256}, " "))
		if byHashOnly && !strings.Contains(haystack, "/by-hash/") {
			return
		}
		if query != "" && !strings.Contains(haystack, query) {
			return
		}
		if total >= (page-1)*pageSize && len(out) < pageSize {
			out = append(out, adminItem)
		}
		total++
	}
	indexes, err := a.findFiles(func(path string) bool { return aptpkg.IsIndexFile(path) })
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "scan_failed", err.Error())
		return
	}
	for _, indexPath := range indexes {
		file, err := os.Open(indexPath)
		if err != nil {
			continue
		}
		reader, err := aptpkg.DecompressByName(filepath.Base(indexPath), file)
		if err != nil {
			_ = file.Close()
			continue
		}
		name := filepath.Base(indexPath)
		switch {
		case strings.HasPrefix(name, "Packages"), strings.HasPrefix(name, "Sources"):
			for _, rec := range aptpkg.ParsePackages(reader) {
				appendRecord(item{SourceIndexPath: indexPath, RecordType: "package_file", Filename: rec.Filename, PackageName: rec.Package, Size: rec.Size, SHA256: rec.SHA256})
			}
		case name == "Release" || name == "InRelease":
			for _, rec := range aptpkg.ParseRelease(reader) {
				appendRecord(item{SourceIndexPath: indexPath, RecordType: "release_file", Filename: rec.Filename, Size: rec.Size, SHA256: rec.SHA256})
			}
		}
		_ = file.Close()
	}
	a.writeAdminData(w, map[string]any{"items": out, "total": total, "page": page, "page_size": pageSize})
}

func (a *App) adminListAPTMirrors(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListAPTMirrors(r.Context(), false)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"items": items})
}

func (a *App) adminCreateAPTMirror(w http.ResponseWriter, r *http.Request) {
	var req store.APTMirror
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	if err := validateAPTMirror(&req); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	created, err := a.store.CreateAPTMirror(r.Context(), req)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := a.reloadRuntimeFromStore(r.Context()); err != nil {
		_ = a.store.DeleteAPTMirror(r.Context(), created.ID)
		a.writeAdminError(w, http.StatusBadRequest, "reload_failed", err.Error())
		return
	}
	a.writeAdminData(w, created)
}

func (a *App) adminAPTMirrorAction(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		a.writeAdminError(w, http.StatusNotFound, "not_found", "apt mirror not found")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", "invalid apt mirror id")
		return
	}
	switch {
	case len(parts) == 3 && r.Method == http.MethodPut:
		var req store.APTMirror
		if !a.decodeAdminJSON(w, r, &req) {
			return
		}
		req.ID = id
		if err := validateAPTMirror(&req); err != nil {
			a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		err = a.store.UpdateAPTMirror(r.Context(), req)
	case len(parts) == 3 && r.Method == http.MethodDelete:
		err = a.store.DeleteAPTMirror(r.Context(), id)
	case len(parts) == 4 && parts[3] == "enable" && r.Method == http.MethodPost:
		err = a.store.SetAPTMirrorEnabled(r.Context(), id, true)
	case len(parts) == 4 && parts[3] == "disable" && r.Method == http.MethodPost:
		err = a.store.SetAPTMirrorEnabled(r.Context(), id, false)
	case len(parts) == 4 && parts[3] == "test" && r.Method == http.MethodPost:
		a.adminTestAPTMirror(w, r, id)
		return
	case len(parts) == 4 && parts[3] == "sources-list" && r.Method == http.MethodGet:
		a.adminAPTMirrorSourcesList(w, r, id)
		return
	default:
		a.writeAdminError(w, http.StatusNotFound, "not_found", "apt mirror action not found")
		return
	}
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := a.reloadRuntimeFromStore(r.Context()); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "reload_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"updated": true})
}

func (a *App) adminTestAPTMirror(w http.ResponseWriter, r *http.Request, id int64) {
	mirror, err := a.store.GetAPTMirror(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.writeAdminError(w, http.StatusNotFound, "not_found", "apt mirror not found")
			return
		}
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			a.writeAdminError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}
	publicPath := strings.TrimRight(mirror.PublicPrefix, "/") + "/" + strings.TrimLeft(req.Path, "/")
	if req.Path == "" {
		publicPath = strings.TrimRight(mirror.PublicPrefix, "/") + "/"
	}
	testReq := httptest.NewRequest(http.MethodGet, publicPath, nil)
	target, err := aptMirrorTarget(mirror, testReq)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodHead, target.String(), nil)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	proxy := mirror.Proxy
	if proxy == "" {
		proxy = a.cfg.Proxy.UpstreamProxy
	}
	start := time.Now()
	resp, err := a.clients.Client(proxy).Do(upstreamReq)
	if err != nil {
		a.writeAdminData(w, map[string]any{"ok": false, "target": target.String(), "error": err.Error(), "duration_ms": time.Since(start).Milliseconds()})
		return
	}
	defer resp.Body.Close()
	a.writeAdminData(w, map[string]any{"ok": resp.StatusCode < 500, "target": target.String(), "status_code": resp.StatusCode, "duration_ms": time.Since(start).Milliseconds()})
}

func (a *App) adminAPTMirrorSourcesList(w http.ResponseWriter, r *http.Request, id int64) {
	mirror, err := a.store.GetAPTMirror(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.writeAdminError(w, http.StatusNotFound, "not_found", "apt mirror not found")
			return
		}
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	host := r.Host
	if host == "" {
		host = "cache.example:3142"
	}
	base := scheme + "://" + host + strings.TrimRight(mirror.PublicPrefix, "/")
	line := "deb " + base + " stable main"
	a.writeAdminData(w, map[string]any{"line": line, "base_url": base})
}

func (a *App) adminReloadAPTIndexes(w http.ResponseWriter, r *http.Request) {
	err := a.aptIndex.LoadFromRoot(filepath.Join(a.cfg.Cache.Root, "apt"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		a.writeAdminError(w, http.StatusInternalServerError, "reload_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"reloaded": true})
}

func (a *App) adminValidateAPT(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          int64  `json:"id"`
		CachePath   string `json:"cache_path"`
		RequestPath string `json:"request_path"`
	}
	if !a.decodeAdminJSON(w, r, &req) {
		return
	}
	cachePath := req.CachePath
	requestPath := req.RequestPath
	if req.ID > 0 {
		obj, err := a.store.GetCacheObject(r.Context(), req.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				a.writeAdminError(w, http.StatusNotFound, "not_found", "cache object not found")
				return
			}
			a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		cachePath = obj.CachePath
		if requestPath == "" {
			requestPath = obj.RequestPath
		}
	}
	if cachePath == "" {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", "cache_path is required")
		return
	}
	var err error
	cachePath, err = a.cleanCachePath(cachePath)
	if err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if requestPath == "" {
		requestPath = cachePath
	}
	if err := a.validateAPT(cachePath, cachePath, requestPath); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"valid": true})
}

func (a *App) adminRequestLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := a.store.ListRequestLogs(r.Context(), limit)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	a.writeAdminData(w, map[string]any{"items": items})
}

func (a *App) adminErrorLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	items, err := a.store.ListRequestLogs(r.Context(), 1000)
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	out := make([]store.RequestLog, 0, limit)
	for _, item := range items {
		if item.StatusCode >= http.StatusBadRequest || item.Error != "" {
			out = append(out, item)
			if len(out) >= limit {
				break
			}
		}
	}
	a.writeAdminData(w, map[string]any{"items": out})
}

func (a *App) adminSystemInfo(w http.ResponseWriter, r *http.Request, user store.AdminUser) {
	info := a.systemInfo(user)
	a.writeAdminData(w, info)
}

func (a *App) adminDiagnosticsPackage(w http.ResponseWriter, r *http.Request, user store.AdminUser) {
	requestLogs, logErr := a.store.ListRequestLogs(r.Context(), 1000)
	errorLogs := make([]store.RequestLog, 0)
	for _, item := range requestLogs {
		if item.StatusCode >= http.StatusBadRequest || item.Error != "" {
			errorLogs = append(errorLogs, item)
		}
	}
	upstreams, upstreamErr := a.store.ListUpstreams(r.Context(), false)
	hashStats, hashErr := a.hashStore.Stats()
	cacheSummary, cacheErr := a.cacheDiskSummary()
	payloads := map[string]any{
		"config.json":        redactedConfig(a.cfg),
		"system.json":        a.systemInfo(user),
		"request_logs.json":  map[string]any{"items": requestLogs, "error": errorString(logErr)},
		"errors.json":        map[string]any{"items": errorLogs, "error": errorString(logErr)},
		"upstreams.json":     map[string]any{"items": upstreams, "error": errorString(upstreamErr)},
		"cache_summary.json": map[string]any{"summary": cacheSummary, "error": errorString(cacheErr)},
		"hash_store.json":    map[string]any{"status": hashStats, "error": errorString(hashErr)},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, payload := range payloads {
		entry, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			a.writeAdminError(w, http.StatusInternalServerError, "diagnostics_failed", err.Error())
			return
		}
		enc := json.NewEncoder(entry)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			_ = zw.Close()
			a.writeAdminError(w, http.StatusInternalServerError, "diagnostics_failed", err.Error())
			return
		}
	}
	if err := zw.Close(); err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "diagnostics_failed", err.Error())
		return
	}
	filename := "apk-cache-diagnostics-" + time.Now().UTC().Format("20060102T150405Z") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (a *App) adminHashStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := a.hashStore.Stats()
	if err != nil {
		a.writeAdminError(w, http.StatusInternalServerError, "hash_store_error", err.Error())
		return
	}
	a.writeAdminData(w, stats)
}

func (a *App) reloadRuntimeFromStore(ctx context.Context) error {
	next, err := a.store.LoadRuntimeConfig(ctx, a.cfg)
	if err != nil {
		return err
	}
	return a.applyRuntimeConfig(next)
}

func (a *App) applyRuntimeConfig(cfg *config.Config) error {
	cfg = runtimeConfigWithCurrentRestartFields(a.cfg, cfg)
	indexTTL, err := time.ParseDuration(cfg.Cache.IndexTTL)
	if err != nil {
		return err
	}
	packageTTL, err := time.ParseDuration(cfg.Cache.PackageTTL)
	if err != nil {
		return err
	}
	actualRevalidate, err := time.ParseDuration(cfg.HashStore.ActualRevalidateInterval)
	if err != nil {
		return err
	}
	clients, err := NewHTTPClientFactory(cfg.Transport)
	if err != nil {
		return err
	}
	var mem *cachepkg.Memory
	var maxItemSize int64
	if cfg.Cache.Memory.Enabled {
		maxSize, err := cachepkg.ParseSize(cfg.Cache.Memory.MaxSize)
		if err != nil {
			return err
		}
		maxItemSize, err = cachepkg.ParseSize(cfg.Cache.Memory.MaxItemSize)
		if err != nil {
			return err
		}
		ttl, err := time.ParseDuration(cfg.Cache.Memory.TTL)
		if err != nil {
			return err
		}
		mem = cachepkg.NewMemory(maxSize, cfg.Cache.Memory.MaxItems, ttl, a.metrics)
	}
	apkManager := upstream.NewManager(clients)
	apkManager.SetMetricsHooks(func() { a.metrics.UpstreamRequests.Inc() }, func() { a.metrics.UpstreamFailovers.Inc() })
	for _, candidate := range cfg.Upstreams {
		kind := strings.ToLower(strings.TrimSpace(candidate.Kind))
		if kind != "" && kind != "apk" {
			continue
		}
		apkManager.Add(upstream.NewServer(candidate.URL, candidate.Proxy, candidate.Name))
	}
	verifier, err := apkpkg.NewVerifier(cfg.APK.KeysDir)
	if err != nil {
		return err
	}
	aptMirrors, err := a.store.ListAPTMirrors(context.Background(), true)
	if err != nil {
		return err
	}
	proxyHostRules, err := a.store.ListProxyHostRules(context.Background(), false)
	if err != nil {
		return err
	}
	oldMem := a.mem
	a.cfg = cfg
	a.indexTTL = indexTTL
	a.pkgTTL = packageTTL
	a.clients = clients
	a.mem = mem
	a.memMax = maxItemSize
	a.apkUpstreams = apkManager
	a.apkVerifier = verifier
	a.aptMirrors = aptMirrors
	a.proxyHostRulesConfigured = len(proxyHostRules) > 0
	a.hashStore.UpdateOptions(cfg.HashStore.TrustFileStat, actualRevalidate)
	if oldMem != nil {
		oldMem.Stop()
	}
	return nil
}

func runtimeConfigWithCurrentRestartFields(current, next *config.Config) *config.Config {
	cfg := *next
	cfg.Upstreams = append([]config.UpstreamConfig(nil), next.Upstreams...)
	cfg.Proxy.AllowedHosts = append([]string(nil), next.Proxy.AllowedHosts...)
	cfg.Server.Listen = current.Server.Listen
	cfg.Database = current.Database
	cfg.Cache.Root = current.Cache.Root
	cfg.Cache.DataRoot = current.Cache.DataRoot
	cfg.HashStore.Path = current.HashStore.Path
	cfg.HashStore.RebuildOnCorruption = current.HashStore.RebuildOnCorruption
	return &cfg
}

func (a *App) deleteCacheObject(ctx context.Context, obj store.CacheObject) error {
	cachePath, err := a.cleanCachePath(obj.CachePath)
	if err != nil {
		return err
	}
	if err := os.Remove(cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if a.mem != nil {
		a.mem.Delete(cachePath)
	}
	a.deleteHashMetadata(cachePath, obj.Class)
	return a.store.DeleteCacheObjectRecord(ctx, obj.ID)
}

func (a *App) cacheObjectFromPath(path string, size int64) store.CacheObject {
	rel, err := filepath.Rel(a.cfg.Cache.Root, path)
	if err != nil {
		return store.CacheObject{}
	}
	relSlash := filepath.ToSlash(rel)
	obj := store.CacheObject{CachePath: path, SizeBytes: size, CacheStatus: "ok", ValidationStatus: "unknown"}
	switch {
	case strings.HasPrefix(relSlash, "apt/"):
		parts := strings.SplitN(strings.TrimPrefix(relSlash, "apt/"), "/", 2)
		obj.Protocol = "apt"
		obj.Host = parts[0]
		if len(parts) > 1 {
			obj.RequestPath = "/" + parts[1]
		}
	case strings.HasPrefix(relSlash, "proxy/"):
		obj.Protocol = "proxy"
		obj.RequestPath = "/" + relSlash
	default:
		obj.Protocol = "apk"
		obj.RequestPath = "/" + relSlash
	}
	if apkpkg.IsIndexFile(path) || aptpkg.IsIndexFile(path) {
		obj.Class = "index"
	} else if apkpkg.IsPackageFile(path) || strings.HasSuffix(path, ".deb") {
		obj.Class = "package"
	} else {
		obj.Class = "other"
	}
	return obj
}

func (a *App) findFiles(match func(string) bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(a.cfg.Cache.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if match(filepath.ToSlash(path)) || match(path) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func (a *App) cleanCachePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ErrInvalidCachePath
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(a.cfg.Cache.Root, cleaned)
	}
	root, err := filepath.Abs(a.cfg.Cache.Root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", ErrInvalidCachePath
	}
	return target, nil
}

func (a *App) cacheDiskSummary() (map[string]any, error) {
	summary := map[string]any{
		"root":       a.cfg.Cache.Root,
		"files":      0,
		"dirs":       0,
		"size_bytes": int64(0),
		"protocols":  map[string]map[string]any{},
	}
	protocols := summary["protocols"].(map[string]map[string]any)
	err := filepath.WalkDir(a.cfg.Cache.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			summary["dirs"] = summary["dirs"].(int) + 1
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		summary["files"] = summary["files"].(int) + 1
		summary["size_bytes"] = summary["size_bytes"].(int64) + info.Size()
		protocol := "apk"
		if rel, err := filepath.Rel(a.cfg.Cache.Root, path); err == nil {
			rel = filepath.ToSlash(rel)
			switch {
			case strings.HasPrefix(rel, "apt/"):
				protocol = "apt"
			case strings.HasPrefix(rel, "proxy/"):
				protocol = "proxy"
			}
		}
		if protocols[protocol] == nil {
			protocols[protocol] = map[string]any{"files": 0, "size_bytes": int64(0)}
		}
		protocols[protocol]["files"] = protocols[protocol]["files"].(int) + 1
		protocols[protocol]["size_bytes"] = protocols[protocol]["size_bytes"].(int64) + info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return summary, nil
	}
	return summary, err
}

func summarizeRequestLogs(logs []store.RequestLog, recentLimit int) (map[string]any, []store.RequestLog, []store.RequestLog) {
	stats := map[string]any{
		"total":          len(logs),
		"errors":         0,
		"cache_hits":     0,
		"cache_misses":   0,
		"memory_hits":    0,
		"bytes_sent":     int64(0),
		"by_protocol":    map[string]int{},
		"by_status_code": map[string]int{},
	}
	byProtocol := stats["by_protocol"].(map[string]int)
	byStatusCode := stats["by_status_code"].(map[string]int)
	recentRequests := make([]store.RequestLog, 0, recentLimit)
	recentErrors := make([]store.RequestLog, 0, recentLimit)
	for _, item := range logs {
		stats["bytes_sent"] = stats["bytes_sent"].(int64) + item.BytesSent
		if item.StatusCode >= http.StatusBadRequest || item.Error != "" {
			stats["errors"] = stats["errors"].(int) + 1
			if len(recentErrors) < recentLimit {
				recentErrors = append(recentErrors, item)
			}
		}
		switch item.CacheStatus {
		case CacheHit:
			stats["cache_hits"] = stats["cache_hits"].(int) + 1
		case CacheMiss:
			stats["cache_misses"] = stats["cache_misses"].(int) + 1
		case CacheMemoryHit:
			stats["memory_hits"] = stats["memory_hits"].(int) + 1
		}
		if item.Protocol != "" {
			byProtocol[item.Protocol]++
		}
		byStatusCode[strconv.Itoa(item.StatusCode)]++
		if len(recentRequests) < recentLimit {
			recentRequests = append(recentRequests, item)
		}
	}
	return stats, recentRequests, recentErrors
}

func (a *App) systemInfo(user store.AdminUser) map[string]any {
	build := map[string]any{}
	if info, ok := debug.ReadBuildInfo(); ok {
		build["path"] = info.Path
		build["main"] = map[string]string{"path": info.Main.Path, "version": info.Main.Version, "sum": info.Main.Sum}
		settings := map[string]string{}
		for _, setting := range info.Settings {
			settings[setting.Key] = setting.Value
		}
		build["settings"] = settings
	}
	deprecatedSettings, _ := a.store.ExistingSettingKeys(context.Background(), []string{"admin.bootstrap_token", "admin.session_secret"})
	return map[string]any{
		"go": map[string]any{
			"version":       runtime.Version(),
			"os":            runtime.GOOS,
			"arch":          runtime.GOARCH,
			"num_cpu":       runtime.NumCPU(),
			"num_goroutine": runtime.NumGoroutine(),
		},
		"process": map[string]any{
			"pid":            os.Getpid(),
			"started_at":     a.startedAt.Format(time.RFC3339Nano),
			"uptime_seconds": int64(time.Since(a.startedAt).Seconds()),
		},
		"paths": map[string]any{
			"cache_root":   a.cfg.Cache.Root,
			"data_root":    a.cfg.Cache.DataRoot,
			"database":     a.store.Path(),
			"hash_store":   a.cfg.HashStore.Path,
			"apk_keys_dir": a.cfg.APK.KeysDir,
		},
		"features": map[string]any{
			"apk_enabled":     a.cfg.APK.Enabled,
			"apt_enabled":     a.cfg.APT.Enabled,
			"proxy_enabled":   a.cfg.Proxy.Enabled,
			"memory_enabled":  a.cfg.Cache.Memory.Enabled,
			"verify_apk_hash": a.cfg.APK.VerifyHash,
			"verify_apt_hash": a.cfg.APT.VerifyHash,
		},
		"admin": map[string]any{
			"username":              user.Username,
			"is_default_credential": user.IsDefaultCredential,
			"last_login_at":         nullableString(user.LastLoginAt),
		},
		"deprecated_settings": deprecatedSettings,
		"build":               build,
	}
}

func redactedConfig(cfg *config.Config) map[string]any {
	upstreams := make([]map[string]any, 0, len(cfg.Upstreams))
	for _, item := range cfg.Upstreams {
		upstreams = append(upstreams, map[string]any{
			"name":  item.Name,
			"url":   item.URL,
			"proxy": redactURL(item.Proxy),
			"kind":  item.Kind,
		})
	}
	return map[string]any{
		"server": map[string]any{"listen": cfg.Server.Listen},
		"database": map[string]any{
			"path": cfg.Database.Path,
		},
		"admin": map[string]any{"auth_mode": "default-admin-db-session"},
		"hash_store": map[string]any{
			"path":                       cfg.HashStore.Path,
			"rebuild_on_corruption":      cfg.HashStore.RebuildOnCorruption,
			"trust_file_stat":            cfg.HashStore.TrustFileStat,
			"actual_revalidate_interval": cfg.HashStore.ActualRevalidateInterval,
		},
		"cache": map[string]any{
			"root":        cfg.Cache.Root,
			"data_root":   cfg.Cache.DataRoot,
			"index_ttl":   cfg.Cache.IndexTTL,
			"package_ttl": cfg.Cache.PackageTTL,
			"memory":      cfg.Cache.Memory,
		},
		"transport": cfg.Transport,
		"apk":       cfg.APK,
		"apt":       cfg.APT,
		"proxy": map[string]any{
			"enabled":                    cfg.Proxy.Enabled,
			"allow_connect":              cfg.Proxy.AllowConnect,
			"cache_non_package_requests": cfg.Proxy.CacheNonPackage,
			"upstream_proxy":             redactURL(cfg.Proxy.UpstreamProxy),
			"allowed_hosts":              append([]string(nil), cfg.Proxy.AllowedHosts...),
		},
		"upstreams": upstreams,
	}
}

func redactURL(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User == nil {
		return value
	}
	username := parsed.User.Username()
	if _, ok := parsed.User.Password(); ok {
		parsed.User = url.UserPassword(username, "<redacted>")
	} else {
		parsed.User = url.User(username)
	}
	return parsed.String()
}

func nullableString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func cacheFilterFromQuery(r *http.Request) store.CacheObjectFilter {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	minSize, _ := strconv.ParseInt(q.Get("min_size"), 10, 64)
	maxSize, _ := strconv.ParseInt(q.Get("max_size"), 10, 64)
	return store.CacheObjectFilter{
		Protocol: q.Get("protocol"),
		Class:    q.Get("class"),
		Host:     q.Get("host"),
		Query:    q.Get("q"),
		Status:   q.Get("status"),
		MinSize:  minSize,
		MaxSize:  maxSize,
		Page:     page,
		PageSize: pageSize,
	}
}

func adminPageFromQuery(r *http.Request) (int, int) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 500 {
		pageSize = 500
	}
	return page, pageSize
}

func parseBoolQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func (a *App) decodeAdminJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		a.writeAdminError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	return true
}

func (a *App) writeAdminData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(adminResponse{OK: true, Data: data})
}

func (a *App) writeAdminError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(adminResponse{OK: false, Error: &adminError{Code: code, Message: message}})
}

func (a *App) setSessionCookies(w http.ResponseWriter, r *http.Request, token, csrf string, expires time.Time) {
	secure := r.TLS != nil
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure})
	http.SetCookie(w, &http.Cookie{Name: adminCSRFCookie, Value: csrf, Path: "/", Expires: expires, HttpOnly: false, SameSite: http.SameSiteLaxMode, Secure: secure})
}

func (a *App) clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil
	expired := time.Unix(0, 0)
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: "", Path: "/", Expires: expired, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure})
	http.SetCookie(w, &http.Cookie{Name: adminCSRFCookie, Value: "", Path: "/", Expires: expired, MaxAge: -1, HttpOnly: false, SameSite: http.SameSiteLaxMode, Secure: secure})
}

func ensureDefaultAdmin(ctx context.Context, st *store.Store) error {
	count, err := st.CountAdmins(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := hashPassword(defaultAdminPass)
	if err != nil {
		return err
	}
	return st.CreateAdmin(ctx, defaultAdminUser, hash, "argon2id", true)
}

func adminUserPayload(user store.AdminUser, csrf string) map[string]any {
	return map[string]any{
		"authenticated":         true,
		"username":              user.Username,
		"csrf_token":            csrf,
		"is_default_credential": user.IsDefaultCredential,
		"last_login_at":         nullableString(user.LastLoginAt),
		"default_username":      defaultAdminUser,
	}
}

func csrfFromRequest(r *http.Request) string {
	if cookie, err := r.Cookie(adminCSRFCookie); err == nil {
		return cookie.Value
	}
	return ""
}

func (a *App) loginBlocked(remote string) bool {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	fail := a.loginFailures[remote]
	return !fail.blockedTo.IsZero() && time.Now().Before(fail.blockedTo)
}

func (a *App) recordLoginFailure(remote string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	fail := a.loginFailures[remote]
	fail.count++
	if fail.count >= 5 {
		fail.blockedTo = time.Now().Add(1 * time.Minute)
	}
	a.loginFailures[remote] = fail
}

func (a *App) clearLoginFailures(remote string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	delete(a.loginFailures, remote)
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 16*1024, 1, 32)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("argon2id$v=19$m=16384,t=2,p=1$%s$%s", enc.EncodeToString(salt), enc.EncodeToString(hash)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 2, 16*1024, 1, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func (a *App) newToken() (plain, hashed string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	return plain, a.hashToken(plain), nil
}

func (a *App) hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func validateProxyHostRule(rule *store.ProxyHostRule) error {
	rule.Host = normalizeProxyHost(rule.Host)
	if rule.Host == "" {
		return errors.New("host is required")
	}
	if strings.ContainsAny(rule.Host, "/\\ \t\r\n") {
		return errors.New("host must not include scheme, path, or whitespace")
	}
	return nil
}

func validateAPTMirror(mirror *store.APTMirror) error {
	mirror.Name = strings.TrimSpace(mirror.Name)
	if mirror.Name == "" {
		mirror.Name = "APT Mirror"
	}
	prefix, err := normalizeAPTPublicPrefix(mirror.PublicPrefix)
	if err != nil {
		return err
	}
	mirror.PublicPrefix = prefix
	parsed, err := url.Parse(strings.TrimSpace(mirror.UpstreamURL))
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("upstream_url must start with http:// or https://")
	}
	if parsed.Host == "" {
		return errors.New("upstream_url must include host")
	}
	mirror.UpstreamURL = strings.TrimRight(parsed.String(), "/")
	if mirror.Proxy != "" {
		proxyURL, err := url.Parse(mirror.Proxy)
		if err != nil {
			return err
		}
		if proxyURL.Host == "" || (proxyURL.Scheme != "http" && proxyURL.Scheme != "https" && proxyURL.Scheme != "socks5") {
			return errors.New("proxy must start with socks5://, http://, or https://")
		}
	}
	return nil
}

func normalizeAPTPublicPrefix(prefix string) (string, error) {
	prefix = "/" + strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "/" {
		return "", errors.New("public_prefix must not be root")
	}
	if strings.Contains(prefix, "..") {
		return "", errors.New("public_prefix must not contain '..'")
	}
	reserved := []string{"/admin", "/api", "/_health", "/metrics", "/alpine"}
	for _, item := range reserved {
		if prefix == item || strings.HasPrefix(prefix, item+"/") {
			return "", fmt.Errorf("public_prefix %q is reserved", prefix)
		}
	}
	return prefix, nil
}

func normalizeProxyHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")
	if slash := strings.IndexByte(host, '/'); slash >= 0 {
		host = host[:slash]
	}
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	return strings.Trim(host, "[]")
}

func randomID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(raw)
}

func protocolForRequest(r *http.Request) string {
	switch classifyRequest(r).protocol {
	case requestProtocolAPK:
		return "apk"
	case requestProtocolAPT:
		return "apt"
	case requestProtocolProxy:
		return "proxy"
	default:
		return "unknown"
	}
}
