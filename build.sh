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
                TITLE) echo "🚀 APK Cache 构建脚本" ;;
                TRIMPATH_DISABLED) echo "⚠️  trimpath已禁用" ;;
                GOPROXY_SET) echo "🌐 GOPROXY=$2" ;;
                ALPINE_MIRROR_SET) echo "📦 设置Alpine APK源: $2" ;;
                ALPINE_MIRROR_SKIP) echo "⚠️  未找到 /etc/apk/repositories，跳过设置Alpine APK源" ;;
                DETECT_COMPRESSOR) echo "🔍 检测HTML压缩工具..." ;;
                FOUND_HTMLMINIFIER) echo "✅ 找到 html-minifier" ;;
                FOUND_MINIFY) echo "✅ 找到 minify" ;;
                FOUND_TIDY) echo "✅ 找到 tidy" ;;
                FOUND_PYHTMLMIN) echo "✅ 找到 python3 + htmlmin" ;;
                NO_COMPRESSOR) echo "⚠️  未找到HTML压缩工具，将使用简单的sed压缩" ;;
                COMPRESS_HTML) echo "📦 压缩 admin.html -> admin_min.html" ;;
                ERROR_NO_HTML) echo "❌ 错误: 找不到 $2" ;;
                COMPRESS_DONE) echo "✅ 压缩完成: $2 → $3 字节 (压缩率: $4%)" ;;
                COMPRESS_TOOL) echo "📝 使用的工具: $2" ;;
                COMPRESS_FAILED) echo "❌ 压缩失败" ;;
                GZIP_CREATE) echo "📦 创建gzip压缩版本（最高压缩率）..." ;;
                GZIP_DONE) echo "✅ Gzip压缩完成: $2 → $3 字节 (总压缩率: $4%)" ;;
                GZIP_FAILED) echo "❌ gzip不可用" ;;
                GO_BUILD_START) echo "🔨 开始Go构建..." ;;
                ERROR_NO_GO) echo "❌ 错误: 找不到Go编译器" ;;
                GO_BUILD_CMD) echo "📦 运行: $2" ;;
                GO_BUILD_SUCCESS) echo "✅ Go构建成功完成!" ;;
                BINARY_FILE) echo "📁 生成的可执行文件: $2" ;;
                BINARY_SIZE) echo "📊 可执行文件大小: $2 字节" ;;
                ERROR_NO_BINARY) echo "❌ 错误: 可执行文件未生成" ;;
                GO_BUILD_FAILED) echo "❌ Go构建失败" ;;
                BUILD_COMPLETE) echo "🎉 构建过程完成!" ;;
                BUILD_STATS) echo "📊 构建统计:" ;;
                STAT_COMPRESSOR) echo "   - HTML压缩工具: $2" ;;
                STAT_ORIGINAL_SIZE) echo "   - 原始HTML大小: $2 字节" ;;
                STAT_COMPRESSED_SIZE) echo "   - 压缩HTML大小: $2 字节" ;;
                STAT_RATIO) echo "   - 压缩率: $2%" ;;
                ERR_ARG_REQUIRES) echo "❌ 参数 $1 需要一个值" ;;
                ERR_UNKNOWN_OPTION) echo "❌ 未知选项: $1" ;;
                ERR_USE_HELP) echo "使用 $0 --help 查看可用选项" ;;
                HELP_TITLE) echo "用法: $0 [选项]" ;;
                HELP_GOPROXY) echo "  --goproxy <value>             设置GOPROXY（用于go build拉取依赖）" ;;
                HELP_ALPINE_MIRROR) echo "  --alpine-apk-mirror <url>   替换 /etc/apk/repositories 中的Alpine源（例: http://mirror/alpine）" ;;
                HELP_APK_MIRROR) echo "  --apk-mirror <url>          同 --alpine-apk-mirror" ;;
                HELP_NO_TRIMPATH) echo "  --no-trimpath              禁用Go构建的-trimpath选项" ;;
                HELP_LANG) echo "  --lang <zh|en>               设置语言（默认自动检测）" ;;
                HELP_HELP) echo "  -h, --help                  显示此帮助信息" ;;
                *) echo "$1" ;;
            esac
            ;;
        *)
            case "$1" in
                TITLE) echo "🚀 APK Cache Build Script" ;;
                TRIMPATH_DISABLED) echo "⚠️  trimpath disabled" ;;
                GOPROXY_SET) echo "🌐 GOPROXY=$2" ;;
                ALPINE_MIRROR_SET) echo "📦 Setting Alpine APK mirror: $2" ;;
                ALPINE_MIRROR_SKIP) echo "⚠️  /etc/apk/repositories not found, skipping Alpine mirror setup" ;;
                DETECT_COMPRESSOR) echo "🔍 Detecting HTML compressor..." ;;
                FOUND_HTMLMINIFIER) echo "✅ Found html-minifier" ;;
                FOUND_MINIFY) echo "✅ Found minify" ;;
                FOUND_TIDY) echo "✅ Found tidy" ;;
                FOUND_PYHTMLMIN) echo "✅ Found python3 + htmlmin" ;;
                NO_COMPRESSOR) echo "⚠️  No HTML compressor found, using simple sed" ;;
                COMPRESS_HTML) echo "📦 Compressing admin.html -> admin_min.html" ;;
                ERROR_NO_HTML) echo "❌ Error: $2 not found" ;;
                COMPRESS_DONE) echo "✅ Compression complete: $2 → $3 bytes (ratio: $4%)" ;;
                COMPRESS_TOOL) echo "📝 Compressor used: $2" ;;
                COMPRESS_FAILED) echo "❌ Compression failed" ;;
                GZIP_CREATE) echo "📦 Creating gzip compressed version (max compression)..." ;;
                GZIP_DONE) echo "✅ Gzip compression complete: $2 → $3 bytes (total ratio: $4%)" ;;
                GZIP_FAILED) echo "❌ gzip not available" ;;
                GO_BUILD_START) echo "🔨 Starting Go build..." ;;
                ERROR_NO_GO) echo "❌ Error: Go compiler not found" ;;
                GO_BUILD_CMD) echo "📦 Running: $2" ;;
                GO_BUILD_SUCCESS) echo "✅ Go build completed!" ;;
                BINARY_FILE) echo "📁 Binary generated: $2" ;;
                BINARY_SIZE) echo "📊 Binary size: $2 bytes" ;;
                ERROR_NO_BINARY) echo "❌ Error: Binary not generated" ;;
                GO_BUILD_FAILED) echo "❌ Go build failed" ;;
                BUILD_COMPLETE) echo "🎉 Build process complete!" ;;
                BUILD_STATS) echo "📊 Build statistics:" ;;
                STAT_COMPRESSOR) echo "   - HTML compressor: $2" ;;
                STAT_ORIGINAL_SIZE) echo "   - Original HTML size: $2 bytes" ;;
                STAT_COMPRESSED_SIZE) echo "   - Compressed HTML size: $2 bytes" ;;
                STAT_RATIO) echo "   - Compression ratio: $2%" ;;
                ERR_ARG_REQUIRES) echo "❌ Option $1 requires a value" ;;
                ERR_UNKNOWN_OPTION) echo "❌ Unknown option: $1" ;;
                ERR_USE_HELP) echo "Run $0 --help for usage" ;;
                HELP_TITLE) echo "Usage: $0 [options]" ;;
                HELP_GOPROXY) echo "  --goproxy <value>             Set GOPROXY (for go build)" ;;
                HELP_ALPINE_MIRROR) echo "  --alpine-apk-mirror <url>   Replace Alpine mirror in /etc/apk/repositories (e.g. http://mirror/alpine)" ;;
                HELP_APK_MIRROR) echo "  --apk-mirror <url>          Alias for --alpine-apk-mirror" ;;
                HELP_NO_TRIMPATH) echo "  --no-trimpath               Disable -trimpath for Go build" ;;
                HELP_LANG) echo "  --lang <zh|en>               Set language (default: auto-detect)" ;;
                HELP_HELP) echo "  -h, --help                  Show this help message" ;;
                *) echo "$1" ;;
            esac
            ;;
    esac
}

