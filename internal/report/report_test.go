package report

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exoscale/vitrine/internal/config"
	"github.com/exoscale/vitrine/internal/record"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "vitrine.json")
	os.WriteFile(p, []byte(config.Example), 0o644)
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParsePeriod(t *testing.T) {
	cases := map[string][2]string{
		"2026":                   {"2026-01-01", "2026-12-31"},
		"2026-H1":                {"2026-01-01", "2026-06-30"},
		"2026-H2":                {"2026-07-01", "2026-12-31"},
		"2024-Q1":                {"2024-01-01", "2024-03-31"},
		"2026-02-01..2026-02-28": {"2026-02-01", "2026-02-28"},
	}
	for in, want := range cases {
		p, err := ParsePeriod(in)
		if err != nil || p.Start != want[0] || p.End != want[1] {
			t.Errorf("%s: %+v %v", in, p, err)
		}
	}
	for _, bad := range []string{"26", "2026-H3", "2026-03-01..2026-02-01", "last year"} {
		if _, err := ParsePeriod(bad); err == nil {
			t.Errorf("%s: expected error", bad)
		}
	}
	p, _ := ParsePeriod("2026")
	if !p.ContainsTS("2026-12-31T23:59:59Z") || p.ContainsTS("2027-01-01T00:00:00Z") || p.ContainsTS("2025-12-31T23:59:59Z") {
		t.Error("period boundaries")
	}
	if !p.ContainsTS("2027-01-01T00:30:00+01:00") {
		t.Error("timestamps must be compared in UTC")
	}
}

func TestMedianAndBanding(t *testing.T) {
	if Durations(nil).Median() != nil {
		t.Fatal("empty median")
	}
	if m := (Durations{5, 1, 3, 100}).Median(); *m != 3 {
		t.Fatalf("lower median = %v", *m)
	}
	b := Bander{Min: 3}
	if b.C(0) != 0 || b.C(1) != "1-2" || b.C(2) != "1-2" || b.C(3) != 3 {
		t.Fatal("banding")
	}
	if (Bander{Min: 2}).C(1) != "1" || (Bander{Min: 1}).C(1) != 1 {
		t.Fatal("banding edge")
	}
}

func eff(seq int64, body any) record.Effective { return record.Effective{Seq: seq, Body: body} }

func g(country, outcome, notif string) *record.GovRequest {
	return &record.GovRequest{ReceivedAt: "2026-03-02T09:00:00Z", AuthorityCountry: country, AuthorityType: "law_enforcement",
		LegalBasis: "x", Kind: "user_data", DataCategory: "non_content", Accounts: 1, Items: 1, Outcome: outcome,
		AcknowledgedAt: "2026-03-02T11:00:00Z", CompletedAt: "2026-03-03T09:00:00Z", CustomerNotification: notif,
		Private: &record.Private{CaseRef: "SECRET-CASE-REF", Notes: "SECRET-NOTE"}}
}

func fixture() []record.Effective {
	var recs []record.Effective
	seq := int64(0)
	add := func(b any) { seq++; recs = append(recs, eff(seq, b)) }
	for i := 0; i < 5; i++ {
		add(g("CH", "disclosed_partial", "notified_before"))
	}
	add(g("CH", "rejected", "not_applicable"))
	add(g("DE", "disclosed_full", "notified_before"))
	add(g("US", "rejected", "notified_before")) // third country, single
	out := g("CH", "disclosed_full", "notified_before")
	out.ReceivedAt = "2025-12-31T23:00:00Z" // outside period
	add(out)
	o9 := g("DE", "actioned", "notified_after")
	o9.DSAOrder, o9.IllegalContentCategory, o9.Kind, o9.Items = "art9", "STATEMENT_CATEGORY_SCAMS_AND_FRAUD", "content_action", 4
	add(o9)
	o10 := g("FR", "disclosed_full", "not_notified_legal_prohibition")
	o10.DSAOrder = "art10"
	add(o10)
	for i := 0; i < 3; i++ {
		add(&record.AbuseNotice{ReceivedAt: "2026-05-01T10:00:00Z", Source: "art16_notice", Category: "STATEMENT_CATEGORY_SCAMS_AND_FRAUD",
			Basis: "law", Action: "forwarded_to_customer", ActionedAt: "2026-05-01T12:00:00Z", Items: 1})
	}
	add(&record.AbuseNotice{ReceivedAt: "2026-05-02T10:00:00Z", Source: "trusted_flagger", Category: "STATEMENT_CATEGORY_PROTECTION_OF_MINORS",
		Basis: "law", Action: "removed", ActionedAt: "2026-05-02T10:30:00Z", Items: 2, AutomatedDetection: true})
	add(&record.AbuseNotice{ReceivedAt: "2026-06-01T10:00:00Z", Source: "own_initiative", Category: "STATEMENT_CATEGORY_OTHER_VIOLATION_TC",
		Basis: "terms", Action: "account_suspended", ActionedAt: "2026-06-01T11:00:00Z", Items: 1, AutomatedDetection: true, AutomatedDecision: true})
	add(&record.SubprocessorChange{Effective: "2025-06-01", Change: "add", Name: "Mail AG", Purpose: "e-mail", Locations: []string{"CH"}})
	add(&record.SubprocessorChange{Effective: "2026-04-01", Change: "add", Name: "Support GmbH", Purpose: "support", Locations: []string{"DE"}, NoticeGiven: "2026-03-01"})
	add(&record.SubprocessorChange{Effective: "2026-09-01", Change: "update", Name: "Mail AG", Purpose: "e-mail and SMS", Locations: []string{"CH", "DE"}})
	add(&record.SubprocessorChange{Effective: "2027-02-01", Change: "remove", Name: "Mail AG"}) // after period
	add(&record.Infrastructure{Effective: "2025-01-01", Zone: "ch-gva-2", Country: "CH", City: "Geneva", LegalEntity: "Ex SA", LegalEntityCountry: "CH", Status: "active",
		Private: &record.Private{Notes: "SECRET-INFRA"}})
	add(&record.Infrastructure{Effective: "2025-01-01", Zone: "de-fra-1", Country: "DE", LegalEntity: "Ex SA", LegalEntityCountry: "CH", Status: "active"})
	add(&record.Infrastructure{Effective: "2026-08-01", Zone: "de-fra-1", Country: "DE", LegalEntity: "Ex SA", LegalEntityCountry: "CH", Status: "retired"})
	add(&record.Measures{Effective: "2025-01-01", Statement: "old"})
	add(&record.Measures{Effective: "2026-01-15", Statement: "current", Technical: []string{"t"}})
	add(&record.Measures{Effective: "2027-01-15", Statement: "future"})
	return recs
}

