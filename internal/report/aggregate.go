// Package report aggregates ledger records and renders publications.
package report

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/transparencykit/transparencykit/internal/geo"
	"github.com/transparencykit/transparencykit/internal/record"
)

// Period is an inclusive UTC date range.
type Period struct {
	Label string `json:"label"`
	Start string `json:"start"`
	End   string `json:"end"`
	start time.Time
	end   time.Time // exclusive (day after End)
}

var (
	reYear  = regexp.MustCompile(`^(\d{4})$`)
	reHalf  = regexp.MustCompile(`^(\d{4})-H([12])$`)
	reQ     = regexp.MustCompile(`^(\d{4})-Q([1-4])$`)
	reRange = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.\.(\d{4}-\d{2}-\d{2})$`)
)

// ParsePeriod accepts 2026, 2026-H1, 2026-Q3, 2026-01-01..2026-03-31.
func ParsePeriod(s string) (Period, error) {
	mk := func(label string, a, b time.Time) Period {
		return Period{Label: label, Start: a.Format("2006-01-02"), End: b.AddDate(0, 0, -1).Format("2006-01-02"), start: a, end: b}
	}
	if m := reYear.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		a := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		return mk(s, a, a.AddDate(1, 0, 0)), nil
	}
	if m := reHalf.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		h, _ := strconv.Atoi(m[2])
		a := time.Date(y, time.Month(1+6*(h-1)), 1, 0, 0, 0, 0, time.UTC)
		return mk(s, a, a.AddDate(0, 6, 0)), nil
	}
	if m := reQ.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		q, _ := strconv.Atoi(m[2])
		a := time.Date(y, time.Month(1+3*(q-1)), 1, 0, 0, 0, 0, time.UTC)
		return mk(s, a, a.AddDate(0, 3, 0)), nil
	}
	if m := reRange.FindStringSubmatch(s); m != nil {
		a, e1 := time.Parse("2006-01-02", m[1])
		b, e2 := time.Parse("2006-01-02", m[2])
		if e1 != nil || e2 != nil || b.Before(a) {
			return Period{}, fmt.Errorf("invalid period range %q", s)
		}
		return mk(m[1]+"_"+m[2], a, b.AddDate(0, 0, 1)), nil
	}
	return Period{}, fmt.Errorf("invalid period %q (use 2026, 2026-H1, 2026-Q3 or 2026-01-01..2026-03-31)", s)
}

// ContainsTS reports whether an RFC 3339 timestamp is in the period.
func (p Period) ContainsTS(ts string) bool {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	t = t.UTC()
	return !t.Before(p.start) && t.Before(p.end)
}

// ContainsDate reports whether a YYYY-MM-DD date is in the period.
func (p Period) ContainsDate(d string) bool {
	t, err := time.Parse("2006-01-02", d)
	return err == nil && !t.Before(p.start) && t.Before(p.end)
}

// NotAfterEnd reports whether a date is on or before the period end.
func (p Period) NotAfterEnd(d string) bool {
	t, err := time.Parse("2006-01-02", d)
	return err == nil && t.Before(p.end)
}

// ---------------------------------------------------------------------------
// Exact aggregates (never banded; used for DSA CSVs and as banding input).

// Durations collects hour values for medians.
type Durations []float64

// Median returns the lower median, rounded to 0.1 h, or nil when empty.
func (d Durations) Median() *float64 {
	if len(d) == 0 {
		return nil
	}
	s := append(Durations(nil), d...)
	sort.Float64s(s)
	m := s[(len(s)-1)/2]
	m = math.Round(m*10) / 10
	return &m
}

func hoursBetween(a, b string) (float64, bool) {
	ta, e1 := time.Parse(time.RFC3339, a)
	tb, e2 := time.Parse(time.RFC3339, b)
	if e1 != nil || e2 != nil {
		return 0, false
	}
	return tb.Sub(ta).Hours(), true
}

// OrderCell aggregates DSA orders for one (category, member state).
type OrderCell struct {
	Act, ActItems     int
	ActAck, ActEffect Durations
	Info              int
	InfoAck, InfoEff  Durations
}

// NoticeCell aggregates DSA Art. 16 notices for one category.
type NoticeCell struct {
	Notices, NoticesTF       int
	Items, ItemsTF           int
	TimeToAction             Durations
	ActionsLaw, ActionsLawTF int
	ActionsTC, ActionsTCTF   int
	AutomatedProcessed       int
	AutomatedActions         int
}

// OwnCell aggregates own-initiative measures for one category.
type OwnCell struct {
	Measures, Items int
	AutoDetected    int
	AutoDecided     int
	ByAction        map[string]int
	TimeToAction    Durations
}

// Exact holds unbanded aggregates for a period.
type Exact struct {
	Gov struct {
		Total, Accounts  int
		ByCountry        map[string]int
		ByCountryOutcome map[string]map[string]int
		ByAuthorityType  map[string]int
		ByKind           map[string]int
		ByOutcome        map[string]int
		ByDataCategory   map[string]int
		ByNotification   map[string]int
		ThirdTotal       int
		ThirdByCountry   map[string]int
		ThirdByOutcome   map[string]int
		ThirdByNotif     map[string]int
		Pending          int
		Completion       Durations
	}
	// Orders[category][memberState]; category "" = not categorised.
	Orders map[string]map[string]*OrderCell
	Abuse  struct {
		Total        int
		BySource     map[string]int
		ByCategory   map[string]int
		ByAction     map[string]int
		ByBasis      map[string]int
		TimeToAction Durations
	}
	Notices  map[string]*NoticeCell // by category
	OwnLaw   map[string]*OwnCell    // own initiative, basis law
	OwnTerms map[string]*OwnCell    // own initiative, basis terms/none

	Subprocessors []SubprocessorOut
	SubChanges    []SubChangeOut
	SubHistory    []SubChangeOut // all changes up to period end (feed)
	Infra         []InfraOut
	Measures      *MeasuresOut
}

func inc(m map[string]int, k string, n int) { m[k] += n }

func zeroMap(keys []string) map[string]int {
	m := make(map[string]int, len(keys))
	for _, k := range keys {
		m[k] = 0
	}
	return m
}

// Aggregate computes exact aggregates for the period.
func Aggregate(recs []record.Effective, p Period, home geo.Set) *Exact {
	x := &Exact{Orders: map[string]map[string]*OrderCell{}, Notices: map[string]*NoticeCell{},
		OwnLaw: map[string]*OwnCell{}, OwnTerms: map[string]*OwnCell{}}
	g := &x.Gov
	g.ByCountry, g.ByCountryOutcome = map[string]int{}, map[string]map[string]int{}
	g.ByAuthorityType, g.ByKind = zeroMap(record.AuthorityTypes), zeroMap(record.RequestKinds)
	g.ByOutcome, g.ByDataCategory = zeroMap(record.Outcomes), zeroMap(record.DataCategories)
	g.ByNotification = zeroMap(record.Notifications)
	g.ThirdByCountry, g.ThirdByOutcome, g.ThirdByNotif = map[string]int{}, zeroMap(record.Outcomes), zeroMap(record.Notifications)
	a := &x.Abuse
	a.BySource, a.ByCategory = zeroMap(record.NoticeSources), zeroMap(record.Categories)
	a.ByAction, a.ByBasis = zeroMap(record.Actions), zeroMap(record.Bases)

	subs := map[string]SubprocessorOut{}
	infra := map[string]InfraOut{}
	var measures *record.Measures

	for _, r := range recs {
		switch b := r.Body.(type) {
		case *record.GovRequest:
			if !p.ContainsTS(b.ReceivedAt) {
				continue
			}
			g.Total++
			g.Accounts += b.Accounts
			inc(g.ByCountry, b.AuthorityCountry, 1)
			if g.ByCountryOutcome[b.AuthorityCountry] == nil {
				g.ByCountryOutcome[b.AuthorityCountry] = map[string]int{}
			}
			inc(g.ByCountryOutcome[b.AuthorityCountry], b.Outcome, 1)
			inc(g.ByAuthorityType, b.AuthorityType, 1)
			inc(g.ByKind, b.Kind, 1)
			inc(g.ByOutcome, b.Outcome, 1)
			if b.DataCategory != "" {
				inc(g.ByDataCategory, b.DataCategory, 1)
			}
			inc(g.ByNotification, b.CustomerNotification, 1)
			if b.Outcome == "pending" {
				g.Pending++
			}
			if h, ok := hoursBetween(b.ReceivedAt, b.CompletedAt); ok {
				g.Completion = append(g.Completion, h)
			}
			if !home[b.AuthorityCountry] {
				g.ThirdTotal++
				inc(g.ThirdByCountry, b.AuthorityCountry, 1)
				inc(g.ThirdByOutcome, b.Outcome, 1)
				inc(g.ThirdByNotif, b.CustomerNotification, 1)
			}
			if b.DSAOrder != "" {
				cat := b.IllegalContentCategory
				if x.Orders[cat] == nil {
					x.Orders[cat] = map[string]*OrderCell{}
				}
				c := x.Orders[cat][b.AuthorityCountry]
				if c == nil {
					c = &OrderCell{}
					x.Orders[cat][b.AuthorityCountry] = c
				}
				ack, okA := hoursBetween(b.ReceivedAt, b.AcknowledgedAt)
				eff, okE := hoursBetween(b.ReceivedAt, b.CompletedAt)
				if b.DSAOrder == "art9" {
					c.Act++
					c.ActItems += b.Items
					if okA {
						c.ActAck = append(c.ActAck, ack)
					}
					if okE {
						c.ActEffect = append(c.ActEffect, eff)
					}
				} else {
					c.Info++
					if okA {
						c.InfoAck = append(c.InfoAck, ack)
					}
					if okE {
						c.InfoEff = append(c.InfoEff, eff)
					}
				}
			}
		case *record.AbuseNotice:
			if !p.ContainsTS(b.ReceivedAt) {
				continue
			}
			a.Total++
			inc(a.BySource, b.Source, 1)
			inc(a.ByCategory, b.Category, 1)
			inc(a.ByAction, b.Action, 1)
			inc(a.ByBasis, b.Basis, 1)
			h, okH := hoursBetween(b.ReceivedAt, b.ActionedAt)
			if okH && b.Action != "none" {
				a.TimeToAction = append(a.TimeToAction, h)
			}
			acted := b.Action != "none"
			if b.Source == "own_initiative" {
				m := x.OwnTerms
				if b.Basis == "law" {
					m = x.OwnLaw
				}
				c := m[b.Category]
				if c == nil {
					c = &OwnCell{ByAction: map[string]int{}}
					m[b.Category] = c
				}
				c.Measures++
				c.Items += b.Items
				if b.AutomatedDetection {
					c.AutoDetected++
				}
				if b.AutomatedDecision {
					c.AutoDecided++
				}
				c.ByAction[b.Action]++
				if okH && acted {
					c.TimeToAction = append(c.TimeToAction, h)
				}
				continue
			}
			c := x.Notices[b.Category]
			if c == nil {
				c = &NoticeCell{}
				x.Notices[b.Category] = c
			}
			tf := b.Source == "trusted_flagger"
			c.Notices++
			c.Items += b.Items
			if tf {
				c.NoticesTF++
				c.ItemsTF += b.Items
			}
			if okH && acted {
				c.TimeToAction = append(c.TimeToAction, h)
			}
			if acted && b.Basis == "law" {
				c.ActionsLaw++
				if tf {
					c.ActionsLawTF++
				}
			}
			if acted && b.Basis == "terms" {
				c.ActionsTC++
				if tf {
					c.ActionsTCTF++
				}
			}
			if b.AutomatedDetection || b.AutomatedDecision {
				c.AutomatedProcessed++
			}
			if b.AutomatedDecision && acted {
				c.AutomatedActions++
			}
		case *record.SubprocessorChange:
			if !p.NotAfterEnd(b.Effective) {
				continue
			}
			ch := SubChangeOut{Seq: r.Seq, Effective: b.Effective, Change: b.Change, Name: b.Name,
				Purpose: b.Purpose, Locations: b.Locations, Services: b.Services, NoticeGiven: b.NoticeGiven}
			x.SubHistory = append(x.SubHistory, ch)
			if p.ContainsDate(b.Effective) {
				x.SubChanges = append(x.SubChanges, ch)
			}
		case *record.Infrastructure:
			if !p.NotAfterEnd(b.Effective) {
				continue
			}
			if cur, ok := infra[b.Zone]; ok && cur.Effective > b.Effective {
				continue
			}
			infra[b.Zone] = InfraOut{Effective: b.Effective, Zone: b.Zone, Country: b.Country, City: b.City,
				Operator: b.Operator, OperatorCountry: b.OperatorCountry, LegalEntity: b.LegalEntity,
				LegalEntityCountry: b.LegalEntityCountry, Services: b.Services, Status: b.Status}
		case *record.Measures:
			if !p.NotAfterEnd(b.Effective) {
				continue
			}
			if measures == nil || b.Effective >= measures.Effective {
				measures = b
			}
		}
	}

	// Fold subprocessor history (stable by effective date, then seq).
	sort.SliceStable(x.SubHistory, func(i, j int) bool {
		if x.SubHistory[i].Effective != x.SubHistory[j].Effective {
			return x.SubHistory[i].Effective < x.SubHistory[j].Effective
		}
		return x.SubHistory[i].Seq < x.SubHistory[j].Seq
	})
	for _, ch := range x.SubHistory {
		switch ch.Change {
		case "remove":
			delete(subs, ch.Name)
		case "add":
			subs[ch.Name] = SubprocessorOut{Name: ch.Name, Purpose: ch.Purpose, Locations: ch.Locations, Services: ch.Services, Since: ch.Effective}
		case "update":
			since := ch.Effective
			if cur, ok := subs[ch.Name]; ok {
				since = cur.Since
			}
			subs[ch.Name] = SubprocessorOut{Name: ch.Name, Purpose: ch.Purpose, Locations: ch.Locations, Services: ch.Services, Since: since}
		}
	}
	for _, s := range subs {
		x.Subprocessors = append(x.Subprocessors, s)
	}
	sort.Slice(x.Subprocessors, func(i, j int) bool { return x.Subprocessors[i].Name < x.Subprocessors[j].Name })
	sort.SliceStable(x.SubChanges, func(i, j int) bool { return x.SubChanges[i].Effective < x.SubChanges[j].Effective })
	for _, z := range infra {
		if z.Status == "retired" {
			continue
		}
		x.Infra = append(x.Infra, z)
	}
	sort.Slice(x.Infra, func(i, j int) bool { return x.Infra[i].Zone < x.Infra[j].Zone })
	if measures != nil {
		x.Measures = &MeasuresOut{Effective: measures.Effective, Statement: measures.Statement,
			Technical: measures.Technical, Organisational: measures.Organisational, Contractual: measures.Contractual}
	}
	return x
}

// Public projections (no private fields by construction).

// SubprocessorOut is a current subprocessor.
type SubprocessorOut struct {
	Name      string   `json:"name"`
	Purpose   string   `json:"purpose"`
	Locations []string `json:"locations"`
	Services  []string `json:"services,omitempty"`
	Since     string   `json:"since"`
}

// SubChangeOut is a public subprocessor change.
type SubChangeOut struct {
	Seq         int64    `json:"-"`
	Effective   string   `json:"effective"`
	Change      string   `json:"change"`
	Name        string   `json:"name"`
	Purpose     string   `json:"purpose,omitempty"`
	Locations   []string `json:"locations,omitempty"`
	Services    []string `json:"services,omitempty"`
	NoticeGiven string   `json:"notice_given,omitempty"`
}

// InfraOut is a public zone jurisdiction row.
type InfraOut struct {
	Effective          string   `json:"effective"`
	Zone               string   `json:"zone"`
	Country            string   `json:"country"`
	City               string   `json:"city,omitempty"`
	Operator           string   `json:"operator,omitempty"`
	OperatorCountry    string   `json:"operator_country,omitempty"`
	LegalEntity        string   `json:"legal_entity"`
	LegalEntityCountry string   `json:"legal_entity_country"`
	Services           []string `json:"services,omitempty"`
	Status             string   `json:"status"`
}

// MeasuresOut is the public Art. 28(1)(b) description.
type MeasuresOut struct {
	Effective      string   `json:"effective"`
	Statement      string   `json:"statement,omitempty"`
	Technical      []string `json:"technical,omitempty"`
	Organisational []string `json:"organisational,omitempty"`
	Contractual    []string `json:"contractual,omitempty"`
}