# ========================================
# 默认配置 / Default Configuration
# ========================================
TRIMPATH_ENABLED=true
CUSTOM_GOPROXY=""
ALPINE_APK_MIRROR=""

# 解析命令行参数
while [ $# -gt 0 ]; do
    case $1 in
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
        --no-trimpath)
            TRIMPATH_ENABLED=false
            shift
            ;;
        --lang)
            if [ $# -lt 2 ]; then
                echo $(t "ERR_ARG_REQUIRES" "$1")
                exit 1
            fi
            CURRENT_LANG="$2"
            shift 2
            ;;
        -h|--help)
            echo $(t "HELP_TITLE")
            echo ""
            echo "Options:"
            echo $(t "HELP_GOPROXY")
            echo $(t "HELP_ALPINE_MIRROR")
            echo $(t "HELP_APK_MIRROR")
            echo $(t "HELP_NO_TRIMPATH")
            echo $(t "HELP_LANG")
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

echo $(t "TITLE")
echo "=========================="
if [ "$TRIMPATH_ENABLED" = "false" ]; then
    echo $(t "TRIMPATH_DISABLED")
fi
if [ -n "$CUSTOM_GOPROXY" ]; then
    export GOPROXY="$CUSTOM_GOPROXY"
    echo $(t "GOPROXY_SET" "$GOPROXY")
