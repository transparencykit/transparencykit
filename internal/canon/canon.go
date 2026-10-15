// Package canon produces canonical JSON and content hashes.
//
// Canonical form: object keys sorted, no insignificant whitespace, no HTML
// escaping, integers only. For the value types TransparencyKit allows this matches
// RFC 8785 (JCS).
package canon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Zero is the genesis "prev" hash.
var Zero = "sha256:" + strings.Repeat("0", 64)

// Marshal returns the canonical JSON encoding of v (any JSON-encodable value).
func Marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Canonicalize(raw)
}

// Canonicalize re-encodes arbitrary JSON bytes canonically. It rejects
// non-integer numbers.
func Canonicalize(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("canon: trailing data")
	}
	if err := checkInts(v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func checkInts(v any) error {
	switch t := v.(type) {
	case json.Number:
		if strings.ContainsAny(t.String(), ".eE") {
			return fmt.Errorf("canon: non-integer number %s not allowed", t)
		}
	case map[string]any:
		for _, x := range t {
			if err := checkInts(x); err != nil {
				return err
			}
		}
	case []any:
		for _, x := range t {
			if err := checkInts(x); err != nil {
				return err
			}
		}
	}
	return nil
}

// Hash returns "sha256:<hex>" of b.
func Hash(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// HexSHA256 returns the bare hex SHA-256 of b.
func HexSHA256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
