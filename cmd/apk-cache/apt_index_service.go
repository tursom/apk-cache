package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tursom/apk-cache/utils"
	aptparse "github.com/tursom/apk-cache/utils/apt"
)

type APTIndexRecord struct {
	Path string
	Hash string
	Size int64
}

type APTIndexService struct {
	cacheRoot string

	mu     sync.RWMutex
	byPath map[string]APTIndexRecord
}

func NewAPTIndexService(cacheRoot string) *APTIndexService {
	return &APTIndexService{
		cacheRoot: cacheRoot,
		byPath:    make(map[string]APTIndexRecord),
	}
}

func (s *APTIndexService) LoadFromRoot(root string) error {
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
		if entry.IsDir() || !utils.IsIndexFile(path) {
			return nil
		}
		return s.LoadFile(path)
	})
}

func (s *APTIndexService) LoadFile(cachePath string) error {
	if !utils.IsIndexFile(cachePath) {
		return nil
	}

	file, err := os.Open(cachePath)
	if err != nil {
		return err
	}
	defer file.Close()

	filename := filepath.Base(cachePath)
	reader, err := utils.Decompress(filename, file)
	if err != nil {
		return err
	}

	host, parentPath, suite, err := s.parseIndexLocation(cachePath)
	if err != nil {
		return err
	}

	switch {
	case strings.HasPrefix(filename, "Packages"), strings.HasPrefix(filename, "Sources"):
		records := make(map[string]APTIndexRecord)
		for item := range aptparse.ParsePackageReader(reader) {
			if item.SHA256 == "" || item.Filename == "" {
				continue
			}
			target := aptparse.GetAPTCacheFilePath(s.cacheRoot, host, filepath.Join(suite, filepath.FromSlash(item.Filename)))
			records[target] = APTIndexRecord{Path: target, Hash: strings.ToLower(item.SHA256), Size: item.Size}
		}
		s.merge(records)
	case filename == "Release" || filename == "InRelease":
		records := make(map[string]APTIndexRecord)
		for item := range aptparse.ParseReleaseReader(reader) {
			if item.SHA256 == "" || item.Filename == "" {
				continue
			}
			target := aptparse.GetAPTCacheFilePath(s.cacheRoot, host, filepath.Join(parentPath, filepath.FromSlash(item.Filename)))
			records[target] = APTIndexRecord{Path: target, Hash: strings.ToLower(item.SHA256), Size: item.Size}
		}
		s.merge(records)
	}

	return nil
}

func (s *APTIndexService) ValidateByHash(cachePath, requestPath string) error {
	algorithm, expectedHash, err := parseHashRequest(requestPath)
	if err != nil {
		return err
	}
	if algorithm != "sha256" {
		return nil
	}
	actualHash, err := hashFile(cachePath)
	if err != nil {
		return err
	}
	if actualHash != expectedHash {
		return ErrCacheCorrupted
	}
	return nil
}

func (s *APTIndexService) ValidateDeb(cachePath string) error {
	return s.ValidateDebFile(cachePath, cachePath)
}

// ValidateDebFile 使用逻辑缓存路径查索引记录，但对 filePath 指向的实际文件做哈希校验。
// 这样下载阶段可以先对临时文件完成校验，再原子替换正式缓存文件。
func (s *APTIndexService) ValidateDebFile(cachePath, filePath string) error {
	s.mu.RLock()
	record, ok := s.byPath[cachePath]
	s.mu.RUnlock()
	if !ok || record.Hash == "" {
		return nil
	}
	actualHash, err := hashFile(filePath)
	if err != nil {
		return err
	}
	if actualHash != record.Hash {
		return ErrCacheCorrupted
	}
	return nil
}

func (s *APTIndexService) merge(records map[string]APTIndexRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, record := range records {
		s.byPath[key] = record
	}
}

func (s *APTIndexService) parseIndexLocation(cachePath string) (host, parentPath, suite string, err error) {
	relative, err := filepath.Rel(filepath.Join(s.cacheRoot, "apt"), cachePath)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) < 2 {
		return "", "", "", errors.New("invalid apt cache path")
	}
	host = parts[0]
	parentPath = filepath.ToSlash(filepath.Dir(strings.Join(parts[1:], "/")))
	suite = filepath.ToSlash(strings.Join(parts[1:], "/"))
	if idx := strings.Index(suite, "/dists/"); idx >= 0 {
		suite = suite[:idx]
	} else {
		suite = filepath.ToSlash(filepath.Dir(strings.Join(parts[1:], "/")))
	}
	return host, parentPath, suite, nil
}

func parseHashRequest(requestPath string) (string, string, error) {
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	for idx := 0; idx < len(parts)-2; idx++ {
		if parts[idx] != "by-hash" {
			continue
		}
		return strings.ToLower(parts[idx+1]), strings.ToLower(parts[idx+2]), nil
	}
	return "", "", errors.New("invalid by-hash path")
}

func hashFile(cachePath string) (string, error) {
	file, err := os.Open(cachePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
