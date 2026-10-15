package main_test

// End-to-end tests: build the real binary and drive it like an operator would,
// against local directories and against an S3-compatible endpoint.

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/exoscale/vitrine/internal/cli"
	"github.com/exoscale/vitrine/internal/s3fake"
	"github.com/exoscale/vitrine/internal/store"
)

var bin string

func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "vitrine-e2e")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(d, "vitrine")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-X github.com/exoscale/vitrine/internal/cli.Version=e2e", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(d)
	os.Exit(code)
}

type runner struct {
	t   *testing.T
	dir string
	env []string
}

func (r runner) run(stdin string, args ...string) (string, string, int) {
	r.t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), r.env...)
	cmd.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return o.String(), e.String(), code
}

func (r runner) ok(stdin string, args ...string) string {
	r.t.Helper()
	o, e, c := r.run(stdin, args...)
	if c != 0 {
		r.t.Fatalf("vitrine %v: exit %d\nstdout: %s\nstderr: %s", args, c, o, e)
	}
	return o
}

func (r runner) template(typ string) string { return r.ok("", "record", "template", typ) }

func addFixture(t *testing.T, r runner) {
	for _, typ := range []string{"infrastructure", "measures", "subprocessor_change", "abuse_notice"} {
		r.ok(r.template(typ), "record", "add", typ, "--by", "ops@example.ch")
	}
	// Two gov requests: a Swiss one, a US (third-country) one rejected.
	ch := r.template("gov_request")
	r.ok(ch, "record", "add", "gov_request")
	us := strings.NewReplacer(`"authority_country": "CH"`, `"authority_country": "US"`,
		`"legal_basis": "CH StPO Art. 265"`, `"legal_basis": "US 18 U.S.C. § 2713 (CLOUD Act)"`,
		`"outcome": "disclosed_partial"`, `"outcome": "pending"`).Replace(ch)
	r.ok(us, "record", "add", "gov_request")
	// Later: the US request is rejected (amendment of seq 6).
	r.ok(strings.Replace(us, `"outcome": "pending"`, `"outcome": "rejected"`, 1), "record", "add", "gov_request", "--amends", "6")
}

func TestE2ELocal(t *testing.T) {
	r := runner{t: t, dir: t.TempDir()}
	if o := r.ok("", "version"); !strings.Contains(o, "e2e") {
		t.Fatalf("version: %s", o)
	}
	if _, _, c := r.run("", "bogus"); c != 2 {
		t.Fatalf("unknown command exit %d", c)
	}
	init := r.ok("", "init")
	if !strings.Contains(init, "mc mb --with-lock") {
		t.Fatalf("init output: %s", init)
	}
	if _, _, c := r.run("", "init"); c == 0 {
		t.Fatal("init must not overwrite")
	}
	addFixture(t, r)

	if _, e, c := r.run(`{"received_at":"nope"}`, "record", "add", "gov_request"); c != 2 || !strings.Contains(e, "RFC 3339") {
		t.Fatalf("invalid record: %d %s", c, e)
	}
	list := r.ok("", "record", "list")
	if !strings.Contains(list, "US user_data → pending") || strings.Count(list, "\n") != 8 {
		t.Fatalf("list:\n%s", list)
	}
	if show := r.ok("", "record", "show", "6"); !strings.Contains(show, "CLOUD Act") {
		t.Fatalf("show: %s", show)
	}
	v := r.ok("", "ledger", "verify")
	if !strings.Contains(v, "ledger OK: 7 entries") {
		t.Fatalf("ledger verify: %s", v)
	}
	if o := r.ok("", "publish", "--period", "2026", "--dry-run"); !strings.Contains(o, "dry run") {
		t.Fatal(o)
	}
	pub := r.ok("", "publish", "--period", "2026")
	if !strings.Contains(pub, "reports/2026/v1/") || !strings.Contains(pub, "government requests 2 (third-country 1-2)") {
		t.Fatalf("publish: %s", pub)
	}
	pubDir := filepath.Join(r.dir, "public")
	ver := r.ok("", "verify", "--pubkey", "vitrine.pub", "file://"+pubDir, "--chain")
	if !strings.Contains(ver, "verified 1 publication") {
		t.Fatalf("verify: %s", ver)
	}

	// The published report reflects the amendment (rejected, not pending) and leaks nothing private.
	rep, _ := os.ReadFile(filepath.Join(pubDir, "latest/report.json"))
	var rj map[string]any
	json.Unmarshal(rep, &rj)
	gov := rj["government_requests"].(map[string]any)
	if gov["by_outcome"].(map[string]any)["pending"] != float64(0) {
		t.Fatalf("amendment not applied: %v", gov["by_outcome"])
	}
	filepath.Walk(pubDir, func(p string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "LEG-2026-014") || strings.Contains(string(b), "never published") {
			t.Errorf("%s leaks private data", p)
		}
		return nil
	})

	// Ledger head committed in manifest can be checked by an auditor.
	mb, _ := os.ReadFile(filepath.Join(pubDir, "latest/manifest.json"))
	var m struct {
		Ledger struct{ Hash string } `json:"ledger"`
	}
	json.Unmarshal(mb, &m)
	if o := r.ok("", "ledger", "verify", "--expect-head", m.Ledger.Hash); !strings.Contains(o, "found at seq 7") {
		t.Fatalf("expect-head: %s", o)
	}
	if _, _, c := r.run("", "ledger", "verify", "--expect-head", "sha256:00"); c != 1 {
		t.Fatal("bad expect-head must exit 1")
	}

	// Tamper a published byte -> exit 1.
	idx := filepath.Join(pubDir, "reports/2026/v1/index.html")
	b, _ := os.ReadFile(idx)
	os.WriteFile(idx, append(b, '\n'), 0o644)
	if o, e, c := r.run("", "verify", "--pubkey", "vitrine.pub", "file://"+pubDir, "reports/2026/v1"); c != 1 || !strings.Contains(e, "VERIFICATION FAILED") {
		t.Fatalf("tamper: exit %d\n%s\n%s", c, o, e)
	}
	os.WriteFile(idx, b, 0o644)

	// Tamper the ledger -> ledger verify exit 1, publish refuses.
	rec := filepath.Join(r.dir, "ledger/records/000000000005.json")
	lb, _ := os.ReadFile(rec)
	os.WriteFile(rec, bytes.Replace(lb, []byte(`"CH"`), []byte(`"FR"`), 1), 0o644)
	if _, e, c := r.run("", "ledger", "verify"); c != 1 || !strings.Contains(e, "seq 5") {
		t.Fatalf("ledger tamper: %d %s", c, e)
	}
	if _, e, c := r.run("", "publish", "--period", "2026"); c == 0 || !strings.Contains(e, "refusing to publish") {
		t.Fatalf("publish on broken ledger: %d %s", c, e)
	}
}

