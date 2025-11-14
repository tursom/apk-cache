# APK Cache

[English](README_EN.md) | 简体中文

一个用于缓存 Alpine Linux APK 包的代理服务器，支持 SOCKS5 代理和多语言界面。

## 功能特性

- 🚀 自动缓存 Alpine Linux APK 包
- 📦 缓存命中时直接从本地提供服务
- 🔄 缓存未命中时从上游服务器获取并保存
- 🌐 支持 SOCKS5 代理访问上游服务器
- 💾 可配置的缓存目录和监听地址
- ⏱️ 灵活的缓存过期策略
  - APKINDEX 索引文件按**修改时间**过期（默认 24 小时）
  - APK 包文件按**访问时间**过期（默认永不过期）
  - 优先使用内存中的访问时间，提升性能
- 🧹 自动清理过期缓存（可配置清理间隔）
- 🔒 客户端断开连接不影响缓存文件保存
- ⚡ 同时写入缓存和客户端，提升响应速度
- 🔐 文件级锁管理，避免并发下载冲突
- 🌍 多语言支持（中文/英文），自动检测系统语言
- 📊 Prometheus 监控指标
- 🎛️ Web 管理界面，实时查看统计信息
- 🔑 管理界面可选 HTTP Basic Auth 认证
- 💰 **缓存配额管理** - 限制缓存大小并自动清理（LRU/LFU/FIFO策略）
- 📈 **智能访问时间跟踪** - 内存优先，避免频繁系统调用

## 快速开始

### 使用 Docker（推荐）

最快的方式是使用 Docker Hub 上的官方镜像：

```bash
# 拉取并运行
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v ./cache:/app/cache \
  tursom/apk-cache:latest
```

访问 http://localhost:3142/_admin/ 查看管理界面。

### 安装

#### 从源码构建

```bash
git clone https://github.com/tursom/apk-cache.git
cd apk-cache
go build -o apk-cache ./cmd/apk-cache
```

#### 使用 Docker

```bash
# 拉取官方镜像
docker pull tursom/apk-cache:latest

# 运行容器
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v ./cache:/app/cache \
  -e ADDR=:80 \
  -e CACHE_DIR=/app/cache \
  -e INDEX_CACHE=24h \
  tursom/apk-cache:latest

# 使用代理
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v ./cache:/app/cache \
  -e PROXY=socks5://127.0.0.1:1080 \
  tursom/apk-cache:latest
```

**Docker Hub**: https://hub.docker.com/r/tursom/apk-cache

### 运行

```bash
# 默认配置运行（自动检测系统语言）
./apk-cache

# 使用配置文件
./apk-cache -config config.toml

# 自定义配置
./apk-cache -addr :3142 -cache ./cache -proxy socks5://127.0.0.1:1080

# 指定语言
./apk-cache -locale zh  # 中文
./apk-cache -locale en  # 英文

# 配置文件 + 命令行参数（命令行参数优先级更高）
./apk-cache -config config.toml -addr :8080
```

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-config` | (空) | 配置文件路径（可选） |
| `-addr` | `:3142` | 监听地址 |
| `-cache` | `./cache` | 缓存目录路径 |
| `-upstream` | `https://dl-cdn.alpinelinux.org` | 上游服务器地址 |
| `-proxy` | (空) | SOCKS5 代理地址，格式: `socks5://[username:password@]host:port` |
| `-index-cache` | `24h` | APKINDEX.tar.gz 索引文件缓存时间（按修改时间） |
| `-pkg-cache` | `0` | APK 包文件缓存时间（按访问时间，0 = 永不过期） |
| `-cleanup-interval` | `1h` | 自动清理过期缓存的间隔（0 = 禁用自动清理） |
| `-locale` | (自动检测) | 界面语言 (`en`/`zh`)，留空则根据 `LANG` 环境变量自动检测 |
| `-admin-user` | `admin` | 管理界面用户名 |
| `-admin-password` | (空) | 管理界面密码（留空则无需认证） |
| `-cache-max-size` | (空) | 最大缓存大小（如 `10GB`, `1TB`, `0` = 无限制） |
| `-cache-clean-strategy` | `LRU` | 缓存清理策略 (`LRU`/`LFU`/`FIFO`) |