fi
if [ -n "$ALPINE_APK_MIRROR" ]; then
    if [ -f /etc/apk/repositories ]; then
        echo $(t "ALPINE_MIRROR_SET" "$ALPINE_APK_MIRROR")
        sed -i "s|https\\?://dl-cdn\\.alpinelinux\\.org/alpine|$ALPINE_APK_MIRROR|g" /etc/apk/repositories
    else
        echo $(t "ALPINE_MIRROR_SKIP")
    fi
fi

# 检查HTML压缩工具
echo ""
echo $(t "DETECT_COMPRESSOR")

HTML_COMPRESSOR=""
HTML_COMPRESSOR_CMD=""

# 检查是否存在html-minifier (Node.js)
if command -v html-minifier &> /dev/null; then
    echo $(t "FOUND_HTMLMINIFIER")
    HTML_COMPRESSOR="html-minifier"
    HTML_COMPRESSOR_CMD="html-minifier --collapse-whitespace --remove-comments --remove-optional-tags --remove-redundant-attributes --remove-script-type-attributes --remove-tag-whitespace --use-short-doctype --minify-css true --minify-js true"
# 检查是否存在minify (Go)
elif command -v minify &> /dev/null; then
    echo $(t "FOUND_MINIFY")
    HTML_COMPRESSOR="minify"
    HTML_COMPRESSOR_CMD="minify --html-keep-document-tags --html-keep-end-tags"
# 检查是否存在tidy
elif command -v tidy &> /dev/null; then
    echo $(t "FOUND_TIDY")
    HTML_COMPRESSOR="tidy"
    HTML_COMPRESSOR_CMD="tidy -q -omit -ashtml --show-errors 0 --show-warnings 0 --clean yes --drop-empty-elements yes --drop-empty-paras yes --hide-comments yes --merge-divs yes --merge-spans yes --output-xhtml yes --wrap 0"
# 检查是否存在python3和htmlmin
elif command -v python3 &> /dev/null && python3 -c "import htmlmin" 2>/dev/null; then
    echo $(t "FOUND_PYHTMLMIN")
    HTML_COMPRESSOR="python-htmlmin"
    HTML_COMPRESSOR_CMD="python3 -c \"import htmlmin, sys; print(htmlmin.minify(sys.stdin.read(), remove_comments=True, remove_empty_space=True))\""
else
    echo $(t "NO_COMPRESSOR")
    HTML_COMPRESSOR="sed"
fi

# 压缩admin.html
echo ""
echo $(t "COMPRESS_HTML")

ADMIN_HTML="cmd/apk-cache/admin.html"
ADMIN_MIN_HTML="cmd/apk-cache/admin_min.html"

if [ ! -f "$ADMIN_HTML" ]; then
    echo $(t "ERROR_NO_HTML" "$ADMIN_HTML")
    exit 1
fi

case $HTML_COMPRESSOR in
    "html-minifier")
        $HTML_COMPRESSOR_CMD "$ADMIN_HTML" > "$ADMIN_MIN_HTML"
        ;;
    "minify")
        $HTML_COMPRESSOR_CMD "$ADMIN_HTML" > "$ADMIN_MIN_HTML"
        ;;
    "tidy")
        $HTML_COMPRESSOR_CMD "$ADMIN_HTML" > "$ADMIN_MIN_HTML" 2>/dev/null || true
        ;;
    "python-htmlmin")
        $HTML_COMPRESSOR_CMD < "$ADMIN_HTML" > "$ADMIN_MIN_HTML"
        ;;
    "sed")
        # 使用sed进行基本压缩
        if command -v sed &> /dev/null; then
            sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e '/^$/d' -e 's|[[:space:]]*<|>|g' "$ADMIN_HTML" > "$ADMIN_MIN_HTML"
        else
            echo $(t "NO_COMPRESSOR")
            cp "$ADMIN_HTML" "$ADMIN_MIN_HTML"
        fi
        ;;
    *)
        echo $(t "COMPRESS_FAILED")
        exit 1
        ;;
