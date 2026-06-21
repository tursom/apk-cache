package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tursom/apk-cache/internal/config"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

type Setting struct {
	Key             string          `json:"key"`
	Value           json.RawMessage `json:"value"`
	ValueType       string          `json:"value_type"`
	RestartRequired bool            `json:"restart_required"`
	UpdatedAt       string          `json:"updated_at"`
	Source          string          `json:"source"`
	Editable        bool            `json:"editable"`
	HotReload       bool            `json:"hot_reload"`
}

type APTMirror struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	PublicPrefix string `json:"public_prefix"`
	UpstreamURL  string `json:"upstream_url"`
	Proxy        string `json:"proxy"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type ProxyHostRule struct {
	ID          int64  `json:"id"`
	Host        string `json:"host"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Upstream struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Proxy     string `json:"proxy"`
	Kind      string `json:"kind"`
	Enabled   bool   `json:"enabled"`
	Priority  int    `json:"priority"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type AdminUser struct {
	ID                  int64
	Username            string
	PasswordHash        string
	PasswordAlgo        string
	IsDefaultCredential bool
	CreatedAt           string
	UpdatedAt           string
	LastLoginAt         sql.NullString
}

type AdminSession struct {
	ID            string
	UserID        int64
	TokenHash     string
	CSRFTokenHash string
	UserAgent     string
	RemoteAddr    string
	ExpiresAt     string
	CreatedAt     string
	LastSeenAt    string
}

type CacheObject struct {
	ID               int64  `json:"id"`
	Protocol         string `json:"protocol"`
	Class            string `json:"class"`
	Host             string `json:"host"`
	RequestPath      string `json:"request_path"`
	CachePath        string `json:"cache_path"`
	SizeBytes        int64  `json:"size_bytes"`
	ContentType      string `json:"content_type"`
	CacheStatus      string `json:"cache_status"`
	ValidationStatus string `json:"validation_status"`
	LastError        string `json:"last_error"`
	FirstCachedAt    string `json:"first_cached_at"`
	LastAccessedAt   string `json:"last_accessed_at"`
	UpdatedAt        string `json:"updated_at"`
}

type CacheObjectFilter struct {
	Protocol string `json:"protocol"`
	Class    string `json:"class"`
	Host     string `json:"host"`
	Query    string `json:"q"`
	Status   string `json:"status"`
	MinSize  int64  `json:"min_size"`
	MaxSize  int64  `json:"max_size"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

type RequestLog struct {
	ID           int64  `json:"id"`
	TS           string `json:"ts"`
	Method       string `json:"method"`
	Protocol     string `json:"protocol"`
	Host         string `json:"host"`
	Path         string `json:"path"`
	StatusCode   int    `json:"status_code"`
	CacheStatus  string `json:"cache_status"`
	UpstreamName string `json:"upstream_name"`
	DurationMS   int64  `json:"duration_ms"`
	BytesSent    int64  `json:"bytes_sent"`
	Error        string `json:"error"`
}

