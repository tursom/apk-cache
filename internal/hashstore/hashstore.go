package hashstore

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble"
)

const (
	Version byte = 0x01

	nsSourceMapping        byte = 0x03
	nsDictByID             byte = 0x10
	nsDictByValue          byte = 0x11
	nsExpectedSHA1         byte = 0x21
	nsExpectedSHA256       byte = 0x22
	nsActualSHA1           byte = 0x23
	nsActualSHA256         byte = 0x24
	nsExpectedByHashSHA1   byte = 0x25
	nsExpectedByHashSHA256 byte = 0x26
	nsAPTByHashSHA1        byte = 0x27
	nsAPTByHashSHA256      byte = 0x28

	dictKindCachePath byte = 0x01
	dictKindHost      byte = 0x02
)

var (
	ErrUnsupportedHashKind = errors.New("unsupported hash kind")
	ErrPathCollision       = errors.New("hashstore path id collision")
	ErrIndexUnavailable    = errors.New("hashstore expected record unavailable")
	ErrCorruptedValue      = errors.New("hashstore value is corrupted")
)

type HashKind byte

const (
	HashSHA1   HashKind = 0x01
	HashSHA256 HashKind = 0x02
)

type RecordType byte

const (
	RecordAPKPackage RecordType = 0x01
	RecordAPTRelease RecordType = 0x02
	RecordAPTPackage RecordType = 0x03
	RecordAPTSource  RecordType = 0x04
)

type Config struct {
	Path                     string
	CacheRoot                string
	TrustFileStat            bool
	ActualRevalidateInterval time.Duration
}

type Store struct {
	db                       *pebble.DB
	path                     string
	cacheRoot                string
	trustFileStat            bool
	actualRevalidateInterval time.Duration
	actualComputes           atomic.Uint64
	actualHits               atomic.Uint64
	lastRebuildUnixNS        atomic.Int64
	lastRebuildReason        atomic.Value
}

type ExpectedRecord struct {
	SourcePath   string
	TargetPath   string
	HashKind     HashKind
	RecordType   RecordType
	ExpectedHash []byte
	ExpectedSize int64
	ByHashHost   string
}

type Expected struct {
	TargetPath    string     `json:"target_path"`
	SourcePath    string     `json:"source_path"`
	HashKind      HashKind   `json:"hash_kind"`
	RecordType    RecordType `json:"record_type"`
	ExpectedHash  []byte     `json:"expected_hash"`
	ExpectedSize  int64      `json:"expected_size"`
	UpdatedUnixNS int64      `json:"updated_unix_nano"`
}

type Actual struct {
	CachePath      string   `json:"cache_path"`
	HashKind       HashKind `json:"hash_kind"`
	SizeBytes      int64    `json:"size_bytes"`
	MTimeUnixNS    int64    `json:"mtime_unix_nano"`
	ActualHash     []byte   `json:"actual_hash"`
	ComputedUnixNS int64    `json:"computed_unix_nano"`
	CacheHit       bool     `json:"cache_hit"`
}

type Stats struct {
	Path               string `json:"path"`
	Status             string `json:"status"`
	CorruptionStatus   string `json:"corruption_status"`
	EstimatedSizeBytes uint64 `json:"estimated_size_bytes"`
	ExpectedRecords    int    `json:"expected_records"`
	ActualRecords      int    `json:"actual_records"`
	SourceMappings     int    `json:"source_mappings"`
	DictionaryEntries  int    `json:"dictionary_entries"`
	ActualCacheHits    uint64 `json:"actual_cache_hits"`
	ActualComputes     uint64 `json:"actual_computes"`
	LastRebuildUnixNS  int64  `json:"last_rebuild_unix_nano,omitempty"`
	LastRebuildReason  string `json:"last_rebuild_reason,omitempty"`
}

func DefaultPath(cacheDataRoot string) string {
	return filepath.Join(cacheDataRoot, "hash.pebble")
}

