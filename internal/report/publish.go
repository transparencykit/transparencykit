package report

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/transparencykit/transparencykit/internal/canon"
	"github.com/transparencykit/transparencykit/internal/config"
	"github.com/transparencykit/transparencykit/internal/ledger"
	"github.com/transparencykit/transparencykit/internal/manifest"
	"github.com/transparencykit/transparencykit/internal/record"
	"github.com/transparencykit/transparencykit/internal/store"
)

// Pointer is latest.json.
type Pointer struct {
	Label          string `json:"label"`
	Version        int    `json:"version"`
	Manifest       string `json:"manifest"`
	ManifestSHA256 string `json:"manifest_sha256"`
	PeriodEnd      string `json:"period_end"`
	// ChainHead is the most recently published manifest (any period); the
	// next publication links to it via "previous".
	ChainHead *manifest.Link `json:"chain_head"`
}

// PublishOptions configures a publication.
type PublishOptions struct {
	Config  *config.Config
	Ledger  *ledger.Ledger
	Public  store.Store
	Key     ed25519.PrivateKey
	Period  Period
	Now     time.Time
	DryRun  bool
	Verbose func(string)
}

// Result summarises a publication.
type Result struct {
	Label, Prefix  string
	Version        int
	Files          int
	ManifestSHA256 string
	Ledger         LedgerRef
	Report         *Report
	DryRun         bool
}