func DefaultDatabasePath(cfg *config.Config) string {
	if cfg.Database.Path != "" {
		return cfg.Database.Path
	}
	return filepath.Join(cfg.Cache.DataRoot, "apk-cache.db")
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.configure(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

func (s *Store) configure(ctx context.Context) error {
	for _, stmt := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS admin_users (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			password_algo TEXT NOT NULL,
			is_default_credential INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_login_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			csrf_token_hash TEXT NOT NULL,
			user_agent TEXT,
			remote_addr TEXT,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES admin_users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			value_type TEXT NOT NULL,
			restart_required INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS upstreams (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			proxy TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'apk',
			enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 100,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_upstreams_kind_enabled_priority
			ON upstreams(kind, enabled, priority)`,
		`CREATE TABLE IF NOT EXISTS apt_mirrors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			public_prefix TEXT NOT NULL UNIQUE,
			upstream_url TEXT NOT NULL,
			proxy TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_apt_mirrors_enabled_prefix
			ON apt_mirrors(enabled, public_prefix)`,
		`CREATE TABLE IF NOT EXISTS proxy_host_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host TEXT NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 1,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_proxy_host_rules_enabled_host
			ON proxy_host_rules(enabled, host)`,
		`CREATE TABLE IF NOT EXISTS cache_objects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			protocol TEXT NOT NULL,
			class TEXT NOT NULL,
			host TEXT NOT NULL,
			request_path TEXT NOT NULL,
			cache_path TEXT NOT NULL UNIQUE,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			content_type TEXT,
			cache_status TEXT NOT NULL DEFAULT 'ok',
			validation_status TEXT NOT NULL DEFAULT 'unknown',
			last_error TEXT,
			first_cached_at TEXT NOT NULL,
			last_accessed_at TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cache_objects_protocol_host ON cache_objects(protocol, host)`,
		`CREATE INDEX IF NOT EXISTS idx_cache_objects_path ON cache_objects(request_path)`,
		`CREATE INDEX IF NOT EXISTS idx_cache_objects_updated ON cache_objects(updated_at)`,
		`CREATE TABLE IF NOT EXISTS apk_package_summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			index_cache_path TEXT NOT NULL,
			package_name TEXT NOT NULL,
			version TEXT NOT NULL,
			arch TEXT,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			checksum_algorithm TEXT NOT NULL,
			package_cache_path TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_apk_package_summaries_name ON apk_package_summaries(package_name)`,
		`CREATE TABLE IF NOT EXISTS apt_record_summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_index_cache_path TEXT NOT NULL,
			record_type TEXT NOT NULL,
			target_cache_path TEXT NOT NULL,
			filename TEXT NOT NULL,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			package_name TEXT,
			version TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_apt_record_summaries_package ON apt_record_summaries(package_name)`,
		`CREATE INDEX IF NOT EXISTS idx_apt_record_summaries_target ON apt_record_summaries(target_cache_path)`,
		`CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL,
			method TEXT NOT NULL,
			protocol TEXT NOT NULL,
			host TEXT,
			path TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			cache_status TEXT,
			upstream_name TEXT,
			duration_ms INTEGER NOT NULL,
			bytes_sent INTEGER NOT NULL DEFAULT 0,
			error TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_ts ON request_logs(ts)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_path ON request_logs(path)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_status ON request_logs(status_code)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?)`,
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, stmt := range stmts {
		if strings.Contains(stmt, "?") {
			if _, err := s.db.ExecContext(ctx, stmt, now); err != nil {
				return err
			}
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "admin_users", "is_default_credential", `ALTER TABLE admin_users ADD COLUMN is_default_credential INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	return nil
}

func (s *Store) EnsureRuntimeConfig(ctx context.Context, cfg *config.Config) (*config.Config, bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings`).Scan(&count); err != nil {
		return nil, false, err
	}
	imported := false
	if count == 0 {
		if err := s.ImportRuntimeConfig(ctx, cfg); err != nil {
			return nil, false, err
		}
		imported = true
	}
	if err := s.ensureRuntimeSettingDefaults(ctx, cfg); err != nil {
		return nil, false, err
	}
	if err := s.ensureProxyHostRulesFromAllowedHosts(ctx); err != nil {
		return nil, false, err
	}
	loaded, err := s.LoadRuntimeConfig(ctx, cfg)
	return loaded, imported, err
}

func (s *Store) ImportRuntimeConfig(ctx context.Context, cfg *config.Config) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, def := range settingDefs {
		value, err := def.marshal(cfg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO settings(key, value_json, value_type, restart_required, updated_at) VALUES(?, ?, ?, ?, ?)`,
			def.key, string(value), def.valueType, boolInt(def.restartRequired), nowText()); err != nil {
			return err
		}
	}
	for idx, up := range cfg.Upstreams {
		name := strings.TrimSpace(up.Name)
		if name == "" {
			name = fmt.Sprintf("upstream-%d", idx+1)
		}
		kind := strings.ToLower(strings.TrimSpace(up.Kind))
		if kind == "" {
			kind = "apk"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO upstreams(name, url, proxy, kind, enabled, priority, created_at, updated_at) VALUES(?, ?, ?, ?, 1, ?, ?, ?)`,
			name, up.URL, up.Proxy, kind, 100+idx, nowText(), nowText()); err != nil {
			return err
		}
	}
	for _, host := range cfg.Proxy.AllowedHosts {
		host = normalizeHostRule(host)
		if host == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO proxy_host_rules(host, enabled, description, created_at, updated_at) VALUES(?, 1, ?, ?, ?)`,
			host, "imported from proxy.allowed_hosts", nowText(), nowText()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LoadRuntimeConfig(ctx context.Context, base *config.Config) (*config.Config, error) {
	cfg := cloneConfig(base)
	rows, err := s.db.QueryContext(ctx, `SELECT key, value_json FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		def := findSettingDef(key)
		if def == nil {
			continue
		}
		if !settingEditable(def) {
			continue
		}
		if err := def.apply(cfg, json.RawMessage(raw)); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	allUpstreams, err := s.ListUpstreams(ctx, false)
	if err != nil {
		return nil, err
	}
	if len(allUpstreams) > 0 {
		cfg.Upstreams = cfg.Upstreams[:0]
		for _, up := range allUpstreams {
			if !up.Enabled {
				continue
			}
			cfg.Upstreams = append(cfg.Upstreams, config.UpstreamConfig{
				Name:  up.Name,
				URL:   up.URL,
				Proxy: up.Proxy,
				Kind:  up.Kind,
			})
		}
	}
	hostRules, err := s.ListProxyHostRules(ctx, true)
	if err != nil {
		return nil, err
	}
	cfg.Proxy.AllowedHosts = cfg.Proxy.AllowedHosts[:0]
	for _, rule := range hostRules {
		cfg.Proxy.AllowedHosts = append(cfg.Proxy.AllowedHosts, rule.Host)
	}
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *Store) ListSettings(ctx context.Context) ([]Setting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value_json, value_type, restart_required, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Setting
	for rows.Next() {
		var item Setting
		var raw string
		var restart int
		if err := rows.Scan(&item.Key, &raw, &item.ValueType, &restart, &item.UpdatedAt); err != nil {
			return nil, err
		}
		def := findSettingDef(item.Key)
		item.Value = json.RawMessage(raw)
		item.RestartRequired = restart != 0
		item.Source = "database"
		item.Editable = def != nil
		item.HotReload = def != nil && !def.restartRequired
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ExistingSettingKeys(ctx context.Context, keys []string) ([]string, error) {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		var exists int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM settings WHERE key = ?`, key).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, nil
}

func (s *Store) UpdateSettings(ctx context.Context, base *config.Config, values map[string]json.RawMessage) (*config.Config, []string, error) {
	next, restartKeys, err := s.ValidateSettings(ctx, base, values)
	if err != nil {
		return nil, nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for key, raw := range values {
		def := findSettingDef(key)
		if !settingEditable(def) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value_json, value_type, restart_required, updated_at) VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET
				value_json = excluded.value_json,
				value_type = excluded.value_type,
				restart_required = excluded.restart_required,
				updated_at = excluded.updated_at`,
			key, string(raw), def.valueType, boolInt(def.restartRequired), nowText()); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return next, restartKeys, nil
}

func (s *Store) ValidateSettings(ctx context.Context, base *config.Config, values map[string]json.RawMessage) (*config.Config, []string, error) {
	next, err := s.LoadRuntimeConfig(ctx, base)
	if err != nil {
		return nil, nil, err
	}
	restartKeys := make([]string, 0)
	for key, raw := range values {
		def := findSettingDef(key)
		if def == nil {
			return nil, nil, fmt.Errorf("unknown setting %q", key)
		}
		if !settingEditable(def) {
			current, err := def.marshal(next)
			if err != nil {
				return nil, nil, err
			}
			if string(current) != string(raw) {
				return nil, nil, fmt.Errorf("%s is read-only", key)
			}
			continue
		}
		if err := def.apply(next, raw); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", key, err)
		}
		if def.restartRequired {
			restartKeys = append(restartKeys, key)
		}
	}
	if err := config.Validate(next); err != nil {
		return nil, nil, err
	}
	return next, restartKeys, nil
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&count)
	return count, err
}

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash, passwordAlgo string, defaultCredential ...bool) error {
	isDefault := false
	if len(defaultCredential) > 0 {
		isDefault = defaultCredential[0]
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_users(id, username, password_hash, password_algo, is_default_credential, created_at, updated_at) VALUES(1, ?, ?, ?, ?, ?, ?)`,
		username, passwordHash, passwordAlgo, boolInt(isDefault), nowText(), nowText())
	return err
}

func (s *Store) GetAdminByUsername(ctx context.Context, username string) (AdminUser, error) {
	var user AdminUser
	var isDefault int
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, password_algo, is_default_credential, created_at, updated_at, last_login_at FROM admin_users WHERE username = ?`, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.PasswordAlgo, &isDefault, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt)
	user.IsDefaultCredential = isDefault != 0
	return user, err
}

func (s *Store) GetAdminByID(ctx context.Context, id int64) (AdminUser, error) {
	var user AdminUser
	var isDefault int
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, password_algo, is_default_credential, created_at, updated_at, last_login_at FROM admin_users WHERE id = ?`, id).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.PasswordAlgo, &isDefault, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt)
	user.IsDefaultCredential = isDefault != 0
	return user, err
}

func (s *Store) UpdateAdminUsername(ctx context.Context, id int64, username string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_users SET username = ?, is_default_credential = 0, updated_at = ? WHERE id = ?`,
		username, nowText(), id)
	return err
}

func (s *Store) UpdateAdminPassword(ctx context.Context, id int64, passwordHash, passwordAlgo string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_users SET password_hash = ?, password_algo = ?, is_default_credential = 0, updated_at = ? WHERE id = ?`,
		passwordHash, passwordAlgo, nowText(), id)
	return err
}

func (s *Store) MarkAdminLogin(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_users SET last_login_at = ? WHERE id = ?`, nowText(), id)
	return err
}

func (s *Store) CreateSession(ctx context.Context, session AdminSession) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_sessions(id, user_id, token_hash, csrf_token_hash, user_agent, remote_addr, expires_at, created_at, last_seen_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.TokenHash, session.CSRFTokenHash, session.UserAgent, session.RemoteAddr, session.ExpiresAt, session.CreatedAt, session.LastSeenAt)
	return err
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (AdminSession, error) {
	var session AdminSession
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, token_hash, csrf_token_hash, user_agent, remote_addr, expires_at, created_at, last_seen_at FROM admin_sessions WHERE token_hash = ?`, tokenHash).
		Scan(&session.ID, &session.UserID, &session.TokenHash, &session.CSRFTokenHash, &session.UserAgent, &session.RemoteAddr, &session.ExpiresAt, &session.CreatedAt, &session.LastSeenAt)
	return session, err
}