func Open(cfg Config) (*Store, error) {
	if cfg.Path == "" {
		return nil, errors.New("hashstore path is required")
	}
	if cfg.CacheRoot == "" {
		return nil, errors.New("hashstore cache root is required")
	}
	if err := os.MkdirAll(cfg.Path, 0o755); err != nil {
		return nil, err
	}
	db, err := pebble.Open(cfg.Path, &pebble.Options{})
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(cfg.CacheRoot)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{
		db:                       db,
		path:                     cfg.Path,
		cacheRoot:                root,
		trustFileStat:            cfg.TrustFileStat,
		actualRevalidateInterval: cfg.ActualRevalidateInterval,
	}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) UpdateOptions(trustFileStat bool, actualRevalidateInterval time.Duration) {
	if s == nil {
		return
	}
	s.trustFileStat = trustFileStat
	s.actualRevalidateInterval = actualRevalidateInterval
}

func (s *Store) MarkRebuilt(reason string) {
	if s == nil {
		return
	}
	s.lastRebuildUnixNS.Store(time.Now().UnixNano())
	s.lastRebuildReason.Store(reason)
}

func (s *Store) CacheKey(path string) (string, error) {
	if path == "" {
		return "", errors.New("cache path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(s.cacheRoot, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == "" || strings.HasPrefix(rel, "../") || rel == ".." || strings.Contains(rel, "\x00") {
		return "", fmt.Errorf("invalid cache path %q", path)
	}
	return rel, nil
}

func PathID(cacheKey string) [16]byte {
	sum := sha256.Sum256([]byte(cacheKey))
	var id [16]byte
	copy(id[:], sum[:16])
	return id
}

func KindFromAlgorithm(algorithm string) (HashKind, error) {
	switch strings.ToLower(strings.TrimSpace(algorithm)) {
	case "sha1":
		return HashSHA1, nil
	case "sha256":
		return HashSHA256, nil
	default:
		return 0, ErrUnsupportedHashKind
	}
}

func (k HashKind) Algorithm() string {
	switch k {
	case HashSHA1:
		return "sha1"
	case HashSHA256:
		return "sha256"
	default:
		return "unknown"
	}
}

func (k HashKind) DigestLen() int {
	switch k {
	case HashSHA1:
		return sha1.Size
	case HashSHA256:
		return sha256.Size
	default:
		return 0
	}
}

func (k HashKind) ExpectedNamespace() byte {
	if k == HashSHA1 {
		return nsExpectedSHA1
	}
	return nsExpectedSHA256
}

func (k HashKind) ActualNamespace() byte {
	if k == HashSHA1 {
		return nsActualSHA1
	}
	return nsActualSHA256
}

func (k HashKind) ExpectedByHashNamespace() byte {
	if k == HashSHA1 {
		return nsExpectedByHashSHA1
	}
	return nsExpectedByHashSHA256
}

func (k HashKind) APTByHashNamespace() byte {
	if k == HashSHA1 {
		return nsAPTByHashSHA1
	}
	return nsAPTByHashSHA256
}

func ValidateHash(kind HashKind, digest []byte) error {
	if kind.DigestLen() == 0 {
		return ErrUnsupportedHashKind
	}
	if len(digest) != kind.DigestLen() {
		return fmt.Errorf("%w: %s digest length=%d", ErrUnsupportedHashKind, kind.Algorithm(), len(digest))
	}
	return nil
}

func DecodeHex(kind HashKind, value string) ([]byte, error) {
	sum, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if err := ValidateHash(kind, sum); err != nil {
		return nil, err
	}
	return sum, nil
}

func (s *Store) ReplaceSource(sourcePath string, records []ExpectedRecord) error {
	sourceKey, err := s.CacheKey(sourcePath)
	if err != nil {
		return err
	}
	sourceID := PathID(sourceKey)
	batch := s.db.NewBatch()
	defer batch.Close()
	pending := make(map[string]string)
	if err := s.ensureDict(batch, pending, dictKindCachePath, sourceKey, sourceID); err != nil {
		return err
	}
	if err := s.deleteSourceRecords(batch, sourceID); err != nil {
		return err
	}
	for _, record := range records {
		if record.SourcePath == "" {
			record.SourcePath = sourcePath
		}
		if record.HashKind == 0 {
			return ErrUnsupportedHashKind
		}
		if err := ValidateHash(record.HashKind, record.ExpectedHash); err != nil {
			return err
		}
		targetKey, err := s.CacheKey(record.TargetPath)
		if err != nil {
			return err
		}
		targetID := PathID(targetKey)
		if err := s.ensureDict(batch, pending, dictKindCachePath, targetKey, targetID); err != nil {
			return err
		}
		if err := batch.Set(expectedKey(record.HashKind, targetID), encodeExpectedValue(record, sourceID), pebble.NoSync); err != nil {
			return err
		}
		if err := batch.Set(expectedByHashKey(record.HashKind, record.ExpectedHash, targetID), []byte{byte(record.RecordType)}, pebble.NoSync); err != nil {
			return err
		}
		if err := batch.Set(sourceMappingKey(sourceID, targetID, record.HashKind), encodeSourceMappingValue(record), pebble.NoSync); err != nil {
			return err
		}
		if record.ByHashHost != "" {
			host := normalizeHost(record.ByHashHost)
			hostID := PathID(host)
			if err := s.ensureDict(batch, pending, dictKindHost, host, hostID); err != nil {
				return err
			}
			if err := batch.Set(aptByHashKey(record.HashKind, hostID, record.ExpectedHash), targetID[:], pebble.NoSync); err != nil {
				return err
			}
		}
	}
	return batch.Commit(pebble.NoSync)
}

func (s *Store) DeleteSource(sourcePath string) error {
	sourceKey, err := s.CacheKey(sourcePath)
	if err != nil {
		return err
	}
	sourceID := PathID(sourceKey)
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.deleteSourceRecords(batch, sourceID); err != nil {
		return err
	}
	return batch.Commit(pebble.NoSync)
}

func (s *Store) GetExpected(targetPath string, kind HashKind) (Expected, error) {
	if kind.DigestLen() == 0 {
		return Expected{}, ErrUnsupportedHashKind
	}
	targetKey, err := s.CacheKey(targetPath)
	if err != nil {
		return Expected{}, err
	}
	targetID := PathID(targetKey)
	value, closer, err := s.db.Get(expectedKey(kind, targetID))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return Expected{}, ErrIndexUnavailable
		}
		return Expected{}, err
	}
	defer closer.Close()
	expected, err := decodeExpectedValue(value, kind)
	if err != nil {
		return Expected{}, err
	}
	expected.TargetPath = targetPath
	expected.HashKind = kind
	if sourcePath, ok := s.getDict(dictKindCachePath, expected.sourceID); ok {
		expected.SourcePath = filepath.Join(s.cacheRoot, filepath.FromSlash(sourcePath))
	}
	return expected.Expected, nil
}

