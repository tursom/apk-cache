package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultAPKTrustedKeys 内置 Alpine 默认 RSA 公钥，避免首次启用验签时还需要额外准备 keyring。
// 这些 key 仍然会被 keys_dir 中的用户自定义 key 补充或覆盖。
var defaultAPKTrustedKeys = func() map[string]*rsa.PublicKey {
	return map[string]*rsa.PublicKey{
		"alpine-devel@lists.alpinelinux.org-4a6a0840.rsa.pub": mustParseRSAPublicKey([]byte(`-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA1yHJxQgsHQREclQu4Ohe
qxTxd1tHcNnvnQTu/UrTky8wWvgXT+jpveroeWWnzmsYlDI93eLI2ORakxb3gA2O
Q0Ry4ws8vhaxLQGC74uQR5+/yYrLuTKydFzuPaS1dK19qJPXB8GMdmFOijnXX4SA
jixuHLe1WW7kZVtjL7nufvpXkWBGjsfrvskdNA/5MfxAeBbqPgaq0QMEfxMAn6/R
L5kNepi/Vr4S39Xvf2DzWkTLEK8pcnjNkt9/aafhWqFVW7m3HCAII6h/qlQNQKSo
GuH34Q8GsFG30izUENV9avY7hSLq7nggsvknlNBZtFUcmGoQrtx3FmyYsIC8/R+B
ywIDAQAB
-----END PUBLIC KEY-----`)),
		"alpine-devel@lists.alpinelinux.org-5261cecb.rsa.pub": mustParseRSAPublicKey([]byte(`-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAwlzMkl7b5PBdfMzGdCT0
cGloRr5xGgVmsdq5EtJvFkFAiN8Ac9MCFy/vAFmS8/7ZaGOXoCDWbYVLTLOO2qtX
yHRl+7fJVh2N6qrDDFPmdgCi8NaE+3rITWXGrrQ1spJ0B6HIzTDNEjRKnD4xyg4j
g01FMcJTU6E+V2JBY45CKN9dWr1JDM/nei/Pf0byBJlMp/mSSfjodykmz4Oe13xB
Ca1WTwgFykKYthoLGYrmo+LKIGpMoeEbY1kuUe04UiDe47l6Oggwnl+8XD1MeRWY
sWgj8sF4dTcSfCMavK4zHRFFQbGp/YFJ/Ww6U9lA3Vq0wyEI6MCMQnoSMFwrbgZw
wwIDAQAB
-----END PUBLIC KEY-----`)),
		"alpine-devel@lists.alpinelinux.org-6165ee59.rsa.pub": mustParseRSAPublicKey([]byte(`-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAutQkua2CAig4VFSJ7v54
ALyu/J1WB3oni7qwCZD3veURw7HxpNAj9hR+S5N/pNeZgubQvJWyaPuQDm7PTs1+
tFGiYNfAsiibX6Rv0wci3M+z2XEVAeR9Vzg6v4qoofDyoTbovn2LztaNEjTkB+oK
tlvpNhg1zhou0jDVYFniEXvzjckxswHVb8cT0OMTKHALyLPrPOJzVtM9C1ew2Nnc
3848xLiApMu3NBk0JqfcS3Bo5Y2b1FRVBvdt+2gFoKZix1MnZdAEZ8xQzL/a0YS5
Hd0wj5+EEKHfOd3A75uPa/WQmA+o0cBFfrzm69QDcSJSwGpzWrD1ScH3AK8nWvoj
v7e9gukK/9yl1b4fQQ00vttwJPSgm9EnfPHLAtgXkRloI27H6/PuLoNvSAMQwuCD
hQRlyGLPBETKkHeodfLoULjhDi1K2gKJTMhtbnUcAA7nEphkMhPWkBpgFdrH+5z4
Lxy+3ek0cqcI7K68EtrffU8jtUj9LFTUC8dERaIBs7NgQ/LfDbDfGh9g6qVj1hZl
k9aaIPTm/xsi8v3u+0qaq7KzIBc9s59JOoA8TlpOaYdVgSQhHHLBaahOuAigH+VI
isbC9vmqsThF2QdDtQt37keuqoda2E6sL7PUvIyVXDRfwX7uMDjlzTxHTymvq2Ck
htBqojBnThmjJQFgZXocHG8CAwEAAQ==
-----END PUBLIC KEY-----`)),
	}
}()

// APKVerifier 封装 APK/APKINDEX 签名验证逻辑，向外只暴露“是否有效”这一层语义。
type APKVerifier struct {
	keys map[string]*rsa.PublicKey
}

// 公钥来源规则固定为“内置默认值 + 用户配置目录”。
func NewAPKVerifier(keysDir string) (*APKVerifier, error) {
	keys := make(map[string]*rsa.PublicKey, len(defaultAPKTrustedKeys))
	for name, key := range defaultAPKTrustedKeys {
		keys[name] = key
	}

	if keysDir != "" {
		loadedKeys, err := loadAPKTrustedKeys(keysDir)
		if err != nil {
			return nil, err
		}
		for name, key := range loadedKeys {
			keys[name] = key
		}
	}

	return &APKVerifier{keys: keys}, nil
}

func (v *APKVerifier) ValidateIndexSignature(cachePath string) error {
	return v.validateArchiveSignature(cachePath)
}

