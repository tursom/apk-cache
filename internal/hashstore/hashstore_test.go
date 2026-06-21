package hashstore

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cockroachdb/pebble"
)

func TestKeyLayoutLengths(t *testing.T) {
	var id [16]byte
	sha1sum := make([]byte, sha1.Size)
	sha256sum := make([]byte, sha256.Size)

	if got := len(expectedKey(HashSHA1, id)); got != 18 {
		t.Fatalf("expected sha1 key len=%d", got)
	}
	if got := len(actualKey(HashSHA256, id)); got != 18 {
		t.Fatalf("actual sha256 key len=%d", got)
	}
	if got := len(expectedByHashKey(HashSHA1, sha1sum, id)); got != 38 {
		t.Fatalf("expected-by-hash sha1 key len=%d", got)
	}
	if got := len(expectedByHashKey(HashSHA256, sha256sum, id)); got != 50 {
		t.Fatalf("expected-by-hash sha256 key len=%d", got)
	}
	if got := len(sourceMappingKey(id, id, HashSHA256)); got != 35 {
		t.Fatalf("source mapping key len=%d", got)
	}
	if got := len(dictByValueKey(dictKindCachePath, "a/variable/path")); got != 3+len("a/variable/path") {
		t.Fatalf("dict-by-value key len=%d", got)
	}
	prefix := sourceMappingPrefix(id)
	got := prefixUpperBound(prefix)
	if len(got) != len(prefix) || !bytes.Equal(got[:len(got)-1], prefix[:len(prefix)-1]) || got[len(got)-1] != prefix[len(prefix)-1]+1 {
		t.Fatalf("unexpected source prefix upper bound: %x", got)
	}
}