func (s *Store) GetExpectedAny(targetPath string) (Expected, error) {
	if expected, err := s.GetExpected(targetPath, HashSHA256); err == nil {
		return expected, nil
	} else if !errors.Is(err, ErrIndexUnavailable) {
		return Expected{}, err
	}
	return s.GetExpected(targetPath, HashSHA1)
}

func (s *Store) ListExpected() ([]Expected, error) {
	var out []Expected
	for _, item := range []struct {
		ns   byte
		kind HashKind
	}{
		{nsExpectedSHA1, HashSHA1},
		{nsExpectedSHA256, HashSHA256},
	} {
		prefix := []byte{item.ns, Version}
		iter, err := s.db.NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
		if err != nil {
			return nil, err
		}
		for valid := iter.First(); valid; valid = iter.Next() {
			key := iter.Key()
			if len(key) != 18 {
				_ = iter.Close()
				return nil, ErrCorruptedValue
			}
			var targetID [16]byte
			copy(targetID[:], key[2:])
			decoded, err := decodeExpectedValue(iter.Value(), item.kind)
			if err != nil {
				_ = iter.Close()
				return nil, err
			}
			expected := decoded.Expected
			expected.HashKind = item.kind
			targetPath, ok := s.getDict(dictKindCachePath, targetID)
			if !ok {
				_ = iter.Close()
				return nil, ErrCorruptedValue
			}
			expected.TargetPath = filepath.Join(s.cacheRoot, filepath.FromSlash(targetPath))
			if sourcePath, ok := s.getDict(dictKindCachePath, decoded.sourceID); ok {
				expected.SourcePath = filepath.Join(s.cacheRoot, filepath.FromSlash(sourcePath))
			}
			out = append(out, expected)
		}
		if err := iter.Error(); err != nil {
			_ = iter.Close()
			return nil, err
		}
		if err := iter.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) FindExpectedByHash(kind HashKind, expectedHash []byte) ([]Expected, error) {
	if err := ValidateHash(kind, expectedHash); err != nil {
		return nil, err
	}
	prefix := []byte{kind.ExpectedByHashNamespace(), Version}
	prefix = append(prefix, expectedHash...)
	iter, err := s.db.NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []Expected
	for valid := iter.First(); valid; valid = iter.Next() {
		key := append([]byte(nil), iter.Key()...)
		if len(key) != 2+kind.DigestLen()+16 {
			return nil, ErrCorruptedValue
		}
		var targetID [16]byte
		copy(targetID[:], key[2+kind.DigestLen():])
		value, closer, err := s.db.Get(expectedKey(kind, targetID))
		if err != nil {
			if errors.Is(err, pebble.ErrNotFound) {
				continue
			}
			return nil, err
		}
		decoded, decErr := decodeExpectedValue(value, kind)
		_ = closer.Close()
		if decErr != nil {
			return nil, decErr
		}
		expected := decoded.Expected
		expected.HashKind = kind
		if targetPath, ok := s.getDict(dictKindCachePath, targetID); ok {
			expected.TargetPath = filepath.Join(s.cacheRoot, filepath.FromSlash(targetPath))
		}
		if sourcePath, ok := s.getDict(dictKindCachePath, decoded.sourceID); ok {
			expected.SourcePath = filepath.Join(s.cacheRoot, filepath.FromSlash(sourcePath))
		}
		out = append(out, expected)
	}
	return out, iter.Error()
}

func (s *Store) GetOrComputeActual(cachePath, filePath string, kind HashKind) (Actual, error) {
	if kind.DigestLen() == 0 {
		return Actual{}, ErrUnsupportedHashKind
	}
	cacheKey, err := s.CacheKey(cachePath)
	if err != nil {
		return Actual{}, err
	}
	cacheID := PathID(cacheKey)
	info, err := os.Stat(filePath)
	if err != nil {
		return Actual{}, err
	}
	key := actualKey(kind, cacheID)
	if s.trustFileStat {
		if value, closer, err := s.db.Get(key); err == nil {
			actual, decErr := decodeActualValue(value, kind)
			_ = closer.Close()
			if decErr != nil {
				return Actual{}, decErr
			}
			now := time.Now()
			freshEnough := s.actualRevalidateInterval <= 0 || now.Sub(time.Unix(0, actual.ComputedUnixNS)) <= s.actualRevalidateInterval
			if actual.SizeBytes == info.Size() && actual.MTimeUnixNS == info.ModTime().UnixNano() && freshEnough {
				actual.CachePath = cachePath
				actual.HashKind = kind
				actual.CacheHit = true
				s.actualHits.Add(1)
				return actual, nil
			}
		} else if !errors.Is(err, pebble.ErrNotFound) {
			return Actual{}, err
		}
	}
	sum, err := computeHash(filePath, kind)
	if err != nil {
		return Actual{}, err
	}
	actual := Actual{
		CachePath:      cachePath,
		HashKind:       kind,
		SizeBytes:      info.Size(),
		MTimeUnixNS:    info.ModTime().UnixNano(),
		ActualHash:     sum,
		ComputedUnixNS: time.Now().UnixNano(),
	}
	if err := s.db.Set(key, encodeActualValue(actual), pebble.NoSync); err != nil {
		return Actual{}, err
	}
	s.actualComputes.Add(1)
	return actual, nil
}

func (s *Store) GetActual(cachePath string, kind HashKind) (Actual, bool, error) {
	if kind.DigestLen() == 0 {
		return Actual{}, false, ErrUnsupportedHashKind
	}
	cacheKey, err := s.CacheKey(cachePath)
	if err != nil {
		return Actual{}, false, err
	}
	cacheID := PathID(cacheKey)
	value, closer, err := s.db.Get(actualKey(kind, cacheID))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return Actual{}, false, nil
		}
		return Actual{}, false, err
	}
	defer closer.Close()
	actual, err := decodeActualValue(value, kind)
	if err != nil {
		return Actual{}, false, err
	}
	actual.CachePath = cachePath
	actual.HashKind = kind
	return actual, true, nil
}

