#!/bin/sh

set -e

# ========================================
# 国际化支持 / Internationalization
# ========================================
# 检测语言 / Detect language
detect_lang() {
    # 如果指定了语言参数，优先使用
    if [ -n "$LANG" ]; then
        echo "$LANG"
        return
    fi
    # 检测系统语言
    case "${LANG:-${LC_ALL:-}}" in
        zh*|cn*)
            echo "zh"
            ;;
        *)
            echo "en"
            ;;
    esac
}

# 当前语言
CURRENT_LANG=$(detect_lang)

# 翻译函数
t() {
    case "$CURRENT_LANG" in
        zh)
            case "$1" in
                TITLE) echo "APK Cache 集成测试" ;;
                STEP1_TITLE) echo "步骤1: 使用 build.sh 打包到本地" ;;
                STEP1_BUILDING) echo "正在执行 build.sh..." ;;
                STEP1_DONE) echo "✅ build.sh 执行完成" ;;
                STEP2_TITLE) echo "步骤2: 构建 Docker 镜像" ;;
                STEP2_BUILDING) echo "正在构建 Docker 镜像..." ;;
                STEP2_SKIP) echo "跳过 Docker 镜像构建 (使用指定镜像)" ;;
                STEP2_DONE) echo "✅ Docker 镜像构建完成: $2" ;;
                STEP3_TITLE) echo "步骤3: 启动 apk-cache 服务" ;;
                STEP3_STARTING) echo "正在启动服务..." ;;
                STEP3_DONE) echo "✅ 服务已启动: $2" ;;
                STEP3_WAITING) echo "⏳ 等待服务启动..." ;;
                STEP3_RUNNING) echo "✅ 服务正常运行" ;;
                STEP3_NON_HTTP) echo "✅ 服务已启动 (非HTTP模式)" ;;
                STEP4_TITLE) echo "步骤4: 启动 alpine:latest 测试客户端" ;;
                STEP4_ALPINE_REPO) echo "--- 当前 apk 源 ---" ;;
                STEP4_ALPINE_REPLACE) echo "--- 替换为 apk-cache 源 ---" ;;
                STEP4_ALPINE_UPDATE) echo "--- 执行 apk update ---" ;;
                STEP4_ALPINE_DONE) echo "✅ apk update 成功!" ;;
                STEP5_TITLE) echo "步骤5: 启动 debian:latest 测试客户端" ;;
                STEP5_APT_CONFIG) echo "--- 配置 APT 使用 HTTP 代理 ---" ;;
                STEP5_APT_UPDATE) echo "--- 执行 apt-get update ---" ;;
                STEP5_APT_DONE) echo "✅ apt-get update 成功!" ;;
                RESULT_TITLE) echo "🎉 所有测试步骤完成!" ;;
                RESULT_ERRORS_TITLE) echo "❌ 测试错误:" ;;
                RESULT_BUILD) echo "  - build.sh 打包: ✅ 成功" ;;
                RESULT_DOCKER) echo "  - Docker 镜像构建: ✅ 成功" ;;
                RESULT_SERVICE) echo "  - 服务启动: ✅ 成功" ;;
                RESULT_ALPINE) echo "  - 客户端 apk update: ✅ 成功" ;;
                RESULT_APT) echo "  - 客户端 apt-get update: ✅ 成功" ;;
                SERVICE_LOCAL) echo "  - 本地访问: http://localhost:$2" ;;
                SERVICE_CONTAINER) echo "  - 容器内访问: http://$2:3142" ;;
                CLEANUP) echo "🧹 清理环境..." ;;
                ERR_ARG_REQUIRES) echo "❌ 参数 $1 需要一个值" ;;
                ERR_UNKNOWN_OPTION) echo "❌ 未知选项: $1" ;;
                ERR_USE_HELP) echo "使用 $0 --help 查看可用选项" ;;
                HELP_TITLE) echo "用法: $0 [选项]" ;;
                HELP_IMAGE) echo "  --image <name>               设置要测试的 Docker 镜像名称" ;;
                HELP_GOPROXY) echo "  --goproxy <value>             设置GOPROXY（用于go build拉取依赖）" ;;
                HELP_ALPINE_MIRROR) echo "  --alpine-apk-mirror <url>   Docker构建/本地构建时使用的Alpine源（例: http://mirror/alpine）" ;;
                HELP_APK_MIRROR) echo "  --apk-mirror <url>          同 --alpine-apk-mirror" ;;
                HELP_LANG) echo "  --lang <zh|en>               设置语言（默认自动检测）" ;;
                HELP_PROXY) echo "  --proxy <url>                设置上游HTTP/SOCKS5代理" ;;
                HELP_HELP) echo "  -h, --help                   显示此帮助信息" ;;
                *) echo "$1" ;;
            esac
            ;;
        *)
            case "$1" in
                TITLE) echo "APK Cache Integration Test" ;;
                STEP1_TITLE) echo "Step 1: Build locally using build.sh" ;;
                STEP1_BUILDING) echo "Running build.sh..." ;;
                STEP1_DONE) echo "✅ build.sh completed" ;;
                STEP2_TITLE) echo "Step 2: Build Docker image" ;;
                STEP2_BUILDING) echo "Building Docker image..." ;;
                STEP2_SKIP) echo "Skipping Docker build (using specified image)" ;;
                STEP2_DONE) echo "✅ Docker image built: $2" ;;
                STEP3_TITLE) echo "Step 3: Start apk-cache service" ;;
                STEP3_STARTING) echo "Starting service..." ;;
                STEP3_DONE) echo "✅ Service started: $2" ;;
                STEP3_WAITING) echo "⏳ Waiting for service..." ;;
                STEP3_RUNNING) echo "✅ Service is running" ;;
                STEP3_NON_HTTP) echo "✅ Service started (non-HTTP mode)" ;;
                STEP4_TITLE) echo "Step 4: Test with alpine:latest client" ;;
                STEP4_ALPINE_REPO) echo "--- Current apk repositories ---" ;;
                STEP4_ALPINE_REPLACE) echo "--- Replacing with apk-cache source ---" ;;
                STEP4_ALPINE_UPDATE) echo "--- Running apk update ---" ;;
                STEP4_ALPINE_DONE) echo "✅ apk update succeeded!" ;;
                STEP5_TITLE) echo "Step 5: Test with debian:latest client" ;;
                STEP5_APT_CONFIG) echo "--- Configure APT HTTP proxy ---" ;;
                STEP5_APT_UPDATE) echo "--- Running apt-get update ---" ;;
                STEP5_APT_DONE) echo "✅ apt-get update succeeded!" ;;
                RESULT_TITLE) echo "🎉 All tests completed!" ;;
                RESULT_ERRORS_TITLE) echo "❌ Test Errors:" ;;
                RESULT_BUILD) echo "  - build.sh: ✅ Success" ;;
                RESULT_DOCKER) echo "  - Docker image: ✅ Success" ;;
                RESULT_SERVICE) echo "  - Service start: ✅ Success" ;;
                RESULT_ALPINE) echo "  - Alpine apk update: ✅ Success" ;;
                RESULT_APT) echo "  - Debian apt-get update: ✅ Success" ;;
                SERVICE_LOCAL) echo "  - Local access: http://localhost:$2" ;;
                SERVICE_CONTAINER) echo "  - Container access: http://$2:3142" ;;
                CLEANUP) echo "🧹 Cleaning up..." ;;
                ERR_ARG_REQUIRES) echo "❌ Option $1 requires a value" ;;
                ERR_UNKNOWN_OPTION) echo "❌ Unknown option: $1" ;;
                ERR_USE_HELP) echo "Run $0 --help for usage" ;;
                HELP_TITLE) echo "Usage: $0 [options]" ;;
                HELP_IMAGE) echo "  --image <name>               Set Docker image name to test" ;;
                HELP_GOPROXY) echo "  --goproxy <value>             Set GOPROXY (for go build)" ;;
                HELP_ALPINE_MIRROR) echo "  --alpine-apk-mirror <url>   Alpine mirror for build (e.g. http://mirror/alpine)" ;;
                HELP_APK_MIRROR) echo "  --apk-mirror <url>          Alias for --alpine-apk-mirror" ;;
                HELP_LANG) echo "  --lang <zh|en>               Set language (default: auto-detect)" ;;
                HELP_PROXY) echo "  --proxy <url>                Set upstream HTTP/SOCKS5 proxy" ;;
                HELP_HELP) echo "  -h, --help                   Show this help message" ;;
                *) echo "$1" ;;
            esac
            ;;
    esac
}

