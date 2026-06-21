package apk

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestReadArchiveBytesParsesMultipleMembers(t *testing.T) {
	first := gzipTar(t, map[string][]byte{"one": []byte("1")})
	second := gzipTar(t, map[string][]byte{"two": []byte("2")})

	members, err := ReadArchiveBytes(append(first, second...))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("members=%d", len(members))
	}
	if members[0].Entries[0].Name != "one" || string(members[1].Entries[0].Body) != "2" {
		t.Fatalf("unexpected entries: %#v", members)
	}
}

func TestAPKIndexLoadAndValidatePackage(t *testing.T) {
	dir := t.TempDir()
	packageBody := []byte("apk payload")
	sum := sha256.Sum256(packageBody)
	indexBody := []byte("P:hello\nV:1.0-r0\nS:" +
		strconv.Itoa(len(packageBody)) + "\nC:Q2" +
		base64.RawStdEncoding.EncodeToString(sum[:]) + "\n\n")

	indexPath := filepath.Join(dir, "APKINDEX.tar.gz")
	if err := os.WriteFile(indexPath, gzipTar(t, map[string][]byte{"APKINDEX": indexBody}), 0o644); err != nil {
		t.Fatal(err)
	}

	index := NewIndex(dir)
	if err := index.LoadFile(indexPath); err != nil {
		t.Fatalf("load index: %v", err)
	}
	packagePath := filepath.Join(dir, "hello-1.0-r0.apk")
	if err := os.WriteFile(packagePath, packageBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := index.ValidatePackage(packagePath, packagePath); err != nil {
		t.Fatalf("validate package: %v", err)
	}
	if err := os.WriteFile(packagePath, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := index.ValidatePackage(packagePath, packagePath); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
}

func TestAPKIndexLoadFromRoot(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "alpine", "v3.23", "main", "x86_64", "APKINDEX.tar.gz")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, gzipTar(t, map[string][]byte{"APKINDEX": []byte("P:p\nV:1\nC:" + sha256Hex("x") + "\n\n")}), 0o644); err != nil {
		t.Fatal(err)
	}
	index := NewIndex(dir)
	if err := index.LoadFromRoot(dir); err != nil {
		t.Fatalf("load from root: %v", err)
	}
	if err := index.ValidatePackage(filepath.Join(filepath.Dir(indexPath), "p-1.apk"), filepath.Join(filepath.Dir(indexPath), "missing.apk")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file error for indexed missing package, got %v", err)
	}
}

func TestDecodeChecksumVariantsAndPredicates(t *testing.T) {
	sha1Alg, _, err := DecodeChecksum("0123456789012345678901234567890123456789")
	if err != nil || sha1Alg != "sha1" {
		t.Fatalf("sha1 checksum: algorithm=%s err=%v", sha1Alg, err)
	}
	sha256Alg, _, err := DecodeChecksum(sha256Hex("abc"))
	if err != nil || sha256Alg != "sha256" {
		t.Fatalf("sha256 checksum: algorithm=%s err=%v", sha256Alg, err)
	}
	if _, _, err := DecodeChecksum("bad"); err == nil {
		t.Fatal("expected unsupported checksum error")
	}
	if !IsIndexFile("/alpine/v3.23/main/x86_64/APKINDEX.tar.gz") || !IsPackageFile("a.apk") || IsPackageFile("a.deb") {
		t.Fatal("file predicates failed")
	}
}

func TestVerifierValidatesSignedArchive(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	archive := signedArchive(t, key, "test.rsa.pub", map[string][]byte{"DESCRIPTION": []byte("payload")})
	path := filepath.Join(t.TempDir(), "pkg.apk")
	if err := os.WriteFile(path, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	verifier := &Verifier{keys: map[string]*rsa.PublicKey{"test.rsa.pub": &key.PublicKey}}
	if err := verifier.ValidateArchive(path); err != nil {
		t.Fatalf("validate signed archive: %v", err)
	}
}

func TestNewVerifierLoadsExtraKeysAndRejectsBadKeysDir(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extra.rsa.pub"), pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}), 0o644); err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(dir)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if verifier.keys["extra.rsa.pub"] == nil {
		t.Fatal("extra key not loaded")
	}

	badDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(badDir, "bad.pub"), []byte("not a key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewVerifier(badDir); err == nil {
		t.Fatal("expected invalid key error")
	}
	if _, err := NewVerifier(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing keys dir error")
	}
}

func TestVerifierRejectsUnsignedAndWrongKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pkg.apk")
	if err := os.WriteFile(path, gzipTar(t, map[string][]byte{"DESCRIPTION": []byte("payload")}), 0o644); err != nil {
		t.Fatal(err)
	}
	verifier := &Verifier{keys: map[string]*rsa.PublicKey{}}
	if err := verifier.ValidateArchive(path); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("expected unsigned, got %v", err)
	}

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	wrong, _ := rsa.GenerateKey(rand.Reader, 2048)
	if err := os.WriteFile(path, signedArchive(t, key, "test.rsa.pub", map[string][]byte{"DESCRIPTION": []byte("payload")}), 0o644); err != nil {
		t.Fatal(err)
	}
	verifier = &Verifier{keys: map[string]*rsa.PublicKey{"test.rsa.pub": &wrong.PublicKey}}
	if err := verifier.ValidateArchive(path); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("expected invalid signature, got %v", err)
	}
}

func TestSignatureNameHashAndKeyLookupBranches(t *testing.T) {
	if name, hashType, err := parseSignatureName(".SIGN.RSA.key"); err != nil || name != "key" || hashType != crypto.SHA1 {
		t.Fatalf("rsa signature name: %s %v %v", name, hashType, err)
	}
	if _, _, err := parseSignatureName(".SIGN.UNKNOWN.key"); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("expected invalid signature name, got %v", err)
	}
	if len(hashSignedMember(crypto.SHA1, []byte("data"))) != sha1.Size {
		t.Fatal("sha1 digest size mismatch")
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verifier := &Verifier{keys: map[string]*rsa.PublicKey{"prefix-key": &key.PublicKey}}
	if len(verifier.lookupKeys("key")) != 1 {
		t.Fatal("suffix lookup failed")
	}
	if len(verifier.lookupKeys("missing")) != 1 {
		t.Fatal("fallback lookup should return all keys")
	}
}

func TestParseRSAPublicKeySupportsPEM(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	parsed, err := ParseRSAPublicKey(pemBytes)
	if err != nil {
		t.Fatalf("parse pem key: %v", err)
	}
	if parsed.N.Cmp(key.N) != 0 {
		t.Fatal("parsed key does not match")
	}
}

func gzipTar(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var gz bytes.Buffer
	gzw := gzip.NewWriter(&gz)
	tw := tar.NewWriter(gzw)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return gz.Bytes()
}

func signedArchive(t *testing.T, key *rsa.PrivateKey, keyName string, entries map[string][]byte) []byte {
	t.Helper()
	signedMember := gzipTar(t, entries)
	sum := sha256.Sum256(signedMember)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	signatureMember := gzipTar(t, map[string][]byte{".SIGN.RSA256." + keyName: signature})
	return append(signatureMember, signedMember...)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
