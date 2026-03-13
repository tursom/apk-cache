# APK Cache Development Guide

[中文](DEV_ZH.md) | English

## Project Overview

APK Cache is a high-performance proxy server for caching Alpine Linux APK packages and Debian/Ubuntu APT packages. It features a three-tier caching architecture (Memory → File → Upstream), health monitoring with self-healing capabilities, and comprehensive security features.

## Project Structure

```
apk-cache/
├── cmd/
│   ├── apk-cache/           # Main application
│   │   ├── main.go          # Entry point
│   │   ├── config.go        # Configuration loading
│   │   ├── cache.go         # File cache implementation
│   │   ├── memory_cache.go  # In-memory cache layer
│   │   ├── handlers.go      # HTTP request handlers
│   │   ├── admin.go         # Admin dashboard
│   │   ├── upstream.go      # Upstream server management
│   │   ├── cleanup.go       # Cache cleanup logic
│   │   ├── cache_quota.go   # Cache quota management
│   │   ├── cache_apt.go     # APT proxy support
│   │   ├── http_proxy.go    # HTTP proxy support
│   │   └── access_tracker.go # Access tracking
│   └── apt-hash/            # APT hash tool
├── build.sh                 # Build script (required)
├── Dockerfile               # Docker build file
├── go.mod                   # Go module definition
├── go.sum                   # Go dependencies
├── config.example.toml      # Configuration example
└── cmd/apk-cache/admin.html # Admin dashboard HTML
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

### Cache Architecture (Three-Tier)
1. **Memory Cache**: LRU cache with TTL support, fastest access
2. **File Cache**: Persistent disk storage
3. **Upstream**: Original package sources

### Health Check System
- Periodic checks for upstream servers, filesystem, memory cache, and cache quota
- Automatic failover to healthy upstreams
- Self-healing mechanisms for common issues

### Security Features
- Proxy authentication (SOCKS5/HTTP)
- Admin dashboard authentication
- IP whitelisting
- Reverse proxy support
- Path security validation

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