# ========================================
# 配置 / Configuration
# ========================================
IMAGE_NAME="apk-cache-test:latest"
CONTAINER_NAME="apk-cache-server"
TEST_CONTAINER_NAME="apk-cache-client"
CACHE_DIR="/tmp/apk-cache-test-cache"
PORT=3142

# 错误记录
ERRORS=()

# 记录错误的函数
record_error() {
    ERRORS+=("$1")
}

CUSTOM_GOPROXY=""
ALPINE_APK_MIRROR=""
USE_CUSTOM_IMAGE=""
CUSTOM_PROXY=""

# 解析命令行参数
while [ $# -gt 0 ]; do
    case $1 in
        --image)
            if [ $# -lt 2 ]; then
                echo $(t "ERR_ARG_REQUIRES" "$1")
                exit 1
            fi
            IMAGE_NAME="$2"
            USE_CUSTOM_IMAGE="true"
            shift 2
            ;;
        --goproxy)
            if [ $# -lt 2 ]; then
                echo $(t "ERR_ARG_REQUIRES" "$1")
                exit 1
            fi
            CUSTOM_GOPROXY="$2"
            shift 2
            ;;
        --alpine-apk-mirror|--apk-mirror)
            if [ $# -lt 2 ]; then
                echo $(t "ERR_ARG_REQUIRES" "$1")
                exit 1
            fi
            ALPINE_APK_MIRROR="$2"
            shift 2
            ;;
        --lang)
            if [ $# -lt 2 ]; then
                echo $(t "ERR_ARG_REQUIRES" "$1")
                exit 1
            fi
            CURRENT_LANG="$2"
            shift 2
            ;;
        --proxy)
            if [ $# -lt 2 ]; then
                echo $(t "ERR_ARG_REQUIRES" "$1")
                exit 1
            fi
            CUSTOM_PROXY="$2"
            shift 2
            ;;
        -h|--help)
            echo $(t "HELP_TITLE")
            echo ""
            echo "Options:"
            echo $(t "HELP_IMAGE")
            echo $(t "HELP_GOPROXY")
            echo $(t "HELP_ALPINE_MIRROR")
            echo $(t "HELP_APK_MIRROR")
            echo $(t "HELP_LANG")
            echo $(t "HELP_PROXY")
            echo $(t "HELP_HELP")
            echo ""
            exit 0
            ;;
        *)
            echo $(t "ERR_UNKNOWN_OPTION" "$1")
            echo $(t "ERR_USE_HELP")
            exit 1
            ;;
    esac
