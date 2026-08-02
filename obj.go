package main

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Obj is the OSL "object" type: a JSON map.
type Obj = map[string]any

// result mirrors OSL json.parse Result.
type result struct {
	ok   bool
	data any
	err  string
}

func (r *result) isOk() bool   { return r != nil && r.ok }
func (r *result) isErr() bool  { return r == nil || !r.ok }
func (r *result) unwrap() any  { if r == nil || !r.ok { return nil }; return r.data }
func (r *result) error() string { if r == nil { return "" }; return r.err }

func tryParseJSON(str string, want string) *result {
	dec := json.NewDecoder(strings.NewReader(str))
	v, err := decodeJSONValue(dec, 0)
	if err != nil {
		return &result{err: err.Error()}
	}
	if _, err := dec.Token(); err != nil && err.Error() != "EOF" {
		return &result{err: "multiple JSON values"}
	}
	return validateKind(v, want)
}

func validateKind(v any, want string) *result {
	if want == "object" {
		if _, ok := v.(map[string]any); !ok {
			return &result{err: "Invalid JSON object"}
		}
	}
	if want == "array" {
		if _, ok := v.([]any); !ok {
			return &result{err: "Invalid JSON array"}
		}
	}
	return &result{ok: true, data: v}
}



// decodeJSONValue uses json.Decoder.Token so numbers become float64, matching
// the OSL json package.
func decodeJSONValue(dec *json.Decoder, depth int) (any, error) {
	if depth > 1000 {
		return nil, fmt.Errorf("JSON nesting is too deep")
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch d := tok.(type) {
	case json.Delim:
		switch d {
		case '{':
			out := map[string]any{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key := kt.(string)
				val, err := decodeJSONValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				out[key] = val
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		case '[':
			out := []any{}
			for dec.More() {
				val, err := decodeJSONValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		}
	}
	return tok, nil
}



func jsonMust(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

func jsonString(v any) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}

func isSafePathPart(v string) bool {
	return v != "" && !strings.Contains(v, "/") && !strings.Contains(v, ".") && !strings.Contains(v, "\\")
}

func mapOf(m map[string]any) map[string]any { return m }

func objHas(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}
func objContains(m map[string]any, k string) bool { return objHas(m, k) }
func objGet(m map[string]any, k string) any {
	if m == nil {
		return nil
	}
	return m[k]
}
func objKeys(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
func arrOf(v []any) []any { return v }

// asArray coerces an OSL array value to []any ([]any{} when not an array).
func asArray(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return []any{}
}

func toObj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func toBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	case float64:
		return t != 0
	default:
		return false
	}
}

// typeof mirrors the OSL builtin string reflection.
func typeOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int64, uint64, json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "object"
	}
}
func arrFrom(m map[string]any, k string) []any { return asArray(m[k]) }

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return formatFloat(t)
	case float32:
		return formatFloat(float64(t))
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case []any:
		return jsonString(t)
	case map[string]any:
		return jsonString(t)
	default:
		return fmt.Sprint(v)
	}
}

func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// toFloat converts an OSL value to a number (0 when not numeric).
func toFloat(v any) float64 {
	switch t := v.(type) {
	case nil:
		return 0
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0
		}
		return f
	case bool:
		if t {
			return 1
		}
		return 0
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0
		}
		return f
	}
	return 0
}

func toIntOf(v any) int { return int(toFloat(v)) }

func parseInt(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}
func parseFloat(s string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(s), 64) }
func parseBool(s string) (bool, error)     { return strconv.ParseBool(strings.TrimSpace(s)) }
func splitHostPort(addr string) (string, string, error) { return net.SplitHostPort(addr) }

// dumpVal prints a value for debugging.
func dumpVal(v any) string { return fmt.Sprintf("%v", v) }

// ---- caches ----

func atoiLoose(s string) int { n, _ := parseInt(s); return n }
func errCode(n int) int      { return n }

// cacheItem mimics OSL cache (create(size, ttlSeconds)).
type cacheItem struct {
	value any
	exp   int64
}

type cache struct {
	mu      sync.RWMutex
	m       map[string]cacheItem
	ttl     int64
	maxSize int
}

func cacheCreate(size, ttlSeconds int) *cache {
	return &cache{m: map[string]cacheItem{}, ttl: int64(ttlSeconds), maxSize: size}
}

func (c *cache) set(key string, v any) {
	now := time.Now().Unix()
	exp := int64(0)
	if c.ttl > 0 {
		exp = now + c.ttl
	}
	c.mu.Lock()
	if c.maxSize > 0 && len(c.m) >= c.maxSize {
		var oldest string
		var oldestExp int64 = 1<<62
		for k, it := range c.m {
			if it.exp < oldestExp {
				oldest, oldestExp = k, it.exp
			}
		}
		delete(c.m, oldest)
	}
	c.m[key] = cacheItem{value: v, exp: exp}
	c.mu.Unlock()
}

func (c *cache) get(key string) any {
	c.mu.RLock()
	it, ok := c.m[key]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	if it.exp > 0 && it.exp < time.Now().Unix() {
		c.delete(key)
		return nil
	}
	return it.value
}

func (c *cache) has(key string) bool {
	return c.get(key) != nil
}

func (c *cache) delete(key string) {
	c.mu.Lock()
	delete(c.m, key)
	c.mu.Unlock()
}

var _ = reflect.DeepEqual