func (s *Store) TouchSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_sessions SET last_seen_at = ? WHERE id = ?`, nowText(), id)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at <= ?`, nowText())
	return err
}

func (s *Store) ListUpstreams(ctx context.Context, enabledOnly bool) ([]Upstream, error) {
	query := `SELECT id, name, url, proxy, kind, enabled, priority, created_at, updated_at FROM upstreams`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY kind, priority, id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upstream
	for rows.Next() {
		var item Upstream
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.URL, &item.Proxy, &item.Kind, &enabled, &item.Priority, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateUpstream(ctx context.Context, up Upstream) (Upstream, error) {
	if up.Kind == "" {
		up.Kind = "apk"
	}
	if up.Priority == 0 {
		up.Priority = 100
	}
	if up.Name == "" {
		up.Name = "upstream"
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO upstreams(name, url, proxy, kind, enabled, priority, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		up.Name, up.URL, up.Proxy, strings.ToLower(up.Kind), boolInt(up.Enabled), up.Priority, nowText(), nowText())
	if err != nil {
		return Upstream{}, err
	}
	id, _ := res.LastInsertId()
	up.ID = id
	return up, nil
}

func (s *Store) UpdateUpstream(ctx context.Context, up Upstream) error {
	_, err := s.db.ExecContext(ctx, `UPDATE upstreams SET name = ?, url = ?, proxy = ?, kind = ?, enabled = ?, priority = ?, updated_at = ? WHERE id = ?`,
		up.Name, up.URL, up.Proxy, strings.ToLower(up.Kind), boolInt(up.Enabled), up.Priority, nowText(), up.ID)
	return err
}

func (s *Store) SetUpstreamEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE upstreams SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), nowText(), id)
	return err
}

