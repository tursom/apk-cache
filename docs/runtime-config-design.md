# 运行配置管理设计文档

## 1. 背景

当前项目已经具备 SQLite 设置表、上游表和管理 API，但产品形态仍偏向“从 TOML/env 启动，再在管理台用 key/value 表格修改”。后续需要把大部分运行配置收敛到前端管理页面，配置文件和环境变量只承担最小启动引导职责。

本设计文档专门描述运行配置中心的产品需求、数据模型、API、前端页面和迁移策略。管理台整体设计见 [管理页面设计文档](admin-console-design.md)，hash 存储设计见 [Hash Store 设计文档](hash-store-design.md)。

## 2. 已确认产品需求

- 配置中心是运行配置的主要入口，大部分配置必须能在前端管理页面查看、校验、保存和重载。
- `cache.root` 允许在前端修改，但首版标记为“需重启”，且不自动迁移旧缓存文件。
- 不再使用 bootstrap token 创建管理员，也不再要求用户配置 session secret。
- 服务首次启动时创建一个默认单管理员账号，后台可以修改用户名和密码。
- APK 需要管理多个上游源。
- APT 继续支持 HTTP 代理模式。
- APT 还需要支持传统镜像站模式，让客户端可以把 sources.list 指向本服务的镜像路径。
- APT/通用代理需要能在前端管理代理目标网站白名单。
- 管理台部署在同一个 Go 服务内，不增加独立前端服务。

## 3. 配置来源模型

### 3.1 来源优先级

启动后运行配置使用以下优先级：

1. 内置默认值。
2. 启动引导配置：TOML/env。
3. 数据库运行配置。

规则：

- DB 没有运行配置时，首次启动把当前 TOML/env 合并后的值导入 DB。
- DB 已有运行配置时，以 DB 为准。
- DB 已有运行配置后，TOML/env 中的运行配置字段只作为历史兼容输入，不再覆盖 DB。
- 前端保存配置时先校验，再写 DB，再按热更新能力应用到运行时。
- 需重启配置保存后只写 DB 和返回 `restart_required`，运行中仍使用旧值，直到进程重启。

### 3.2 仍保留在 TOML/env 的启动配置

这些配置在打开数据库或启动 HTTP server 前必须已知，首版不从 DB 读取：

| 配置 | 是否前端可改 | 说明 |
| --- | --- | --- |
| `server.listen` | 可展示，可保存为下次启动值 | HTTP server 监听地址，修改需重启 |
| `cache.data_root` | 只展示 | 默认数据库和 Pebble 根目录依赖它，修改需要迁移数据目录，首版不在页面直接改 |
| `database.path` | 只展示 | 用于打开 SQLite，运行时无法切换 |
| `config path` | 只展示 | 进程启动参数，不属于运行配置 |

废弃项：

- `admin.bootstrap_token` / `ADMIN_BOOTSTRAP_TOKEN`
- `admin.session_secret` / `ADMIN_SESSION_SECRET`

### 3.3 迁入数据库的运行配置

| 配置组 | 字段 | 生效策略 |
| --- | --- | --- |
| cache | `root` | 可前端修改，需重启，不自动迁移旧缓存 |
| cache | `index_ttl`, `package_ttl` | 热更新 |
| cache.memory | `enabled`, `max_size`, `max_item_size`, `ttl`, `max_items` | 重建内存缓存后热更新 |
| transport | `timeout`, `idle_conn_timeout`, `max_idle_conns` | 重建 HTTP client factory 后热更新，旧连接自然释放 |
| apk | `enabled`, `verify_hash`, `verify_signature`, `keys_dir` | 开关热更新；`keys_dir` 重载 verifier 后生效 |
| apt | `enabled`, `verify_hash`, `load_index_async` | 热更新 |
| apt.mirrors | `name`, `public_prefix`, `upstream_url`, `proxy`, `enabled` | 重建 APT mirror router 后热更新 |
| proxy | `enabled`, `allow_connect`, `cache_non_package_requests`, `upstream_proxy` | 热更新 |
| proxy.host_rules | `host`, `enabled`, `description` | 热更新 |
| hash_store | `trust_file_stat`, `actual_revalidate_interval` | 热更新 |
| hash_store | `path`, `rebuild_on_corruption` | 需重启 |

## 4. 默认管理员设计

