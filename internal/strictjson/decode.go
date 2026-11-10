// Package strictjson decodes one unambiguous, bounded import document.
// Existing on-disk records continue to use their compatible decoder.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var errInvalid = errors.New("invalid or ambiguous JSON")

// SignedInt64 accepts a numeric JSON integer without float64 rounding. It
// rejects strings, booleans, fractions and values outside the signed range.
func SignedInt64(v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	_, err = json.Number(string(b)).Int64()
	return err == nil
}

func Decode(b []byte) (any, error) {
	if len(b) > 2*1024*1024 || !utf8.Valid(b) {
		return nil, errInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, err := value(d, 0)
	if err != nil {
		return nil, errInvalid
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errInvalid
	}
	return v, nil
}

func value(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, errInvalid
	}
	t, err := d.Token()
	if err != nil {
		return nil, errInvalid
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return nil, errInvalid
			}
			key, ok := k.(string)
			if !ok {
				return nil, errInvalid
			}
			if _, exists := m[key]; exists {
				return nil, errInvalid
			}
			v, err := value(d, depth+1)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
		if end, err := d.Token(); err != nil || end != json.Delim('}') {
			return nil, errInvalid
		}
		return m, nil
	case '[':
		a := []any{}
		for d.More() {
			v, err := value(d, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		if end, err := d.Token(); err != nil || end != json.Delim(']') {
			return nil, errInvalid
		}
		return a, nil
	}
	return nil, errInvalid
}
