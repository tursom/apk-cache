package apt

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseDiffReader_BinaryPackage(t *testing.T) {
	// 示例内容：二进制包条目
	input := `527a
Package: firmware-nvidia-gsp
Source: nvidia-graphics-drivers
Version: 550.163.01-4~bpo13+1
Installed-Size: 60698
Maintainer: Debian NVIDIA Maintainers <pkg-nvidia-devel@lists.alioth.debian.org>
Architecture: amd64
Description: NVIDIA GSP firmware
Filename: pool/non-free-firmware/n/nvidia-graphics-drivers/firmware-nvidia-gsp_550.163.01-4~bpo13+1_amd64.deb
Size: 37155432
SHA256: 3b6b9485084ee9133d6aec662e6cb2bf90c840237c04c7d4a63c66760d0d7f57

.
`

	scanner := bufio.NewScanner(strings.NewReader(input))
	entries := make([]*AptDiffEntry, 0)

	for entry := range ParseDiffReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Package != "firmware-nvidia-gsp" {
		t.Errorf("expected Package 'firmware-nvidia-gsp', got '%s'", entry.Package)
	}
	if entry.Filename != "pool/non-free-firmware/n/nvidia-graphics-drivers/firmware-nvidia-gsp_550.163.01-4~bpo13+1_amd64.deb" {
		t.Errorf("unexpected Filename: %s", entry.Filename)
	}
	if entry.Size != "37155432" {
		t.Errorf("expected Size '37155432', got '%s'", entry.Size)
	}
	if entry.SHA256 != "3b6b9485084ee9133d6aec662e6cb2bf90c840237c04c7d4a63c66760d0d7f57" {
		t.Errorf("unexpected SHA256: %s", entry.SHA256)
	}
}

func TestParseDiffReader_SourcePackageWithChecksums(t *testing.T) {
	// 示例内容：源包条目，包含 Checksums-Sha256 块
	input := `51a
Package: nvidia-graphics-drivers
Binary: nvidia-driver, firmware-nvidia-gsp
Version: 550.163.01-4~bpo13+1
Maintainer: Debian NVIDIA Maintainers <pkg-nvidia-devel@lists.alioth.debian.org>
Architecture: amd64 arm64
Checksums-Sha256:
 b32c52595efe2fd111b48ce579c77b90ef6d42416fcf4b1942c320b16bd5f292 7332 nvidia-graphics-drivers_550.163.01-4~bpo13+1.dsc
 a6f475b9d3f03c225b89067f38c95417368c97f3180a8895f6ad578634dd6d12 306015701 nvidia-graphics-drivers_550.163.01.orig-amd64.tar.gz
 f0db85c84cd824662d5c0f464f4f07181f5477fe949a740f9d1637114a2e3333 138 nvidia-graphics-drivers_550.163.01.orig.tar.gz
Directory: pool/non-free-firmware/n/nvidia-graphics-drivers
Priority: optional

.
`

	scanner := bufio.NewScanner(strings.NewReader(input))
	entries := make([]*AptDiffEntry, 0)

	for entry := range ParseDiffReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	// 应该有：1个主条目（空行时输出） + 3个 checksum 条目
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// 验证 checksum 条目
	checksumEntries := entries[:3]
	expectedFiles := []string{
		"pool/non-free-firmware/n/nvidia-graphics-drivers/nvidia-graphics-drivers_550.163.01-4~bpo13+1.dsc",
		"pool/non-free-firmware/n/nvidia-graphics-drivers/nvidia-graphics-drivers_550.163.01.orig-amd64.tar.gz",
		"pool/non-free-firmware/n/nvidia-graphics-drivers/nvidia-graphics-drivers_550.163.01.orig.tar.gz",
	}
	expectedHashes := []string{
		"b32c52595efe2fd111b48ce579c77b90ef6d42416fcf4b1942c320b16bd5f292",
		"a6f475b9d3f03c225b89067f38c95417368c97f3180a8895f6ad578634dd6d12",
		"f0db85c84cd824662d5c0f464f4f07181f5477fe949a740f9d1637114a2e3333",
	}

	for i, entry := range checksumEntries {
		if entry.Filename != expectedFiles[i] {
			t.Errorf("checksum entry %d: expected Filename '%s', got '%s'", i, expectedFiles[i], entry.Filename)
		}
		if entry.SHA256 != expectedHashes[i] {
			t.Errorf("checksum entry %d: expected SHA256 '%s', got '%s'", i, expectedHashes[i], entry.SHA256)
		}
	}

	// 验证主条目
	mainEntry := entries[3]
	if mainEntry.Package != "nvidia-graphics-drivers" {
		t.Errorf("expected Package 'nvidia-graphics-drivers', got '%s'", mainEntry.Package)
	}
}