### 4.1 默认账号

DB 中没有管理员时，启动过程自动创建单管理员：

| 字段 | 默认值 |
| --- | --- |
| 用户名 | `admin` |
| 密码 | `admin123456` |

密码使用 Argon2id 写入 DB，不保存明文。首次登录后页面必须提示“正在使用默认管理员密码”，并提供修改入口。

### 4.2 后台账号管理

管理台提供“账号安全”区域：

- 查看当前管理员用户名。
- 修改用户名。
- 修改密码。
- 显示是否仍在使用默认凭据。
- 注销当前会话。
- 可选：注销其他会话。

不做多用户、多角色，也不做操作审计日志。

### 4.3 会话安全

不再依赖 session secret。会话使用随机 token：

- 登录成功生成 `session_token` 和 `csrf_token`。
- Cookie 只保存随机 token。
- DB 只保存 token hash。
- 每次请求用 Cookie token hash 查 DB session。
- 非 GET 请求要求 `X-CSRF-Token`。
- Cookie 使用 `HttpOnly`、`SameSite=Lax`，TLS 下加 `Secure`。
- 登录失败按来源 IP 做短时限速。

## 5. 配置中心前端设计

配置页面不再只展示裸 key/value 表格，而是按业务分组展示表单。

### 5.1 页面结构

```text
/admin/config
  ├─ 基础运行
  ├─ 缓存目录与 TTL
  ├─ 内存缓存
  ├─ 网络传输
  ├─ APK
  ├─ APT
  ├─ 代理
  └─ Hash Store
```

`/admin/upstreams` 保持独立，用于 APK 上游源管理。

`/admin/apt` 增加 APT 镜像站管理 tab。

`/admin/proxy` 增加代理网站白名单 tab。

### 5.2 控件要求

| 类型 | 控件 |
| --- | --- |
| bool | toggle |
| duration | 文本输入，保存前校验 Go duration |
| size | 文本输入，保存前校验 `MB` / `GB` 等单位 |
| int | number input |
| path | 文本输入，显示重启和迁移提示 |
| URL | 文本输入，保存前校验 scheme 和 host |
| host list | 表格 CRUD，不使用多行文本作为主交互 |
| enum | select 或 segmented control |

每个配置项必须展示：

- 名称。
- 当前值。
- 来源：`default` / `imported` / `database` / `startup`。
- 是否热更新。
- 是否需重启。
- 说明。
- 校验错误。

## 6. APK 上游管理

APK upstream 继续使用独立表管理。

字段：

| 字段 | 说明 |
| --- | --- |
| `name` | 展示名称 |
| `url` | APK 上游根 URL |
| `proxy` | 此 upstream 专用出站代理 |
| `kind` | 固定为 `apk`，保留扩展字段 |
| `enabled` | 是否启用 |
| `priority` | 选择顺序 |

页面能力：

- 新增、编辑、删除。
- 启用、禁用。
- 连通性测试。
- 展示健康状态、最近错误、failover 次数。
- 保存后热更新 upstream manager。

## 7. APT 支持模式

APT 需要支持两种入口模式。

### 7.1 代理模式

客户端配置：

```conf
Acquire::HTTP::Proxy "http://cache.example:3142";
Acquire::HTTPS::Proxy "http://cache.example:3142";
```

行为：

- HTTP APT 请求是 absolute URL，本服务按目标 host/path 缓存。
- HTTPS APT 请求通过 `CONNECT` 透传，不解密，不缓存 TLS 内部内容。
- `proxy.host_rules` 非空时，只允许白名单中的目标 host。
- `proxy.upstream_proxy` 作为出站代理。

### 7.2 传统镜像站模式

客户端可以把 sources.list 指向本服务暴露的镜像路径，例如：

```text
deb http://cache.example:3142/debian bookworm main
deb http://cache.example:3142/ubuntu jammy main universe
```

服务通过 APT mirror 配置把本地路径映射到真实镜像站：

| 字段 | 示例 | 说明 |
| --- | --- | --- |
| `name` | `Debian Official` | 展示名称 |
| `public_prefix` | `/debian` | 暴露给客户端的本地路径前缀 |
| `upstream_url` | `https://deb.debian.org/debian` | 真实镜像站根 URL |
| `proxy` | `socks5://127.0.0.1:1080` | 可选，此镜像站专用出站代理 |
| `enabled` | `true` | 是否启用 |

