package compat

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/BROadmin/broray/internal/platform"
)

// Canonical returns the `jq -cS .` form of v plus "\n": compact, object keys
// sorted at every level, no HTML escaping. 3.x hashes these bytes for
// confirmation tokens and duplicate keys. Both producer and consumer of such
// hashes are Go in 3.5.0, so only determinism matters, not jq byte-identity.
func Canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf) // map keys are sorted by encoding/json
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CanonicalSHA256 is SHA256Hex(Canonical(v)).
func CanonicalSHA256(v any) (string, error) {
	b, err := Canonical(v)
	if err != nil {
		return "", err
	}
	return SHA256Hex(b), nil
}

// Marshal encodes without HTML escaping (3.x JSON is produced by jq).
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteJSON writes v atomically (0600) without HTML escaping.
func WriteJSON(path string, v any) error {
	b, err := Marshal(v)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, b, 0o600)
}

// ReadJSON reads a JSON file into v (numbers keep their text via UseNumber
// when v is a map/any).
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(v)
}

// ReadObject reads a JSON object file; ok=false if missing or not an object.
func ReadObject(path string) (map[string]any, bool) {
	var m map[string]any
	if err := ReadJSON(path, &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}
