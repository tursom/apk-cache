package data_integrity

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"strings"

	"github.com/tursom/apk-cache/utils/apt"
	bolt "go.etcd.io/bbolt"
)

// APTManager 专用于 APT 包的数据完整性管理器
// 直接解析 apt 索引文件，并将哈希保存在内存中
type APTManager struct {
	cachePath string
	db        *bolt.DB
}

// NewAPTManager 创建 APT 管理器
func NewAPTManager(cachePath string, db *bolt.DB) *APTManager {
	return &APTManager{
		cachePath: cachePath,
		db:        db,
	}
}

// LoadIndexFile 加载一个索引文件（Packages.gz 或 Packages）到内存中
func (a *APTManager) LoadIndexFile(indexFilePath string) error {
	// TODO
	return nil
}

// LoadIndexDirectory 扫描目录下的所有索引文件并加载
func (a *APTManager) LoadIndexDirectory(dir string) error {
	// TODO
	return nil
}

// VerifyFileIntegrity 验证 APT 包文件的完整性
// 如果文件不是 .deb 文件，返回错误
func (a *APTManager) VerifyFileIntegrity(filePath string) (bool, error) {
	// TODO
	return false, nil
}

func (a *APTManager) parseInRelease(filePath string) error {
	// TODO 解析内容并写入 db
	return nil
}

func (a *APTManager) parseRelease(filePath string) error {
	// TODO 校验 GPG 签名然后解析内容
	return nil
}

// parseDiff 解析 APT diff 文件（ed 脚本格式）并将哈希信息写入数据库
// diff 文件格式类似于 ed 脚本，包含行号命令（如 "51a"）和包信息块
// 每个包信息块以空行和 "." 结束
func (a *APTManager) parseDiff(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var reader io.Reader = file
	// 支持 gzip 压缩的 diff 文件
	if strings.HasSuffix(filePath, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer gzReader.Close()
		reader = gzReader
	}

	return a.parseDiffReader(reader)
}

// parseDiffReader 从 io.Reader 解析 diff 内容
func (a *APTManager) parseDiffReader(r io.Reader) error {
	scanner := bufio.NewScanner(r)

	for entry := range apt.ParseDiffReader(scanner) {
		if entry.Filename != "" && entry.SHA256 != "" {
			return a.saveHashToDB(entry.Filename, entry.SHA256)
		}
	}

	return scanner.Err()
}

// aptDiffEntry 表示 diff 文件中的一个包条目
type aptDiffEntry struct {
	Package  string
	Filename string
	Size     string
	SHA256   string
}

// saveHashToDB 将文件哈希保存到数据库
func (a *APTManager) saveHashToDB(filePath string, hash string) error {
	if a.db == nil {
		return nil
	}
	return a.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("apt_file_hashes"))
		if err != nil {
			return err
		}
		return bucket.Put([]byte(filePath), []byte(hash))
	})
}
