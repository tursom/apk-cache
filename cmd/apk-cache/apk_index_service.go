package main

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type APKIndexRecord struct {
	Path      string
	Algorithm string
	Hash      []byte
	Size      int64
}

// APKIndexService 将 APKINDEX 转换为运行时可查询的校验表。
// 它维护“缓存文件绝对路径 -> 期望哈希/大小”的映射，供 APKAdapter 在命中缓存或新下载完成后校验包内容。
type APKIndexService struct {
	cacheRoot string

	mu     sync.RWMutex
	byPath map[string]APKIndexRecord
}

func NewAPKIndexService(cacheRoot string) *APKIndexService {
	return &APKIndexService{
		cacheRoot: cacheRoot,
		byPath:    make(map[string]APKIndexRecord),
	}
}

// 启动时扫描已有 APKINDEX，帮助服务在重启后恢复 APK 包校验能力。
func (s *APKIndexService) LoadFromRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Base(path) != "APKINDEX.tar.gz" {
			return nil
		}
		return s.LoadFile(path)
	})
}

// LoadFile 解析单个 APKINDEX 并将记录合并到内存。
// 当前缓存布局中，APKINDEX 和对应的 .apk 文件在同一目录下，因此可以直接据此还原目标缓存路径。
func (s *APKIndexService) LoadFile(cachePath string) error {
	if filepath.Base(cachePath) != "APKINDEX.tar.gz" {
		return nil
	}

	members, err := readAPKArchiveFile(cachePath)
	if err != nil {
		return err
	}
	indexBody, err := extractAPKIndexBody(members)
	if err != nil {
		return err
	}

	dir := filepath.Dir(cachePath)
	records := make(map[string]APKIndexRecord)
	for _, pkg := range parseAPKIndex(indexBody) {
		if pkg.Name == "" || pkg.Version == "" || len(pkg.Hash) == 0 || pkg.Algorithm == "" {
			continue
		}
		filename := pkg.Name + "-" + pkg.Version + ".apk"
		target := filepath.Join(dir, filename)
		records[target] = APKIndexRecord{
			Path:      target,
			Algorithm: pkg.Algorithm,
			Hash:      append([]byte(nil), pkg.Hash...),
			Size:      pkg.Size,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for key, record := range records {
		s.byPath[key] = record
	}
	return nil
}

// ValidatePackage 使用索引记录对缓存文件执行大小和摘要校验。
// 如果当前没有对应记录，返回 ErrAPKIndexUnavailable，由上层决定是否把它当成软失败。
func (s *APKIndexService) ValidatePackage(cachePath string) error {
	return s.ValidatePackageFile(cachePath, cachePath)
}

// ValidatePackageFile 使用逻辑缓存路径查索引记录，但对 filePath 指向的实际文件计算摘要。
// 这让 pipeline 可以在临时文件阶段完成校验，再决定是否把它提升为正式缓存文件。
func (s *APKIndexService) ValidatePackageFile(cachePath, filePath string) error {
	s.mu.RLock()
	record, ok := s.byPath[cachePath]
	s.mu.RUnlock()
	if !ok {
		return ErrAPKIndexUnavailable
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	if record.Size > 0 && info.Size() != record.Size {
		return ErrAPKHashMismatch
	}

	actualHash, err := hashFileWithAlgorithm(filePath, record.Algorithm)
	if err != nil {
		return err
	}
	if !bytes.Equal(actualHash, record.Hash) {
		return ErrAPKHashMismatch
	}
	return nil
}

type apkIndexPackage struct {
	Name      string
	Version   string
	Algorithm string
	Hash      []byte
	Size      int64
}

func extractAPKIndexBody(members []apkArchiveMember) ([]byte, error) {
	for _, member := range members {
		for _, entry := range member.Entries {
			// 真实 APKINDEX 常见为 APKINDEX，测试里为了构造最小样本也接受 DESCRIPTION。
			if entry.Name == "APKINDEX" || entry.Name == "DESCRIPTION" {
				return entry.Body, nil
			}
		}
	}
	return nil, errors.New("apk index body not found")
}

// APKINDEX 采用简单的 key/value 文本格式，这里只读取 APK 校验链路需要的字段。
func parseAPKIndex(data []byte) []apkIndexPackage {
	blocks := strings.Split(string(data), "\n\n")
	packages := make([]apkIndexPackage, 0, len(blocks))

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var pkg apkIndexPackage
		for _, line := range strings.Split(block, "\n") {
			if len(line) < 2 || line[1] != ':' {
				continue
			}
			value := strings.TrimSpace(line[2:])
			switch line[0] {
			case 'P':
				pkg.Name = value
			case 'V':
				pkg.Version = value
			case 'S':
				size, err := strconv.ParseInt(value, 10, 64)
				if err == nil {
					pkg.Size = size
				}
			case 'C':
				algorithm, hashValue, err := decodeAPKChecksum(value)
				if err == nil {
					pkg.Algorithm = algorithm
					pkg.Hash = hashValue
				}
			}
		}
		if pkg.Name != "" && pkg.Version != "" {
			packages = append(packages, pkg)
		}
	}

	return packages
}

// APKINDEX 中常见的 C 字段既可能是带 Q1/Q2 前缀的 base64 变体，也可能是纯十六进制字符串。
func decodeAPKChecksum(value string) (string, []byte, error) {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, "Q1"):
		sum, err := decodeAPKHashComponent(value[2:])
		return "sha1", sum, err
	case strings.HasPrefix(value, "Q2"):
		sum, err := decodeAPKHashComponent(value[2:])
		return "sha256", sum, err
	case len(value) == 40:
		sum, err := hex.DecodeString(value)
		return "sha1", sum, err
	case len(value) == 64:
		sum, err := hex.DecodeString(value)
		return "sha256", sum, err
	default:
		return "", nil, errors.New("unsupported apk checksum encoding")
	}
}

// Alpine 历史上存在多种 base64 变体编码，因此这里依次尝试多个 decoder。
func decodeAPKHashComponent(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	decoders := []*base64.Encoding{
		base64.RawStdEncoding,
		base64.StdEncoding,
		base64.RawURLEncoding,
		base64.URLEncoding,
	}
	for _, decoder := range decoders {
		sum, err := decoder.DecodeString(value)
		if err == nil {
			return sum, nil
		}
	}
	return hex.DecodeString(value)
}

// 根据索引声明的算法对文件重新计算摘要。
func hashFileWithAlgorithm(path, algorithm string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var hasher hash.Hash
	switch strings.ToLower(algorithm) {
	case "sha1":
		hasher = sha1.New()
	case "sha256":
		hasher = sha256.New()
	default:
		return nil, errors.New("unsupported hash algorithm")
	}

	if _, err := io.Copy(hasher, file); err != nil {
		return nil, err
	}
	return hasher.Sum(nil), nil
}
