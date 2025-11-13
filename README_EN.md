# APK Cache

English | [简体中文](README.md)

A caching proxy server for Alpine Linux APK packages with SOCKS5 proxy support and multilingual interface.

## Features

- 🚀 Automatically cache Alpine Linux APK packages
- 📦 Serve directly from local cache on cache hit
- 🔄 Fetch from upstream server and save on cache miss
- 🌐 SOCKS5 proxy support for upstream server access
- 💾 Configurable cache directory and listen address
- ⏱️ Flexible cache expiration strategies
  - APKINDEX index files expire by **modification time** (default 24 hours)
  - APK package files expire by **access time** (default never expire)
  - Prioritize in-memory access time for better performance
- 🧹 Automatic expired cache cleanup (configurable interval)
- 🔒 Client disconnection doesn't affect cache file saving
- ⚡ Simultaneous writing to cache and client for improved response speed
- 🔐 File-level lock management to avoid concurrent download conflicts
- 🌍 Multilingual support (Chinese/English) with automatic system language detection
- 📊 Prometheus monitoring metrics
- 🎛️ Web admin dashboard with real-time statistics
- 🔑 Optional HTTP Basic Auth for admin interface

## Quick Start

### Using Docker (Recommended)

The fastest way is to use the official image from Docker Hub:

```bash
# Pull and run
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v ./cache:/app/cache \
  tursom/apk-cache:latest
```

Visit http://localhost:3142/_admin/ to access the admin dashboard.

### Installation

#### Build from Source

```bash
git clone https://github.com/tursom/apk-cache.git
cd apk-cache
go build -o apk-cache ./cmd/apk-cache
```

#### Using Docker

```bash
# Pull official image
docker pull tursom/apk-cache:latest

# Run container
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v ./cache:/app/cache \
  -e ADDR=:80 \
  -e CACHE_DIR=/app/cache \
  -e INDEX_CACHE=24h \
  tursom/apk-cache:latest

# With proxy
docker run -d \
  --name apk-cache \
  -p 3142:80 \
  -v ./cache:/app/cache \
  -e PROXY=socks5://127.0.0.1:1080 \
  tursom/apk-cache:latest
```

**Docker Hub**: https://hub.docker.com/r/tursom/apk-cache

### Running

```bash
# Run with default configuration (auto-detect system language)
./apk-cache

# Use config file
./apk-cache -config config.toml

# Run with custom configuration
./apk-cache -addr :3142 -cache ./cache -proxy socks5://127.0.0.1:1080

# Specify language
./apk-cache -locale zh  # Chinese
./apk-cache -locale en  # English

# Config file + command line args (command line has higher priority)
./apk-cache -config config.toml -addr :8080
```

### Command Line Arguments

| Argument | Default | Description |
|----------|---------|-------------|
| `-config` | (empty) | Config file path (optional) |
| `-addr` | `:3142` | Listen address |
| `-cache` | `./cache` | Cache directory path |
| `-upstream` | `https://dl-cdn.alpinelinux.org` | Upstream server URL |
| `-proxy` | (empty) | SOCKS5 proxy address, format: `socks5://[username:password@]host:port` |
| `-index-cache` | `24h` | APKINDEX.tar.gz index file cache duration (by modification time) |
| `-pkg-cache` | `0` | APK package file cache duration (by access time, 0 = never expire) |
| `-cleanup-interval` | `1h` | Automatic cleanup interval for expired cache (0 = disabled) |
| `-locale` | (auto-detect) | Interface language (`en`/`zh`), auto-detect from `LANG` environment variable if empty |
| `-admin-user` | `admin` | Admin dashboard username |
| `-admin-password` | (empty) | Admin dashboard password (empty = no authentication) |

## Usage

### Configure Alpine Linux to Use Cache Server

Edit `/etc/apk/repositories`:

```bash
# Replace default mirror address with cache server address
sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories
```

Or use command line directly:

```bash
# Specify cache server when installing packages
apk add --repositories-file /dev/null --repository http://your-cache-server:3142/alpine/v3.22/main <package-name>
```

### Real-World Usage Examples

#### 1. Use in Dockerfile

```dockerfile
FROM alpine:3.22

# Configure to use APK cache server
RUN sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories

# Install packages (will use cache)
RUN apk update && apk add --no-cache \
    curl \
    wget \
    git \
    build-base

# Other build steps...
```

#### 2. Use in Alpine VM

