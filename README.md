# APK Cache

[English](README_EN.md) | 简体中文

一个用于缓存 Alpine Linux APK 包的代理服务器，支持 SOCKS5/HTTP 代理、APT 包缓存、HTTP/HTTPS 代理和多语言界面。

## 功能特性

- 🚀 **自动缓存** - 自动缓存 Alpine Linux APK 包
- 📦 **三级缓存架构** - 内存 → 文件 → 上游缓存架构
- 🔄 **智能缓存** - 缓存命中时直接从本地提供服务，未命中时从上游获取
- 🌐 **代理支持** - 支持 SOCKS5/HTTP 代理访问上游服务器
- 📦 **APT 包缓存** - 支持 Debian/Ubuntu APT 包缓存
- 🔄 **HTTP/HTTPS 代理** - 支持 HTTP/HTTPS 代理功能，可缓存 APT 和 APK 包
- 💾 **灵活配置** - 可配置的缓存目录、监听地址和缓存策略
- ⏱️ **过期策略** - 灵活的缓存过期时间和自动清理机制
- 🧹 **自动清理** - 自动清理过期缓存和磁盘空间管理
- 🔒 **并发安全** - 文件级锁管理，避免并发下载冲突
- 🌍 **多语言界面** - 支持中文/英文界面和错误消息
- 📊 **监控指标** - Prometheus 监控指标和实时统计
- 🎛️ **Web 管理界面** - 现代化的管理仪表板
- 💰 **缓存配额** - 缓存配额管理（支持 LRU/LFU/FIFO 清理策略）
- 🚀 **内存缓存** - 高性能内存缓存层，减少磁盘 I/O
- 🩺 **健康检查** - 上游服务器状态监控和自愈机制
- 🚦 **请求限流** - 基于令牌桶算法的请求频率限制
- 🔍 **数据完整性** - SHA-256 文件校验和验证和自动修复
- 🔐 **身份验证** - 支持代理身份验证和管理界面认证
- 📈 **故障转移** - 多上游服务器支持和自动故障转移
- 🛡️ **安全增强** - IP 白名单、反向代理支持和路径安全验证

## 快速开始

### 使用 Docker（推荐）

```bash
# 拉取并运行
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v ./cache:/app/cache \
  tursom/apk-cache:latest
```

访问 http://localhost:3142/_admin/ 查看管理界面。

### 从源码构建

**必须使用构建脚本**，因为需要预压缩管理界面的HTML文件：

```bash
git clone https://github.com/tursom/apk-cache.git
cd apk-cache
./build.sh
```

构建脚本会自动：
- 检测系统中可用的HTML压缩工具
- 压缩管理界面的HTML文件
- 使用最高压缩率生成gzip版本
- 执行优化的Go构建

**注意**：直接使用 `go build` 会失败，因为缺少预压缩的HTML文件。

### 运行

```bash
# 默认配置运行
./apk-cache

# 使用配置文件
./apk-cache -config config.toml

# 自定义配置
./apk-cache -addr :3142 -cache ./cache -proxy socks5://127.0.0.1:1080
```

## 配置 Alpine Linux 使用缓存服务器

编辑 `/etc/apk/repositories`:

```bash
sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories
```

或在 Dockerfile 中使用:

```dockerfile
FROM alpine:3.22

# 配置使用 APK 缓存服务器
RUN sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories

# 安装软件包（将使用缓存）
RUN apk update && apk add --no-cache curl wget git
```

## 配置 Debian/Ubuntu 使用 APT 缓存服务器

APT 代理功能需要通过 HTTP 代理方式使用，不支持直接 URL 访问。

### 配置 APT 使用 HTTP 代理

方法一：创建代理配置文件

```bash
echo 'Acquire::HTTP::Proxy "http://your-cache-server:3142";
Acquire::HTTPS::Proxy "http://your-cache-server:3142";' > /etc/apt/apt.conf.d/01proxy
```

方法二：编辑现有配置文件

编辑 `/etc/apt/apt.conf.d/95proxies`：

```bash
Acquire::HTTP::Proxy "http://your-cache-server:3142";
Acquire::HTTPS::Proxy "http://your-cache-server:3142";
```

