package main

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNewAPKVerifierIncludesBuiltinKeys(t *testing.T) {
	verifier, err := NewAPKVerifier("")
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if len(verifier.keys) < len(defaultAPKTrustedKeys) {
		t.Fatalf("verifier loaded %d keys, want at least %d", len(verifier.keys), len(defaultAPKTrustedKeys))
	}
}

func TestAPKVerifierFallsBackToAllTrustedKeysWhenSignerNameDiffers(t *testing.T) {
	root := t.TempDir()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	keyDir := filepath.Join(root, "keys")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatalf("mkdir keys: %v", err)
	}
	writeTrustedKey(t, keyDir, "trusted-name-does-not-match.rsa.pub", privateKey)

	verifier, err := NewAPKVerifier(keyDir)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	archivePath := filepath.Join(root, "test.apk")
	archiveBytes := buildSignedAPKPackage(t, "unexpected-signer-name.rsa.pub", privateKey, []byte("pkg"), false, false)
	if err := os.WriteFile(archivePath, archiveBytes, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	if err := verifier.ValidatePackageSignature(archivePath); err != nil {
		t.Fatalf("validate package signature: %v", err)
	}
}

func TestAPKVerifierRejectsPackageWithoutSignature(t *testing.T) {
	root := t.TempDir()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	keyDir := filepath.Join(root, "keys")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatalf("mkdir keys: %v", err)
	}
	writeTrustedKey(t, keyDir, "test.rsa.pub", privateKey)

	verifier, err := NewAPKVerifier(keyDir)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	archivePath := filepath.Join(root, "test.apk")
	archiveBytes := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("pkg"), true, false)
	if err := os.WriteFile(archivePath, archiveBytes, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	err = verifier.ValidatePackageSignature(archivePath)
	if err == nil {
		t.Fatalf("expected error for unsigned package")
	}
	if !errors.Is(err, ErrAPKUnsigned) {
		t.Fatalf("expected ErrAPKUnsigned, got %v", err)
	}
}

func TestAPKVerifierRejectsCorruptedSignature(t *testing.T) {
	root := t.TempDir()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	keyDir := filepath.Join(root, "keys")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatalf("mkdir keys: %v", err)
	}
	writeTrustedKey(t, keyDir, "test.rsa.pub", privateKey)

	verifier, err := NewAPKVerifier(keyDir)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	archivePath := filepath.Join(root, "test.apk")
	archiveBytes := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("pkg"), false, true)
	if err := os.WriteFile(archivePath, archiveBytes, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	err = verifier.ValidatePackageSignature(archivePath)
	if err == nil {
		t.Fatalf("expected error for corrupted signature")
	}
	if !errors.Is(err, ErrAPKSignatureInvalid) {
		t.Fatalf("expected ErrAPKSignatureInvalid, got %v", err)
	}
}

func TestAPKVerifierRejectsTamperedPackage(t *testing.T) {
	root := t.TempDir()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	keyDir := filepath.Join(root, "keys")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatalf("mkdir keys: %v", err)
	}
	writeTrustedKey(t, keyDir, "test.rsa.pub", privateKey)

	verifier, err := NewAPKVerifier(keyDir)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	archiveBytes := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("pkg"), false, false)

	// Tamper with the control member that the signature covers.
	// The APK archive is: [gzip(signature)] [gzip(control)] [gzip(data)]
	// Skip past the first gzip member and modify a byte deep enough
	// into the second (control) member to invalidate the hash.
	br := bytes.NewReader(archiveBytes)
	gz, err := gzip.NewReader(br)
	if err != nil {
		t.Fatalf("read signature member: %v", err)
	}
	gz.Multistream(false)
	if _, err := io.Copy(io.Discard, gz); err != nil {
		t.Fatalf("skip signature member: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip reader: %v", err)
	}
	controlStart, err := br.Seek(0, io.SeekCurrent)
	if err != nil {
		t.Fatalf("seek control member: %v", err)
	}
	if controlStart+30 < int64(len(archiveBytes)) {
		archiveBytes[controlStart+30] ^= 0xFF
	} else {
		t.Fatalf("archive too short to tamper control member")
	}

	archivePath := filepath.Join(root, "test.apk")
	if err := os.WriteFile(archivePath, archiveBytes, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	err = verifier.ValidatePackageSignature(archivePath)
	if err == nil {
		t.Fatalf("expected error for tampered package")
	}
}

func TestAPKVerifierRejectsWrongKey(t *testing.T) {
	root := t.TempDir()
	keyA, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	keyB, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}

	keyDir := filepath.Join(root, "keys")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatalf("mkdir keys: %v", err)
	}
	writeTrustedKey(t, keyDir, "test-key-b.rsa.pub", keyB)

	verifier, err := NewAPKVerifier(keyDir)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	archivePath := filepath.Join(root, "test.apk")
	archiveBytes := buildSignedAPKPackage(t, "test-key-a.rsa.pub", keyA, []byte("pkg"), false, false)
	if err := os.WriteFile(archivePath, archiveBytes, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	err = verifier.ValidatePackageSignature(archivePath)
	if err == nil {
		t.Fatalf("expected error with wrong key")
	}
	if !errors.Is(err, ErrAPKSignatureInvalid) {
		t.Fatalf("expected ErrAPKSignatureInvalid, got %v", err)
	}
}
