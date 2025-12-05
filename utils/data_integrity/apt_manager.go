package data_integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"log"

	"github.com/tursom/apk-cache/utils"
	"github.com/tursom/apk-cache/utils/apt"
	"github.com/tursom/apk-cache/utils/i18n"
	bolt "go.etcd.io/bbolt"
)

const apt_bolt_bucket = "apt_file_hashes"

// APTManager 专用于 APT 包的数据完整性管理器
// 直接解析 apt 索引文件，并将哈希保存在内存中
type APTManager struct {
	cachePath    string
	absCachePath string
	db           *bolt.DB
}

type APTFileHash struct {
	FilePath string `json:"file"`
	Size     int64  `json:"size"`
	Hash     string `json:"hash"`
}

// NewAPTManager 创建 APT 管理器
func NewAPTManager(cachePath string, db *bolt.DB) *APTManager {
	absCachePath, err := filepath.Abs(cachePath)
	if err != nil {
		// 如果无法获取绝对路径，使用原始路径并记录警告
		log.Println(i18n.T("GetAbsolutePathFailed", map[string]any{"Path": cachePath, "Error": err}))
		absCachePath = cachePath
	}
	return &APTManager{
		cachePath:    cachePath,
		absCachePath: absCachePath,
		db:           db,
	}
}

// LoadIndexFile 加载一个索引文件（Packages.gz 或 Packages）到内存中
func (a *APTManager) LoadIndexFile(indexFilePath string) error {
	log.Println(i18n.T("LoadingIndexFile", map[string]any{"Path": indexFilePath}))

	// 将 indexFilePath 转换为绝对路径以便比较
	absIndexPath, err := filepath.Abs(indexFilePath)
	if err != nil {
		log.Println(i18n.T("GetAbsolutePathFailed", map[string]any{"Path": indexFilePath, "Error": err}))
		absIndexPath = indexFilePath
	}

	var host, path, filename, suite string
	if utils.IsHashRequest(indexFilePath) {
		_, hashValue, err := utils.ParseHashFromPath(indexFilePath)
		if err != nil {
			log.Println(i18n.T("ParseHashFromPathFailed", map[string]any{"Path": indexFilePath, "Error": err}))
			return nil
		}

		hash, err := a.getHashByValue(hashValue)
		if err != nil {
			log.Println(i18n.T("GetHashByValueFailed", map[string]any{"Hash": hashValue, "Error": err}))
			return nil
		}
		if hash == nil {
			log.Println(i18n.T("NoHashRecordFound", map[string]any{"Hash": hashValue}))
			return nil
		}

		// 使用存储的文件路径解析 host、path、filename
		host, path, filename, err = a.parseFilePath(hash.FilePath)
		if err != nil {
			log.Println(i18n.T("ParseFilePathFailed", map[string]any{"Path": absIndexPath, "Error": err}))
			return err
		}

		suiteIndex := strings.Index(path, "/dists")
		suite = path[:suiteIndex]
	} else {
		host, path, filename, err = a.parseFilePath(absIndexPath)
		if err != nil {
			log.Println(i18n.T("ParseFilePathFailed", map[string]any{"Path": absIndexPath, "Error": err}))
			return err
		}
	}

	log.Println(i18n.T("ParsedIndexFile", map[string]any{"Host": host, "Path": path, "Filename": filename}))
	if filename == "InRelease" {
		log.Println(i18n.T("ParsingInReleaseFile", map[string]any{"Path": absIndexPath}))
		err := a.parseInRelease(absIndexPath, host, path)
		if err != nil {
			log.Println(i18n.T("ParseInReleaseFileFailed", map[string]any{"Path": absIndexPath, "Error": err}))
		} else {
			log.Println(i18n.T("InReleaseFileLoaded", map[string]any{"Path": absIndexPath}))
		}
		return err
	} else if strings.HasPrefix(filename, "Packages") {
		log.Println(i18n.T("ParsingPackagesFile", map[string]any{"Path": absIndexPath}))
		err := a.parsePackages(absIndexPath, filename, host, suite)
		if err != nil {
			log.Println(i18n.T("ParsePackagesFileFailed", map[string]any{"Path": absIndexPath, "Error": err}))
		} else {
			log.Println(i18n.T("PackagesFileLoaded", map[string]any{"Path": absIndexPath}))
		}
		return err
	}
	// TODO
	log.Println(i18n.T("IndexFileTypeNotImplemented", map[string]any{"Filename": filename}))
	return nil
}

