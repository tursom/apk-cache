#!/bin/sh
set -eu

CONFIG=${CONFIG:-/tmp/apk-cache.toml}
LISTEN=${LISTEN:-${ADDR:-:3142}}
CACHE_ROOT=${CACHE_ROOT:-${CACHE_DIR:-/app/cache}}
DATA_ROOT=${DATA_ROOT:-/app/data}
APK_UPSTREAM=${APK_UPSTREAM:-${UPSTREAM:-https://dl-cdn.alpinelinux.org}}
APK_UPSTREAM_PROXY=${APK_UPSTREAM_PROXY:-${PROXY:-}}
INDEX_TTL=${INDEX_TTL:-24h}
PACKAGE_TTL=${PACKAGE_TTL:-720h}
MEMORY_CACHE_ENABLED=${MEMORY_CACHE_ENABLED:-true}
MEMORY_CACHE_SIZE=${MEMORY_CACHE_SIZE:-256MB}
MEMORY_CACHE_MAX_ITEM_SIZE=${MEMORY_CACHE_MAX_ITEM_SIZE:-16MB}
MEMORY_CACHE_TTL=${MEMORY_CACHE_TTL:-30m}
MEMORY_CACHE_MAX_ITEMS=${MEMORY_CACHE_MAX_ITEMS:-2048}
TRANSPORT_TIMEOUT=${TRANSPORT_TIMEOUT:-30s}
TRANSPORT_IDLE_CONN_TIMEOUT=${TRANSPORT_IDLE_CONN_TIMEOUT:-90s}
TRANSPORT_MAX_IDLE_CONNS=${TRANSPORT_MAX_IDLE_CONNS:-128}
APK_ENABLED=${APK_ENABLED:-true}
APT_ENABLED=${APT_ENABLED:-true}
APT_VERIFY_HASH=${APT_VERIFY_HASH:-true}
APT_LOAD_INDEX_ASYNC=${APT_LOAD_INDEX_ASYNC:-true}
PROXY_ENABLED=${PROXY_ENABLED:-true}
PROXY_ALLOW_CONNECT=${PROXY_ALLOW_CONNECT:-true}
PROXY_CACHE_NON_PACKAGE_REQUESTS=${PROXY_CACHE_NON_PACKAGE_REQUESTS:-false}
PROXY_REQUIRE_AUTH=${PROXY_REQUIRE_AUTH:-false}
PROXY_ALLOWED_HOSTS=${PROXY_ALLOWED_HOSTS:-}

mkdir -p "$(dirname "$CONFIG")" "$CACHE_ROOT" "$DATA_ROOT"

cat >"$CONFIG" <<EOF
[server]
listen = "$LISTEN"

[[upstreams]]
name = "Primary APK upstream"
url = "$APK_UPSTREAM"
kind = "apk"
EOF

if [ -n "$APK_UPSTREAM_PROXY" ]; then
    cat >>"$CONFIG" <<EOF
proxy = "$APK_UPSTREAM_PROXY"
EOF
fi

cat >>"$CONFIG" <<EOF

[cache]
root = "$CACHE_ROOT"
data_root = "$DATA_ROOT"
index_ttl = "$INDEX_TTL"
package_ttl = "$PACKAGE_TTL"

[cache.memory]
enabled = $MEMORY_CACHE_ENABLED
max_size = "$MEMORY_CACHE_SIZE"
max_item_size = "$MEMORY_CACHE_MAX_ITEM_SIZE"
ttl = "$MEMORY_CACHE_TTL"
max_items = $MEMORY_CACHE_MAX_ITEMS

[transport]
timeout = "$TRANSPORT_TIMEOUT"
idle_conn_timeout = "$TRANSPORT_IDLE_CONN_TIMEOUT"
max_idle_conns = $TRANSPORT_MAX_IDLE_CONNS

[apk]
enabled = $APK_ENABLED

[apt]
enabled = $APT_ENABLED
verify_hash = $APT_VERIFY_HASH
load_index_async = $APT_LOAD_INDEX_ASYNC

[proxy]
enabled = $PROXY_ENABLED
allow_connect = $PROXY_ALLOW_CONNECT
cache_non_package_requests = $PROXY_CACHE_NON_PACKAGE_REQUESTS
require_auth = $PROXY_REQUIRE_AUTH
EOF

if [ -n "$PROXY_ALLOWED_HOSTS" ]; then
    printf 'allowed_hosts = [' >>"$CONFIG"
    first=1
    old_ifs=$IFS
    IFS=','
    for host in $PROXY_ALLOWED_HOSTS; do
        if [ $first -eq 0 ]; then
            printf ', ' >>"$CONFIG"
        fi
        printf '"%s"' "$(echo "$host" | sed 's/^ *//;s/ *$//')" >>"$CONFIG"
        first=0
    done
    IFS=$old_ifs
    printf ']\n' >>"$CONFIG"
fi

exec /app/apk-cache -config "$CONFIG"