func (s *Store) DeleteActual(cachePath string) error {
	cacheKey, err := s.CacheKey(cachePath)
	if err != nil {
		return err
	}
	cacheID := PathID(cacheKey)
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := batch.Delete(actualKey(HashSHA1, cacheID), pebble.NoSync); err != nil && !errors.Is(err, pebble.ErrNotFound) {
		return err
	}
	if err := batch.Delete(actualKey(HashSHA256, cacheID), pebble.NoSync); err != nil && !errors.Is(err, pebble.ErrNotFound) {
		return err
	}
	return batch.Commit(pebble.NoSync)
}

func (s *Store) FindAPTByHash(host string, kind HashKind, expectedHash []byte) (string, bool, error) {
	if err := ValidateHash(kind, expectedHash); err != nil {
		return "", false, err
	}
	hostID := PathID(normalizeHost(host))
	value, closer, err := s.db.Get(aptByHashKey(kind, hostID, expectedHash))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	defer closer.Close()
	if len(value) != 16 {
		return "", false, ErrCorruptedValue
	}
	var targetID [16]byte
	copy(targetID[:], value)
	cacheKey, ok := s.getDict(dictKindCachePath, targetID)
	if !ok {
		return "", false, ErrCorruptedValue
	}
	return filepath.Join(s.cacheRoot, filepath.FromSlash(cacheKey)), true, nil
}

