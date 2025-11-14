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

- `apk_cache_hits_total` - Cache hit count
- `apk_cache_misses_total` - Cache miss count
- `apk_cache_download_bytes_total` - Total download bytes

## Troubleshooting

### Common Issues

**Cache Miss**: Check cache directory permissions and disk space

**Proxy Connection Failed**: Verify proxy address format and availability (supports SOCKS5/HTTP protocols)

**Management Interface Unreachable**: Ensure correct access to `/_admin/` path

## License

GPLv3 License

## Development Roadmap

See [ROADMAP.md](ROADMAP.md) for future development directions and improvement plans.

## Links

- GitHub: https://github.com/tursom/apk-cache
- Docker Hub: https://hub.docker.com/r/tursom/apk-cache
- Issue Tracker: https://github.com/tursom/apk-cache/issues