done

# 透传到 build.sh / docker build 的参数
BUILD_SH_ARGS=""
DOCKER_BUILD_ARGS=""
if [ -n "$CUSTOM_GOPROXY" ]; then
    BUILD_SH_ARGS="$BUILD_SH_ARGS --goproxy \"$CUSTOM_GOPROXY\""
    DOCKER_BUILD_ARGS="$DOCKER_BUILD_ARGS --build-arg GOPROXY=$CUSTOM_GOPROXY"
fi
if [ -n "$ALPINE_APK_MIRROR" ]; then
    BUILD_SH_ARGS="$BUILD_SH_ARGS --alpine-apk-mirror \"$ALPINE_APK_MIRROR\""
    DOCKER_BUILD_ARGS="$DOCKER_BUILD_ARGS --build-arg ALPINE_APK_MIRROR=$ALPINE_APK_MIRROR"
fi
if [ -n "$CURRENT_LANG" ] && [ "$CURRENT_LANG" != "auto" ]; then
    BUILD_SH_ARGS="$BUILD_SH_ARGS --lang \"$CURRENT_LANG\""
fi

# 清理函数
cleanup() {
    echo ""
    echo $(t "CLEANUP")
    docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
    docker rm -f "$TEST_CONTAINER_NAME" 2>/dev/null || true
    # Only remove the image if we built it ourselves
    if [ "$USE_CUSTOM_IMAGE" != "true" ]; then
        docker rmi "$IMAGE_NAME" 2>/dev/null || true
    fi
}