func TestParseDiffReader_MultipleEntries(t *testing.T) {
	// 多个条目
	input := `100a
Package: package-a
Filename: pool/main/a/package-a_1.0_amd64.deb
Size: 1234
SHA256: aaaa1111

.
200a
Package: package-b
Filename: pool/main/b/package-b_2.0_amd64.deb
Size: 5678
SHA256: bbbb2222

.
`

	scanner := bufio.NewScanner(strings.NewReader(input))
	entries := make([]*AptDiffEntry, 0)

	for entry := range ParseDiffReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Package != "package-a" {
		t.Errorf("expected first package 'package-a', got '%s'", entries[0].Package)
	}
	if entries[0].SHA256 != "aaaa1111" {
		t.Errorf("expected first SHA256 'aaaa1111', got '%s'", entries[0].SHA256)
	}

	if entries[1].Package != "package-b" {
		t.Errorf("expected second package 'package-b', got '%s'", entries[1].Package)
	}
	if entries[1].SHA256 != "bbbb2222" {
		t.Errorf("expected second SHA256 'bbbb2222', got '%s'", entries[1].SHA256)
	}
}

func TestParseDiffReader_EdCommands(t *testing.T) {
	// 测试不同的 ed 命令格式
	input := `51a
Package: pkg1
Filename: file1.deb
SHA256: hash1

.
100,200d
300c
Package: pkg2
Filename: file2.deb
SHA256: hash2

.
`

	scanner := bufio.NewScanner(strings.NewReader(input))
	entries := make([]*AptDiffEntry, 0)

	for entry := range ParseDiffReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Package != "pkg1" {
		t.Errorf("expected 'pkg1', got '%s'", entries[0].Package)
	}
	if entries[1].Package != "pkg2" {
		t.Errorf("expected 'pkg2', got '%s'", entries[1].Package)
	}
}

func TestParseDiffReader_EmptyInput(t *testing.T) {
	input := ""
	scanner := bufio.NewScanner(strings.NewReader(input))
	entries := make([]*AptDiffEntry, 0)

	for entry := range ParseDiffReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for empty input, got %d", len(entries))
	}
}

func TestParseDiffReader_NoEndMarker(t *testing.T) {
	// 测试没有结束标记的情况（应该在迭代结束时输出最后的条目）
	input := `51a
Package: pkg-no-end
Filename: file.deb
SHA256: hashvalue
`

	scanner := bufio.NewScanner(strings.NewReader(input))
	entries := make([]*AptDiffEntry, 0)

	for entry := range ParseDiffReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Package != "pkg-no-end" {
		t.Errorf("expected 'pkg-no-end', got '%s'", entries[0].Package)
	}
	if entries[0].SHA256 != "hashvalue" {
		t.Errorf("expected 'hashvalue', got '%s'", entries[0].SHA256)
	}
}

func TestParseDiffReader_ChecksumsWithoutDirectory(t *testing.T) {
	// 测试没有 Directory 字段的 Checksums-Sha256
	input := `51a
Package: some-pkg
Checksums-Sha256:
 abc123 1000 somefile.dsc
 def456 2000 otherfile.tar.gz

.
`

	scanner := bufio.NewScanner(strings.NewReader(input))
	entries := make([]*AptDiffEntry, 0)

	for entry := range ParseDiffReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	// 2个 checksum 条目 + 1个主条目
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// checksum 条目不应该有路径前缀
	if entries[0].Filename != "somefile.dsc" {
		t.Errorf("expected 'somefile.dsc', got '%s'", entries[0].Filename)
	}
	if entries[0].SHA256 != "abc123" {
		t.Errorf("expected 'abc123', got '%s'", entries[0].SHA256)
	}

	if entries[1].Filename != "otherfile.tar.gz" {
		t.Errorf("expected 'otherfile.tar.gz', got '%s'", entries[1].Filename)
	}
}

func TestParseDiffReader_EarlyBreak(t *testing.T) {
	// 测试提前中断迭代
	input := `1a
Package: pkg1
Filename: file1.deb
SHA256: hash1

.
2a
Package: pkg2
Filename: file2.deb
SHA256: hash2

.
3a
Package: pkg3
Filename: file3.deb
SHA256: hash3

.
`

	scanner := bufio.NewScanner(strings.NewReader(input))
	count := 0

	for entry := range ParseDiffReader(scanner) {
		count++
		if entry.Package == "pkg2" {
			break // 提前中断
		}
	}

	if count != 2 {
		t.Errorf("expected to process 2 entries before break, got %d", count)
	}
}
