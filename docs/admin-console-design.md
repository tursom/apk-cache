# 管理页面设计文档

## 1. 背景

当前 APK Cache 已重写为轻量 Go 服务，核心能力包括 APK/APT 缓存、完整性校验、通用代理、`/_health` 和 `/metrics`。下一阶段需要恢复一个完整的管理页面，并把大部分运行配置从 TOML/env 迁移到数据库中。

用户确认的范围：

- 需要登录。
- 单管理员即可。
- 当前不需要操作审计日志。
- 仪表盘、缓存管理、APT/APK 管理、上游管理、代理管理、配置管理、日志排障等功能基本都需要。
- 管理页面部署在同一个 Go 服务内。
- 允许新增管理 API。
- 需要把现有大部分配置转到数据库内。

## 2. 目标

### 2.1 产品目标

- 提供一个内置 Web 管理台，入口为 `/admin/`。
- 管理台和缓存代理运行在同一个 Go 进程中，无需单独部署前端服务。
- 用单管理员账号保护所有管理页面和管理 API。
- 通过页面完成常见运维动作：观察、搜索、清理、配置、排障。
- 将运行配置持久化到 SQLite，TOML/env 只保留启动引导职责。

### 2.2 工程目标

- 保持当前缓存代理链路稳定，不为了管理台重写核心请求路径。
- 管理 API 使用清晰的 `/api/admin/v1` 前缀，避免和包代理请求冲突。
- 数据库迁移可重复执行，支持从旧 TOML/env 首次导入。
- 配置更新必须先校验，再写 DB，再应用到运行时。
- 对不能安全热更新的配置明确标记为 `restart_required`。
- 集成测试覆盖登录、配置读写、缓存清理、真实数据链路不回退。

## 3. 非目标

- 不做多用户、多角色、多租户。
- 不做完整操作审计日志。
- 不做 WebSocket 实时推送，首版用轮询即可。
- 不在首版支持远程数据库；默认只支持本地 SQLite。
- 不把包文件本体写入数据库；缓存文件仍保留在磁盘目录。
- 不解密 HTTPS APT `CONNECT` 隧道，管理台只能展示隧道状态和配置。

## 4. 总体架构

