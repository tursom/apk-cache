# APK Cache

简体中文 | [English](README_EN.md)

APK Cache 是一个面向 Linux 包仓库的 HTTP 缓存代理服务，当前实现聚焦三条核心链路：

- Alpine Linux APK 缓存代理
- Debian/Ubuntu APT HTTP 代理缓存
- 通用 HTTP/HTTPS 代理转发，主要用于 HTTPS APT 源的 `CONNECT` 隧道

项目已经按新的轻量内核重写，保留核心缓存、校验、代理、监控和 Docker 部署能力；旧管理台、旧 i18n、未接入主流程的限流/磁盘配额/策略系统不再属于当前版本。

## 功能概览

- APK 缓存：缓存 `.apk` 包和 `APKINDEX.tar.gz`。
- APT 缓存：通过 HTTP 代理模式缓存 `.deb`、`Release`、`InRelease`、`Packages*`、`Sources*` 和 `by-hash` 请求。
- 统一缓存流水线：内存缓存 -> 磁盘缓存 -> 上游回源。
- 并发安全：同一缓存 key 使用文件级互斥，避免并发重复下载和写坏缓存。
- 完整性校验：APK 支持 APKINDEX hash 和 RSA 签名校验；APT 支持 Release/Packages 索引 SHA256、by-hash 和包文件校验。
- 多 APK 上游：支持多个 APK upstream，按健康状态和顺序 failover。
- 上游代理：APK upstream 可单独配置代理；APT/通用代理可使用统一 `proxy.upstream_proxy`。
- HTTPS 隧道：支持 `CONNECT`，用于 HTTPS APT 源透传。
- 运维端点：`/_health` 和 `/metrics`。
- Docker 部署：entrypoint 可根据环境变量生成运行配置。
- CI/CD：GitHub Actions 覆盖测试、二进制构建、Docker 构建/冒烟和 tag release。

## 快速开始

### Docker 运行

```sh
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v "$PWD/cache:/app/cache" \
  -v "$PWD/data:/app/data" \
  ghcr.io/tursom/apk-cache:latest
```

健康检查：

```sh
curl http://127.0.0.1:3142/_health
```

Prometheus 指标：

```sh
curl http://127.0.0.1:3142/metrics
```

### 从源码构建

要求：

- Go `1.25.4` 或兼容版本
- 可选：Docker，用于容器构建和本地冒烟

构建：

```sh
./build.sh
```

运行：

```sh
cp config.example.toml config.toml
./apk-cache -config config.toml
```

测试：

```sh
go test ./...
go test ./... -coverprofile=coverage.out
```

## 客户端配置

### Alpine APK

将 Alpine 源改为 APK Cache 服务地址。例如服务地址为 `http://cache.example:3142`：

```sh
sed -i 's|https://dl-cdn.alpinelinux.org|http://cache.example:3142|g' /etc/apk/repositories
apk update
apk add curl
```

Dockerfile 示例：

```dockerfile
FROM alpine:3.23

RUN sed -i 's|https://dl-cdn.alpinelinux.org|http://cache.example:3142|g' /etc/apk/repositories \
    && apk add --no-cache curl
```

APK 请求示例：

```text
/alpine/v3.23/main/x86_64/APKINDEX.tar.gz
/alpine/v3.23/main/x86_64/busybox-1.37.0-r0.apk
```

### Debian/Ubuntu APT

APT 通过 HTTP 代理方式使用 APK Cache。

创建 `/etc/apt/apt.conf.d/01-apk-cache-proxy`：

```conf
Acquire::HTTP::Proxy "http://cache.example:3142";
Acquire::HTTPS::Proxy "http://cache.example:3142";
```

然后运行：

```sh
apt-get update
apt-get install curl
```

注意：

- `http://` APT 源可以被解析和缓存。
- `https://` APT 源通常通过 `CONNECT` 建立 TLS 隧道，当前服务会透传，但不会解密或缓存 TLS 内部内容。

## 配置文件

默认配置文件路径是 `config.toml`，也可以用 `-config` 指定：

```sh
./apk-cache -config /path/to/config.toml
```

完整示例见 [config.example.toml](config.example.toml)。

### 配置示例

