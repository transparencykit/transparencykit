package verify_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/transparencykit/transparencykit/internal/cli"
	"github.com/transparencykit/transparencykit/internal/config"
	"github.com/transparencykit/transparencykit/internal/ledger"
	"github.com/transparencykit/transparencykit/internal/manifest"
	"github.com/transparencykit/transparencykit/internal/record"
	"github.com/transparencykit/transparencykit/internal/report"
	"github.com/transparencykit/transparencykit/internal/store"
	"github.com/transparencykit/transparencykit/internal/verify"
)

type env struct {
	cfg  *config.Config
	led  *ledger.Ledger
	pub  store.Store
	key  ed25519.PrivateKey
	pk   ed25519.PublicKey
	root string
}

func setup(t *testing.T) *env {
	t.Helper()
	d := t.TempDir()
	cp := filepath.Join(d, "transparencykit.json")
	os.WriteFile(cp, []byte(config.Example), 0o644)
	cfg, err := config.Load(cp)
	if err != nil {
		t.Fatal(err)
	}
	ls, _ := store.Open(cfg.Resolve(cfg.Ledger))
	ps, _ := store.Open(cfg.Resolve(cfg.Public))
	pk, err := manifest.GenerateKey(filepath.Join(d, "transparencykit.key"), filepath.Join(d, "transparencykit.pub"))
	if err != nil {
		t.Fatal(err)
	}
	key, _ := manifest.LoadPrivate(filepath.Join(d, "transparencykit.key"))
	e := &env{cfg: cfg, led: &ledger.Ledger{S: ls}, pub: ps, key: key, pk: pk, root: filepath.Join(d, "public")}
	for _, typ := range []string{record.TypeInfra, record.TypeMeasures, record.TypeGovRequest, record.TypeAbuseNotice, record.TypeSubprocessor} {
		tp, _ := record.Template(typ)
		b, _ := json.Marshal(tp)
		if _, err := e.led.Append(context.Background(), typ, b, "test", 0); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *env) publish(t *testing.T, period string) *report.Result {
	t.Helper()
	p, _ := report.ParsePeriod(period)
	res, err := report.Publish(context.Background(), report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub,
		Key: e.key, Period: p, Now: time.Date(2027, 2, 10, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPublishVerifyChainAndTamper(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	r1 := e.publish(t, "2026")
	if r1.Version != 1 || r1.Prefix != "reports/2026/v1/" {
		t.Fatalf("%+v", r1)
	}
	// Republish same period: v2 linked to v1.
	r2 := e.publish(t, "2026")
	if r2.Version != 2 {
		t.Fatal("expected v2")
	}
	f := verify.Store{S: e.pub}
	o := verify.Options{Keys: []ed25519.PublicKey{e.pk}, Chain: true}
	got, err := verify.Run(ctx, f, "", o)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Manifest.Version != 2 || got[1].Manifest.Version != 1 {
		t.Fatalf("chain = %d", len(got))
	}
	if got[0].Manifest.Previous == nil || got[0].Manifest.Previous.Path != "reports/2026/v1/manifest.json" {
		t.Fatal("v2 must link v1")
	}
	// latest/ is independently verifiable.
	if _, err := verify.Run(ctx, f, "latest", verify.Options{Keys: []ed25519.PublicKey{e.pk}}); err != nil {
		t.Fatalf("latest/: %v", err)
	}
	// Publishing an earlier period does not move latest/, but extends the chain.
	r0 := e.publish(t, "2025")
	lp, _ := e.pub.Get(ctx, "latest.json")
	var ptr report.Pointer
	json.Unmarshal(lp, &ptr)
	if ptr.Label != "2026" || ptr.ChainHead.Path != r0.Prefix+"manifest.json" {
		t.Fatalf("pointer: %+v", ptr)
	}
	all, err := verify.Run(ctx, f, r0.Prefix, o)
	if err != nil || len(all) != 3 {
		t.Fatalf("chain from 2025: %d %v", len(all), err)
	}

	// Wrong key.
	other, _ := manifest.GenerateKey(filepath.Join(t.TempDir(), "o.key"), filepath.Join(t.TempDir(), "o.pub"))
	if _, err := verify.Run(ctx, f, "", verify.Options{Keys: []ed25519.PublicKey{other}}); !verify.IsFailure(err) {
		t.Fatalf("wrong key: %v", err)
	}
	// Tamper one byte of a published file in v1.
	p := filepath.Join(e.root, "reports/2026/v1/report.json")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), `"total": 1`, `"total": 0`, 1)), 0o644)
	_, err = verify.Run(ctx, f, "", o)
	if !verify.IsFailure(err) || !strings.Contains(err.Error(), "reports/2026/v1/report.json") {
		t.Fatalf("tamper not detected: %v", err)
	}
	os.WriteFile(p, b, 0o644)
	// Rewrite v1 manifest + re-sign with the right key: the v2 link catches it.
	mp := filepath.Join(e.root, "reports/2026/v1/manifest.json")
	mb, _ := os.ReadFile(mp)
	mb2 := []byte(strings.Replace(string(mb), `"generated_at": "2027-02-10T09:00:00Z"`, `"generated_at": "2027-02-11T09:00:00Z"`, 1))
	os.WriteFile(mp, mb2, 0o644)
	os.WriteFile(filepath.Join(e.root, "reports/2026/v1/manifest.sig"), manifest.Sign(e.key, mb2), 0o644)
	_, err = verify.Run(ctx, f, "reports/2026/v2", o)
	if !verify.IsFailure(err) || !strings.Contains(err.Error(), "does not match link") {
		t.Fatalf("silent re-signing of an old report not detected: %v", err)
	}
	// ...and the next publish refuses to build on a tampered chain head? (head is 2025, untouched) – ok.
	// Tamper the chain head: publishing must refuse.
	hp := filepath.Join(e.root, r0.Prefix+"manifest.json")
	hb, _ := os.ReadFile(hp)
	os.WriteFile(hp, append(hb, ' '), 0o644)
	pp, _ := report.ParsePeriod("2026-H1")
	if _, err := report.Publish(ctx, report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub, Key: e.key, Period: pp, Now: time.Now()}); err == nil {
		t.Fatal("publish on tampered chain head accepted")
	}
}

