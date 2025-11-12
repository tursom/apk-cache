# apk-cache

A simple caching proxy for Alpine Linux APK repositories. This service acts as a local cache for Alpine packages, reducing bandwidth usage and speeding up package installations.

## Features

- **Transparent caching**: Automatically caches APK packages and repository metadata
- **Long-term storage**: Cached packages are kept for 365 days
- **Efficient**: Uses nginx's proven caching mechanisms
- **Easy deployment**: Simple Docker setup with docker-compose
- **Health monitoring**: Built-in health check endpoint

## Quick Start

### Using Docker Compose (Recommended)

1. Clone this repository:
```bash
git clone https://github.com/tursom/apk-cache.git
cd apk-cache
```

2. Start the cache server:
```bash
docker-compose up -d
```

The cache will be available at `http://localhost:3000`

### Using Docker

```bash
docker build -t apk-cache .
docker run -d -p 3000:80 -v apk-cache-data:/var/cache/nginx/apk apk-cache
```

## Configuration

### Configure Alpine Linux to use the cache

Edit `/etc/apk/repositories` on your Alpine Linux system:

```bash
# Replace with your cache server's address
http://your-cache-server:3000/alpine/v3.18/main
http://your-cache-server:3000/alpine/v3.18/community
```

Replace `v3.18` with your Alpine version (e.g., `v3.17`, `v3.19`, `edge`).

### Test the cache

```bash
# Update package index
apk update

# Install a package (will be cached)
apk add curl

# The second install will be served from cache
apk add --force-reinstall curl
```

### Check cache status

The cache adds an `X-Cache-Status` header to responses:
- `MISS`: First request, fetched from upstream
- `HIT`: Served from cache
- `UPDATING`: Cache is being updated

```bash
curl -I http://localhost:3000/alpine/v3.18/main/x86_64/APKINDEX.tar.gz
```

## Monitoring

### Health Check

```bash
curl http://localhost:3000/health
```

### View Logs

```bash
docker-compose logs -f apk-cache
```

### Cache Statistics

The access logs include cache status. View them with:

```bash
docker-compose exec apk-cache cat /var/log/nginx/access.log
```

## Advanced Configuration

### Adjust Cache Size

Edit `nginx.conf` and modify the `proxy_cache_path` directive:

```nginx
proxy_cache_path /var/cache/nginx/apk levels=1:2 keys_zone=apk_cache:10m max_size=20g inactive=365d use_temp_path=off;
```

- `max_size`: Maximum cache size (default: 10g)
- `inactive`: How long to keep unused items (default: 365d)

### Change Port

Edit `docker-compose.yml` and change the port mapping:

```yaml
ports:
  - "8080:80"  # Use port 8080 instead of 3000
```

## Architecture

```
Alpine Client -> apk-cache (nginx) -> dl-cdn.alpinelinux.org
                      ↓
                  Local Cache
```

The cache sits between your Alpine systems and the official Alpine CDN, transparently caching all package downloads.

## Troubleshooting

### Cache not working

1. Check if the service is running:
```bash
docker-compose ps
```

2. Check logs for errors:
```bash
docker-compose logs apk-cache
```

3. Verify cache directory permissions:
```bash
docker-compose exec apk-cache ls -la /var/cache/nginx/apk
```

### Clear cache

To clear all cached packages:

```bash
docker-compose down
docker volume rm apk-cache_apk-cache-data
docker-compose up -d
```

## License

MIT License - feel free to use this for any purpose.