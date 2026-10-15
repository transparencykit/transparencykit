package report

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"sort"
	"strings"
)

// JSON renders v as indented JSON with a trailing newline.
func JSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var funcs = template.FuncMap{
	"human": func(s string) string {
		if l, ok := labels[s]; ok {
			return l
		}
		s = strings.ReplaceAll(s, "_", " ")
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	},
	"cat":  CategoryName,
	"join": func(xs []string) string { return strings.Join(xs, ", ") },
	"cell": func(c Cell) string { return fmt.Sprint(c) },
	"nz": func(c Cell) bool {
		if n, ok := c.(int); ok {
			return n != 0
		}
		return true
	},
	"hours": func(f *float64) string {
		if f == nil {
			return "n/a"
		}
		return fmt.Sprintf("%.1f h", *f)
	},
	"pct": func(f *float64) string {
		if f == nil {
			return "n/a"
		}
		return fmt.Sprintf("%.1f %%", *f)
	},
	"sorted": func(m map[string]Cell) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{k, v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
		return out
	},
	"short": func(h string) string {
		if len(h) > 23 {
			return h[:23] + "…"
		}
		return h
	},
}

var labels = map[string]string{
	"art16_notice":                   "Art. 16 notice",
	"trusted_flagger":                "Trusted flagger",
	"non_content":                    "Non-content (metadata, subscriber info)",
	"not_notified_legal_prohibition": "Not notified (legal prohibition)",
	"not_notified_other":             "Not notified (other reason)",
	"notified_before":                "Notified before disclosure",
	"notified_after":                 "Notified after disclosure",
	"no_data":                        "No data held",
	"forwarded_to_customer":          "Forwarded to customer",
}

type kv struct {
	K string
	V Cell
}

const css = `
:root{--bg:#fbfaf7;--fg:#1c1c1a;--muted:#6b6a64;--line:#e4e1d8;--accent:#0f5c4d;--chip:#eef3f1;--warn:#8a4b00}
@media (prefers-color-scheme:dark){:root{--bg:#141413;--fg:#ecebe6;--muted:#a19f97;--line:#2e2d2a;--accent:#5cc2a8;--chip:#1d2a27;--warn:#e0a35a}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:980px;margin:0 auto;padding:40px 16px 80px}
header{border-bottom:2px solid var(--fg);padding-bottom:18px;margin-bottom:28px}
h1{font:600 2rem/1.15 ui-serif,Georgia,serif;margin:0 0 6px}h2{font:600 1.3rem/1.2 ui-serif,Georgia,serif;margin:40px 0 12px;padding-top:14px;border-top:1px solid var(--line)}
h3{font-size:.95rem;margin:22px 0 8px;color:var(--muted);text-transform:uppercase;letter-spacing:.06em}
.meta{color:var(--muted);font-size:.9rem}.meta code{font-size:.8rem}
.kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:12px;margin:18px 0}
.kpi{border:1px solid var(--line);border-radius:8px;padding:14px}.kpi b{display:block;font:600 1.8rem/1.1 ui-serif,Georgia,serif}.kpi span{color:var(--muted);font-size:.85rem}
table{width:100%;border-collapse:collapse;font-size:.92rem;margin:8px 0 4px}th,td{text-align:left;padding:7px 10px;border-bottom:1px solid var(--line);vertical-align:top}
th{font-weight:600;color:var(--muted);font-size:.8rem;text-transform:uppercase;letter-spacing:.04em}td.n,th.n{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}
.wrap{overflow-x:auto}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(270px,1fr));gap:4px 32px}.grid h3{margin-top:18px}.chip{display:inline-block;background:var(--chip);color:var(--accent);border-radius:4px;padding:0 6px;font-size:.78rem;font-weight:600}
.third{color:var(--warn)}.note{color:var(--muted);font-size:.85rem}ul{padding-left:20px}a{color:var(--accent)}
footer{margin-top:48px;padding-top:16px;border-top:1px solid var(--line);color:var(--muted);font-size:.82rem}
`