```bash
# Backup original configuration
cp /etc/apk/repositories /etc/apk/repositories.bak

# Configure cache server
cat > /etc/apk/repositories << EOF
http://your-cache-server:3142/alpine/v3.22/main
http://your-cache-server:3142/alpine/v3.22/community
EOF

# Update index and install packages
apk update
apk add docker python3 nodejs
```

#### 3. Temporary Use (without modifying config file)

```bash
# Use cache server for a single command
apk add --repositories-file /dev/null \
  --repository http://your-cache-server:3142/alpine/v3.22/main \
  --repository http://your-cache-server:3142/alpine/v3.22/community \
  nginx
```

#### 4. Verify Cache is Working

```bash
# First request (cache miss)
curl -I http://your-cache-server:3142/alpine/v3.22/main/x86_64/APKINDEX.tar.gz
# Response header should contain: X-Cache: MISS

# Second request (cache hit)
curl -I http://your-cache-server:3142/alpine/v3.22/main/x86_64/APKINDEX.tar.gz
# Response header should contain: X-Cache: HIT
```

## How It Works

1. When a client requests a package, the server first checks local cache
2. For `APKINDEX.tar.gz` index files, check if cache is expired (default 24 hours)
3. If cache hit and not expired (`X-Cache: HIT`), serve directly from local file
4. If cache miss (`X-Cache: MISS`), download from upstream server
5. Use **file-level locks** during download to avoid duplicate downloads from multiple requests
6. Simultaneously write to cache file and client response for better user experience
7. Cache file will be saved completely even if client disconnects mid-transfer
8. Downloaded files are saved to local cache directory for subsequent requests

### Cache Strategy

- **APKINDEX.tar.gz index files**: 
  - Expire by **modification time** (default 24 hours)
  - Refresh periodically to get latest package information
  - Adjustable via `-index-cache` parameter
  
- **APK package files**: 
  - Expire by **access time** (default never expire)
  - Prioritize in-memory recorded access time
  - If not in memory, read atime from file system
  - If atime unavailable, use process start time (prevents immediate cleanup after restart)
  - Set expiration time via `-pkg-cache` parameter (e.g., `168h` = 7 days)

- **Automatic cleanup**:
  - Set cleanup interval via `-cleanup-interval`
  - Only enabled when `-pkg-cache` is not 0
  - Periodically scans and deletes expired files

### Concurrency Control

- Uses file-level lock manager to avoid duplicate downloads of the same file from concurrent requests
- First request acquires lock and downloads file, subsequent requests wait for lock release and read from cache directly
- Reference counting mechanism automatically cleans up unused locks to avoid memory leaks

## Notes

- Cache directory grows with usage, recommend periodic cleanup or disk quota settings
- When using SOCKS5 proxy, ensure proxy server is accessible
- Server listens on all network interfaces by default, firewall rules recommended for production
- APKINDEX.tar.gz index files refresh periodically to get latest package information
- Index cache time recommended between 1 hour to 24 hours, adjust based on production needs
- Concurrent requests for the same file will only download once, other requests wait and share cache

## Advanced Configuration

### Web Admin Dashboard

Visit `http://your-server:3142/_admin/` to view:

- 📈 Real-time statistics (cache hit rate, download volume, etc.)
- 🔒 Active file locks count
- 📝 Tracked access time records
- 💾 Total cache size
- 🗑️ One-click cache clearing
- 📊 Prometheus metrics link

#### Enable Authentication

```bash
# Set admin dashboard password
./apk-cache -admin-password "your-secret-password"

# Access requires:
# Username: admin
# Password: your-secret-password
```

### Prometheus Monitoring

Visit `http://your-server:3142/metrics` for metrics:

- `apk_cache_hits_total` - Total cache hits
- `apk_cache_misses_total` - Total cache misses
- `apk_cache_download_bytes_total` - Total bytes downloaded from upstream

Configure Prometheus:

```yaml
scrape_configs:
  - job_name: 'apk-cache'
    static_configs:
      - targets: ['your-server:3142']
```

### Configuration File

Create `config.toml`:

```toml
[server]
addr = ":3142"
locale = "en"  # or "zh"

[upstream]
url = "https://dl-cdn.alpinelinux.org"
# proxy = "socks5://127.0.0.1:1080"

[cache]
dir = "./cache"
index_duration = "24h"
pkg_duration = "168h"  # 7 days
cleanup_interval = "1h"

[security]
admin_password = "your-secret-password"
```

Use config file:

```bash
./apk-cache -config config.toml
```

**Priority Rule**: Command line arguments > Config file > Default values

