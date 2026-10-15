package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExampleAndDefaults(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "transparencykit.json")
	os.WriteFile(p, []byte(Example), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.MinCount != 3 || len(c.HomeJurisdictions) != 3 {
		t.Fatalf("defaults: %+v", c)
	}
	if got := c.Resolve("file://./ledger"); got != "file://"+filepath.Join(d, "ledger") {
		t.Fatalf("resolve file url: %s", got)
	}
	if got := c.Resolve("transparencykit.key"); got != filepath.Join(d, "transparencykit.key") {
		t.Fatalf("resolve path: %s", got)
	}
	if got := c.Resolve("sos://b/?zone=ch-gva-2"); got != "sos://b/?zone=ch-gva-2" {
		t.Fatal("remote url changed")
	}
	h := c.Home()
	if !h["CH"] || !h["DE"] || !h["NO"] || h["US"] || h["GB"] {
		t.Fatalf("home set wrong: %v", h.Sorted())
	}
}

func TestInvalidConfig(t *testing.T) {
	d := t.TempDir()
	for name, body := range map[string]string{
		"unknown field": `{"provider":{"name":"x"},"ledger":"file://l","nope":1}`,
		"no name":       `{"provider":{},"ledger":"file://l"}`,
		"bad home":      `{"provider":{"name":"x"},"ledger":"file://l","home_jurisdictions":["UK"]}`,
		"bad retention": `{"provider":{"name":"x"},"ledger":"file://l","ledger_retention":{"mode":"soft","days":1}}`,
	} {
		p := filepath.Join(d, strings.ReplaceAll(name, " ", "_")+".json")
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := Load(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
