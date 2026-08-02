package main

import (
	"bytes"
	"compress/gzip"
	"io"
)

// zip.osl helpers (gzip / copy).

func gzipLimited(srcPath, dstPath string, maxInBytes, maxOutBytes float64) bool {
	if fsGetSize(srcPath) > maxInBytes {
		return false
	}
	data := fsReadFileBytes(srcPath)
	if float64(len(data)) > maxInBytes {
		return false
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return false
	}
	if err := gz.Close(); err != nil {
		return false
	}
	if float64(buf.Len()) > maxOutBytes {
		return false
	}
	return fsWriteFileBytes(dstPath, buf.Bytes())
}

func copyFile(srcPath, dstPath string) bool {
	if fsGetSize(srcPath) > maxStoredProjectJsonBytes {
		return false
	}
	return fsWriteFileBytes(dstPath, fsReadFileBytes(srcPath))
}

// gunzip decompresses srcPath (gzip) into dstPath. Mirrors OSL zip.gunzip.
func gunzip(srcPath, dstPath string) bool {
	data := fsReadFileBytes(srcPath)
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return false
	}
	out, err := io.ReadAll(gz)
	gz.Close()
	if err != nil {
		return false
	}
	return fsWriteFileBytes(dstPath, out)
}

var _ = bytes.NewReader
var _ = io.EOF