```bash
# Config file sets addr = ":3142"
# But command line argument overrides it
./apk-cache -config config.toml -addr :8080
# Finally listens on :8080
```

### Docker Deployment

#### Using Environment Variables

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

**Supported Environment Variables**:
- `ADDR` - Listen address (default `:3142`)
- `CACHE_DIR` - Cache directory (default `./cache`)
- `INDEX_CACHE` - Index cache duration (default `24h`)
- `PKG_CACHE` - Package cache duration (default `0`, never expire)
- `CLEANUP_INTERVAL` - Automatic cleanup interval (default `1h`)
- `PROXY` - Proxy address (optional)
- `UPSTREAM` - Upstream server (default `https://dl-cdn.alpinelinux.org`)
- `LOCALE` - Interface language, `en` or `zh` (optional, auto-detect by default)
- `ADMIN_USER` - Admin dashboard username (default `admin`)
- `ADMIN_PASSWORD` - Admin dashboard password (optional, no auth if empty)
- `CONFIG` - Config file path (optional)

#### Using Configuration File

```bash
# Create config file
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

# Run with mounted config file
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
      - ./config.toml:/app/config.toml  # Optional
    environment:
      - ADDR=:80
      - CACHE_DIR=/app/cache
      - INDEX_CACHE=24h
      - PKG_CACHE=168h  # 7 days
      - CLEANUP_INTERVAL=1h
      - LOCALE=en  # or zh
      - ADMIN_USER=admin
      - ADMIN_PASSWORD=your-secret-password
      # - PROXY=socks5://host.docker.internal:1080
      # - CONFIG=/app/config.toml
    restart: unless-stopped
```

**Build from Source** (Optional):

```bash
# Clone repository
git clone https://github.com/tursom/apk-cache.git
cd apk-cache

# Build image
docker build -t apk-cache:local .

# Use locally built image
docker run -d -p 3142:80 -v ./cache:/app/cache apk-cache:local
```

### Multiple Upstream Servers and Failover

Configure multiple upstream servers with automatic failover:

```toml
# config.toml
[[upstreams]]
name = "Official CDN"
url = "https://dl-cdn.alpinelinux.org"

[[upstreams]]
name = "Tsinghua Mirror"
url = "https://mirrors.tuna.tsinghua.edu.cn/alpine"
proxy = "socks5://127.0.0.1:1080"  # Optional proxy

[[upstreams]]
name = "USTC Mirror"
url = "https://mirrors.ustc.edu.cn/alpine"
proxy = "http://proxy.example.com:8080"  # Supports HTTP proxy
```

**Features**:
- ✅ Try all upstream servers in order
- ✅ Automatically switch to next server on failure
- ✅ Each server can have its own proxy configuration
- ✅ Supports both SOCKS5 and HTTP proxies
- ✅ Automatically logs which server responded successfully

**Workflow**:
1. Try first upstream server
2. If fails (network error or non-200 status), try next one
3. Continue until success or all servers exhausted
4. Log when using fallback servers

### Multilingual Support

The program automatically detects system language (via `LC_ALL`, `LC_MESSAGES`, or `LANG` environment variables):

```bash
# Use system default language
./apk-cache

# Force Chinese
./apk-cache -locale zh
LANG=zh_CN.UTF-8 ./apk-cache

# Force English
./apk-cache -locale en
LANG=en_US.UTF-8 ./apk-cache
```

### Adjust Index Cache Time

```bash
# stable version (recommended for production) - mainly security updates, infrequent updates
./apk-cache -index-cache 24h   # 1 day

# edge version (development environment) - frequent package updates
./apk-cache -index-cache 2h    # 2 hours

# High timeliness requirements
./apk-cache -index-cache 1h    # 1 hour

# Intranet environment, not sensitive to upstream server load
./apk-cache -index-cache 12h   # 12 hours
```

**Note**: Go's `time.ParseDuration` doesn't support `d` (days) unit, please use hours `h`. For example: 1 day = `24h`, 7 days = `168h`.

### Use SOCKS5 Proxy with Authentication

```bash
./apk-cache -proxy socks5://username:password@127.0.0.1:1080
```

### Custom Upstream Server

```bash
# Use Tsinghua University mirror
./apk-cache -upstream https://mirrors.tuna.tsinghua.edu.cn/alpine

# Use Alibaba Cloud mirror
./apk-cache -upstream https://mirrors.aliyun.com/alpine
```

### Cache Expiration and Auto Cleanup

