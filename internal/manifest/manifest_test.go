package manifest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyRoundTripSignVerify(t *testing.T) {
	d := t.TempDir()
	kp, pp := filepath.Join(d, "k.key"), filepath.Join(d, "k.pub")
	pub, err := GenerateKey(kp, pp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateKey(kp, pp); err == nil {
		t.Fatal("overwrote existing key")
	}
	if st, _ := os.Stat(kp); st.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode %v", st.Mode().Perm())
	}
	priv, err := LoadPrivate(kp)
	if err != nil {
		t.Fatal(err)
	}
	pb, _ := os.ReadFile(pp)
	pub2, err := ParsePublic(pb)
	if err != nil || !pub.Equal(pub2) {
		t.Fatal("public key round-trip")
	}
	m := &Manifest{Schema: Schema, Provider: "P", Label: "2026", Version: 1, KeyID: KeyID(pub)}
	m.AddFile("b.html", []byte("b"))
	m.AddFile("a.json", []byte("a"))
	mb, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mb), `"previous": null`) {
		t.Error("previous must be explicit null for the first publication")
	}
	if strings.Index(string(mb), "a.json") > strings.Index(string(mb), "b.html") {
		t.Error("files not sorted")
	}
	sig := Sign(priv, mb)
	if err := VerifySig(pub, mb, sig); err != nil {
		t.Fatal(err)
	}
	tampered := []byte(strings.Replace(string(mb), `"version": 1`, `"version": 2`, 1))
	if err := VerifySig(pub, tampered, sig); err == nil {
		t.Fatal("tampered manifest verified")
	}
	other, _ := GenerateKey(filepath.Join(d, "o.key"), filepath.Join(d, "o.pub"))
	if err := VerifySig(other, mb, sig); err == nil {
		t.Fatal("wrong key verified")
	}
	if _, err := Parse(mb); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]byte(`{"schema":"other"}`)); err == nil {
		t.Fatal("wrong schema accepted")
	}
	if !strings.HasPrefix(KeyID(pub), "ed25519:") || len(KeyID(pub)) != 8+16 {
		t.Fatalf("key id %s", KeyID(pub))
	}
}

// TestOpenSSLInterop checks the key files are standard PEM (skipped without openssl).
func TestOpenSSLInterop(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not installed")
	}
	d := t.TempDir()
	kp, pp := filepath.Join(d, "k.key"), filepath.Join(d, "k.pub")
	if _, err := GenerateKey(kp, pp); err != nil {
		t.Fatal(err)
	}
	priv, _ := LoadPrivate(kp)
	msg := filepath.Join(d, "m")
	os.WriteFile(msg, []byte("hello"), 0o644)
	sig := Sign(priv, []byte("hello"))
	raw := filepath.Join(d, "sig.bin")
	dec := exec.Command("openssl", "base64", "-d", "-out", raw)
	dec.Stdin = strings.NewReader(string(sig))
	if out, err := dec.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	out, err := exec.Command("openssl", "pkeyutl", "-verify", "-pubin", "-inkey", pp, "-rawin", "-in", msg, "-sigfile", raw).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Success") {
		t.Fatalf("openssl verify: %v %s", err, out)
	}
}
