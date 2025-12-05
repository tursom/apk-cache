package apt

import (
	"path/filepath"
	"strings"
)

func IsDebFile(filePath string) bool {
	return strings.HasSuffix(filePath, ".deb")
}

// getAPTCacheFilePath 为APT协议请求生成包含host的缓存文件路径
func GetAPTCacheFilePath(cachePath, host, path string) string {
	// 获取host信息，如果没有则使用默认值
	if host == "" {
		host = "default"
	}

	// 清理host中的特殊字符，使其适合作为目录名
	host = sanitizeHostForPath(host)

	// 构建包含host的缓存路径
	safePath := filepath.Join(cachePath, "apt", host, path)
	return safePath
}

// sanitizeHostForPath 清理host字符串，使其适合作为文件路径
func sanitizeHostForPath(host string) string {
	return sanitizeHostForPathOptimized(host)
}

// sanitizeHostForPathOriginal 原始实现版本
func sanitizeHostForPathOriginal(host string) string {
	// 替换不安全的字符
	host = strings.ReplaceAll(host, ":", "_")
	host = strings.ReplaceAll(host, "/", "_")
	host = strings.ReplaceAll(host, "\\", "_")
	host = strings.ReplaceAll(host, "..", "_")
	host = strings.ReplaceAll(host, "*", "_")
	host = strings.ReplaceAll(host, "?", "_")
	host = strings.ReplaceAll(host, "\"", "_")
	host = strings.ReplaceAll(host, "<", "_")
	host = strings.ReplaceAll(host, ">", "_")
	host = strings.ReplaceAll(host, "|", "_")

	// 如果host为空，使用默认值
	if host == "" {
		host = "default"
	}

	return host
}

// sanitizeHostForPathOptimized 优化实现版本
func sanitizeHostForPathOptimized(host string) string {
	// 如果host为空，使用默认值
	if host == "" {
		return "default"
	}

	// 使用strings.Builder进行高效字符串构建
	var builder strings.Builder
	builder.Grow(len(host)) // 预分配容量，避免多次分配

	// 遍历字符串，替换不安全字符
	for i := 0; i < len(host); i++ {
		switch host[i] {
		case ':', '/', '\\', '*', '?', '"', '<', '>', '|':
			builder.WriteByte('_')
		case '.':
			// 检查是否是 ".." 序列
			if i+1 < len(host) && host[i+1] == '.' {
				builder.WriteByte('_')
				i++ // 跳过下一个点
			} else {
				builder.WriteByte('.')
			}
		default:
			builder.WriteByte(host[i])
		}
	}

	return builder.String()
}
