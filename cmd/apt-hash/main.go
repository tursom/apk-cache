package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/tursom/apk-cache/utils/apt"
	bolt "go.etcd.io/bbolt"
)

const (
	defaultBucket = "apt_file_hashes"
)

func main() {
	dbPath := flag.String("db", "./data/file_hashes.db", "Database file path")
	cachePath := flag.String("cache", "./cache", "Cache directory path")
	host := flag.String("host", "default", "APT host")
	bucket := flag.String("bucket", defaultBucket, "Bucket name")
	list := flag.Bool("list", false, "List all hashes")
	flag.Parse()

	args := flag.Args()
	if !*list && len(args) == 0 {
		log.Fatal("Usage: apt-hash [options] <package-path>")
	}

	// Open database
	db, err := bolt.Open(*dbPath, 0600, &bolt.Options{})
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if *list {
		err = listHashes(db, *bucket)
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	// Single package query
	pkgPath := args[0]
	key := determineKey(pkgPath, *cachePath, *host)

	var hash []byte
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(*bucket))
		if b == nil {
			return fmt.Errorf("bucket %q does not exist", *bucket)
		}
		hash = b.Get([]byte(key))
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	if hash == nil {
		log.Fatalf("No hash found for key: %s", key)
	}
	fmt.Printf("%s\n", hash)
}

func determineKey(pkgPath, cachePath, host string) string {
	// If pkgPath is already an absolute path, use it as is
	if filepath.IsAbs(pkgPath) {
		return pkgPath
	}
	// If pkgPath starts with cachePath, treat as absolute
	if strings.HasPrefix(pkgPath, cachePath) {
		return pkgPath
	}
	// Otherwise, assume it's a relative path within APT repository
	// Use apt.GetAPTCacheFilePath to construct the full path
	return apt.GetAPTCacheFilePath(cachePath, host, pkgPath)
}

func listHashes(db *bolt.DB, bucket string) error {
	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %q does not exist", bucket)
		}
		return b.ForEach(func(k, v []byte) error {
			fmt.Printf("%s -> %s\n", k, v)
			return nil
		})
	})
}
