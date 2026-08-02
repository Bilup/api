package main

import (
	"os"
	"path/filepath"
)

// fs helpers mirror the OSL `fs` package.

func fsExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fsReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func fsReadFileBytes(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return []byte{}
	}
	return data
}

func fsWriteFile(path string, data string) bool {
	return fsWriteFileBytes(path, []byte(data))
}

func fsWriteFileBytes(path string, data []byte) bool {
	return os.WriteFile(path, data, 0644) == nil
}

func fsAppendToFile(path, data string) bool {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.WriteString(data)
	return err == nil
}

func fsMkdirAll(path string) bool { return os.MkdirAll(path, 0755) == nil }

func fsRename(oldPath, newPath string) bool { return os.Rename(oldPath, newPath) == nil }

func fsRemove(path string) bool {
	if !fsExists(path) {
		return false
	}
	return os.RemoveAll(path) == nil
}

func fsReadDir(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return []string{}
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func fsGetSize(path string) float64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return float64(info.Size())
}

func fsIsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

var _ = filepath.Separator