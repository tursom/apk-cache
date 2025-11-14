# APK Cache

[English](README_EN.md) | 简体中文

一个用于缓存 Alpine Linux APK 包的代理服务器，支持 SOCKS5/HTTP 代理和多语言界面。

## 功能特性

- 🚀 自动缓存 Alpine Linux APK 包
- 📦 缓存命中时直接从本地提供服务
- 🔄 缓存未命中时从上游服务器获取并保存
- 🌐 支持 SOCKS5/HTTP 代理访问上游服务器
- 💾 可配置的缓存目录和监听地址
- ⏱️ 灵活的缓存过期策略
- 🧹 自动清理过期缓存
- 🔒 文件级锁管理，避免并发下载冲突
- 🌍 多语言支持（中文/英文）
- 📊 Prometheus 监控指标
- 🎛️ Web 管理界面
- 💰 缓存配额管理（支持 LRU/LFU/FIFO 清理策略）

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

```bash
git clone https://github.com/tursom/apk-cache.git
cd apk-cache
go build -o apk-cache ./cmd/apk-cache
```

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

## 主要配置参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `:3142` | 监听地址 |
| `-cache` | `./cache` | 缓存目录路径 |
| `-upstream` | `https://dl-cdn.alpinelinux.org` | 上游服务器地址 |
| `-proxy` | (空) | 代理地址（支持 SOCKS5/HTTP 协议） |
| `-index-cache` | `24h` | 索引文件缓存时间 |
| `-pkg-cache` | `0` | 包文件缓存时间（0 = 永不过期） |
| `-cache-max-size` | (空) | 最大缓存大小（如 `10GB`, `1TB`） |
| `-cache-clean-strategy` | `LRU` | 缓存清理策略 (`LRU`/`LFU`/`FIFO`) |

## 配置文件示例

创建 `config.toml`：

```toml
[server]
addr = ":3142"
locale = "zh"

# 上游服务器列表（支持故障转移）
[[upstreams]]
name = "Official CDN"
url = "https://dl-cdn.alpinelinux.org"
# proxy = "socks5://127.0.0.1:1080"  # 或 "http://127.0.0.1:8080"

[cache]
dir = "./cache"
index_duration = "24h"
pkg_duration = "168h"  # 7 天
cleanup_interval = "1h"
max_size = "10GB"      # 最大缓存大小
clean_strategy = "LRU" # 清理策略 (`LRU`/`LFU`/`FIFO`)

[security]
# admin_user = "admin" # 管理界面用户名（默认：admin）
# admin_password = "your-secret-password"  # 管理界面密码
```

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

- `apk_cache_hits_total` - 缓存命中次数
- `apk_cache_misses_total` - 缓存未命中次数
- `apk_cache_download_bytes_total` - 下载总字节数

## 故障排除

### 常见问题

**缓存未命中**：检查缓存目录权限和磁盘空间

**代理连接失败**：验证代理地址格式和可用性（支持 SOCKS5/HTTP 协议）

**管理界面无法访问**：确保正确访问 `/_admin/` 路径

## 许可证

GPLv3 License

## 链接

- GitHub: https://github.com/tursom/apk-cache
- Docker Hub: https://hub.docker.com/r/tursom/apk-cache
- Issue Tracker: https://github.com/tursom/apk-cache/issues
