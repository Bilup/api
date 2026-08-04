package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3-compatible R2 client using AWS Signature Version 4.
// R2 is fully S3-compatible; path-style addressing is used
// (https://<accountid>.r2.cloudflarestorage.com/<bucket>/<key>).

const (
	s3Service = "s3"
	s3Region  = "auto"
)

func sha256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// r2ObjectURL builds a path-style URL for the given object key.
func r2ObjectURL(key string) string {
	endpoint := strings.TrimRight(r2Endpoint, "/")
	return endpoint + "/" + r2Bucket + "/" + key
}

// r2PutRemote uploads a blob to R2 via S3-compatible PUT with SigV4.
func r2PutRemote(key string, body []byte, contentType, contentEncoding string) bool {
	urlStr := r2ObjectURL(key)
	req, err := http.NewRequest("PUT", urlStr, bytes.NewReader(body))
	if err != nil {
		fmtLog("r2PutRemote: failed to build request for " + key + ": " + err.Error())
		return false
	}

	payloadHash := sha256HexBytes(body)
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", contentType)
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	if err := signSigV4(req, payloadHash, amzDate, dateStamp); err != nil {
		fmtLog("r2PutRemote: signing failed for " + key + ": " + err.Error())
		return false
	}

	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		fmtLog("r2PutRemote: request failed for " + key + ": " + err.Error())
		return false
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		fmtLog("r2PutRemote: R2 returned " + fmt.Sprintf("%d", res.StatusCode) + " for " + key + ": " + string(respBody))
		return false
	}
	return true
}

// r2RemoveRemote deletes a blob from R2 via S3-compatible DELETE with SigV4.
func r2RemoveRemote(key string) bool {
	urlStr := r2ObjectURL(key)
	req, err := http.NewRequest("DELETE", urlStr, nil)
	if err != nil {
		return false
	}

	payloadHash := sha256HexBytes(nil) // hash of empty body
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	if err := signSigV4(req, payloadHash, amzDate, dateStamp); err != nil {
		return false
	}

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	// 204 No Content or 404 Not Found are both acceptable for delete
	return res.StatusCode >= 200 && res.StatusCode < 300 || res.StatusCode == 404
}

// signSigV4 computes the AWS Signature V4 Authorization header and sets it on req.
func signSigV4(req *http.Request, payloadHash, amzDate, dateStamp string) error {
	host := req.URL.Host

	// Canonical URI: path segments URI-encoded per RFC 3986, slashes preserved.
	canonURI := s3CanonicalURI(req.URL.Path)

	// Canonical query string
	canonQS := s3CanonicalQueryString(req.URL.Query())

	// Gather headers to sign: host + all request headers (lowercased, trimmed)
	headers := map[string]string{"host": host}
	for k, vals := range req.Header {
		lk := strings.ToLower(k)
		if len(vals) > 0 {
			headers[lk] = strings.TrimSpace(vals[0])
		}
	}

	// Sort header keys for deterministic canonical form
	var keys []string
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonHeaders strings.Builder
	var signedList []string
	for _, k := range keys {
		canonHeaders.WriteString(k)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(headers[k])
		canonHeaders.WriteByte('\n')
		signedList = append(signedList, k)
	}
	signedHeaders := strings.Join(signedList, ";")

	// Canonical request
	canonRequest := req.Method + "\n" +
		canonURI + "\n" +
		canonQS + "\n" +
		canonHeaders.String() + "\n" +
		signedHeaders + "\n" +
		payloadHash

	// Credential scope: dateStamp/region/service/aws4_request
	credentialScope := dateStamp + "/" + s3Region + "/" + s3Service + "/aws4_request"

	// String to sign
	stringToSign := "AWS4-HMAC-SHA256\n" +
		amzDate + "\n" +
		credentialScope + "\n" +
		sha256Hex(canonRequest)

	// Derive signing key: kSecret -> kDate -> kRegion -> kService -> kSigning
	kDate := hmacSHA256([]byte("AWS4"+r2SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, s3Region)
	kService := hmacSHA256(kRegion, s3Service)
	kSigning := hmacSHA256(kService, "aws4_request")

	// Signature
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	// Authorization header
	authz := "AWS4-HMAC-SHA256 " +
		"Credential=" + r2AccessKeyId + "/" + credentialScope + ", " +
		"SignedHeaders=" + signedHeaders + ", " +
		"Signature=" + signature

	req.Header.Set("Authorization", authz)
	return nil
}

// s3CanonicalURI encodes the URI path for S3 SigV4. Each path segment is
// percent-encoded per RFC 3986; forward slashes are preserved as separators.
func s3CanonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = s3URIEscape(p)
	}
	return strings.Join(parts, "/")
}

// s3CanonicalQueryString builds the canonical query string for S3 SigV4.
func s3CanonicalQueryString(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, s3URIEscape(k)+"="+s3URIEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

// s3URIEscape percent-encodes a string per RFC 3986 for S3 SigV4.
// Unreserved characters (A-Z a-z 0-9 - _ . ~) are not encoded.
func s3URIEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return b.String()
}