## 使用方法

### 配置 Alpine Linux 使用缓存服务器

编辑 `/etc/apk/repositories`:

```bash
# 将默认的镜像地址替换为缓存服务器地址
sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories
```

或者直接使用命令行:

```bash
# 安装软件包时指定缓存服务器
apk add --repositories-file /dev/null --repository http://your-cache-server:3142/alpine/v3.22/main <package-name>
```

### 实际使用示例

#### 1. 在 Dockerfile 中使用

```dockerfile
FROM alpine:3.22

# 配置使用 APK 缓存服务器
RUN sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories

# 安装软件包（将使用缓存）
RUN apk update && apk add --no-cache \
    curl \
    wget \
    git \
    build-base

# 其他构建步骤...
```

#### 2. 在 Alpine 虚拟机中使用

```bash
# 备份原有配置
cp /etc/apk/repositories /etc/apk/repositories.bak

# 配置缓存服务器
cat > /etc/apk/repositories << EOF
http://your-cache-server:3142/alpine/v3.22/main
http://your-cache-server:3142/alpine/v3.22/community
EOF

# 更新索引并安装软件
apk update
apk add docker python3 nodejs
```

#### 3. 临时使用（不修改配置文件）

```bash
# 单次使用缓存服务器
apk add --repositories-file /dev/null \
  --repository http://your-cache-server:3142/alpine/v3.22/main \
  --repository http://your-cache-server:3142/alpine/v3.22/community \
  nginx
```

#### 4. 验证缓存是否工作

```bash
# 第一次请求（缓存未命中）
curl -I http://your-cache-server:3142/alpine/v3.22/main/x86_64/APKINDEX.tar.gz
# 响应头应包含: X-Cache: MISS

# 第二次请求（缓存命中）
curl -I http://your-cache-server:3142/alpine/v3.22/main/x86_64/APKINDEX.tar.gz
# 响应头应包含: X-Cache: HIT
```

## 工作原理

1. 客户端请求软件包时，服务器首先检查本地缓存
2. 对于 `APKINDEX.tar.gz` 索引文件，检查缓存是否过期（默认 24 小时）
3. 如果缓存命中且未过期 (`X-Cache: HIT`)，直接从本地文件返回
4. 如果缓存未命中 (`X-Cache: MISS`)，从上游服务器下载
5. 下载时使用**文件级锁**避免多个请求重复下载同一文件
6. 同时写入缓存文件和客户端响应，提升用户体验
7. 即使客户端中途断开连接，缓存文件也会完整保存
8. 下载完成后，文件保存到本地缓存目录供后续请求使用

### 缓存策略

- **APKINDEX.tar.gz 索引文件**: 
  - 按**修改时间**过期（默认 24 小时）
  - 定期刷新以获取最新的软件包信息
  - 可通过 `-index-cache` 参数调整
  
- **APK 包文件**: 
  - 按**访问时间**过期（默认永不过期）
  - 优先使用内存中记录的访问时间
  - 如果内存中没有记录，从文件系统读取 atime
  - 如果无法获取 atime，使用进程启动时间（避免程序重启后立即清理）
  - 可通过 `-pkg-cache` 参数设置过期时间（如 `168h` = 7天）

- **自动清理**:
  - 通过 `-cleanup-interval` 设置清理间隔
  - 仅在 `-pkg-cache` 不为 0 时启用
  - 定期扫描并删除过期文件

### 并发控制

- 使用文件级锁管理器，避免并发请求重复下载同一文件
- 第一个请求获取锁并下载文件，后续请求等待锁释放后直接读取缓存
- 引用计数机制自动清理不再使用的锁，避免内存泄漏

## 注意事项

