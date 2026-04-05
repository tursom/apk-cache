package main

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"
)

func TestAPKIndexServiceLoadFileAndValidatePackage(t *testing.T) {
	root := t.TempDir()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	packageBytes := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("pkg"), false, false)
	indexBytes := buildSignedAPKIndex(t, "test.rsa.pub", privateKey, map[string][]byte{
		"test-1.apk": packageBytes,
	})

	indexPath := filepath.Join(root, "alpine", "v3.20", "main", "x86_64", "APKINDEX.tar.gz")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatalf("mkdir index: %v", err)
	}
	if err := os.WriteFile(indexPath, indexBytes, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	service := NewAPKIndexService(root)
	if err := service.LoadFile(indexPath); err != nil {
		t.Fatalf("load index: %v", err)
	}

	packagePath := filepath.Join(filepath.Dir(indexPath), "test-1.apk")
	if err := os.WriteFile(packagePath, packageBytes, 0o644); err != nil {
		t.Fatalf("write package: %v", err)
	}
	if err := service.ValidatePackage(packagePath); err != nil {
		t.Fatalf("validate package: %v", err)
	}
}

func TestAPKIndexServiceLoadFromRootRestoresExistingIndexes(t *testing.T) {
	root := t.TempDir()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	packageBytes := buildSignedAPKPackage(t, "test.rsa.pub", privateKey, []byte("pkg"), false, false)
	indexBytes := buildSignedAPKIndex(t, "test.rsa.pub", privateKey, map[string][]byte{
		"test-1.apk": packageBytes,
	})

	indexPath := filepath.Join(root, "alpine", "v3.20", "main", "x86_64", "APKINDEX.tar.gz")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatalf("mkdir index: %v", err)
	}
	if err := os.WriteFile(indexPath, indexBytes, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	packagePath := filepath.Join(filepath.Dir(indexPath), "test-1.apk")
	if err := os.WriteFile(packagePath, packageBytes, 0o644); err != nil {
		t.Fatalf("write package: %v", err)
	}

	service := NewAPKIndexService(root)
	if err := service.LoadFromRoot(root); err != nil {
		t.Fatalf("load from root: %v", err)
	}
	if err := service.ValidatePackage(packagePath); err != nil {
		t.Fatalf("validate package after restore: %v", err)
	}
}
