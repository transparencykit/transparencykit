// Package manifest signs and verifies publication manifests (Ed25519).
package manifest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Schema is the manifest schema id.
const Schema = "vitrine/manifest/v1"

// File is one covered file.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

// Link points at a previous manifest.
type Link struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Period is an inclusive date range.
type Period struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// LedgerRef commits to the private ledger head.
type LedgerRef struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
}

// Manifest describes one publication.
type Manifest struct {
	Schema      string    `json:"schema"`
	Provider    string    `json:"provider"`
	Label       string    `json:"label"`
	Version     int       `json:"version"`
	Period      Period    `json:"period"`
	GeneratedAt string    `json:"generated_at"`
	Ledger      LedgerRef `json:"ledger"`
	Previous    *Link     `json:"previous"`
	KeyID       string    `json:"key_id"`
	Files       []File    `json:"files"`
}

// AddFile records a file's hash and size.
func (m *Manifest) AddFile(path string, data []byte) {
	s := sha256.Sum256(data)
	m.Files = append(m.Files, File{Path: path, SHA256: hex.EncodeToString(s[:]), Size: len(data)})
}

// Encode returns the deterministic, indented manifest bytes (files sorted).
func (m *Manifest) Encode() ([]byte, error) {
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Parse decodes a manifest strictly.
func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m.Schema != Schema {
		return nil, fmt.Errorf("unsupported manifest schema %q", m.Schema)
	}
	return &m, nil
}

// --- keys --------------------------------------------------------------------

// GenerateKey writes a PKCS#8 private key and PKIX public key (PEM).
func GenerateKey(privPath, pubPath string) (ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(privPath); err == nil {
		return nil, fmt.Errorf("%s already exists; refusing to overwrite", privPath)
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	pb, err := EncodePublic(pub)
	if err != nil {
		return nil, err
	}
	return pub, os.WriteFile(pubPath, pb, 0o644)
}

// EncodePublic returns PKIX PEM bytes.
func EncodePublic(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// LoadPrivate reads a PKCS#8 PEM Ed25519 key.
func LoadPrivate(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("%s: no PEM block", path)
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	ek, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an Ed25519 key", path)
	}
	return ek, nil
}

// ParsePublic parses a PKIX PEM Ed25519 public key.
func ParsePublic(b []byte) (ed25519.PublicKey, error) {
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, errors.New("public key: no PEM block")
	}
	k, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key: not Ed25519")
	}
	return ek, nil
}

// KeyID returns "ed25519:<16 hex of SHA-256(PKIX DER)>".
func KeyID(pub ed25519.PublicKey) string {
	der, _ := x509.MarshalPKIXPublicKey(pub)
	s := sha256.Sum256(der)
	return "ed25519:" + hex.EncodeToString(s[:8])
}

// Sign returns base64(Ed25519(data)) + newline.
func Sign(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n")
}

// VerifySig checks a signature file against data.
func VerifySig(pub ed25519.PublicKey, data, sig []byte) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	if !ed25519.Verify(pub, data, raw) {
		return errors.New("signature: invalid (wrong key or modified manifest)")
	}
	return nil
}
