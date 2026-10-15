package report

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
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
	Config *config.Config
	Ledger *ledger.Ledger
	Public store.Store
	Key    ed25519.PrivateKey
	Period Period
	Now    time.Time
	// AsOf is the date of state-like disclosures (infrastructure, measures,
	// current subprocessors). Zero means the earlier of Now and the period
	// end.
	AsOf time.Time
	// Qualitative holds the DSA qualitative answers (QualitativeKeys).
	Qualitative map[string]string
	DryRun      bool
	// DraftDir, if set, renders the publication into this local directory
	// for review instead of signing and uploading it.
	DraftDir string
	// FromDraft, if set, publishes the reviewed draft in this directory. The
	// outputs are rendered again from the ledger state recorded in the draft
	// and must match it byte for byte, except the DSA qualitative answers in
	// dsa/tab11_qualitative.csv, which may be edited during review.
	FromDraft string
	Verbose   func(string)
}

// Result summarises a publication.
type Result struct {
	Label, Prefix  string
	Version        int
	Sequence       int
	Files          int
	ManifestSHA256 string
	Ledger         LedgerRef
	Report         *Report
	DryRun         bool
	DraftDir       string
	// Abandoned lists version folders left behind by interrupted uploads.
	Abandoned []string
	// Adopted lists signed publications found outside the chain recorded in
	// latest.json (an interrupted run, or an older latest.json put back) that
	// were linked back into the history.
	Adopted []string
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

var (
	versionRe = regexp.MustCompile(`^reports/([^/]+)/v(\d+)/manifest\.sig$`)
	anyVerRe  = regexp.MustCompile(`^reports/([^/]+)/v(\d+)/`)
)

// Published lists complete publications (signature present), newest label
// first.
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

// chainState is what the public store says about publication history.
type chainState struct {
	ptr       *Pointer
	head      *manifest.Link // newest publication in the chain
	length    int            // publications in the chain
	version   int            // next free version for the label
	abandoned []string
	adopted   []string
	adoptedM  map[string]*manifest.Manifest
}

// readChain walks the publication chain from latest.json, links back signed
// publications that are missing from it only because the pointer is stale,
// and refuses when any other signed publication is outside the chain (the
// next publication would fork history).
func readChain(ctx context.Context, s store.Store, label string, pub ed25519.PublicKey) (*chainState, error) {
	st := &chainState{version: 1, adoptedM: map[string]*manifest.Manifest{}}
	keys, err := s.List(ctx, "reports/")
	if err != nil {
		return nil, err
	}
	dirs := map[string]bool{}   // every version folder
	signed := map[string]bool{} // folders with manifest.sig
	for _, k := range keys {
		m := anyVerRe.FindStringSubmatch(k)
		if m == nil {
			continue
		}
		dir := m[0]
		dirs[dir] = true
		if strings.HasSuffix(k, "/manifest.sig") && strings.Count(strings.TrimPrefix(k, dir), "/") == 0 {
			signed[dir] = true
		}
		if v, _ := strconv.Atoi(m[2]); m[1] == label && v >= st.version {
			st.version = v + 1
		}
	}
	for d := range dirs {
		if !signed[d] {
			st.abandoned = append(st.abandoned, d)
		}
	}
	sort.Strings(st.abandoned)

	if b, err := s.Get(ctx, "latest.json"); err == nil {
		st.ptr = &Pointer{}
		if err := json.Unmarshal(b, st.ptr); err != nil {
			return nil, fmt.Errorf("latest.json: %w", err)
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if st.ptr == nil || st.ptr.ChainHead == nil {
		if len(signed) > 0 {
			return nil, fmt.Errorf("public store has publications but no valid latest.json chain head")
		}
		return st, nil
	}

	// Walk the recorded chain (hashes recomputed from bytes).
	inChain := map[string]bool{}
	link := st.ptr.ChainHead
	for p, want := link.Path, link.SHA256; p != ""; {
		mb, err := s.Get(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("chain manifest %s: %w", p, err)
		}
		if sum := canon.HexSHA256(mb); sum != want {
			return nil, fmt.Errorf("chain manifest %s hash %s does not match its link (%s): public store tampered?", p, sum, want)
		}
		m, err := manifest.Parse(mb)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		inChain[path.Dir(p)+"/"] = true
		st.length++
		if m.Previous == nil {
			break
		}
		p, want = m.Previous.Path, m.Previous.SHA256
	}
	st.head = &manifest.Link{Path: link.Path, SHA256: link.SHA256}

	// Signed publications outside the chain. One that links to the current
	// head and carries our signature was published after the pointer was
	// last written: link it back and repeat.
	for {
		var outside []string
		for d := range signed {
			if !inChain[d] {
				outside = append(outside, d)
			}
		}
		if len(outside) == 0 {
			break
		}
		sort.Strings(outside)
		progress := false
		for _, d := range outside {
			mb, err := s.Get(ctx, d+"manifest.json")
			if err != nil {
				continue
			}
			sig, err := s.Get(ctx, d+"manifest.sig")
			if err != nil {
				continue
			}
			m, err := manifest.Parse(mb)
			if err != nil || m.Previous == nil || m.Previous.Path != st.head.Path || m.Previous.SHA256 != st.head.SHA256 {
				continue
			}
			if manifest.VerifySig(pub, mb, sig) != nil {
				continue
			}
			st.head = &manifest.Link{Path: d + "manifest.json", SHA256: canon.HexSHA256(mb)}
			st.adopted = append(st.adopted, d)
			st.adoptedM[d] = m
			inChain[d] = true
			st.length++
			progress = true
			break
		}
		if !progress {
			return nil, fmt.Errorf("signed publication(s) %s are not part of the history recorded in latest.json (chain head %s); "+
				"latest.json was replaced or rolled back. Publishing now would fork the history: restore latest.json so that "+
				"chain_head points to the newest publication (see docs/OPERATIONS.md)", strings.Join(outside, ", "), st.ptr.ChainHead.Path)
		}
	}
	return st, nil
}

// Draft is draft.json, written next to the rendered files of a draft.
type Draft struct {
	Schema      string          `json:"schema"`
	Label       string          `json:"label"`
	Version     int             `json:"version"`
	Sequence    int             `json:"sequence"`
	AsOf        string          `json:"as_of"`
	GeneratedAt string          `json:"generated_at"`
	Ledger      LedgerRef       `json:"ledger"`
	Previous    *manifest.Link  `json:"previous"`
	KeyID       string          `json:"key_id"`
	Files       []manifest.File `json:"files"`
}

// DraftSchema identifies draft.json.
const DraftSchema = "transparencykit.draft/v1"

// ReadDraft loads draft.json from dir.
func ReadDraft(dir string) (*Draft, error) {
	b, err := os.ReadFile(filepath.Join(dir, "draft.json"))
	if err != nil {
		return nil, err
	}
	var d Draft
	if err := json.Unmarshal(b, &d); err != nil || d.Schema != DraftSchema {
		return nil, fmt.Errorf("%s: not a transparencykit draft", dir)
	}
	return &d, nil
}

// QualitativeFile is the one draft file reviewers may edit.
const QualitativeFile = "dsa/tab11_qualitative.csv"

// Publish verifies the ledger, renders all outputs, signs and uploads them
// (or writes a draft for review, see PublishOptions).
func Publish(ctx context.Context, o PublishOptions) (*Result, error) {
	log := o.Verbose
	if log == nil {
		log = func(string) {}
	}
	if o.DraftDir != "" && o.FromDraft != "" {
		return nil, errors.New("draft and from-draft are exclusive")
	}
	now, asOf := o.Now, o.AsOf
	var d *Draft
	if o.FromDraft != "" {
		var err error
		if d, err = ReadDraft(o.FromDraft); err != nil {
			return nil, err
		}
		if o.Period, err = ParsePeriod(d.Label); err != nil {
			return nil, err
		}
		if now, err = time.Parse(time.RFC3339, d.GeneratedAt); err != nil {
			return nil, fmt.Errorf("draft generated_at: %w", err)
		}
		if asOf, err = time.Parse("2006-01-02", d.AsOf); err != nil {
			return nil, fmt.Errorf("draft as_of: %w", err)
		}
	}
	if asOf.IsZero() {
		asOf = now
	}
	period := o.Period.WithAsOf(asOf)

	entries, head, err := o.Ledger.Verify(ctx)
	if err != nil {
		return nil, fmt.Errorf("ledger verification failed, refusing to publish: %w", err)
	}
	log(fmt.Sprintf("ledger verified: %d entries, head %s", head.Seq, head.Hash))
	if d != nil {
		// Render from the reviewed ledger state, even if records were appended
		// since.
		n := -1
		for i, e := range entries {
			if e.Seq == d.Ledger.Seq && e.Hash == d.Ledger.Hash {
				n = i
			}
		}
		if n < 0 && d.Ledger.Seq != 0 {
			return nil, fmt.Errorf("draft ledger state #%d %s is not part of the ledger", d.Ledger.Seq, d.Ledger.Hash)
		}
		entries = entries[:n+1]
		head = ledger.Head{Seq: d.Ledger.Seq, Hash: d.Ledger.Hash}
	}
	eff, err := record.Resolve(entries)
	if err != nil {
		return nil, err
	}
	cfg := o.Config
	x := Aggregate(eff, period, cfg.Home())
	led := LedgerRef{Seq: head.Seq, Hash: head.Hash}
	gen := now.UTC().Format(time.RFC3339)

	pub := o.Key.Public().(ed25519.PublicKey)
	keyID := manifest.KeyID(pub)
	cs, err := readChain(ctx, o.Public, period.Label, pub)
	if err != nil {
		return nil, err
	}
	for _, a := range cs.abandoned {
		log("skipping incomplete version folder " + a + " (interrupted upload; safe to delete)")
	}
	for _, a := range cs.adopted {
		log("linking back signed publication " + a + " missing from latest.json")
	}
	version, sequence, prev := cs.version, cs.length+1, cs.head
	if d != nil {
		if d.Version != version || d.Sequence != sequence || !sameLink(d.Previous, prev) {
			return nil, fmt.Errorf("the public store changed since the draft was made (draft v%d #%d, now v%d #%d); make a new draft",
				d.Version, d.Sequence, version, sequence)
		}
		if d.KeyID != keyID {
			return nil, fmt.Errorf("draft was rendered for key %s, signing key is %s", d.KeyID, keyID)
		}
	}
	prefix := fmt.Sprintf("reports/%s/v%d/", period.Label, version)

	rep := Build(x, cfg, period, version, gen, led)
	art := BuildArt28(x, cfg, period, gen, led)
	subs := Subprocessors{Schema: SubSchema, Provider: cfg.Provider.Name, AsOf: period.StateDate(),
		Current: rep.Subprocessors.Current, History: x.SubHistory, Upcoming: rep.Subprocessors.Upcoming}
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
	feed := append(append([]SubChangeOut{}, x.SubHistory...), x.SubUpcoming...)
	sa, err := SubAtom(cfg.Provider.Name, idBase, feed, gen)
	if err := add("subprocessors.atom", sa, err); err != nil {
		return nil, err
	}
	for n, b := range DSAFiles(x, cfg, period, version, gen, o.Qualitative) {
		files["dsa/"+n] = b
	}
	files["dsa/README.txt"] = DSAReadme(period)

	if d != nil {
		if err := applyDraft(o.FromDraft, d, files); err != nil {
			return nil, err
		}
		log("draft " + o.FromDraft + " matches the ledger state it was made from")
	}

	m := &manifest.Manifest{Schema: manifest.Schema, Provider: cfg.Provider.Name, Label: period.Label,
		Version: version, Sequence: sequence, Period: manifest.Period{Start: period.Start, End: period.End}, GeneratedAt: gen,
		Ledger: manifest.LedgerRef{Seq: head.Seq, Hash: head.Hash}, Previous: prev, KeyID: keyID}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m.AddFile(n, files[n])
	}
	res := &Result{Label: period.Label, Prefix: prefix, Version: version, Sequence: sequence, Files: len(files) + 2,
		Ledger: led, Report: rep, DryRun: o.DryRun, Abandoned: cs.abandoned, Adopted: cs.adopted}

	if o.DraftDir != "" {
		if err := writeDraft(o.DraftDir, files, &Draft{Schema: DraftSchema, Label: period.Label, Version: version,
			Sequence: sequence, AsOf: period.StateDate(), GeneratedAt: gen, Ledger: led, Previous: prev, KeyID: keyID,
			Files: m.Files}); err != nil {
			return nil, err
		}
		res.DraftDir = o.DraftDir
		return res, nil
	}
	mb, err := m.Encode()
	if err != nil {
		return nil, err
	}
	sig := manifest.Sign(o.Key, mb)
	msum := canon.HexSHA256(mb)
	res.ManifestSHA256 = msum
	if o.DryRun {
		return res, nil
	}

	put := func(key string, b []byte, createOnly bool) error {
		return o.Public.Put(ctx, key, b, store.PutOptions{ContentType: ContentType(key), CreateOnly: createOnly, Public: true})
	}
	// Immutable versioned copy: files first, then manifest and signature.
	// An interrupted run leaves a folder without manifest.sig, which the next
	// run skips (it takes the next version number).
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
	log(fmt.Sprintf("uploaded %s (%d files, publication #%d)", prefix, len(files)+2, sequence))

	// Which publication readers see under latest/: the one with the latest
	// period end (a back-filled older period does not replace it).
	shown := cs.ptr
	for _, a := range cs.adopted {
		am := cs.adoptedM[a]
		if shown == nil || am.Period.End >= shown.PeriodEnd {
			ab, err := o.Public.Get(ctx, a+"manifest.json")
			if err != nil {
				return nil, err
			}
			shown = &Pointer{Label: am.Label, Version: am.Version, Manifest: a + "manifest.json", ManifestSHA256: canon.HexSHA256(ab), PeriodEnd: am.Period.End}
		}
	}
	np := Pointer{ChainHead: &manifest.Link{Path: prefix + "manifest.json", SHA256: msum}}
	if shown == nil || period.End >= shown.PeriodEnd {
		np.Label, np.Version, np.Manifest, np.ManifestSHA256, np.PeriodEnd = period.Label, version, prefix+"manifest.json", msum, period.End
	} else {
		np.Label, np.Version, np.Manifest, np.ManifestSHA256, np.PeriodEnd = shown.Label, shown.Version, shown.Manifest, shown.ManifestSHA256, shown.PeriodEnd
	}
	if err := copyLatest(ctx, o.Public, np.Manifest, put); err != nil {
		return nil, err
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

// copyLatest makes latest/ an exact copy of the publication at mpath, so the
// stable URLs readers open match a signed manifest.
func copyLatest(ctx context.Context, s store.Store, mpath string, put func(string, []byte, bool) error) error {
	dir := path.Dir(mpath) + "/"
	mb, err := s.Get(ctx, mpath)
	if err != nil {
		return err
	}
	m, err := manifest.Parse(mb)
	if err != nil {
		return err
	}
	for _, f := range m.Files {
		b, err := s.Get(ctx, dir+f.Path)
		if err != nil {
			return err
		}
		if err := put("latest/"+f.Path, b, false); err != nil {
			return err
		}
	}
	sig, err := s.Get(ctx, dir+"manifest.sig")
	if err != nil {
		return err
	}
	// Signature and manifest last: a reader never sees a new manifest next to
	// old files.
	if err := put("latest/manifest.sig", sig, false); err != nil {
		return err
	}
	return put("latest/manifest.json", mb, false)
}

func sameLink(a, b *manifest.Link) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Path == b.Path && a.SHA256 == b.SHA256
}

func writeDraft(dir string, files map[string][]byte, d *Draft) error {
	if ents, err := os.ReadDir(dir); err == nil && len(ents) > 0 {
		return fmt.Errorf("draft directory %s is not empty", dir)
	}
	for n, b := range files {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	db, err := JSON(d)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "draft.json"), db, 0o644)
}

// applyDraft checks the reviewed draft against the fresh rendering and takes
// over the edited qualitative answers.
func applyDraft(dir string, d *Draft, files map[string][]byte) error {
	listed := map[string]bool{}
	for _, f := range d.Files {
		listed[f.Path] = true
	}
	for n, b := range files {
		if !listed[n] {
			return fmt.Errorf("draft has no %s; make a new draft", n)
		}
		db, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(n)))
		if err != nil {
			return fmt.Errorf("draft: %w", err)
		}
		if n == QualitativeFile {
			nb, err := mergeQualitative(b, db)
			if err != nil {
				return fmt.Errorf("draft %s: %w", n, err)
			}
			files[n] = nb
			continue
		}
		if !bytes.Equal(b, db) {
			return fmt.Errorf("draft %s differs from what the ledger produces; only %s may be edited (make a new draft)", n, QualitativeFile)
		}
	}
	return nil
}

// mergeQualitative keeps the rendered rows and questions and takes the
// Answer column from the edited file.
func mergeQualitative(rendered, edited []byte) ([]byte, error) {
	read := func(b []byte) ([][]string, error) {
		r := csv.NewReader(bytes.NewReader(b))
		r.Comma = ';'
		return r.ReadAll()
	}
	want, err := read(rendered)
	if err != nil {
		return nil, err
	}
	got, err := read(edited)
	if err != nil {
		return nil, err
	}
	if len(got) != len(want) {
		return nil, fmt.Errorf("has %d rows, expected %d (edit only the Answer column)", len(got), len(want))
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			return nil, fmt.Errorf("row %d has %d columns, expected %d", i+1, len(got[i]), len(want[i]))
		}
		for j := 0; j < len(want[i])-1; j++ {
			if got[i][j] != want[i][j] {
				return nil, fmt.Errorf("row %d column %d was changed (edit only the Answer column)", i+1, j+1)
			}
		}
		if i == 0 && got[0][len(got[0])-1] != want[0][len(want[0])-1] {
			return nil, fmt.Errorf("header was changed")
		}
		want[i][len(want[i])-1] = got[i][len(got[i])-1]
	}
	return writeCSV(want), nil
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
