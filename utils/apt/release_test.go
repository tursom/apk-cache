package apt

import (
	"strings"
	"testing"
)

func TestParseReleaseReader_Simple(t *testing.T) {
	input := `Origin: Debian
Label: Debian
Suite: stable
Codename: trixie
Date: Tue, 02 Dec 2025 02:13:36 UTC
Architectures: amd64 arm64
Components: main contrib
Description: Debian trixie
SHA256:
 9fc1b672e39c67284a935ccb159b433ed320106a924c98d4f9a86f9c660bb175   389489 contrib/Contents-all
 eb31200171780aa32dd72882de861f9a8d0ecb09ba79e93804171d6f9a24e9db    10605 contrib/Contents-all.diff/Index
`

	scanner := strings.NewReader(input)
	entries := make([]*File, 0)

	for entry := range ParseReleaseReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// check first entry
	if entries[0].Filename != "contrib/Contents-all" {
		t.Errorf("expected Filename 'contrib/Contents-all', got '%s'", entries[0].Filename)
	}
	if entries[0].Size != 389489 {
		t.Errorf("expected Size '389489', got '%d'", entries[0].Size)
	}
	if entries[0].SHA256 != "9fc1b672e39c67284a935ccb159b433ed320106a924c98d4f9a86f9c660bb175" {
		t.Errorf("incorrect SHA256 for first entry: %s", entries[0].SHA256)
	}

	// check second entry
	if entries[1].Filename != "contrib/Contents-all.diff/Index" {
		t.Errorf("expected Filename 'contrib/Contents-all.diff/Index', got '%s'", entries[1].Filename)
	}
	if entries[1].Size != 10605 {
		t.Errorf("expected Size '10605', got '%d'", entries[1].Size)
	}
	if entries[1].SHA256 != "eb31200171780aa32dd72882de861f9a8d0ecb09ba79e93804171d6f9a24e9db" {
		t.Errorf("incorrect SHA256 for second entry: %s", entries[1].SHA256)
	}
}

func TestParseReleaseReader_NoHashes(t *testing.T) {
	input := `Origin: Debian
Suite: stable
`
	scanner := strings.NewReader(input)
	entries := make([]*File, 0)

	for entry := range ParseReleaseReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseReleaseReader_MultipleHashTypes(t *testing.T) {
	input := `Origin: Debian
MD5:
 abc 100 file1
SHA1:
 def 200 file2
SHA256:
 ghi 300 file3
`
	scanner := strings.NewReader(input)
	entries := make([]*File, 0)

	for entry := range ParseReleaseReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	// 我们只期望 SHA256 条目，因为当前实现可能只处理 SHA256
	// 但测试可以灵活一些
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (SHA256), got %d", len(entries))
	}
	if entries[0].SHA256 != "ghi" {
		t.Errorf("expected SHA256 'ghi', got '%s'", entries[0].SHA256)
	}
	if entries[0].Size != 300 {
		t.Errorf("expected Size '300', got '%d'", entries[0].Size)
	}
	if entries[0].Filename != "file3" {
		t.Errorf("expected Filename 'file3', got '%s'", entries[0].Filename)
	}
}

func TestParseReleaseReader_PGPSignedMessage(t *testing.T) {
	// 包含 PGP 签名块的 Release 文件
	input := `-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

Origin: Debian Backports
Label: Debian Backports
Suite: stable-backports
Codename: trixie-backports
SHA256:
 9fc1b672e39c67284a935ccb159b433ed320106a924c98d4f9a86f9c660bb175   389489 contrib/Contents-all
-----BEGIN PGP SIGNATURE-----
iQIzBAEBCAAdFiEETLUBkCB7R1ij9zp5btDnuCZD4TEFAmkuS3QACgkQbtDnuCZD
4THMAhAAnnxhDTMxtpX2bi/fqqATbhFRNNITD31Cd5YshMbqffqdmrqGVrididc3
-----END PGP SIGNATURE-----
`
	scanner := strings.NewReader(input)
	entries := make([]*File, 0)

	for entry := range ParseReleaseReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Filename != "contrib/Contents-all" {
		t.Errorf("expected Filename 'contrib/Contents-all', got '%s'", entries[0].Filename)
	}
	if entries[0].SHA256 != "9fc1b672e39c67284a935ccb159b433ed320106a924c98d4f9a86f9c660bb175" {
		t.Errorf("incorrect SHA256: %s", entries[0].SHA256)
	}
}