```toml
[server]
listen = ":3142"

[[upstreams]]
name = "Official Alpine CDN"
url = "https://dl-cdn.alpinelinux.org"
kind = "apk"
# proxy = "socks5://127.0.0.1:1080"

[cache]
root = "./cache"
data_root = "./data"
index_ttl = "24h"
package_ttl = "720h"

[cache.memory]
enabled = true
max_size = "256MB"
max_item_size = "16MB"
ttl = "30m"
max_items = 2048

[transport]
timeout = "30s"
idle_conn_timeout = "90s"
max_idle_conns = 128

[apk]
enabled = true
verify_hash = true
verify_signature = true
keys_dir = ""

[apt]
enabled = true
verify_hash = true
load_index_async = true

[proxy]
enabled = true
allow_connect = true
cache_non_package_requests = false
upstream_proxy = ""
allowed_hosts = []
```

### 配置项说明

| 配置 | 默认值 | 说明 |
| --- | --- | --- |
| `server.listen` | `:3142` | HTTP 监听地址 |
| `upstreams[].name` | `Official Alpine CDN` | APK 上游名称，仅用于识别 |
| `upstreams[].url` | `https://dl-cdn.alpinelinux.org` | APK 上游基础地址 |
| `upstreams[].kind` | `apk` | 当前仅 APK 上游参与 APK 回源 |
| `upstreams[].proxy` | 空 | 当前 APK 上游使用的出站代理 |
| `cache.root` | `./cache` | 磁盘缓存目录 |
| `cache.data_root` | `./data` | 运行数据目录，当前保留给后续扩展 |
| `cache.index_ttl` | `24h` | 索引文件缓存 TTL |
| `cache.package_ttl` | `720h` | 包文件缓存 TTL；`0` 表示不过期 |
| `cache.memory.enabled` | `true` | 是否启用内存缓存 |
| `cache.memory.max_size` | `256MB` | 内存缓存总大小 |
| `cache.memory.max_item_size` | `16MB` | 可进入内存缓存的单文件最大大小 |
| `cache.memory.ttl` | `30m` | 内存缓存项 TTL |
| `cache.memory.max_items` | `2048` | 内存缓存最大条目数 |
| `transport.timeout` | `30s` | 回源 HTTP client 超时 |
| `transport.idle_conn_timeout` | `90s` | 空闲连接保留时间 |
| `transport.max_idle_conns` | `128` | HTTP transport 最大空闲连接数 |
| `apk.enabled` | `true` | 是否启用 APK 链路 |
| `apk.verify_hash` | `true` | 是否使用 APKINDEX 校验 `.apk` |
| `apk.verify_signature` | `true` | 是否校验 APK/APKINDEX RSA 签名 |
| `apk.keys_dir` | 空 | 额外 Alpine RSA 公钥目录 |
| `apt.enabled` | `true` | 是否启用 APT 链路 |
| `apt.verify_hash` | `true` | 是否校验 APT by-hash、Release 索引引用文件和包文件 SHA256 |
| `apt.load_index_async` | `true` | 是否异步加载新缓存的 APT 索引 |
| `proxy.enabled` | `true` | 是否启用通用 HTTP/HTTPS 代理 |
| `proxy.allow_connect` | `true` | 是否允许 `CONNECT` 隧道 |
| `proxy.cache_non_package_requests` | `false` | 是否缓存非 APK/APT 普通 HTTP 请求 |
| `proxy.upstream_proxy` | 空 | APT 和通用代理使用的出站代理 |
| `proxy.allowed_hosts` | `[]` | 非空时只允许代理这些目标 host |

支持的代理 URL：

```text
socks5://127.0.0.1:1080
http://127.0.0.1:8080
https://127.0.0.1:8443
```

带认证的 HTTP 代理可写成：

```text
http://user:password@proxy.example:8080
```

## Docker 环境变量

容器启动时，`entrypoint.sh` 会根据环境变量生成临时 TOML 配置，然后执行 `/app/apk-cache -config <generated>`。