func (a *APTManager) parseFilePath(filePath string) (host, path, filename string, err error) {
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return "", "", "", err
	}

	// 确保缓存路径是绝对的（已经在 NewAPTManager 中处理）
	absCachePath := a.absCachePath
	// 检查绝对路径是否以缓存路径开头
	if !strings.HasPrefix(absFilePath, absCachePath) {
		return "", "", "", nil
	}
	// 移除缓存路径部分
	relPath := absFilePath[len(absCachePath):]
	// 期望格式为 /apt/host/path/filename
	if !strings.HasPrefix(relPath, "/apt/") {
		return "", "", "", nil
	}
	// 移除 "/apt/"
	relPath = relPath[5:]
	// 现在 relPath 格式为 "host/path/filename"
	hostIndex := strings.IndexByte(relPath, filepath.Separator)
	if hostIndex == -1 {
		return "", "", "", nil
	}
	filenameIndex := strings.LastIndexByte(relPath, filepath.Separator)
	if filenameIndex == -1 || filenameIndex <= hostIndex {
		return "", "", "", nil
	}
	host = relPath[:hostIndex]
	path = relPath[hostIndex+1 : filenameIndex]
	filename = relPath[filenameIndex+1:]
	return host, path, filename, nil
}

// LoadIndexDirectory 扫描目录下的所有索引文件并加载
func (a *APTManager) LoadIndexDirectory(dir string) error {
	// TODO
	return nil
}

// VerifyFileIntegrity 验证 APT 包文件的完整性
// 如果文件不是 .deb 文件，返回错误
func (a *APTManager) VerifyFileIntegrity(filePath string) (bool, error) {
	log.Println(i18n.T("StartingIntegrityVerification", map[string]any{"File": filePath})) // 添加日志
	// Open the file to compute its hash
	file, err := os.Open(filePath)
	if err != nil {
		log.Println(i18n.T("OpenFileFailed", map[string]any{"File": filePath, "Error": err})) // 添加日志
		return false, err
	}
	defer file.Close()

	return a.VerifyDataIntegrity(filePath, file) // Hash matches
}

// VerifyFileIntegrity 验证 APT 包文件的完整性
// 如果文件不是 .deb 文件，返回错误
func (a *APTManager) VerifyDataIntegrity(filePath string, data io.Reader) (bool, error) {
	log.Println(i18n.T("StartingIntegrityVerification", map[string]any{"File": filePath})) // 添加日志

	// Compute the SHA256 hash of the file
	hasher := sha256.New()
	if _, err := io.Copy(hasher, data); err != nil {
		log.Println(i18n.T("ComputeHashFailed", map[string]any{"File": filePath, "Error": err})) // 添加日志
		return false, err
	}
	computedHash := hasher.Sum(nil)
	computedHashHex := hex.EncodeToString(computedHash)

	// Retrieve the stored hash record using getHashByPath
	record, err := a.getHashByPath(filePath)
	if err != nil {
		log.Println(i18n.T("RetrieveStoredHashFailed", map[string]any{"File": filePath, "Error": err})) // 添加日志
		return false, err
	}
	if record == nil {
		log.Println(i18n.T("NoStoredHash", map[string]any{"File": filePath})) // 添加日志
		return true, nil
	}

	// Compare hash strings (case-insensitive)
	result := strings.EqualFold(computedHashHex, record.Hash)
	if result {
		log.Println(i18n.T("FileIntegrityVerifiedSuccess", map[string]any{"File": filePath})) // 添加日志
	} else {
		log.Println(i18n.T("FileIntegrityVerifiedFailed", map[string]any{"File": filePath})) // 添加日志
	}
	return result, nil
}

func (a *APTManager) parseInRelease(filePath, host, path string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader, err := utils.Decompress(filePath, file)
	if err != nil {
		return err
	}
	return a.saveHashesToDb(apt.ParseReleaseReader(reader), func(pkg *apt.File) string {
		return apt.GetAPTCacheFilePath(a.cachePath, host, filepath.Join(path, pkg.Filename))
	})
}

