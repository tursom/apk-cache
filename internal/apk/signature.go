package apk

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

var (
	ErrUnsigned         = errors.New("apk unsigned")
	ErrSignatureInvalid = errors.New("apk signature invalid")
)

type Verifier struct {
	keys map[string]*rsa.PublicKey
}

func NewVerifier(keysDir string) (*Verifier, error) {
	keys := make(map[string]*rsa.PublicKey, len(defaultTrustedKeys))
	for name, key := range defaultTrustedKeys {
		keys[name] = key
	}
	if keysDir != "" {
		extra, err := loadTrustedKeys(keysDir)
		if err != nil {
			return nil, err
		}
		for name, key := range extra {
			keys[name] = key
		}
	}
	return &Verifier{keys: keys}, nil
}

func (v *Verifier) ValidateArchive(path string) error {
	members, err := ReadArchiveFile(path)
	if err != nil {
		return err
	}
	signature, signed, err := locateSignedMember(members)
	if err != nil {
		return err
	}
	keyName, hashType, err := parseSignatureName(signature.Name)
	if err != nil {
		return err
	}
	digest := hashSignedMember(hashType, signed.Raw)
	for _, key := range v.lookupKeys(keyName) {
		if err := rsa.VerifyPKCS1v15(key, hashType, digest, signature.Body); err == nil {
			return nil
		}
	}
	return ErrSignatureInvalid
}

func locateSignedMember(members []ArchiveMember) (*ArchiveEntry, *ArchiveMember, error) {
	for idx := range members {
		for entryIdx := range members[idx].Entries {
			entry := &members[idx].Entries[entryIdx]
			if strings.HasPrefix(entry.Name, ".SIGN.") {
				if idx+1 >= len(members) {
					return nil, nil, ErrSignatureInvalid
				}
				return entry, &members[idx+1], nil
			}
		}
	}
	return nil, nil, ErrUnsigned
}

func parseSignatureName(name string) (string, crypto.Hash, error) {
	switch {
	case strings.HasPrefix(name, ".SIGN.RSA256."):
		return strings.TrimPrefix(name, ".SIGN.RSA256."), crypto.SHA256, nil
	case strings.HasPrefix(name, ".SIGN.RSA."):
		return strings.TrimPrefix(name, ".SIGN.RSA."), crypto.SHA1, nil
	default:
		return "", 0, ErrSignatureInvalid
	}
}

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

func (v *Verifier) lookupKeys(name string) []*rsa.PublicKey {
	if key, ok := v.keys[name]; ok {
		return []*rsa.PublicKey{key}
	}
	var matched []*rsa.PublicKey
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

func loadTrustedKeys(keysDir string) (map[string]*rsa.PublicKey, error) {
	entries, err := os.ReadDir(keysDir)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]*rsa.PublicKey, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(keysDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		key, err := ParseRSAPublicKey(data)
		if err != nil {
			return nil, err
		}
		keys[entry.Name()] = key
	}
	return keys, nil
}

func ParseRSAPublicKey(data []byte) (*rsa.PublicKey, error) {
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		if key, err := parseRSADER(block.Bytes); err == nil {
			return key, nil
		}
		data = rest
	}
	return parseRSADER(data)
}

func parseRSADER(data []byte) (*rsa.PublicKey, error) {
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

func mustParsePublicKey(data []byte) *rsa.PublicKey {
	key, err := ParseRSAPublicKey(data)
	if err != nil {
		panic(fmt.Sprintf("parse built-in APK public key: %v", err))
	}
	return key
}

var defaultTrustedKeys = map[string]*rsa.PublicKey{
	"alpine-devel@lists.alpinelinux.org-4a6a0840.rsa.pub": mustParsePublicKey([]byte(`-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA1yHJxQgsHQREclQu4Ohe
qxTxd1tHcNnvnQTu/UrTky8wWvgXT+jpveroeWWnzmsYlDI93eLI2ORakxb3gA2O
Q0Ry4ws8vhaxLQGC74uQR5+/yYrLuTKydFzuPaS1dK19qJPXB8GMdmFOijnXX4SA
jixuHLe1WW7kZVtjL7nufvpXkWBGjsfrvskdNA/5MfxAeBbqPgaq0QMEfxMAn6/R
L5kNepi/Vr4S39Xvf2DzWkTLEK8pcnjNkt9/aafhWqFVW7m3HCAII6h/qlQNQKSo
GuH34Q8GsFG30izUENV9avY7hSLq7nggsvknlNBZtFUcmGoQrtx3FmyYsIC8/R+B
ywIDAQAB
-----END PUBLIC KEY-----`)),
	"alpine-devel@lists.alpinelinux.org-5261cecb.rsa.pub": mustParsePublicKey([]byte(`-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAwlzMkl7b5PBdfMzGdCT0
cGloRr5xGgVmsdq5EtJvFkFAiN8Ac9MCFy/vAFmS8/7ZaGOXoCDWbYVLTLOO2qtX
yHRl+7fJVh2N6qrDDFPmdgCi8NaE+3rITWXGrrQ1spJ0B6HIzTDNEjRKnD4xyg4j
g01FMcJTU6E+V2JBY45CKN9dWr1JDM/nei/Pf0byBJlMp/mSSfjodykmz4Oe13xB
Ca1WTwgFykKYthoLGYrmo+LKIGpMoeEbY1kuUe04UiDe47l6Oggwnl+8XD1MeRWY
sWgj8sF4dTcSfCMavK4zHRFFQbGp/YFJ/Ww6U9lA3Vq0wyEI6MCMQnoSMFwrbgZw
wwIDAQAB
-----END PUBLIC KEY-----`)),
	"alpine-devel@lists.alpinelinux.org-6165ee59.rsa.pub": mustParsePublicKey([]byte(`-----BEGIN PUBLIC KEY-----
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
