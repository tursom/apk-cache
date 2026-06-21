# APK Cache

[Simplified Chinese](README.md) | English

APK Cache is an HTTP caching proxy for Linux package repositories. The current implementation focuses on three core paths:

- Alpine Linux APK cache proxy
- Debian/Ubuntu APT HTTP proxy cache
- Generic HTTP/HTTPS proxy forwarding, mainly for APT HTTPS `CONNECT` tunnels

The project has been rebuilt around a smaller runtime core. It keeps package caching, validation, proxying, observability, Docker deployment, CI/CD, and an embedded admin console. The old i18n layer and disconnected rate-limit/quota/policy subsystems are not part of the current version.

## Features

- APK caching: caches `.apk` packages and `APKINDEX.tar.gz`.
- APT caching: caches `.deb`, `Release`, `InRelease`, `Packages*`, `Sources*`, and `by-hash` requests in HTTP proxy mode.
- Unified cache pipeline: memory cache -> disk cache -> upstream.
- Concurrent safety: requests for the same cache key are serialized to avoid duplicate downloads and corrupted writes.
- Integrity validation: APK supports APKINDEX hash checks and RSA signature verification; APT supports Release/Packages-index SHA256 checks, by-hash, and package-file validation.
- Multiple APK upstreams: configured APK upstreams are tried with failover.
- Upstream proxy support: APK upstreams can have their own proxy; APT and generic proxy traffic use `proxy.upstream_proxy`.
- HTTPS tunneling: supports `CONNECT` for HTTPS APT sources.
- Admin console: embedded `/admin/` page and `/api/admin/v1/*` APIs with single-admin login.
- Persistent configuration: imports TOML/env runtime settings on first boot, then treats SQLite as the source of truth.
- Persistent hashes: stores APK/APT expected hashes, actual-hash cache, and APT by-hash mappings in Pebble.
- Operations endpoints: `/_health`, `/metrics`, and `/admin/`.
- Docker deployment: entrypoint generates runtime TOML from environment variables.
- CI/CD: GitHub Actions runs tests, binary builds, Docker build/smoke test, and tag releases.

## Quick Start

### Docker

```sh
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v "$PWD/cache:/app/cache" \
  -v "$PWD/data:/app/data" \
  ghcr.io/tursom/apk-cache:latest
```

Health check:

```sh
curl http://127.0.0.1:3142/_health
```

Prometheus metrics:

```sh
curl http://127.0.0.1:3142/metrics
```

### Docker Compose Build And Deployment

The repository includes [docker-compose.yml](docker-compose.yml). The default image name is the GitHub Container Registry image: `ghcr.io/tursom/apk-cache:latest`.

Build from the current source tree and start:

```sh
ADMIN_BOOTSTRAP_TOKEN='change-me-once' \
ADMIN_SESSION_SECRET='replace-with-random-secret' \
docker compose up -d --build
```

Deploy only with the image already published on GitHub:

```sh
ADMIN_BOOTSTRAP_TOKEN='change-me-once' \
ADMIN_SESSION_SECRET='replace-with-random-secret' \
docker compose up -d
```

Use a different GitHub tag:

```sh
APK_CACHE_IMAGE=ghcr.io/tursom/apk-cache:v1.2.3 \
ADMIN_BOOTSTRAP_TOKEN='change-me-once' \
ADMIN_SESSION_SECRET='replace-with-random-secret' \
docker compose up -d
```

`--build` rebuilds from the current source tree and tags the result locally as `ghcr.io/tursom/apk-cache:latest`; it does not push to GitHub.

Default mounts:

```text
./cache -> /app/cache
./data  -> /app/data
```

Common variables:

- `APK_CACHE_HTTP_PORT`: host port, default `3142`.
- `APK_CACHE_CACHE_DIR`: host cache directory, default `./cache`.
- `APK_CACHE_DATA_DIR`: host data directory, default `./data`.
- `APK_UPSTREAM`: APK upstream, default `https://dl-cdn.alpinelinux.org`.
- `UPSTREAM_PROXY`: outbound proxy for APT and generic proxy traffic.
- `GOPROXY`: Go module proxy for the build stage.

First-time admin setup requires a bootstrap token:

```sh
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v "$PWD/cache:/app/cache" \
  -v "$PWD/data:/app/data" \
  -e ADMIN_BOOTSTRAP_TOKEN='change-me-once' \
  ghcr.io/tursom/apk-cache:latest
```

Open `http://127.0.0.1:3142/admin/` and create the single administrator with the bootstrap token. After setup, normal admin login uses the configured username and password.

