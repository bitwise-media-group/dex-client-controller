// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dexconnector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// FieldValue is one secret value destined for a dotted config path.
type FieldValue struct {
	Path  string
	Value string
}

// BuildConfig returns the connector config dex receives: raw (a JSON object,
// or empty for {}) with every value set at its dotted path, in canonical
// form. Intermediate objects are created as needed; a path through a
// non-object value is an error rather than a silent overwrite.
func BuildConfig(raw []byte, values []FieldValue) ([]byte, error) {
	cfg, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	for _, v := range values {
		if err := setPath(cfg, v.Path, v.Value); err != nil {
			return nil, err
		}
	}
	return encode(cfg)
}

// Normalize returns the canonical form of a JSON config, so configs that
// differ only in key order or whitespace compare equal.
func Normalize(raw []byte) ([]byte, error) {
	v, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return encode(v)
}

func decodeObject(raw []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	v, err := decode(raw)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("config must be a JSON object")
	}
	return m, nil
}

// decode keeps numbers as json.Number: a float64 round trip would corrupt
// large integers (a GitHub org ID, an LDAP timeout in nanoseconds).
func decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("config is not valid JSON: %w", err)
	}
	if dec.More() {
		return nil, errors.New("config is not valid JSON: trailing data")
	}
	return v, nil
}

// encode marshals with sorted map keys (encoding/json's map order) and no
// HTML escaping, without the encoder's trailing newline.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func setPath(cfg map[string]any, path, value string) error {
	keys := strings.Split(path, ".")
	m := cfg
	for i, k := range keys[:len(keys)-1] {
		next, ok := m[k]
		if !ok || next == nil {
			child := map[string]any{}
			m[k] = child
			m = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("configField %q: %q is not an object", path, strings.Join(keys[:i+1], "."))
		}
		m = child
	}
	m[keys[len(keys)-1]] = value
	return nil
}