func (s *Store) DeleteUpstream(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM upstreams WHERE id = ?`, id)
	return err
}

func (s *Store) ListAPTMirrors(ctx context.Context, enabledOnly bool) ([]APTMirror, error) {
	query := `SELECT id, name, public_prefix, upstream_url, proxy, enabled, created_at, updated_at FROM apt_mirrors`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY length(public_prefix) DESC, public_prefix, id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APTMirror
	for rows.Next() {
		var item APTMirror
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.PublicPrefix, &item.UpstreamURL, &item.Proxy, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetAPTMirror(ctx context.Context, id int64) (APTMirror, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, public_prefix, upstream_url, proxy, enabled, created_at, updated_at FROM apt_mirrors WHERE id = ?`, id)
	if err != nil {
		return APTMirror{}, err
	}
	defer rows.Close()
	items, err := scanAPTMirrors(rows)
	if err != nil {
		return APTMirror{}, err
	}
	if len(items) == 0 {
		return APTMirror{}, sql.ErrNoRows
	}
	return items[0], nil
}

func (s *Store) CreateAPTMirror(ctx context.Context, mirror APTMirror) (APTMirror, error) {
	now := nowText()
	res, err := s.db.ExecContext(ctx, `INSERT INTO apt_mirrors(name, public_prefix, upstream_url, proxy, enabled, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		mirror.Name, mirror.PublicPrefix, mirror.UpstreamURL, mirror.Proxy, boolInt(mirror.Enabled), now, now)
	if err != nil {
		return APTMirror{}, err
	}
	id, _ := res.LastInsertId()
	mirror.ID = id
	mirror.CreatedAt = now
	mirror.UpdatedAt = now
	return mirror, nil
}

func (s *Store) UpdateAPTMirror(ctx context.Context, mirror APTMirror) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apt_mirrors SET name = ?, public_prefix = ?, upstream_url = ?, proxy = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		mirror.Name, mirror.PublicPrefix, mirror.UpstreamURL, mirror.Proxy, boolInt(mirror.Enabled), nowText(), mirror.ID)
	return err
}

func (s *Store) SetAPTMirrorEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apt_mirrors SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), nowText(), id)
	return err
}

func (s *Store) DeleteAPTMirror(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM apt_mirrors WHERE id = ?`, id)
	return err
}

