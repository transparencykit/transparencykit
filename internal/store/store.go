// Package store abstracts the object storage holding ledger and public files.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ErrNotFound is returned when a key does not exist.
var ErrNotFound = errors.New("not found")

// ErrExists is returned by a create-only Put when the key already exists.
var ErrExists = errors.New("already exists")

// PutOptions controls a write.
type PutOptions struct {
	ContentType string
	// CreateOnly sends If-None-Match: * (S3) / O_EXCL (file).
	CreateOnly bool
	// Public sets a public-read ACL on S3 stores.
	Public bool
	// Retention, when set, adds S3 object-lock retention headers.
	Retention *Retention
}

// Retention is an S3 object-lock retention.
type Retention struct {
	Mode string `json:"mode"` // GOVERNANCE or COMPLIANCE
	Days int    `json:"days"`
}

// Store is a minimal object store.
type Store interface {
	Put(ctx context.Context, key string, data []byte, opt PutOptions) error
	Get(ctx context.Context, key string) ([]byte, error)
	// List returns all keys under prefix, sorted ascending.
	List(ctx context.Context, prefix string) ([]string, error)
	// String returns a display form of the store location.
	String() string
}

// Open parses a store URL: file://path, sos://bucket/prefix?zone=…,
// s3://bucket/prefix?endpoint=…&region=….
func Open(raw string) (Store, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("store url %q: %w", raw, err)
	}
	switch u.Scheme {
	case "file":
		p := u.Host + u.Path
		if u.Host == "." || u.Host == ".." {
			p = u.Host + u.Path
		}
		if p == "" {
			return nil, fmt.Errorf("store url %q: empty path", raw)
		}
		return NewFile(p)
	case "sos", "s3":
		bucket := u.Host
		if bucket == "" {
			return nil, fmt.Errorf("store url %q: missing bucket", raw)
		}
		prefix := strings.Trim(u.Path, "/")
		q := u.Query()
		cfg := S3Config{Bucket: bucket, Prefix: prefix, PathStyle: true}
		if u.Scheme == "sos" {
			zone := q.Get("zone")
			if zone == "" {
				return nil, fmt.Errorf("store url %q: sos:// needs ?zone=… (e.g. ch-gva-2)", raw)
			}
			cfg.Endpoint = "https://sos-" + zone + ".exo.io"
			cfg.Region = zone
		} else {
			cfg.Endpoint = q.Get("endpoint")
			cfg.Region = q.Get("region")
			if cfg.Endpoint == "" || cfg.Region == "" {
				return nil, fmt.Errorf("store url %q: s3:// needs ?endpoint=…&region=…", raw)
			}
			if q.Get("path_style") == "false" {
				cfg.PathStyle = false
			}
		}
		if ep := q.Get("endpoint"); ep != "" {
			cfg.Endpoint = ep // explicit override (tests, private endpoints)
		}
		cfg.AccessKey, cfg.SecretKey = credentials()
		return NewS3(cfg)
	}
	return nil, fmt.Errorf("store url %q: unsupported scheme (use file://, sos:// or s3://)", raw)
}

func credentials() (string, string) {
	if k := os.Getenv("EXOSCALE_API_KEY"); k != "" {
		return k, os.Getenv("EXOSCALE_API_SECRET")
	}
	return os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
}
