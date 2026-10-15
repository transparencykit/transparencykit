package verify_test

// Regression tests for review findings S1, S2, S3 (verification and history),
// U1 (interrupted publication), U6 (key rotation) and U7 (draft workflow).

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/transparencykit/transparencykit/internal/manifest"
	"github.com/transparencykit/transparencykit/internal/report"
	"github.com/transparencykit/transparencykit/internal/store"
	"github.com/transparencykit/transparencykit/internal/verify"
)

func (e *env) opts(chain bool) verify.Options {
	return verify.Options{Keys: []ed25519.PublicKey{e.pk}, Chain: chain}
}

// S1: the default check covers the latest/ copies readers open.
func TestVerifyCoversLatestCopies(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.publish(t, "2026")
	f := verify.Store{S: e.pub}
	if _, err := verify.Run(ctx, f, "", e.opts(false)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(e.root, "latest/index.html")
	os.WriteFile(p, []byte("<h1>All good, zero requests</h1>"), 0o644)
	_, err := verify.Run(ctx, f, "", e.opts(false))
	if !verify.IsFailure(err) || !strings.Contains(err.Error(), "latest/") {
		t.Fatalf("tampered latest/index.html not detected: %v", err)
	}
}

// S2: --chain starts at the newest publication of any period, so a
// back-filled period is covered even though latest/ shows a later one.
func TestChainCoversBackfilledPeriods(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.publish(t, "2026")
	r25 := e.publish(t, "2025")
	sp := filepath.Join(e.root, r25.Prefix+"manifest.sig")
	sig, _ := os.ReadFile(sp)
	sig[0] ^= 0xff
	os.WriteFile(sp, sig, 0o644)
	_, err := verify.Run(ctx, verify.Store{S: e.pub}, "", e.opts(true))
	if !verify.IsFailure(err) || !strings.Contains(err.Error(), r25.Prefix) {
		t.Fatalf("corrupt back-filled publication not detected: %v", err)
	}
}

// S3: an old latest.json put back must not fork history.
func TestReplayedPointerDoesNotFork(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.publish(t, "2026")
	old, _ := e.pub.Get(ctx, "latest.json")
	e.publish(t, "2026")
	// A verifier remembers what it saw.
	cp := &verify.Checkpoint{}
	o := e.opts(true)
	o.Checkpoint = cp
	if _, err := verify.Run(ctx, verify.Store{S: e.pub}, "", o); err != nil || len(cp.Entries) != 2 {
		t.Fatalf("checkpoint: %v %+v", err, cp)
	}
	// Restore the old pointer; the publisher links v2 back in instead of
	// building v3 on v1.
	e.pub.Put(ctx, "latest.json", old, store.PutOptions{Public: true})
	r3 := e.publish(t, "2026")
	if r3.Sequence != 3 || len(r3.Adopted) != 1 || r3.Adopted[0] != "reports/2026/v2/" {
		t.Fatalf("v3: %+v", r3)
	}
	got, err := verify.Run(ctx, verify.Store{S: e.pub}, "", o)
	if err != nil || len(got) != 3 {
		t.Fatalf("history after replay: %d %v", len(got), err)
	}

	// A fork the publisher cannot repair is refused.
	e.pub.Put(ctx, "latest.json", old, store.PutOptions{Public: true})
	// Make v3 unreachable from v1 by breaking v2 (v3 links to v2, v2 to v1).
	os.Remove(filepath.Join(e.root, "reports/2026/v2/manifest.sig"))
	if _, err := report.Publish(ctx, report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub, Key: e.key,
		Period: mustPeriod("2026"), Now: time.Now()}); err == nil || !strings.Contains(err.Error(), "fork") {
		t.Fatalf("fork not refused: %v", err)
	}
}

