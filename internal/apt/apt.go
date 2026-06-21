package apt

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/tursom/apk-cache/internal/hashstore"
	"github.com/ulikunitz/xz"
)

var ErrCacheCorrupted = errors.New("apt cache validation failed")

type Record struct {
	Path string
	Hash string
	Size int64
}

type Index struct {
	cacheRoot string
	mu        sync.RWMutex
	records   map[string]Record
	hashStore *hashstore.Store
}

func NewIndex(cacheRoot string) *Index {
	return &Index{
		cacheRoot: cacheRoot,
		records:   make(map[string]Record),
	}
}

func (i *Index) SetHashStore(store *hashstore.Store) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.hashStore = store
}

func (i *Index) LoadExpected(records []hashstore.Expected) {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, record := range records {
		switch record.RecordType {
		case hashstore.RecordAPTRelease, hashstore.RecordAPTPackage, hashstore.RecordAPTSource:
		default:
			continue
		}
		if record.TargetPath == "" {
			continue
		}
		i.records[record.TargetPath] = Record{
			Path: record.TargetPath,
			Hash: hex.EncodeToString(record.ExpectedHash),
			Size: record.ExpectedSize,
		}
	}
}

func (i *Index) LoadFromRoot(root string) error {
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
		if entry.IsDir() || !IsIndexFile(path) {
			return nil
		}
		return i.LoadFile(path)
	})
}

func (i *Index) LoadFile(cachePath string) error {
	return i.loadFileAs(cachePath, cachePath)
}

func (i *Index) LoadFileByHash(cachePath, requestPath string) error {
	algorithm, expected, err := ParseHashPath(requestPath)
	if err != nil {
		return err
	}
	if algorithm != "sha256" {
		return nil
	}
	indexPath, ok := i.indexPathForHash(cachePath, expected)
	if !ok {
		return nil
	}
	return i.loadFileAs(cachePath, indexPath)
}

