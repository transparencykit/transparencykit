package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File is a directory-backed store for development, tests and `--out`.
//
// Confinement: keys are cleaned, ".." is rejected, and no path component
// below the root may be a symbolic link, so a symlink placed inside the
// directory cannot expose files outside it (e.g. through `serve`).
//
// Permissions: private writes (the ledger) create 0700 directories and 0600
// files; writes with PutOptions.Public create 0755 directories and 0644 files.
type File struct{ root string }

// NewFile opens (and creates) a directory store.
func NewFile(root string) (*File, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// Resolve symlinks in the root itself once; below it none are followed.
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	return &File{root: abs}, nil
}

// ErrSymlink is returned when a key would traverse a symbolic link.
var ErrSymlink = errors.New("path traverses a symbolic link")

func (f *File) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if clean == "/" || strings.Contains(key, "..") || strings.ContainsRune(key, 0) {
		return "", fmt.Errorf("invalid key %q", key)
	}
	return filepath.Join(f.root, filepath.FromSlash(clean)), nil
}

// confined checks that no existing component of p below the root is a
// symlink, and that p itself (if it exists) is a regular file.
func (f *File) confined(p string) error {
	rel, err := filepath.Rel(f.root, p)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("key escapes store root")
	}
	cur := f.root
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: %w", filepath.ToSlash(rel), ErrSymlink)
		}
		if i == len(parts)-1 && !fi.Mode().IsRegular() {
			return fmt.Errorf("%s: not a regular file", filepath.ToSlash(rel))
		}
	}
	return nil
}

func modes(public bool) (dir, file os.FileMode) {
	if public {
		return 0o755, 0o644
	}
	return 0o700, 0o600
}

// Put writes atomically; CreateOnly fails with ErrExists if present.
func (f *File) Put(_ context.Context, key string, data []byte, opt PutOptions) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := f.confined(p); err != nil {
		return err
	}
	dmode, fmode := modes(opt.Public)
	if err := os.MkdirAll(filepath.Dir(p), dmode); err != nil {
		return err
	}
	if err := f.confined(p); err != nil { // re-check after creating parents
		return err
	}
	if opt.CreateOnly {
		fh, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fmode)
		if errors.Is(err, fs.ErrExist) {
			return ErrExists
		}
		if err != nil {
			return err
		}
		if _, err := fh.Write(data); err != nil {
			fh.Close()
			return err
		}
		return fh.Close()
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), fmode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Get reads a key. Symlinks and non-regular files are refused.
func (f *File) Get(_ context.Context, key string) ([]byte, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, err
	}
	if err := f.confined(p); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// List returns keys of regular files under prefix (symlinks are skipped).
func (f *File) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	err := filepath.WalkDir(f.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		rel, _ := filepath.Rel(f.root, p)
		k := filepath.ToSlash(rel)
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
		return nil
	})
	sort.Strings(keys)
	return keys, err
}

func (f *File) String() string { return "file://" + f.root }