// S3 (verifier side): a forked history fails against a checkpoint.
func TestCheckpointDetectsFork(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.publish(t, "2026")
	e.publish(t, "2026")
	cp := &verify.Checkpoint{}
	o := e.opts(true)
	o.Checkpoint = cp
	if _, err := verify.Run(ctx, verify.Store{S: e.pub}, "", o); err != nil {
		t.Fatal(err)
	}
	// Forge a fork signed with the real key: v3 -> v1, skipping v2.
	m1b, _ := e.pub.Get(ctx, "reports/2026/v1/manifest.json")
	m1, _ := manifest.Parse(m1b)
	fork := *m1
	fork.Version, fork.Sequence = 3, 2
	fork.Previous = &manifest.Link{Path: "reports/2026/v1/manifest.json", SHA256: sha(m1b)}
	fb, _ := fork.Encode()
	e.pub.Put(ctx, "reports/2026/v3/manifest.json", fb, store.PutOptions{Public: true})
	e.pub.Put(ctx, "reports/2026/v3/manifest.sig", manifest.Sign(e.key, fb), store.PutOptions{Public: true})
	for _, fl := range m1.Files {
		b, _ := e.pub.Get(ctx, "reports/2026/v1/"+fl.Path)
		e.pub.Put(ctx, "reports/2026/v3/"+fl.Path, b, store.PutOptions{Public: true})
	}
	ptr := report.Pointer{Label: "2026", Version: 3, Manifest: "reports/2026/v3/manifest.json", ManifestSHA256: sha(fb),
		PeriodEnd: "2026-12-31", ChainHead: &manifest.Link{Path: "reports/2026/v3/manifest.json", SHA256: sha(fb)}}
	pb, _ := json.Marshal(ptr)
	e.pub.Put(ctx, "latest.json", pb, store.PutOptions{Public: true})
	for _, fl := range append(m1.Files, manifest.File{Path: "manifest.json"}, manifest.File{Path: "manifest.sig"}) {
		b, _ := e.pub.Get(ctx, "reports/2026/v3/"+fl.Path)
		e.pub.Put(ctx, "latest/"+fl.Path, b, store.PutOptions{Public: true})
	}
	// Without memory the forged history is self-consistent...
	if _, err := verify.Run(ctx, verify.Store{S: e.pub}, "", e.opts(true)); err != nil {
		t.Fatalf("self-consistent fork: %v", err)
	}
	// ...but a verifier that saw v2 notices it is gone.
	_, err := verify.Run(ctx, verify.Store{S: e.pub}, "", o)
	if !verify.IsFailure(err) || !strings.Contains(err.Error(), "reports/2026/v2/manifest.json") {
		t.Fatalf("fork not detected with checkpoint: %v", err)
	}
}

// Sequence numbers catch a link that skips publications even without a
// checkpoint, when the skipped publication had a lower number.
func TestSequenceGapDetected(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.publish(t, "2026")
	e.publish(t, "2026")
	r3 := e.publish(t, "2026")
	// Re-sign v3 so that it links to v1 directly while keeping sequence 3.
	mb, _ := e.pub.Get(ctx, r3.Prefix+"manifest.json")
	m, _ := manifest.Parse(mb)
	m1b, _ := e.pub.Get(ctx, "reports/2026/v1/manifest.json")
	m.Previous = &manifest.Link{Path: "reports/2026/v1/manifest.json", SHA256: sha(m1b)}
	nb, _ := m.Encode()
	os.WriteFile(filepath.Join(e.root, r3.Prefix+"manifest.json"), nb, 0o644)
	os.WriteFile(filepath.Join(e.root, r3.Prefix+"manifest.sig"), manifest.Sign(e.key, nb), 0o644)
	_, err := verify.Run(ctx, verify.Store{S: e.pub}, r3.Prefix, e.opts(true))
	if !verify.IsFailure(err) || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("sequence gap not detected: %v", err)
	}
}

// U1: an interrupted upload leaves an unsigned folder; the next run uses the
// next version and succeeds.
func TestInterruptedPublicationRecovers(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.pub.Put(ctx, "reports/2026/v1/index.html", []byte("partial"), store.PutOptions{CreateOnly: true, Public: true})
	e.pub.Put(ctx, "reports/2026/v1/report.json", []byte("{}"), store.PutOptions{CreateOnly: true, Public: true})
	r := e.publish(t, "2026")
	if r.Version != 2 || len(r.Abandoned) != 1 || r.Abandoned[0] != "reports/2026/v1/" {
		t.Fatalf("retry: %+v", r)
	}
	if _, err := verify.Run(ctx, verify.Store{S: e.pub}, "", e.opts(true)); err != nil {
		t.Fatal(err)
	}
	items, _ := report.Published(ctx, e.pub)
	if len(items) != 1 || items[0].Version != 2 {
		t.Fatalf("index must list only complete publications: %+v", items)
	}
	// Interrupted after the signature but before latest.json: linked back.
	r3 := e.publish(t, "2026")
	prev, _ := e.pub.Get(ctx, "reports/2026/v2/manifest.json")
	ptr := report.Pointer{Label: "2026", Version: 2, Manifest: "reports/2026/v2/manifest.json", ManifestSHA256: sha(prev),
		PeriodEnd: "2026-12-31", ChainHead: &manifest.Link{Path: "reports/2026/v2/manifest.json", SHA256: sha(prev)}}
	pb, _ := json.Marshal(ptr)
	e.pub.Put(ctx, "latest.json", pb, store.PutOptions{Public: true})
	r4 := e.publish(t, "2026")
	if len(r4.Adopted) != 1 || r4.Adopted[0] != r3.Prefix || r4.Sequence != 3 {
		t.Fatalf("resume after signature: %+v", r4)
	}
}