### Build From Source

Requirements:

- Go `1.25.4` or a compatible version
- Node.js `22` or a compatible version, for building the React admin console
- Optional: Docker, for container builds and smoke tests

Build:

```sh
./build.sh
```

Run:

```sh
cp config.example.toml config.toml
./apk-cache -config config.toml
```

Test:

```sh
go test ./...
go test ./... -coverprofile=coverage.out
```

## Client Configuration

### Alpine APK

Point Alpine repositories at the APK Cache service. If the service is available at `http://cache.example:3142`:

```sh
sed -i 's|https://dl-cdn.alpinelinux.org|http://cache.example:3142|g' /etc/apk/repositories
apk update
apk add curl
```

Dockerfile example:

```dockerfile
FROM alpine:3.23

RUN sed -i 's|https://dl-cdn.alpinelinux.org|http://cache.example:3142|g' /etc/apk/repositories \
    && apk add --no-cache curl
```

Example APK requests:

```text
/alpine/v3.23/main/x86_64/APKINDEX.tar.gz
/alpine/v3.23/main/x86_64/busybox-1.37.0-r0.apk
```

### Debian/Ubuntu APT

APT uses APK Cache as an HTTP proxy.

Create `/etc/apt/apt.conf.d/01-apk-cache-proxy`:

```conf
Acquire::HTTP::Proxy "http://cache.example:3142";
Acquire::HTTPS::Proxy "http://cache.example:3142";
```

Then run:

```sh
apt-get update
apt-get install curl
```

Notes:

- `http://` APT repositories can be parsed and cached.
- `https://` APT repositories usually use `CONNECT` TLS tunnels. This service forwards those tunnels, but it does not decrypt or cache TLS contents.

## Configuration

The default config path is `config.toml`; use `-config` to choose another file:

```sh
./apk-cache -config /path/to/config.toml
```

See [config.example.toml](config.example.toml) for a complete example.

### Example

```toml
[server]
listen = ":3142"

[database]
path = ""

[admin]
bootstrap_token = ""
session_secret = ""

[hash_store]
path = ""
rebuild_on_corruption = false
trust_file_stat = true
actual_revalidate_interval = "24h"

[[upstreams]]
name = "Official Alpine CDN"
url = "https://dl-cdn.alpinelinux.org"
kind = "apk"
# proxy = "socks5://127.0.0.1:1080"

[cache]
root = "./cache"
data_root = "./data"
index_ttl = "24h"
package_ttl = "720h"

[cache.memory]
enabled = true
max_size = "256MB"
max_item_size = "16MB"
ttl = "30m"
max_items = 2048

[transport]
timeout = "30s"
idle_conn_timeout = "90s"
max_idle_conns = 128

[apk]
enabled = true
verify_hash = true
verify_signature = true
keys_dir = ""

[apt]
enabled = true
verify_hash = true
load_index_async = true

[proxy]
enabled = true
allow_connect = true
cache_non_package_requests = false
upstream_proxy = ""
allowed_hosts = []
```

### Configuration Reference