func TestAggregateExact(t *testing.T) {
	cfg := testConfig(t)
	p, _ := ParsePeriod("2026")
	x := Aggregate(fixture(), p, cfg.Home())
	gv := x.Gov
	if gv.Total != 10 || gv.ByCountry["CH"] != 6 || gv.ThirdTotal != 1 || gv.ThirdByCountry["US"] != 1 {
		t.Fatalf("gov exact: total=%d ch=%d third=%d", gv.Total, gv.ByCountry["CH"], gv.ThirdTotal)
	}
	if gv.ByNotification["not_notified_legal_prohibition"] != 1 || gv.ByOutcome["rejected"] != 2 {
		t.Fatal("notification/outcome counts")
	}
	if c := x.Orders["STATEMENT_CATEGORY_SCAMS_AND_FRAUD"]["DE"]; c == nil || c.Act != 1 || c.ActItems != 4 || *c.ActAck.Median() != 2 || *c.ActEffect.Median() != 24 {
		t.Fatalf("art9 order cell: %+v", c)
	}
	if c := x.Orders[""]["FR"]; c == nil || c.Info != 1 {
		t.Fatal("art10 order cell")
	}
	if n := x.Notices["STATEMENT_CATEGORY_PROTECTION_OF_MINORS"]; n.NoticesTF != 1 || n.ItemsTF != 2 || n.ActionsLawTF != 1 || n.AutomatedProcessed != 1 {
		t.Fatalf("notice TF: %+v", n)
	}
	if o := x.OwnTerms["STATEMENT_CATEGORY_OTHER_VIOLATION_TC"]; o == nil || o.AutoDecided != 1 || o.ByAction["account_suspended"] != 1 {
		t.Fatal("own initiative")
	}
	if len(x.Subprocessors) != 2 || x.Subprocessors[0].Name != "Mail AG" || x.Subprocessors[0].Since != "2025-06-01" ||
		len(x.Subprocessors[0].Locations) != 2 {
		t.Fatalf("subprocessor fold: %+v", x.Subprocessors)
	}
	if len(x.SubChanges) != 2 || len(x.SubHistory) != 3 {
		t.Fatalf("sub changes %d history %d", len(x.SubChanges), len(x.SubHistory))
	}
	if len(x.Infra) != 1 || x.Infra[0].Zone != "ch-gva-2" {
		t.Fatalf("infra fold (retired zone must drop): %+v", x.Infra)
	}
	if x.Measures == nil || x.Measures.Statement != "current" {
		t.Fatal("measures fold")
	}
	// H1 sees de-fra-1 still active.
	h1, _ := ParsePeriod("2026-H1")
	if xh := Aggregate(fixture(), h1, cfg.Home()); len(xh.Infra) != 2 {
		t.Fatal("de-fra-1 should be active in H1")
	}
}

