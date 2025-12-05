package utils

import (
	"errors"
	"strconv"
	"strings"

	"github.com/tursom/apk-cache/utils/i18n"
)

var supportedHashAlgorithms = NewSetFromSlice([]string{"SHA256", "SHA1", "MD5SUM", "MD5"})

// ParseSizeString 解析大小字符串（如 "10GB", "1TB"）
func ParseSizeString(sizeStr string) (int64, error) {
	if sizeStr == "" || sizeStr == "0" {
		return 0, nil // 0 表示无限制
	}

	sizeStr = strings.ToUpper(sizeStr)
	multiplier := int64(1)

	// 检查单位
	if strings.HasSuffix(sizeStr, "TB") {
		multiplier = 1024 * 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "TB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "B") {
		sizeStr = strings.TrimSuffix(sizeStr, "B")
	}

	// 解析数字部分
	value, err := strconv.ParseInt(strings.TrimSpace(sizeStr), 10, 64)
	if err != nil {
		return 0, err
	}

	return value * multiplier, nil
}

// ParseHashFromPath 从 URL 中解析哈希算法和哈希值
func ParseHashFromPath(path string) (string, string, error) {
	// 解析 URL 路径，提取哈希算法和哈希值
	// 格式: /xxx/by-hash/ALGORITHM/HASH_VALUE

	// 提前测试高频的 /by-hash/SHA256/3c2d4503889027ca51df58e16ec12798d6b438290662e006efab80806ddcb18c
	if len(path) >= 80 && path[len(path)-80:len(path)-64] == "/by-hash/SHA256/" {
		return "SHA256", path[len(path)-64:], nil
	}

	i := strings.LastIndexByte(path, '/')
	hashValue := path[i+1:]
	path = path[:i]

	i = strings.LastIndexByte(path, '/')
	algorithm := path[i+1:]
	path = path[:i]

	if path[len(path)-8:] != "by-hash" {
		return "", "", errors.New(i18n.T("InvalidHashURLFormat", map[string]any{"Path": path}))
	}

	// 验证算法是否支持
	if supportedHashAlgorithms.Contains(algorithm) {
		return "", "", errors.New(i18n.T("UnsupportedHashAlgorithm", map[string]any{"Algorithm": algorithm}))
	}

	// 验证哈希值格式（基本格式检查）
	if len(hashValue) == 0 {
		return "", "", errors.New(i18n.T("EmptyHashValue", nil))
	}

	return algorithm, hashValue, nil
}