映射规则：

```text
GET /debian/dists/bookworm/InRelease
 -> https://deb.debian.org/debian/dists/bookworm/InRelease

GET /debian/pool/main/c/curl/curl_*.deb
 -> https://deb.debian.org/debian/pool/main/c/curl/curl_*.deb
```

缓存路径仍按真实 upstream host 和请求路径归档，避免不同镜像站路径互相覆盖。

约束：

- `public_prefix` 必须以 `/` 开头。
- 不允许使用 `/admin`、`/api`、`/_health`、`/metrics`、`/alpine` 等保留前缀。
- 同一个 `public_prefix` 只能绑定一个启用中的 APT mirror。
- 镜像站模式只处理 APT index/package 路径，其他路径返回 404 或按配置进入普通代理。
- APT mirror 保存后热更新路由表。

### 7.3 APT 页面能力

APT 页面增加 tabs：

- 索引。
- 记录。
- 镜像站。
- 校验。

镜像站 tab：

- 新增、编辑、删除镜像站。
- 启用、禁用。
- 测试 `InRelease` 或指定路径。
- 生成 `sources.list` 示例。
- 展示最近错误和缓存命中情况。

## 8. 代理网站白名单

当前 `proxy.allowed_hosts` 是字符串数组。产品层改为结构化“代理网站白名单”。

### 8.1 规则

字段：

| 字段 | 说明 |
| --- | --- |
| `host` | 目标 host，不包含 scheme；可带端口，比较时默认忽略端口 |
| `enabled` | 是否启用 |
| `description` | 备注 |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |

行为：

- 白名单为空时保持兼容：允许所有代理目标。
- 白名单非空时，只允许启用状态的 host。
- HTTP absolute URL 和 HTTPS `CONNECT` 都走同一套校验。
- APT 代理模式也受该白名单限制。
- APT 镜像站模式不依赖白名单，因为目标 upstream 已由 mirror 配置显式指定。

### 8.2 页面能力

- 新增、编辑、删除 host。
- 启用、禁用。
- 从最近请求日志中一键加入白名单。
- 保存后热更新 proxy 校验逻辑。

## 9. 数据库设计

### 9.1 settings

保留 typed key-value，用于标量配置：

```sql
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  value_type TEXT NOT NULL,
  restart_required INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
```

配置元数据不建议完全放进 DB，优先由 Go 代码中的 schema 定义：

- 分组。
- 标题。
- 描述。
- 控件类型。
- 默认值。
- 校验规则。
- 是否敏感。
- 是否热更新。

### 9.2 admin_users

```sql
CREATE TABLE admin_users (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  password_algo TEXT NOT NULL,
  is_default_credential INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_login_at TEXT
);
```

修改用户名或密码后，将 `is_default_credential` 置为 `0`。

### 9.3 apt_mirrors

```sql
CREATE TABLE apt_mirrors (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  public_prefix TEXT NOT NULL UNIQUE,
  upstream_url TEXT NOT NULL,
  proxy TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_apt_mirrors_enabled_prefix
  ON apt_mirrors(enabled, public_prefix);
```

### 9.4 proxy_host_rules

```sql
CREATE TABLE proxy_host_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  host TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_proxy_host_rules_enabled_host
  ON proxy_host_rules(enabled, host);
```

兼容策略：

- 迁移时把旧 `proxy.allowed_hosts` 导入 `proxy_host_rules`。
- 新代码读取 `proxy_host_rules` 生成运行时白名单。
- `proxy.allowed_hosts` 可以保留为兼容输出，但不作为前端主编辑入口。

## 10. 管理 API 设计

### 10.1 配置中心

```text
GET  /api/admin/v1/config
PUT  /api/admin/v1/config
GET  /api/admin/v1/config/schema
POST /api/admin/v1/config/validate
POST /api/admin/v1/config/reload
```

新增 schema 字段：

```json
{
  "key": "cache.root",
  "group": "cache",
  "title": "磁盘缓存目录",
  "description": "保存缓存文件的目录，修改后需重启。",
  "value_type": "string",
  "control": "path",
  "editable": true,
  "hot_reload": false,
  "restart_required": true,
  "sensitive": false
}
```

### 10.2 管理员账号