```bash
# Index files expire in 24h, package files in 7 days, cleanup every hour
./apk-cache -index-cache 24h -pkg-cache 168h -cleanup-interval 1h

# Index files expire in 12h, package files in 30 days, cleanup every 6 hours
./apk-cache -index-cache 12h -pkg-cache 720h -cleanup-interval 6h

# Disable package file expiration (permanent cache)
./apk-cache -pkg-cache 0

# Set expiration but disable auto cleanup (manual cleanup via admin dashboard)
./apk-cache -pkg-cache 168h -cleanup-interval 0
```

**Note**: 
- Auto cleanup only enabled when `-pkg-cache` is not 0
- When `-cleanup-interval` is 0, auto cleanup goroutine won't start

## Performance Features

### Concurrency Safety

- **File-level lock management**: Uses custom `FileLockManager` to ensure each file is only downloaded once
- **Reference counting**: Automatically manages lock lifecycle to avoid memory leaks
- **Double-check**: Checks cache again after acquiring lock to avoid duplicate downloads

### Client-Friendly

- **Streaming transfer**: Downloads while transferring to client, no need to wait for complete download
- **Resilient caching**: Client disconnection doesn't affect cache integrity
- **Concurrent downloads**: Different files can be downloaded concurrently without interfering with each other

### Smart Access Time Tracking

- **Memory priority**: Prioritize in-memory recorded access time, avoid frequent syscalls
- **File system fallback**: Read atime from file system when not in memory
- **Process start protection**: Use process start time when atime unavailable, prevents immediate cleanup after restart
- **Auto cleanup**: Synchronize memory cleanup when deleting files

## Monitoring and Management

### Web Admin Dashboard

- **Access URL**: `http://your-server:3142/_admin/`
- **Real-time Stats**: Cache hit rate, download volume, active locks, tracked files, etc.
- **Cache Management**: View cache size, one-click cache clearing
- **Server Info**: Listen address, cache directory, upstream server, configuration parameters, etc.

### Prometheus Metrics

- **Access URL**: `http://your-server:3142/metrics`
- **Metrics List**:
  - `apk_cache_hits_total` - Total cache hits
  - `apk_cache_misses_total` - Total cache misses
  - `apk_cache_download_bytes_total` - Total bytes downloaded from upstream

### Authentication Protection

```bash
# Enable admin interface authentication
./apk-cache -admin-password "secure-password"

# Access /_admin/ requires:
# Username: admin
# Password: secure-password
```

## Project Structure

```
apk-cache/
├── cmd/
│   └── apk-cache/
│       ├── main.go            # Main program entry
│       ├── config.go          # Configuration file handling
│       ├── cache.go           # Cache handling logic
│       ├── web.go             # Web admin dashboard
│       ├── cleanup.go         # Auto cleanup functionality
│       ├── lockman.go         # File lock manager
│       ├── lockman_test.go    # Lock manager unit tests
│       ├── access_tracker.go  # Access time tracker
│       ├── admin.html         # Admin dashboard HTML (embedded)
│       └── locales/
│           ├── en.toml        # English translations (embedded)
│           └── zh.toml        # Chinese translations (embedded)
├── cache/                     # Cache directory (generated at runtime)
├── Dockerfile                 # Docker image build file
├── entrypoint.sh              # Docker container startup script
├── config.example.toml        # Configuration file example
├── go.mod
├── go.sum
├── README.md                  # Chinese documentation
├── README_EN.md               # English documentation
├── ADMIN.md                   # Admin dashboard documentation
└── LICENSE                    # GPLv3 License
```

## Dependencies

- `golang.org/x/net/proxy` - SOCKS5 proxy support
- `github.com/nicksnyder/go-i18n/v2` - Internationalization support
- `github.com/BurntSushi/toml` - TOML configuration file parsing
- `github.com/prometheus/client_golang` - Prometheus monitoring metrics
- `golang.org/x/text/language` - Language detection and handling

## Troubleshooting

### 1. Cannot Connect to Upstream Server

**Problem**: Logs show "dial tcp: lookup dl-cdn.alpinelinux.org: no such host"

**Solution**:
```bash
# Check DNS resolution
nslookup dl-cdn.alpinelinux.org

# If DNS has issues, use mirror site
./apk-cache -upstream https://mirrors.tuna.tsinghua.edu.cn/alpine

# Or configure multiple upstream servers for failover
```

### 2. Proxy Connection Failed

**Problem**: Connection timeout when using SOCKS5 proxy

