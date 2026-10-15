// Package cli implements the transparencykit command line.
package cli

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/transparencykit/transparencykit/internal/config"
	"github.com/transparencykit/transparencykit/internal/ledger"
	"github.com/transparencykit/transparencykit/internal/manifest"
	"github.com/transparencykit/transparencykit/internal/record"
	"github.com/transparencykit/transparencykit/internal/report"
	"github.com/transparencykit/transparencykit/internal/store"
	"github.com/transparencykit/transparencykit/internal/verify"
)

// Version is set at build time via -ldflags "-X …/cli.Version=…".
var Version = "dev"

// Exit codes.
const (
	ExitOK    = 0
	ExitFail  = 1 // verification failure
	ExitUsage = 2 // usage or I/O error
)

// App holds I/O and clock (overridable in tests).
type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
	Getenv func(string) string
	// Prog is the invoked program name ("transparencykit" or "tkit").
	Prog string
}

// usageText renders the usage with the invoked program name.
func (a *App) usageText() string {
	p := a.Prog
	if p == "" {
		p = "transparencykit"
	}
	return usageFor(p)
}

// Main runs the CLI and returns an exit code.
func Main(args []string) int {
	a := &App{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Now: time.Now, Getenv: os.Getenv,
		Prog: filepath.Base(os.Args[0])}
	return a.Run(args)
}

// commands is the usage table: synopsis (without program name) and summary.
var commands = [][2]string{
	{"init [--dir .]", "Write transparencykit.json and a signing key pair."},
	{"keygen --out transparencykit.key", "Generate an Ed25519 key pair."},
	{"record template <type>", "Print a JSON skeleton for a record type."},
	{"record add <type> [-f file|-] [--amends N] [--by who]", "Validate and append a record."},
	{"record validate <type> [-f file|-] [--amends N]", "Validate a record without appending it."},
	{"record import -f records.json [--from N] [--check]", "Bulk append; the whole batch is validated first."},
	{"record retract <seq> --reason \"…\"", "Exclude an earlier record (it stays in the ledger)."},
	{"record list [--type t] [--effective] [--json]", "List records (operator view, includes private fields)."},
	{"record show <seq>", "Show one record."},
	{"ledger verify [--expect-head sha256:…]", "Verify the whole hash chain."},
	{"publish --period 2026|2026-H1|2026-Q3|A..B [--as-of date] [--out dir] [--dry-run]", "Aggregate, render, sign and upload."},
	{"publish --period … --draft dir", "Render a draft for review; nothing is signed or uploaded."},
	{"publish --from-draft dir", "Sign and upload a reviewed draft."},
	{"verify --pubkey key.pub [--pubkey old.pub] <https://base|store-url> [reports/<label>/v<N>] [--chain] [--checkpoint file]", "Verify a publication."},
	{"serve [--addr :8080] [--store url]", "Serve the public store read-only over HTTP."},
	{"version", "Print the version."},
}

const usageFooter = `
Record types: gov_request, abuse_notice, subprocessor_change, infrastructure, measures, retraction
Global flag:  --config path (default ./transparencykit.json or $TRANSPARENCYKIT_CONFIG)
Credentials:  EXOSCALE_API_KEY / EXOSCALE_API_SECRET (or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY)
Docs:         https://transparencykit.github.io/transparencykit/
`

func usageFor(prog string) string {
	var b strings.Builder
	b.WriteString("transparencykit (short name: tkit): tamper-evident transparency ledger and signed reports\n\nUsage:\n")
	for _, c := range commands {
		b.WriteString("  " + prog + " " + c[0] + "\n      " + c[1] + "\n")
	}
	b.WriteString(usageFooter)
	return b.String()
}