## 主要配置参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `:3142` | 监听地址 |
| `-cache` | `./cache` | 缓存目录路径 |
| `-upstream` | `https://dl-cdn.alpinelinux.org` | 上游服务器地址 |
| `-proxy` | (空) | 代理地址（支持 SOCKS5/HTTP 协议） |
| `-index-cache` | `24h` | 索引文件缓存时间 |
| `-pkg-cache` | `0` | 包文件缓存时间（0 = 永不过期） |
| `-cleanup-interval` | `1h` | 自动清理间隔（0 = 禁用） |
| `-locale` | (空) | 语言设置 (en/zh)，留空自动检测 |
| `-admin-user` | `admin` | 管理界面用户名 |
| `-admin-password` | (空) | 管理界面密码（留空则无需认证） |
| `-config` | (空) | 配置文件路径（可选） |
| `-proxy-auth` | `false` | 启用代理身份验证 |
| `-proxy-user` | `proxy` | 代理身份验证用户名 |
| `-proxy-password` | (空) | 代理身份验证密码（留空则无需认证） |
| `-proxy-auth-exempt-ips` | (空) | 不需要验证的 IP 网段（CIDR格式，逗号分隔） |
| `-trusted-reverse-proxy-ips` | (空) | 信任的反向代理 IP（逗号分隔） |
| `-cache-max-size` | (空) | 最大缓存大小（如 `10GB`, `1TB`） |
| `-cache-clean-strategy` | `LRU` | 缓存清理策略 (`LRU`/`LFU`/`FIFO`) |
| `-memory-cache` | `false` | 启用内存缓存 |
| `-memory-cache-size` | `100MB` | 内存缓存大小 |
| `-memory-cache-max-items` | `1000` | 内存缓存最大项目数 |
| `-memory-cache-ttl` | `30m` | 内存缓存项过期时间 |
| `-memory-cache-max-file-size` | `10MB` | 单个文件最大缓存大小 |
| `-health-check-interval` | `30s` | 健康检查间隔 |
| `-health-check-timeout` | `10s` | 健康检查超时时间 |
| `-enable-self-healing` | `true` | 启用自愈机制 |
| `-rate-limit` | `false` | 启用请求限流 |
| `-rate-limit-rate` | `100` | 限流速率（每秒请求数） |
| `-rate-limit-burst` | `200` | 限流突发容量 |
| `-rate-limit-exempt-paths` | `/_health` | 豁免限流的路径（逗号分隔） |
| `-data-integrity-check-interval` | `1h` | 数据完整性检查间隔（0 = 禁用） |
| `-data-integrity-auto-repair` | `true` | 启用损坏文件自动修复 |
| `-data-integrity-periodic-check` | `true` | 启用定期数据完整性检查 |
| `-data-integrity-initialize-existing-files` | `false` | 启动时初始化现有文件的哈希记录 |

## 配置文件示例

完整的配置示例请参考 [`config.example.toml`](config.example.toml) 文件。

创建 `config.toml` 并参考示例文件进行配置：

```bash
# 复制示例配置文件
cp config.example.toml config.toml

# 编辑配置文件
vim config.toml
```

主要配置节包括：
- `[server]` - 服务器基本配置
- `[[upstreams]]` - 上游服务器列表（支持多个）
- `[cache]` - 缓存配置
- `[security]` - 安全配置（身份验证等）
- `[health_check]` - 健康检查配置
- `[rate_limit]` - 请求限流配置
- `[data_integrity]` - 数据完整性校验配置

## Docker Compose 示例

```yaml
version: '3.8'
services:
  apk-cache:
    image: tursom/apk-cache:latest
    ports:
      - "3142:3142"
    volumes:
      - ./cache:/app/cache
    environment:
      - ADDR=:3142
      - CACHE_DIR=/app/cache
      - INDEX_CACHE=24h
      - MEMORY_CACHE_ENABLED=true
      - MEMORY_CACHE_SIZE=100MB
      - HEALTH_CHECK_INTERVAL=30s
      - ENABLE_SELF_HEALING=true
      - RATE_LIMIT_ENABLED=true
      - RATE_LIMIT_RATE=100
      - RATE_LIMIT_BURST=200
      - RATE_LIMIT_EXEMPT_PATHS=/_health
      - DATA_INTEGRITY_CHECK_INTERVAL=1h
      - DATA_INTEGRITY_AUTO_REPAIR=true
      - DATA_INTEGRITY_PERIODIC_CHECK=true
    restart: unless-stopped
```

## 管理界面

访问 `http://your-server:3142/_admin/` 查看：

- 实时统计数据（缓存命中率、下载量等）
- 缓存总大小和文件数量
- 一键清空缓存功能
- Prometheus 指标链接

## 监控

访问 `http://your-server:3142/metrics` 获取 Prometheus 指标：

### 缓存性能指标
- `apk_cache_hits_total` - 缓存命中次数
- `apk_cache_misses_total` - 缓存未命中次数
- `apk_cache_download_bytes_total` - 下载总字节数

