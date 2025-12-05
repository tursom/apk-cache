package apt

import (
	"bufio"
	"io"
	"iter"
	"strconv"
	"strings"
)

func ParsePackageReader(reader io.Reader) iter.Seq[*File] {
	br := bufio.NewReader(reader)
	return func(yield func(*File) bool) {
		var currentEntry File
		inEntry := false

		for {
			line, err := br.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				return
			}
			line = strings.TrimRight(line, "\n")

			// 空行表示当前包条目结束
			if line == "" {
				if inEntry && !currentEntry.IsEmpty() {
					if !yield(&currentEntry) {
						return
					}
				}
				currentEntry = File{}
				inEntry = false
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
				case "Filename":
					currentEntry.Filename = value
					inEntry = true
				case "Size":
					size, err := strconv.ParseInt(value, 10, 64)
					if err != nil {
						size = 0
					}
					currentEntry.Size = size
					inEntry = true
				case "SHA256":
					currentEntry.SHA256 = value
					inEntry = true
					// 忽略其他字段
				}
			}
			// 忽略不以冒号开头的行（如多行描述）
		}

		// 处理最后一个条目（如果没有空行结束）
		if inEntry && !currentEntry.IsEmpty() {
			yield(&currentEntry)
		}
	}
}
