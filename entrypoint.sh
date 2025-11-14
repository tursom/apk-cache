#!/bin/sh
set -e

# 读取环境变量，使用默认值
ADDR=${ADDR:-:3142}
CACHE_DIR=${CACHE_DIR:-./cache}
INDEX_CACHE=${INDEX_CACHE:-24h}
PKG_CACHE=${PKG_CACHE:-0}
CLEANUP_INTERVAL=${CLEANUP_INTERVAL:-1h}
PROXY=${PROXY:-}
UPSTREAM=${UPSTREAM:-https://dl-cdn.alpinelinux.org}
LOCALE=${LOCALE:-}
ADMIN_USER=${ADMIN_USER:-admin}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-}
CONFIG=${CONFIG:-}
CACHE_MAX_SIZE=${CACHE_MAX_SIZE:-}
CACHE_CLEAN_STRATEGY=${CACHE_CLEAN_STRATEGY:-}

# 新增：内存缓存相关环境变量
MEMORY_CACHE_ENABLED=${MEMORY_CACHE_ENABLED:-false}
MEMORY_CACHE_SIZE=${MEMORY_CACHE_SIZE:-100MB}
MEMORY_CACHE_MAX_ITEMS=${MEMORY_CACHE_MAX_ITEMS:-1000}
MEMORY_CACHE_TTL=${MEMORY_CACHE_TTL:-30m}
MEMORY_CACHE_MAX_FILE_SIZE=${MEMORY_CACHE_MAX_FILE_SIZE:-10MB}

# 新增：健康检查相关环境变量
HEALTH_CHECK_INTERVAL=${HEALTH_CHECK_INTERVAL:-30s}
HEALTH_CHECK_TIMEOUT=${HEALTH_CHECK_TIMEOUT:-10s}
ENABLE_SELF_HEALING=${ENABLE_SELF_HEALING:-true}

# 新增：请求限流相关环境变量
RATE_LIMIT_ENABLED=${RATE_LIMIT_ENABLED:-false}
RATE_LIMIT_RATE=${RATE_LIMIT_RATE:-100}
RATE_LIMIT_BURST=${RATE_LIMIT_BURST:-200}
RATE_LIMIT_EXEMPT_PATHS=${RATE_LIMIT_EXEMPT_PATHS:-/_health}

# 构建参数
ARGS="-addr $ADDR -cache $CACHE_DIR -index-cache $INDEX_CACHE -pkg-cache $PKG_CACHE -cleanup-interval $CLEANUP_INTERVAL"

# 如果 PROXY 不为空，添加 -proxy 参数
if [ -n "$PROXY" ]; then
    ARGS="$ARGS -proxy $PROXY"
fi

# 如果 UPSTREAM 不为空，添加 -upstream 参数
if [ -n "$UPSTREAM" ]; then
    ARGS="$ARGS -upstream $UPSTREAM"
fi

# 如果 LOCALE 不为空，添加 -locale 参数
if [ -n "$LOCALE" ]; then
    ARGS="$ARGS -locale $LOCALE"
fi

# 添加 -admin-user 参数
ARGS="$ARGS -admin-user $ADMIN_USER"

# 如果 ADMIN_PASSWORD 不为空，添加 -admin-password 参数
if [ -n "$ADMIN_PASSWORD" ]; then
    ARGS="$ARGS -admin-password $ADMIN_PASSWORD"
fi

# 如果 CONFIG 不为空，添加 -config 参数
if [ -n "$CONFIG" ]; then
    ARGS="$ARGS -config $CONFIG"
fi

# 如果 CACHE_MAX_SIZE 不为空，添加 -cache-max-size 参数
if [ -n "$CACHE_MAX_SIZE" ]; then
    ARGS="$ARGS -cache-max-size $CACHE_MAX_SIZE"
fi

# 如果 CACHE_CLEAN_STRATEGY 不为空，添加 -cache-clean-strategy 参数
if [ -n "$CACHE_CLEAN_STRATEGY" ]; then
    ARGS="$ARGS -cache-clean-strategy $CACHE_CLEAN_STRATEGY"
fi

# 新增：内存缓存相关参数
if [ "$MEMORY_CACHE_ENABLED" = "true" ]; then
    ARGS="$ARGS -memory-cache"
fi

# 如果 MEMORY_CACHE_SIZE 不为空，添加 -memory-cache-size 参数
if [ -n "$MEMORY_CACHE_SIZE" ]; then
    ARGS="$ARGS -memory-cache-size $MEMORY_CACHE_SIZE"
fi

# 如果 MEMORY_CACHE_MAX_ITEMS 不为空，添加 -memory-cache-max-items 参数
if [ -n "$MEMORY_CACHE_MAX_ITEMS" ]; then
    ARGS="$ARGS -memory-cache-max-items $MEMORY_CACHE_MAX_ITEMS"
fi

# 如果 MEMORY_CACHE_TTL 不为空，添加 -memory-cache-ttl 参数
if [ -n "$MEMORY_CACHE_TTL" ]; then
    ARGS="$ARGS -memory-cache-ttl $MEMORY_CACHE_TTL"
fi

# 如果 MEMORY_CACHE_MAX_FILE_SIZE 不为空，添加 -memory-cache-max-file-size 参数
if [ -n "$MEMORY_CACHE_MAX_FILE_SIZE" ]; then
    ARGS="$ARGS -memory-cache-max-file-size $MEMORY_CACHE_MAX_FILE_SIZE"
fi

# 新增：健康检查相关参数
# 如果 HEALTH_CHECK_INTERVAL 不为空，添加 -health-check-interval 参数
if [ -n "$HEALTH_CHECK_INTERVAL" ]; then
    ARGS="$ARGS -health-check-interval $HEALTH_CHECK_INTERVAL"
fi

# 如果 HEALTH_CHECK_TIMEOUT 不为空，添加 -health-check-timeout 参数
if [ -n "$HEALTH_CHECK_TIMEOUT" ]; then
    ARGS="$ARGS -health-check-timeout $HEALTH_CHECK_TIMEOUT"
fi

# 如果 ENABLE_SELF_HEALING 不为空，添加 -enable-self-healing 参数
if [ -n "$ENABLE_SELF_HEALING" ]; then
    ARGS="$ARGS -enable-self-healing $ENABLE_SELF_HEALING"
fi

# 新增：请求限流相关参数
# 如果 RATE_LIMIT_ENABLED 为 true，添加 -rate-limit 参数
if [ "$RATE_LIMIT_ENABLED" = "true" ]; then
    ARGS="$ARGS -rate-limit"
fi

# 如果 RATE_LIMIT_RATE 不为空，添加 -rate-limit-rate 参数
if [ -n "$RATE_LIMIT_RATE" ]; then
    ARGS="$ARGS -rate-limit-rate $RATE_LIMIT_RATE"
fi

# 如果 RATE_LIMIT_BURST 不为空，添加 -rate-limit-burst 参数
if [ -n "$RATE_LIMIT_BURST" ]; then
    ARGS="$ARGS -rate-limit-burst $RATE_LIMIT_BURST"
fi

# 如果 RATE_LIMIT_EXEMPT_PATHS 不为空，添加 -rate-limit-exempt-paths 参数
if [ -n "$RATE_LIMIT_EXEMPT_PATHS" ]; then
    ARGS="$ARGS -rate-limit-exempt-paths $RATE_LIMIT_EXEMPT_PATHS"
fi

# 启动应用
exec /app/apk-cache $ARGS