| Key | Default | Description |
| --- | --- | --- |
| `server.listen` | `:3142` | HTTP listen address |
| `database.path` | `${cache.data_root}/apk-cache.db` | SQLite database path; empty uses the default path |
| `admin.bootstrap_token` | empty | Required for first-time admin creation; prefer `ADMIN_BOOTSTRAP_TOKEN` |
| `admin.session_secret` | empty | HMAC key for session/CSRF token hashes; prefer an environment variable in production |
| `hash_store.path` | `${cache.data_root}/hash.pebble` | Pebble hash-store path |
| `hash_store.rebuild_on_corruption` | `false` | Allow deleting and rebuilding the hash store after corruption |
| `hash_store.trust_file_stat` | `true` | Reuse actual-hash cache entries when size and mtime match |
| `hash_store.actual_revalidate_interval` | `24h` | Maximum interval before recomputing an actual hash even if stat matches |
| `upstreams[].name` | `Official Alpine CDN` | APK upstream display name |
| `upstreams[].url` | `https://dl-cdn.alpinelinux.org` | APK upstream base URL |
| `upstreams[].kind` | `apk` | Only APK upstreams are used for APK fetches |
| `upstreams[].proxy` | empty | Outbound proxy for this APK upstream |
| `cache.root` | `./cache` | Disk cache directory |
| `cache.data_root` | `./data` | Runtime data directory; stores SQLite and Pebble by default |
| `cache.index_ttl` | `24h` | Index-file cache TTL |
| `cache.package_ttl` | `720h` | Package-file cache TTL; `0` means never expire |
| `cache.memory.enabled` | `true` | Enable memory cache |
| `cache.memory.max_size` | `256MB` | Maximum memory-cache size |
| `cache.memory.max_item_size` | `16MB` | Maximum single file size allowed in memory cache |
| `cache.memory.ttl` | `30m` | Memory-cache item TTL |
| `cache.memory.max_items` | `2048` | Maximum memory-cache item count |
| `transport.timeout` | `30s` | Upstream HTTP client timeout |
| `transport.idle_conn_timeout` | `90s` | Idle connection timeout |
| `transport.max_idle_conns` | `128` | Max idle connections |
| `apk.enabled` | `true` | Enable APK handling |
| `apk.verify_hash` | `true` | Validate `.apk` files against APKINDEX |
| `apk.verify_signature` | `true` | Verify APK/APKINDEX RSA signatures |
| `apk.keys_dir` | empty | Directory with extra Alpine RSA public keys |
| `apt.enabled` | `true` | Enable APT handling |
| `apt.verify_hash` | `true` | Validate APT by-hash, files referenced by Release indexes, and package-file SHA256 |
| `apt.load_index_async` | `true` | Load newly cached APT indexes asynchronously |
| `proxy.enabled` | `true` | Enable generic HTTP/HTTPS proxying |
| `proxy.allow_connect` | `true` | Allow `CONNECT` tunnels |
| `proxy.cache_non_package_requests` | `false` | Cache non APK/APT HTTP requests |
| `proxy.upstream_proxy` | empty | Outbound proxy for APT and generic proxy traffic |
| `proxy.allowed_hosts` | `[]` | If non-empty, only these target hosts are allowed |

Supported proxy URL schemes:

```text
socks5://127.0.0.1:1080
http://127.0.0.1:8080
https://127.0.0.1:8443
```

HTTP proxy credentials can be embedded in the URL:

```text
http://user:password@proxy.example:8080
```

## Docker Environment Variables

At container startup, `entrypoint.sh` generates a temporary TOML config from environment variables and executes `/app/apk-cache -config <generated>`.

| Environment variable | Default | Config target |
| --- | --- | --- |
| `CONFIG` | `/tmp/apk-cache.toml` | Generated config path |
| `LISTEN` / `ADDR` | `:3142` | `server.listen` |
| `CACHE_ROOT` / `CACHE_DIR` | `/app/cache` | `cache.root` |
| `DATA_ROOT` | `/app/data` | `cache.data_root` |
| `DATABASE_PATH` | empty | `database.path`; empty uses `${DATA_ROOT}/apk-cache.db` |
| `ADMIN_BOOTSTRAP_TOKEN` | empty | `admin.bootstrap_token` |
| `ADMIN_SESSION_SECRET` | empty | `admin.session_secret` |
| `HASH_STORE_PATH` | empty | `hash_store.path`; empty uses `${DATA_ROOT}/hash.pebble` |
| `HASH_STORE_REBUILD_ON_CORRUPTION` | `false` | `hash_store.rebuild_on_corruption` |
| `HASH_STORE_TRUST_FILE_STAT` | `true` | `hash_store.trust_file_stat` |
| `HASH_STORE_ACTUAL_REVALIDATE_INTERVAL` | `24h` | `hash_store.actual_revalidate_interval` |
| `APK_UPSTREAM` / `UPSTREAM` | `https://dl-cdn.alpinelinux.org` | APK upstream URL |
| `APK_UPSTREAM_PROXY` / `PROXY` | empty | APK upstream proxy |
| `UPSTREAM_PROXY` | empty | `proxy.upstream_proxy` |
| `INDEX_TTL` | `24h` | `cache.index_ttl` |
| `PACKAGE_TTL` | `720h` | `cache.package_ttl` |
| `MEMORY_CACHE_ENABLED` | `true` | `cache.memory.enabled` |
| `MEMORY_CACHE_SIZE` | `256MB` | `cache.memory.max_size` |
| `MEMORY_CACHE_MAX_ITEM_SIZE` | `16MB` | `cache.memory.max_item_size` |
| `MEMORY_CACHE_TTL` | `30m` | `cache.memory.ttl` |
| `MEMORY_CACHE_MAX_ITEMS` | `2048` | `cache.memory.max_items` |
| `TRANSPORT_TIMEOUT` | `30s` | `transport.timeout` |
| `TRANSPORT_IDLE_CONN_TIMEOUT` | `90s` | `transport.idle_conn_timeout` |
| `TRANSPORT_MAX_IDLE_CONNS` | `128` | `transport.max_idle_conns` |
| `APK_ENABLED` | `true` | `apk.enabled` |
| `APK_VERIFY_HASH` | `true` | `apk.verify_hash` |
| `APK_VERIFY_SIGNATURE` | `true` | `apk.verify_signature` |
| `APT_ENABLED` | `true` | `apt.enabled` |
| `APT_VERIFY_HASH` | `true` | `apt.verify_hash` |
| `APT_LOAD_INDEX_ASYNC` | `true` | `apt.load_index_async` |
| `PROXY_ENABLED` | `true` | `proxy.enabled` |
| `PROXY_ALLOW_CONNECT` | `true` | `proxy.allow_connect` |
| `PROXY_CACHE_NON_PACKAGE_REQUESTS` | `false` | `proxy.cache_non_package_requests` |
| `PROXY_ALLOWED_HOSTS` | empty | Comma-separated `proxy.allowed_hosts` |

