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

# 启动应用
exec /app/apk-cache $ARGS
