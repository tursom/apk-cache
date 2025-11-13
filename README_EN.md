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

### Installation

```bash
git clone git@github.com:tursom/apk-cache.git
cd apk-cache
go build -o apk-cache cmd/apk-cache/main.go
```

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
├── go.mod
├── go.sum
├── README.md                  # Chinese documentation
├── README_EN.md               # English documentation
└── ADMIN.md                   # Admin dashboard documentation
```

## Dependencies

- `golang.org/x/net/proxy` - SOCKS5 proxy support
- `github.com/nicksnyder/go-i18n/v2` - Internationalization support
- `github.com/BurntSushi/toml` - TOML configuration file parsing
- `github.com/prometheus/client_golang` - Prometheus monitoring metrics
- `golang.org/x/text/language` - Language detection and handling

## License

GPLv3 License

## Contributing

Issues and Pull Requests are welcome!