**Solution**:
```bash
# Verify proxy is available
curl -x socks5://127.0.0.1:1080 https://dl-cdn.alpinelinux.org

# Check if proxy format is correct
./apk-cache -proxy socks5://username:password@host:port

# Try HTTP proxy
./apk-cache -proxy http://127.0.0.1:8080
```

### 3. Cache Miss

**Problem**: Always shows `X-Cache: MISS`, cache not working

**Solution**:
```bash
# Check cache directory permissions
ls -la ./cache

# Ensure write permission
chmod 755 ./cache

# Check disk space
df -h

# View logs for detailed errors
./apk-cache -addr :3142 2>&1 | tee apk-cache.log
```

### 4. Admin Dashboard Not Accessible

**Problem**: Accessing `/_admin/` returns 404 or authentication fails

**Solution**:
```bash
# Check if accessing correctly (note the trailing slash)
curl http://localhost:3142/_admin/

# If password is set, use Basic Auth
curl -u admin:your-password http://localhost:3142/_admin/

# When accessing in browser, use correct credentials:
# Username: admin
# Password: (your set password)
```

### 5. Auto Cleanup Not Working

**Problem**: Old files are not being automatically deleted

**Solution**:
```bash
# Ensure both pkg-cache and cleanup-interval are set
./apk-cache -pkg-cache 168h -cleanup-interval 1h

# Check logs for cleanup records
# If pkg-cache is 0, auto cleanup is disabled

# Manual cleanup (via admin dashboard)
curl -u admin:password -X POST http://localhost:3142/_admin/clear
```

### 6. Slow Speed with Concurrent Downloads

**Problem**: Performance degrades when multiple requests download the same file

**This is normal**: File lock mechanism ensures only one download, other requests wait for the first to complete. This prevents duplicate downloads and cache conflicts.

**Optimization suggestions**:
- Use faster upstream server or mirror
- Configure SOCKS5/HTTP proxy to optimize network path
- Increase bandwidth or use CDN

### 7. Proxy Not Working in Docker Container

**Problem**: Cannot access host proxy via `127.0.0.1` inside container

**Solution**:
```bash
# Use host.docker.internal (Mac/Windows)
docker run -e PROXY=socks5://host.docker.internal:1080 apk-cache

# Use host network mode (Linux)
docker run --network host -e PROXY=socks5://127.0.0.1:1080 apk-cache

# Or use host IP address
docker run -e PROXY=socks5://192.168.1.100:1080 apk-cache
```

## Performance Optimization Tips

### 1. Use SSD for Cache Directory

```bash
# Placing cache directory on SSD significantly improves performance
./apk-cache -cache /mnt/ssd/apk-cache
```

### 2. Adjust Cache Expiration Times

```bash
# Production: Extend index cache time to reduce upstream requests
./apk-cache -index-cache 24h -pkg-cache 720h  # 30 days

# Development: Shorten cache time to get latest packages
./apk-cache -index-cache 2h -pkg-cache 168h   # 7 days
```

### 3. Use Local Mirror Sites

```bash
# Choose the geographically closest mirror
./apk-cache -upstream https://mirrors.tuna.tsinghua.edu.cn/alpine
```

### 4. Configure Multiple Upstream Servers

Improve availability and download speed:

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

## Frequently Asked Questions (FAQ)

**Q: How much disk space will the cache use?**

A: Depends on usage. A complete Alpine version with all packages is about 2-3 GB, but in practice you'll usually only cache needed packages, typically a few hundred MB.

**Q: Can I serve cache for multiple Alpine versions simultaneously?**

A: Yes. The cache directory automatically organizes by path (e.g., `cache/alpine/v3.22/main/x86_64/`), different versions don't interfere with each other.

**Q: What if cache hit rate is low?**

A: 
- Check if index cache time is too short
- Ensure client requests use consistent URLs (don't mix HTTP/HTTPS or different domains)
- View admin dashboard for detailed statistics

**Q: Does it support HTTPS?**

A: The program itself doesn't support HTTPS. It's recommended to place a reverse proxy like Nginx in front to provide HTTPS support.

**Q: Can I limit cache size?**

A: There's currently no built-in cache size limit. It's recommended to manage disk space through filesystem quotas or periodic cleanup.

## License

GPLv3 License

## Contributing

Issues and Pull Requests are welcome!

## Author

[tursom](https://github.com/tursom)

## Links

- GitHub: https://github.com/tursom/apk-cache
- Docker Hub: https://hub.docker.com/r/tursom/apk-cache
- Issue Tracker: https://github.com/tursom/apk-cache/issues
- Alpine Linux: https://alpinelinux.org/
