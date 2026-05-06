# APK Cache Development Guide

[中文](DEV_ZH.md) | English

## Project Overview

APK Cache is a high-performance proxy server for caching Alpine Linux APK packages and Debian/Ubuntu APT packages. It features a three-tier caching architecture (Memory → File → Upstream), health monitoring with self-healing capabilities, and comprehensive security features.

## Project Structure

```
apk-cache/
├── cmd/
│   ├── apk-cache/              # Main application
│   │   ├── main.go             # Entry point
│   │   ├── app.go              # App assembly (config, HTTP server, shutdown)
│   │   ├── pipeline.go         # Unified request pipeline (cache tiering)
│   │   ├── protocol.go         # ProtocolAdapter interface + APK/APT/Proxy adapters
│   │   ├── proxy_tunnel.go     # TCP tunnel helpers for CONNECT
│   │   ├── memory_cache.go     # In-memory LRU cache with TTL
│   │   ├── apk_index_service.go    # APKINDEX index/validation
│   │   ├── apk_archive.go      # APK .apk archive reading
│   │   ├── apk_signature.go    # APK RSA signature verification
│   │   ├── apt_index_service.go    # APT Release/Packages index/validation
│   │   ├── admin.html          # Admin dashboard HTML
│   │   ├── *_test.go           # Tests
│   │   └── apk_test_helpers_test.go  # Test helper utilities
│   ├── apt-hash/               # APT hash tool
│   └── i18n-analyzer/          # i18n coverage analyzer
├── internal/
│   ├── config/
│   │   └── config.go           # TOML config loading + validation
│   ├── upstream/
│   │   ├── upstream.go         # Upstream server manager + failover fetcher
│   │   └── transport.go        # HTTP transport with proxy (SOCKS5/HTTP)
│   └── policy/
│       └── policy.go           # Fine-grained cache policies
├── utils/
│   ├── monitoring.go           # Prometheus metrics
│   ├── lockman.go              # In-process file-level lock manager
│   ├── rate_limiter.go         # Token-bucket rate limiter
│   ├── ip_utils.go             # IP validation utilities
│   ├── matcher.go              # Path matching utilities
│   ├── parse_utils.go          # Path normalization
│   ├── set.go                  # Set data structure
│   ├── decompress.go           # Decompression helpers
│   ├── apt_index.go            # APT index file detection
│   ├── apt/                    # APT package parsing
│   │   ├── package.go
│   │   ├── release.go
│   │   ├── diff.go
│   │   └── utils.go
│   ├── data_integrity/         # File integrity verification
│   │   ├── manager.go
│   │   ├── memory.go
│   │   ├── persistent.go
│   │   └── apt_manager.go
│   └── i18n/                   # Internationalization
│       ├── i18n.go
│       └── locales/
├── build.sh                    # Build script (required for admin.html compression)
├── run_test.sh                 # Integration test script
├── Dockerfile                  # Docker build
├── entrypoint.sh               # Docker entrypoint
├── config.example.toml         # Configuration reference
├── go.mod                      # Go module
└── go.sum                      # Go dependencies
```

## Prerequisites

- Go 1.25 or later
- Git
- For HTML compression (optional): html-minifier, python-htmlmin, or esbuild

## Build Instructions

**Important**: Always use the build script, not `go build` directly:

```bash
./build.sh
```

The build script automatically:
1. Detects available HTML compression tools
2. Compresses the admin dashboard HTML
3. Creates gzip-compressed versions
4. Builds the Go application with optimizations

## Running the Application

```bash
# Default configuration
./apk-cache

# With custom config
./apk-cache -config config.toml

# With command-line arguments
./apk-cache -addr :3142 -cache ./cache -proxy socks5://127.0.0.1:1080
```

## Running Tests

### Prerequisites

- Docker installed and running

### Using run_test.sh

The project includes an integration test script that tests both APK and APT caching functionality:

