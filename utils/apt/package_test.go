package apt

import (
	"strings"
	"testing"
)

func TestParsePackageReader_Simple(t *testing.T) {
	input := `Package: aide
Version: 0.19.1-2+deb13u1
Installed-Size: 330
Maintainer: Aide Maintainers <aide@packages.debian.org>
Architecture: amd64
Replaces: aide-dynamic, aide-xen
Provides: aide-binary
Depends: libacl1 (>= 2.2.23), libaudit1 (>= 1:2.2.1), libc6 (>= 2.38), libcap2 (>= 1:2.10), libext2fs2t64 (>= 1.46.2), libnettle8t64 (>= 3.8), libpcre2-8-0 (>= 10.22), libselinux1 (>= 3.1~), zlib1g (>= 1:1.1.4)
Suggests: figlet
Description: Advanced Intrusion Detection Environment - dynamic binary
Homepage: https://aide.github.io
Description-md5: 1d70ba920a3b80bc791be197bf18814c
Recommends: aide-common (= 0.19.1-2+deb13u1)
Section: admin
Priority: optional
Filename: pool/updates/main/a/aide/aide_0.19.1-2+deb13u1_amd64.deb
Size: 147836
SHA256: c356918c0fb2b9d93b5160531237b4bf5773209f8ed0382d5fcde8efff80f538

`
	entries := make([]*File, 0)

	for entry := range ParsePackageReader(strings.NewReader(input)) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Package != "aide" {
		t.Errorf("expected Package 'aide', got '%s'", entry.Package)
	}
	if entry.Filename != "pool/updates/main/a/aide/aide_0.19.1-2+deb13u1_amd64.deb" {
		t.Errorf("expected Filename 'pool/updates/main/a/aide/aide_0.19.1-2+deb13u1_amd64.deb', got '%s'", entry.Filename)
	}
	if entry.Size != 147836 {
		t.Errorf("expected Size 147836, got %d", entry.Size)
	}
	if entry.SHA256 != "c356918c0fb2b9d93b5160531237b4bf5773209f8ed0382d5fcde8efff80f538" {
		t.Errorf("expected SHA256 'c356918c0fb2b9d93b5160531237b4bf5773209f8ed0382d5fcde8efff80f538', got '%s'", entry.SHA256)
	}
}

func TestParsePackageReader_MultipleEntries(t *testing.T) {
	input := `Package: foo
Filename: pool/main/f/foo/foo_1.0_amd64.deb
Size: 1234
SHA256: aaaa1111

Package: bar
Filename: pool/main/b/bar/bar_2.0_amd64.deb
Size: 5678
SHA256: bbbb2222

`
	entries := make([]*File, 0)

	for entry := range ParsePackageReader(strings.NewReader(input)) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Package != "foo" {
		t.Errorf("expected first Package 'foo', got '%s'", entries[0].Package)
	}
	if entries[0].SHA256 != "aaaa1111" {
		t.Errorf("expected first SHA256 'aaaa1111', got '%s'", entries[0].SHA256)
	}
	if entries[1].Package != "bar" {
		t.Errorf("expected second Package 'bar', got '%s'", entries[1].Package)
	}
	if entries[1].SHA256 != "bbbb2222" {
		t.Errorf("expected second SHA256 'bbbb2222', got '%s'", entries[1].SHA256)
	}
}

func TestParsePackageReader_NoPackageField(t *testing.T) {
	// 缺少 Package 字段，应该跳过或产生空 Package？
	input := `Filename: some.deb
Size: 100
SHA256: hash



`
	entries := make([]*File, 0)

	for entry := range ParsePackageReader(strings.NewReader(input)) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	// 根据实现，可能产生一个 Package 为空的条目，或者跳过
	// 我们暂时假设会产生一个条目
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Package != "" {
		t.Errorf("expected empty Package, got '%s'", entries[0].Package)
	}
}

func TestParsePackageReader_EmptyInput(t *testing.T) {
	input := ""
	entries := make([]*File, 0)

	for entry := range ParsePackageReader(strings.NewReader(input)) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for empty input, got %d", len(entries))
	}
}

func TestParsePackageReader_PartialEntry(t *testing.T) {
	// 不完整的条目（缺少空行结束），应该仍然能解析
	input := `Package: partial
Filename: partial.deb
SHA256: partialhash`
	entries := make([]*File, 0)

	for entry := range ParsePackageReader(strings.NewReader(input)) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Package != "partial" {
		t.Errorf("expected Package 'partial', got '%s'", entries[0].Package)
	}
}
