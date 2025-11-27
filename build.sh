#!/bin/sh

set -e

echo "🚀 APK Cache Build Script"
echo "=========================="

# 检查HTML压缩工具
echo "🔍 检测HTML压缩工具..."

HTML_COMPRESSOR=""
HTML_COMPRESSOR_CMD=""

# 检查是否存在html-minifier (Node.js)
if command -v html-minifier &> /dev/null; then
    echo "✅ 找到 html-minifier"
    HTML_COMPRESSOR="html-minifier"
    HTML_COMPRESSOR_CMD="html-minifier --collapse-whitespace --remove-comments --remove-optional-tags --remove-redundant-attributes --remove-script-type-attributes --remove-tag-whitespace --use-short-doctype --minify-css true --minify-js true"
# 检查是否存在minify (Go)
elif command -v minify &> /dev/null; then
    echo "✅ 找到 minify"
    HTML_COMPRESSOR="minify"
    HTML_COMPRESSOR_CMD="minify --html-keep-document-tags --html-keep-end-tags"
# 检查是否存在tidy
elif command -v tidy &> /dev/null; then
    echo "✅ 找到 tidy"
    HTML_COMPRESSOR="tidy"
    HTML_COMPRESSOR_CMD="tidy -q -omit -ashtml --show-errors 0 --show-warnings 0 --clean yes --drop-empty-elements yes --drop-empty-paras yes --hide-comments yes --merge-divs yes --merge-spans yes --output-xhtml yes --wrap 0"
# 检查是否存在python3和htmlmin
elif command -v python3 &> /dev/null && python3 -c "import htmlmin" 2>/dev/null; then
    echo "✅ 找到 python3 + htmlmin"
    HTML_COMPRESSOR="python-htmlmin"
    HTML_COMPRESSOR_CMD="python3 -c \"import htmlmin, sys; print(htmlmin.minify(sys.stdin.read(), remove_comments=True, remove_empty_space=True))\""
else
    echo "⚠️  未找到HTML压缩工具，将使用简单的sed压缩"
    HTML_COMPRESSOR="sed"
fi

# 压缩admin.html
echo ""
echo "📦 压缩 admin.html -> admin_min.html"

ADMIN_HTML="cmd/apk-cache/admin.html"
ADMIN_MIN_HTML="cmd/apk-cache/admin_min.html"

if [ ! -f "$ADMIN_HTML" ]; then
    echo "❌ 错误: 找不到 $ADMIN_HTML"
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
            sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e '/^$/d' -e 's/>[[:space:]]*</></g' "$ADMIN_HTML" > "$ADMIN_MIN_HTML"
        else
            echo "⚠️  sed也不可用，直接复制文件"
            cp "$ADMIN_HTML" "$ADMIN_MIN_HTML"
        fi
        ;;
    *)
        echo "❌ 未知的压缩工具: $HTML_COMPRESSOR"
        exit 1
        ;;
esac

# 检查压缩是否成功
if [ -f "$ADMIN_MIN_HTML" ] && [ -s "$ADMIN_MIN_HTML" ]; then
    ORIGINAL_SIZE=$(stat -c%s "$ADMIN_HTML" 2>/dev/null || stat -f%z "$ADMIN_HTML")
    COMPRESSED_SIZE=$(stat -c%s "$ADMIN_MIN_HTML" 2>/dev/null || stat -f%z "$ADMIN_MIN_HTML")
    COMPRESSION_RATIO=$(echo "scale=2; (1 - $COMPRESSED_SIZE / $ORIGINAL_SIZE) * 100" | bc)
    
    echo "✅ 压缩完成: $ORIGINAL_SIZE → $COMPRESSED_SIZE 字节 (压缩率: ${COMPRESSION_RATIO}%)"
    echo "📝 使用的工具: $HTML_COMPRESSOR"
else
    echo "❌ 压缩失败"
    exit 1
fi

# 创建gzip压缩版本
echo ""
echo "📦 创建gzip压缩版本（最高压缩率）..."
ADMIN_MIN_HTML_GZ="$ADMIN_MIN_HTML.gz"

if command -v gzip &> /dev/null; then
    gzip -9 -c "$ADMIN_MIN_HTML" > "$ADMIN_MIN_HTML_GZ"
    GZIP_SIZE=$(stat -c%s "$ADMIN_MIN_HTML_GZ" 2>/dev/null || stat -f%z "$ADMIN_MIN_HTML_GZ")
    GZIP_RATIO=$(echo "scale=2; (1 - $GZIP_SIZE / $ORIGINAL_SIZE) * 100" | bc)
    echo "✅ Gzip压缩完成: $ORIGINAL_SIZE → $GZIP_SIZE 字节 (总压缩率: ${GZIP_RATIO}%)"
else
    echo "❌  gzip不可用"
    exit 1
fi

echo ""
echo "🔨 开始Go构建..."

# 检查Go是否安装
if ! command -v go &> /dev/null; then
    echo "❌ 错误: 找不到Go编译器"
    exit 1
fi

# 执行Go构建
echo "📦 运行: go build -ldflags=\"-s -w\" -trimpath -o apk-cache ./cmd/apk-cache"
if go build -ldflags="-s -w" -trimpath -o apk-cache ./cmd/apk-cache; then
    echo "✅ Go构建成功完成!"
    echo "📁 生成的可执行文件: apk-cache"
    
    # 显示构建产物信息
    if [ -f "apk-cache" ]; then
        BINARY_SIZE=$(stat -c%s "apk-cache" 2>/dev/null || stat -f%z "apk-cache")
        echo "📊 可执行文件大小: $BINARY_SIZE 字节"
    else
        echo "❌ 错误: 可执行文件未生成"
        exit 1
    fi
else
    echo "❌ Go构建失败"
    exit 1
fi

echo ""
echo "🎉 构建过程完成!"
echo "📊 构建统计:"
echo "   - HTML压缩工具: $HTML_COMPRESSOR"
echo "   - 原始HTML大小: $ORIGINAL_SIZE 字节"
echo "   - 压缩HTML大小: $COMPRESSED_SIZE 字节"
echo "   - 压缩率: ${COMPRESSION_RATIO}%"