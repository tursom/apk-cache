package main

import (
	"strings"
)

// PackageType 表示包的类型
type PackageType int

const (
	PackageTypeUnknown PackageType = iota
	PackageTypeAPK
	PackageTypeAPT
)

// detectPackageType 检测包类型
func detectPackageType(path string) PackageType {
	// 优化的检测顺序，按出现频率排序
	switch {
	case strings.HasSuffix(path, ".apk"):
		return PackageTypeAPK
	case strings.HasSuffix(path, ".deb"):
		return PackageTypeAPT
	case strings.HasSuffix(path, "/APKINDEX.tar.gz"):
		return PackageTypeAPK
	case strings.Contains(path, "/alpine/"):
		return PackageTypeAPK
	case strings.Contains(path, "/dists/"):
		return PackageTypeAPT
	case strings.Contains(path, "/pool/"):
		return PackageTypeAPT
	case strings.Contains(path, "/by-hash/"):
		if strings.Contains(path, "/alpine/") {
			return PackageTypeAPK
		}
		return PackageTypeAPT
	default:
		return PackageTypeUnknown
	}
}

// detectPackageTypeFast 快速包类型检测
// 使用字节切片和 Boyer-Moore 启发式算法
func detectPackageTypeFast(path string) PackageType {
	// 转换为字节切片以获得更好的性能
	n := len(path)
	if n < 6 {
		return PackageTypeUnknown
	}

	// 检查开头
	if path[0] == '/' {
		// "/alpine"
		if n >= 8 {
			if path[1] == 'a' && path[2] == 'l' && path[3] == 'p' &&
				path[4] == 'i' && path[5] == 'n' && path[6] == 'e' {
				return PackageTypeAPK
			}
		}
		// "/debian"
		if n >= 8 {
			if path[1] == 'd' && path[2] == 'e' && path[3] == 'b' &&
				path[4] == 'i' && path[5] == 'a' && path[6] == 'n' {
				return PackageTypeAPT
			}
		}
		// "/ubuntu"
		if n >= 7 {
			if path[1] == 'u' && path[2] == 'b' && path[3] == 'u' &&
				path[4] == 'n' && path[5] == 't' && path[6] == 'u' {
				return PackageTypeAPT
			}
		}
	}

	// 检查文件后缀
	if n >= 4 {
		if path[n-4] == '.' {
			// 检查 .apk 后缀
			if path[n-3] == 'a' && path[n-2] == 'p' && path[n-1] == 'k' {
				return PackageTypeAPK
			}

			// 检查 .deb 后缀
			if path[n-3] == 'd' && path[n-2] == 'e' && path[n-1] == 'b' {
				return PackageTypeAPT
			}

		}
	}

	// 检查特定文件名
	// 检查 "/APKINDEX.tar.gz"
	if n >= 16 {
		if path[n-16:] == "/APKINDEX.tar.gz" {
			return PackageTypeAPK
		}
	}

	// 检查 "/InRelease"
	if n >= 10 {
		if path[n-10:] == "/InRelease" {
			return PackageTypeAPT
		}
	}

	// 检查 "/by-hash/SHA256/..."
	if n >= 80 {
		if path[n-80:n-64] == "/by-hash/SHA256/" {
			return PackageTypeAPT
		}
	}

	// 单次扫描检测所有路径模式
	// 使用更高效的字节比较
	for i := 1; i < n; i++ {
		if path[i] != '/' {
			continue
		}

		// 检查 "/alpine/"
		if i+8 <= n &&
			path[i+1] == 'a' && path[i+2] == 'l' && path[i+3] == 'p' &&
			path[i+4] == 'i' && path[i+5] == 'n' && path[i+6] == 'e' && path[i+7] == '/' {
			return PackageTypeAPK
		}
		// 检查 "/dists/"
		if i+7 <= n &&
			path[i+1] == 'd' && path[i+2] == 'i' && path[i+3] == 's' &&
			path[i+4] == 't' && path[i+5] == 's' && path[i+6] == '/' {
			return PackageTypeAPT
		}
		// 检查 "/pool/"
		if i+6 <= n &&
			path[i+1] == 'p' && path[i+2] == 'o' && path[i+3] == 'o' &&
			path[i+4] == 'l' && path[i+5] == '/' {
			return PackageTypeAPT
		}
		// 检查 "/by-hash/"
		if i+9 <= n &&
			path[i+1] == 'b' && path[i+2] == 'y' && path[i+3] == '-' &&
			path[i+4] == 'h' && path[i+5] == 'a' && path[i+6] == 's' &&
			path[i+7] == 'h' && path[i+8] == '/' {
			return PackageTypeAPT
		}
	}

	return PackageTypeUnknown
}