const reportTpl = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Provider.Name}} · Transparency report {{.Period.Label}}</title><style>{{css}}</style></head><body><main>
<header><div class="meta">Transparency report · version {{.Version}}</div>
<h1>{{.Provider.Name}} — {{.Period.Start}} to {{.Period.End}}</h1>
<div class="meta">Generated {{.GeneratedAt}} · Ledger head #{{.Ledger.Seq}} <code title="{{.Ledger.Hash}}">{{short .Ledger.Hash}}</code> · <a href="manifest.json">signed manifest</a> · <a href="report.json">JSON</a></div></header>

<div class="kpis">
<div class="kpi"><b>{{.Government.Total}}</b><span>government &amp; authority requests</span></div>
<div class="kpi"><b>{{cell .Government.ThirdCountry.Total}}</b><span>from third countries (outside {{join .HomeJurisdictions}})</span></div>
<div class="kpi"><b>{{pct .Government.DisclosureRatePct}}</b><span>requests resulting in disclosure or action</span></div>
<div class="kpi"><b>{{.Abuse.Total}}</b><span>abuse notices &amp; own-initiative measures</span></div>
</div>

<h2>Government and authority requests</h2>
<p class="note">Counts between 1 and {{minus .MinCount}} are shown as a range to protect customer confidentiality. Zero is always exact.</p>
{{if .Government.ByCountry}}<div class="wrap"><table><tr><th>Country</th><th class="n">Requests</th><th class="n">Disclosed / actioned</th><th class="n">Rejected</th><th class="n">Other</th></tr>
{{range .Government.ByCountry}}<tr><td>{{.Country}}{{if .ThirdCountry}} <span class="chip third">third country</span>{{end}}</td><td class="n">{{cell .Total}}</td><td class="n">{{cell .Disclosed}}</td><td class="n">{{cell .Rejected}}</td><td class="n">{{cell .Other}}</td></tr>{{end}}
</table></div>{{else}}<p>No requests received in this period.</p>{{end}}