func (s *Store) ListProxyHostRules(ctx context.Context, enabledOnly bool) ([]ProxyHostRule, error) {
	query := `SELECT id, host, enabled, description, created_at, updated_at FROM proxy_host_rules`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY host, id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProxyHostRule
	for rows.Next() {
		var item ProxyHostRule
		var enabled int
		if err := rows.Scan(&item.ID, &item.Host, &enabled, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateProxyHostRule(ctx context.Context, rule ProxyHostRule) (ProxyHostRule, error) {
	rule.Host = normalizeHostRule(rule.Host)
	now := nowText()
	res, err := s.db.ExecContext(ctx, `INSERT INTO proxy_host_rules(host, enabled, description, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		rule.Host, boolInt(rule.Enabled), rule.Description, now, now)
	if err != nil {
		return ProxyHostRule{}, err
	}
	id, _ := res.LastInsertId()
	rule.ID = id
	rule.CreatedAt = now
	rule.UpdatedAt = now
	return rule, nil
}

func (s *Store) UpdateProxyHostRule(ctx context.Context, rule ProxyHostRule) error {
	rule.Host = normalizeHostRule(rule.Host)
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_host_rules SET host = ?, enabled = ?, description = ?, updated_at = ? WHERE id = ?`,
		rule.Host, boolInt(rule.Enabled), rule.Description, nowText(), rule.ID)
	return err
}

func (s *Store) SetProxyHostRuleEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_host_rules SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), nowText(), id)
	return err
}