- 缓存目录会随着使用逐渐增大，建议定期清理或设置磁盘配额
- 使用 SOCKS5 代理时，确保代理服务器可访问
- 服务器默认监听所有网络接口，生产环境建议配置防火墙规则
- APKINDEX.tar.gz 索引文件会定期刷新以获取最新的软件包信息
- 索引缓存时间建议设置在 1 小时到 24 小时之间，生产环境可根据实际情况调整
- 并发请求同一文件时，只会下载一次，其他请求会等待并共享缓存

## 高级配置

### Web 管理界面

访问 `http://your-server:3142/_admin/` 查看：

- 📈 实时统计数据（缓存命中率、下载量等）
- 🔒 活跃的文件锁数量
- 📝 跟踪的访问时间记录数
- 💾 缓存总大小
- 🗑️ 一键清空缓存
- 📊 Prometheus 指标链接

#### 启用认证

```bash
# 设置管理界面密码
./apk-cache -admin-password "your-secret-password"

# 访问时需要输入：
# 用户名: admin
# 密码: your-secret-password
```

### Prometheus 监控

访问 `http://your-server:3142/metrics` 获取指标：

- `apk_cache_hits_total` - 缓存命中次数
- `apk_cache_misses_total` - 缓存未命中次数
- `apk_cache_download_bytes_total` - 从上游下载的总字节数

配置 Prometheus：

```yaml
scrape_configs:
  - job_name: 'apk-cache'
    static_configs:
      - targets: ['your-server:3142']
```

### 配置文件

创建 `config.toml`：

```toml
[server]
addr = ":3142"
locale = "zh"  # 或 "en"

[upstream]
url = "https://dl-cdn.alpinelinux.org"
# proxy = "socks5://127.0.0.1:1080"

[cache]
dir = "./cache"
index_duration = "24h"
pkg_duration = "168h"  # 7 天
cleanup_interval = "1h"
# 新增：缓存配额配置
max_size = "10GB"        # 最大缓存大小（如 "10GB", "1TB", "0" = 无限制）
clean_strategy = "LRU"   # 清理策略（"LRU", "LFU", "FIFO"）

[security]
admin_password = "your-secret-password"
```

使用配置文件：

```bash
./apk-cache -config config.toml
```

**优先级规则**：命令行参数 > 配置文件 > 默认值

```bash
# 配置文件设置 addr = ":3142"
# 但命令行参数会覆盖它
./apk-cache -config config.toml -addr :8080
# 最终监听在 :8080
```

### Docker 部署

#### 使用环境变量

```bash
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v $(pwd)/cache:/app/cache \
  -e ADDR=:80 \
  -e CACHE_DIR=/app/cache \
  -e INDEX_CACHE=24h \
  -e PROXY=socks5://127.0.0.1:1080 \
  -e UPSTREAM=https://dl-cdn.alpinelinux.org \
  tursom/apk-cache:latest
```

**支持的环境变量**：
- `ADDR` - 监听地址（默认 `:3142`）
- `CACHE_DIR` - 缓存目录（默认 `./cache`）
- `INDEX_CACHE` - 索引缓存时间（默认 `24h`）
- `PKG_CACHE` - 包缓存时间（默认 `0`，永不过期）
- `CLEANUP_INTERVAL` - 自动清理间隔（默认 `1h`）
- `PROXY` - 代理地址（可选）
- `UPSTREAM` - 上游服务器（默认 `https://dl-cdn.alpinelinux.org`）
- `LOCALE` - 界面语言，`en` 或 `zh`（可选，默认自动检测）
- `ADMIN_USER` - 管理界面用户名（默认 `admin`）
- `ADMIN_PASSWORD` - 管理界面密码（可选，留空则无需认证）
- `CONFIG` - 配置文件路径（可选）
- `CACHE_MAX_SIZE` - 最大缓存大小（如 `10GB`, `1TB`, `0` = 无限制）
- `CACHE_CLEAN_STRATEGY` - 缓存清理策略（`LRU`/`LFU`/`FIFO`，默认 `LRU`）

#### 使用配置文件