// Run dispatches a command.
func (a *App) Run(args []string) int {
	cfgPath := a.Getenv("TRANSPARENCYKIT_CONFIG")
	if cfgPath == "" {
		cfgPath = "transparencykit.json"
	}
	// Extract a leading or anywhere --config flag.
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--config" && i+1 < len(args):
			cfgPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--config="):
			cfgPath = strings.TrimPrefix(args[i], "--config=")
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) == 0 {
		fmt.Fprint(a.Stderr, a.usageText())
		return ExitUsage
	}
	ctx := context.Background()
	var err error
	code := ExitOK
	switch rest[0] {
	case "help", "-h", "--help":
		fmt.Fprint(a.Stdout, a.usageText())
		return ExitOK
	case "version":
		fmt.Fprintln(a.Stdout, "transparencykit", Version)
		return ExitOK
	case "init":
		err = a.cmdInit(rest[1:])
	case "keygen":
		err = a.cmdKeygen(rest[1:])
	case "record":
		err = a.cmdRecord(ctx, cfgPath, rest[1:])
	case "ledger":
		code, err = a.cmdLedger(ctx, cfgPath, rest[1:])
	case "publish":
		err = a.cmdPublish(ctx, cfgPath, rest[1:])
	case "verify":
		code, err = a.cmdVerify(ctx, rest[1:])
	case "serve":
		err = a.cmdServe(cfgPath, rest[1:])
	default:
		fmt.Fprintf(a.Stderr, "unknown command %q\n\n%s", rest[0], a.usageText())
		return ExitUsage
	}
	if err != nil {
		fmt.Fprintln(a.Stderr, "error:", err)
		if code == ExitOK {
			code = ExitUsage
		}
	}
	return code
}

func (a *App) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	return fs
}

// parse allows flags after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func (a *App) open(cfgPath string) (*config.Config, *ledger.Ledger, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, err
	}
	s, err := store.OpenRole(cfg.Resolve(cfg.Ledger), store.RoleLedger)
	if err != nil {
		return nil, nil, err
	}
	return cfg, &ledger.Ledger{S: s, Retention: cfg.LedgerRetention, Now: a.Now}, nil
}

// --- init / keygen -----------------------------------------------------------

func (a *App) cmdInit(args []string) error {
	fs := a.flags("init")
	dir := fs.String("dir", ".", "directory")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	cp := filepath.Join(*dir, "transparencykit.json")
	if _, err := os.Stat(cp); err == nil {
		return fmt.Errorf("%s already exists", cp)
	}
	if err := os.WriteFile(cp, []byte(config.Example), 0o644); err != nil {
		return err
	}
	kp := filepath.Join(*dir, "transparencykit.key")
	pub, err := manifest.GenerateKey(kp, filepath.Join(*dir, "transparencykit.pub"))
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, `Wrote %s, %s (keep secret) and transparencykit.pub (key %s).

Next steps for Exoscale SOS (see docs/OPERATIONS.md):
  1. Create the private ledger bucket WITH object lock (cannot be enabled later), e.g. with MinIO client:
       mc alias set sos https://sos-ch-gva-2.exo.io "$EXOSCALE_API_KEY" "$EXOSCALE_API_SECRET"
       mc mb --with-lock sos/<org>-transparencykit-ledger
       mc retention set --default GOVERNANCE 3650d sos/<org>-transparencykit-ledger
  2. Create the public bucket:  mc mb sos/<org>-transparency
  3. Edit transparencykit.json: "ledger": "sos://<org>-transparencykit-ledger/?zone=ch-gva-2",
                        "public": "sos://<org>-transparency/?zone=ch-gva-2"
  4. Publish transparencykit.pub out-of-band (website, contracts, DNS) so verifiers can trust it.
`, cp, kp, manifest.KeyID(pub))
	return nil
}

func (a *App) cmdKeygen(args []string) error {
	fs := a.flags("keygen")
	out := fs.String("out", "transparencykit.key", "private key path (public key written next to it as .pub)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if strings.HasSuffix(*out, ".pub") {
		return fmt.Errorf("--out names the private key file; %s ends in .pub (use e.g. --out %s.key)", *out, strings.TrimSuffix(*out, ".pub"))
	}
	pubPath := strings.TrimSuffix(*out, filepath.Ext(*out)) + ".pub"
	pub, err := manifest.GenerateKey(*out, pubPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "wrote %s and %s (%s)\n", *out, pubPath, manifest.KeyID(pub))
	return nil
}

