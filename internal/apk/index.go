package apk

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

	"github.com/tursom/apk-cache/internal/hashstore"
)

var (
	ErrIndexUnavailable = errors.New("apk index unavailable")
	ErrHashMismatch     = errors.New("apk hash mismatch")
)

type Record struct {
	Path      string
	Algorithm string
	Hash      []byte
	Size      int64
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
		if record.RecordType != hashstore.RecordAPKPackage || record.TargetPath == "" {
			continue
		}
		i.records[record.TargetPath] = Record{
			Path:      record.TargetPath,
			Algorithm: record.HashKind.Algorithm(),
			Hash:      append([]byte(nil), record.ExpectedHash...),
			Size:      record.ExpectedSize,
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
		if entry.IsDir() || filepath.Base(path) != "APKINDEX.tar.gz" {
			return nil
		}
		return i.LoadFile(path)
	})
}

func (i *Index) LoadFile(cachePath string) error {
	if filepath.Base(cachePath) != "APKINDEX.tar.gz" {
		return nil
	}
	members, err := ReadArchiveFile(cachePath)
	if err != nil {
		return err
	}
	body, err := extractIndexBody(members)
	if err != nil {
		return err
	}

	dir := filepath.Dir(cachePath)
	records := make(map[string]Record)
	hashRecords := make([]hashstore.ExpectedRecord, 0)
	for _, pkg := range ParseIndex(body) {
		if pkg.Name == "" || pkg.Version == "" || pkg.Algorithm == "" || len(pkg.Hash) == 0 {
			continue
		}
		target := filepath.Join(dir, pkg.Name+"-"+pkg.Version+".apk")
		records[target] = Record{
			Path:      target,
			Algorithm: pkg.Algorithm,
			Hash:      append([]byte(nil), pkg.Hash...),
			Size:      pkg.Size,
		}
		kind, err := hashstore.KindFromAlgorithm(pkg.Algorithm)
		if err == nil {
			hashRecords = append(hashRecords, hashstore.ExpectedRecord{
				SourcePath:   cachePath,
				TargetPath:   target,
				HashKind:     kind,
				RecordType:   hashstore.RecordAPKPackage,
				ExpectedHash: append([]byte(nil), pkg.Hash...),
				ExpectedSize: pkg.Size,
			})
		}
	}

	i.mu.RLock()
	store := i.hashStore
	i.mu.RUnlock()
	if store != nil {
		if err := store.ReplaceSource(cachePath, hashRecords); err != nil {
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

func (i *Index) ValidatePackage(cachePath, filePath string) error {
	i.mu.RLock()
	store := i.hashStore
	i.mu.RUnlock()
	if store != nil {
		expected, err := store.GetExpectedAny(cachePath)
		switch {
		case err == nil:
			if expected.ExpectedSize > 0 {
				info, err := os.Stat(filePath)
				if err != nil {
					return err
				}
				if info.Size() != expected.ExpectedSize {
					return ErrHashMismatch
				}
			}
			actual, err := store.GetOrComputeActual(cachePath, filePath, expected.HashKind)
			if err != nil {
				return err
			}
			if !hashstore.EqualHash(actual.ActualHash, expected.ExpectedHash) {
				return ErrHashMismatch
			}
			return nil
		case !errors.Is(err, hashstore.ErrIndexUnavailable):
			return err
		}
	}

	i.mu.RLock()
	record, ok := i.records[cachePath]
	i.mu.RUnlock()
	if !ok {
		return ErrIndexUnavailable
	}
	if record.Size > 0 {
		info, err := os.Stat(filePath)
		if err != nil {
			return err
		}
		if info.Size() != record.Size {
			return ErrHashMismatch
		}
	}
	actual, err := hashFile(filePath, record.Algorithm)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, record.Hash) {
		return ErrHashMismatch
	}
	return nil
}

type Package struct {
	Name      string
	Version   string
	Algorithm string
	Hash      []byte
	Size      int64
}

func ParseIndex(data []byte) []Package {
	blocks := strings.Split(string(data), "\n\n")
	out := make([]Package, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var pkg Package
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
				size, _ := strconv.ParseInt(value, 10, 64)
				pkg.Size = size
			case 'C':
				algorithm, sum, err := DecodeChecksum(value)
				if err == nil {
					pkg.Algorithm = algorithm
					pkg.Hash = sum
				}
			}
		}
		if pkg.Name != "" && pkg.Version != "" {
			out = append(out, pkg)
		}
	}
	return out
}

func DecodeChecksum(value string) (string, []byte, error) {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, "Q1"):
		sum, err := decodeHashComponent(value[2:])
		return "sha1", sum, err
	case strings.HasPrefix(value, "Q2"):
		sum, err := decodeHashComponent(value[2:])
		return "sha256", sum, err
	case len(value) == 40:
		sum, err := hex.DecodeString(value)
		return "sha1", sum, err
	case len(value) == 64:
		sum, err := hex.DecodeString(value)
		return "sha256", sum, err
	default:
		return "", nil, errors.New("unsupported apk checksum")
	}
}

func IsIndexFile(path string) bool {
	return strings.HasSuffix(path, "/APKINDEX.tar.gz") || filepath.Base(path) == "APKINDEX.tar.gz"
}

func IsPackageFile(path string) bool {
	return strings.HasSuffix(path, ".apk")
}

func extractIndexBody(members []ArchiveMember) ([]byte, error) {
	for _, member := range members {
		for _, entry := range member.Entries {
			if entry.Name == "APKINDEX" || entry.Name == "DESCRIPTION" {
				return entry.Body, nil
			}
		}
	}
	return nil, errors.New("apk index body not found")
}

func decodeHashComponent(value string) ([]byte, error) {
	decoders := []*base64.Encoding{
		base64.RawStdEncoding,
		base64.StdEncoding,
		base64.RawURLEncoding,
		base64.URLEncoding,
	}
	for _, decoder := range decoders {
		if sum, err := decoder.DecodeString(value); err == nil {
			return sum, nil
		}
	}
	return hex.DecodeString(value)
}

func hashFile(path, algorithm string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var h hash.Hash
	switch strings.ToLower(algorithm) {
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	default:
		return nil, errors.New("unsupported hash algorithm")
	}
	if _, err := io.Copy(h, file); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
