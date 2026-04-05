package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
)

type apkArchiveMember struct {
	// Raw 是该 member 在原始归档中的压缩字节。
	// APK 签名校验针对的是压缩 member 本身，所以这里必须保留原文。
	Raw []byte
	// Payload 是该 member 解压后的 tar 数据。
	Payload []byte
	// Entries 是 tar member 中展开后的文件项，便于按名字查找签名和元数据文件。
	Entries []apkArchiveEntry
}

type apkArchiveEntry struct {
	Name string
	Body []byte
}

func readAPKArchiveFile(path string) ([]apkArchiveMember, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return readAPKArchiveBytes(data)
}

// APK/APKINDEX 文件通常由多个 gzip member 依次拼接而成。
// 这里显式关闭 multistream，这样每次循环只解析一个 member，
// 便于同时拿到该段压缩原文和解压后的 tar 内容。
func readAPKArchiveBytes(data []byte) ([]apkArchiveMember, error) {
	reader := bytes.NewReader(data)
	members := make([]apkArchiveMember, 0, 4)

	for {
		start, err := reader.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		if start >= int64(len(data)) {
			break
		}

		gzReader, err := gzip.NewReader(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		gzReader.Multistream(false)

		payload, err := io.ReadAll(gzReader)
		closeErr := gzReader.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}

		end, err := reader.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		member := apkArchiveMember{
			Raw:     append([]byte(nil), data[start:end]...),
			Payload: payload,
		}
		// 预先展开条目，后续索引解析和签名定位都不需要再关心 tar 细节。
		member.Entries = parseAPKArchiveEntries(payload)
		members = append(members, member)
	}

	return members, nil
}

// 这里只收集普通文件项；如果解析出错，返回当前已收集结果，让上层统一决定如何处理该归档。
func parseAPKArchiveEntries(payload []byte) []apkArchiveEntry {
	entries := make([]apkArchiveEntry, 0, 4)
	reader := tar.NewReader(bytes.NewReader(payload))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			return entries
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			return entries
		}
		entries = append(entries, apkArchiveEntry{Name: header.Name, Body: body})
	}
}