```text
┌──────────────────────────────────────────────────────────────┐
│ Go process: apk-cache                                        │
│                                                              │
│  ┌──────────────┐        ┌────────────────────────────────┐  │
│  │ /admin/*     │        │ /api/admin/v1/*                │  │
│  │ embedded SPA │───────▶│ admin JSON API + auth middleware│  │
│  └──────────────┘        └────────────────────────────────┘  │
│                                      │                       │
│                                      ▼                       │
│  ┌──────────────┐        ┌────────────────────────────────┐  │
│  │ proxy routes │        │ runtime services               │  │
│  │ APK/APT/HTTP │◀──────▶│ cache/app/upstream/index/logs  │  │
│  └──────────────┘        └────────────────────────────────┘  │
│                                      │                       │
│                                      ▼                       │
│                           ┌────────────────────────────────┐ │
│                           │ SQLite + Pebble under data_root│ │
│                           │ settings/logs + hash store     │ │
│                           └────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

建议新增包：

| 包 | 职责 |
| --- | --- |
| `internal/store` | SQLite 连接、迁移、事务、基础 repository |
| `internal/hashstore` | Pebble hash store、二进制 key 编码、expected/actual hash cache |
| `internal/admin` | 管理页面静态资源、响应模型 |
| `internal/admin/web` | React + TypeScript 管理台源码，Vite 构建到 `internal/admin/static` 后由 Go embed |
| `internal/runtime` | DB 配置快照、运行时热更新、重启需求判断 |
| `internal/logs` | 最近请求日志环形缓冲和可选 DB 持久化 |

前端使用 React + TypeScript + Vite 实现。源码放在 `internal/admin/web`，生产构建输出到 `internal/admin/static`，Go 服务通过 `embed.FS` 提供 `/admin/` 和 `/admin/assets/*`。管理台仍部署在同一个 Go 进程内，不需要单独的前端服务。

## 5. 启动与配置来源

### 5.1 配置来源分层

当前所有配置来自 TOML/env。迁移后改为三层：

1. **内置默认值**：`config.Default()`。
2. **启动引导配置**：TOML/env，只用于找到监听地址、数据目录、数据库和首次管理员初始化。
3. **数据库运行配置**：大部分业务配置的最终来源。

### 5.2 仍保留在 TOML/env 的启动配置

这些配置在进程启动前必须已知，不建议首版放进 DB：

| 配置 | 原因 |
| --- | --- |
| `server.listen` | HTTP server 启动前必须确定；后续页面可展示并标记修改需重启 |
| `cache.data_root` | SQLite 默认放在 data root 下，打开 DB 前必须确定 |
| `admin.bootstrap_token` / `ADMIN_BOOTSTRAP_TOKEN` | 首次创建管理员账号时使用 |
| `admin.session_secret` / `ADMIN_SESSION_SECRET` | Cookie 签名和 CSRF 派生密钥，建议环境变量注入 |
| `database.path` / `DATABASE_PATH` | 可选；默认 `${data_root}/apk-cache.db` |

### 5.3 迁入数据库的运行配置

| 配置组 | 字段 | 热更新策略 |
| --- | --- | --- |
| cache | `root`, `index_ttl`, `package_ttl` | TTL 可热更新；`root` 首版标记需重启 |
| cache.memory | `enabled`, `max_size`, `max_item_size`, `ttl`, `max_items` | 可重建内存缓存后热更新 |
| transport | `timeout`, `idle_conn_timeout`, `max_idle_conns` | 新建 HTTP client factory 后热更新，旧连接自然释放 |
| apk | `enabled`, `verify_hash`, `verify_signature`, `keys_dir` | 校验开关可热更新；`keys_dir` 需要重载 verifier |
| apt | `enabled`, `verify_hash`, `load_index_async` | 可热更新 |
| proxy | `enabled`, `allow_connect`, `cache_non_package_requests`, `upstream_proxy`, `allowed_hosts` | 可热更新 |
| upstreams | `name`, `url`, `proxy`, `kind`, `enabled`, `priority` | 重建 upstream manager 后热更新 |

### 5.4 首次启动迁移

启动流程：

1. 读取默认配置。
2. 读取 TOML/env 引导配置。
3. 打开 SQLite。
4. 打开 Pebble hash store。
5. 执行 migrations。
6. 如果 DB 中没有运行配置：
   - 将当前 TOML/env 合并结果写入 DB。
   - 写入默认 Alpine upstream。
7. 如果 DB 中已有运行配置：
   - DB 配置为准。
   - TOML/env 中属于运行配置的字段只作为兼容输入，不覆盖 DB。
8. 如果 Pebble hash store 为空或版本不匹配：
   - 从磁盘缓存索引文件重建 hash records。
9. 校验 DB 配置。
10. 创建 `App`。

这样可以做到一次迁移后，页面修改不会被旧 TOML/env 意外覆盖。

## 6. 数据库设计

SQLite driver 建议使用 `modernc.org/sqlite`，保持 `CGO_ENABLED=0` 的跨平台构建能力。

hash 相关值不进入 SQLite 热路径，单独使用 Pebble 存储。详细设计见 [Hash Store 设计文档](hash-store-design.md)。

### 6.1 migrations

新增表：

```sql
CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);
```

### 6.2 管理员与会话

```sql
CREATE TABLE admin_users (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  password_algo TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_login_at TEXT
);

CREATE TABLE admin_sessions (
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
);
```

密码哈希建议使用 Argon2id。Session token 使用 `crypto/rand` 生成，只保存 SHA256 hash。

### 6.3 设置表

首版建议使用 typed key-value，避免为每个配置组设计过早复杂 schema：

```sql
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  value_type TEXT NOT NULL,
  restart_required INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
```

示例 key：

```text
cache.root
cache.index_ttl
cache.package_ttl
cache.memory.enabled
transport.timeout
apk.verify_hash
apt.load_index_async
proxy.allowed_hosts
```

### 6.4 上游表

上游是列表型配置，单独建表：

```sql
CREATE TABLE upstreams (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  proxy TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'apk',
  enabled INTEGER NOT NULL DEFAULT 1,
  priority INTEGER NOT NULL DEFAULT 100,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_upstreams_kind_enabled_priority
  ON upstreams(kind, enabled, priority);
```

### 6.5 缓存元数据

缓存文件仍在磁盘，DB 只保存索引和状态，便于页面搜索和批量操作。

```sql
CREATE TABLE cache_objects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  protocol TEXT NOT NULL,      -- apk / apt / proxy
  class TEXT NOT NULL,         -- index / package / other
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
);

CREATE INDEX idx_cache_objects_protocol_host ON cache_objects(protocol, host);
CREATE INDEX idx_cache_objects_path ON cache_objects(request_path);
CREATE INDEX idx_cache_objects_updated ON cache_objects(updated_at);
```

写入策略：

- 首次缓存成功后 upsert。
- 缓存命中时异步更新 `last_accessed_at`。
- 校验失败、删除、重新下载时更新状态。
- 后台 reconciler 定期扫描 `cache.root`，修正缺失或已删除文件。

### 6.6 APK/APT 索引元数据

hash expected records、actual hash cache、APT by-hash 映射的真实来源是 Pebble。SQLite 只保存管理台分页、搜索和展示需要的摘要字段；页面详情需要完整 hash 时，通过 hash service 点查 Pebble。

```sql
CREATE TABLE apk_package_summaries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  index_cache_path TEXT NOT NULL,
  package_name TEXT NOT NULL,
  version TEXT NOT NULL,
  arch TEXT,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  checksum_algorithm TEXT NOT NULL,
  package_cache_path TEXT,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_apk_package_summaries_name ON apk_package_summaries(package_name);

CREATE TABLE apt_record_summaries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_index_cache_path TEXT NOT NULL,
  record_type TEXT NOT NULL,       -- release_file / package_file
  target_cache_path TEXT NOT NULL,
  filename TEXT NOT NULL,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  package_name TEXT,
  version TEXT,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_apt_record_summaries_package ON apt_record_summaries(package_name);
CREATE INDEX idx_apt_record_summaries_target ON apt_record_summaries(target_cache_path);
```

这些 summary 表不能作为校验依据；校验必须读取 Pebble 中的二进制 hash records。

### 6.7 请求日志

这不是操作审计日志，只用于排障。

```sql
CREATE TABLE request_logs (
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
);

CREATE INDEX idx_request_logs_ts ON request_logs(ts);
CREATE INDEX idx_request_logs_path ON request_logs(path);
CREATE INDEX idx_request_logs_status ON request_logs(status_code);
```

保留策略默认：

- 内存 ring buffer 保留最近 1000 条。
- DB 保留最近 7 天或最多 100000 条，可配置。
- 不记录请求 body，不记录认证 token。

## 7. 认证设计

### 7.1 首次管理员创建

推荐流程：

1. 如果 `admin_users` 为空，`/admin/setup` 页面可用。
2. 创建管理员必须提供 `ADMIN_BOOTSTRAP_TOKEN`。
3. 创建完成后立即删除 bootstrap 状态，后续只能登录。

如果没有设置 bootstrap token：

- 管理 API 返回 `setup_required=true` 和 `bootstrap_configured=false`。
- 页面提示需要设置环境变量并重启。

### 7.2 登录与会话

接口：

```text
POST /api/admin/v1/auth/login
POST /api/admin/v1/auth/logout
GET  /api/admin/v1/auth/me
POST /api/admin/v1/auth/change-password
```

安全策略：

- Cookie: `HttpOnly`, `SameSite=Lax`, TLS 下加 `Secure`。
- 管理 API 除登录/setup 外都要求 session。
- 非 GET 请求要求 `X-CSRF-Token`。
- 登录失败做短时内存限速。
- Session 默认有效期 24 小时，可在 DB 设置。

## 8. 管理 API 设计

统一响应：

```json
{
  "ok": true,
  "data": {},
  "error": null
}
```

错误响应：

```json
{
  "ok": false,
  "data": null,
  "error": {
    "code": "validation_failed",
    "message": "cache.index_ttl must be a duration"
  }
}
```

### 8.1 仪表盘

```text
GET /api/admin/v1/dashboard/summary
GET /api/admin/v1/dashboard/series?range=1h&step=1m
```

返回内容：

- 运行状态：healthy/degraded。
- 缓存命中、miss、memory hit。
- 上游请求、failover、错误。
- 下载流量、响应流量。
- 磁盘缓存大小、文件数量。
- 内存缓存大小、条目数。
- 校验失败、APK hash/signature 失败。
- CONNECT 活跃数量。

### 8.2 缓存管理

```text
GET    /api/admin/v1/cache/objects
GET    /api/admin/v1/cache/objects/{id}
DELETE /api/admin/v1/cache/objects/{id}
POST   /api/admin/v1/cache/delete
POST   /api/admin/v1/cache/prewarm
POST   /api/admin/v1/cache/reconcile
POST   /api/admin/v1/cache/memory/clear
```

列表过滤：

- `protocol=apk|apt|proxy`
- `class=index|package|other`
- `host=...`
- `q=...`
- `status=ok|corrupted|missing`
- `min_size` / `max_size`
- `page` / `page_size`

批量删除必须支持 `dry_run=true`，页面先展示影响范围，再确认执行。

### 8.3 APK 管理

```text
GET  /api/admin/v1/apk/indexes
GET  /api/admin/v1/apk/packages
POST /api/admin/v1/apk/indexes/reload
GET  /api/admin/v1/apk/keys
POST /api/admin/v1/apk/keys/reload
```

页面能力：

- 查看 APKINDEX 文件列表。
- 搜索包名/版本。
- 查看 checksum 算法、hash、大小、缓存状态。
- 查看签名公钥状态。
- 手动重载索引和公钥。

### 8.4 APT 管理

```text
GET  /api/admin/v1/apt/indexes
GET  /api/admin/v1/apt/records
POST /api/admin/v1/apt/indexes/reload
POST /api/admin/v1/apt/validate
```

页面能力：

- 查看 Release/InRelease、Packages/Sources、by-hash 记录。
- 查看索引文件到 `.deb` 的映射关系。
- 搜索包名、filename、sha256。
- 手动重载 APT 索引。
- 对指定缓存文件触发校验。

### 8.5 上游管理

```text
GET    /api/admin/v1/upstreams
POST   /api/admin/v1/upstreams
PUT    /api/admin/v1/upstreams/{id}
DELETE /api/admin/v1/upstreams/{id}
POST   /api/admin/v1/upstreams/{id}/test
POST   /api/admin/v1/upstreams/{id}/enable
POST   /api/admin/v1/upstreams/{id}/disable
```

能力：

- 管理 APK upstream。
- 配置 upstream proxy。
- 展示健康状态、failover 次数、最近错误。
- 手动测试连通性。
- 禁用上游后运行时立即不再选择该 upstream。

### 8.6 代理管理

```text
GET /api/admin/v1/proxy/status
PUT /api/admin/v1/proxy/config
```

能力：

- 开关通用代理。
- 开关 CONNECT。
- 配置 `proxy.upstream_proxy`。
- 配置 allowed hosts。
- 查看当前活跃 CONNECT 数、上限、拒绝次数。

### 8.7 配置管理

```text
GET /api/admin/v1/config
PUT /api/admin/v1/config
GET /api/admin/v1/config/schema
POST /api/admin/v1/config/validate
POST /api/admin/v1/config/reload
```

响应必须标识每个字段：

- 当前值。
- 来源：default / imported / database / environment-bootstrap。
- 是否可编辑。
- 是否热更新。
- 修改后是否需要重启。

### 8.8 日志与诊断

```text
GET  /api/admin/v1/logs/requests
GET  /api/admin/v1/logs/errors
POST /api/admin/v1/diagnostics/package
GET  /api/admin/v1/system/info
```

诊断包内容：

- 当前配置快照，敏感字段脱敏。
- 最近请求日志。
- 最近错误。
- 上游状态。
- 缓存目录摘要。
- Go runtime 信息。

## 9. 页面设计

首版页面结构：

```text
/admin/login
/admin/setup
/admin/dashboard
/admin/cache
/admin/apk
/admin/apt
/admin/upstreams
/admin/proxy
/admin/config
/admin/logs
/admin/system
```

### 9.1 导航

左侧固定导航：

- 仪表盘
- 缓存
- APK
- APT
- 上游
- 代理
- 配置
- 日志
- 系统

右上角：

- 当前管理员。
- 修改密码。
- 退出登录。

### 9.2 仪表盘

模块：

- 服务状态。
- 命中率、请求量、流量。
- 磁盘缓存和内存缓存。
- 上游健康状态。
- 校验失败。
- CONNECT 活跃数量。
- 最近错误和最近请求。

### 9.3 缓存页面

表格字段：

- 协议。
- host。
- 路径。
- 类型。
- 大小。
- 缓存状态。
- 校验状态。
- 最后访问。
- 操作。

操作：

- 查看详情。
- 删除。
- 批量删除。
- 预热。
- 重扫缓存目录。
- 清空内存缓存。

### 9.4 APK 页面

Tabs：

- APKINDEX。
- 包列表。
- 公钥。

关键能力：

- 搜索包。
- 查看包是否已缓存。
- 查看 hash/signature 状态。
- 重载索引。
- 重载公钥。

### 9.5 APT 页面

Tabs：

- Release/InRelease。
- Packages/Sources。
- 包记录。
- by-hash。

关键能力：

- 查看索引依赖关系。
- 搜索 `.deb` filename / package / sha256。
- 校验指定缓存。
- 重载索引。

### 9.6 配置页面

按配置组展示：

- Cache。
- Memory Cache。
- Transport。
- APK。
- APT。
- Proxy。
- Upstreams。

保存流程：

1. 页面提交变更。
2. 后端校验。
3. 返回影响摘要。
4. 用户确认。
5. 写 DB。
6. 尝试热更新。
7. 如果有需重启字段，页面展示“已保存，重启后生效”。

## 10. 运行时热更新设计

新增 `RuntimeConfigManager`：

```go
type RuntimeConfigManager struct {
    store Store
    current atomic.Value // *config.Config
}
```

`App` 不再长期依赖可变的 `*config.Config` 指针，而是：

- 请求路径中读取当前配置快照。
- 需要重建的组件通过 `ApplyConfig` 更新。

首版可按风险分阶段：

1. 先支持 DB 持久化和页面修改。
2. 修改后要求重启生效。
3. 再逐步把低风险字段改为热更新。

推荐首版热更新：

- APK/APT/proxy 开关。
- 校验开关。
- TTL。
- allowed hosts。
- upstream 列表。

推荐首版需重启：

- `server.listen`。
- `cache.data_root`。
- `database.path`。
- `cache.root`。

## 11. 兼容与迁移策略

### 11.1 配置兼容

- 保留 `config.example.toml`，但缩减说明为 bootstrap 配置。
- 旧配置文件仍能启动。
- 首次启动时把旧配置导入 DB。
- 后续页面修改以 DB 为准。

### 11.2 Docker 兼容

保留当前环境变量：

- `LISTEN`
- `CACHE_ROOT`
- `DATA_ROOT`
- `APK_UPSTREAM`
- `UPSTREAM_PROXY`
- `APK_VERIFY_HASH`
- `APT_VERIFY_HASH`

迁移行为：

- 如果 DB 不存在，这些变量参与首次导入。
- 如果 DB 已存在，这些变量不覆盖 DB 运行配置。
- 文档必须明确：要强制重置 DB 配置，需要删除 DB 或使用导入命令。

新增环境变量：

```text
DATABASE_PATH
HASH_STORE_PATH
ADMIN_BOOTSTRAP_TOKEN
ADMIN_SESSION_SECRET
```

## 12. 安全边界

- 管理 API 全部加认证。
- 管理 API 不和代理路径复用。
- 删除缓存、修改配置、诊断包下载都要求 CSRF token。
- 配置中的 proxy URL 如果包含密码，页面默认脱敏。
- 诊断包也必须脱敏。
- 登录失败限速。
- 批量删除必须先 dry run。
- `cache_path` 相关 API 不接受任意文件路径，只接受 cache object id 或受控 key。

## 13. 测试计划

### 13.1 单元测试

- SQLite migration 可重复执行。
- Pebble hash store 二进制 key 编码保持可变长字段在末尾。
- Pebble expected/actual/source/by-hash records 可写入、查找和删除。
- settings encode/decode。
- TOML/env 首次导入。
- 配置校验失败不写 DB。
- session token hash 和过期逻辑。
- CSRF 校验。

### 13.2 API 测试

- 未登录访问管理 API 返回 401。
- setup 创建管理员。
- 登录、获取当前用户、退出。
- 修改密码。
- 读取/修改配置。
- 上游 CRUD。
- 缓存列表、删除、dry run。

### 13.3 集成测试

- `go test ./...` 必须继续包含真实 APK/APT fixture 测试。
- 管理台修改 upstream 后，APK 请求使用新 upstream。
- 管理台关闭 APT 后，APT 请求不再进入 APT 缓存分支。
- 缓存删除后，下一次请求回源并重新写缓存。
- DB 存在时，重启后配置仍生效。

### 13.4 Docker 冒烟

- 容器启动后 `/_health` 正常。
- `/admin/` 返回页面。
- 使用 bootstrap token 完成首次管理员创建。
- 登录后 dashboard API 正常。

## 14. 实施阶段

### Phase 1: DB 与认证骨架

- 新增 SQLite store 和 migrations。
- 新增 Pebble hash store 目录和基础健康检查。
- 新增 bootstrap 配置。
- 实现管理员 setup/login/logout/me/change-password。
- `/admin/` 提供最小登录页。
- CI 跑管理 API 基础测试。

### Phase 2: 配置入库

- 将当前 `config.Config` 映射到 DB。
- 首次启动导入 TOML/env。
- `/api/admin/v1/config` 读写。
- 页面展示配置分组。
- 修改后支持重启提示。

### Phase 3: 仪表盘与运行状态

- dashboard summary API。
- 上游状态 API。
- CONNECT 活跃数量指标。
- 管理台仪表盘页面。

### Phase 4: 缓存管理

- `cache_objects` 元数据写入。
- 后台 cache reconciler。
- 缓存列表、搜索、删除、批量 dry run。
- 清空内存缓存。

### Phase 5: APK/APT Hash Store 与管理

- APKINDEX / APK package hash records 写入 Pebble。
- APT Release / Packages / package hash records 写入 Pebble。
- actual file hash cache 写入 Pebble，避免重复计算。
- 索引重载和指定缓存校验 API。
- APK/APT 页面。

### Phase 6: 日志与诊断

- 请求日志 middleware。
- logs 页面。
- 诊断包下载。

## 15. 待确认细节

当前设计已按用户确认范围做默认选择。实现前仍建议确认：

- 首次管理员创建是否接受 `ADMIN_BOOTSTRAP_TOKEN` 方案。
- 是否允许引入 `modernc.org/sqlite` 作为纯 Go SQLite driver。
- 是否允许引入 `github.com/cockroachdb/pebble` 作为本地 hash KV store。
- 管理页面采用 React + TypeScript + Vite，构建产物仍内嵌到 Go 服务。
- `cache.root` 修改首版是否接受“保存后重启生效”。
