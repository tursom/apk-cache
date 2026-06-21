export type AdminResponse<T> = {
  ok: boolean;
  data: T;
  error?: { code: string; message: string } | null;
};

export type CurrentUser = {
  authenticated: boolean;
  username: string;
  csrf_token: string;
  is_default_credential: boolean;
  last_login_at?: string;
  default_username?: string;
};

export type Setting = {
  key: string;
  value: unknown;
  value_type: 'string' | 'bool' | 'int' | 'string[]';
  restart_required: boolean;
  updated_at: string;
  source: string;
  editable: boolean;
  hot_reload: boolean;
};

export type SettingSchema = {
  key: string;
  group: string;
  title: string;
  description: string;
  value_type: Setting['value_type'];
  control: string;
  editable: boolean;
  hot_reload: boolean;
  restart_required: boolean;
  sensitive: boolean;
};

export type Upstream = {
  id: number;
  name: string;
  url: string;
  proxy: string;
  kind: string;
  enabled: boolean;
  priority: number;
  created_at: string;
  updated_at: string;
};

export type CacheObject = {
  id: number;
  protocol: string;
  class: string;
  host: string;
  request_path: string;
  cache_path: string;
  size_bytes: number;
  content_type: string;
  cache_status: string;
  validation_status: string;
  last_error: string;
  first_cached_at: string;
  last_accessed_at: string;
  updated_at: string;
};

export type RequestLog = {
  id: number;
  ts: string;
  method: string;
  protocol: string;
  host: string;
  path: string;
  status_code: number;
  cache_status: string;
  upstream_name: string;
  duration_ms: number;
  bytes_sent: number;
  error: string;
};

export type DashboardSummary = {
  status: string;
  cache_objects: number;
  apk_upstreams: { healthy: number; total: number };
  memory_cache?: { size: number; max: number; items: number } | null;
  disk_cache?: { root: string; files: number; dirs: number; size_bytes: number; protocols: Record<string, { files: number; size_bytes: number }> };
  connect: { active: number; limit: number; rejected?: number };
  hash_store: Record<string, unknown>;
  database: { path: string };
  requests: Record<string, unknown>;
  recent_requests: RequestLog[];
  recent_errors: RequestLog[];
};

export type APKPackage = {
  index_cache_path: string;
  package_name: string;
  version: string;
  checksum_algorithm: string;
  size_bytes: number;
};

export type APTRecord = {
  source_index_cache_path: string;
  record_type: string;
  target_cache_path?: string;
  filename: string;
  package_name?: string;
  size_bytes: number;
  sha256: string;
};

export type APTMirror = {
  id: number;
  name: string;
  public_prefix: string;
  upstream_url: string;
  proxy: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
};

export type ProxyHostRule = {
  id: number;
  host: string;
  enabled: boolean;
  description: string;
  created_at: string;
  updated_at: string;
};

export type ToastState = {
  message: string;
  tone: 'ok' | 'error';
};
