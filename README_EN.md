# APK Cache

English | [简体中文](README.md)

A proxy server for caching Alpine Linux APK packages, supporting SOCKS5/HTTP proxy and multi-language interface.

## Features

- 🚀 Automatic caching of Alpine Linux APK packages
- 📦 Serve directly from local cache on cache hits
- 🔄 Fetch from upstream servers and save on cache misses
- 🌐 Support SOCKS5/HTTP proxy for upstream access
- 💾 Configurable cache directory and listening address
- ⏱️ Flexible cache expiration policies
- 🧹 Automatic cleanup of expired cache
- 🔒 File-level lock management to avoid concurrent download conflicts
- 🌍 Multi-language support (Chinese/English)
- 📊 Prometheus monitoring metrics
- 🎛️ Web management interface
- 💰 Cache quota management (supports LRU/LFU/FIFO cleanup strategies)
- 🚀 **Memory Cache Layer**: Three-tier caching architecture (memory → file → upstream)
- 🩺 **Health Check**: Upstream server status monitoring and self-healing mechanisms
- 🚦 **Request Rate Limiting**: Token bucket algorithm for request frequency limiting

## Quick Start

### Using Docker (Recommended)

```bash
# Pull and run
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v ./cache:/app/cache \
  tursom/apk-cache:latest
```

Visit http://localhost:3142/_admin/ to view the management interface.

### Build from Source

```bash
git clone https://github.com/tursom/apk-cache.git
cd apk-cache
go build -o apk-cache ./cmd/apk-cache
```

### Run

```bash
# Run with default configuration
./apk-cache

# Use configuration file
./apk-cache -config config.toml

# Custom configuration
./apk-cache -addr :3142 -cache ./cache -proxy socks5://127.0.0.1:1080
```

## Configure Alpine Linux to Use Cache Server

Edit `/etc/apk/repositories`:

```bash
sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories
```

Or use in Dockerfile:

```dockerfile
FROM alpine:3.22

# Configure to use APK cache server
RUN sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories

# Install packages (will use cache)
RUN apk update && apk add --no-cache curl wget git
```

## Main Configuration Parameters

| Parameter | Default Value | Description |
|-----------|---------------|-------------|
| `-addr` | `:3142` | Listening address |
| `-cache` | `./cache` | Cache directory path |
| `-upstream` | `https://dl-cdn.alpinelinux.org` | Upstream server address |
| `-proxy` | (empty) | Proxy address (supports SOCKS5/HTTP protocols) |
| `-index-cache` | `24h` | Index file cache duration |
| `-pkg-cache` | `0` | Package file cache duration (0 = never expire) |
| `-cache-max-size` | (empty) | Maximum cache size (e.g., `10GB`, `1TB`) |
| `-cache-clean-strategy` | `LRU` | Cache cleanup strategy (`LRU`/`LFU`/`FIFO`) |
| `-memory-cache` | `false` | Enable memory cache |
| `-memory-cache-size` | `100MB` | Memory cache size |
| `-memory-cache-max-items` | `1000` | Maximum number of items in memory cache |
| `-memory-cache-ttl` | `30m` | Memory cache item expiration time |
| `-memory-cache-max-file-size` | `10MB` | Maximum file size for memory caching |
| `-health-check-interval` | `30s` | Health check interval |
| `-health-check-timeout` | `10s` | Health check timeout |
| `-enable-self-healing` | `true` | Enable self-healing mechanisms |
| `-rate-limit` | `false` | Enable request rate limiting |
| `-rate-limit-rate` | `100` | Rate limit (requests per second) |
| `-rate-limit-burst` | `200` | Rate limit burst capacity |
| `-rate-limit-exempt-paths` | `/_health` | Paths exempt from rate limiting (comma-separated) |

## Configuration File Example

Create `config.toml`:

```toml
[server]
addr = ":3142"
locale = "en"

# Upstream servers list (supports failover)
[[upstreams]]
name = "Official CDN"
url = "https://dl-cdn.alpinelinux.org"
# proxy = "socks5://127.0.0.1:1080"  # or "http://127.0.0.1:8080"

[cache]
dir = "./cache"
index_duration = "24h"
pkg_duration = "168h"  # 7 days
cleanup_interval = "1h"
max_size = "10GB"      # Maximum cache size
clean_strategy = "LRU" # Cleanup strategy (`LRU`/`LFU`/`FIFO`)

# Memory cache configuration
[memory_cache]
enabled = true
max_size = "100MB"     # Maximum memory cache size
max_items = 1000       # Maximum number of items in memory cache
ttl = "30m"            # Memory cache item expiration time
max_file_size = "10MB" # Maximum file size for memory caching

# Health check configuration
[health_check]
interval = "30s"       # Health check interval
timeout = "10s"        # Health check timeout
enable_self_healing = true  # Enable self-healing mechanisms

# Request rate limiting configuration
[rate_limit]
enabled = false        # Enable request rate limiting
rate = 100             # Rate limit (requests per second)
burst = 200            # Rate limit burst capacity
exempt_paths = ["/_health"]  # Paths exempt from rate limiting

[security]
# admin_user = "admin" # Management interface username (default: admin)
# admin_password = "your-secret-password"  # Management interface password
```

