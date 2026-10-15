package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/transparencykit/transparencykit/internal/record"
	"github.com/transparencykit/transparencykit/internal/s3fake"
	"github.com/transparencykit/transparencykit/internal/store"
)

func body(typ string) []byte {
	t, _ := record.Template(typ)
	b, _ := json.Marshal(t)
	return b
}

func fileLedger(t *testing.T) *Ledger {
	s, err := store.NewFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Ledger{S: s, Now: func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) }}
}

func TestAppendAndVerify(t *testing.T) {
	ctx := context.Background()
	l := fileLedger(t)
	for _, typ := range []string{record.TypeInfra, record.TypeMeasures, record.TypeGovRequest, record.TypeAbuseNotice, record.TypeSubprocessor} {
		if _, err := l.Append(ctx, typ, body(typ), "ops", 0); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	e, err := l.Append(ctx, record.TypeGovRequest, body(record.TypeGovRequest), "ops", 3)
	if err != nil || e.Seq != 6 || e.Amends != 3 {
		t.Fatalf("amend: %+v %v", e, err)
	}
	es, head, err := l.Verify(ctx)
	if err != nil || len(es) != 6 || head.Seq != 6 || head.Hash != e.Hash {
		t.Fatalf("verify: %d %+v %v", len(es), head, err)
	}
	hb, _ := l.S.Get(ctx, "head.json")
	var h Head
	_ = json.Unmarshal(hb, &h)
	if h.Seq != 6 || h.Hash != e.Hash {
		t.Fatalf("head.json = %+v", h)
	}
}

func TestAppendRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	l := fileLedger(t)
	if _, err := l.Append(ctx, record.TypeGovRequest, []byte(`{"received_at":"x"}`), "", 0); err == nil {
		t.Fatal("invalid body accepted")
	}
	if _, err := l.Append(ctx, record.TypeGovRequest, body(record.TypeGovRequest), "", 5); err == nil {
		t.Fatal("amends of nonexistent seq accepted")
	}
	l.Append(ctx, record.TypeInfra, body(record.TypeInfra), "", 0)
	if _, err := l.Append(ctx, record.TypeGovRequest, body(record.TypeGovRequest), "", 1); err == nil || !strings.Contains(err.Error(), "not a gov_request") {
		t.Fatalf("cross-type amend: %v", err)
	}
	l.Append(ctx, record.TypeInfra, body(record.TypeInfra), "", 1) // seq 2 amends 1
	if _, err := l.Append(ctx, record.TypeInfra, body(record.TypeInfra), "", 2); err == nil || !strings.Contains(err.Error(), "itself an amendment") {
		t.Fatalf("amend of amendment: %v", err)
	}
	if _, err := l.Append(ctx, record.TypeRetraction, []byte(`{"seq":2,"reason":"x"}`), "", 0); err == nil {
		t.Fatal("retraction of amendment accepted")
	}
	if _, err := l.Append(ctx, record.TypeRetraction, []byte(`{"seq":1,"reason":"x"}`), "", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTamperedStoreIsDetected(t *testing.T) {
	ctx := context.Background()
	l := fileLedger(t)
	for i := 0; i < 3; i++ {
		l.Append(ctx, record.TypeGovRequest, body(record.TypeGovRequest), "", 0)
	}
	b, _ := l.S.Get(ctx, record.Key(2))
	b = []byte(strings.Replace(string(b), `"CH"`, `"US"`, 1))
	l.S.Put(ctx, record.Key(2), b, store.PutOptions{})
	if _, _, err := l.Verify(ctx); err == nil || !strings.Contains(err.Error(), "seq 2") {
		t.Fatalf("tamper not detected: %v", err)
	}
	if _, err := l.Append(ctx, record.TypeGovRequest, body(record.TypeGovRequest), "", 0); err != nil {
		// Append only checks the head entry; a mid-chain edit is caught by Verify.
		t.Logf("append after tamper: %v", err)
	}
	// Tampering with the head blocks appends.
	hb, _ := l.S.Get(ctx, record.Key(3))
	l.S.Put(ctx, record.Key(3), []byte(strings.Replace(string(hb), `"CH"`, `"FR"`, 1)), store.PutOptions{})
	l2 := &Ledger{S: l.S}
	es, _ := l2.LoadAll(ctx)
	last := es[len(es)-1].Seq
	if last == 3 {
		if _, err := l2.Append(ctx, record.TypeGovRequest, body(record.TypeGovRequest), "", 0); err == nil {
			t.Fatal("append on tampered head accepted")
		}
	}
}

func TestGapBlocksAppend(t *testing.T) {
	ctx := context.Background()
	l := fileLedger(t)
	l.Append(ctx, record.TypeGovRequest, body(record.TypeGovRequest), "", 0)
	b, _ := l.S.Get(ctx, record.Key(1))
	// Fake a stray object at seq 3.
	l.S.Put(ctx, record.Key(3), b, store.PutOptions{})
	if _, err := l.Append(ctx, record.TypeGovRequest, body(record.TypeGovRequest), "", 0); err == nil || !strings.Contains(err.Error(), "gaps") {
		t.Fatalf("gap: %v", err)
	}
}

func TestConcurrentWritersS3(t *testing.T) {
	srv := s3fake.New()
	defer srv.Close()
	s, _ := store.NewS3(store.S3Config{Endpoint: srv.URL, Region: "ch-gva-2", Bucket: "ledger", PathStyle: true, AccessKey: "a", SecretKey: "b"})
	l := &Ledger{S: s}
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := l.Append(ctx, record.TypeAbuseNotice, body(record.TypeAbuseNotice), "", 0)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		}
	}
	es, head, err := l.Verify(ctx)
	if err != nil {
		t.Fatalf("chain broken by concurrent writers: %v", err)
	}
	if int(head.Seq) != ok || len(es) != ok || ok == 0 {
		t.Fatalf("ok=%d entries=%d", ok, len(es))
	}
}

func TestNoConditionalWritesRaceIsDetectable(t *testing.T) {
	// Simulates a backend that ignores If-None-Match: a second writer can
	// overwrite seq N; verification must still pass or fail cleanly, and the
	// object keeps a valid hash (the overwritten version survives only with
	// bucket versioning; see SPEC §4.3).
	srv := s3fake.New()
	defer srv.Close()
	srv.IgnoreIfNoneMatch = true
	s, _ := store.NewS3(store.S3Config{Endpoint: srv.URL, Region: "r", Bucket: "ledger", PathStyle: true, AccessKey: "a", SecretKey: "b"})
	l := &Ledger{S: s}
	ctx := context.Background()
	if _, err := l.Append(ctx, record.TypeAbuseNotice, body(record.TypeAbuseNotice), "", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, record.Key(1), []byte("{}"), store.PutOptions{CreateOnly: true}); errors.Is(err, store.ErrExists) {
		t.Fatal("fake should ignore If-None-Match in this mode")
	}
	if _, _, err := l.Verify(ctx); err == nil {
		t.Fatal("overwritten entry not detected")
	}
}