| 环境变量 | 默认值 | 对应配置 |
| --- | --- | --- |
| `CONFIG` | `/tmp/apk-cache.toml` | 生成的配置文件路径 |
| `LISTEN` / `ADDR` | `:3142` | `server.listen` |
| `CACHE_ROOT` / `CACHE_DIR` | `/app/cache` | `cache.root` |
| `DATA_ROOT` | `/app/data` | `cache.data_root` |
| `APK_UPSTREAM` / `UPSTREAM` | `https://dl-cdn.alpinelinux.org` | APK upstream URL |
| `APK_UPSTREAM_PROXY` / `PROXY` | 空 | APK upstream proxy |
| `UPSTREAM_PROXY` | 空 | `proxy.upstream_proxy` |
| `INDEX_TTL` | `24h` | `cache.index_ttl` |
| `PACKAGE_TTL` | `720h` | `cache.package_ttl` |
| `MEMORY_CACHE_ENABLED` | `true` | `cache.memory.enabled` |
| `MEMORY_CACHE_SIZE` | `256MB` | `cache.memory.max_size` |
| `MEMORY_CACHE_MAX_ITEM_SIZE` | `16MB` | `cache.memory.max_item_size` |
| `MEMORY_CACHE_TTL` | `30m` | `cache.memory.ttl` |
| `MEMORY_CACHE_MAX_ITEMS` | `2048` | `cache.memory.max_items` |
| `TRANSPORT_TIMEOUT` | `30s` | `transport.timeout` |
| `TRANSPORT_IDLE_CONN_TIMEOUT` | `90s` | `transport.idle_conn_timeout` |
| `TRANSPORT_MAX_IDLE_CONNS` | `128` | `transport.max_idle_conns` |
| `APK_ENABLED` | `true` | `apk.enabled` |
| `APK_VERIFY_HASH` | `true` | `apk.verify_hash` |
| `APK_VERIFY_SIGNATURE` | `true` | `apk.verify_signature` |
| `APT_ENABLED` | `true` | `apt.enabled` |
| `APT_VERIFY_HASH` | `true` | `apt.verify_hash` |
| `APT_LOAD_INDEX_ASYNC` | `true` | `apt.load_index_async` |
| `PROXY_ENABLED` | `true` | `proxy.enabled` |
| `PROXY_ALLOW_CONNECT` | `true` | `proxy.allow_connect` |
| `PROXY_CACHE_NON_PACKAGE_REQUESTS` | `false` | `proxy.cache_non_package_requests` |
| `PROXY_ALLOWED_HOSTS` | 空 | 逗号分隔的 `proxy.allowed_hosts` |

Docker 示例：

```sh
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v "$PWD/cache:/app/cache" \
  -e APK_UPSTREAM=https://mirrors.tuna.tsinghua.edu.cn/alpine \
  -e UPSTREAM_PROXY=socks5://127.0.0.1:1080 \
  ghcr.io/tursom/apk-cache:latest
```

## 工作原理

### 请求分流

所有请求进入同一个 HTTP handler：

1. `/_health` 返回健康检查。
2. `/metrics` 返回 Prometheus 指标。
3. `CONNECT` 进入隧道代理。
4. 普通请求按路径或 URL 判断为 APK、APT 或通用代理。

### 缓存流程

APK、APT 和可缓存的普通代理请求都会走统一缓存流程：

1. 查询内存缓存，命中返回 `X-Cache: MEMORY-HIT`。
2. 查询磁盘缓存，命中返回 `X-Cache: HIT`。
3. 对同一缓存 key 加锁，避免并发重复下载。
4. 再次查询缓存，防止等待锁期间已有其他请求写入。
5. 回源请求，上游响应一边返回客户端，一边写入临时文件。
6. 下载完成后执行协议校验。
7. 校验通过后原子 `rename` 为正式缓存文件。
8. 必要时写入内存缓存。
9. 首次回源返回 `X-Cache: MISS`。

非 `200 OK` 的上游响应会直接透传，不写入缓存，返回 `X-Cache: BYPASS`。

### APK 校验

APK 校验由 `internal/apk` 实现：

- `APKINDEX.tar.gz` 会被解析为包名、版本、大小和 checksum 的映射。
- `.apk` 命中缓存或下载完成后，可以按 APKINDEX 记录校验大小和 hash。
- APK/APKINDEX 可以校验 `.SIGN.*` RSA 签名。
- 内置 Alpine 默认公钥，也可以通过 `apk.keys_dir` 加载额外公钥。
- 新下载内容签名失败时会返回给客户端，但不会写入缓存。

