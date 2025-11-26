# APK Cache - Alpine Linux Package Cache Server

[![Docker Pulls](https://img.shields.io/docker/pulls/tursom/apk-cache)](https://hub.docker.com/r/tursom/apk-cache)
[![Docker Image Size](https://img.shields.io/docker/image-size/tursom/apk-cache)](https://hub.docker.com/r/tursom/apk-cache)

A high-performance proxy server for caching Alpine Linux APK packages, featuring memory caching, health checks, self-healing capabilities, and APT package caching support.

## 🚀 Quick Start

### Run with Docker

```bash
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v ./cache:/app/cache \
  tursom/apk-cache:latest
```

### Using Docker Compose

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
      - HEALTH_CHECK_INTERVAL=30s
      - ENABLE_SELF_HEALING=true
    restart: unless-stopped
```

## 📋 Features

- 🚀 **Automatic Caching** - Cache Alpine Linux APK packages automatically
- 📦 **Three-Tier Cache** - Memory → File → Upstream caching architecture
- 🌐 **Proxy Support** - SOCKS5/HTTP proxy for upstream access
- 📦 **APT Package Caching** - Support for Debian/Ubuntu APT package caching
- 🔄 **HTTP/HTTPS Proxy** - HTTP/HTTPS proxy functionality with caching support
- 🩺 **Health Checks** - Automatic upstream server monitoring
- 🔄 **Self-Healing** - Automatic recovery from failures
- 📊 **Monitoring** - Prometheus metrics and web dashboard
- 💾 **Cache Quota** - Configurable cache size limits
- 🔒 **File Locking** - Prevent concurrent download conflicts
- 🚦 **Rate Limiting** - Token bucket algorithm for request limiting
- 🔍 **Data Integrity** - File checksum validation and automatic repair

## ⚙️ Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ADDR` | `:3142` | Listening address |
| `CACHE_DIR` | `./cache` | Cache directory path |
| `UPSTREAM` | `https://dl-cdn.alpinelinux.org` | Upstream server URL |
| `PROXY` | (empty) | Proxy address (SOCKS5/HTTP) |
| `INDEX_CACHE` | `24h` | Index file cache duration |
| `PKG_CACHE` | `0` | Package cache duration (0 = never expire) |
| `CACHE_MAX_SIZE` | (empty) | Maximum cache size (e.g., `10GB`, `1TB`) |
| `MEMORY_CACHE_ENABLED` | `false` | Enable memory cache |
| `MEMORY_CACHE_SIZE` | `100MB` | Memory cache size |
| `MEMORY_CACHE_MAX_ITEMS` | `1000` | Maximum items in memory cache |
| `HEALTH_CHECK_INTERVAL` | `30s` | Health check interval |
| `ENABLE_SELF_HEALING` | `true` | Enable self-healing mechanisms |
| `RATE_LIMIT_ENABLED` | `false` | Enable request rate limiting |
| `RATE_LIMIT_RATE` | `100` | Rate limit (requests per second) |
| `RATE_LIMIT_BURST` | `200` | Rate limit burst capacity |
| `RATE_LIMIT_EXEMPT_PATHS` | `/_health` | Paths exempt from rate limiting |
| `DATA_INTEGRITY_CHECK_INTERVAL` | `1h` | Data integrity check interval (0 = disabled) |
| `DATA_INTEGRITY_AUTO_REPAIR` | `true` | Enable automatic repair of corrupted files |
| `DATA_INTEGRITY_PERIODIC_CHECK` | `true` | Enable periodic data integrity checks |

### Configure Alpine Linux

Edit `/etc/apk/repositories`:

```bash
sed -i 's|https://dl-cdn.alpinelinux.org|http://your-cache-server:3142|g' /etc/apk/repositories
```

Or in Dockerfile:

```dockerfile
FROM alpine:3.22

# Configure to use APK cache server
RUN sed -i 's|https://dl-cdn.alpinelinux.org|http://your-cache-server:3142|g' /etc/apk/repositories

# Install packages (will use cache)
RUN apk update && apk add --no-cache curl wget git
```

### Configure Debian/Ubuntu for APT Caching

APT proxy functionality must be used through HTTP proxy mode and does not support direct URL access.

Create APT proxy configuration:

```bash
echo 'Acquire::HTTP::Proxy "http://your-cache-server:3142";
Acquire::HTTPS::Proxy "http://your-cache-server:3142";' > /etc/apt/apt.conf.d/01proxy
```

## 📊 Monitoring

### Web Dashboard

Access the management interface at:
```
http://your-server:3142/_admin/
```

### Prometheus Metrics

Access metrics at:
```
http://your-server:3142/metrics
```

Key metrics include:
- `apk_cache_hits_total` - Cache hit count
- `apk_cache_misses_total` - Cache miss count
- `apk_cache_health_status` - Component health status
- `apk_cache_memory_hits_total` - Memory cache hit count
- `apk_cache_rate_limit_allowed_total` - Allowed requests count
- `apk_cache_rate_limit_rejected_total` - Rejected requests count
- `apk_cache_data_integrity_checks_total` - Data integrity check count
- `apk_cache_data_integrity_corrupted_files_total` - Corrupted files count
- `apk_cache_data_integrity_repaired_files_total` - Data integrity repair count

## 🔧 Advanced Configuration

### Using Configuration File

Create `config.toml`:

```toml
[server]
addr = ":3142"
locale = "en"

[[upstreams]]
name = "Official CDN"
url = "https://dl-cdn.alpinelinux.org"

[cache]
dir = "./cache"
index_duration = "24h"
pkg_duration = "168h"  # 7 days
max_size = "10GB"
clean_strategy = "LRU"

[memory_cache]
enabled = true
max_size = "100MB"
ttl = "30m"
max_file_size = "10MB"

[health_check]
interval = "30s"
timeout = "10s"
enable_self_healing = true

[rate_limit]
enabled = true
rate = 100
burst = 200
exempt_paths = ["/_health", "/metrics"]

[data_integrity]
check_interval = "1h"
auto_repair = true
periodic_check = true
```

Mount and use the configuration file:

```bash
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v ./cache:/app/cache \
  -v ./config.toml:/app/config.toml \
  -e CONFIG=/app/config.toml \
  tursom/apk-cache:latest
```

## 🔍 Health Check & Self-Healing

The container includes comprehensive health monitoring:

- **Upstream Health Checks**: Automatically detects and avoids unhealthy upstream servers
- **Filesystem Monitoring**: Verifies cache directory permissions and disk space
- **Memory Cache Health**: Monitors memory usage and automatically cleans expired items
- **Data Integrity Monitoring**: Validates file checksums and detects corruption
- **Self-Healing**: Automatically repairs common issues like directory permissions and corrupted files
- **Rate Limiting**: Token bucket algorithm prevents abuse and ensures service stability

## 📈 Performance & Reliability

- **Memory Cache**: Reduces disk I/O for frequently accessed small files
- **Concurrent Safety**: File-level locking prevents download conflicts
- **Smart Caching**: Configurable cache durations and cleanup strategies
- **Failover Support**: Automatic switch to healthy upstream servers
- **Data Integrity**: SHA-256 checksum validation ensures file reliability
- **Self-Healing**: Automatic repair of corrupted files and system issues

## 🔗 Links

- **GitHub**: https://github.com/tursom/apk-cache
- **Documentation**: See [README.md](https://github.com/tursom/apk-cache/blob/main/README.md) for detailed documentation
- **Issues**: https://github.com/tursom/apk-cache/issues

## 📄 License

GPLv3 License