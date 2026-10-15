package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/exoscale/vitrine/internal/store"
)

// TestSOSIntegration runs the store contract against a real Exoscale SOS
// bucket. It is skipped unless these are set:
//
//	VITRINE_SOS_TEST_BUCKET=my-test-bucket VITRINE_SOS_TEST_ZONE=ch-gva-2
//	EXOSCALE_API_KEY=… EXOSCALE_API_SECRET=…
//
// Objects are written under a unique prefix (vitrine-it/<timestamp>/). If the
// bucket has object lock, they cannot be deleted before retention expires, so
// use a dedicated test bucket without lock. It also reports whether SOS
// honours If-None-Match (conditional writes), which SPEC §4.3 relies on for
// multi-writer safety.
func TestSOSIntegration(t *testing.T) {
	bucket, zone := os.Getenv("VITRINE_SOS_TEST_BUCKET"), os.Getenv("VITRINE_SOS_TEST_ZONE")
	if bucket == "" || zone == "" || os.Getenv("EXOSCALE_API_KEY") == "" {
		t.Skip("set VITRINE_SOS_TEST_BUCKET, VITRINE_SOS_TEST_ZONE, EXOSCALE_API_KEY, EXOSCALE_API_SECRET to run")
	}
	prefix := fmt.Sprintf("vitrine-it/%d", time.Now().UnixNano())
	s, err := store.Open(fmt.Sprintf("sos://%s/%s?zone=%s", bucket, prefix, zone))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Put(ctx, "probe", []byte("1"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	err = s.Put(ctx, "probe", []byte("2"), store.PutOptions{CreateOnly: true})
	conditional := errors.Is(err, store.ErrExists)
	if conditional {
		t.Log("SOS honours If-None-Match: concurrent writers are safe")
	} else {
		t.Logf("SOS does NOT honour If-None-Match (err=%v): run a single writer (SPEC §4.3)", err)
	}
	contractCond(t, s, conditional)
}
