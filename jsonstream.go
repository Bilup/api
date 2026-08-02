package main

// jsonstream.go — verbatim port of the OSL `json.Stream` state machine
// (osl/packages/json.go), so inspectProjectExtensionUrls behaves identically.

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

type jsonFrame struct {
	kind         json.Delim
	expectingKey bool
}

type jsonStream struct {
	decoder      *json.Decoder
	stack        []jsonFrame
	buffered     map[string]any
	failErr      error
	done         bool
	rootComplete bool
}

func jsonEvent(kind string, value any, depth int) map[string]any {
	return map[string]any{"type": kind, "value": value, "depth": depth}
}

func (s *jsonStream) fail(err error) {
	if s.failErr == nil {
		s.failErr = err
	}
	s.done = true
}

func (s *jsonStream) completeValue() {
	if len(s.stack) == 0 {
		s.rootComplete = true
		return
	}
	parent := &s.stack[len(s.stack)-1]
	if parent.kind == '{' {
		parent.expectingKey = true
	}
}

func (s *jsonStream) readEvent() map[string]any {
	if s == nil || s.decoder == nil || s.done {
		return nil
	}
	if s.rootComplete {
		if _, err := s.decoder.Token(); err != io.EOF {
			if err == nil {
				err = errMultipleValues
			}
			s.fail(err)
		} else {
			s.done = true
		}
		return nil
	}
	if len(s.stack) > 0 {
		frame := &s.stack[len(s.stack)-1]
		if frame.kind == '{' && frame.expectingKey && s.decoder.More() {
			token, err := s.decoder.Token()
			if err != nil {
				s.fail(err)
				return nil
			}
			key, ok := token.(string)
			if !ok {
				s.fail(errInvalidKey)
				return nil
			}
			frame.expectingKey = false
			return jsonEvent("key", key, len(s.stack))
		}
	}
	token, err := s.decoder.Token()
	if err != nil {
		if err == io.EOF {
			s.done = true
		} else {
			s.fail(err)
		}
		return nil
	}
	if delim, ok := token.(json.Delim); ok {
		switch delim {
		case '{', '[':
			depth := len(s.stack)
			if depth >= 10000 {
				s.fail(errTooDeep)
				return nil
			}
			s.stack = append(s.stack, jsonFrame{kind: delim, expectingKey: delim == '{'})
			if delim == '{' {
				return jsonEvent("object-start", nil, depth)
			}
			return jsonEvent("array-start", nil, depth)
		case '}', ']':
			if len(s.stack) == 0 || (delim == '}' && s.stack[len(s.stack)-1].kind != '{') || (delim == ']' && s.stack[len(s.stack)-1].kind != '[') {
				s.fail(errInvalidDelim)
				return nil
			}
			s.stack = s.stack[:len(s.stack)-1]
			s.completeValue()
			if delim == '}' {
				return jsonEvent("object-end", nil, len(s.stack))
			}
			return jsonEvent("array-end", nil, len(s.stack))
		}
	}
	depth := len(s.stack)
	s.completeValue()
	switch token.(type) {
	case string:
		return jsonEvent("string", token, depth)
	case float64:
		return jsonEvent("number", token, depth)
	case bool:
		return jsonEvent("boolean", token, depth)
	case nil:
		return jsonEvent("null", nil, depth)
	default:
		s.fail(errInvalidValue)
		return nil
	}
}

func (s *jsonStream) more() bool {
	if s == nil {
		return false
	}
	if s.buffered == nil {
		s.buffered = s.readEvent()
	}
	return s.buffered != nil
}

func (s *jsonStream) next() map[string]any {
	if !s.more() {
		return jsonEvent("eof", nil, 0)
	}
	event := s.buffered
	s.buffered = nil
	return event
}

func (s *jsonStream) readMap(maxValues any, stringsOnly bool) *result {
	limit := toIntVal(maxValues)
	if limit < 0 {
		return &result{err: "max values cannot be negative"}
	}
	if toString(s.next()["type"]) != "object-start" {
		return &result{err: "next JSON value is not an object"}
	}
	out := map[string]any{}
	count := 0
	for s.more() {
		event := s.next()
		if toString(event["type"]) == "object-end" {
			return &result{ok: true, data: out}
		}
		if toString(event["type"]) != "key" {
			return &result{err: "invalid object"}
		}
		key := toString(event["value"])
		count++
		if count > limit {
			return &result{err: "too many values"}
		}
		value := s.next()
		valueType := toString(value["type"])
		if valueType == "object-start" || valueType == "array-start" || valueType == "eof" {
			return &result{err: "object contains a nested value"}
		}
		if stringsOnly && valueType != "string" {
			return &result{err: "object contains a non-string value"}
		}
		out[key] = value["value"]
	}
	if s.failErr != nil {
		return &result{err: s.failErr.Error()}
	}
	return &result{err: "incomplete object"}
}

func (s *jsonStream) readScalarMap(maxValues any) *result { return s.readMap(maxValues, false) }

func (s *jsonStream) readStringMap(maxValues any) *result { return s.readMap(maxValues, true) }

func (s *jsonStream) skip() bool {
	if !s.more() {
		return false
	}
	eventType := toString(s.next()["type"])
	if eventType != "object-start" && eventType != "array-start" {
		return true
	}
	depth := 1
	for depth > 0 && s.more() {
		eventType = toString(s.next()["type"])
		if eventType == "object-start" || eventType == "array-start" {
			depth++
		} else if eventType == "object-end" || eventType == "array-end" {
			depth--
		}
	}
	return depth == 0 && s.failErr == nil
}

func (s *jsonStream) ok() bool { return s != nil && s.failErr == nil }

func (s *jsonStream) error() string {
	if s == nil || s.failErr == nil {
		return ""
	}
	return s.failErr.Error()
}

func (s *jsonStream) close() {
	s.done = true
	s.buffered = nil
}

func openJSONStream(path string, maxBytes float64) *jsonStream {
	info, err := os.Stat(path)
	if err != nil {
		return &jsonStream{failErr: err, done: true}
	}
	var limit float64
	if maxBytes > 0 {
		limit = maxBytes
		if info.Size() > int64(limit) {
			return &jsonStream{failErr: errTooLarge, done: true}
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &jsonStream{failErr: err, done: true}
	}
	reader := strings.NewReader(string(data))
	if limit > 0 {
		reader = strings.NewReader(string(data))
	}
	return &jsonStream{decoder: json.NewDecoder(reader)}
}

func toIntVal(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}

var (
	errMultipleValues = errors.New("multiple JSON values")
	errInvalidKey     = errors.New("invalid object key")
	errTooDeep        = errors.New("JSON nesting is too deep")
	errInvalidDelim   = errors.New("invalid JSON delimiter")
	errInvalidValue   = errors.New("invalid JSON value")
	errTooLarge       = errors.New("file is too large")
)