# 确保清理
trap cleanup EXIT

# ========================================
# 测试步骤 / Test Steps
# ========================================

# 标题
echo "========================================"
echo $(t "TITLE")
echo "========================================"

# 步骤1: 使用 build.sh 打包到本地
echo ""
echo "========================================"
echo $(t "STEP1_TITLE")
echo "========================================"
echo $(t "STEP1_BUILDING")
cd .
if [ -n "$BUILD_SH_ARGS" ]; then
    eval "sh build.sh $BUILD_SH_ARGS"
else
    sh build.sh
fi
echo $(t "STEP1_DONE")

# 步骤2: 构建 Docker 镜像 (如果未指定自定义镜像)
echo ""
echo "========================================"
if [ "$USE_CUSTOM_IMAGE" = "true" ]; then
    echo $(t "STEP2_SKIP")
else
    echo $(t "STEP2_TITLE")
    echo "========================================"
    echo $(t "STEP2_BUILDING")
    if [ -n "$DOCKER_BUILD_ARGS" ]; then
        eval "docker build $DOCKER_BUILD_ARGS -t \"$IMAGE_NAME\" ."
    else
        docker build -t "$IMAGE_NAME" .
    fi
    echo $(t "STEP2_DONE" "$IMAGE_NAME")
fi

# 创建缓存目录
mkdir -p "$CACHE_DIR"

# 步骤3: 启动镜像
echo ""
echo "========================================"
echo $(t "STEP3_TITLE")
echo "========================================"
echo $(t "STEP3_STARTING")

# 构建 docker run 命令
DOCKER_RUN_ARGS="docker run -d \
    --name \"$CONTAINER_NAME\" \
    -p \"${PORT}:3142\" \
    -v \"$CACHE_DIR:/app/cache\" \
    -e \"ADDR=:3142\" \
    -e \"CACHE_DIR=/app/cache\""

# 如果指定了代理，添加到环境变量
if [ -n "$CUSTOM_PROXY" ]; then
    DOCKER_RUN_ARGS="$DOCKER_RUN_ARGS \
    -e \"PROXY=$CUSTOM_PROXY\""
fi

DOCKER_RUN_ARGS="$DOCKER_RUN_ARGS \"$IMAGE_NAME\""

# 执行 docker run
eval "$DOCKER_RUN_ARGS"

echo $(t "STEP3_DONE" "$CONTAINER_NAME")

# 等待服务启动
echo $(t "STEP3_WAITING")
sleep 3

# 检查服务是否正常运行
if docker exec "$CONTAINER_NAME" sh -c "wget -q -O /dev/null http://localhost:3142/ 2>&1 || true" | grep -q "200\|OK"; then
    echo $(t "STEP3_RUNNING")
else
    echo $(t "STEP3_NON_HTTP")
fi

# 步骤4: 启动 alpine:latest 镜像测试
echo ""
echo "========================================"
echo $(t "STEP4_TITLE")
echo "========================================"