```bash
./run_test.sh
```

The test script performs the following steps:
1. Build the application using `build.sh` (called automatically during Docker build)
2. Build Docker image
3. Start the apk-cache service
4. Test with Alpine Linux client (apk update)
5. Test with Debian client (apt-get update)

The script automatically cleans up the test environment (containers, images) after tests complete, but preserves the cache directory (`/tmp/apk-cache-test-cache`) for inspection.

### Supported Parameters

| Parameter | Description |
|-----------|-------------|
| `--goproxy <value>` | Set GOPROXY for go build dependencies |
| `--alpine-apk-mirror <url>` | Alpine mirror for Docker build (e.g., http://mirror/alpine) |
| `--apk-mirror <url>` | Alias for --alpine-apk-mirror |
| `--lang <zh\|en>` | Set language (default: auto-detect) |
| `-h, --help` | Show help message |

### Examples

```bash
# Run tests with Chinese output
./run_test.sh --lang zh

# Run tests with a custom Go proxy
./run_test.sh --goproxy https://goproxy.cn

# Run tests with a custom Alpine mirror
./run_test.sh --alpine-apk-mirror http://mirror.example.com/alpine
```

## Code Conventions

### Naming
- Use camelCase for variable and function names
- Use PascalCase for exported types, functions, and constants
- Use mixedCaps for unexported global variables

### Error Handling
- Always handle errors explicitly
- Return errors instead of logging silently when appropriate
- Use `fmt.Errorf("context: %v", err)` for error messages
- Use `errors.New()` for static error messages

### Logging
- Use structured logging with appropriate levels
- Include context in log messages

### Internationalization (i18n)
- All user-facing strings must use the i18n system via `i18n.T()`
- Never hardcode visible strings - use translation keys
- Provide translation keys in `utils/i18n/` directory

### Configuration
- All configuration options must have CLI flags
- Configuration can also be loaded from TOML files
- Use sensible defaults with clear documentation

## Key Components

### Protocol Adapter Pipeline
All incoming requests flow through a unified pipeline that delegates to protocol-specific adapters:
- **APKAdapter**: Matches APK package/index requests by path pattern. Normalizes to upstream path, handles APKINDEX hash verification and RSA signature validation.
- **APTAdapter**: Matches APT proxy requests (absolute-form URLs). Handles by-hash verification and Release/Packages index parsing.
- **ProxyAdapter**: Matches CONNECT tunnels and generic absolute-form HTTP requests. Supports upstream proxy chaining.

### Cache Architecture (Three-Tier)
1. **Memory Cache**: LRU cache with TTL, fastest access. Only for package files and APT indexes, not APKINDEX.
2. **File Cache**: Persistent disk storage with per-file locking via `FileLockManager`.
3. **Upstream**: Original package sources via `upstream.Manager` with health-based failover.

### Request Pipeline Flow
1. Match adapter by request pattern
2. Normalize request to adapter-specific format
3. Check cache policy → try memory cache → try disk cache (with TTL + validation)
4. On miss: acquire file lock → double-check caches → fetch from upstream
5. Stream response to client while writing to temp file
6. Validate temp file → promote to cache → notify index services

### Health Check System
- Automatic failover between healthy upstream servers
- Panic recovery in HTTP handler (returns 500, logs stack trace)
- Request timeout protection (120s default, CONNECT exempt)
- Graceful shutdown with background goroutine draining

## Adding New Features

1. Create a new branch: `git checkout -b feature/your-feature`
2. Make changes following code conventions
3. Add tests for new functionality
4. Update documentation
5. Submit pull request

## Dependencies

Core dependencies (see `go.mod`):
- `github.com/prometheus/client_golang` - Prometheus metrics
- `go.etcd.io/bbolt` - Embedded database
- `golang.org/x/net` - HTTP utilities
- `github.com/nicksnyder/go-i18n/v2` - Internationalization