// --- record ------------------------------------------------------------------

func (a *App) cmdRecord(ctx context.Context, cfgPath string, args []string) error {
	if len(args) == 0 {
		return errors.New("record: expected template|add|retract|list|show")
	}
	switch args[0] {
	case "template":
		if len(args) < 2 {
			return fmt.Errorf("record template <type> (types: %s)", strings.Join(record.Types, ", "))
		}
		t, err := record.Template(args[1])
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Fprintln(a.Stdout, string(b))
		return nil
	case "add":
		fs := a.flags("record add")
		file := fs.String("f", "-", "JSON body file ('-' = stdin)")
		amends := fs.Int64("amends", 0, "seq of the record this amends")
		by := fs.String("by", "", "operator identity (recorded_by)")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New("record add <type> [-f file]")
		}
		var raw []byte
		if *file == "-" {
			raw, err = io.ReadAll(a.Stdin)
		} else {
			raw, err = os.ReadFile(*file)
		}
		if err != nil {
			return err
		}
		_, l, err := a.open(cfgPath)
		if err != nil {
			return err
		}
		e, err := l.Append(ctx, pos[0], raw, *by, *amends)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "appended seq %d (%s) %s\n", e.Seq, e.Type, e.Hash)
		return nil
	case "validate":
		fs := a.flags("record validate")
		file := fs.String("f", "-", "JSON body file ('-' = stdin)")
		amends := fs.Int64("amends", 0, "seq of the record this amends")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New("record validate <type> [-f file]")
		}
		raw, err := a.readInput(*file)
		if err != nil {
			return err
		}
		_, l, err := a.open(cfgPath)
		if err != nil {
			return err
		}
		head, err := l.Check(ctx, []ledger.Item{{Type: pos[0], Body: raw, Amends: *amends}})
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "valid %s; would be appended as seq %d (nothing written)\n", pos[0], head+1)
		return nil
	case "import":
		fs := a.flags("record import")
		file := fs.String("f", "-", `JSON array of {"type","body","amends"?,"by"?} ('-' = stdin)`)
		from := fs.Int("from", 1, "start at this item (1-based), to resume an interrupted import")
		check := fs.Bool("check", false, "validate the batch and exit without appending")
		if _, err := parse(fs, args[1:]); err != nil {
			return err
		}
		raw, err := a.readInput(*file)
		if err != nil {
			return err
		}
		var items []ledger.Item
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("import: %w", err)
		}
		if *from < 1 || *from > len(items) {
			return fmt.Errorf("--from %d: the batch has %d items", *from, len(items))
		}
		todo := items[*from-1:]
		_, l, err := a.open(cfgPath)
		if err != nil {
			return err
		}
		// Validate everything, including amendment and retraction references
		// (to existing records or to earlier items), before writing anything.
		head, err := l.Check(ctx, todo)
		if err != nil {
			return fmt.Errorf("import: nothing appended: %w (items counted from --from %d)", err, *from)
		}
		if *check {
			fmt.Fprintf(a.Stdout, "batch valid: %d records would be appended as seq %d..%d (nothing written)\n",
				len(todo), head+1, head+int64(len(todo)))
			return nil
		}
		var first, last int64
		for i, it := range todo {
			e, err := l.Append(ctx, it.Type, it.Body, it.By, it.Amends)
			if err != nil {
				n := *from + i
				if i == 0 {
					return fmt.Errorf("import item %d: %w (nothing appended)", n, err)
				}
				return fmt.Errorf("import item %d: %w\nitems %d..%d were appended (seq %d..%d); after fixing the cause, resume with --from %d",
					n, err, *from, n-1, first, last, n)
			}
			if first == 0 {
				first = e.Seq
			}
			last = e.Seq
		}
		fmt.Fprintf(a.Stdout, "imported %d records (seq %d..%d)\n", len(todo), first, last)
		return nil
	case "retract":
		fs := a.flags("record retract")
		reason := fs.String("reason", "", "reason (required)")
		by := fs.String("by", "", "operator identity")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 || *reason == "" {
			return errors.New(`record retract <seq> --reason "…"`)
		}
		seq, err := strconv.ParseInt(pos[0], 10, 64)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(record.Retraction{Seq: seq, Reason: *reason})
		_, l, err := a.open(cfgPath)
		if err != nil {
			return err
		}
		e, err := l.Append(ctx, record.TypeRetraction, raw, *by, 0)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "appended seq %d (retraction of %d) %s\n", e.Seq, seq, e.Hash)
		return nil
	case "list":
		fs := a.flags("record list")
		typ := fs.String("type", "", "filter by type")
		asJSON := fs.Bool("json", false, "JSON lines output")
		effective := fs.Bool("effective", false, "one row per record as it counts in reports: amendments applied, retractions marked")
		if _, err := parse(fs, args[1:]); err != nil {
			return err
		}
		_, l, err := a.open(cfgPath)
		if err != nil {
			return err
		}
		es, err := l.LoadAll(ctx)
		if err != nil {
			return err
		}
		if *effective {
			return a.listEffective(es, *typ, *asJSON)
		}
		tw := tabwriter.NewWriter(a.Stdout, 0, 2, 2, ' ', 0)
		if !*asJSON {
			fmt.Fprintln(tw, "SEQ\tTYPE\tRECORDED\tAMENDS\tSUMMARY")
		}
		for _, e := range es {
			if *typ != "" && e.Type != *typ {
				continue
			}
			if *asJSON {
				b, _ := e.Encode()
				fmt.Fprintln(a.Stdout, string(b))
				continue
			}
			am := ""
			if e.Amends != 0 {
				am = strconv.FormatInt(e.Amends, 10)
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", e.Seq, e.Type, e.RecordedAt, am, summary(e))
		}
		return tw.Flush()
	case "show":
		if len(args) < 2 {
			return errors.New("record show <seq>")
		}
		seq, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return err
		}
		_, l, err := a.open(cfgPath)
		if err != nil {
			return err
		}
		b, err := l.S.Get(ctx, record.Key(seq))
		if err != nil {
			return fmt.Errorf("seq %d: %w", seq, err)
		}
		var v any
		_ = json.Unmarshal(b, &v)
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Fprintln(a.Stdout, string(out))
		return nil
	}
	return fmt.Errorf("record: unknown subcommand %q", args[0])
}

