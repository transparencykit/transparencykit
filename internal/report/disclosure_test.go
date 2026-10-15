package report

// Regression tests for review findings S6 (derived metrics undo banding) and
// U3 (historical period end drives "current" disclosures).

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/transparencykit/transparencykit/internal/record"
)

func TestDerivable(t *testing.T) {
	cases := []struct {
		cells      map[string]int
		total, min int
		want       []string
	}{
		// One banded cell next to an exact total is always determined.
		{map[string]int{"a": 5, "b": 1}, 6, 3, []string{"b"}},
		// Two banded cells summing to 2 must both be 1.
		{map[string]int{"a": 5, "b": 1, "c": 1}, 7, 3, []string{"b", "c"}},
		// Two banded cells summing to 4 must both be 2.
		{map[string]int{"a": 5, "b": 2, "c": 2}, 9, 3, []string{"b", "c"}},
		// Sum 3 over two cells: (1,2) or (2,1), not determined.
		{map[string]int{"a": 5, "b": 1, "c": 2}, 8, 3, nil},
		// Cells that do not add up to the total publish no relation.
		{map[string]int{"a": 5, "b": 1}, 9, 3, nil},
		// Nothing banded.
		{map[string]int{"a": 5, "b": 0}, 5, 3, nil},
	}
	for i, c := range cases {
		if got := derivable(c.cells, c.total, c.min); !reflect.DeepEqual(got, c.want) {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func TestRateDoesNotUndoBanding(t *testing.T) {
	// The reviewer's case: 3 requests, 1 disclosed, 2 rejected, min_count 3.
	// Both outcome cells are "1-2"; an exact 33.3 % would reveal the split.
	cells := map[string]int{"disclosed_full": 1, "rejected": 2, "pending": 0}
	num := []string{"disclosed_full"}
	den := []string{"disclosed_full", "rejected"}
	if got := rateCell(cells, num, den, 3, 3); got != "33.3–66.7" {
		t.Fatalf("rate = %v", got)
	}
	// Exact inputs give an exact rate.
	if got := rateCell(map[string]int{"disclosed_full": 4, "rejected": 6}, num, den, 10, 3); got != 40.0 {
		t.Fatalf("exact rate = %v", got)
	}
	// Nothing decided: no rate.
	if got := rateCell(map[string]int{"pending": 4}, num, den, 4, 3); got != nil {
		t.Fatalf("empty rate = %v", got)
	}
}

func TestMedianSuppressedBelowMin(t *testing.T) {
	if medianCell(Durations{5, 7}, 3) != nil {
		t.Fatal("median over 2 observations must be suppressed")
	}
	if m := medianCell(Durations{5, 7, 9}, 3); m == nil || *m != 7 {
		t.Fatal("median over 3 observations")
	}
}

func TestBuildWithholdsDerivableBreakdowns(t *testing.T) {
	cfg := testConfig(t) // min_count 3
	p, _ := ParsePeriod("2026")
	var recs []record.Effective
	for i := 0; i < 5; i++ {
		recs = append(recs, eff(int64(i+1), g("CH", "rejected", "notified_before")))
	}
	recs = append(recs, eff(6, g("CH", "disclosed_full", "notified_before")))
	x := Aggregate(recs, p, cfg.Home())
	r := Build(x, cfg, p, 1, "2027-01-10T00:00:00Z", LedgerRef{})
	if r.Government.ByOutcome != nil {
		t.Fatalf("outcome breakdown must be withheld: %v", r.Government.ByOutcome)
	}
	found := false
	for _, w := range r.Withheld {
		if w.Section == "government_requests.by_outcome" && reflect.DeepEqual(w.Cells, []string{"disclosed_full"}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("withheld list: %+v", r.Withheld)
	}
	// 1 of 6 decided: an exact 16.7 % would reveal the withheld split.
	if r.Government.DisclosureRatePct != nil {
		t.Fatalf("rate leaks: %v", r.Government.DisclosureRatePct)
	}
	// The country row (total 6, rejected 5) would show disclosed as "1-2"
	// although only 1 fits.
	if row := r.Government.ByCountry[0]; row.Disclosed != Withheld0 {
		t.Fatalf("country row leaks: %+v", row)
	}
	h, err := HTML(r, "k")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(h), "Withheld to protect small counts") {
		t.Fatal("HTML must say the breakdown is withheld")
	}

	cfg.DerivableCounts = "publish"
	r = Build(x, cfg, p, 1, "2027-01-10T00:00:00Z", LedgerRef{})
	if r.Government.ByOutcome == nil || len(r.Withheld) == 0 {
		t.Fatal(`derivable_counts "publish" keeps the breakdown and lists the warning`)
	}
}

func TestAsOfSeparatesCurrentStateFromPeriod(t *testing.T) {
	cfg := testConfig(t)
	p, _ := ParsePeriod("2026")
	// Published in October: the December subprocessor change is announced,
	// not current.
	recs := []record.Effective{
		eff(1, &record.SubprocessorChange{Effective: "2026-02-01", Change: "add", Name: "Mail AG", Locations: []string{"CH"}}),
		eff(2, &record.SubprocessorChange{Effective: "2026-12-01", Change: "add", Name: "Later GmbH", Locations: []string{"DE"}, NoticeGiven: "2026-10-01"}),
	}
	oct := p.WithAsOf(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	if oct.AsOf != "2026-10-15" {
		t.Fatalf("as_of = %q", oct.AsOf)
	}
	x := Aggregate(recs, oct, cfg.Home())
	if len(x.Subprocessors) != 1 || x.Subprocessors[0].Name != "Mail AG" {
		t.Fatalf("current list must stop at the as-of date: %+v", x.Subprocessors)
	}
	if len(x.SubUpcoming) != 1 || x.SubUpcoming[0].Name != "Later GmbH" {
		t.Fatalf("upcoming: %+v", x.SubUpcoming)
	}
	// After the period has ended the as-of date is the period end.
	jan := p.WithAsOf(time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC))
	if jan.AsOf != "2026-12-31" {
		t.Fatalf("as_of after period = %q", jan.AsOf)
	}
	if x := Aggregate(recs, jan, cfg.Home()); len(x.Subprocessors) != 2 || len(x.SubUpcoming) != 0 {
		t.Fatalf("full-year state: %+v %+v", x.Subprocessors, x.SubUpcoming)
	}
}
