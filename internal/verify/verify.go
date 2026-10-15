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
	// Keys are the trusted public keys. Each manifest is checked against the
	// key matching its key_id, so a chain that spans a key rotation verifies
	// when both the retired and the current key are given.
	Keys  []ed25519.PublicKey
	Chain bool
	// Checkpoint, if set, holds publications seen in earlier runs. Every
	// entry must still be part of the chain (fork / rewrite detection); the
	// current chain head is added on success. Requires Chain.
	Checkpoint *Checkpoint
	Log        func(string)
}

// Checkpoint is a verifier's memory of publications it has already seen.
type Checkpoint struct {
	Entries []CheckEntry `json:"entries"`
}

// CheckEntry is one remembered publication.
type CheckEntry struct {
	Sequence int    `json:"sequence"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
}

// Pointer is the subset of latest.json the verifier uses.
type Pointer struct {
	Manifest       string `json:"manifest"`
	ManifestSHA256 string `json:"manifest_sha256"`
	ChainHead      *struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"chain_head"`
}

func readPointer(ctx context.Context, f Fetcher) (*Pointer, error) {
	b, err := f.Fetch(ctx, "latest.json")
	if err != nil {
		return nil, fmt.Errorf("latest.json: %w", err)
	}
	var p Pointer
	if err := json.Unmarshal(b, &p); err != nil || p.Manifest == "" {
		return nil, fmt.Errorf("latest.json: invalid pointer")
	}
	return &p, nil
}

func manifestPath(target string) string {
	t := strings.TrimRight(target, "/")
	if !strings.HasSuffix(t, "manifest.json") {
		t += "/manifest.json"
	}
	return t
}

// Resolve returns the manifest path for target ("" = latest.json pointer).
func Resolve(ctx context.Context, f Fetcher, target string) (string, error) {
	if target != "" {
		return manifestPath(target), nil
	}
	p, err := readPointer(ctx, f)
	if err != nil {
		return "", err
	}
	return p.Manifest, nil
}

func keyFor(keys []ed25519.PublicKey, id string) ed25519.PublicKey {
	for _, k := range keys {
		if manifest.KeyID(k) == id {
			return k
		}
	}
	return nil
}

// One verifies the manifest at mpath and all files it lists. If wantSHA is
// non-empty, the manifest bytes must hash to it.
func One(ctx context.Context, f Fetcher, mpath, wantSHA string, o Options) (*Checked, error) {
	log := o.Log
	if log == nil {
		log = func(string) {}
	}
	if len(o.Keys) == 0 {
		return nil, errors.New("no trusted public key")
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
	m, err := manifest.Parse(mb)
	if err != nil {
		return nil, fail("%s: %v", mpath, err)
	}
	pub := keyFor(o.Keys, m.KeyID)
	if pub == nil {
		return nil, fail("%s: signed with key %s, which is not among the trusted keys", mpath, m.KeyID)
	}
	if err := manifest.VerifySig(pub, mb, sig); err != nil {
		return nil, fail("%s: %v", mpath, err)
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
	log(fmt.Sprintf("ok  %s  (%s v%d, publication #%d, %d files, ledger #%d)", mpath, m.Label, m.Version, m.Sequence, len(m.Files), m.Ledger.Seq))
	return &Checked{Dir: dir, Manifest: m, SHA256: sum}, nil
}

// walk verifies a chain of manifests starting at mpath.
// seed holds an already verified newest manifest (whose previous link is
// mpath), so it is not fetched twice.
func walk(ctx context.Context, f Fetcher, mpath, want string, o Options, seed *Checked) ([]*Checked, error) {
	var out []*Checked
	seen := map[string]bool{}
	if seed != nil {
		out = append(out, seed)
		seen[seed.Dir+"/manifest.json"] = true
	}
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
	// Publication #n must be the n-th from the start of the history: this
	// catches a link that skips publications (fork) or a history cut short.
	// Manifests written before sequence numbers existed carry 0 and are
	// counted but not checked.
	if o.Chain {
		for i, c := range out {
			if s := c.Manifest.Sequence; s > 0 && s != len(out)-i {
				return out, fail("%s: publication #%d is number %d in the history it links to (publications skipped or history truncated)", c.Dir, s, len(out)-i)
			}
		}
	}
	return out, nil
}

// Run verifies a publication. With no target it verifies what readers see:
// the publication latest.json points to and the latest/ copies of it. With
// Chain it walks the whole publication history from the newest publication
// (latest.json chain_head), so back-filled periods are covered too, and
// checks that the displayed publication is part of that history.
func Run(ctx context.Context, f Fetcher, target string, o Options) ([]*Checked, error) {
	if o.Checkpoint != nil && !o.Chain {
		return nil, errors.New("a checkpoint requires --chain")
	}
	var ptr *Pointer
	start, want := "", ""
	if target != "" {
		start = manifestPath(target)
	} else {
		p, err := readPointer(ctx, f)
		if err != nil {
			return nil, err
		}
		ptr = p
		start, want = p.Manifest, p.ManifestSHA256
	}

	// 1. The displayed publication itself.
	shown, err := One(ctx, f, start, want, o)
	if err != nil {
		return nil, err
	}
	// 2. The stable latest/ copies readers open must match it byte for byte.
	if ptr != nil {
		if _, err := One(ctx, f, "latest/manifest.json", shown.SHA256, o); err != nil {
			if IsFailure(err) || errors.Is(err, store.ErrNotFound) {
				return nil, fail("latest/ does not match the signed publication %s: %v", start, err)
			}
			return nil, err
		}
	}
	if !o.Chain {
		return []*Checked{shown}, nil
	}

	// 3. The whole history, from the newest publication of any period.
	headPath, headWant := start, shown.SHA256
	if ptr != nil && ptr.ChainHead != nil && ptr.ChainHead.Path != "" {
		headPath, headWant = ptr.ChainHead.Path, ptr.ChainHead.SHA256
	}
	var seed *Checked
	if headPath == start && headWant == shown.SHA256 {
		seed, headPath, headWant = shown, "", ""
		if p := shown.Manifest.Previous; p != nil {
			headPath, headWant = p.Path, p.SHA256
		}
	}
	out, err := walk(ctx, f, headPath, headWant, o, seed)
	if err != nil {
		return out, err
	}
	found := false
	for _, c := range out {
		if c.SHA256 == shown.SHA256 {
			found = true
		}
	}
	if !found {
		return out, fail("%s is not part of the publication history starting at %s", start, headPath)
	}

	// 4. Earlier observations must still be part of the history.
	if cp := o.Checkpoint; cp != nil {
		byPath := map[string]*Checked{}
		for _, c := range out {
			byPath[c.Dir+"/manifest.json"] = c
		}
		for _, e := range cp.Entries {
			c, ok := byPath[e.Path]
			if !ok {
				return out, fail("checkpoint: publication #%d (%s) is no longer in the history; it was forked or rewritten", e.Sequence, e.Path)
			}
			if c.SHA256 != e.SHA256 {
				return out, fail("checkpoint: %s changed: was %s, now %s; history was rewritten", e.Path, e.SHA256, c.SHA256)
			}
		}
		for _, c := range out {
			p := c.Dir + "/manifest.json"
			known := false
			for _, e := range cp.Entries {
				if e.Path == p {
					known = true
				}
			}
			if !known {
				cp.Entries = append(cp.Entries, CheckEntry{Sequence: c.Manifest.Sequence, Path: p, SHA256: c.SHA256})
			}
		}
	}
	return out, nil
}
