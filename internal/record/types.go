// Package record defines ledger entry envelopes and record body schemas.
package record

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/transparencykit/transparencykit/internal/geo"
)

// Record type names.
const (
	TypeGovRequest   = "gov_request"
	TypeAbuseNotice  = "abuse_notice"
	TypeSubprocessor = "subprocessor_change"
	TypeInfra        = "infrastructure"
	TypeMeasures     = "measures"
	TypeRetraction   = "retraction"
)

// Types lists all record types in display order.
var Types = []string{TypeGovRequest, TypeAbuseNotice, TypeSubprocessor, TypeInfra, TypeMeasures, TypeRetraction}

// Categories are the DSA statement categories (Transparency Database constants,
// aligned by the Commission with the Annex I template).
var Categories = []string{
	"STATEMENT_CATEGORY_ANIMAL_WELFARE",
	"STATEMENT_CATEGORY_CONSUMER_INFORMATION",
	"STATEMENT_CATEGORY_CYBER_VIOLENCE",
	"STATEMENT_CATEGORY_CYBER_VIOLENCE_AGAINST_WOMEN",
	"STATEMENT_CATEGORY_DATA_PROTECTION_AND_PRIVACY_VIOLATIONS",
	"STATEMENT_CATEGORY_ILLEGAL_OR_HARMFUL_SPEECH",
	"STATEMENT_CATEGORY_INTELLECTUAL_PROPERTY_INFRINGEMENTS",
	"STATEMENT_CATEGORY_NEGATIVE_EFFECTS_ON_CIVIC_DISCOURSE_OR_ELECTIONS",
	"STATEMENT_CATEGORY_NOT_SPECIFIED_NOTICE",
	"STATEMENT_CATEGORY_OTHER_VIOLATION_TC",
	"STATEMENT_CATEGORY_PROTECTION_OF_MINORS",
	"STATEMENT_CATEGORY_RISK_FOR_PUBLIC_SECURITY",
	"STATEMENT_CATEGORY_SCAMS_AND_FRAUD",
	"STATEMENT_CATEGORY_SELF_HARM",
	"STATEMENT_CATEGORY_UNSAFE_AND_PROHIBITED_PRODUCTS",
	"STATEMENT_CATEGORY_VIOLENCE",
}

// Enumerations.
var (
	AuthorityTypes = []string{"law_enforcement", "court", "intelligence", "regulator", "other"}
	RequestKinds   = []string{"user_data", "content_action", "preservation", "emergency", "other"}
	DSAOrders      = []string{"art9", "art10"}
	DataCategories = []string{"none", "non_content", "content", "both"}
	Outcomes       = []string{"pending", "disclosed_full", "disclosed_partial", "actioned", "rejected", "withdrawn", "no_data"}
	Notifications  = []string{"notified_before", "notified_after", "not_notified_legal_prohibition", "not_notified_other", "not_applicable"}
	NoticeSources  = []string{"art16_notice", "trusted_flagger", "own_initiative", "other_notification"}
	Bases          = []string{"law", "terms", "none"}
	Actions        = []string{"none", "removed", "disabled", "restricted", "account_suspended", "account_terminated", "forwarded_to_customer"}
	SubChanges     = []string{"add", "update", "remove"}
	InfraStatus    = []string{"active", "retired"}
)