func TestExpectedActualAndByHash(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "apt", "deb.example", "debian", "dists", "bookworm", "Release")
	targetPath := filepath.Join(root, "apt", "deb.example", "debian", "dists", "bookworm", "main", "binary-amd64", "Packages.xz")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("package-index")
	if err := os.WriteFile(targetPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)

	s := openTestStore(t, root)
	defer s.Close()
	if err := s.ReplaceSource(sourcePath, []ExpectedRecord{{
		TargetPath:   targetPath,
		HashKind:     HashSHA256,
		RecordType:   RecordAPTRelease,
		ExpectedHash: sum[:],
		ExpectedSize: int64(len(body)),
		ByHashHost:   "deb.example",
	}}); err != nil {
		t.Fatal(err)
	}
	expected, err := s.GetExpected(targetPath, HashSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected.ExpectedHash, sum[:]) || expected.ExpectedSize != int64(len(body)) {
		t.Fatalf("unexpected expected record: %#v", expected)
	}
	actual, err := s.GetOrComputeActual(targetPath, targetPath, HashSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual.ActualHash, sum[:]) || actual.CacheHit {
		t.Fatalf("unexpected first actual: %#v", actual)
	}
	actual, err = s.GetOrComputeActual(targetPath, targetPath, HashSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !actual.CacheHit {
		t.Fatal("second actual lookup should hit cache")
	}
	originalPath, ok, err := s.FindAPTByHash("deb.example", HashSHA256, sum[:])
	if err != nil || !ok {
		t.Fatalf("find by hash ok=%v err=%v", ok, err)
	}
	if originalPath != targetPath {
		t.Fatalf("originalPath=%s want %s", originalPath, targetPath)
	}
	byHash, err := s.FindExpectedByHash(HashSHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	if len(byHash) != 1 || byHash[0].TargetPath != targetPath || !bytes.Equal(byHash[0].ExpectedHash, sum[:]) {
		t.Fatalf("unexpected expected-by-hash result: %#v", byHash)
	}
	expectedList, err := s.ListExpected()
	if err != nil {
		t.Fatal(err)
	}
	if len(expectedList) != 1 || expectedList[0].TargetPath != targetPath || expectedList[0].SourcePath != sourcePath || !bytes.Equal(expectedList[0].ExpectedHash, sum[:]) {
		t.Fatalf("unexpected expected list: %#v", expectedList)
	}
	stats, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ExpectedRecords != 1 || stats.ActualRecords != 1 || stats.SourceMappings != 1 || stats.ActualComputes != 1 || stats.ActualCacheHits != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	newBody := []byte("new-package-index")
	newSum := sha256.Sum256(newBody)
	if err := s.ReplaceSource(sourcePath, []ExpectedRecord{{
		TargetPath:   targetPath,
		HashKind:     HashSHA256,
		RecordType:   RecordAPTRelease,
		ExpectedHash: newSum[:],
		ExpectedSize: int64(len(newBody)),
		ByHashHost:   "deb.example",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.FindAPTByHash("deb.example", HashSHA256, sum[:]); err != nil || ok {
		t.Fatalf("old by-hash mapping ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.FindAPTByHash("deb.example", HashSHA256, newSum[:]); err != nil || !ok {
		t.Fatalf("new by-hash mapping ok=%v err=%v", ok, err)
	}
}

func TestDeleteSourceRemovesExpectedSourceAndByHash(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "apt", "deb.example", "debian", "dists", "bookworm", "Release")
	targetPath := filepath.Join(root, "apt", "deb.example", "debian", "dists", "bookworm", "main", "binary-amd64", "Packages.xz")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("packages"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("packages"))

	s := openTestStore(t, root)
	defer s.Close()
	if err := s.ReplaceSource(sourcePath, []ExpectedRecord{{
		TargetPath:   targetPath,
		HashKind:     HashSHA256,
		RecordType:   RecordAPTRelease,
		ExpectedHash: sum[:],
		ExpectedSize: int64(len("packages")),
		ByHashHost:   "deb.example",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSource(sourcePath); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetExpected(targetPath, HashSHA256); !errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("expected record should be deleted, got %v", err)
	}
	if found, err := s.FindExpectedByHash(HashSHA256, sum[:]); err != nil || len(found) != 0 {
		t.Fatalf("expected-by-hash should be deleted: found=%#v err=%v", found, err)
	}
	if _, ok, err := s.FindAPTByHash("deb.example", HashSHA256, sum[:]); err != nil || ok {
		t.Fatalf("apt by-hash should be deleted: ok=%v err=%v", ok, err)
	}
	stats, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ExpectedRecords != 0 || stats.SourceMappings != 0 {
		t.Fatalf("unexpected stats after source delete: %#v", stats)
	}
}

func TestDictionaryCollision(t *testing.T) {
	root := t.TempDir()
	s := openTestStore(t, root)
	defer s.Close()

	id := PathID("same-id")
	if err := s.db.Set(dictByIDKey(dictKindCachePath, id), []byte("old/path"), pebble.NoSync); err != nil {
		t.Fatal(err)
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	err := s.ensureDict(batch, map[string]string{}, dictKindCachePath, "new/path", id)
	if !errors.Is(err, ErrPathCollision) {
		t.Fatalf("expected collision, got %v", err)
	}
}

func TestDictionaryReusesSamePath(t *testing.T) {
	root := t.TempDir()
	s := openTestStore(t, root)
	defer s.Close()

	id := PathID("same/path")
	batch := s.db.NewBatch()
	defer batch.Close()
	pending := map[string]string{}
	if err := s.ensureDict(batch, pending, dictKindCachePath, "same/path", id); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureDict(batch, pending, dictKindCachePath, "same/path", id); err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(pebble.NoSync); err != nil {
		t.Fatal(err)
	}
	if value, ok := s.getDict(dictKindCachePath, id); !ok || value != "same/path" {
		t.Fatalf("dict value=%q ok=%v", value, ok)
	}
}

func TestDictionaryPendingCollision(t *testing.T) {
	root := t.TempDir()
	s := openTestStore(t, root)
	defer s.Close()

	id := PathID("same-id")
	batch := s.db.NewBatch()
	defer batch.Close()
	pending := map[string]string{}
	if err := s.ensureDict(batch, pending, dictKindCachePath, "old/path", id); err != nil {
		t.Fatal(err)
	}
	err := s.ensureDict(batch, pending, dictKindCachePath, "new/path", id)
	if !errors.Is(err, ErrPathCollision) {
		t.Fatalf("expected pending collision, got %v", err)
	}
}

func TestActualRevalidateInterval(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pkg.apk")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openTestStore(t, root)
	s.actualRevalidateInterval = time.Nanosecond
	defer s.Close()
	if _, err := s.GetOrComputeActual(path, path, HashSHA1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := s.GetOrComputeActual(path, path, HashSHA1); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ActualComputes != 2 {
		t.Fatalf("actual computes=%d", stats.ActualComputes)
	}
}

func TestActualRecomputesWhenSizeOrMTimeChangesAndDelete(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pkg.apk")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openTestStore(t, root)
	defer s.Close()

	first, err := s.GetOrComputeActual(path, path, HashSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if first.CacheHit {
		t.Fatal("first actual lookup should compute")
	}
	cached, ok, err := s.GetActual(path, HashSHA256)
	if err != nil || !ok || !bytes.Equal(cached.ActualHash, first.ActualHash) {
		t.Fatalf("get actual ok=%v err=%v cached=%#v first=%#v", ok, err, cached, first)
	}
	if err := os.WriteFile(path, []byte("two!"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := s.GetOrComputeActual(path, path, HashSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if second.CacheHit || bytes.Equal(first.ActualHash, second.ActualHash) {
		t.Fatalf("size change should recompute: first=%#v second=%#v", first, second)
	}
	mtime := time.Now().Add(time.Hour)
	if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	third, err := s.GetOrComputeActual(path, path, HashSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if third.CacheHit {
		t.Fatal("mtime change should recompute")
	}
	if err := s.DeleteActual(path); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetActual(path, HashSHA256); err != nil || ok {
		t.Fatalf("actual should be deleted ok=%v err=%v", ok, err)
	}
	fourth, err := s.GetOrComputeActual(path, path, HashSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.CacheHit {
		t.Fatal("lookup after DeleteActual should recompute")
	}
	stats, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ActualRecords != 1 || stats.ActualComputes != 4 {
		t.Fatalf("unexpected actual stats: %#v", stats)
	}
}

func openTestStore(t *testing.T, root string) *Store {
	t.Helper()
	s, err := Open(Config{
		Path:                     filepath.Join(t.TempDir(), "hash.pebble"),
		CacheRoot:                root,
		TrustFileStat:            true,
		ActualRevalidateInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
