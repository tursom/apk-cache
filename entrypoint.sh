#!/bin/sh
set -e

# 读取环境变量，使用默认值
ADDR=${ADDR:-:80}
CACHE_DIR=${CACHE_DIR:-./cache}
INDEX_CACHE=${INDEX_CACHE:-24h}
PROXY=${PROXY:-}
UPSTREAM=${UPSTREAM:-}

# 构建参数
ARGS="-addr $ADDR -cache $CACHE_DIR -index-cache $INDEX_CACHE"

# 如果 PROXY 不为空，添加 -proxy 参数
if [ -n "$PROXY" ]; then
    ARGS="$ARGS -proxy $PROXY"
fi

# 如果 UPSTREAM 不为空，添加 -upstream 参数
if [ -n "$UPSTREAM" ]; then
    ARGS="$ARGS -upstream $UPSTREAM"
fi

# 启动应用
exec /app/apk-cache $ARGS