func TestParseReleaseReader_PGPSignedFull(t *testing.T) {
	// 使用用户提供的完整 PGP 签名 Release 文件（仅包含前几行哈希）
	// 确保解析器能够校验 PGP 签名标记并提取哈希
	input := `-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

Origin: Debian Backports
Label: Debian Backports
Suite: stable-backports
Codename: trixie-backports
Changelogs: https://metadata.ftp-master.debian.org/changelogs/@CHANGEPATH@_changelog
Date: Tue, 02 Dec 2025 02:13:36 UTC
Valid-Until: Tue, 09 Dec 2025 02:13:36 UTC
NotAutomatic: yes
ButAutomaticUpgrades: yes
Acquire-By-Hash: yes
No-Support-for-Architecture-all: Packages
Architectures: all amd64 arm64 armel armhf i386 ppc64el riscv64 s390x
Components: main contrib non-free-firmware non-free
Description: Debian trixie - Backports
SHA256:
 9fc1b672e39c67284a935ccb159b433ed320106a924c98d4f9a86f9c660bb175   389489 contrib/Contents-all
 eb31200171780aa32dd72882de861f9a8d0ecb09ba79e93804171d6f9a24e9db    10605 contrib/Contents-all.diff/Index
 b6f9ffef8d676245a5477dcb03f15e72473725c999529bf62749c647c26e5d5b    23437 contrib/Contents-all.gz
 fb067fd4c98056f227b2b348af4703b9658773121d379ac6169472903dd60f55   284954 contrib/Contents-amd64
 2be6447623d0d92bc0af3b2dd285f64a92c99b88bbd98cb1f7a23ad23d0913ad     9483 contrib/Contents-amd64.diff/Index
-----BEGIN PGP SIGNATURE-----
iQIzBAEBCAAdFiEETLUBkCB7R1ij9zp5btDnuCZD4TEFAmkuS3QACgkQbtDnuCZD
4THMAhAAnnxhDTMxtpX2bi/fqqATbhFRNNITD31Cd5YshMbqffqdmrqGVrididc3
-----END PGP SIGNATURE-----
`
	scanner := strings.NewReader(input)
	entries := make([]*File, 0)

	for entry := range ParseReleaseReader(scanner) {
		newEntry := *entry
		entries = append(entries, &newEntry)
	}

	// 应该解析出5个条目
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	// 检查第一个条目
	if entries[0].Filename != "contrib/Contents-all" {
		t.Errorf("expected Filename 'contrib/Contents-all', got '%s'", entries[0].Filename)
	}
	if entries[0].Size != 389489 {
		t.Errorf("expected Size '389489', got '%d'", entries[0].Size)
	}
	if entries[0].SHA256 != "9fc1b672e39c67284a935ccb159b433ed320106a924c98d4f9a86f9c660bb175" {
		t.Errorf("incorrect SHA256 for first entry: %s", entries[0].SHA256)
	}

	// 检查最后一个条目
	last := entries[4]
	if last.Filename != "contrib/Contents-amd64.diff/Index" {
		t.Errorf("expected Filename 'contrib/Contents-amd64.diff/Index', got '%s'", last.Filename)
	}
	if last.Size != 9483 {
		t.Errorf("expected Size '9483', got '%d'", last.Size)
	}
	if last.SHA256 != "2be6447623d0d92bc0af3b2dd285f64a92c99b88bbd98cb1f7a23ad23d0913ad" {
		t.Errorf("incorrect SHA256 for last entry: %s", last.SHA256)
	}
}