Docker example:

```sh
docker run -d \
  --name apk-cache \
  -p 3142:3142 \
  -v "$PWD/cache:/app/cache" \
  -e APK_UPSTREAM=https://mirrors.tuna.tsinghua.edu.cn/alpine \
  -e UPSTREAM_PROXY=socks5://127.0.0.1:1080 \
  ghcr.io/tursom/apk-cache:latest
```

## How It Works

### Request Routing

Every request enters the same HTTP handler:

1. `/_health` returns health information.
2. `/metrics` returns Prometheus metrics.
3. `/admin/` returns the embedded admin console.
4. `/api/admin/v1/*` enters the admin API; all endpoints except setup/login require a session and CSRF token.
5. `CONNECT` enters the tunnel proxy.
6. Other requests are classified as APK, APT, or generic proxy traffic.

### Configuration And Storage

The service still reads TOML/env on startup, but those inputs mainly bootstrap the listen address, data directory, database path, and first admin setup. After SQLite opens, migrations run and runtime configuration is loaded as follows:

1. If `settings` is empty, the merged TOML/env runtime configuration is imported into SQLite.
2. If SQLite already has settings, SQLite wins; runtime settings in TOML/env no longer overwrite the database.
3. Upstreams, admin users, sessions, cache objects, request logs, and management summaries are stored in SQLite.
4. APK/APT expected hashes, actual-hash cache, source mappings, APT by-hash mappings, and path dictionaries are stored in Pebble.

After a setting is saved in the admin console, a restart will not accidentally revert it from an old TOML file. Startup-time settings such as `server.listen`, `cache.root`, `cache.data_root`, and `hash_store.path` are marked as restart-required by the API.

### Cache Pipeline

APK, APT, and cacheable generic proxy requests share the same cache flow:

1. Check memory cache; hit returns `X-Cache: MEMORY-HIT`.
2. Check disk cache; hit returns `X-Cache: HIT`.
3. Lock by cache key to prevent duplicate concurrent downloads.
4. Check caches again after acquiring the lock.
5. Fetch upstream while streaming the response to the client and a temporary file.
6. Validate the temporary file.
7. Atomically `rename` the temporary file into the final cache path.
8. Store in memory cache when applicable.
9. First upstream fetch returns `X-Cache: MISS`.

Non-`200 OK` upstream responses are passed through without caching and return `X-Cache: BYPASS`.

### APK Validation

APK validation is implemented in `internal/apk`:

- `APKINDEX.tar.gz` is parsed into package name, version, size, and checksum records.
- `.apk` files can be checked against APKINDEX size and hash records.
- APK/APKINDEX archives can be verified using `.SIGN.*` RSA signatures.
- Built-in Alpine public keys are included; extra keys can be loaded from `apk.keys_dir`.
- A newly fetched response with an invalid signature is returned to the client but is not cached.

### APT Validation

APT validation is implemented in `internal/apt`:

- `Release` / `InRelease` files provide SHA256 file lists.
- `Packages*` / `Sources*` files provide package paths, sizes, and SHA256 hashes.
- `by-hash/SHA256/<hash>` requests are checked against the hash declared in the URL.
- If a `by-hash` request matches an index file recorded by `Release`, its `Packages*` / `Sources*` body is parsed through the original index path.
- `Packages*` / `Sources*` files referenced by `Release` are validated on cache hits and after downloads.
- `.deb` files are checked against index SHA256 when an index record is available.
- Missing index records do not block the request.

Actual file hashes are cached in Pebble. A cached actual hash is reused when file size and mtime match; by default it is recomputed at least every 24 hours to reduce long-lived stat-spoofing risk.

### CONNECT Tunnels