## Docker Compose Example

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
    restart: unless-stopped
```

## Management Interface

Visit `http://your-server:3142/_admin/` to view:

- Real-time statistics (cache hit rate, download volume, etc.)
- Total cache size and file count
- One-click cache clearing function
- Prometheus metrics link

## Monitoring

Visit `http://your-server:3142/metrics` to get Prometheus metrics:

### Cache Performance Metrics
- `apk_cache_hits_total` - Cache hit count
- `apk_cache_misses_total` - Cache miss count
- `apk_cache_download_bytes_total` - Total download bytes

### Memory Cache Metrics
- `apk_cache_memory_hits_total` - Memory cache hit count
- `apk_cache_memory_misses_total` - Memory cache miss count
- `apk_cache_memory_size_bytes` - Current memory cache size
- `apk_cache_memory_items_total` - Memory cache item count
- `apk_cache_memory_evictions_total` - Memory cache eviction count

### Health Check Metrics
- `apk_cache_health_status` - Component health status (1=healthy, 0=unhealthy)
  - `component="upstream"` - Upstream server health status
  - `component="filesystem"` - Filesystem health status
  - `component="memory_cache"` - Memory cache health status
  - `component="cache_quota"` - Cache quota health status
- `apk_cache_health_check_duration_seconds` - Health check duration
  - `component="upstream"` - Upstream server check duration
  - `component="filesystem"` - Filesystem check duration
  - `component="memory_cache"` - Memory cache check duration
  - `component="cache_quota"` - Cache quota check duration
- `apk_cache_health_check_errors_total` - Health check error count
  - `component="upstream"` - Upstream server check errors
  - `component="filesystem"` - Filesystem check errors
  - `component="memory_cache"` - Memory cache check errors
  - `component="cache_quota"` - Cache quota check errors

### Upstream Server Metrics
- `apk_cache_upstream_healthy_count` - Number of healthy upstream servers
- `apk_cache_upstream_total_count` - Total number of upstream servers
- `apk_cache_upstream_failover_count` - Number of failover events

### Request Rate Limiting Metrics
- `apk_cache_rate_limit_allowed_total` - Number of allowed requests
- `apk_cache_rate_limit_rejected_total` - Number of rejected requests
- `apk_cache_rate_limit_tokens_current` - Current token count

## Health Check and Self-Healing Mechanism

### How It Works

APK Cache implements a comprehensive health check and self-healing mechanism to ensure high service availability:

#### 1. Health Check Components

**Upstream Server Health Check**:
- Periodically checks availability of all upstream servers
- Tests multiple paths using HEAD requests (root directory, Alpine mirror directories, index files, etc.)
- Supports failover, automatically switching to healthy upstream servers
- Configurable check interval and timeout

**Filesystem Health Check**:
- Verifies cache directory existence and writability
- Monitors disk space usage
- Automatically repairs directory permission issues

**Memory Cache Health Check**:
- Monitors memory usage and cache item count
- Detects when memory cache approaches capacity limits
- Automatically cleans up expired cache items

**Cache Quota Health Check**:
- Monitors disk cache usage
- Alerts when cache quota approaches limits

#### 2. Self-Healing Mechanism

When issues are detected, the system automatically attempts repairs:

**Upstream Server Self-Healing**:
- Automatically retries connections to failed upstream servers
- Resets health status counters
- Supports automatic recovery of failed servers

**Filesystem Self-Healing**:
- Automatically repairs cache directory permissions
- Recreates necessary subdirectory structures
- Cleans up corrupted temporary files

**Memory Cache Self-Healing**:
- Automatically cleans up expired cache items
- Resets memory cache statistics

## Troubleshooting

### Common Issues

**Cache Miss**: Check cache directory permissions and disk space

**Proxy Connection Failed**: Verify proxy address format and availability (supports SOCKS5/HTTP protocols)

**Management Interface Unreachable**: Ensure correct access to `/_admin/` path

**Health Check Failed**: Check upstream server reachability and network connectivity

## License

GPLv3 License

## Development Roadmap

See [ROADMAP.md](ROADMAP.md) for future development directions and improvement plans.

## Links

- GitHub: https://github.com/tursom/apk-cache
- Docker Hub: https://hub.docker.com/r/tursom/apk-cache
- Issue Tracker: https://github.com/tursom/apk-cache/issues