// U6: after a key rotation the chain verifies with both keys.
func TestChainAcrossKeyRotation(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.publish(t, "2026")
	d := t.TempDir()
	newPub, err := manifest.GenerateKey(filepath.Join(d, "new.key"), filepath.Join(d, "new.pub"))
	if err != nil {
		t.Fatal(err)
	}
	newKey, _ := manifest.LoadPrivate(filepath.Join(d, "new.key"))
	if _, err := report.Publish(ctx, report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub, Key: newKey,
		Period: mustPeriod("2026"), Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	f := verify.Store{S: e.pub}
	_, err = verify.Run(ctx, f, "", verify.Options{Keys: []ed25519.PublicKey{newPub}, Chain: true})
	if !verify.IsFailure(err) || !strings.Contains(err.Error(), "not among the trusted keys") {
		t.Fatalf("old publication with new key only: %v", err)
	}
	got, err := verify.Run(ctx, f, "", verify.Options{Keys: []ed25519.PublicKey{newPub, e.pk}, Chain: true})
	if err != nil || len(got) != 2 {
		t.Fatalf("both keys: %d %v", len(got), err)
	}
	// Both keys in one PEM file.
	a, _ := manifest.EncodePublic(newPub)
	b, _ := manifest.EncodePublic(e.pk)
	ks, err := manifest.ParsePublicAll(append(a, b...))
	if err != nil || len(ks) != 2 {
		t.Fatalf("ParsePublicAll: %d %v", len(ks), err)
	}
}

// U7: draft, review (qualitative answers), publish exactly what was reviewed.
func TestDraftReviewPublish(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	dir := filepath.Join(t.TempDir(), "draft")
	now := time.Date(2027, 2, 10, 9, 0, 0, 0, time.UTC)
	o := report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub, Key: e.key, Period: mustPeriod("2026"),
		Now: now, DraftDir: dir, Qualitative: map[string]string{"training": "Yearly."}}
	if _, err := report.Publish(ctx, o); err != nil {
		t.Fatal(err)
	}
	if keys, _ := e.pub.List(ctx, ""); len(keys) != 0 {
		t.Fatalf("draft wrote to the public store: %v", keys)
	}
	q := filepath.Join(dir, report.QualitativeFile)
	b, _ := os.ReadFile(q)
	if !strings.Contains(string(b), "Yearly.") {
		t.Fatal("qualitative answer missing from draft")
	}
	// Reviewer fills in an answer; a record is appended meanwhile.
	os.WriteFile(q, []byte(strings.Replace(string(b), "own initiative;", "own initiative;Notices only.", 1)), 0o644)
	if _, err := e.led.Append(ctx, "retraction", []byte(`{"seq":3,"reason":"duplicate"}`), "x", 0); err != nil {
		t.Fatal(err)
	}
	pub := report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub, Key: e.key, Now: time.Now(), FromDraft: dir}
	res, err := report.Publish(ctx, pub)
	if err != nil {
		t.Fatal(err)
	}
	if res.Ledger.Seq != 5 || res.Report.Government.Total != 1 {
		t.Fatalf("must publish the reviewed ledger state: %+v", res.Ledger)
	}
	got, _ := e.pub.Get(ctx, res.Prefix+report.QualitativeFile)
	if !strings.Contains(string(got), "Notices only.") {
		t.Fatalf("edited answer not published: %s", got)
	}
	if _, err := verify.Run(ctx, verify.Store{S: e.pub}, "", e.opts(true)); err != nil {
		t.Fatal(err)
	}
	// Edits to anything else are refused, and so is a stale draft.
	dir2 := filepath.Join(t.TempDir(), "d2")
	o.DraftDir = dir2
	if _, err := report.Publish(ctx, o); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir2, "index.html"), []byte("edited"), 0o644)
	pub.FromDraft = dir2
	if _, err := report.Publish(ctx, pub); err == nil || !strings.Contains(err.Error(), "index.html differs") {
		t.Fatalf("edited draft accepted: %v", err)
	}
	e.publish(t, "2025")
	if _, err := report.Publish(ctx, pub); err == nil || !strings.Contains(err.Error(), "changed since the draft") {
		t.Fatalf("stale draft accepted: %v", err)
	}
}

// U3: publishing mid-period shows current state as of today, announced
// changes separately.
func TestAsOfDefaultsToToday(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.led.Append(ctx, "subprocessor_change", []byte(`{"effective":"2026-12-01","change":"add","name":"Later GmbH","purpose":"x","locations":["DE"]}`), "x", 0)
	res, err := report.Publish(ctx, report.PublishOptions{Config: e.cfg, Ledger: e.led, Public: e.pub, Key: e.key,
		Period: mustPeriod("2026"), Now: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	sp := res.Report.Subprocessors
	for _, c := range sp.Current {
		if c.Name == "Later GmbH" {
			t.Fatal("December change listed as current in October")
		}
	}
	if len(sp.Upcoming) != 1 || res.Report.Period.AsOf != "2026-10-15" {
		t.Fatalf("upcoming %+v as_of %q", sp.Upcoming, res.Report.Period.AsOf)
	}
}

func mustPeriod(s string) report.Period {
	p, err := report.ParsePeriod(s)
	if err != nil {
		panic(err)
	}
	return p
}

func sha(b []byte) string {
	m := &manifest.Manifest{}
	m.AddFile("x", b)
	return m.Files[0].SHA256
}