# 启动测试容器并执行 apk update
# apk-cache 返回错误时应该直接报错，而不是掩盖问题
docker run --rm \
    --name "$TEST_CONTAINER_NAME" \
    --link "$CONTAINER_NAME" \
    alpine:latest sh -c "
        echo '$(t STEP4_ALPINE_REPO)'
        cat /etc/apk/repositories
        echo ''
        echo '$(t STEP4_ALPINE_REPLACE)'
        sed -i 's|https://dl-cdn.alpinelinux.org|http://apk-cache-server:3142|g' /etc/apk/repositories
        cat /etc/apk/repositories
        echo ''
        echo '$(t STEP4_ALPINE_UPDATE)'
        
        # 捕获 apk update 的输出和退出码
        APK_UPDATE_OUTPUT=\$(apk update 2>&1)
        APK_UPDATE_EXIT=\$?
        echo \"\$APK_UPDATE_OUTPUT\"
        
        # 检查是否有错误，记录错误标记供宿主机捕获
        if [ \$APK_UPDATE_EXIT -ne 0 ]; then
            echo 'ERROR:APK_UPDATE_EXIT_CODE:\$APK_UPDATE_EXIT'
        fi
        
        # 检查输出中是否包含错误关键词
        if echo \"\$APK_UPDATE_OUTPUT\" | grep -qiE 'HTTP [45]|error|failed|unable|unavailable'; then
            echo 'ERROR:APK_UPDATE_CONTAINS_ERROR'
        fi
        
        echo ''
        echo '$(t STEP4_ALPINE_DONE)'
    "

# 检查 apk update 是否有错误
if [ $? -ne 0 ] || docker logs "$TEST_CONTAINER_NAME" 2>&1 | grep -qE "ERROR:APK_UPDATE"; then
    # 提取错误信息
    APK_ERRORS=$(docker logs "$TEST_CONTAINER_NAME" 2>&1 | grep "ERROR:APK_UPDATE" | head -5)
    if [ -n "$APK_ERRORS" ]; then
        record_error "Alpine apk update failed: $APK_ERRORS"
    else
        record_error "Alpine apk update failed"
    fi
fi

# 步骤5: 启动 debian 测试客户端
echo ""
echo "========================================"
echo $(t "STEP5_TITLE")
echo "========================================"

TEST_CONTAINER_NAME_DEB="apk-cache-client-deb"
docker run --rm \
    --name "$TEST_CONTAINER_NAME_DEB" \
    --link "$CONTAINER_NAME" \
    debian:latest sh -c "
        echo '$(t STEP5_APT_CONFIG)'
        echo 'Acquire::HTTP::Proxy \"http://apk-cache-server:3142\";' > /etc/apt/apt.conf.d/01proxy
        echo 'Acquire::HTTPS::Proxy \"http://apk-cache-server:3142\";' >> /etc/apt/apt.conf.d/01proxy
        cat /etc/apt/apt.conf.d/01proxy
        echo ''
        echo '$(t STEP5_APT_UPDATE)'
        apt-get update
        echo ''
        echo '$(t STEP5_APT_DONE)'
    "

# 清理 Debian 测试容器名称记录（避免 cleanup 函数尝试清理不存在的容器）
TEST_CONTAINER_NAME_DEB=""

echo ""
echo "========================================"
echo $(t "RESULT_TITLE")
echo "========================================"
echo ""
echo $(t "RESULT_BUILD")
echo $(t "RESULT_DOCKER")
echo $(t "RESULT_SERVICE")
echo $(t "RESULT_ALPINE")
echo $(t "RESULT_APT")

# 输出错误统计
if [ ${#ERRORS[@]} -gt 0 ]; then
    echo ""
    echo "========================================"
    echo $(t "RESULT_ERRORS_TITLE")
    echo "========================================"
    for i in "${!ERRORS[@]}"; do
        echo "  $((i+1)). ${ERRORS[$i]}"
    done
    echo ""
    echo "Total errors: ${#ERRORS[@]}"
fi

echo ""
echo $(t "SERVICE_LOCAL" "$PORT")
echo $(t "SERVICE_CONTAINER" "$CONTAINER_NAME")
echo ""