// Private holds operator-only data. It is never published.
type Private struct {
	CaseRef string `json:"case_ref,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

// GovRequest is a request or order from a public authority.
type GovRequest struct {
	ReceivedAt             string   `json:"received_at"`
	AuthorityCountry       string   `json:"authority_country"`
	AuthorityType          string   `json:"authority_type"`
	LegalBasis             string   `json:"legal_basis"`
	Kind                   string   `json:"kind"`
	DSAOrder               string   `json:"dsa_order,omitempty"`
	IllegalContentCategory string   `json:"illegal_content_category,omitempty"`
	DataCategory           string   `json:"data_category,omitempty"`
	Accounts               int      `json:"accounts,omitempty"`
	Items                  int      `json:"items,omitempty"`
	Outcome                string   `json:"outcome"`
	AcknowledgedAt         string   `json:"acknowledged_at,omitempty"`
	CompletedAt            string   `json:"completed_at,omitempty"`
	CustomerNotification   string   `json:"customer_notification"`
	Service                string   `json:"service,omitempty"`
	Zone                   string   `json:"zone,omitempty"`
	Private                *Private `json:"private,omitempty"`
}

// AbuseNotice is a notice (DSA Art. 16) or own-initiative detection.
type AbuseNotice struct {
	ReceivedAt         string   `json:"received_at"`
	Source             string   `json:"source"`
	Category           string   `json:"category"`
	Basis              string   `json:"basis"`
	Action             string   `json:"action"`
	ActionedAt         string   `json:"actioned_at,omitempty"`
	AutomatedDetection bool     `json:"automated_detection,omitempty"`
	AutomatedDecision  bool     `json:"automated_decision,omitempty"`
	Items              int      `json:"items,omitempty"`
	Service            string   `json:"service,omitempty"`
	Private            *Private `json:"private,omitempty"`
}

// SubprocessorChange adds, updates or removes a subprocessor.
type SubprocessorChange struct {
	Effective   string   `json:"effective"`
	Change      string   `json:"change"`
	Name        string   `json:"name"`
	Purpose     string   `json:"purpose,omitempty"`
	Locations   []string `json:"locations,omitempty"`
	Services    []string `json:"services,omitempty"`
	NoticeGiven string   `json:"notice_given,omitempty"`
	Private     *Private `json:"private,omitempty"`
}

// Infrastructure describes the jurisdiction of one zone (Data Act Art. 28(1)(a)).
type Infrastructure struct {
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
	Private            *Private `json:"private,omitempty"`
}

// Measures describes protections against international governmental access
// (Data Act Art. 28(1)(b)).
type Measures struct {
	Effective      string   `json:"effective"`
	Statement      string   `json:"statement,omitempty"`
	Technical      []string `json:"technical,omitempty"`
	Organisational []string `json:"organisational,omitempty"`
	Contractual    []string `json:"contractual,omitempty"`
	Private        *Private `json:"private,omitempty"`
}

// Retraction excludes an earlier entry from aggregation.
type Retraction struct {
	Seq    int64  `json:"seq"`
	Reason string `json:"reason"`
}

// NewBody returns a zero body for a type.
func NewBody(typ string) (any, error) {
	switch typ {
	case TypeGovRequest:
		return &GovRequest{}, nil
	case TypeAbuseNotice:
		return &AbuseNotice{}, nil
	case TypeSubprocessor:
		return &SubprocessorChange{}, nil
	case TypeInfra:
		return &Infrastructure{}, nil
	case TypeMeasures:
		return &Measures{}, nil
	case TypeRetraction:
		return &Retraction{}, nil
	}
	return nil, fmt.Errorf("unknown record type %q (valid: %v)", typ, Types)
}

// DecodeBody strictly decodes and validates a body of the given type.
func DecodeBody(typ string, raw []byte) (any, error) {
	b, err := NewBody(typ)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(b); err != nil {
		return nil, fmt.Errorf("%s: %w", typ, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%s: trailing data", typ)
	}
	applyDefaults(b)
	if err := Validate(b); err != nil {
		return nil, fmt.Errorf("%s: %w", typ, err)
	}
	return b, nil
}

func applyDefaults(b any) {
	switch t := b.(type) {
	case *GovRequest:
		if t.Items == 0 {
			t.Items = 1
		}
	case *AbuseNotice:
		if t.Items == 0 {
			t.Items = 1
		}
	}
}

// --- validation -------------------------------------------------------------

type verr struct{ errs []string }

func (v *verr) add(f string, a ...any) { v.errs = append(v.errs, fmt.Sprintf(f, a...)) }
func (v *verr) err() error {
	if len(v.errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid: %s", join(v.errs))
}

func join(xs []string) string {
	var b bytes.Buffer
	for i, x := range xs {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(x)
	}
	return b.String()
}

func (v *verr) req(name, val string) bool {
	if val == "" {
		v.add("%s is required", name)
		return false
	}
	return true
}

func (v *verr) enum(name, val string, allowed []string, required bool) {
	if val == "" {
		if required {
			v.add("%s is required (one of %v)", name, allowed)
		}
		return
	}
	for _, a := range allowed {
		if a == val {
			return
		}
	}
	v.add("%s %q not one of %v", name, val, allowed)
}

func (v *verr) ts(name, val string, required bool) {
	if val == "" {
		if required {
			v.add("%s is required (RFC 3339 timestamp)", name)
		}
		return
	}
	if _, err := time.Parse(time.RFC3339, val); err != nil {
		v.add("%s %q is not an RFC 3339 timestamp", name, val)
	}
}

func (v *verr) date(name, val string, required bool) {
	if val == "" {
		if required {
			v.add("%s is required (YYYY-MM-DD)", name)
		}
		return
	}
	if _, err := time.Parse("2006-01-02", val); err != nil {
		v.add("%s %q is not a YYYY-MM-DD date", name, val)
	}
}

func (v *verr) country(name, val string, required bool) {
	if val == "" {
		if required {
			v.add("%s is required", name)
		}
		return
	}
	if err := geo.ValidCountry(val); err != nil {
		v.add("%s: %v", name, err)
	}
}

func (v *verr) order(aName, a, bName, b string) {
	if a == "" || b == "" {
		return
	}
	ta, e1 := time.Parse(time.RFC3339, a)
	tb, e2 := time.Parse(time.RFC3339, b)
	if e1 == nil && e2 == nil && tb.Before(ta) {
		v.add("%s is before %s", bName, aName)
	}
}

// Validate checks a decoded body.
func Validate(b any) error {
	v := &verr{}
	switch t := b.(type) {
	case *GovRequest:
		v.ts("received_at", t.ReceivedAt, true)
		v.country("authority_country", t.AuthorityCountry, true)
		v.enum("authority_type", t.AuthorityType, AuthorityTypes, true)
		v.req("legal_basis", t.LegalBasis)
		v.enum("kind", t.Kind, RequestKinds, true)
		v.enum("dsa_order", t.DSAOrder, DSAOrders, false)
		if t.DSAOrder != "" && t.AuthorityCountry != "" && !geo.IsEU(t.AuthorityCountry) {
			v.add("dsa_order requires an EU Member State authority, got %s", t.AuthorityCountry)
		}
		if t.DSAOrder == "art9" {
			v.enum("illegal_content_category", t.IllegalContentCategory, Categories, true)
		} else {
			v.enum("illegal_content_category", t.IllegalContentCategory, Categories, false)
		}
		v.enum("data_category", t.DataCategory, DataCategories, false)
		if t.Accounts < 0 || t.Items < 0 {
			v.add("accounts and items must be >= 0")
		}
		v.enum("outcome", t.Outcome, Outcomes, true)
		v.ts("acknowledged_at", t.AcknowledgedAt, false)
		v.ts("completed_at", t.CompletedAt, false)
		v.order("received_at", t.ReceivedAt, "acknowledged_at", t.AcknowledgedAt)
		v.order("received_at", t.ReceivedAt, "completed_at", t.CompletedAt)
		v.enum("customer_notification", t.CustomerNotification, Notifications, true)
	case *AbuseNotice:
		v.ts("received_at", t.ReceivedAt, true)
		v.enum("source", t.Source, NoticeSources, true)
		v.enum("category", t.Category, Categories, true)
		v.enum("basis", t.Basis, Bases, true)
		v.enum("action", t.Action, Actions, true)
		v.ts("actioned_at", t.ActionedAt, false)
		v.order("received_at", t.ReceivedAt, "actioned_at", t.ActionedAt)
		if t.Items < 1 {
			v.add("items must be >= 1")
		}
	case *SubprocessorChange:
		v.date("effective", t.Effective, true)
		v.enum("change", t.Change, SubChanges, true)
		v.req("name", t.Name)
		if t.Change == "add" || t.Change == "update" {
			v.req("purpose", t.Purpose)
			if len(t.Locations) == 0 {
				v.add("locations is required for %s", t.Change)
			}
		}
		for _, c := range t.Locations {
			v.country("locations[]", c, true)
		}
		v.date("notice_given", t.NoticeGiven, false)
	case *Infrastructure:
		v.date("effective", t.Effective, true)
		v.req("zone", t.Zone)
		v.country("country", t.Country, true)
		v.country("operator_country", t.OperatorCountry, false)
		v.req("legal_entity", t.LegalEntity)
		v.country("legal_entity_country", t.LegalEntityCountry, true)
		v.enum("status", t.Status, InfraStatus, true)
	case *Measures:
		v.date("effective", t.Effective, true)
		if t.Statement == "" && len(t.Technical)+len(t.Organisational)+len(t.Contractual) == 0 {
			v.add("measures needs a statement or at least one measure")
		}
	case *Retraction:
		if t.Seq < 1 {
			v.add("seq must be >= 1")
		}
		v.req("reason", t.Reason)
	default:
		v.add("unknown body type %T", b)
	}
	return v.err()
}

// Template returns an example body for a type (used by `record template`).
func Template(typ string) (any, error) {
	switch typ {
	case TypeGovRequest:
		return GovRequest{
			ReceivedAt: "2026-03-02T09:30:00Z", AuthorityCountry: "CH", AuthorityType: "law_enforcement",
			LegalBasis: "CH StPO Art. 265", Kind: "user_data", DataCategory: "non_content", Accounts: 1, Items: 1,
			Outcome: "disclosed_partial", AcknowledgedAt: "2026-03-02T11:00:00Z", CompletedAt: "2026-03-06T16:00:00Z",
			CustomerNotification: "notified_before", Service: "compute", Zone: "ch-gva-2",
			Private: &Private{CaseRef: "LEG-2026-014", Notes: "never published"},
		}, nil
	case TypeAbuseNotice:
		return AbuseNotice{
			ReceivedAt: "2026-03-02T09:30:00Z", Source: "art16_notice", Category: "STATEMENT_CATEGORY_SCAMS_AND_FRAUD",
			Basis: "law", Action: "forwarded_to_customer", ActionedAt: "2026-03-02T13:00:00Z", Items: 1, Service: "compute",
		}, nil
	case TypeSubprocessor:
		return SubprocessorChange{Effective: "2026-04-01", Change: "add", Name: "Example Mail AG",
			Purpose: "Transactional e-mail", Locations: []string{"CH"}, Services: []string{"portal"}, NoticeGiven: "2026-03-01"}, nil
	case TypeInfra:
		return Infrastructure{Effective: "2026-01-01", Zone: "ch-gva-2", Country: "CH", City: "Geneva",
			Operator: "Example DC SA", OperatorCountry: "CH", LegalEntity: "Example Cloud SA", LegalEntityCountry: "CH",
			Services: []string{"compute", "object-storage"}, Status: "active"}, nil
	case TypeMeasures:
		return Measures{Effective: "2026-01-01",
			Statement:      "Customer data is stored and processed only in the selected zone by entities under Swiss/EU law.",
			Technical:      []string{"Encryption at rest with provider-managed keys held in-zone", "Customer-managed keys (SSE-C) supported"},
			Organisational: []string{"Legal review of every authority request", "No staff with access subject to third-country disclosure orders"},
			Contractual:    []string{"DPA clause: challenge and notify on third-country requests", "Subprocessors bound by equivalent terms"}}, nil
	case TypeRetraction:
		return Retraction{Seq: 1, Reason: "Recorded twice by mistake"}, nil
	}
	return nil, fmt.Errorf("unknown record type %q", typ)
}
