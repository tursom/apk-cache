package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type apkFixturePackage struct {
	Name           string
	Version        string
	Body           []byte
	Unsigned       bool
	InvalidSig     bool
	HashAlgorithm  string
	ExpectedStatus string
}

func writeTrustedKey(t *testing.T, dir, name string, key *rsa.PrivateKey) string {
	t.Helper()

	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, block, 0o644); err != nil {
		t.Fatalf("write trusted key: %v", err)
	}
	return path
}

func buildSignedAPKIndex(t *testing.T, signerName string, privateKey *rsa.PrivateKey, records map[string][]byte) []byte {
	t.Helper()

	var lines []string
	for filename, packageBytes := range records {
		base := filepath.Base(filename)
		name, version := splitAPKFilename(t, base)
		sum := sha256.Sum256(packageBytes)
		lines = append(lines,
			"P:"+name,
			"V:"+version,
			fmt.Sprintf("S:%d", len(packageBytes)),
			"C:Q2"+base64.RawStdEncoding.EncodeToString(sum[:]),
			"",
		)
	}
	indexBody := strings.Join(lines, "\n")
	return buildSignedArchive(t, signerName, privateKey, "DESCRIPTION", []byte(indexBody), false, false)
}

func buildSignedAPKPackage(t *testing.T, signerName string, privateKey *rsa.PrivateKey, payload []byte, unsigned, invalidSig bool) []byte {
	t.Helper()
	return buildSignedArchive(t, signerName, privateKey, ".PKGINFO", payload, unsigned, invalidSig)
}

func buildSignedArchive(t *testing.T, signerName string, privateKey *rsa.PrivateKey, entryName string, entryBody []byte, unsigned, invalidSig bool) []byte {
	t.Helper()

	controlMember := gzipTarMember(t, map[string][]byte{
		entryName: entryBody,
	})
	var archive bytes.Buffer
	if !unsigned {
		digest := sha256.Sum256(controlMember)
		signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatalf("sign archive member: %v", err)
		}
		if invalidSig {
			signature[0] ^= 0xFF
		}
		archive.Write(gzipTarMember(t, map[string][]byte{
			".SIGN.RSA256." + signerName: signature,
		}))
	}
	archive.Write(controlMember)
	if entryName == ".PKGINFO" {
		archive.Write(gzipTarMember(t, map[string][]byte{
			"data.tar": []byte("payload"),
		}))
	}
	return archive.Bytes()
}

func gzipTarMember(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var compressed bytes.Buffer
	gzWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzWriter)
	for name, body := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(body)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tarWriter.Write(body); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return compressed.Bytes()
}

func splitAPKFilename(t *testing.T, base string) (string, string) {
	t.Helper()

	trimmed := strings.TrimSuffix(base, ".apk")
	idx := strings.LastIndex(trimmed, "-")
	if idx <= 0 {
		t.Fatalf("unexpected apk filename: %s", base)
	}
	return trimmed[:idx], trimmed[idx+1:]
}
