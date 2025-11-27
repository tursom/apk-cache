package utils

import (
	"testing"
)

// 测试用例数据
var testCases = []struct {
	path     string
	expected PackageType
	desc     string
}{
	// APK 测试用例
	{"/alpine/v3.18/main/x86_64/package-1.0.0-r0.apk", PackageTypeAPK, "APK包文件"},
	{"/alpine/edge/main/x86_64/APKINDEX.tar.gz", PackageTypeAPK, "APK索引文件"},
	{"/alpine/v3.17/community/aarch64/", PackageTypeAPK, "Alpine仓库路径"},
	{"/alpine/latest-stable/main/x86_64/test.apk", PackageTypeAPK, "APK文件"},

	// APT 测试用例
	{"/ubuntu/dists/focal/main/binary-amd64/Packages.gz", PackageTypeAPT, "APT索引文件"},
	{"/debian/pool/main/g/gcc/gcc_10.2.1-1_amd64.deb", PackageTypeAPT, "DEB包文件"},
	{"/ubuntu/dists/jammy/", PackageTypeAPT, "APT发行版路径"},
	{"/debian/pool/contrib/", PackageTypeAPT, "APT软件池"},
	{"/by-hash/SHA256/abc123", PackageTypeAPT, "哈希请求(默认APT)"},

	// 未知类型
	{"/other/file.txt", PackageTypeUnknown, "未知文件类型"},
	{"/random/path/", PackageTypeUnknown, "随机路径"},
}

