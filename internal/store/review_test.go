package store_test

// Regression tests for review findings S4 (symlink escape), S5 (private
// permissions) and U2 (per-store credentials).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/transparencykit/transparencykit/internal/store"
)

func TestFileStoreRefusesSymlinks(t *testing.T) {
	ctx := context.Background()
	outside := t.TempDir()
	secret := filepath.Join(outside, "transparencykit.key")
	os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600)
	os.MkdirAll(filepath.Join(outside, "dir"), 0o755)
	os.WriteFile(filepath.Join(outside, "dir", "x"), []byte("x"), 0o644)

	root := t.TempDir()
	s, _ := store.NewFile(root)
	s.Put(ctx, "ok.txt", []byte("fine"), store.PutOptions{Public: true})
	os.Symlink(secret, filepath.Join(root, "leak.key"))
	os.Symlink(filepath.Join(outside, "dir"), filepath.Join(root, "linkdir"))

	if b, err := s.Get(ctx, "leak.key"); !errors.Is(err, store.ErrSymlink) {
		t.Fatalf("symlinked file served: %q %v", b, err)
	}
	if _, err := s.Get(ctx, "linkdir/x"); !errors.Is(err, store.ErrSymlink) {
		t.Fatalf("symlinked directory traversed: %v", err)
	}
	if err := s.Put(ctx, "linkdir/new", []byte("y"), store.PutOptions{}); !errors.Is(err, store.ErrSymlink) {
		t.Fatalf("write through symlinked directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "dir", "new")); err == nil {
		t.Fatal("file created outside the store root")
	}
	keys, _ := s.List(ctx, "")
	if len(keys) != 1 || keys[0] != "ok.txt" {
		t.Fatalf("List must skip symlinks: %v", keys)
	}
	if b, err := s.Get(ctx, "ok.txt"); err != nil || string(b) != "fine" {
		t.Fatalf("regular file: %q %v", b, err)
	}
}

func TestFileStorePermissions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, _ := store.NewFile(root)
	s.Put(ctx, "records/000000000001.json", []byte("{}"), store.PutOptions{CreateOnly: true})
	s.Put(ctx, "records/000000000002.json", []byte("{}"), store.PutOptions{})
	s.Put(ctx, "reports/2026/v1/index.html", []byte("<p>"), store.PutOptions{CreateOnly: true, Public: true})
	mode := func(p string) os.FileMode {
		fi, err := os.Stat(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		return fi.Mode().Perm()
	}
	for p, want := range map[string]os.FileMode{
		"records": 0o700, "records/000000000001.json": 0o600, "records/000000000002.json": 0o600,
		"reports": 0o755, "reports/2026/v1/index.html": 0o644,
	} {
		if got := mode(p); got&^0o022 != want&^0o022 || got&0o077 > want&0o077 {
			t.Errorf("%s mode %o, want %o", p, got, want)
		}
	}
}

func TestCredentialsByRole(t *testing.T) {
	t.Setenv("EXOSCALE_API_KEY", "EXOdefault")
	t.Setenv("EXOSCALE_API_SECRET", "s-default")
	t.Setenv("TRANSPARENCYKIT_LEDGER_ACCESS_KEY", "EXOledger")
	t.Setenv("TRANSPARENCYKIT_LEDGER_SECRET_KEY", "s-ledger")
	if k, s := store.Credentials(store.RoleLedger); k != "EXOledger" || s != "s-ledger" {
		t.Fatalf("ledger role: %s %s", k, s)
	}
	if k, _ := store.Credentials(store.RolePublic); k != "EXOdefault" {
		t.Fatalf("public role must fall back to default: %s", k)
	}
	t.Setenv("TRANSPARENCYKIT_PUBLIC_ACCESS_KEY", "EXOpublic")
	if k, _ := store.Credentials(store.RolePublic); k != "EXOpublic" {
		t.Fatalf("public role: %s", k)
	}
	if k, _ := store.Credentials(""); k != "EXOdefault" {
		t.Fatalf("no role: %s", k)
	}
}