```text
GET  /api/admin/v1/account
PUT  /api/admin/v1/account/username
PUT  /api/admin/v1/account/password
POST /api/admin/v1/auth/logout
```

### 10.3 APT 镜像站

```text
GET    /api/admin/v1/apt/mirrors
POST   /api/admin/v1/apt/mirrors
PUT    /api/admin/v1/apt/mirrors/{id}
DELETE /api/admin/v1/apt/mirrors/{id}
POST   /api/admin/v1/apt/mirrors/{id}/enable
POST   /api/admin/v1/apt/mirrors/{id}/disable
POST   /api/admin/v1/apt/mirrors/{id}/test
GET    /api/admin/v1/apt/mirrors/{id}/sources-list
```

### 10.4 代理白名单

```text
GET    /api/admin/v1/proxy/host-rules
POST   /api/admin/v1/proxy/host-rules
PUT    /api/admin/v1/proxy/host-rules/{id}
DELETE /api/admin/v1/proxy/host-rules/{id}
POST   /api/admin/v1/proxy/host-rules/{id}/enable
POST   /api/admin/v1/proxy/host-rules/{id}/disable
```

## 11. 运行时应用策略

运行时维护一个不可变配置快照：

```text
DB settings + tables
  -> validate
  -> build RuntimeConfig
  -> atomic swap App runtime fields
```

热更新时需要重建：

- HTTP client factory。
- APK upstream manager。
- APT mirror router。
- APK verifier。
- 内存缓存。
- proxy host matcher。
- hash store 可热更新选项。

不能热更新的字段写入 DB 后只标记 pending restart：

- `server.listen`
- `cache.root`
- `cache.data_root`
- `database.path`
- `hash_store.path`
- `hash_store.rebuild_on_corruption`

系统页需要显示“存在待重启配置”和字段列表。

## 12. 迁移策略

### 12.1 从当前实现迁移

1. 增加 DB migrations：`apt_mirrors`、`proxy_host_rules`、`admin_users.is_default_credential`。
2. 启动时如果 `admin_users` 为空，创建默认管理员。
3. 废弃 `/admin/setup` 页面，登录页直接提示默认账号。
4. 如果旧 DB 已有管理员，保留现有账号，不创建默认账号。
5. 导入旧 `proxy.allowed_hosts` 到 `proxy_host_rules`。
6. 如果旧设置里存在 `admin.bootstrap_token` 或 `admin.session_secret`，忽略并在系统页提示“已废弃”。
7. 配置中心 schema 增加标题、说明、控件类型和敏感字段。

### 12.2 配置文件调整

`config.example.toml` 最终只保留启动配置示例和少量首次导入兼容项。README 需要明确：

- 默认管理员为 `admin` / `admin123456`。
- 首次登录后应立即修改密码。
- 管理台修改过配置后，DB 是运行配置来源。
- 对运行配置继续设置 env 不会覆盖已有 DB 值。

## 13. 验收标准

- 空 DB 启动后可直接用默认管理员登录。
- 后台可修改用户名和密码，修改后默认凭据提示消失。
- 配置中心按业务分组展示，不再依赖裸 key/value 作为主要交互。
- `cache.root` 可保存，返回需重启提示，运行中不切换目录。
- APK upstream 可 CRUD、测试并热更新。
- APT 代理模式仍可缓存 HTTP APT 请求。
- APT 镜像站模式可用 sources.list 指向本服务路径，并缓存 Release/Packages/deb。
- 代理白名单对 HTTP proxy 和 CONNECT 生效。
- 白名单为空时保持兼容，允许所有目标。
- DB 已有运行配置时，重启不会被 TOML/env 覆盖。
- `go test ./...`、前端 `npm run build`、Docker compose 配置检查通过。

## 14. 实施阶段

1. **后端模型与迁移**：默认管理员、配置 schema、APT mirror、proxy host rules。
2. **运行时配置快照**：DB source of truth、热更新、pending restart 状态。
3. **管理 API**：账号、配置中心、APT mirror、proxy host rules。
4. **前端页面**：产品化配置中心、账号安全、APT 镜像站、代理白名单。
5. **文档与兼容清理**：README、config example、Docker env 说明、废弃项提示。
6. **验证**：单元测试、真实 APT/APK fixture、前端构建、compose 检查。