func (s *Store) Empty() (bool, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte{nsExpectedSHA1, Version},
		UpperBound: []byte{nsExpectedSHA1, Version + 1},
	})
	if err != nil {
		return false, err
	}
	defer iter.Close()
	if iter.First() {
		return false, nil
	}
	iter, err = s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte{nsExpectedSHA256, Version},
		UpperBound: []byte{nsExpectedSHA256, Version + 1},
	})
	if err != nil {
		return false, err
	}
	defer iter.Close()
	return !iter.First(), nil
}

func (s *Store) Stats() (Stats, error) {
	stats := Stats{
		Path:             s.path,
		Status:           "ok",
		CorruptionStatus: "ok",
		ActualCacheHits:  s.actualHits.Load(),
		ActualComputes:   s.actualComputes.Load(),
	}
	if last := s.lastRebuildUnixNS.Load(); last > 0 {
		stats.LastRebuildUnixNS = last
	}
	if reason, _ := s.lastRebuildReason.Load().(string); reason != "" {
		stats.LastRebuildReason = reason
		if reason == "corruption" {
			stats.CorruptionStatus = "rebuilt"
		}
	}
	if metrics := s.db.Metrics(); metrics != nil {
		stats.EstimatedSizeBytes = uint64(metrics.DiskSpaceUsage())
	}
	counts := []struct {
		prefix []byte
		dst    *int
	}{
		{[]byte{nsExpectedSHA1, Version}, &stats.ExpectedRecords},
		{[]byte{nsExpectedSHA256, Version}, &stats.ExpectedRecords},
		{[]byte{nsActualSHA1, Version}, &stats.ActualRecords},
		{[]byte{nsActualSHA256, Version}, &stats.ActualRecords},
		{[]byte{nsSourceMapping, Version}, &stats.SourceMappings},
		{[]byte{nsDictByID, Version}, &stats.DictionaryEntries},
	}
	for _, item := range counts {
		n, err := s.countPrefix(item.prefix)
		if err != nil {
			return Stats{}, err
		}
		*item.dst += n
	}
	return stats, nil
}

func (s *Store) countPrefix(prefix []byte) (int, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return 0, err
	}
	defer iter.Close()
	count := 0
	for valid := iter.First(); valid; valid = iter.Next() {
		count++
	}
	return count, iter.Error()
}

func (s *Store) ensureDict(batch *pebble.Batch, pending map[string]string, kind byte, value string, id [16]byte) error {
	mapKey := string(append([]byte{kind}, id[:]...))
	if existing, ok := pending[mapKey]; ok {
		if existing != value {
			return fmt.Errorf("%w: %x", ErrPathCollision, id)
		}
		return nil
	}
	if current, ok, err := s.getDictStrict(kind, id); err != nil {
		return err
	} else if ok && current != value {
		return fmt.Errorf("%w: %x", ErrPathCollision, id)
	}
	pending[mapKey] = value
	if err := batch.Set(dictByIDKey(kind, id), []byte(value), pebble.NoSync); err != nil {
		return err
	}
	return batch.Set(dictByValueKey(kind, value), id[:], pebble.NoSync)
}