func (a *APTManager) parsePackages(filePath, filename, host, suite string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader, err := utils.Decompress(filename, file)
	if err != nil {
		return err
	}
	return a.saveHashesToDb(apt.ParsePackageReader(reader), func(pkg *apt.File) string {
		return apt.GetAPTCacheFilePath(a.cachePath, host, filepath.Join(suite, pkg.Filename))
	})
}

// parseDiff 解析 APT diff 文件（ed 脚本格式）并将哈希信息写入数据库
// diff 文件格式类似于 ed 脚本，包含行号命令（如 "51a"）和包信息块
// 每个包信息块以空行和 "." 结束
// TODO
func (a *APTManager) parseDiff(filePath, filename string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader, err := utils.Decompress(filename, file)
	if err != nil {
		return err
	}

	return a.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(apt_bolt_bucket))
		if err != nil {
			return err
		}
		for entry := range apt.ParseDiffReader(reader) {
			if entry.Filename != "" && entry.SHA256 != "" {
				return a.saveHashToDB(bucket, entry.Filename, entry.SHA256, entry.Size)
			}
		}

		return nil
	})

}

// saveHashToDB 将文件哈希保存到数据库（JSON格式）
func (a *APTManager) saveHashToDB(bucket *bolt.Bucket, filePath string, hash string, size int64) error {
	record := APTFileHash{
		FilePath: filePath,
		Size:     size,
		Hash:     hash,
	}
	jsonData, err := json.Marshal(record)
	if err != nil {
		return err
	}

	if err := bucket.Put([]byte(aptHashPathKey(filePath)), jsonData); err != nil {
		return err
	}
	return bucket.Put([]byte(aptHashValueKey(hash)), []byte(filePath))
}

func (a *APTManager) getHashByPath(filePath string) (*APTFileHash, error) {
	var data []byte
	err := a.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(apt_bolt_bucket))
		if bucket == nil {
			return nil // bucket不存在，表示没有哈希
		}
		data = bucket.Get([]byte(aptHashPathKey(filePath)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil // 没有找到记录
	}
	var hashRecord APTFileHash
	if err := json.Unmarshal(data, &hashRecord); err != nil {
		return nil, err
	}
	return &hashRecord, nil
}

func (a *APTManager) getHashByValue(hash string) (*APTFileHash, error) {
	var data []byte
	err := a.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(apt_bolt_bucket))
		if bucket == nil {
			return nil // bucket不存在，表示没有哈希
		}
		data = bucket.Get([]byte(aptHashValueKey(hash)))
		if data != nil {
			data = bucket.Get([]byte(aptHashPathKey(string(data))))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil // 没有找到记录
	}
	var hashRecord APTFileHash
	if err := json.Unmarshal(data, &hashRecord); err != nil {
		return nil, err
	}
	return &hashRecord, nil
}

func (a *APTManager) IsIndexFile(filePath string) bool {
	// 检查是否是 hash 名文件（路径中包含 /by-hash/）
	if utils.IsHashRequest(filePath) {
		_, hash, err := utils.ParseHashFromPath(filePath)
		if err == nil {
			record, err := a.getHashByValue(hash)
			if err != nil {
				log.Println(i18n.T("GetHashByValueFailed", map[string]any{"Hash": hash, "Error": err}))
				return false
			}
			if record != nil {
				// 使用真实文件名判断
				return utils.IsIndexFile(record.FilePath)
			}
		}
	}
	// 非 hash 文件或未找到记录，直接判断
	return utils.IsIndexFile(filePath)
}

func (a *APTManager) saveHashesToDb(it iter.Seq[*apt.File], getPath func(*apt.File) string) error {
	return a.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(apt_bolt_bucket))
		if err != nil {
			return err
		}

		for pkg := range it {
			if err := a.saveHashToDB(bucket, getPath(pkg), pkg.SHA256, pkg.Size); err != nil {
				return err
			}
		}

		return nil
	})
}

func aptHashPathKey(filepath string) string {
	return "path:" + filepath
}

func aptHashValueKey(filepath string) string {
	return "hash:" + filepath
}