func TestPublicReportBandingAndPrivacy(t *testing.T) {
	cfg := testConfig(t)
	p, _ := ParsePeriod("2026")
	x := Aggregate(fixture(), p, cfg.Home())
	r := Build(x, cfg, p, 1, "2027-01-10T00:00:00Z", LedgerRef{Seq: 1, Hash: "sha256:x"})
	if r.Government.Total != 10 {
		t.Fatal("totals must be exact")
	}
	if r.Government.ThirdCountry.Total != "1-2" {
		t.Fatalf("third-country single request must be banded, got %v", r.Government.ThirdCountry.Total)
	}
	if r.Government.ByAuthorityType["intelligence"] != 0 {
		t.Fatal("zeros must be exact and present")
	}
	for _, row := range r.Government.ByCountry {
		if row.Country == "US" && (row.Total != "1-2" || !row.ThirdCountry) {
			t.Fatalf("US row: %+v", row)
		}
		if row.Country == "CH" && row.Total != 6 {
			t.Fatalf("CH row: %+v", row)
		}
	}
	if r.Government.DisclosureRatePct == nil || *r.Government.DisclosureRatePct != 80 {
		t.Fatalf("disclosure rate = %v", r.Government.DisclosureRatePct)
	}
	js, _ := JSON(r)
	h, err := HTML(r, "ed25519:abc")
	if err != nil {
		t.Fatal(err)
	}
	art := BuildArt28(x, cfg, p, "2027-01-10T00:00:00Z", r.Ledger)
	ah, err := Art28HTML(art)
	if err != nil {
		t.Fatal(err)
	}
	aj, _ := JSON(art)
	for name, b := range map[string][]byte{"report.json": js, "index.html": h, "art28.html": ah, "art28.json": aj} {
		for _, secret := range []string{"SECRET-CASE-REF", "SECRET-NOTE", "SECRET-INFRA", "case_ref", "private"} {
			if strings.Contains(string(b), secret) {
				t.Errorf("%s leaks %q", name, secret)
			}
		}
	}
	if !strings.Contains(string(h), "third country") || !strings.Contains(string(ah), "Article 28") {
		t.Error("expected content missing from HTML")
	}
	if !strings.Contains(string(js), `"total": "1-2"`) {
		t.Error("banded value not in JSON")
	}
}

func readCSV(t *testing.T, b []byte) [][]string {
	r := csv.NewReader(strings.NewReader(string(b)))
	r.Comma = ';'
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestDSAFilesExact(t *testing.T) {
	cfg := testConfig(t)
	p, _ := ParsePeriod("2026")
	x := Aggregate(fixture(), p, cfg.Home())
	files := DSAFiles(x, cfg, p, 1, "2027-01-10T00:00:00Z")
	for _, n := range []string{"tab1_identification.csv", "tab2_categories_names.csv", "tab3_member_state_orders.csv",
		"tab4_notices.csv", "tab5_own_initiative_illegal.csv", "tab6_own_initiative_TC.csv", "tab11_qualitative.csv"} {
		if _, ok := files[n]; !ok {
			t.Fatalf("missing %s", n)
		}
	}
	t3 := readCSV(t, files["tab3_member_state_orders.csv"])
	if len(t3[0]) != 6+7+7 {
		t.Fatalf("tab3 has %d columns", len(t3[0]))
	}
	total := t3[1]
	if total[3] != "TOTAL" || total[2] != "2026-01-01/2026-12-31" || total[6] != "1" || total[7] != "4" || total[10] != "1" {
		t.Fatalf("tab3 TOTAL row: %v", total)
	}
	found := false
	for _, row := range t3 {
		if row[3] == "STATEMENT_CATEGORY_SCAMS_AND_FRAUD" && row[5] == "DE" {
			found = row[6] == "1" && row[8] == "2.0" && row[9] == "24.0"
		}
	}
	if !found {
		t.Fatal("tab3 DE scams row missing or wrong")
	}
	t4 := readCSV(t, files["tab4_notices.csv"])
	if len(t4[0]) != 6+11+11 {
		t.Fatalf("tab4 has %d columns (published template has 28)", len(t4[0]))
	}
	// DSA data is never banded: a single trusted-flagger notice appears as 1.
	if t4[1][6] != "4" || t4[1][7] != "1" || t4[1][9] != "2" {
		t.Fatalf("tab4 TOTAL: %v", t4[1])
	}
	if len(t4) != 2+len(record.Categories) {
		t.Fatalf("tab4 rows %d", len(t4))
	}
	t6 := readCSV(t, files["tab6_own_initiative_TC.csv"])
	if t6[1][6] != "1" || t6[1][9] != "1" || t6[1][13] != "1" {
		t.Fatalf("tab6 TOTAL: %v", t6[1])
	}
	if strings.Contains(string(files["tab4_notices.csv"]), "1-2") {
		t.Fatal("DSA CSV must not be banded")
	}
}

func TestAtomFeed(t *testing.T) {
	cfg := testConfig(t)
	p, _ := ParsePeriod("2026")
	x := Aggregate(fixture(), p, cfg.Home())
	b, err := SubAtom("Ex SA", "urn:vitrine:ex-sa", x.SubHistory, "2027-01-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `xmlns="http://www.w3.org/2005/Atom"`) || strings.Count(s, "<entry>") != 3 {
		t.Fatalf("atom: %s", s)
	}
	if strings.Index(s, "Update Mail AG") > strings.Index(s, "Add Support GmbH") {
		t.Error("feed must be newest first")
	}
	if slug("Exoscale (Akenes SA)") != "exoscale-akenes-sa" {
		t.Error(slug("Exoscale (Akenes SA)"))
	}
}
