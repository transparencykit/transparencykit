// Package verify checks published reports against their signed manifests.
package verify

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/transparencykit/transparencykit/internal/canon"
	"github.com/transparencykit/transparencykit/internal/manifest"
	"github.com/transparencykit/transparencykit/internal/store"
)

// Fetcher retrieves published files by relative path.
type Fetcher interface {
	Fetch(ctx context.Context, p string) ([]byte, error)
}

// HTTP fetches from a base URL (e.g. direct SOS object URLs or `transparencykit serve`).
type HTTP struct {
	Base   string
	Client *http.Client
}

// Fetch GETs base/p.
func (h HTTP) Fetch(ctx context.Context, p string) ([]byte, error) {
	c := h.Client
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	u := strings.TrimRight(h.Base, "/") + "/" + strings.TrimLeft(p, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", u, store.ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// Store fetches from an object store.
type Store struct{ S store.Store }

// Fetch reads p from the store.
func (s Store) Fetch(ctx context.Context, p string) ([]byte, error) { return s.S.Get(ctx, p) }

// Failure is a verification failure (as opposed to an I/O error).
type Failure struct{ Msg string }

func (f *Failure) Error() string { return f.Msg }

func fail(format string, a ...any) error { return &Failure{Msg: fmt.Sprintf(format, a...)} }

// IsFailure reports whether err is a verification failure.
func IsFailure(err error) bool {
	var f *Failure
	return errors.As(err, &f)
}

// Checked describes one verified publication.
type Checked struct {
	Dir      string
	Manifest *manifest.Manifest
	SHA256   string
}

// Options configures verification.
type Options struct {
	Pub   ed25519.PublicKey
	Chain bool
	Log   func(string)
}

// Resolve returns the manifest path for target ("" = latest.json pointer).
func Resolve(ctx context.Context, f Fetcher, target string) (string, error) {
	if target != "" {
		t := strings.TrimRight(target, "/")
		if !strings.HasSuffix(t, "manifest.json") {
			t += "/manifest.json"
		}
		return t, nil
	}
	b, err := f.Fetch(ctx, "latest.json")
	if err != nil {
		return "", fmt.Errorf("latest.json: %w", err)
	}
	var p struct {
		Manifest string `json:"manifest"`
	}
	if err := json.Unmarshal(b, &p); err != nil || p.Manifest == "" {
		return "", fmt.Errorf("latest.json: invalid pointer")
	}
	return p.Manifest, nil
}

// One verifies the manifest at mpath and all files it lists. If wantSHA is
// non-empty, the manifest bytes must hash to it.
func One(ctx context.Context, f Fetcher, mpath, wantSHA string, o Options) (*Checked, error) {
	log := o.Log
	if log == nil {
		log = func(string) {}
	}
	mb, err := f.Fetch(ctx, mpath)
	if err != nil {
		return nil, err
	}
	sum := canon.HexSHA256(mb)
	if wantSHA != "" && sum != wantSHA {
		return nil, fail("%s: SHA-256 %s does not match link %s", mpath, sum, wantSHA)
	}
	dir := path.Dir(mpath)
	sig, err := f.Fetch(ctx, dir+"/manifest.sig")
	if err != nil {
		return nil, err
	}
	if err := manifest.VerifySig(o.Pub, mb, sig); err != nil {
		return nil, fail("%s: %v", mpath, err)
	}
	m, err := manifest.Parse(mb)
	if err != nil {
		return nil, fail("%s: %v", mpath, err)
	}
	if m.KeyID != manifest.KeyID(o.Pub) {
		return nil, fail("%s: key_id %s does not match trusted key %s", mpath, m.KeyID, manifest.KeyID(o.Pub))
	}
	for _, fl := range m.Files {
		if strings.Contains(fl.Path, "..") || strings.HasPrefix(fl.Path, "/") {
			return nil, fail("%s: unsafe file path %q", mpath, fl.Path)
		}
		b, err := f.Fetch(ctx, dir+"/"+fl.Path)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, fail("%s/%s: missing", dir, fl.Path)
			}
			return nil, err
		}
		if len(b) != fl.Size || canon.HexSHA256(b) != fl.SHA256 {
			return nil, fail("%s/%s: content does not match manifest (size %d vs %d)", dir, fl.Path, len(b), fl.Size)
		}
	}
	log(fmt.Sprintf("ok  %s  (%s v%d, %d files, ledger #%d)", mpath, m.Label, m.Version, len(m.Files), m.Ledger.Seq))
	return &Checked{Dir: dir, Manifest: m, SHA256: sum}, nil
}

// Run verifies target and, with Chain, every previous publication.
func Run(ctx context.Context, f Fetcher, target string, o Options) ([]*Checked, error) {
	mpath, err := Resolve(ctx, f, target)
	if err != nil {
		return nil, err
	}
	var out []*Checked
	want := ""
	seen := map[string]bool{}
	for mpath != "" {
		if seen[mpath] {
			return out, fail("manifest chain loops at %s", mpath)
		}
		seen[mpath] = true
		c, err := One(ctx, f, mpath, want, o)
		if err != nil {
			return out, err
		}
		out = append(out, c)
		if !o.Chain || c.Manifest.Previous == nil {
			break
		}
		mpath, want = c.Manifest.Previous.Path, c.Manifest.Previous.SHA256
	}
	return out, nil
}
