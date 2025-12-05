package apt

import (
	"bufio"
	"bytes"
	"io"
	"iter"
	"strconv"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

// ParseReleaseReader 从 io.Reader 解析 APT Release 内容，返回包条目迭代器。
// 如果内容包含 PGP 签名，会自动解码并提取明文。
func ParseReleaseReader(r io.Reader) iter.Seq[*File] {
	// 读取全部数据（Release 文件通常不大）
	data, err := io.ReadAll(r)
	if err != nil {
		// 读取错误，返回空序列
		return func(yield func(*File) bool) {}
	}

	// 尝试解码 PGP 签名消息
	block, _ := clearsign.Decode(data)
	var plaintext []byte
	if block != nil {
		plaintext = block.Plaintext
	} else {
		plaintext = data
	}

	return func(yield func(*File) bool) {
		scanner := bufio.NewScanner(bytes.NewReader(plaintext))
		var inSHA256Block bool

		for scanner.Scan() {
			line := scanner.Text()

			// 检查是否是键值对
			if idx := strings.Index(line, ":"); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				if key == "SHA256" {
					inSHA256Block = true
					continue
				} else {
					// 其他键结束 SHA256 块
					inSHA256Block = false
					continue
				}
			}

			// 如果在 SHA256 块中且行以空格开头（续行）
			if inSHA256Block && len(line) > 0 && line[0] == ' ' {
				// 解析哈希条目：格式为 "hash size filename"
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					hash := fields[0]
					size, err := strconv.ParseInt(fields[1], 10, 64)
					if err != nil {
						size = 0
					}
					filename := fields[2]
					entry := &File{
						Filename: filename,
						Size:     size,
						SHA256:   hash,
					}
					if !yield(entry) {
						return
					}
				}
				continue
			}

			// 空行或其他行结束 SHA256 块
			inSHA256Block = false
		}
	}
}
