# APK Cache

English | [简体中文](README.md)

A caching proxy server for Alpine Linux APK packages with SOCKS5 proxy support and multilingual interface.

## Features

- 🚀 Automatically cache Alpine Linux APK packages
- 📦 Serve directly from local cache on cache hit
- 🔄 Fetch from upstream server and save on cache miss
- 🌐 SOCKS5 proxy support for upstream server access
- 💾 Configurable cache directory and listen address
- ⏱️ Automatic expiration and refresh for APKINDEX.tar.gz index files
- 🔒 Client disconnection doesn't affect cache file saving
- ⚡ Simultaneous writing to cache and client for improved response speed
- 🔐 File-level lock management to avoid concurrent download conflicts
- 🌍 Multilingual support (Chinese/English) with automatic system language detection

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

# Run with custom configuration
./apk-cache -addr :8080 -cache ./cache -proxy socks5://127.0.0.1:1080

# Specify language
./apk-cache -locale zh  # Chinese
./apk-cache -locale en  # English
```

### Command Line Arguments

| Argument | Default | Description |
|----------|---------|-------------|
| `-addr` | `:8080` | Listen address |
| `-cache` | `./cache` | Cache directory path |
| `-upstream` | `https://dl-cdn.alpinelinux.org` | Upstream server URL |
| `-proxy` | (empty) | SOCKS5 proxy address, format: `socks5://[username:password@]host:port` |
| `-index-cache` | `24h` | APKINDEX.tar.gz index file cache duration |
| `-locale` | (auto-detect) | Interface language (`en`/`zh`), auto-detect from `LANG` environment variable if empty |

## Usage

### Configure Alpine Linux to Use Cache Server

Edit `/etc/apk/repositories`:

```bash
# Replace default mirror address with cache server address
sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:8080/g' /etc/apk/repositories
```

Or use command line directly:

```bash
# Specify cache server when installing packages
apk add --repositories-file /dev/null --repository http://your-cache-server:8080/alpine/v3.22/main <package-name>
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

- **APK package files**: Permanent cache, never expires
- **APKINDEX.tar.gz index files**: Periodic expiration (default 24 hours), adjustable via `-index-cache` parameter
- **Other files**: Permanent cache

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

## Performance Features

### Concurrency Safety

- **File-level lock management**: Uses custom `FileLockManager` to ensure each file is only downloaded once
- **Reference counting**: Automatically manages lock lifecycle to avoid memory leaks
- **Double-check**: Checks cache again after acquiring lock to avoid duplicate downloads

### Client-Friendly

- **Streaming transfer**: Downloads while transferring to client, no need to wait for complete download
- **Resilient caching**: Client disconnection doesn't affect cache integrity
- **Concurrent downloads**: Different files can be downloaded concurrently without interfering with each other

## Project Structure

```
apk-cache/
├── cmd/
│   └── apk-cache/
│       ├── main.go          # Main program
│       ├── lockman.go       # File lock manager
│       └── lockman_test.go  # Unit tests
├── locales/
│   ├── en.toml             # English translations
│   └── zh.toml             # Chinese translations
├── cache/                  # Cache directory (generated at runtime)
├── go.mod
├── go.sum
└── README.md
```

## Dependencies

- `golang.org/x/net/proxy` - SOCKS5 proxy support
- `github.com/nicksnyder/go-i18n/v2` - Internationalization support
- `github.com/BurntSushi/toml` - TOML configuration file parsing

## License

GPLv3 License

## Contributing

Issues and Pull Requests are welcome!
