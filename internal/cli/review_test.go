package cli

// Regression tests for review findings U4 (import validation and resume),
// U5 (keygen overwriting its private key) and the operator commands
// "record validate" and "record list --effective".

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/transparencykit/transparencykit/internal/record"
)

func workspace(t *testing.T) (run func(stdin string, args ...string) (string, string, int)) {
	t.Helper()
	d := t.TempDir()
	run = func(stdin string, args ...string) (string, string, int) {
		a, o, e := app()
		a.Stdin = strings.NewReader(stdin)
		c := a.Run(append([]string{"--config", filepath.Join(d, "transparencykit.json")}, args...))
		return o.String(), e.String(), c
	}
	if _, e, c := run("", "init", "--dir", d); c != 0 {
		t.Fatal(e)
	}
	return run
}

func body(t *testing.T, typ string) json.RawMessage {
	tp, err := record.Template(typ)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(tp)
	return b
}

func TestKeygenRefusesPubAndOverwrite(t *testing.T) {
	d := t.TempDir()
	a, _, e := app()
	if a.Run([]string{"keygen", "--out", filepath.Join(d, "rotated.pub")}) != ExitUsage || !strings.Contains(e.String(), "ends in .pub") {
		t.Fatalf("--out x.pub accepted: %s", e)
	}
	if _, err := os.Stat(filepath.Join(d, "rotated.pub")); err == nil {
		t.Fatal("a file was written")
	}
	a, _, _ = app()
	if a.Run([]string{"keygen", "--out", filepath.Join(d, "k.key")}) != ExitOK {
		t.Fatal("keygen")
	}
	before, _ := os.ReadFile(filepath.Join(d, "k.key"))
	a, _, e = app()
	if a.Run([]string{"keygen", "--out", filepath.Join(d, "k.key")}) != ExitUsage {
		t.Fatal("keygen overwrote an existing key")
	}
	after, _ := os.ReadFile(filepath.Join(d, "k.key"))
	if string(before) != string(after) || !strings.Contains(string(after), "PRIVATE KEY") {
		t.Fatal("existing private key changed")
	}
}

func TestImportValidatesReferencesBeforeWriting(t *testing.T) {
	run := workspace(t)
	gov := body(t, record.TypeGovRequest)
	bad, _ := json.Marshal([]map[string]any{
		{"type": record.TypeGovRequest, "body": gov},
		{"type": record.TypeGovRequest, "body": gov, "amends": 999},
	})
	if _, e, c := run(string(bad), "record", "import"); c == 0 || !strings.Contains(e, "item 2") || !strings.Contains(e, "nothing appended") {
		t.Fatalf("bad amendment: %d %s", c, e)
	}
	if o, _, _ := run("", "ledger", "verify"); !strings.Contains(o, "0 entries") {
		t.Fatalf("records appended before the failure: %s", o)
	}
	// References to earlier items of the same batch are valid.
	good, _ := json.Marshal([]map[string]any{
		{"type": record.TypeGovRequest, "body": gov},
		{"type": record.TypeGovRequest, "body": gov, "amends": 1},
		{"type": record.TypeRetraction, "body": map[string]any{"seq": 1, "reason": "test"}},
	})
	if o, _, c := run(string(good), "record", "import", "--check"); c != 0 || !strings.Contains(o, "seq 1..3") {
		t.Fatalf("--check: %s", o)
	}
	if o, e, c := run(string(good), "record", "import"); c != 0 || !strings.Contains(o, "imported 3") {
		t.Fatalf("import: %s %s", o, e)
	}
	// Resume: --from skips items already appended.
	more, _ := json.Marshal([]map[string]any{
		{"type": record.TypeGovRequest, "body": gov},
		{"type": record.TypeAbuseNotice, "body": body(t, record.TypeAbuseNotice)},
	})
	if o, e, c := run(string(more), "record", "import", "--from", "2"); c != 0 || !strings.Contains(o, "imported 1 records (seq 4..4)") {
		t.Fatalf("--from: %s %s", o, e)
	}
	eff, _, _ := run("", "record", "list", "--effective")
	if !strings.Contains(eff, "RETRACTED by #3") || strings.Count(eff, "\n") != 3 {
		t.Fatalf("effective view:\n%s", eff)
	}
}

func TestRecordValidateWritesNothing(t *testing.T) {
	run := workspace(t)
	gov := string(body(t, record.TypeGovRequest))
	if o, e, c := run(gov, "record", "validate", record.TypeGovRequest); c != 0 || !strings.Contains(o, "seq 1") {
		t.Fatalf("validate: %s %s", o, e)
	}
	if _, e, c := run(gov, "record", "validate", record.TypeGovRequest, "--amends", "1"); c == 0 || !strings.Contains(e, "does not exist") {
		t.Fatalf("validate amendment: %s", e)
	}
	if o, _, _ := run("", "ledger", "verify"); !strings.Contains(o, "0 entries") {
		t.Fatalf("validate appended: %s", o)
	}
}