```bash
# 创建配置文件
cat > config.toml << EOF
[server]
addr = ":80"

[[upstreams]]
name = "Official CDN"
url = "https://dl-cdn.alpinelinux.org"

[cache]
dir = "/app/cache"
index_duration = "24h"
EOF

# 挂载配置文件运行
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v $(pwd)/cache:/app/cache \
  -v $(pwd)/config.toml:/app/config.toml \
  tursom/apk-cache:latest -config /app/config.toml
```

#### Docker Compose

```yaml
version: '3.8'

services:
  apk-cache:
    image: tursom/apk-cache:latest
    container_name: apk-cache
    ports:
      - "3142:80"
    volumes:
      - ./cache:/app/cache
      - ./config.toml:/app/config.toml  # 可选
    environment:
      - ADDR=:80
      - CACHE_DIR=/app/cache
      - INDEX_CACHE=24h
      - PKG_CACHE=168h  # 7天
      - CLEANUP_INTERVAL=1h
      - LOCALE=zh  # 或 en
      - ADMIN_USER=admin
      - ADMIN_PASSWORD=your-secret-password
      # - PROXY=socks5://host.docker.internal:1080
      # - CONFIG=/app/config.toml
    restart: unless-stopped
```

**从源码构建镜像**（可选）:

```bash
# 克隆仓库
git clone https://github.com/tursom/apk-cache.git
cd apk-cache

# 构建镜像
docker build -t apk-cache:local .

# 使用本地构建的镜像
docker run -d -p 3142:80 -v ./cache:/app/cache apk-cache:local
```

### 缓存配额管理

新增的缓存配额管理功能可以限制缓存总大小，并在空间不足时自动清理旧文件。

#### 清理策略

支持三种清理策略：

- **LRU** (最近最少使用) - 默认策略，删除最长时间未被访问的文件
- **LFU** (最不经常使用) - 删除访问频率最低的文件
- **FIFO** (先进先出) - 删除最早缓存的文件

#### 使用示例

```bash
# 限制缓存大小为 10GB，使用 LRU 策略
./apk-cache -cache-max-size 10GB -cache-clean-strategy LRU

# 限制为 1TB，使用 FIFO 策略
./apk-cache -cache-max-size 1TB -cache-clean-strategy FIFO

# 禁用缓存大小限制（默认行为）
./apk-cache -cache-max-size 0
```

#### 配置文件示例

```toml
[cache]
dir = "./cache"
index_duration = "24h"
pkg_duration = "168h"
cleanup_interval = "1h"
max_size = "10GB"        # 最大缓存大小
clean_strategy = "LRU"   # 清理策略
```

#### Docker 环境变量

```bash
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v ./cache:/app/cache \
  -e CACHE_MAX_SIZE=10GB \
  -e CACHE_CLEAN_STRATEGY=LRU \
  tursom/apk-cache:latest
```

#### 工作原理

1. **实时监控**: 每次添加新文件时检查缓存大小
2. **智能清理**: 当空间不足时，按选定策略清理旧文件
3. **索引保护**: 优先保留索引文件，确保系统可用性
4. **内存优化**: 使用内存跟踪访问时间，避免频繁系统调用

#### Prometheus 指标

新增的监控指标：
- `apk_cache_quota_size_bytes{type="max"}` - 最大缓存大小
- `apk_cache_quota_size_bytes{type="current"}` - 当前缓存大小
- `apk_cache_quota_files_total` - 缓存文件总数
- `apk_cache_quota_cleanups_total` - 配额清理次数
- `apk_cache_quota_bytes_freed_total` - 释放的总字节数

### 多上游服务器和故障转移

配置多个上游服务器，自动故障转移：

```toml
# config.toml
[[upstreams]]
name = "Official CDN"
url = "https://dl-cdn.alpinelinux.org"

[[upstreams]]
name = "Tsinghua Mirror"
url = "https://mirrors.tuna.tsinghua.edu.cn/alpine"
proxy = "socks5://127.0.0.1:1080"  # 可选的代理

[[upstreams]]
name = "USTC Mirror"
url = "https://mirrors.ustc.edu.cn/alpine"
proxy = "http://proxy.example.com:8080"  # 支持 HTTP 代理
```