// 基准测试数据
var benchmarkPaths = []string{
	"/ubuntu/dists/noble-security/universe/binary-amd64/by-hash/SHA256/c92f7e7bc306b24ac3f668b16e43b3659e469a8f2f2f0acce383bb6a91fafc5e",
	"/ubuntu/dists/noble-security/restricted/binary-amd64/by-hash/SHA256/2a4f602eab0793435cd6b26bfcf95650efb84b10a9201c3174774fd2d919c71b",
	"/debian/pve/dists/trixie/pve-no-subscription/binary-amd64/libpve-storage-perl_9.1.0_all.deb",
	"/debian/pbs/dists/trixie/pbs-no-subscription/binary-amd64/proxmox-backup-docs_4.0.22-1_all.deb",
	"/linux/ubuntu/dists/noble/pool/stable/amd64/docker-buildx-plugin_0.30.0-1~ubuntu.24.04~noble_amd64.deb",
	"/ubuntu/pool/main/libd/libdrm/libdrm-amdgpu1_2.4.122-1~ubuntu0.24.04.2_amd64.deb",
	"/alpine/v3.22/main/x86_64/xcb-util-0.4.1-r3.apk",
	"/alpine/v3.22/main/x86_64/xkeyboard-config-2.43-r0.apk",
	"/alpine/v3.22/main/x86_64/xz-5.8.1-r0.apk",
	"/alpine/v3.22/main/x86_64/xz-dev-5.8.1-r0.apk",
	"/alpine/v3.22/main/x86_64/yaml-0.2.5-r2.apk",
	"/alpine/v3.22/main/x86_64/zlib-dev-1.3.1-r2.apk",
	"/debian/dists/bookworm-backports/InRelease",
	"/debian-security/dists/bookworm-security/InRelease",
	"/debian-security/dists/bookworm-security/main/binary-amd64/by-hash/SHA256/0443583a234e303a356a0d243166e9271e29624fd831ec9646474719a8c82b51",
	"/debian-security/dists/bookworm-security/main/i18n/by-hash/SHA256/cc5a4c4861319e62e2619c8e35811da6435e1ea1dc32d1c56cadaf029cd7926d",
	"/debian-security/dists/bookworm-security/main/source/by-hash/SHA256/c251c8604efa9bf6fccd6799d6fe79b40a68ff38e7031dd431ec10f9fca40546",
	"/debian-security/dists/trixie-security/InRelease",
	"/debian-security/dists/trixie-security/main/binary-amd64/by-hash/SHA256/c583b26731dfc388ecf121d81f6bc625bfe11ad4acbc16396864b0ddb8229e28",
	"/debian-security/dists/trixie-security/main/i18n/by-hash/SHA256/1dc256d7e720e4987d14aca76e3e2408bf143d3f76a6de772fecf9f4d92b9f55",
	"/debian-security/dists/trixie-security/main/source/by-hash/SHA256/271ae7a6860ff2eea21e2ea164008173a598733bf31779e9be789aaa07ad67ca",
	"/dists/bookworm-security/InRelease",
	"/dists/bookworm-security/main/binary-amd64/by-hash/SHA256/0443583a234e303a356a0d243166e9271e29624fd831ec9646474719a8c82b51",
	"/dists/bookworm-security/main/i18n/by-hash/SHA256/cc5a4c4861319e62e2619c8e35811da6435e1ea1dc32d1c56cadaf029cd7926d",
	"/ubuntu/dists/noble-security/InRelease",
	"/ubuntu/dists/noble-security/main/binary-amd64/by-hash/SHA256/d2f34e36ca4241ea445f81059f36a25a53145ed98789bc49ec22e726aa57e37c",
	"/ubuntu/dists/noble-security/main/cnf/by-hash/SHA256/664c4cce5ed22e3888d14998af03795048c1d0721aa18f53d5af2316d4e17cb2",
	"/ubuntu/dists/noble-security/main/i18n/by-hash/SHA256/53c31fc9b3fb9d68d9c682d393523634c3907f1443e258f52fff599201ed7730",
	"/ubuntu/dists/noble-security/multiverse/binary-amd64/by-hash/SHA256/3428474ee4a0feeda1589886e7b7535ef35ae8ecf7d907e326d19ce7e4f5ae8f",
	"/ubuntu/dists/noble-security/multiverse/cnf/by-hash/SHA256/079712668a40a623e563a1b55965f09b4df0f8fc86b3fea6cc3d37b6b247bf06",
	"/ubuntu/dists/noble-security/multiverse/i18n/by-hash/SHA256/ef78e3cddb70c9be7903b1118292b369d44e89e9ffdf1108023eb5797dd2711e",
	"/ubuntu/dists/noble-security/restricted/binary-amd64/by-hash/SHA256/2a4f602eab0793435cd6b26bfcf95650efb84b10a9201c3174774fd2d919c71b",
	"/ubuntu/dists/noble-security/restricted/cnf/by-hash/SHA256/bc44572c9f1020c0dd2ab952c32962cc11ffd39d93fd8658417cc4df7bdbf6af",
	"/ubuntu/dists/noble-security/restricted/i18n/by-hash/SHA256/84cc8eb22a7ce723e18a62dafc051d60a0f382c76ca4b9cd3929b0647487c0bc",
	"/ubuntu/dists/noble-security/universe/binary-amd64/by-hash/SHA256/c92f7e7bc306b24ac3f668b16e43b3659e469a8f2f2f0acce383bb6a91fafc5e",
	"/ubuntu/dists/noble-security/universe/cnf/by-hash/SHA256/8ce42e9fbc6902482626ac198dd6fd1f73e86bb66fa9d858e72eed22d968b008",
	"/ubuntu/dists/noble-security/universe/i18n/by-hash/SHA256/3c2d4503889027ca51df58e16ec12798d6b438290662e006efab80806ddcb18c",
	"/ubuntu/pool/main/p/python3.12/python3.12_3.12.3-1ubuntu0.9_amd64.deb",
	"/ubuntu/pool/main/p/python3.12/python3.12-dev_3.12.3-1ubuntu0.9_amd64.deb",
	"/ubuntu/pool/main/p/python3.12/python3.12-minimal_3.12.3-1ubuntu0.9_amd64.deb",
	"/ubuntu/pool/main/s/systemd-hwe/systemd-hwe-hwdb_255.1.6_all.deb",
	"/ubuntu/pool/main/s/systemd/libnss-systemd_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/libpam-systemd_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/libsystemd0_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/libsystemd-shared_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/libudev1_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/systemd_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/systemd-dev_255.4-1ubuntu8.11_all.deb",
	"/ubuntu/pool/main/s/systemd/systemd-resolved_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/systemd-sysv_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/systemd-timesyncd_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/s/systemd/udev_255.4-1ubuntu8.11_amd64.deb",
	"/ubuntu/pool/main/u/ubuntu-advantage-tools/ubuntu-pro-client_37.1ubuntu0~24.04_amd64.deb",
	"/ubuntu/pool/main/u/ubuntu-advantage-tools/ubuntu-pro-client-l10n_37.1ubuntu0~24.04_amd64.deb",
	"/ubuntu/pool/universe/g/gcc-11/cpp-11_11.5.0-1ubuntu1~24.04_amd64.deb",
	"/ubuntu/pool/universe/g/gcc-11/gcc-11_11.5.0-1ubuntu1~24.04_amd64.deb",
	"/ubuntu/pool/universe/g/gcc-11/gcc-11-base_11.5.0-1ubuntu1~24.04_amd64.deb",
	"/ubuntu/pool/universe/g/gcc-11/libasan6_11.5.0-1ubuntu1~24.04_amd64.deb",
	"/ubuntu/pool/universe/g/gcc-11/libgcc-11-dev_11.5.0-1ubuntu1~24.04_amd64.deb",
	"/ubuntu/pool/universe/g/gcc-11/libtsan0_11.5.0-1ubuntu1~24.04_amd64.deb",
	"/ubuntu/pool/universe/g/gcc-12/gcc-12-base_12.4.0-2ubuntu1~24.04_amd64.deb",
	"/ubuntu/pool/universe/p/python3.12/python3.12-venv_3.12.3-1ubuntu0.9_amd64.deb",
	"/ubuntu/pool/universe/t/tree/tree_2.1.1-2ubuntu3.24.04.2_amd64.deb",
	"/ubuntu/pool/universe/u/ubuntu-advantage-tools/ubuntu-advantage-tools_37.1ubuntu0~24.04_all.deb",
	"/debian/dists/bookworm-updates/InRelease",
	"/debian/dists/trixie-backports/InRelease",
	"/debian/dists/trixie-backports/main/binary-amd64/Packages.diff/by-hash/SHA256/ce515c689718f95ad5b34d2d25eb487f68b3b2cdb6a6a02975cfb360af24930b",
	"/debian/dists/trixie-backports/main/binary-amd64/Packages.diff/T-2025-11-26-0204.33-F-2025-11-23-1407.32.gz",
	"/debian/dists/trixie-backports/main/binary-amd64/Packages.diff/T-2025-11-26-0204.33-F-2025-11-25-1412.46.gz",
	"/debian/dists/trixie-backports/main/binary-amd64/Packages.diff/T-2025-11-26-0204.33-F-2025-11-26-0204.33.gz",
	"/debian/dists/trixie-backports/main/i18n/Translation-en.diff/by-hash/SHA256/d031353598c8b573e72dbc736101c1e2fd66d283266be220eb45d7de97647d69",
	"/debian/dists/trixie-backports/main/source/Sources.diff/by-hash/SHA256/4574adcdd034915c0a6149bdb75c0b46842f960a0952f1a7618aabc3edf8be2c",
	"/debian/dists/trixie-backports/main/source/Sources.diff/T-2025-11-26-0204.33-F-2025-11-26-0204.33.gz",
	"/debian/dists/trixie-backports/non-free-firmware/binary-amd64/Packages.diff/by-hash/SHA256/eb37e3761c691cf250e92fc8ef6a634ab9a9c2923be8c21d7188da12bf333ba9",
	"/debian/dists/trixie-backports/non-free-firmware/binary-amd64/Packages.diff/T-2025-11-26-0204.33-F-2025-11-26-0204.33.gz",
	"/debian/dists/trixie-backports/non-free-firmware/source/Sources.diff/by-hash/SHA256/a0fb943ddbf48d4ece367a64d4b412bfc09b016b51367489757a95c54a7c7a0b",
	"/debian/dists/trixie-backports/non-free-firmware/source/Sources.diff/T-2025-11-26-0204.33-F-2025-11-26-0204.33.gz",
	"/debian/dists/trixie-updates/InRelease",
	"/linux/debian/dists/trixie/InRelease",
	"/linux/ubuntu/dists/noble/InRelease",
	"/linux/ubuntu/dists/noble/pool/stable/amd64/docker-buildx-plugin_0.30.0-1~ubuntu.24.04~noble_amd64.deb",
	"/linux/ubuntu/dists/noble/pool/stable/amd64/docker-ce_29.0.4-1~ubuntu.24.04~noble_amd64.deb",
	"/linux/ubuntu/dists/noble/pool/stable/amd64/docker-ce-cli_29.0.4-1~ubuntu.24.04~noble_amd64.deb",
	"/linux/ubuntu/dists/noble/pool/stable/amd64/docker-ce-rootless-extras_29.0.4-1~ubuntu.24.04~noble_amd64.deb",
	"/linux/ubuntu/dists/noble/pool/stable/amd64/docker-compose-plugin_2.40.3-1~ubuntu.24.04~noble_amd64.deb",
	"/debian/pbs/dists/trixie/InRelease",
	"/debian/pbs/dists/trixie/pbs-no-subscription/binary-amd64/Packages.gz",
	"/debian/pbs/dists/trixie/pbs-no-subscription/binary-amd64/pbs-i18n_3.6.3_all.deb",
	"/debian/pbs/dists/trixie/pbs-no-subscription/binary-amd64/proxmox-backup-docs_4.0.22-1_all.deb",
	"/debian/pbs/dists/trixie/pbs-no-subscription/binary-amd64/proxmox-backup-server_4.0.22-1_amd64.deb",
	"/debian/pve/dists/trixie/pve-no-subscription/binary-amd64/libpve-storage-perl_9.1.0_all.deb",
}

func TestDetectPackageType(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := DetectPackageType(tc.path)
			if result != tc.expected {
				t.Errorf("DetectPackageTypeFast(%q) = %v; want %v",
					tc.path, result, tc.expected)
			}
		})
	}
}

// 性能基准测试
func BenchmarkDetectPackageType(b *testing.B) {

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, path := range benchmarkPaths {
			DetectPackageType(path)
		}
	}
}

func BenchmarkDetectPackageTypeFast(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, path := range benchmarkPaths {
			DetectPackageTypeFast(path)
		}
	}
}
