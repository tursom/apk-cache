package apk

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
)

const maxDecompressedMemberSize = 256 << 20

type ArchiveMember struct {
	Raw     []byte
	Payload []byte
	Entries []ArchiveEntry
}

type ArchiveEntry struct {
	Name string
	Body []byte
}

func ReadArchiveFile(path string) ([]ArchiveMember, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ReadArchiveBytes(data)
}

func ReadArchiveBytes(data []byte) ([]ArchiveMember, error) {
	reader := bytes.NewReader(data)
	var members []ArchiveMember
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

		payload, readErr := io.ReadAll(io.LimitReader(gzReader, maxDecompressedMemberSize))
		if readErr == nil && int64(len(payload)) >= maxDecompressedMemberSize {
			readErr = errors.New("decompressed member exceeds limit")
		}
		closeErr := gzReader.Close()
		if readErr == nil {
			readErr = closeErr
		}
		if readErr != nil {
			return nil, readErr
		}

		end, err := reader.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		member := ArchiveMember{
			Raw:     append([]byte(nil), data[start:end]...),
			Payload: payload,
		}
		member.Entries = parseTarEntries(payload)
		members = append(members, member)
	}
	return members, nil
}

func parseTarEntries(payload []byte) []ArchiveEntry {
	reader := tar.NewReader(bytes.NewReader(payload))
	var entries []ArchiveEntry
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
		body, err := io.ReadAll(io.LimitReader(reader, maxDecompressedMemberSize))
		if err != nil || int64(len(body)) >= maxDecompressedMemberSize {
			return entries
		}
		entries = append(entries, ArchiveEntry{Name: header.Name, Body: body})
	}
}