**特性**：
- ✅ 按顺序尝试所有上游服务器
- ✅ 第一个服务器失败时自动切换到下一个
- ✅ 每个服务器可以单独配置代理
- ✅ 支持 SOCKS5 和 HTTP 代理
- ✅ 自动记录哪个服务器成功响应

**工作流程**：
1. 尝试第一个上游服务器
2. 如果失败（网络错误或非 200 状态码），尝试下一个
3. 直到找到成功的服务器或全部失败
4. 使用备用服务器时会在日志中记录

### 多语言支持

程序会自动检测系统语言（通过 `LC_ALL`、`LC_MESSAGES` 或 `LANG` 环境变量）：

```bash
# 使用系统默认语言
./apk-cache

# 强制使用中文
./apk-cache -locale zh
LANG=zh_CN.UTF-8 ./apk-cache

# 强制使用英文
./apk-cache -locale en
LANG=en_US.UTF-8 ./apk-cache
```

### 调整索引缓存时间

```bash
# stable 版本（生产环境推荐）- 主要是安全更新，更新不频繁
./apk-cache -index-cache 24h   # 1 天

# edge 版本（开发环境）- 包更新频繁
./apk-cache -index-cache 2h    # 2 小时

# 对时效性要求极高的场景
./apk-cache -index-cache 1h    # 1 小时

# 内网环境，对上游服务器负载不敏感
./apk-cache -index-cache 12h   # 12 小时
```

**注意**: Go 的 `time.ParseDuration` 不支持 `d`（天）单位，请使用小时 `h`。例如 1 天 = `24h`，7 天 = `168h`。

### 使用带认证的 SOCKS5 代理

```bash
./apk-cache -proxy socks5://username:password@127.0.0.1:1080
```

### 自定义上游服务器

```bash
# 使用清华大学镜像
./apk-cache -upstream https://mirrors.tuna.tsinghua.edu.cn/alpine

# 使用阿里云镜像
./apk-cache -upstream https://mirrors.aliyun.com/alpine
```

### 缓存过期和自动清理

```bash
# 索引文件 24 小时过期，包文件 7 天过期，每小时清理一次
./apk-cache -index-cache 24h -pkg-cache 168h -cleanup-interval 1h

# 索引文件 12 小时过期，包文件 30 天过期，每 6 小时清理
./apk-cache -index-cache 12h -pkg-cache 720h -cleanup-interval 6h

# 禁用包文件过期（永久缓存）
./apk-cache -pkg-cache 0

# 设置过期时间但禁用自动清理（通过管理界面手动清理）
./apk-cache -pkg-cache 168h -cleanup-interval 0
```

**注意**: 
- 只有当 `-pkg-cache` 不为 0 时，自动清理才会启用
- `-cleanup-interval` 设为 0 时，不会启动自动清理协程

## 性能特性

### 并发安全

- **文件级锁管理**: 使用自定义的 `FileLockManager`，确保同一文件只会被下载一次
- **引用计数**: 自动管理锁的生命周期，避免内存泄漏
- **双重检查**: 获取锁后再次检查缓存，避免重复下载

### 客户端友好

- **流式传输**: 边下载边传输给客户端，无需等待完整下载
- **断点续传**: 客户端断开不影响缓存完整性
- **并发下载**: 不同文件可以并发下载，互不影响

### 智能访问时间跟踪

- **内存优先**: 优先使用内存中记录的访问时间，避免频繁系统调用
- **文件系统降级**: 内存中无记录时，从文件系统读取 atime
- **进程启动保护**: 无法获取 atime 时使用进程启动时间，避免程序重启后立即清理旧缓存
- **自动清理**: 删除文件时同步清理内存中的访问时间记录

## 监控和管理

### Web 管理界面