func (v *APKVerifier) ValidatePackageSignature(cachePath string) error {
	return v.validateArchiveSignature(cachePath)
}

// APK/APKINDEX 的签名通常通过单独的 .SIGN.* member 指向其后一个被签名 member。
// 这里统一处理两种文件，避免让协议层关心归档格式细节。
func (v *APKVerifier) validateArchiveSignature(cachePath string) error {
	members, err := readAPKArchiveFile(cachePath)
	if err != nil {
		return err
	}
	signatureEntry, signedMember, err := locateSignedMember(members)
	if err != nil {
		return err
	}

	keyName, hashType, err := parseSignatureEntryName(signatureEntry.Name)
	if err != nil {
		return err
	}
	digest := hashSignedMember(hashType, signedMember.Raw)
	for _, key := range v.lookupKeys(keyName) {
		if err := rsa.VerifyPKCS1v15(key, hashType, digest, signatureEntry.Body); err == nil {
			return nil
		}
	}
	return ErrAPKSignatureInvalid
}

// 没有 .SIGN.* 时返回 ErrAPKUnsigned；
// 找到了签名 member 但结构不完整时返回 ErrAPKSignatureInvalid。
func locateSignedMember(members []apkArchiveMember) (*apkArchiveEntry, *apkArchiveMember, error) {
	for idx := range members {
		for entryIdx := range members[idx].Entries {
			entry := &members[idx].Entries[entryIdx]
			if strings.HasPrefix(entry.Name, ".SIGN.") {
				if idx+1 >= len(members) {
					return nil, nil, ErrAPKSignatureInvalid
				}
				return entry, &members[idx+1], nil
			}
		}
	}
	return nil, nil, ErrAPKUnsigned
}

// .SIGN.RSA.* / .SIGN.RSA256.* 文件名同时编码了摘要算法和 key name。
func parseSignatureEntryName(name string) (string, crypto.Hash, error) {
	switch {
	case strings.HasPrefix(name, ".SIGN.RSA256."):
		return strings.TrimPrefix(name, ".SIGN.RSA256."), crypto.SHA256, nil
	case strings.HasPrefix(name, ".SIGN.RSA."):
		return strings.TrimPrefix(name, ".SIGN.RSA."), crypto.SHA1, nil
	default:
		return "", 0, ErrAPKSignatureInvalid
	}
}

// 被签名对象是后续 gzip member 的原始压缩字节，而不是解压后的 tar payload。
func hashSignedMember(hashType crypto.Hash, raw []byte) []byte {
	switch hashType {
	case crypto.SHA256:
		sum := sha256.Sum256(raw)
		return sum[:]
	default:
		sum := sha1.Sum(raw)
		return sum[:]
	}
}

// 先做名称匹配，再在找不到 signer name 对应 key 时回退尝试所有受信任公钥。
// 这样即便内置 key 的别名和归档中的 signer name 不完全一致，也不会平白验签失败。
func (v *APKVerifier) lookupKeys(name string) []*rsa.PublicKey {
	if key, ok := v.keys[name]; ok {
		return []*rsa.PublicKey{key}
	}

	matched := make([]*rsa.PublicKey, 0, len(v.keys))
	for candidate, key := range v.keys {
		if strings.HasSuffix(candidate, name) || strings.HasSuffix(name, candidate) {
			matched = append(matched, key)
		}
	}
	if len(matched) > 0 {
		return matched
	}

	all := make([]*rsa.PublicKey, 0, len(v.keys))
	for _, key := range v.keys {
		all = append(all, key)
	}
	return all
}

// 用户配置的 keys_dir 会把目录中的每个普通文件都尝试解析为 RSA 公钥。
func loadAPKTrustedKeys(keysDir string) (map[string]*rsa.PublicKey, error) {
	entries, err := os.ReadDir(keysDir)
	if err != nil {
		return nil, err
	}

	keys := make(map[string]*rsa.PublicKey, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(keysDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		key, err := parseRSAPublicKey(data)
		if err != nil {
			return nil, err
		}
		keys[entry.Name()] = key
	}
	return keys, nil
}

// 同时支持 PEM 包裹和裸 DER 数据。
func parseRSAPublicKey(data []byte) (*rsa.PublicKey, error) {
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		key, err := parseRSAPublicKeyDER(block.Bytes)
		if err == nil {
			return key, nil
		}
		data = rest
	}
	return parseRSAPublicKeyDER(data)
}

// 兼容 PKIX、PKCS#1 和带 RSA 公钥的 X.509 证书三种常见载体。
func parseRSAPublicKeyDER(data []byte) (*rsa.PublicKey, error) {
	if publicKey, err := x509.ParsePKIXPublicKey(data); err == nil {
		if rsaKey, ok := publicKey.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
	}
	if rsaKey, err := x509.ParsePKCS1PublicKey(data); err == nil {
		return rsaKey, nil
	}
	if cert, err := x509.ParseCertificate(data); err == nil {
		if rsaKey, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
	}
	return nil, errors.New("unsupported RSA public key")
}

func mustParseRSAPublicKey(data []byte) *rsa.PublicKey {
	key, err := parseRSAPublicKey(data)
	if err != nil {
		panic(fmt.Sprintf("parse built-in APK public key: %v", err))
	}
	return key
}