func (s *Store) DeleteProxyHostRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proxy_host_rules WHERE id = ?`, id)
	return err
}

func (s *Store) ReplaceProxyHostRules(ctx context.Context, hosts []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM proxy_host_rules`); err != nil {
		return err
	}
	normalized := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = normalizeHostRule(host)
		if host == "" {
			continue
		}
		normalized = append(normalized, host)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO proxy_host_rules(host, enabled, description, created_at, updated_at) VALUES(?, 1, ?, ?, ?)`,
			host, "imported from proxy.allowed_hosts", nowText(), nowText()); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value_json, value_type, restart_required, updated_at) VALUES('proxy.allowed_hosts', ?, 'string[]', 0, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			value_type = excluded.value_type,
			restart_required = excluded.restart_required,
			updated_at = excluded.updated_at`, string(raw), nowText()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SyncProxyAllowedHostsSetting(ctx context.Context) error {
	rules, err := s.ListProxyHostRules(ctx, true)
	if err != nil {
		return err
	}
	hosts := make([]string, 0, len(rules))
	for _, rule := range rules {
		hosts = append(hosts, rule.Host)
	}
	raw, err := json.Marshal(hosts)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key, value_json, value_type, restart_required, updated_at) VALUES('proxy.allowed_hosts', ?, 'string[]', 0, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			value_type = excluded.value_type,
			restart_required = excluded.restart_required,
			updated_at = excluded.updated_at`, string(raw), nowText())
	return err
}

func (s *Store) UpsertCacheObject(ctx context.Context, obj CacheObject) error {
	now := nowText()
	if obj.FirstCachedAt == "" {
		obj.FirstCachedAt = now
	}
	if obj.CacheStatus == "" {
		obj.CacheStatus = "ok"
	}
	if obj.ValidationStatus == "" {
		obj.ValidationStatus = "unknown"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO cache_objects(protocol, class, host, request_path, cache_path, size_bytes, content_type, cache_status, validation_status, last_error, first_cached_at, last_accessed_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cache_path) DO UPDATE SET
			protocol = excluded.protocol,
			class = excluded.class,
			host = excluded.host,
			request_path = excluded.request_path,
			size_bytes = excluded.size_bytes,
			content_type = excluded.content_type,
			cache_status = excluded.cache_status,
			validation_status = excluded.validation_status,
			last_error = excluded.last_error,
			last_accessed_at = excluded.last_accessed_at,
			updated_at = excluded.updated_at`,
		obj.Protocol, obj.Class, obj.Host, obj.RequestPath, obj.CachePath, obj.SizeBytes, obj.ContentType, obj.CacheStatus, obj.ValidationStatus, obj.LastError, obj.FirstCachedAt, obj.LastAccessedAt, now)
	return err
}

func (s *Store) MarkCacheAccess(ctx context.Context, cachePath string, size int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cache_objects SET size_bytes = ?, last_accessed_at = ?, updated_at = ? WHERE cache_path = ?`,
		size, nowText(), nowText(), cachePath)
	return err
}

func (s *Store) ListCacheObjects(ctx context.Context, filter CacheObjectFilter) ([]CacheObject, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if filter.Protocol != "" {
		where = append(where, "protocol = ?")
		args = append(args, filter.Protocol)
	}
	if filter.Class != "" {
		where = append(where, "class = ?")
		args = append(args, filter.Class)
	}
	if filter.Host != "" {
		where = append(where, "host = ?")
		args = append(args, filter.Host)
	}
	if filter.Status != "" {
		where = append(where, "cache_status = ?")
		args = append(args, filter.Status)
	}
	if filter.Query != "" {
		where = append(where, "(request_path LIKE ? OR cache_path LIKE ?)")
		q := "%" + filter.Query + "%"
		args = append(args, q, q)
	}
	if filter.MinSize > 0 {
		where = append(where, "size_bytes >= ?")
		args = append(args, filter.MinSize)
	}
	if filter.MaxSize > 0 {
		where = append(where, "size_bytes <= ?")
		args = append(args, filter.MaxSize)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cache_objects WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	pageSize := filter.PageSize
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, protocol, class, host, request_path, cache_path, size_bytes, COALESCE(content_type, ''), cache_status, validation_status, COALESCE(last_error, ''), first_cached_at, COALESCE(last_accessed_at, ''), updated_at FROM cache_objects WHERE `+whereSQL+` ORDER BY updated_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, err := scanCacheObjects(rows)
	return out, total, err
}

func (s *Store) GetCacheObject(ctx context.Context, id int64) (CacheObject, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, protocol, class, host, request_path, cache_path, size_bytes, COALESCE(content_type, ''), cache_status, validation_status, COALESCE(last_error, ''), first_cached_at, COALESCE(last_accessed_at, ''), updated_at FROM cache_objects WHERE id = ?`, id)
	if err != nil {
		return CacheObject{}, err
	}
	defer rows.Close()
	items, err := scanCacheObjects(rows)
	if err != nil {
		return CacheObject{}, err
	}
	if len(items) == 0 {
		return CacheObject{}, sql.ErrNoRows
	}
	return items[0], nil
}

