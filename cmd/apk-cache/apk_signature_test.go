package main

import (
	"crypto/rand"
	"crypto/rsa"
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