### 内存缓存指标
- `apk_cache_memory_hits_total` - 内存缓存命中次数
- `apk_cache_memory_misses_total` - 内存缓存未命中次数
- `apk_cache_memory_size_bytes` - 内存缓存当前大小
- `apk_cache_memory_items_total` - 内存缓存项数量
- `apk_cache_memory_evictions_total` - 内存缓存淘汰次数

### 健康检查指标
- `apk_cache_health_status` - 组件健康状态（1=健康，0=不健康）
  - `component="upstream"` - 上游服务器健康状态
  - `component="filesystem"` - 文件系统健康状态
  - `component="memory_cache"` - 内存缓存健康状态
  - `component="cache_quota"` - 缓存配额健康状态
- `apk_cache_health_check_duration_seconds` - 健康检查耗时
  - `component="upstream"` - 上游服务器检查耗时
  - `component="filesystem"` - 文件系统检查耗时
  - `component="memory_cache"` - 内存缓存检查耗时
  - `component="cache_quota"` - 缓存配额检查耗时
- `apk_cache_health_check_errors_total` - 健康检查错误次数
  - `component="upstream"` - 上游服务器检查错误
  - `component="filesystem"` - 文件系统检查错误
  - `component="memory_cache"` - 内存缓存检查错误
  - `component="cache_quota"` - 缓存配额检查错误

### 上游服务器指标
- `apk_cache_upstream_healthy_count` - 健康上游服务器数量
- `apk_cache_upstream_total_count` - 总上游服务器数量
- `apk_cache_upstream_failover_count` - 故障转移次数

### 请求限流指标
- `apk_cache_rate_limit_allowed_total` - 允许通过的请求数量
- `apk_cache_rate_limit_rejected_total` - 被拒绝的请求数量
- `apk_cache_rate_limit_tokens_current` - 当前令牌数量

### 数据完整性校验指标
- `apk_cache_data_integrity_checks_total` - 数据完整性检查次数
- `apk_cache_data_integrity_corrupted_files_total` - 损坏文件数量
- `apk_cache_data_integrity_repaired_files_total` - 数据完整性修复次数
- `apk_cache_data_integrity_check_duration_seconds` - 数据完整性检查耗时

## 健康检查和自愈机制

### 工作原理

APK Cache 实现了完整的健康检查和自愈机制，确保服务的高可用性：

#### 1. 健康检查组件

**上游服务器健康检查**：
- 定期检查所有上游服务器的可用性
- 使用 HEAD 请求测试多个路径（根目录、Alpine 镜像目录、索引文件等）
- 支持故障转移，自动切换到健康的上游服务器
- 可配置的检查间隔和超时时间

**文件系统健康检查**：
- 检查缓存目录是否存在且可写
- 验证磁盘空间使用情况
- 自动修复目录权限问题

**内存缓存健康检查**：
- 监控内存使用率和缓存项数量
- 检测内存缓存是否接近容量上限
- 自动清理过期缓存项

**缓存配额健康检查**：
- 监控磁盘缓存使用情况
- 预警缓存配额接近上限

**数据完整性健康检查**：
- 定期验证缓存文件的完整性
- 检测损坏或篡改的文件
- 监控校验和验证状态

#### 2. 自愈机制

当检测到问题时，系统会自动尝试修复：

**上游服务器自愈**：
- 自动重试连接失败的上游服务器
- 重置健康状态计数器
- 支持故障服务器自动恢复

**文件系统自愈**：
- 自动修复缓存目录权限
- 重新创建必要的子目录结构
- 清理损坏的临时文件

**内存缓存自愈**：
- 自动清理过期缓存项
- 重置内存缓存统计信息

**数据完整性自愈**：
- 自动修复损坏的缓存文件
- 重新下载校验和验证失败的文件
- 清理无法修复的损坏文件

## 故障排除

### 常见问题

**缓存未命中**：检查缓存目录权限和磁盘空间

**代理连接失败**：验证代理地址格式和可用性（支持 SOCKS5/HTTP 协议）

**管理界面无法访问**：确保正确访问 `/_admin/` 路径

**健康检查失败**：检查上游服务器可达性和网络连接

**数据完整性错误**：检查磁盘空间和文件系统完整性

## 许可证

GPLv3 License

## 开发路线图

查看 [ROADMAP.md](ROADMAP.md) 了解项目的未来发展方向和改进计划。

## 链接

- GitHub: https://github.com/tursom/apk-cache
- Docker Hub: https://hub.docker.com/r/tursom/apk-cache
- Issue Tracker: https://github.com/tursom/apk-cache/issues
