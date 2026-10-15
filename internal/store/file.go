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

// File is a directory-backed store for development and tests.
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
	return &File{root: abs}, nil
}

func (f *File) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if clean == "/" || strings.Contains(key, "..") {
		return "", fmt.Errorf("invalid key %q", key)
	}
	return filepath.Join(f.root, filepath.FromSlash(clean)), nil
}

// Put writes atomically; CreateOnly fails with ErrExists if present.
func (f *File) Put(_ context.Context, key string, data []byte, opt PutOptions) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if opt.CreateOnly {
		fh, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Get reads a key.
func (f *File) Get(_ context.Context, key string) ([]byte, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// List returns keys under prefix.
func (f *File) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	err := filepath.WalkDir(f.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".tmp-") {
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