`CONNECT` only creates a TCP tunnel:

- TLS is not inspected.
- TLS contents are not cached.
- `proxy.allow_connect=false` disables tunnels.
- `proxy.allowed_hosts` can restrict destination hosts.
- Concurrent tunnels have a fixed limit to prevent unbounded connection usage.

## Operations Endpoints

### `GET /admin/`

Embedded admin console. First-time flow:

1. Set `ADMIN_BOOTSTRAP_TOKEN` and start the service.
2. Open `/admin/` and use the setup page.
3. Enter the bootstrap token, username, and password to create the single admin.
4. Later access uses normal admin login.

The first admin console includes:

- Dashboard: health, upstream status, cache-object count, CONNECT count, and hash-store status.
- Configuration: inspect and update SQLite-backed runtime settings, including hot-reload vs restart-required markers.
- Upstreams: add, enable, disable, and delete APK upstreams.
- Proxy: enable/disable the generic proxy, CONNECT, non-package caching, and allowed hosts.
- Cache: search cache objects, delete objects, dry-run batch deletion, reconcile disk metadata, clear memory cache, and prewarm URLs.
- APK/APT: inspect indexes and parsed records, reload indexes, and trigger APT validation.
- Logs, System, and Hash: inspect recent request logs, error logs, system information, diagnostic packages, and Pebble hash-store statistics.

Frontend source lives in `internal/admin/web` and uses React + TypeScript + Vite. The production build is written to `internal/admin/static` and embedded into the Go binary. `./build.sh` runs the frontend build automatically.

The admin API prefix is `/api/admin/v1` and responses use:

```json
{
  "ok": true,
  "data": {},
  "error": null
}
```

### `GET /_health`

Example response:

```json
{
  "status": "healthy",
  "apk_upstreams_total": 1,
  "apk_upstreams": {
    "healthy": 1,
    "total": 1
  },
  "disk_cache": {
    "status": "healthy"
  },
  "memory_cache": {
    "items": 0,
    "size": 0,
    "max": 268435456
  }
}
```

If the cache directory is unavailable or all APK upstreams are unhealthy, the status becomes `degraded` and the HTTP status code is `503`.

### `GET /metrics`

Prometheus metrics include:

- `apk_cache_hits_total`
- `apk_cache_misses_total`
- `apk_cache_download_bytes_total`
- `apk_cache_response_bytes_total`
- `apk_cache_upstream_requests_total`
- `apk_cache_upstream_failovers_total`
- `apk_cache_validation_failures_total`
- `apk_cache_apk_hash_failures_total`
- `apk_cache_apk_signature_failures_total`
- `apk_cache_apk_bypass_responses_total`
- `apk_cache_memory_hits_total`
- `apk_cache_memory_misses_total`
- `apk_cache_memory_evictions_total`
- `apk_cache_memory_size_bytes`
- `apk_cache_memory_items_total`

## Development And Testing

Common commands:

```sh
npm ci --prefix internal/admin/web
npm run --prefix internal/admin/web build
go test ./...
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
./build.sh
docker build -t apk-cache:local .
```

Real-data integration tests live in `internal/app/real_integration_test.go`:

- APT uses `Release`, `Packages.xz`, and `.deb` fixtures fetched from `deb.debian.org`, covering the `Release -> Packages.xz -> by-hash -> .deb` cache and SHA256 validation chain.
- APK uses `APKINDEX.tar.gz` and `.apk` fixtures fetched from `dl-cdn.alpinelinux.org`, covering real signature verification, APKINDEX hash validation, and refetch after cache corruption.
- Tests serve those fixtures through a local `httptest.Server`, so they do not require external network access.

Local Docker smoke test:

```sh
docker run --rm -p 3142:3142 apk-cache:local
curl http://127.0.0.1:3142/_health
```

## GitHub Actions

[.github/workflows/build.yml](.github/workflows/build.yml) currently runs:

- Go unit tests and coverage artifact upload.
- Linux / Windows / macOS binary builds.
- Docker image build and `/_health` smoke test.
- GHCR image push for non-PR events.
- GitHub Release creation for `v*` tags, including binary artifacts.

## Current Limitations

- The admin console is single-admin only; there are no multi-user roles.
- There is no operation audit log yet; request logs are for troubleshooting only.
- The admin console polls APIs; it does not use WebSocket push.
- No global request rate limiter.
- No disk quota manager or automatic cleanup policy.
- HTTPS APT sources are forwarded through `CONNECT`; they are not decrypted or cached.

These capabilities can be redesigned on top of the current smaller core, but the old mismatched implementations are not restored.