func (s *Store) getDict(kind byte, id [16]byte) (string, bool) {
	value, ok, err := s.getDictStrict(kind, id)
	if err != nil {
		return "", false
	}
	return value, ok
}

func (s *Store) getDictStrict(kind byte, id [16]byte) (string, bool, error) {
	value, closer, err := s.db.Get(dictByIDKey(kind, id))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	defer closer.Close()
	return string(value), true, nil
}

func (s *Store) deleteSourceRecords(batch *pebble.Batch, sourceID [16]byte) error {
	prefix := sourceMappingPrefix(sourceID)
	var aptHostID *[16]byte
	if sourceKey, ok := s.getDict(dictKindCachePath, sourceID); ok {
		if host, ok := aptHostFromCacheKey(sourceKey); ok {
			id := PathID(host)
			aptHostID = &id
		}
	}
	iter, err := s.db.NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return err
	}
	defer iter.Close()
	for valid := iter.First(); valid; valid = iter.Next() {
		key := append([]byte(nil), iter.Key()...)
		value := append([]byte(nil), iter.Value()...)
		targetID, kind, err := decodeSourceMappingKey(key)
		if err != nil {
			return err
		}
		recordType, expectedHash, err := decodeSourceMappingValue(value, kind)
		if err != nil {
			return err
		}
		_ = recordType
		if err := batch.Delete(expectedKey(kind, targetID), pebble.NoSync); err != nil && !errors.Is(err, pebble.ErrNotFound) {
			return err
		}
		if err := batch.Delete(expectedByHashKey(kind, expectedHash, targetID), pebble.NoSync); err != nil && !errors.Is(err, pebble.ErrNotFound) {
			return err
		}
		if aptHostID != nil {
			if err := batch.Delete(aptByHashKey(kind, *aptHostID, expectedHash), pebble.NoSync); err != nil && !errors.Is(err, pebble.ErrNotFound) {
				return err
			}
		}
		if err := batch.Delete(key, pebble.NoSync); err != nil && !errors.Is(err, pebble.ErrNotFound) {
			return err
		}
	}
	return iter.Error()
}

func computeHash(path string, kind HashKind) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var h hash.Hash
	switch kind {
	case HashSHA1:
		h = sha1.New()
	case HashSHA256:
		h = sha256.New()
	default:
		return nil, ErrUnsupportedHashKind
	}
	if _, err := io.Copy(h, file); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func expectedKey(kind HashKind, targetID [16]byte) []byte {
	key := []byte{kind.ExpectedNamespace(), Version}
	return append(key, targetID[:]...)
}

func actualKey(kind HashKind, cacheID [16]byte) []byte {
	key := []byte{kind.ActualNamespace(), Version}
	return append(key, cacheID[:]...)
}

func expectedByHashKey(kind HashKind, expectedHash []byte, targetID [16]byte) []byte {
	key := []byte{kind.ExpectedByHashNamespace(), Version}
	key = append(key, expectedHash...)
	return append(key, targetID[:]...)
}

func sourceMappingKey(sourceID, targetID [16]byte, kind HashKind) []byte {
	key := []byte{nsSourceMapping, Version}
	key = append(key, sourceID[:]...)
	key = append(key, targetID[:]...)
	return append(key, byte(kind))
}

func sourceMappingPrefix(sourceID [16]byte) []byte {
	key := []byte{nsSourceMapping, Version}
	return append(key, sourceID[:]...)
}

func aptByHashKey(kind HashKind, hostID [16]byte, expectedHash []byte) []byte {
	key := []byte{kind.APTByHashNamespace(), Version}
	key = append(key, hostID[:]...)
	return append(key, expectedHash...)
}

func dictByIDKey(kind byte, id [16]byte) []byte {
	key := []byte{nsDictByID, Version, kind}
	return append(key, id[:]...)
}

func dictByValueKey(kind byte, value string) []byte {
	key := []byte{nsDictByValue, Version, kind}
	return append(key, []byte(value)...)
}

func prefixUpperBound(prefix []byte) []byte {
	out := append([]byte(nil), prefix...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] != 0xff {
			out[i]++
			return out[:i+1]
		}
	}
	return nil
}