### APT 校验

APT 校验由 `internal/apt` 实现：

- `Release` / `InRelease` 会解析 SHA256 文件清单。
- `Packages*` / `Sources*` 会解析包文件路径、大小和 SHA256。
- `by-hash/SHA256/<hash>` 请求会按 URL 中声明的 hash 校验。
- 如果 `by-hash` 命中的是 `Release` 中记录的索引文件，会按原始索引路径解析 `Packages*` / `Sources*` 内容。
- `Release` 引用的 `Packages*` / `Sources*` 文件会在缓存命中和下载完成后按 SHA256 校验。
- `.deb` 如果能从索引中找到 SHA256，就在缓存命中和下载完成后校验。
- 未找到索引记录时不阻断请求。

### CONNECT 隧道

`CONNECT` 只建立 TCP 隧道：

- 不解析 TLS。
- 不缓存 TLS 内部内容。
- 可通过 `proxy.allow_connect=false` 禁用。
- 可通过 `proxy.allowed_hosts` 限制目标 host。
- 并发隧道有固定上限，防止无限占用连接。

## 运维端点

### `GET /_health`

返回 JSON，例如：

```json
{
  "status": "healthy",
  "apk_upstreams_total": 1,
  "apk_upstreams": {
    "healthy": 1,
    "total": 1
  },
  "disk_cache": {
    "status": "healthy"
  },
  "memory_cache": {
    "items": 0,
    "size": 0,
    "max": 268435456
  }
}
```

当缓存目录不可用或 APK upstream 全部不可用时，状态会降级为 `degraded`，HTTP 状态码为 `503`。

### `GET /metrics`

暴露 Prometheus 指标，主要包括：

- `apk_cache_hits_total`
- `apk_cache_misses_total`
- `apk_cache_download_bytes_total`
- `apk_cache_response_bytes_total`
- `apk_cache_upstream_requests_total`
- `apk_cache_upstream_failovers_total`
- `apk_cache_validation_failures_total`
- `apk_cache_apk_hash_failures_total`
- `apk_cache_apk_signature_failures_total`
- `apk_cache_apk_bypass_responses_total`
- `apk_cache_memory_hits_total`
- `apk_cache_memory_misses_total`
- `apk_cache_memory_evictions_total`
- `apk_cache_memory_size_bytes`
- `apk_cache_memory_items_total`

## 开发与测试

常用命令：

```sh
go test ./...
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
./build.sh
docker build -t apk-cache:local .
```

真实数据集成测试在 `internal/app/real_integration_test.go`：

- APT 使用从 `deb.debian.org` 获取的 `Release`、`Packages.xz` 和 `.deb` fixture，覆盖 `Release -> Packages.xz -> by-hash -> .deb` 的缓存与 SHA256 校验。
- APK 使用从 `dl-cdn.alpinelinux.org` 获取的 `APKINDEX.tar.gz` 和 `.apk` fixture，覆盖真实签名校验、APKINDEX hash 校验和缓存损坏后回源重取。
- 测试运行时由本地 `httptest.Server` 提供这些 fixture，不依赖外网。

本地 Docker 冒烟：

```sh
docker run --rm -p 3142:3142 apk-cache:local
curl http://127.0.0.1:3142/_health
```

## GitHub Actions

[.github/workflows/build.yml](.github/workflows/build.yml) 当前包含：

- Go 单元测试和 coverage artifact。
- Linux / Windows / macOS 多平台二进制构建。
- Docker 镜像构建和 `/_health` 冒烟。
- 非 PR 事件推送 GHCR 镜像。
- `v*` tag 创建 GitHub Release 并上传二进制产物。

## 当前限制

- 没有 Web 管理台；观察入口是 `/_health` 和 `/metrics`。
- 没有管理台认证。
- 没有全局限流。
- 没有磁盘配额和自动清理策略。
- HTTPS APT 源通过 `CONNECT` 透传，不解密也不缓存。
- `data_root` 当前保留给后续扩展，核心缓存状态主要来自磁盘缓存和内存索引。

这些能力后续可以在当前简化内核之上重新设计，但不再恢复旧版已经失配的实现。