esac

# 检查压缩是否成功
if [ -f "$ADMIN_MIN_HTML" ] && [ -s "$ADMIN_MIN_HTML" ]; then
    ORIGINAL_SIZE=$(stat -c%s "$ADMIN_HTML" 2>/dev/null || stat -f%z "$ADMIN_HTML")
    COMPRESSED_SIZE=$(stat -c%s "$ADMIN_MIN_HTML" 2>/dev/null || stat -f%z "$ADMIN_MIN_HTML")
    COMPRESSION_RATIO=$(echo "scale=2; (1 - $COMPRESSED_SIZE / $ORIGINAL_SIZE) * 100" | bc)

    echo $(t "COMPRESS_DONE" "$ORIGINAL_SIZE" "$COMPRESSED_SIZE" "$COMPRESSION_RATIO")
    echo $(t "COMPRESS_TOOL" "$HTML_COMPRESSOR")
else
    echo $(t "COMPRESS_FAILED")
    exit 1
fi

# 创建gzip压缩版本
echo ""
echo $(t "GZIP_CREATE")
ADMIN_MIN_HTML_GZ="$ADMIN_MIN_HTML.gz"

if command -v gzip &> /dev/null; then
    gzip -9 -c "$ADMIN_MIN_HTML" > "$ADMIN_MIN_HTML_GZ"
    GZIP_SIZE=$(stat -c%s "$ADMIN_MIN_HTML_GZ" 2>/dev/null || stat -f%z "$ADMIN_MIN_HTML_GZ")
    GZIP_RATIO=$(echo "scale=2; (1 - $GZIP_SIZE / $ORIGINAL_SIZE) * 100" | bc)
    echo $(t "GZIP_DONE" "$ORIGINAL_SIZE" "$GZIP_SIZE" "$GZIP_RATIO")
else
    echo $(t "GZIP_FAILED")
    exit 1
fi

echo ""
echo $(t "GO_BUILD_START")

# 检查Go是否安装
GO_CMD=""
if command -v go &> /dev/null; then
    GO_CMD="go"
else
    # 尝试从已知路径查找
    for go_path in "$HOME/go/bin/go" "/usr/local/go/bin/go" "/opt/hostedtoolcache/go/1.25/x64/bin/go" "/opt/hostedtoolcache/go/1.25.7/x64/bin/go"; do
        if [ -x "$go_path" ]; then
            GO_CMD="$go_path"
            break
        fi
    done
fi

if [ -z "$GO_CMD" ]; then
    echo $(t "ERROR_NO_GO")
    exit 1
fi

# 构建Go命令
GO_BUILD_CMD="$GO_CMD build -ldflags=\"-s -w\""
if [ "$TRIMPATH_ENABLED" = "true" ]; then
    GO_BUILD_CMD="$GO_BUILD_CMD -trimpath"
fi
GO_BUILD_CMD="$GO_BUILD_CMD -o apk-cache ./cmd/apk-cache"

# 执行Go构建
echo $(t "GO_BUILD_CMD" "$GO_BUILD_CMD")
if eval $GO_BUILD_CMD; then
    echo $(t "GO_BUILD_SUCCESS")
    echo $(t "BINARY_FILE" "apk-cache")

    # 显示构建产物信息
    if [ -f "apk-cache" ]; then
        BINARY_SIZE=$(stat -c%s "apk-cache" 2>/dev/null || stat -f%z "apk-cache")
        echo $(t "BINARY_SIZE" "$BINARY_SIZE")
    else
        echo $(t "ERROR_NO_BINARY")
        exit 1
    fi
else
    echo $(t "GO_BUILD_FAILED")
    exit 1
fi

echo ""
echo $(t "BUILD_COMPLETE")
echo $(t "BUILD_STATS")
echo $(t "STAT_COMPRESSOR" "$HTML_COMPRESSOR")
echo $(t "STAT_ORIGINAL_SIZE" "$ORIGINAL_SIZE")
echo $(t "STAT_COMPRESSED_SIZE" "$COMPRESSED_SIZE")
echo $(t "STAT_RATIO" "$COMPRESSION_RATIO")
