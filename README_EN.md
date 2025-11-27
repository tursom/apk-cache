# APK Cache

English | [简体中文](README.md)

A proxy server for caching Alpine Linux APK packages, supporting SOCKS5/HTTP proxy, APT package caching, HTTP/HTTPS proxy and multi-language interface.

## Features

- 🚀 **Automatic Caching** - Automatic caching of Alpine Linux APK packages
- 📦 **Three-Tier Cache** - Memory → File → Upstream caching architecture
- 🔄 **Smart Caching** - Serve directly from local cache on cache hits, fetch from upstream on misses
- 🌐 **Proxy Support** - Support SOCKS5/HTTP proxy for upstream access
- 📦 **APT Package Caching** - Support for Debian/Ubuntu APT package caching
- 🔄 **HTTP/HTTPS Proxy** - Support for HTTP/HTTPS proxy functionality, can cache APT and APK packages
- 💾 **Flexible Configuration** - Configurable cache directory, listening address and caching strategies
- ⏱️ **Expiration Policies** - Flexible cache expiration times and automatic cleanup mechanisms
- 🧹 **Automatic Cleanup** - Automatic cleanup of expired cache and disk space management
- 🔒 **Concurrent Safety** - File-level lock management to avoid concurrent download conflicts
- 🌍 **Multi-language Interface** - Support for Chinese/English interface and error messages
- 📊 **Monitoring Metrics** - Prometheus monitoring metrics and real-time statistics
- 🎛️ **Web Management Interface** - Modern management dashboard
- 💰 **Cache Quota** - Cache quota management (supports LRU/LFU/FIFO cleanup strategies)
- 🚀 **Memory Cache** - High-performance memory cache layer, reducing disk I/O
- 🩺 **Health Check** - Upstream server status monitoring and self-healing mechanisms
- 🚦 **Request Rate Limiting** - Token bucket algorithm for request frequency limiting
- 🔍 **Data Integrity** - SHA-256 file checksum validation and automatic repair
- 🔐 **Authentication** - Support for proxy authentication and management interface authentication
- 📈 **Failover Support** - Multiple upstream servers support and automatic failover
- 🛡️ **Security Enhancements** - IP whitelisting, reverse proxy support and path security validation

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

**You must use the build script** because pre-compressed HTML files are required for the management interface:

```bash
git clone https://github.com/tursom/apk-cache.git
cd apk-cache
./build.sh
```

The build script automatically:
- Detects available HTML compression tools in the system
- Compresses the management interface HTML files
- Generates gzip versions with maximum compression ratio
- Executes optimized Go build

**Note**: Direct use of `go build` will fail due to missing pre-compressed HTML files.

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

## Configure Debian/Ubuntu to Use APT Cache Server

APT proxy functionality must be used through HTTP proxy mode and does not support direct URL access.

### Configure APT to Use HTTP Proxy

Method 1: Create proxy configuration file

```bash
echo 'Acquire::HTTP::Proxy "http://your-cache-server:3142";
Acquire::HTTPS::Proxy "http://your-cache-server:3142";' > /etc/apt/apt.conf.d/01proxy
```

Method 2: Edit existing configuration file

Edit `/etc/apt/apt.conf.d/95proxies`:

```bash
Acquire::HTTP::Proxy "http://your-cache-server:3142";
Acquire::HTTPS::Proxy "http://your-cache-server:3142";
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
| `-cleanup-interval` | `1h` | Automatic cleanup interval (0 = disabled) |
| `-locale` | (empty) | Language (en/zh), auto-detect if empty |
| `-admin-user` | `admin` | Admin dashboard username |
| `-admin-password` | (empty) | Admin dashboard password (empty = no auth) |
| `-config` | (empty) | Config file path (optional) |
| `-proxy-auth` | `false` | Enable proxy authentication |
| `-proxy-user` | `proxy` | Proxy authentication username |
| `-proxy-password` | (empty) | Proxy authentication password (empty = no auth) |
| `-proxy-auth-exempt-ips` | (empty) | Comma-separated list of IP ranges exempt from proxy auth (CIDR format) |
| `-trusted-reverse-proxy-ips` | (empty) | Comma-separated list of trusted reverse proxy IPs |
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
| `-data-integrity-check-interval` | `1h` | Data integrity check interval (0 = disabled) |
| `-data-integrity-auto-repair` | `true` | Enable automatic repair of corrupted files |
| `-data-integrity-periodic-check` | `true` | Enable periodic data integrity checks |
| `-data-integrity-initialize-existing-files` | `false` | Initialize existing files hash records on startup |

## Configuration File Example

For a complete configuration example, please refer to the [`config.example.toml`](config.example.toml) file.

Create `config.toml` and refer to the example file for configuration:

```bash
# Copy example configuration file
cp config.example.toml config.toml

# Edit configuration file
vim config.toml
```

Main configuration sections include:
- `[server]` - Server basic configuration
- `[[upstreams]]` - Upstream servers list (supports multiple)
- `[cache]` - Cache configuration
- `[security]` - Security configuration (authentication, etc.)
- `[health_check]` - Health check configuration
- `[rate_limit]` - Request rate limiting configuration
- `[data_integrity]` - Data integrity verification configuration

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
      - DATA_INTEGRITY_CHECK_INTERVAL=1h
      - DATA_INTEGRITY_AUTO_REPAIR=true
      - DATA_INTEGRITY_PERIODIC_CHECK=true
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

### Data Integrity Verification Metrics
- `apk_cache_data_integrity_checks_total` - Number of data integrity checks
- `apk_cache_data_integrity_corrupted_files_total` - Number of corrupted files
- `apk_cache_data_integrity_repaired_files_total` - Number of data integrity repairs
- `apk_cache_data_integrity_check_duration_seconds` - Data integrity check duration

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

**Data Integrity Health Check**:
- Periodically verifies cache file integrity
- Detects corrupted or tampered files
- Monitors checksum verification status

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

**Data Integrity Self-Healing**:
- Automatically repairs corrupted cache files
- Re-downloads files with failed checksum verification
- Cleans up files that cannot be repaired

## Troubleshooting

### Common Issues

**Cache Miss**: Check cache directory permissions and disk space

**Proxy Connection Failed**: Verify proxy address format and availability (supports SOCKS5/HTTP protocols)

**Management Interface Unreachable**: Ensure correct access to `/_admin/` path

**Health Check Failed**: Check upstream server reachability and network connectivity

**Data Integrity Errors**: Check disk space and filesystem integrity

## License

GPLv3 License

## Development Roadmap

See [ROADMAP.md](ROADMAP.md) for future development directions and improvement plans.

## Links

- GitHub: https://github.com/tursom/apk-cache
- Docker Hub: https://hub.docker.com/r/tursom/apk-cache
- Issue Tracker: https://github.com/tursom/apk-cache/issues