func (s *Store) DeleteCacheObjectRecord(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cache_objects WHERE id = ?`, id)
	return err
}

func (s *Store) AddRequestLog(ctx context.Context, log RequestLog) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO request_logs(ts, method, protocol, host, path, status_code, cache_status, upstream_name, duration_ms, bytes_sent, error) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.TS, log.Method, log.Protocol, log.Host, log.Path, log.StatusCode, log.CacheStatus, log.UpstreamName, log.DurationMS, log.BytesSent, log.Error)
	return err
}

func (s *Store) ListRequestLogs(ctx context.Context, limit int) ([]RequestLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, ts, method, protocol, COALESCE(host, ''), path, status_code, COALESCE(cache_status, ''), COALESCE(upstream_name, ''), duration_ms, bytes_sent, COALESCE(error, '') FROM request_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestLog
	for rows.Next() {
		var item RequestLog
		if err := rows.Scan(&item.ID, &item.TS, &item.Method, &item.Protocol, &item.Host, &item.Path, &item.StatusCode, &item.CacheStatus, &item.UpstreamName, &item.DurationMS, &item.BytesSent, &item.Error); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanCacheObjects(rows *sql.Rows) ([]CacheObject, error) {
	var out []CacheObject
	for rows.Next() {
		var item CacheObject
		if err := rows.Scan(&item.ID, &item.Protocol, &item.Class, &item.Host, &item.RequestPath, &item.CachePath, &item.SizeBytes, &item.ContentType, &item.CacheStatus, &item.ValidationStatus, &item.LastError, &item.FirstCachedAt, &item.LastAccessedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanAPTMirrors(rows *sql.Rows) ([]APTMirror, error) {
	var out []APTMirror
	for rows.Next() {
		var item APTMirror
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.PublicPrefix, &item.UpstreamURL, &item.Proxy, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ensureRuntimeSettingDefaults(ctx context.Context, cfg *config.Config) error {
	for _, def := range settingDefs {
		value, err := def.marshal(cfg)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key, value_json, value_type, restart_required, updated_at) VALUES(?, ?, ?, ?, ?)`,
			def.key, string(value), def.valueType, boolInt(def.restartRequired), nowText()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureProxyHostRulesFromAllowedHosts(ctx context.Context) error {
	var ruleCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proxy_host_rules`).Scan(&ruleCount); err != nil {
		return err
	}
	if ruleCount > 0 {
		return nil
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = 'proxy.allowed_hosts'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var hosts []string
	if err := json.Unmarshal([]byte(raw), &hosts); err != nil {
		return err
	}
	for _, host := range hosts {
		host = normalizeHostRule(host)
		if host == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO proxy_host_rules(host, enabled, description, created_at, updated_at) VALUES(?, 1, ?, ?, ?)`,
			host, "imported from proxy.allowed_hosts", nowText(), nowText()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table, column, alterSQL string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, alterSQL)
	return err
}

func cloneConfig(cfg *config.Config) *config.Config {
	next := *cfg
	next.Upstreams = append([]config.UpstreamConfig(nil), cfg.Upstreams...)
	next.Proxy.AllowedHosts = append([]string(nil), cfg.Proxy.AllowedHosts...)
	return &next
}

func normalizeHostRule(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")
	if slash := strings.IndexByte(host, '/'); slash >= 0 {
		host = host[:slash]
	}
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	host = strings.Trim(host, "[]")
	return host
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
