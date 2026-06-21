package apt

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParsePackages(t *testing.T) {
	items := ParsePackages(strings.NewReader(`Package: hello
Filename: pool/main/h/hello/hello_1_amd64.deb
Size: 5
SHA256: abcdef

`))
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	if items[0].Filename != "pool/main/h/hello/hello_1_amd64.deb" {
		t.Fatalf("filename=%s", items[0].Filename)
	}
}

func TestValidateByHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	body := []byte("index")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	index := NewIndex(dir)
	if err := index.ValidateByHash(path, "/debian/dists/bookworm/by-hash/SHA256/"+hash); err != nil {
		t.Fatalf("validate good hash: %v", err)
	}
	if err := index.ValidateByHash(path, "/debian/dists/bookworm/by-hash/SHA256/0000"); err == nil {
		t.Fatal("expected bad hash error")
	}
}

func TestLoadPackagesIndexAndValidateDeb(t *testing.T) {
	root := t.TempDir()
	host := "deb.example.org"
	debBody := []byte("deb body")
	sum := sha256.Sum256(debBody)
	packages := `Package: hello
Filename: pool/main/h/hello/hello_1_amd64.deb
Size: ` + strconv.Itoa(len(debBody)) + `
SHA256: ` + hex.EncodeToString(sum[:]) + `

`
	indexPath := filepath.Join(root, "apt", host, "debian", "dists", "bookworm", "main", "binary-amd64", "Packages")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, []byte(packages), 0o644); err != nil {
		t.Fatal(err)
	}

	index := NewIndex(root)
	if err := index.LoadFile(indexPath); err != nil {
		t.Fatalf("load packages: %v", err)
	}
	debPath := CachePath(root, host, "debian/pool/main/h/hello/hello_1_amd64.deb")
	if err := os.MkdirAll(filepath.Dir(debPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debPath, debBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := index.ValidateDeb(debPath, debPath); err != nil {
		t.Fatalf("validate deb: %v", err)
	}
	if err := os.WriteFile(debPath, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := index.ValidateDeb(debPath, debPath); !errors.Is(err, ErrCacheCorrupted) {
		t.Fatalf("expected corrupted deb, got %v", err)
	}
}

func TestLoadReleaseIndexMapsReferencedIndex(t *testing.T) {
	root := t.TempDir()
	host := "deb.example.org"
	body := []byte("packages")
	sum := sha256.Sum256(body)
	release := `Origin: Debian
SHA256:
 ` + hex.EncodeToString(sum[:]) + ` ` + strconv.Itoa(len(body)) + ` main/binary-amd64/Packages

`
	releasePath := filepath.Join(root, "apt", host, "debian", "dists", "bookworm", "Release")
	if err := os.MkdirAll(filepath.Dir(releasePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releasePath, []byte(release), 0o644); err != nil {
		t.Fatal(err)
	}
	index := NewIndex(root)
	if err := index.LoadFromRoot(filepath.Join(root, "apt")); err != nil {
		t.Fatalf("load from root: %v", err)
	}
	packagesPath := CachePath(root, host, "debian/dists/bookworm/main/binary-amd64/Packages")
	if err := os.MkdirAll(filepath.Dir(packagesPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagesPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := index.ValidateDeb(packagesPath, packagesPath); err != nil {
		t.Fatalf("release mapped file should validate: %v", err)
	}
}

func TestParseReleaseAndPathHelpers(t *testing.T) {
	items := ParseRelease(strings.NewReader(`Origin: Test
SHA256:
 abc 5 main/binary-amd64/Packages
 def 6 main/source/Sources
MD5Sum:
 ignored 1 ignored
`))
	if len(items) != 2 || items[0].Filename != "main/binary-amd64/Packages" {
		t.Fatalf("unexpected release items: %#v", items)
	}
	if CachePath("/cache", "host:80", "/debian/../debian/pool/a.deb") != filepath.Join("/cache", "apt", "host_80", "debian", "pool", "a.deb") {
		t.Fatal("cache path sanitization failed")
	}
	if !IsIndexFile("/dists/bookworm/InRelease") || !IsIndexFile("/dists/bookworm/Packages.gz") {
		t.Fatal("index predicate failed")
	}
	if !IsHashRequest("/by-hash/SHA256/abcd") {
		t.Fatal("hash predicate failed")
	}
	if _, _, err := ParseHashPath("/not/hash"); err == nil {
		t.Fatal("expected invalid hash path")
	}
}

func TestDecompressByNameGzip(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := DecompressByName("Packages.gz", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "body" {
		t.Fatalf("data=%q", data)
	}
	if _, err := DecompressByName("Packages.gz", strings.NewReader("bad gzip")); err == nil {
		t.Fatal("expected gzip error")
	}
}
