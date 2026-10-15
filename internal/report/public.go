package report

import (
	"fmt"
	"sort"

	"github.com/exoscale/vitrine/internal/config"
)

// Schema ids.
const (
	ReportSchema = "vitrine/report/v1"
	Art28Schema  = "vitrine/data-act-art28/v1"
	SubSchema    = "vitrine/subprocessors/v1"
)

// Cell is a published count: an int (exact) or a string band like "1-2".
type Cell = any

// Bander applies small-count banding.
type Bander struct{ Min int }

// C bands one breakdown cell.
func (b Bander) C(n int) Cell {
	if n > 0 && n < b.Min {
		if b.Min == 2 {
			return "1"
		}
		return fmt.Sprintf("1-%d", b.Min-1)
	}
	return n
}

// M bands a whole map.
func (b Bander) M(m map[string]int) map[string]Cell {
	out := make(map[string]Cell, len(m))
	for k, v := range m {
		out[k] = b.C(v)
	}
	return out
}

// LedgerRef commits to the ledger head.
type LedgerRef struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
}

// CountryRow is one country line in the government-request table.
type CountryRow struct {
	Country      string `json:"country"`
	ThirdCountry bool   `json:"third_country"`
	Total        Cell   `json:"total"`
	Disclosed    Cell   `json:"disclosed"`
	Rejected     Cell   `json:"rejected"`
	Other        Cell   `json:"other"`
}

// GovOut is the public government-request section.
type GovOut struct {
	Total                int             `json:"total"`
	AccountsSpecified    int             `json:"accounts_specified"`
	ByCountry            []CountryRow    `json:"by_country"`
	ByAuthorityType      map[string]Cell `json:"by_authority_type"`
	ByKind               map[string]Cell `json:"by_kind"`
	ByOutcome            map[string]Cell `json:"by_outcome"`
	ByDataCategory       map[string]Cell `json:"by_data_category"`
	CustomerNotification map[string]Cell `json:"customer_notification"`
	DisclosureRatePct    *float64        `json:"disclosure_rate_pct"`
	MedianHoursToRespond *float64        `json:"median_hours_to_respond"`
	ThirdCountry         ThirdOut        `json:"third_country"`
}

// ThirdOut is the third-country (e.g. CLOUD Act) section.
type ThirdOut struct {
	Total                Cell            `json:"total"`
	ByCountry            map[string]Cell `json:"by_country"`
	ByOutcome            map[string]Cell `json:"by_outcome"`
	CustomerNotification map[string]Cell `json:"customer_notification"`
}

// AbuseOut is the public abuse/notice section.
type AbuseOut struct {
	Total               int             `json:"total"`
	BySource            map[string]Cell `json:"by_source"`
	ByCategory          map[string]Cell `json:"by_category"`
	ByAction            map[string]Cell `json:"by_action"`
	ByBasis             map[string]Cell `json:"by_basis"`
	MedianHoursToAction *float64        `json:"median_hours_to_action"`
}

// SubOut is the subprocessor section.
type SubOut struct {
	Current []SubprocessorOut `json:"current"`
	Changes []SubChangeOut    `json:"changes_in_period"`
}

// Report is report.json.
type Report struct {
	Schema            string          `json:"schema"`
	Provider          config.Provider `json:"provider"`
	Period            Period          `json:"period"`
	Version           int             `json:"version"`
	GeneratedAt       string          `json:"generated_at"`
	Ledger            LedgerRef       `json:"ledger"`
	HomeJurisdictions []string        `json:"home_jurisdictions"`
	MinCount          int             `json:"min_count"`
	Government        GovOut          `json:"government_requests"`
	Abuse             AbuseOut        `json:"abuse_notices"`
	Subprocessors     SubOut          `json:"subprocessors"`
	Infrastructure    []InfraOut      `json:"infrastructure"`
	Measures          *MeasuresOut    `json:"measures"`
}

