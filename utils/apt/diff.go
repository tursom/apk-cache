package apt

import (
	"bufio"
	"iter"
	"path/filepath"
	"regexp"
	"strings"
)

// AptDiffEntry 表示 diff 文件中的一个包条目
type AptDiffEntry struct {
	Package  string
	Filename string
	Size     string
	SHA256   string
}

// ParseDiffReader 从 io.Reader 解析 APT diff 内容，返回条目迭代器
func ParseDiffReader(scanner *bufio.Scanner) iter.Seq[*AptDiffEntry] {
	// ed 命令的正则表达式：匹配如 "51a", "100,200d", "300c" 等格式
	edCommandRegex := regexp.MustCompile(`^(\d+)(,\d+)?[acd]$`)

	return func(yield func(*AptDiffEntry) bool) {

		var currentEntry AptDiffEntry
		inEntry := false
		inChecksumsSha256 := false
		directory := ""
		// 用于延迟输出的 checksum 条目列表
		var pendingChecksums []*AptDiffEntry

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

		for scanner.Scan() {
			line := scanner.Text()

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
				currentEntry = AptDiffEntry{}
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
				currentEntry = AptDiffEntry{}
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
				currentEntry = AptDiffEntry{}
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
					currentEntry.Size = value
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
					pendingChecksums = append(pendingChecksums, &AptDiffEntry{
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

func (entry *AptDiffEntry) IsEmpty() bool {
	return entry.Package == "" && (entry.Filename == "" || entry.SHA256 == "")
}
