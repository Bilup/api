package main

import (
	"io"
	"net/http"
	"strings"
	"time"
)

// requests.osl — HTTP client helper (subset used by returns.models).

type httpResp struct {
	success bool
	status  int
	body    string
	headers map[string]string
}

func requestsGet(url string, opts map[string]any) httpResp {
	return requestsDo("GET", url, opts)
}

func requestsPost(url string, opts map[string]any) httpResp {
	return requestsDo("POST", url, opts)
}

func requestsDo(method, url string, opts map[string]any) httpResp {
	out := httpResp{}
	if opts == nil {
		opts = map[string]any{}
	}
	var bodyReader io.Reader
	headers := map[string]string{}
	if rawBody, ok := opts["body"]; ok && rawBody != nil {
		var bs string
		switch v := rawBody.(type) {
		case string:
			bs = v
		default:
			bs = jsonString(v)
		}
		bodyReader = strings.NewReader(bs)
		if headers["Content-Type"] == "" {
			headers["Content-Type"] = "application/json"
		}
	}
	if hmap, ok := opts["headers"].(map[string]any); ok {
		for k, v := range hmap {
			headers[k] = toString(v)
		}
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return out
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return out
	}
	defer res.Body.Close()
	out.status = res.StatusCode
	out.success = res.StatusCode >= 200 && res.StatusCode < 300
	out.headers = map[string]string{}
	for k, v := range res.Header {
		if len(v) > 0 {
			out.headers[k] = v[0]
		}
	}
	cap := res.ContentLength
	if cap < 0 {
		cap = 0
	}
	buf := make([]byte, 0, cap)
	tmp := make([]byte, 1024)
	for {
		n, rerr := res.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	out.body = string(buf)
	return out
}