func TestE2ES3(t *testing.T) {
	srv := s3fake.New()
	defer srv.Close()
	srv.PageSize = 3
	dir := t.TempDir()
	r := runner{t: t, dir: dir, env: []string{"EXOSCALE_API_KEY=EXOtestkey", "EXOSCALE_API_SECRET=testsecret"}}
	r.ok("", "init")
	// sos:// with an endpoint override exercises the SOS URL path against the fake.
	cfg, _ := os.ReadFile(filepath.Join(dir, "vitrine.json"))
	cfg = bytes.Replace(cfg, []byte(`"file://./ledger"`), []byte(`"sos://acme-ledger/vitrine?zone=ch-gva-2&endpoint=`+srv.URL+`"`), 1)
	cfg = bytes.Replace(cfg, []byte(`"file://./public"`), []byte(`"s3://acme-transparency/?endpoint=`+srv.URL+`&region=ch-gva-2"`), 1)
	cfg = bytes.Replace(cfg, []byte(`"min_count": 3`), []byte(`"min_count": 3, "ledger_retention": {"mode": "GOVERNANCE", "days": 3650}`), 1)
	os.WriteFile(filepath.Join(dir, "vitrine.json"), cfg, 0o644)

	addFixture(t, r)
	if _, h, ok := srv.Object("acme-ledger", "vitrine/records/000000000001.json"); !ok || h.Get("X-Amz-Object-Lock-Mode") != "GOVERNANCE" {
		t.Fatal("ledger object missing or without retention headers")
	}
	r.ok("", "ledger", "verify")
	r.ok("", "publish", "--period", "2026")
	r.ok("", "publish", "--period", "2026") // v2
	if _, h, ok := srv.Object("acme-transparency", "reports/2026/v2/index.html"); !ok || h.Get("X-Amz-Acl") != "public-read" {
		t.Fatal("public object missing or not public-read")
	}
	storeURL := "s3://acme-transparency/?endpoint=" + srv.URL + "&region=ch-gva-2"
	o := r.ok("", "verify", "--pubkey", "vitrine.pub", "--chain", storeURL)
	if !strings.Contains(o, "verified 2 publication") {
		t.Fatalf("verify s3: %s", o)
	}
	// Anonymous HTTP verification through the read-only server.
	ps, err := store.Open(storeURL)
	if err != nil {
		t.Fatal(err)
	}
	web := httptest.NewServer(cli.Handler(ps))
	defer web.Close()
	o = r.ok("", "verify", "--pubkey", "vitrine.pub", "--chain", web.URL)
	if !strings.Contains(o, "verified 2 publication") {
		t.Fatalf("verify http: %s", o)
	}
	// Without --pubkey: works but warns.
	_, e, c := r.run("", "verify", web.URL)
	if c != 0 || !strings.Contains(e, "WARNING") {
		t.Fatalf("no-pubkey verify: %d %s", c, e)
	}
	// Tamper via the S3 backend.
	b, _, _ := srv.Object("acme-transparency", "reports/2026/v1/data-act-art28.json")
	srv.SetObject("acme-transparency", "reports/2026/v1/data-act-art28.json", bytes.Replace(b, []byte("CH"), []byte("US"), 1))
	if _, _, c := r.run("", "verify", "--pubkey", "vitrine.pub", "--chain", web.URL); c != 1 {
		t.Fatalf("tamper over http: exit %d", c)
	}
}

func TestStaticBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if _, err := exec.LookPath("file"); err != nil {
		t.Skip("file(1) not installed")
	}
	out, _ := exec.Command("file", bin).CombinedOutput()
	if !strings.Contains(string(out), "statically linked") {
		t.Fatalf("binary not static: %s", out)
	}
}