func (a *App) readInput(file string) ([]byte, error) {
	if file == "-" {
		return io.ReadAll(a.Stdin)
	}
	return os.ReadFile(file)
}

// listEffective prints each original record with its latest amendment
// applied, and marks amended and retracted records.
func (a *App) listEffective(es []*record.Entry, typ string, asJSON bool) error {
	latest := map[int64]*record.Entry{}
	amendedBy := map[int64][]string{}
	retractedBy := map[int64]string{}
	var order []int64
	for _, e := range es {
		switch {
		case e.Type == record.TypeRetraction:
			if b, err := e.Decoded(); err == nil {
				retractedBy[b.(*record.Retraction).Seq] = strconv.FormatInt(e.Seq, 10)
			}
		case e.Amends != 0:
			latest[e.Amends] = e
			amendedBy[e.Amends] = append(amendedBy[e.Amends], strconv.FormatInt(e.Seq, 10))
		default:
			latest[e.Seq] = e
			order = append(order, e.Seq)
		}
	}
	tw := tabwriter.NewWriter(a.Stdout, 0, 2, 2, ' ', 0)
	if !asJSON {
		fmt.Fprintln(tw, "SEQ\tTYPE\tSTATUS\tSUMMARY (latest version)")
	}
	for _, seq := range order {
		e := latest[seq]
		if typ != "" && e.Type != typ {
			continue
		}
		status := "original"
		if am := amendedBy[seq]; len(am) > 0 {
			status = "amended by #" + strings.Join(am, ", #")
		}
		if r, ok := retractedBy[seq]; ok {
			status = "RETRACTED by #" + r + " (not counted)"
		}
		if asJSON {
			b, _ := json.Marshal(map[string]any{"seq": seq, "type": e.Type, "status": status, "effective_seq": e.Seq,
				"retracted": retractedBy[seq] != "", "body": e.Body})
			fmt.Fprintln(a.Stdout, string(b))
			continue
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", seq, e.Type, status, summary(e))
	}
	return tw.Flush()
}

func summary(e *record.Entry) string {
	b, err := e.Decoded()
	if err != nil {
		return "INVALID: " + err.Error()
	}
	switch t := b.(type) {
	case *record.GovRequest:
		return fmt.Sprintf("%s %s %s → %s", t.ReceivedAt[:10], t.AuthorityCountry, t.Kind, t.Outcome)
	case *record.AbuseNotice:
		return fmt.Sprintf("%s %s %s → %s", t.ReceivedAt[:10], t.Source, report.CategoryName(t.Category), t.Action)
	case *record.SubprocessorChange:
		return fmt.Sprintf("%s %s %s", t.Effective, t.Change, t.Name)
	case *record.Infrastructure:
		return fmt.Sprintf("%s %s %s (%s)", t.Effective, t.Zone, t.Country, t.Status)
	case *record.Measures:
		return fmt.Sprintf("%s %d measures", t.Effective, len(t.Technical)+len(t.Organisational)+len(t.Contractual))
	case *record.Retraction:
		return fmt.Sprintf("retracts %d: %s", t.Seq, t.Reason)
	}
	return ""
}

// --- ledger ------------------------------------------------------------------

func (a *App) cmdLedger(ctx context.Context, cfgPath string, args []string) (int, error) {
	if len(args) == 0 || args[0] != "verify" {
		return ExitUsage, errors.New("ledger verify [--expect-head sha256:…]")
	}
	fs := a.flags("ledger verify")
	expect := fs.String("expect-head", "", "assert the head hash (e.g. from a published manifest)")
	if _, err := parse(fs, args[1:]); err != nil {
		return ExitUsage, err
	}
	_, l, err := a.open(cfgPath)
	if err != nil {
		return ExitUsage, err
	}
	es, head, err := l.Verify(ctx)
	if err != nil {
		return ExitFail, fmt.Errorf("ledger INVALID: %w", err)
	}
	if *expect != "" {
		ok := false
		for _, e := range es {
			if e.Hash == *expect {
				ok = true
				fmt.Fprintf(a.Stdout, "expected head %s found at seq %d\n", *expect, e.Seq)
			}
		}
		if !ok {
			return ExitFail, fmt.Errorf("expected head %s is not in the ledger", *expect)
		}
	}
	counts := map[string]int{}
	for _, e := range es {
		counts[e.Type]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	fmt.Fprintf(a.Stdout, "ledger OK: %d entries, head seq %d %s\n", len(es), head.Seq, head.Hash)
	if len(parts) > 0 {
		fmt.Fprintln(a.Stdout, "  "+strings.Join(parts, " "))
	}
	return ExitOK, nil
}

// --- publish -----------------------------------------------------------------

func (a *App) cmdPublish(ctx context.Context, cfgPath string, args []string) error {
	fs := a.flags("publish")
	period := fs.String("period", "", "2026 | 2026-H1 | 2026-Q3 | 2026-01-01..2026-03-31")
	asOf := fs.String("as-of", "", "date (YYYY-MM-DD) of current infrastructure, measures and subprocessors (default: today, capped at the period end)")
	out := fs.String("out", "", "publish to this local directory instead of the configured public store")
	dry := fs.Bool("dry-run", false, "compute and print, write nothing")
	draft := fs.String("draft", "", "render into this directory for review; nothing is signed or uploaded")
	fromDraft := fs.String("from-draft", "", "sign and upload the reviewed draft in this directory")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	var p report.Period
	var err error
	switch {
	case *fromDraft != "":
		if *period != "" || *asOf != "" || *draft != "" {
			return errors.New("--from-draft takes the period and as-of date from the draft")
		}
	case *period == "":
		return errors.New("publish --period is required")
	default:
		if p, err = report.ParsePeriod(*period); err != nil {
			return err
		}
	}
	var asOfT time.Time
	if *asOf != "" {
		if asOfT, err = time.Parse("2006-01-02", *asOf); err != nil {
			return fmt.Errorf("--as-of: %w", err)
		}
	}
	cfg, l, err := a.open(cfgPath)
	if err != nil {
		return err
	}
	pubURL := cfg.Resolve(cfg.Public)
	if *out != "" {
		abs, _ := filepath.Abs(*out)
		pubURL = "file://" + abs
	}
	if pubURL == "" {
		return errors.New("no public store: set \"public\" in transparencykit.json or use --out")
	}
	ps, err := store.OpenRole(pubURL, store.RolePublic)
	if err != nil {
		return err
	}
	if cfg.SigningKey == "" {
		return errors.New("signing_key not set in config")
	}
	key, err := manifest.LoadPrivate(cfg.Resolve(cfg.SigningKey))
	if err != nil {
		return err
	}
	qual, err := loadQualitative(cfg)
	if err != nil {
		return err
	}
	res, err := report.Publish(ctx, report.PublishOptions{Config: cfg, Ledger: l, Public: ps, Key: key, Period: p,
		Now: a.Now(), AsOf: asOfT, Qualitative: qual, DryRun: *dry, DraftDir: *draft, FromDraft: *fromDraft,
		Verbose: func(s string) { fmt.Fprintln(a.Stderr, s) }})
	if err != nil {
		return err
	}
	g := res.Report.Government
	withheld := func() {
		for _, w := range res.Report.Withheld {
			fmt.Fprintf(a.Stdout, "  withheld %s: would reveal %s\n", w.Section, strings.Join(w.Cells, ", "))
		}
	}
	if res.DraftDir != "" {
		fmt.Fprintf(a.Stdout, "draft %s v%d written to %s (nothing signed or uploaded)\n  review %s\n  edit only %s if needed, then: publish --from-draft %s\n",
			res.Label, res.Version, res.DraftDir, filepath.Join(res.DraftDir, "index.html"), report.QualitativeFile, res.DraftDir)
		withheld()
		return nil
	}
	verb := "published"
	if res.DryRun {
		verb = "dry run (nothing written)"
	}
	fmt.Fprintf(a.Stdout, "%s %s%s → %s (publication #%d)\n  %d files, manifest sha256 %s\n  ledger head #%d %s\n  government requests %d (third-country %v), abuse notices %d\n",
		verb, ps.String()+"/", res.Prefix, res.Label, res.Sequence, res.Files, res.ManifestSHA256, res.Ledger.Seq, res.Ledger.Hash,
		g.Total, g.ThirdCountry.Total, res.Report.Abuse.Total)
	withheld()
	if !res.DryRun && cfg.PublicBaseURL != "" {
		base := strings.TrimRight(cfg.PublicBaseURL, "/")
		fmt.Fprintf(a.Stdout, "  report:   %s/%sindex.html\n  data act: %s/latest/data-act-art28.html\n", base, res.Prefix, base)
	}
	return nil
}

func loadQualitative(cfg *config.Config) (map[string]string, error) {
	if cfg.DSAQualitative == "" {
		return nil, nil
	}
	b, err := os.ReadFile(cfg.Resolve(cfg.DSAQualitative))
	if err != nil {
		return nil, fmt.Errorf("dsa_qualitative: %w", err)
	}
	var qual map[string]string
	if err := json.Unmarshal(b, &qual); err != nil {
		return nil, fmt.Errorf("dsa_qualitative: %w", err)
	}
	for k := range qual {
		known := false
		for _, q := range report.QualitativeKeys {
			known = known || q == k
		}
		if !known {
			return nil, fmt.Errorf("dsa_qualitative: unknown key %q (expected %s)", k, strings.Join(report.QualitativeKeys, ", "))
		}
	}
	return qual, nil
}

// --- verify ------------------------------------------------------------------

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func (a *App) cmdVerify(ctx context.Context, args []string) (int, error) {
	fs := a.flags("verify")
	var pubPaths multi
	fs.Var(&pubPaths, "pubkey", "trusted Ed25519 public key (PEM), obtained out-of-band; repeat for retired keys")
	chain := fs.Bool("chain", false, "verify the whole publication history from the newest publication")
	cpPath := fs.String("checkpoint", "", "file remembering publications seen in earlier runs (created if missing; implies --chain)")
	pos, err := parse(fs, args)
	if err != nil {
		return ExitUsage, err
	}
	if len(pos) < 1 || len(pos) > 2 {
		return ExitUsage, errors.New("verify --pubkey key.pub <base-url|store-url> [reports/<label>/v<N>]")
	}
	var f verify.Fetcher
	src := pos[0]
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		f = verify.HTTP{Base: src}
	} else {
		s, err := store.OpenRole(src, store.RolePublic)
		if err != nil {
			return ExitUsage, err
		}
		f = verify.Store{S: s}
	}
	var keys []ed25519.PublicKey
	if len(pubPaths) == 0 {
		fmt.Fprintln(a.Stderr, "WARNING: no --pubkey given; using transparencykit.pub served next to the reports. This proves integrity, not authenticity.")
		pb, err := f.Fetch(ctx, "transparencykit.pub")
		if err != nil {
			return ExitUsage, fmt.Errorf("public key: %w", err)
		}
		if keys, err = manifest.ParsePublicAll(pb); err != nil {
			return ExitUsage, err
		}
	}
	for _, p := range pubPaths {
		pb, err := os.ReadFile(p)
		if err != nil {
			return ExitUsage, fmt.Errorf("public key: %w", err)
		}
		ks, err := manifest.ParsePublicAll(pb)
		if err != nil {
			return ExitUsage, fmt.Errorf("%s: %w", p, err)
		}
		keys = append(keys, ks...)
	}
	target := ""
	if len(pos) == 2 {
		target = pos[1]
	}
	opts := verify.Options{Keys: keys, Chain: *chain || *cpPath != "", Log: func(s string) { fmt.Fprintln(a.Stdout, s) }}
	if *cpPath != "" {
		opts.Checkpoint = &verify.Checkpoint{}
		if b, err := os.ReadFile(*cpPath); err == nil {
			if err := json.Unmarshal(b, opts.Checkpoint); err != nil {
				return ExitUsage, fmt.Errorf("checkpoint %s: %w", *cpPath, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return ExitUsage, err
		}
	}
	checked, err := verify.Run(ctx, f, target, opts)
	if err != nil {
		if verify.IsFailure(err) {
			return ExitFail, fmt.Errorf("VERIFICATION FAILED: %w", err)
		}
		return ExitUsage, err
	}
	if cp := opts.Checkpoint; cp != nil {
		b, _ := json.MarshalIndent(cp, "", "  ")
		if err := os.WriteFile(*cpPath, append(b, '\n'), 0o644); err != nil {
			return ExitUsage, fmt.Errorf("checkpoint: %w", err)
		}
		fmt.Fprintf(a.Stdout, "checkpoint %s: %d publication(s) remembered\n", *cpPath, len(cp.Entries))
	}
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, manifest.KeyID(k))
	}
	fmt.Fprintf(a.Stdout, "verified %d publication(s) with trusted key(s) %s\n", len(checked), strings.Join(ids, ", "))
	return ExitOK, nil
}

// --- serve -------------------------------------------------------------------

func (a *App) cmdServe(cfgPath string, args []string) error {
	fs := a.flags("serve")
	addr := fs.String("addr", ":8080", "listen address")
	src := fs.String("store", "", "public store URL (default: config \"public\")")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	u := *src
	if u == "" {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		u = cfg.Resolve(cfg.Public)
	}
	s, err := store.OpenRole(u, store.RolePublic)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stderr, "serving %s on %s (read-only)\n", s.String(), *addr)
	srv := &http.Server{Addr: *addr, Handler: Handler(s), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// Handler serves a public store read-only; directories map to index.html.
func Handler(s store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" || strings.HasSuffix(p, "/") {
			p += "index.html"
		}
		if strings.Contains(p, "..") {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		b, err := s.Get(r.Context(), p)
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "upstream error", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", report.ContentType(p))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			return
		}
		w.Write(b)
	})
}