func encodeExpectedValue(record ExpectedRecord, sourceID [16]byte) []byte {
	buf := []byte{byte(record.RecordType)}
	buf = binary.AppendUvarint(buf, uint64(record.ExpectedSize))
	buf = append(buf, record.ExpectedHash...)
	buf = append(buf, sourceID[:]...)
	buf = binary.AppendVarint(buf, time.Now().UnixNano())
	return buf
}

type decodedExpected struct {
	Expected
	sourceID [16]byte
}

func decodeExpectedValue(value []byte, kind HashKind) (decodedExpected, error) {
	if len(value) < 1 {
		return decodedExpected{}, ErrCorruptedValue
	}
	out := decodedExpected{}
	out.RecordType = RecordType(value[0])
	rest := value[1:]
	size, n := binary.Uvarint(rest)
	if n <= 0 {
		return decodedExpected{}, ErrCorruptedValue
	}
	out.ExpectedSize = int64(size)
	rest = rest[n:]
	hashLen := kind.DigestLen()
	if len(rest) < hashLen+16 {
		return decodedExpected{}, ErrCorruptedValue
	}
	out.ExpectedHash = append([]byte(nil), rest[:hashLen]...)
	rest = rest[hashLen:]
	copy(out.sourceID[:], rest[:16])
	rest = rest[16:]
	updated, n := binary.Varint(rest)
	if n <= 0 {
		return decodedExpected{}, ErrCorruptedValue
	}
	out.UpdatedUnixNS = updated
	return out, nil
}

func encodeActualValue(actual Actual) []byte {
	buf := binary.AppendUvarint(nil, uint64(actual.SizeBytes))
	buf = binary.AppendVarint(buf, actual.MTimeUnixNS)
	buf = append(buf, actual.ActualHash...)
	buf = binary.AppendVarint(buf, actual.ComputedUnixNS)
	return buf
}

func decodeActualValue(value []byte, kind HashKind) (Actual, error) {
	size, n := binary.Uvarint(value)
	if n <= 0 {
		return Actual{}, ErrCorruptedValue
	}
	rest := value[n:]
	mtime, n := binary.Varint(rest)
	if n <= 0 {
		return Actual{}, ErrCorruptedValue
	}
	rest = rest[n:]
	hashLen := kind.DigestLen()
	if len(rest) < hashLen {
		return Actual{}, ErrCorruptedValue
	}
	actual := Actual{
		HashKind:    kind,
		SizeBytes:   int64(size),
		MTimeUnixNS: mtime,
		ActualHash:  append([]byte(nil), rest[:hashLen]...),
	}
	rest = rest[hashLen:]
	computed, n := binary.Varint(rest)
	if n <= 0 {
		return Actual{}, ErrCorruptedValue
	}
	actual.ComputedUnixNS = computed
	return actual, nil
}

func encodeSourceMappingValue(record ExpectedRecord) []byte {
	buf := []byte{byte(record.RecordType)}
	return append(buf, record.ExpectedHash...)
}

func decodeSourceMappingKey(key []byte) ([16]byte, HashKind, error) {
	var targetID [16]byte
	if len(key) != 35 || key[0] != nsSourceMapping || key[1] != Version {
		return targetID, 0, ErrCorruptedValue
	}
	copy(targetID[:], key[18:34])
	kind := HashKind(key[34])
	if kind.DigestLen() == 0 {
		return targetID, 0, ErrCorruptedValue
	}
	return targetID, kind, nil
}

func decodeSourceMappingValue(value []byte, kind HashKind) (RecordType, []byte, error) {
	if len(value) != 1+kind.DigestLen() {
		return 0, nil, ErrCorruptedValue
	}
	return RecordType(value[0]), append([]byte(nil), value[1:]...), nil
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

func aptHostFromCacheKey(cacheKey string) (string, bool) {
	rest := strings.TrimPrefix(cacheKey, "apt/")
	if rest == cacheKey {
		return "", false
	}
	host, _, ok := strings.Cut(rest, "/")
	if !ok || host == "" {
		return "", false
	}
	return normalizeHost(host), true
}

func EqualHash(a, b []byte) bool {
	return bytes.Equal(a, b)
}