// Build turns exact aggregates into the banded public report.
func Build(x *Exact, cfg *config.Config, p Period, version int, generatedAt string, led LedgerRef) *Report {
	b := Bander{Min: cfg.MinCount}
	home := cfg.Home()
	g := x.Gov
	gov := GovOut{
		Total: g.Total, AccountsSpecified: g.Accounts,
		ByAuthorityType: b.M(g.ByAuthorityType), ByKind: b.M(g.ByKind), ByOutcome: b.M(g.ByOutcome),
		ByDataCategory: b.M(g.ByDataCategory), CustomerNotification: b.M(g.ByNotification),
		MedianHoursToRespond: g.Completion.Median(),
		ThirdCountry: ThirdOut{Total: b.C(g.ThirdTotal), ByCountry: b.M(g.ThirdByCountry),
			ByOutcome: b.M(g.ThirdByOutcome), CustomerNotification: b.M(g.ThirdByNotif)},
	}
	countries := make([]string, 0, len(g.ByCountry))
	for c := range g.ByCountry {
		countries = append(countries, c)
	}
	sort.Slice(countries, func(i, j int) bool {
		if g.ByCountry[countries[i]] != g.ByCountry[countries[j]] {
			return g.ByCountry[countries[i]] > g.ByCountry[countries[j]]
		}
		return countries[i] < countries[j]
	})
	for _, c := range countries {
		o := g.ByCountryOutcome[c]
		disc := o["disclosed_full"] + o["disclosed_partial"] + o["actioned"]
		rej := o["rejected"]
		gov.ByCountry = append(gov.ByCountry, CountryRow{Country: c, ThirdCountry: !home[c],
			Total: b.C(g.ByCountry[c]), Disclosed: b.C(disc), Rejected: b.C(rej), Other: b.C(g.ByCountry[c] - disc - rej)})
	}
	if decided := g.Total - g.Pending; decided >= cfg.MinCount {
		disc := g.ByOutcome["disclosed_full"] + g.ByOutcome["disclosed_partial"] + g.ByOutcome["actioned"]
		r := float64(int(float64(disc)/float64(decided)*1000+0.5)) / 10
		gov.DisclosureRatePct = &r
	}
	a := x.Abuse
	ab := AbuseOut{Total: a.Total, BySource: b.M(a.BySource), ByCategory: b.M(a.ByCategory),
		ByAction: b.M(a.ByAction), ByBasis: b.M(a.ByBasis), MedianHoursToAction: a.TimeToAction.Median()}
	subs := SubOut{Current: x.Subprocessors, Changes: x.SubChanges}
	if subs.Current == nil {
		subs.Current = []SubprocessorOut{}
	}
	if subs.Changes == nil {
		subs.Changes = []SubChangeOut{}
	}
	infra := x.Infra
	if infra == nil {
		infra = []InfraOut{}
	}
	return &Report{Schema: ReportSchema, Provider: cfg.Provider, Period: p, Version: version, GeneratedAt: generatedAt,
		Ledger: led, HomeJurisdictions: cfg.HomeJurisdictions, MinCount: cfg.MinCount,
		Government: gov, Abuse: ab, Subprocessors: subs, Infrastructure: infra, Measures: x.Measures}
}

// Art28 is data-act-art28.json.
type Art28 struct {
	Schema      string          `json:"schema"`
	Provider    config.Provider `json:"provider"`
	AsOf        string          `json:"as_of"`
	GeneratedAt string          `json:"generated_at"`
	LegalBasis  string          `json:"legal_basis"`
	Zones       []InfraOut      `json:"jurisdiction_of_ict_infrastructure"`
	Measures    *MeasuresOut    `json:"measures_against_international_governmental_access"`
	Ledger      LedgerRef       `json:"ledger"`
}

// BuildArt28 builds the Data Act Art. 28 disclosure.
func BuildArt28(x *Exact, cfg *config.Config, p Period, generatedAt string, led LedgerRef) *Art28 {
	z := x.Infra
	if z == nil {
		z = []InfraOut{}
	}
	return &Art28{Schema: Art28Schema, Provider: cfg.Provider, AsOf: p.End, GeneratedAt: generatedAt,
		LegalBasis: "Regulation (EU) 2023/2854 (Data Act), Article 28(1)(a) and (b)",
		Zones:      z, Measures: x.Measures, Ledger: led}
}

// Subprocessors is subprocessors.json.
type Subprocessors struct {
	Schema   string            `json:"schema"`
	Provider string            `json:"provider"`
	AsOf     string            `json:"as_of"`
	Current  []SubprocessorOut `json:"current"`
	History  []SubChangeOut    `json:"history"`
}
