package store_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/transparencykit/transparencykit/internal/s3fake"
	"github.com/transparencykit/transparencykit/internal/store"
)

func newS3(t *testing.T, srv *s3fake.Server, prefix string) store.Store {
	t.Helper()
	s, err := store.NewS3(store.S3Config{Endpoint: srv.URL, Region: "ch-gva-2", Bucket: "bkt", Prefix: prefix,
		PathStyle: true, AccessKey: "EXOtest", SecretKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// contract runs the same behavioural checks against any Store.
func contract(t *testing.T, s store.Store) { contractCond(t, s, true) }

// contractCond optionally skips the conditional-write assertion (for backends
// whose If-None-Match support is being probed).
func contractCond(t *testing.T, s store.Store, conditional bool) {
	ctx := context.Background()
	if _, err := s.Get(ctx, "missing.json"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get missing: %v, want ErrNotFound", err)
	}
	if err := s.Put(ctx, "records/000000000001.json", []byte(`{"a":1}`), store.PutOptions{CreateOnly: true}); err != nil {
		t.Fatal(err)
	}
	if conditional {
		if err := s.Put(ctx, "records/000000000001.json", []byte(`{"a":2}`), store.PutOptions{CreateOnly: true}); !errors.Is(err, store.ErrExists) {
			t.Fatalf("CreateOnly on existing: %v, want ErrExists", err)
		}
	}
	b, err := s.Get(ctx, "records/000000000001.json")
	if err != nil || string(b) != `{"a":1}` {
		t.Fatalf("Get = %q, %v", b, err)
	}
	if err := s.Put(ctx, "head.json", []byte("1"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "head.json", []byte("2"), store.PutOptions{}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	for i := 2; i <= 7; i++ {
		if err := s.Put(ctx, fmt.Sprintf("records/%012d.json", i), []byte("x"), store.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put(ctx, "dir with space/é.txt", []byte("u"), store.PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	if b, err := s.Get(ctx, "dir with space/é.txt"); err != nil || string(b) != "u" {
		t.Fatalf("unicode key: %q %v", b, err)
	}
	keys, err := s.List(ctx, "records/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 7 || keys[0] != "records/000000000001.json" || keys[6] != "records/000000000007.json" {
		t.Fatalf("List = %v", keys)
	}
	all, _ := s.List(ctx, "")
	if len(all) != 9 {
		t.Fatalf("List all = %v", all)
	}
}

func TestFileStoreContract(t *testing.T) {
	s, err := store.NewFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contract(t, s)
	if err := s.Put(context.Background(), "../escape", []byte("x"), store.PutOptions{}); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestS3StoreContractWithPaginationAndPrefix(t *testing.T) {
	srv := s3fake.New()
	defer srv.Close()
	srv.PageSize = 2 // force several ListObjectsV2 pages
	s := newS3(t, srv, "transparencykit/ledger")
	contract(t, s)
	if _, _, ok := srv.Object("bkt", "transparencykit/ledger/records/000000000001.json"); !ok {
		t.Fatal("prefix not applied to object keys")
	}
}

func TestS3PutHeaders(t *testing.T) {
	srv := s3fake.New()
	defer srv.Close()
	fixed := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	s, _ := store.NewS3(store.S3Config{Endpoint: srv.URL, Region: "ch-gva-2", Bucket: "bkt", PathStyle: true,
		AccessKey: "k", SecretKey: "s", Now: func() time.Time { return fixed }})
	err := s.Put(context.Background(), "r.json", []byte("{}"), store.PutOptions{ContentType: "application/json",
		CreateOnly: true, Public: true, Retention: &store.Retention{Mode: "governance", Days: 10}})
	if err != nil {
		t.Fatal(err)
	}
	_, h, _ := srv.Object("bkt", "r.json")
	want := map[string]string{
		"If-None-Match":                       "*",
		"X-Amz-Acl":                           "public-read",
		"X-Amz-Object-Lock-Mode":              "GOVERNANCE",
		"X-Amz-Object-Lock-Retain-Until-Date": "2026-10-24T12:00:00Z",
		"Content-Type":                        "application/json",
	}
	for k, v := range want {
		if h.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, h.Get(k), v)
		}
	}
	if h.Get("Content-Md5") == "" {
		t.Error("Content-MD5 missing (required by S3 for object-lock PUTs)")
	}
	auth := h.Get("Authorization")
	for _, sh := range []string{"content-md5", "if-none-match", "x-amz-object-lock-mode", "x-amz-acl"} {
		if !strings.Contains(auth, sh) {
			t.Errorf("header %s not signed: %s", sh, auth)
		}
	}
}

func TestS3MissingCredentials(t *testing.T) {
	srv := s3fake.New()
	defer srv.Close()
	s, _ := store.NewS3(store.S3Config{Endpoint: srv.URL, Region: "x", Bucket: "b", PathStyle: true})
	if _, err := s.Get(context.Background(), "k"); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("want credentials error, got %v", err)
	}
}

func TestOpenURLs(t *testing.T) {
	t.Setenv("EXOSCALE_API_KEY", "EXOk")
	t.Setenv("EXOSCALE_API_SECRET", "sec")
	cases := []struct {
		url, want string
		bad       bool
	}{
		{url: "sos://ledger/transparencykit?zone=ch-gva-2", want: "s3://ledger/transparencykit @ https://sos-ch-gva-2.exo.io"},
		{url: "sos://ledger?zone=de-fra-1", want: "s3://ledger @ https://sos-de-fra-1.exo.io"},
		{url: "s3://b/p?endpoint=https://s3.example.com&region=eu-1", want: "s3://b/p @ https://s3.example.com"},
		{url: "sos://ledger/x", bad: true},
		{url: "s3://b/p", bad: true},
		{url: "ftp://x", bad: true},
	}
	for _, c := range cases {
		s, err := store.Open(c.url)
		if c.bad {
			if err == nil {
				t.Errorf("%s: expected error", c.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.url, err)
			continue
		}
		if s.String() != c.want {
			t.Errorf("%s: String() = %q, want %q", c.url, s.String(), c.want)
		}
	}
	d := t.TempDir()
	s, err := store.Open("file://" + d)
	if err != nil || !reflect.DeepEqual(s.String(), "file://"+d) {
		t.Fatalf("file url: %v %v", s, err)
	}
}
