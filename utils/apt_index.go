package utils

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tursom/apk-cache/utils/i18n"
)

// PackageHash 表示一个包的哈希值
type PackageHash struct {
	Filename string // 相对于发行版根目录的路径，例如 "pool/main/f/foo/foo_1.0_amd64.deb"
	Size     int64
	MD5      string
	SHA1     string
	SHA256   string
}

// ParsePackagesFile 解析 Packages 文件（可能是 .gz 压缩）并返回包哈希映射
func ParsePackagesFile(filePath string) (map[string]PackageHash, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var reader io.Reader = file
	if strings.HasSuffix(filePath, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
		reader = gzReader
	}

	return parsePackages(reader)
}

// parsePackages 从 io.Reader 解析 Packages 内容
func parsePackages(r io.Reader) (map[string]PackageHash, error) {
	scanner := bufio.NewScanner(r)
	hashes := make(map[string]PackageHash)
	var current PackageHash
	inEntry := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// 空行表示条目结束
			if inEntry && current.Filename != "" {
				hashes[current.Filename] = current
			}
			inEntry = false
			current = PackageHash{}
			continue
		}

		// 检查是否是键值对
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			switch key {
			case "Filename":
				current.Filename = value
				inEntry = true
			case "Size":
				// 忽略错误，默认为0
				size, _ := parseInt64(value)
				current.Size = size
			case "MD5sum":
				current.MD5 = value
			case "SHA1":
				current.SHA1 = value
			case "SHA256":
				current.SHA256 = value
			}
		}
	}

	// 处理最后一个条目
	if inEntry && current.Filename != "" {
		hashes[current.Filename] = current
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return hashes, nil
}

func parseInt64(s string) (int64, error) {
	var val int64
	_, err := fmt.Sscanf(s, "%d", &val)
	return val, err
}

// FindPackageHash 在缓存目录中查找给定 .deb 文件的哈希值
// debPath 是相对于缓存根目录的路径，例如 "apt/default/ubuntu/pool/main/f/foo/foo_1.0_amd64.deb"
// 返回 SHA256 哈希，如果找不到则返回空字符串
func FindPackageHash(cacheRoot, debPath string) (string, error) {
	// 从 debPath 提取发行版和组件信息
	// 假设路径格式为 apt/{host}/{dist}/pool/{component}/...
	rel, err := filepath.Rel(filepath.Join(cacheRoot, "apt"), debPath)
	if err != nil {
		return "", err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 4 {
		return "", errors.New(i18n.T("InvalidDebPath", map[string]any{"Path": debPath}))
	}
	host := parts[0]
	dist := parts[1]
	// 接下来是 "pool"，然后是组件
	if parts[2] != "pool" {
		return "", errors.New(i18n.T("InvalidDebPath", map[string]any{"Path": debPath}))
	}
	component := parts[3]
	// 架构需要从索引文件名推断，或者从包路径中提取
	// 简化：扫描所有可能的索引文件
	indexPatterns := []string{
		filepath.Join(cacheRoot, "apt", host, dist, "dists", "*", "*", "binary-*", "Packages.gz"),
		filepath.Join(cacheRoot, "apt", host, dist, "dists", "*", "*", "binary-*", "Packages"),
	}
	for _, pattern := range indexPatterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, indexFile := range matches {
			hashes, err := ParsePackagesFile(indexFile)
			if err != nil {
				continue
			}
			// 在 hashes 中查找包，键为相对于发行版根目录的路径，例如 "pool/main/f/foo/foo_1.0_amd64.deb"
			// 我们需要从 debPath 中提取相对路径
			relPath := filepath.Join("pool", component, strings.Join(parts[4:], string(filepath.Separator)))
			if hash, ok := hashes[relPath]; ok && hash.SHA256 != "" {
				return hash.SHA256, nil
			}
		}
	}
	return "", nil
}
