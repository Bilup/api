package main

import (
	"archive/zip"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// runtime.go — shared OSL runtime primitives that the domain files rely on
// (array methods, md5, zip decompress, streaming JSON, object sorting).

// timestamp mirrors the OSL builtin: current time as epoch milliseconds.
func timestamp() int64 { return nowMs() }

func strContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func strRemove(list []string, s string) []string {
	out := []string{}
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

func containsStr(items []any, s string) bool {
	for _, v := range items {
		if toString(v) == s {
			return true
		}
	}
	return false
}

func removeStr(items []any, s string) []any {
	out := []any{}
	for _, v := range items {
		if toString(v) != s {
			out = append(out, v)
		}
	}
	return out
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func regexMatch(pattern, s string) bool {
	ok, err := regexp.MatchString(pattern, s)
	return err == nil && ok
}

func regexFindAll(pattern, s string) []any {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return []any{}
	}
	ms := re.FindAllString(s, -1)
	return anyList(ms)
}

func urlEscape(s string) string { return url.QueryEscape(s) }

// sortStrArray sorts a []string ascending.
func sortStrArray(list []string) []string { sort.Strings(list); return list }

// sortObjBy sorts []any of objects by a numeric field in place.
func sortObjBy(items []any, key, dir string) {
	sort.SliceStable(items, func(i, j int) bool {
		a := toFloat(items[i].(map[string]any)[key])
		b := toFloat(items[j].(map[string]any)[key])
		if dir == "descending" {
			return a > b
		}
		return a < b
	})
}

// stringsArray returns a []string from []any.
func stringsArray(items []any) []string {
	out := make([]string, len(items))
	for i, v := range items {
		out[i] = toString(v)
	}
	return out
}

func anyList(items []string) []any {
	out := make([]any, len(items))
	for i, v := range items {
		out[i] = v
	}
	return out
}

func reverseAny(items []any) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func sortFloatSlice(items []float64) { sort.Float64s(items) }

func isEmpty(v any) bool { return v == nil }

// --- zip.decompressLimited ---

func decompressLimited(zipPath, destDir string, maxTotalBytes, maxEntries float64) bool {
	if !fsMkdirAll(destDir) {
		return false
	}
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return false
	}
	defer r.Close()
	total := 0.0
	entries := 0.0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if entries >= maxEntries {
			return false
		}
		if uint64(total)+f.UncompressedSize64 > uint64(maxTotalBytes) {
			return false
		}
		clean := filepath.Clean("/" + f.Name)
		clean = strings.TrimPrefix(clean, "/")
		if clean == "" || strings.Contains(clean, "..") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return false
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return false
		}
		dst := filepath.Join(destDir, filepath.FromSlash(clean))
		if !fsWriteFileBytes(dst, data) {
			return false
		}
		total += float64(len(data))
		entries++
	}
	return true
}

// --- streaming JSON: verbatim port in jsonstream.go ---

var _ = json.Delim('{')