// ContentType guesses by extension.
func ContentType(p string) string {
	switch path.Ext(p) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".atom":
		return "application/atom+xml"
	case ".pub", ".txt", ".sig":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

var versionRe = regexp.MustCompile(`^reports/([^/]+)/v(\d+)/manifest\.json$`)

// Published lists existing publications (newest label first).
func Published(ctx context.Context, s store.Store) ([]IndexItem, error) {
	keys, err := s.List(ctx, "reports/")
	if err != nil {
		return nil, err
	}
	var out []IndexItem
	for _, k := range keys {
		if m := versionRe.FindStringSubmatch(k); m != nil {
			v, _ := strconv.Atoi(m[2])
			out = append(out, IndexItem{Label: m[1], Version: v, Path: "reports/" + m[1] + "/v" + m[2] + "/"})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Label != out[j].Label {
			return out[i].Label > out[j].Label
		}
		return out[i].Version > out[j].Version
	})
	return out, nil
}

// Publish verifies the ledger, renders all outputs, signs and uploads them.
func Publish(ctx context.Context, o PublishOptions) (*Result, error) {
	log := o.Verbose
	if log == nil {
		log = func(string) {}
	}
	entries, head, err := o.Ledger.Verify(ctx)
	if err != nil {
		return nil, fmt.Errorf("ledger verification failed, refusing to publish: %w", err)
	}
	log(fmt.Sprintf("ledger verified: %d entries, head %s", head.Seq, head.Hash))
	eff, err := record.Resolve(entries)
	if err != nil {
		return nil, err
	}
	cfg := o.Config
	x := Aggregate(eff, o.Period, cfg.Home())
	led := LedgerRef{Seq: head.Seq, Hash: head.Hash}
	gen := o.Now.UTC().Format(time.RFC3339)

	pubs, err := Published(ctx, o.Public)
	if err != nil {
		return nil, err
	}
	version := 1
	for _, p := range pubs {
		if p.Label == o.Period.Label && p.Version >= version {
			version = p.Version + 1
		}
	}
	prefix := fmt.Sprintf("reports/%s/v%d/", o.Period.Label, version)

	// Previous manifest in the publication chain (hash recomputed from bytes).
	var ptr *Pointer
	if b, err := o.Public.Get(ctx, "latest.json"); err == nil {
		ptr = &Pointer{}
		if err := json.Unmarshal(b, ptr); err != nil {
			return nil, fmt.Errorf("latest.json: %w", err)
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	var prev *manifest.Link
	if ptr != nil && ptr.ChainHead != nil {
		mb, err := o.Public.Get(ctx, ptr.ChainHead.Path)
		if err != nil {
			return nil, fmt.Errorf("previous manifest %s: %w", ptr.ChainHead.Path, err)
		}
		sum := canon.HexSHA256(mb)
		if sum != ptr.ChainHead.SHA256 {
			return nil, fmt.Errorf("previous manifest %s hash %s does not match latest.json (%s): public store tampered?", ptr.ChainHead.Path, sum, ptr.ChainHead.SHA256)
		}
		prev = &manifest.Link{Path: ptr.ChainHead.Path, SHA256: sum}
	} else if len(pubs) > 0 {
		return nil, fmt.Errorf("public store has publications but no valid latest.json chain head")
	}

	pub := o.Key.Public().(ed25519.PublicKey)
	keyID := manifest.KeyID(pub)
	rep := Build(x, cfg, o.Period, version, gen, led)
	art := BuildArt28(x, cfg, o.Period, gen, led)
	subs := Subprocessors{Schema: SubSchema, Provider: cfg.Provider.Name, AsOf: o.Period.End,
		Current: rep.Subprocessors.Current, History: x.SubHistory}
	if subs.History == nil {
		subs.History = []SubChangeOut{}
	}

	files := map[string][]byte{}
	add := func(name string, b []byte, err error) error {
		if err != nil {
			return fmt.Errorf("render %s: %w", name, err)
		}
		files[name] = b
		return nil
	}
	reportJSON, err := JSON(rep)
	if err := add("report.json", reportJSON, err); err != nil {
		return nil, err
	}
	h, err := HTML(rep, keyID)
	if err := add("index.html", h, err); err != nil {
		return nil, err
	}
	aj, err := JSON(art)
	if err := add("data-act-art28.json", aj, err); err != nil {
		return nil, err
	}
	ah, err := Art28HTML(art)
	if err := add("data-act-art28.html", ah, err); err != nil {
		return nil, err
	}
	sj, err := JSON(subs)
	if err := add("subprocessors.json", sj, err); err != nil {
		return nil, err
	}
	idBase := "urn:transparencykit:" + slug(cfg.Provider.Name)
	sa, err := SubAtom(cfg.Provider.Name, idBase, x.SubHistory, gen)
	if err := add("subprocessors.atom", sa, err); err != nil {
		return nil, err
	}
	for n, b := range DSAFiles(x, cfg, o.Period, version, gen) {
		files["dsa/"+n] = b
	}
	files["dsa/README.txt"] = DSAReadme(o.Period)

	m := &manifest.Manifest{Schema: manifest.Schema, Provider: cfg.Provider.Name, Label: o.Period.Label,
		Version: version, Period: manifest.Period{Start: o.Period.Start, End: o.Period.End}, GeneratedAt: gen,
		Ledger: manifest.LedgerRef{Seq: head.Seq, Hash: head.Hash}, Previous: prev, KeyID: keyID}
	for n, b := range files {
		m.AddFile(n, b)
	}
	mb, err := m.Encode()
	if err != nil {
		return nil, err
	}
	sig := manifest.Sign(o.Key, mb)
	msum := canon.HexSHA256(mb)
	res := &Result{Label: o.Period.Label, Prefix: prefix, Version: version, Files: len(files) + 2,
		ManifestSHA256: msum, Ledger: led, Report: rep, DryRun: o.DryRun}
	if o.DryRun {
		return res, nil
	}

	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	put := func(key string, b []byte, createOnly bool) error {
		return o.Public.Put(ctx, key, b, store.PutOptions{ContentType: ContentType(key), CreateOnly: createOnly, Public: true})
	}
	// Immutable versioned copy: files first, then manifest and signature.
	for _, n := range names {
		if err := put(prefix+n, files[n], true); err != nil {
			return nil, fmt.Errorf("upload %s: %w", prefix+n, err)
		}
	}
	if err := put(prefix+"manifest.json", mb, true); err != nil {
		return nil, err
	}
	if err := put(prefix+"manifest.sig", sig, true); err != nil {
		return nil, err
	}
	log(fmt.Sprintf("uploaded %s (%d files)", prefix, len(files)+2))

	// Pointer and convenience copies.
	np := Pointer{ChainHead: &manifest.Link{Path: prefix + "manifest.json", SHA256: msum}}
	updateLatest := ptr == nil || o.Period.End >= ptr.PeriodEnd
	if updateLatest {
		np.Label, np.Version, np.Manifest, np.ManifestSHA256, np.PeriodEnd = o.Period.Label, version, prefix+"manifest.json", msum, o.Period.End
		for _, n := range names {
			if err := put("latest/"+n, files[n], false); err != nil {
				return nil, err
			}
		}
		if err := put("latest/manifest.json", mb, false); err != nil {
			return nil, err
		}
		if err := put("latest/manifest.sig", sig, false); err != nil {
			return nil, err
		}
	} else {
		np.Label, np.Version, np.Manifest, np.ManifestSHA256, np.PeriodEnd = ptr.Label, ptr.Version, ptr.Manifest, ptr.ManifestSHA256, ptr.PeriodEnd
	}
	pb, _ := JSON(np)
	if err := put("latest.json", pb, false); err != nil {
		return nil, err
	}
	pk, _ := manifest.EncodePublic(pub)
	if err := put("transparencykit.pub", pk, false); err != nil {
		return nil, err
	}
	all, err := Published(ctx, o.Public)
	if err != nil {
		return nil, err
	}
	rootHTML, err := RootHTML(cfg.Provider.Name, all)
	if err != nil {
		return nil, err
	}
	if err := put("index.html", rootHTML, false); err != nil {
		return nil, err
	}
	return res, nil
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
