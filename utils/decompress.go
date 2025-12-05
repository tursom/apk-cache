package utils

import (
	"compress/bzip2"
	"compress/gzip"
	"io"
	"strings"

	"github.com/ulikunitz/xz"
)

func Decompress(filename string, reader io.Reader) (io.Reader, error) {
	if strings.HasSuffix(filename, ".gz") {
		gzReader, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		reader = gzReader
	} else if strings.HasSuffix(filename, ".xz") {
		xzReader, err := xz.NewReader(reader)
		if err != nil {
			return nil, err
		}
		reader = io.NopCloser(xzReader)
	} else if strings.HasSuffix(filename, ".bz2") {
		bz2Reader := bzip2.NewReader(reader)
		reader = bz2Reader
	} else if strings.HasSuffix(filename, ".lzma") {
		lzmaReader, err := xz.NewReader(reader)
		if err != nil {
			return nil, err
		}
		reader = lzmaReader
	}

	return reader, nil
}
