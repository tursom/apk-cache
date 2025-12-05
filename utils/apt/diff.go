package apt

import (
	"bufio"
	"io"
	"iter"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// File 表示 diff 文件中的一个包条目
type File struct {
	Package  string
	Filename string
	Size     int64
	SHA256   string
}

// ed 命令的正则表达式：匹配如 "51a", "100,200d", "300c" 等格式
var edCommandRegex = regexp.MustCompile(`^\d+(,\d+)?[acd]$`)

// ParseDiffReader 从 io.Reader 解析 APT diff 内容，返回条目迭代器
func ParseDiffReader(reader io.Reader) iter.Seq[*File] {
	bufReader := bufio.NewReader(reader)

	return func(yield func(*File) bool) {
		var currentEntry File
		inEntry := false
		inChecksumsSha256 := false
		directory := ""
		// 用于延迟输出的 checksum 条目列表
		var pendingChecksums []*File

		// flushPendingChecksums 输出所有待处理的 checksum 条目（应用当前 directory）
		flushPendingChecksums := func() bool {
			for _, entry := range pendingChecksums {
				if directory != "" && !strings.Contains(entry.Filename, "/") {
					entry.Filename = filepath.Join(directory, entry.Filename)
				}
				if entry.IsEmpty() || !yield(entry) {
					return false
				}
			}
			pendingChecksums = pendingChecksums[:0]
			return true
		}

		for {
			line, err := bufReader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				return
			}
			line = strings.TrimRight(line, "\n")

			// 检查是否是 ed 命令行（如 "51a", "527a"）
			if edCommandRegex.MatchString(line) {
				// 输出待处理的 checksum 条目
				if !flushPendingChecksums() {
					return
				}
				// 保存之前的条目
				if inEntry {
					if currentEntry.IsEmpty() || !yield(&currentEntry) {
						return
					}
				}
				// 重置状态，开始新的块
				currentEntry = File{}
				inEntry = false
				inChecksumsSha256 = false
				directory = ""
				continue
			}

			// 检查是否是条目结束标记 "."
			if line == "." {
				// 输出待处理的 checksum 条目
				if !flushPendingChecksums() {
					return
				}
				if inEntry {
					if currentEntry.IsEmpty() || !yield(&currentEntry) {
						return
					}
				}
				currentEntry = File{}
				inEntry = false
				inChecksumsSha256 = false
				directory = ""
				continue
			}

			// 空行表示当前包条目结束
			if line == "" {
				// 输出待处理的 checksum 条目
				if !flushPendingChecksums() {
					return
				}
				if inEntry {
					if currentEntry.IsEmpty() || !yield(&currentEntry) {
						return
					}
				}
				currentEntry = File{}
				inEntry = false
				inChecksumsSha256 = false
				// 保留 directory，可能后续条目也在同一目录
				continue
			}

			// 解析键值对
			if idx := strings.Index(line, ":"); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				value := strings.TrimSpace(line[idx+1:])

				switch key {
				case "Package":
					currentEntry.Package = value
					inEntry = true
					inChecksumsSha256 = false
				case "Filename":
					currentEntry.Filename = value
					inEntry = true
					inChecksumsSha256 = false
				case "Size":
					var err error
					currentEntry.Size, err = strconv.ParseInt(value, 10, 64)
					if err != nil {
						currentEntry.Size = 0
					}
					inChecksumsSha256 = false
				case "SHA256":
					currentEntry.SHA256 = value
					inChecksumsSha256 = false
				case "Directory":
					directory = value
					inChecksumsSha256 = false
				case "Checksums-Sha256":
					inChecksumsSha256 = true
				default:
					inChecksumsSha256 = false
				}
			} else if inChecksumsSha256 && strings.HasPrefix(line, " ") {
				// 解析 Checksums-Sha256 块中的哈希条目
				// 格式: " <sha256> <size> <filename>"
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					hash := fields[0]
					filename := fields[2]
					// 暂存 checksum 条目，等待获取完整的 directory 后再输出
					pendingChecksums = append(pendingChecksums, &File{
						SHA256:   hash,
						Filename: filename,
					})
				}
			}
		}

		// 输出剩余的 checksum 条目
		flushPendingChecksums()

		// 处理最后一个条目
		if inEntry && !currentEntry.IsEmpty() {
			yield(&currentEntry)
		}
	}
}

func (entry *File) IsEmpty() bool {
	return entry.Package == "" && (entry.Filename == "" || entry.SHA256 == "")
}