func TestVersionsAreImmutable(t *testing.T) {
	e := setup(t)
	e.publish(t, "2026")
	err := e.pub.Put(context.Background(), "reports/2026/v1/index.html", []byte("x"), store.PutOptions{CreateOnly: true})
	if err != store.ErrExists {
		t.Fatalf("versioned files must be create-only: %v", err)
	}
}

func TestPublishRefusesBrokenLedger(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	b, _ := e.led.S.Get(ctx, record.Key(3))
	e.led.S.Put(ctx, record.Key(3), []byte(strings.Replace(string(b), "CH StPO", "XX", 1)), store.PutOptions{})
	p, _ := report.ParsePeriod("2026")
	_, err := report.Publish(ctx, report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub, Key: e.key, Period: p, Now: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "refusing to publish") {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyOverHTTP(t *testing.T) {
	e := setup(t)
	e.publish(t, "2026")
	srv := httptest.NewServer(cli.Handler(e.pub))
	defer srv.Close()
	got, err := verify.Run(context.Background(), verify.HTTP{Base: srv.URL}, "", verify.Options{Keys: []ed25519.PublicKey{e.pk}, Chain: true})
	if err != nil || len(got) != 1 {
		t.Fatalf("http verify: %v", err)
	}
	// The root index and pointers are served.
	if _, err := (verify.HTTP{Base: srv.URL}).Fetch(context.Background(), "/"); err != nil {
		t.Fatal(err)
	}
	if _, err := (verify.HTTP{Base: srv.URL}).Fetch(context.Background(), "nope.json"); err == nil {
		t.Fatal("expected 404")
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	e := setup(t)
	p, _ := report.ParsePeriod("2026")
	res, err := report.Publish(context.Background(), report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub, Key: e.key, Period: p, Now: time.Now(), DryRun: true})
	if err != nil || !res.DryRun {
		t.Fatal(err)
	}
	keys, _ := e.pub.List(context.Background(), "")
	if len(keys) != 0 {
		t.Fatalf("dry run wrote %v", keys)
	}
}