func (i *Index) loadFileAs(filePath, indexPath string) error {
	if !IsIndexFile(indexPath) {
		return nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader, err := DecompressByName(filepath.Base(indexPath), file)
	if err != nil {
		return err
	}
	host, parentPath, suite, err := i.indexLocation(indexPath)
	if err != nil {
		return err
	}

	filename := filepath.Base(indexPath)
	records := make(map[string]Record)
	hashRecords := make([]hashstore.ExpectedRecord, 0)
	switch {
	case strings.HasPrefix(filename, "Packages"), strings.HasPrefix(filename, "Sources"):
		recordType := hashstore.RecordAPTPackage
		if strings.HasPrefix(filename, "Sources") {
			recordType = hashstore.RecordAPTSource
		}
		for _, item := range ParsePackages(reader) {
			if item.Filename == "" || item.SHA256 == "" {
				continue
			}
			target := CachePath(i.cacheRoot, host, filepath.Join(suite, filepath.FromSlash(item.Filename)))
			records[target] = Record{Path: target, Hash: strings.ToLower(item.SHA256), Size: item.Size}
			sum, err := hashstore.DecodeHex(hashstore.HashSHA256, item.SHA256)
			if err == nil {
				hashRecords = append(hashRecords, hashstore.ExpectedRecord{
					SourcePath:   indexPath,
					TargetPath:   target,
					HashKind:     hashstore.HashSHA256,
					RecordType:   recordType,
					ExpectedHash: sum,
					ExpectedSize: item.Size,
				})
			}
		}
	case filename == "Release" || filename == "InRelease":
		for _, item := range ParseRelease(reader) {
			if item.Filename == "" || item.SHA256 == "" {
				continue
			}
			target := CachePath(i.cacheRoot, host, filepath.Join(parentPath, filepath.FromSlash(item.Filename)))
			records[target] = Record{Path: target, Hash: strings.ToLower(item.SHA256), Size: item.Size}
			sum, err := hashstore.DecodeHex(hashstore.HashSHA256, item.SHA256)
			if err == nil {
				hashRecords = append(hashRecords, hashstore.ExpectedRecord{
					SourcePath:   indexPath,
					TargetPath:   target,
					HashKind:     hashstore.HashSHA256,
					RecordType:   hashstore.RecordAPTRelease,
					ExpectedHash: sum,
					ExpectedSize: item.Size,
					ByHashHost:   host,
				})
			}
		}
	}

	i.mu.RLock()
	store := i.hashStore
	i.mu.RUnlock()
	if store != nil {
		if err := store.ReplaceSource(indexPath, hashRecords); err != nil {
			return err
		}
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	for key, record := range records {
		i.records[key] = record
	}
	return nil
}

func (i *Index) indexPathForHash(cachePath, expectedHash string) (string, bool) {
	host, _, _, err := i.indexLocation(cachePath)
	if err != nil {
		return "", false
	}
	i.mu.RLock()
	store := i.hashStore
	i.mu.RUnlock()
	if store != nil {
		sum, err := hashstore.DecodeHex(hashstore.HashSHA256, expectedHash)
		if err == nil {
			if path, ok, err := store.FindAPTByHash(host, hashstore.HashSHA256, sum); err == nil && ok {
				return path, true
			}
		}
	}
	hostRoot := filepath.Join(i.cacheRoot, "apt", host)

	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, record := range i.records {
		if record.Hash != expectedHash || !IsIndexFile(record.Path) {
			continue
		}
		relative, err := filepath.Rel(hostRoot, record.Path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		return record.Path, true
	}
	return "", false
}

func (i *Index) ValidateByHash(cachePath, filePath, requestPath string) error {
	algorithm, expected, err := ParseHashPath(requestPath)
	if err != nil {
		return err
	}
	kind, err := hashstore.KindFromAlgorithm(algorithm)
	if err != nil {
		return nil
	}
	sum, err := hashstore.DecodeHex(kind, expected)
	if err != nil {
		return err
	}
	i.mu.RLock()
	store := i.hashStore
	i.mu.RUnlock()
	if store != nil {
		actual, err := store.GetOrComputeActual(cachePath, filePath, kind)
		if err != nil {
			return err
		}
		if !hashstore.EqualHash(actual.ActualHash, sum) {
			return ErrCacheCorrupted
		}
		return nil
	}
	actual, err := HashFile(filePath)
	if err != nil {
		return err
	}
	if actual != strings.ToLower(expected) {
		return ErrCacheCorrupted
	}
	return nil
}

func (i *Index) ValidateFile(cachePath, filePath string) error {
	i.mu.RLock()
	store := i.hashStore
	i.mu.RUnlock()
	if store != nil {
		expected, err := store.GetExpected(cachePath, hashstore.HashSHA256)
		switch {
		case err == nil:
			if expected.ExpectedSize > 0 {
				info, err := os.Stat(filePath)
				if err != nil {
					return err
				}
				if info.Size() != expected.ExpectedSize {
					return ErrCacheCorrupted
				}
			}
			actual, err := store.GetOrComputeActual(cachePath, filePath, hashstore.HashSHA256)
			if err != nil {
				return err
			}
			if !hashstore.EqualHash(actual.ActualHash, expected.ExpectedHash) {
				return ErrCacheCorrupted
			}
			return nil
		case !errors.Is(err, hashstore.ErrIndexUnavailable):
			return err
		}
	}

	i.mu.RLock()
	record, ok := i.records[cachePath]
	i.mu.RUnlock()
	if !ok || record.Hash == "" {
		return nil
	}
	if record.Size > 0 {
		info, err := os.Stat(filePath)
		if err != nil {
			return err
		}
		if info.Size() != record.Size {
			return ErrCacheCorrupted
		}
	}
	actual, err := HashFile(filePath)
	if err != nil {
		return err
	}
	if actual != record.Hash {
		return ErrCacheCorrupted
	}
	return nil
}

func (i *Index) ValidateDeb(cachePath, filePath string) error {
	return i.ValidateFile(cachePath, filePath)
}

func (i *Index) indexLocation(cachePath string) (host, parentPath, suite string, err error) {
	relative, err := filepath.Rel(filepath.Join(i.cacheRoot, "apt"), cachePath)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) < 2 {
		return "", "", "", errors.New("invalid apt cache path")
	}
	host = parts[0]
	path := strings.Join(parts[1:], "/")
	parentPath = filepath.ToSlash(filepath.Dir(path))
	suite = parentPath
	if idx := strings.Index(path, "/dists/"); idx >= 0 {
		suite = path[:idx]
	}
	return host, parentPath, suite, nil
}

type PackageFile struct {
	Package  string
	Filename string
	Size     int64
	SHA256   string
}

func ParsePackages(reader io.Reader) []PackageFile {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var out []PackageFile
	var current PackageFile
	inEntry := false

	flush := func() {
		if inEntry && (current.Filename != "" || current.Package != "") {
			out = append(out, current)
		}
		current = PackageFile{}
		inEntry = false
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		switch key {
		case "Package":
			current.Package = value
			inEntry = true
		case "Filename":
			current.Filename = value
			inEntry = true
		case "Size":
			size, _ := strconv.ParseInt(value, 10, 64)
			current.Size = size
			inEntry = true
		case "SHA256":
			current.SHA256 = strings.ToLower(value)
			inEntry = true
		}
	}
	flush()
	return out
}

func ParseRelease(reader io.Reader) []PackageFile {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil
	}
	block, _ := clearsign.Decode(data)
	if block != nil {
		data = block.Plaintext
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	var out []PackageFile
	inSHA256 := false
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, ":"); idx > 0 {
			inSHA256 = strings.TrimSpace(line[:idx]) == "SHA256"
			continue
		}
		if !inSHA256 || len(line) == 0 || line[0] != ' ' {
			if strings.TrimSpace(line) == "" {
				inSHA256 = false
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		size, _ := strconv.ParseInt(fields[1], 10, 64)
		out = append(out, PackageFile{
			Filename: fields[2],
			Size:     size,
			SHA256:   strings.ToLower(fields[0]),
		})
	}
	return out
}

func CachePath(root, host, requestPath string) string {
	host = SanitizeHost(host)
	requestPath = strings.TrimPrefix(filepath.Clean("/"+requestPath), string(filepath.Separator))
	return filepath.Join(root, "apt", host, requestPath)
}

func SanitizeHost(host string) string {
	host = strings.TrimSpace(host)
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "[", "_", "]", "_", "\x00", "_")
	return replacer.Replace(host)
}

func IsIndexFile(path string) bool {
	for _, suffix := range []string{
		"/APKINDEX.tar.gz",
		"/InRelease",
		"/Release",
		"/Packages",
		"/Packages.gz",
		"/Packages.xz",
		"/Packages.bz2",
		"/Packages.lzma",
		"/Sources",
		"/Sources.gz",
		"/Sources.xz",
		"/Sources.bz2",
		"/Sources.lzma",
	} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func IsHashRequest(path string) bool {
	return strings.Contains(path, "/by-hash/")
}

func ParseHashPath(path string) (algorithm, hash string, err error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for idx := 0; idx < len(parts)-2; idx++ {
		if parts[idx] == "by-hash" {
			return strings.ToLower(parts[idx+1]), strings.ToLower(parts[idx+2]), nil
		}
	}
	return "", "", errors.New("invalid by-hash path")
}

func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func DecompressByName(filename string, reader io.Reader) (io.Reader, error) {
	switch {
	case strings.HasSuffix(filename, ".gz"):
		return gzip.NewReader(reader)
	case strings.HasSuffix(filename, ".xz"):
		return xz.NewReader(reader)
	case strings.HasSuffix(filename, ".bz2"):
		return bzip2.NewReader(reader), nil
	case strings.HasSuffix(filename, ".lzma"):
		return xz.NewReader(reader)
	default:
		return reader, nil
	}
}
