package main

import (
	"crypto/rand"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// utils.osl

func randomString(length int) string {
	if length <= 0 || length > 16*1024*1024 {
		return ""
	}
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	buf := make([]byte, length)
	rand.Read(buf)
	for i := range b {
		b[i] = charset[int(buf[i])%len(charset)]
	}
	return string(b)
}

func generateId() string {
	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	return "p" + ts + randomString(6)
}

func generateCommentId() string { return "c" + randomString(16) }

func normalizeUsername(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

func normalizeTag(raw string) string {
	t := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "#", ""), " ", "-")
	if t == "" || len(t) > 30 {
		return ""
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(t) {
		return ""
	}
	return t
}

func isValidMd5Ext(name string) bool {
	return regexp.MustCompile(`^[0-9a-f]{32}\.[0-9a-zA-Z]{1,5}$`).MatchString(name)
}

func md5PartOf(name string) string {
	parts := strings.Split(name, ".")
	return parts[0]
}

func extPartOf(name string) string {
	parts := strings.Split(name, ".")
	return parts[len(parts)-1]
}

func clampNumber(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func paginate(items []any, offset, limit float64) []any {
	page := []any{}
	count := 0.0
	for i, item := range items {
		idx := float64(i + 1)
		if idx > offset && count < limit {
			page = append(page, item)
			count++
		}
	}
	return page
}

func sleepMs(ms int64) { time.Sleep(time.Duration(ms) * time.Millisecond) }

func fmtLog(s string) { log.Println(s) }