- **访问地址**: `http://your-server:3142/_admin/`
- **实时统计**: 缓存命中率、下载量、活跃锁、跟踪文件数等
- **缓存管理**: 查看缓存大小、一键清空缓存
- **服务器信息**: 监听地址、缓存目录、上游服务器、配置参数等

### Prometheus 指标

- **访问地址**: `http://your-server:3142/metrics`
- **指标列表**:
  - `apk_cache_hits_total` - 缓存命中总次数
  - `apk_cache_misses_total` - 缓存未命中总次数
  - `apk_cache_download_bytes_total` - 从上游下载的总字节数

### 认证保护

```bash
# 启用管理界面认证
./apk-cache -admin-password "secure-password"

# 访问 /_admin/ 时需要输入：
# 用户名: admin
# 密码: secure-password
```

## 项目结构

```
apk-cache/
├── cmd/
│   └── apk-cache/
│       ├── main.go            # 主程序入口
│       ├── config.go          # 配置文件处理
│       ├── cache.go           # 缓存处理逻辑
│       ├── web.go             # Web 管理界面
│       ├── cleanup.go         # 自动清理功能
│       ├── lockman.go         # 文件锁管理器
│       ├── lockman_test.go    # 锁管理器单元测试
│       ├── access_tracker.go  # 访问时间跟踪器
│       ├── admin.html         # 管理界面 HTML（嵌入）
│       └── locales/
│           ├── en.toml        # 英文翻译（嵌入）
│           └── zh.toml        # 中文翻译（嵌入）
├── cache/                     # 缓存目录（运行时生成）
├── Dockerfile                 # Docker 镜像构建文件
├── entrypoint.sh              # Docker 容器启动脚本
├── config.example.toml        # 配置文件示例
├── go.mod
├── go.sum
├── README.md                  # 中文文档
├── README_EN.md               # 英文文档
├── ADMIN.md                   # 管理界面文档
└── LICENSE                    # GPLv3 许可证
```

## 依赖

- `golang.org/x/net/proxy` - SOCKS5 代理支持
- `github.com/nicksnyder/go-i18n/v2` - 国际化支持
- `github.com/BurntSushi/toml` - TOML 配置文件解析
- `github.com/prometheus/client_golang` - Prometheus 监控指标
- `golang.org/x/text/language` - 语言检测和处理

## 故障排除

### 1. 无法连接到上游服务器

**问题**: 日志显示 "dial tcp: lookup dl-cdn.alpinelinux.org: no such host"

**解决方案**:
```bash
# 检查 DNS 解析
nslookup dl-cdn.alpinelinux.org

# 如果 DNS 有问题，使用镜像站
./apk-cache -upstream https://mirrors.tuna.tsinghua.edu.cn/alpine

# 或配置多个上游服务器实现故障转移
```

### 2. 代理连接失败

**问题**: 使用 SOCKS5 代理时连接超时

**解决方案**:
```bash
# 验证代理是否可用
curl -x socks5://127.0.0.1:1080 https://dl-cdn.alpinelinux.org

# 检查代理格式是否正确
./apk-cache -proxy socks5://username:password@host:port

# 尝试 HTTP 代理
./apk-cache -proxy http://127.0.0.1:8080
```

### 3. 缓存未命中

**问题**: 总是显示 `X-Cache: MISS`，缓存不生效

**解决方案**:
```bash
# 检查缓存目录权限
ls -la ./cache

# 确保有写入权限
chmod 755 ./cache

# 检查磁盘空间
df -h

# 查看日志了解详细错误
./apk-cache -addr :3142 2>&1 | tee apk-cache.log
```

### 4. 管理界面无法访问

**问题**: 访问 `/_admin/` 返回 404 或认证失败

**解决方案**:
```bash
# 检查是否正确访问管理界面（注意末尾的斜杠）
curl http://localhost:3142/_admin/

# 如果设置了密码，使用 Basic Auth
curl -u admin:your-password http://localhost:3142/_admin/

# 在浏览器中访问时，使用正确的凭据：
# 用户名: admin
# 密码: (你设置的密码)
```

### 5. 自动清理不工作

**问题**: 旧文件没有被自动删除