<div class="kpis">
<div class="kpi"><b>{{.Government.AccountsSpecified}}</b><span>accounts specified</span></div>
<div class="kpi"><b>{{hours .Government.MedianHoursToRespond}}</b><span>median time to respond</span></div>
</div>
<div class="grid"><section><h3>By outcome</h3><table>{{range sorted .Government.ByOutcome}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section><section><h3>By authority type</h3><table>{{range sorted .Government.ByAuthorityType}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section><section><h3>By request kind</h3><table>{{range sorted .Government.ByKind}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section><section><h3>Data category (content vs non-content)</h3><table>{{range sorted .Government.ByDataCategory}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section><section><h3>Customer notification</h3><table>{{range sorted .Government.CustomerNotification}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section></div>

<h2>Third-country requests</h2>
<p>Requests from authorities outside {{join .HomeJurisdictions}}, e.g. under the US CLOUD Act. Under Data Act Art. 32(5) the customer is informed before compliance, except for law-enforcement confidentiality.</p>
<div class="grid"><section><h3>Outcome (third-country)</h3><table>{{range sorted .Government.ThirdCountry.ByOutcome}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section><section><h3>Customer notification (third-country)</h3><table>{{range sorted .Government.ThirdCountry.CustomerNotification}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section></div>

<h2>Abuse notices and own-initiative measures</h2>
<div class="kpis"><div class="kpi"><b>{{hours .Abuse.MedianHoursToAction}}</b><span>median time to action</span></div></div>
<div class="grid"><section><h3>By source</h3><table>{{range sorted .Abuse.BySource}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section><section><h3>By category (non-zero)</h3><table>{{range sorted .Abuse.ByCategory}}{{if nz .V}}<tr><td>{{cat .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}{{end}}</table></section><section><h3>By action</h3><table>{{range sorted .Abuse.ByAction}}<tr><td>{{human .K}}</td><td class="n">{{cell .V}}</td></tr>{{end}}</table></section></div>
<p class="note">DSA Art. 15 figures (exact, template-aligned CSV): <a href="dsa/tab1_identification.csv">identification</a> · <a href="dsa/tab3_member_state_orders.csv">Member State orders</a> · <a href="dsa/tab4_notices.csv">notices</a> · <a href="dsa/tab5_own_initiative_illegal.csv">own initiative (illegal)</a> · <a href="dsa/tab6_own_initiative_TC.csv">own initiative (T&amp;C)</a> · <a href="dsa/README.txt">notes</a></p>

<h2>Subprocessors</h2>
{{if .Subprocessors.Current}}<div class="wrap"><table><tr><th>Name</th><th>Purpose</th><th>Locations</th><th>Since</th></tr>
{{range .Subprocessors.Current}}<tr><td>{{.Name}}</td><td>{{.Purpose}}</td><td>{{join .Locations}}</td><td>{{.Since}}</td></tr>{{end}}</table></div>{{else}}<p>No subprocessors recorded.</p>{{end}}
{{if .Subprocessors.Changes}}<h3>Changes in this period</h3><table><tr><th>Effective</th><th>Change</th><th>Name</th><th>Notice given</th></tr>
{{range .Subprocessors.Changes}}<tr><td>{{.Effective}}</td><td><span class="chip">{{.Change}}</span></td><td>{{.Name}}</td><td>{{.NoticeGiven}}</td></tr>{{end}}</table>{{end}}
<p class="note"><a href="subprocessors.json">JSON</a> · <a href="subprocessors.atom">Atom feed</a></p>

<h2>Infrastructure jurisdiction</h2>
<p class="note">See also the <a href="data-act-art28.html">EU Data Act Art. 28 disclosure</a>.</p>
{{template "zones" .Infrastructure}}

<footer>Every number on this page is derived from a hash-chained, append-only ledger. All files of this publication are listed with their SHA-256 in <a href="manifest.json">manifest.json</a>, signed with Ed25519 (<a href="manifest.sig">manifest.sig</a>, key <code>{{keyid}}</code>). Verify with <code>vitrine verify</code>. Contact: {{.Provider.Contact}}.</footer>
</main></body></html>
{{define "zones"}}{{if .}}<div class="wrap"><table><tr><th>Zone</th><th>Location</th><th>Legal entity</th><th>Data-centre operator</th><th>Services</th></tr>
{{range .}}<tr><td><b>{{.Zone}}</b></td><td>{{.City}}{{if .City}}, {{end}}{{.Country}}</td><td>{{.LegalEntity}} ({{.LegalEntityCountry}})</td><td>{{.Operator}}{{if .OperatorCountry}} ({{.OperatorCountry}}){{end}}</td><td>{{join .Services}}</td></tr>{{end}}
</table></div>{{else}}<p>No infrastructure recorded.</p>{{end}}{{end}}`

const art28Tpl = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Provider.Name}} · EU Data Act Art. 28 disclosure</title><style>{{css}}</style></head><body><main>
<header><div class="meta">EU Data Act · Article 28 information</div>
<h1>{{.Provider.Name}}: international access and transfer</h1>
<div class="meta">Information as of {{.AsOf}} · generated {{.GeneratedAt}} · <a href="data-act-art28.json">JSON</a> · <a href="manifest.json">signed manifest</a></div></header>
<p>This page provides the information required by {{.LegalBasis}} for the data processing services of {{.Provider.LegalEntity}}{{if .Provider.Country}} ({{.Provider.Country}}){{end}}.</p>
<h2>(a) Jurisdiction of the ICT infrastructure</h2>
{{template "zones" .Zones}}
<h2>(b) Measures against international governmental access</h2>
{{with .Measures}}<p class="meta">Effective {{.Effective}}</p>{{if .Statement}}<p>{{.Statement}}</p>{{end}}
{{if .Technical}}<h3>Technical</h3><ul>{{range .Technical}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .Organisational}}<h3>Organisational</h3><ul>{{range .Organisational}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .Contractual}}<h3>Contractual</h3><ul>{{range .Contractual}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{else}}<p>No measures recorded.</p>{{end}}
<footer>Derived from ledger head #{{.Ledger.Seq}} <code>{{short .Ledger.Hash}}</code>. Contact: {{.Provider.Contact}}.</footer>
</main></body></html>
{{define "zones"}}{{if .}}<div class="wrap"><table><tr><th>Zone</th><th>Location</th><th>Legal entity</th><th>Data-centre operator</th><th>Services</th></tr>
{{range .}}<tr><td><b>{{.Zone}}</b></td><td>{{.City}}{{if .City}}, {{end}}{{.Country}}</td><td>{{.LegalEntity}} ({{.LegalEntityCountry}})</td><td>{{.Operator}}{{if .OperatorCountry}} ({{.OperatorCountry}}){{end}}</td><td>{{join .Services}}</td></tr>{{end}}
</table></div>{{else}}<p>No infrastructure recorded.</p>{{end}}{{end}}`

const rootTpl = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Provider}} · Transparency</title><style>{{css}}</style></head><body><main>
<header><div class="meta">Transparency</div><h1>{{.Provider}}</h1>
<div class="meta">Latest: <a href="latest/index.html">report</a> · <a href="latest/data-act-art28.html">Data Act Art. 28</a> · <a href="latest/subprocessors.atom">subprocessor feed</a> · <a href="vitrine.pub">public key</a></div></header>
<table><tr><th>Period</th><th>Version</th><th>Report</th><th>Manifest</th></tr>
{{range .Items}}<tr><td>{{.Label}}</td><td>v{{.Version}}</td><td><a href="{{.Path}}index.html">HTML</a> · <a href="{{.Path}}report.json">JSON</a></td><td><a href="{{.Path}}manifest.json">manifest</a></td></tr>{{end}}
</table><footer>Published with vitrine. Verify: <code>vitrine verify --pubkey vitrine.pub &lt;this URL&gt;</code></footer></main></body></html>`

func tpl(name, src, keyID string) *template.Template {
	f := template.FuncMap{"css": func() template.CSS { return template.CSS(css) },
		"minus": func(n int) int { return n - 1 },
		"keyid": func() string { return keyID }}
	for k, v := range funcs {
		f[k] = v
	}
	return template.Must(template.New(name).Funcs(f).Parse(src))
}

// HTML renders the report page. keyID is shown in the footer.
func HTML(r *Report, keyID string) ([]byte, error) {
	var buf bytes.Buffer
	if err := tpl("report", reportTpl, keyID).Execute(&buf, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Art28HTML renders the Data Act page.
func Art28HTML(a *Art28) ([]byte, error) {
	var buf bytes.Buffer
	if err := tpl("art28", art28Tpl, "").Execute(&buf, a); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// IndexItem is one publication in the root index.
type IndexItem struct {
	Label   string
	Version int
	Path    string // "reports/2026/v1/"
}

// RootHTML renders the root index.
func RootHTML(provider string, items []IndexItem) ([]byte, error) {
	var buf bytes.Buffer
	err := tpl("root", rootTpl, "").Execute(&buf, struct {
		Provider string
		Items    []IndexItem
	}{provider, items})
	return buf.Bytes(), err
}

// --- Atom ------------------------------------------------------------------

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Author  atomAuthor  `xml:"author"`
	Entries []atomEntry `xml:"entry"`
}
type atomAuthor struct {
	Name string `xml:"name"`
}
type atomEntry struct {
	Title   string `xml:"title"`
	ID      string `xml:"id"`
	Updated string `xml:"updated"`
	Summary string `xml:"summary"`
}

// SubAtom renders the subprocessor change feed (newest first).
func SubAtom(provider, idBase string, hist []SubChangeOut, updated string) ([]byte, error) {
	f := atomFeed{Title: provider + " — subprocessor changes", ID: idBase + ":subprocessors",
		Updated: updated, Author: atomAuthor{Name: provider}}
	for i := len(hist) - 1; i >= 0; i-- {
		h := hist[i]
		sum := fmt.Sprintf("%s: %s", h.Change, h.Name)
		if h.Purpose != "" {
			sum += " — " + h.Purpose
		}
		if len(h.Locations) > 0 {
			sum += " (" + strings.Join(h.Locations, ", ") + ")"
		}
		if h.NoticeGiven != "" {
			sum += "; customers notified " + h.NoticeGiven
		}
		f.Entries = append(f.Entries, atomEntry{
			Title: fmt.Sprintf("%s %s (effective %s)", strings.ToUpper(h.Change[:1])+h.Change[1:], h.Name, h.Effective),
			ID:    fmt.Sprintf("%s:subprocessor:%d", idBase, h.Seq), Updated: h.Effective + "T00:00:00Z", Summary: sum})
	}
	b, err := xml.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(b, '\n')...), nil
}
