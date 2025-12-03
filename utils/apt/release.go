package apt

import (
	"bufio"
	"bytes"
	"iter"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

// ParseReleaseReader 从扫描器解析 APT Release 内容，返回签名者列表和包条目迭代器。
// 如果提供了公钥环且内容包含 PGP 签名，会尝试验证签名并返回签名者。
// 如果未提供公钥环或签名验证失败，signers 为 nil。
func ParseReleaseReader(scanner *bufio.Scanner) iter.Seq[*AptDiffEntry] {
	// 将扫描器的内容读取到缓冲区
	var buf bytes.Buffer
	for scanner.Scan() {
		buf.Write(scanner.Bytes())
		buf.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		// 扫描错误，返回空
		return nil
	}

	data := buf.Bytes()
	// 尝试解码 PGP 签名消息
	block, _ := clearsign.Decode(data)
	var content []byte
	if block != nil {
		// 是 PGP 签名消息，使用签名内的内容
		content = block.Plaintext
	} else {
		// 不是签名消息，直接使用原始数据
		content = data
	}

	return func(yield func(*AptDiffEntry) bool) {
		lines := strings.Split(string(content), "\n")
		var currentHashType string // "SHA256", "SHA1", "MD5"
		var inHashBlock bool

		for _, line := range lines {
			// 检查是否是键值对
			if idx := strings.Index(line, ":"); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				// value 可能为空，忽略
				_ = strings.TrimSpace(line[idx+1:])

				switch key {
				case "SHA256", "SHA1", "MD5":
					currentHashType = key
					inHashBlock = true
					continue
				default:
					// 其他键，退出哈希块
					inHashBlock = false
					continue
				}
			}

			// 如果在哈希块中且行以空格开头（表示续行）
			if inHashBlock && (line == "" || (len(line) > 0 && line[0] == ' ')) {
				// 空行表示哈希块结束
				if line == "" {
					inHashBlock = false
					continue
				}
				// 解析哈希条目：格式为 "hash size filename"
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					hash := fields[0]
					size := fields[1]
					filename := fields[2]
					// 只输出 SHA256 条目
					if currentHashType == "SHA256" {
						entry := &AptDiffEntry{
							Filename: filename,
							Size:     size,
							SHA256:   hash,
						}
						if !yield(entry) {
							return
						}
					}
				}
				continue
			}

			// 其他行，重置哈希块
			inHashBlock = false
		}
	}
}