**解决方案**:
```bash
# 确保同时设置了 pkg-cache 和 cleanup-interval
./apk-cache -pkg-cache 168h -cleanup-interval 1h

# 检查日志中是否有清理记录
# 如果 pkg-cache 为 0，自动清理会被禁用

# 手动清理（通过管理界面）
curl -u admin:password -X POST http://localhost:3142/_admin/clear
```

### 6. 多个客户端并发下载时速度慢

**问题**: 多个请求同时下载同一文件时性能下降

**这是正常的**: 文件锁机制确保只下载一次，其他请求会等待第一个请求完成。这是为了避免重复下载和缓存冲突。

**优化建议**:
- 使用更快的上游服务器或镜像
- 配置 SOCKS5/HTTP 代理优化网络路径
- 增加带宽或使用 CDN

### 7. Docker 容器中代理不工作

**问题**: 容器内无法通过 `127.0.0.1` 访问主机代理

**解决方案**:
```bash
# 使用 host.docker.internal（Mac/Windows）
docker run -e PROXY=socks5://host.docker.internal:1080 apk-cache

# 使用主机网络模式（Linux）
docker run --network host -e PROXY=socks5://127.0.0.1:1080 apk-cache

# 或使用宿主机 IP 地址
docker run -e PROXY=socks5://192.168.1.100:1080 apk-cache
```

## 性能优化建议

### 1. 缓存目录使用 SSD

```bash
# 将缓存目录放在 SSD 上可显著提升性能
./apk-cache -cache /mnt/ssd/apk-cache
```

### 2. 调整缓存过期时间

```bash
# 生产环境：延长索引缓存时间减少上游请求
./apk-cache -index-cache 24h -pkg-cache 720h  # 30 天

# 开发环境：缩短缓存时间获取最新包
./apk-cache -index-cache 2h -pkg-cache 168h   # 7 天
```

### 3. 使用本地镜像站

```bash
# 选择地理位置最近的镜像站
./apk-cache -upstream https://mirrors.tuna.tsinghua.edu.cn/alpine
```

### 4. 配置多个上游服务器

提高可用性和下载速度：

```toml
[[upstreams]]
name = "Primary CDN"
url = "https://dl-cdn.alpinelinux.org"

[[upstreams]]
name = "Backup Mirror 1"
url = "https://mirrors.tuna.tsinghua.edu.cn/alpine"

[[upstreams]]
name = "Backup Mirror 2"
url = "https://mirrors.ustc.edu.cn/alpine"
```

## 常见问题 (FAQ)

**Q: 缓存会占用多少磁盘空间？**

A: 取决于使用情况。一个完整的 Alpine 版本所有包约 2-3 GB，但实际使用中通常只会缓存需要的包，一般占用几百 MB。

**Q: 可以同时为多个 Alpine 版本提供缓存吗？**

A: 可以。缓存目录会按路径自动组织（如 `cache/alpine/v3.22/main/x86_64/`），不同版本互不影响。

**Q: 缓存命中率低怎么办？**

A: 
- 检查索引缓存时间是否太短
- 确保客户端请求的 URL 一致（不要混用 HTTP/HTTPS 或不同域名）
- 查看管理界面了解具体统计信息

**Q: 支持 HTTPS 吗？**

A: 程序本身不支持 HTTPS，建议在前面放置 Nginx 等反向代理来提供 HTTPS 支持。

**Q: 可以限制缓存大小吗？**

A: 是的，现在支持缓存配额管理功能！可以使用 `-cache-max-size` 参数或配置文件的 `max_size` 选项来限制缓存大小，支持 LRU/LFU/FIFO 清理策略自动管理磁盘空间。

## 许可证

GPLv3 License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 作者

[tursom](https://github.com/tursom)

## 链接

- GitHub: https://github.com/tursom/apk-cache
- Docker Hub: https://hub.docker.com/r/tursom/apk-cache
- Issue Tracker: https://github.com/tursom/apk-cache/issues
- Alpine Linux: https://alpinelinux.org/
