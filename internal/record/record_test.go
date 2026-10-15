package record

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/transparencykit/transparencykit/internal/canon"
)

var t0 = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

func gov(mut func(*GovRequest)) []byte {
	g, _ := Template(TypeGovRequest)
	gr := g.(GovRequest)
	if mut != nil {
		mut(&gr)
	}
	b, _ := json.Marshal(gr)
	return b
}

func TestTemplatesAreValid(t *testing.T) {
	for _, typ := range Types {
		tpl, err := Template(typ)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(tpl)
		if _, err := DecodeBody(typ, b); err != nil {
			t.Errorf("template %s invalid: %v", typ, err)
		}
	}
}

func TestValidation(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		typ  string
		want string
	}{
		{"unknown field", []byte(`{"received_at":"2026-01-01T00:00:00Z","bogus":1}`), TypeGovRequest, "unknown field"},
		{"bad country UK", gov(func(g *GovRequest) { g.AuthorityCountry = "UK" }), TypeGovRequest, "use GB"},
		{"bad country lower", gov(func(g *GovRequest) { g.AuthorityCountry = "ch" }), TypeGovRequest, "ISO 3166-1"},
		{"bad enum", gov(func(g *GovRequest) { g.Outcome = "maybe" }), TypeGovRequest, "outcome"},
		{"bad ts", gov(func(g *GovRequest) { g.ReceivedAt = "2026-03-02" }), TypeGovRequest, "RFC 3339"},
		{"ack before received", gov(func(g *GovRequest) { g.AcknowledgedAt = "2026-03-01T00:00:00Z" }), TypeGovRequest, "before"},
		{"dsa order non-EU", gov(func(g *GovRequest) { g.DSAOrder = "art10" }), TypeGovRequest, "EU Member State"},
		{"art9 needs category", gov(func(g *GovRequest) { g.AuthorityCountry = "DE"; g.DSAOrder = "art9" }), TypeGovRequest, "illegal_content_category"},
		{"missing notification", gov(func(g *GovRequest) { g.CustomerNotification = "" }), TypeGovRequest, "customer_notification"},
		{"float rejected", []byte(`{"seq":1.5,"reason":"x"}`), TypeRetraction, "seq"},
		{"subproc add needs locations", []byte(`{"effective":"2026-01-01","change":"add","name":"X","purpose":"p"}`), TypeSubprocessor, "locations"},
		{"measures empty", []byte(`{"effective":"2026-01-01"}`), TypeMeasures, "statement"},
		{"unknown type", []byte(`{}`), "nope", "unknown record type"},
	}
	for _, c := range cases {
		_, err := DecodeBody(c.typ, c.body)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", c.name, err, c.want)
		}
	}
	// Valid DSA art9 order from an EU authority.
	if _, err := DecodeBody(TypeGovRequest, gov(func(g *GovRequest) {
		g.AuthorityCountry = "DE"
		g.DSAOrder = "art9"
		g.IllegalContentCategory = "STATEMENT_CATEGORY_SCAMS_AND_FRAUD"
	})); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsItems(t *testing.T) {
	b, err := DecodeBody(TypeAbuseNotice, []byte(`{"received_at":"2026-01-01T00:00:00Z","source":"own_initiative","category":"STATEMENT_CATEGORY_SCAMS_AND_FRAUD","basis":"terms","action":"removed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.(*AbuseNotice).Items != 1 {
		t.Fatal("items default not applied")
	}
}

// chain builds a valid ledger from (type, body, amends) triples.
func chain(t *testing.T, items ...any) []*Entry {
	t.Helper()
	var out []*Entry
	prev := canon.Zero
	for i := 0; i < len(items); i += 3 {
		typ, raw, am := items[i].(string), items[i+1].([]byte), items[i+2].(int)
		body, err := DecodeBody(typ, raw)
		if err != nil {
			t.Fatal(err)
		}
		e, err := New(int64(len(out)+1), prev, typ, body, "test", int64(am), t0)
		if err != nil {
			t.Fatal(err)
		}
		// Round-trip through storage encoding.
		enc, _ := e.Encode()
		e2, err := Parse(enc)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, e2)
		prev = e.Hash
	}
	return out
}

func TestChainVerifies(t *testing.T) {
	es := chain(t, TypeGovRequest, gov(nil), 0, TypeGovRequest, gov(nil), 0,
		TypeGovRequest, gov(func(g *GovRequest) { g.Outcome = "rejected" }), 1)
	h, err := VerifyChain(es)
	if err != nil {
		t.Fatal(err)
	}
	if h != es[2].Hash {
		t.Fatal("head mismatch")
	}
	if h, err := VerifyChain(nil); err != nil || h != "" {
		t.Fatal("empty chain")
	}
}

func TestChainTamperDetection(t *testing.T) {
	base := func() []*Entry {
		return chain(t, TypeGovRequest, gov(nil), 0, TypeGovRequest, gov(nil), 0, TypeGovRequest, gov(nil), 0)
	}
	cases := []struct {
		name string
		mut  func([]*Entry) []*Entry
		want string
	}{
		{"edit body", func(es []*Entry) []*Entry {
			es[1].Body = []byte(strings.Replace(string(es[1].Body), "disclosed_partial", "rejected", 1))
			return es
		}, "seq 2: hash mismatch"},
		{"edit body and rehash", func(es []*Entry) []*Entry {
			es[1].Body = []byte(strings.Replace(string(es[1].Body), "disclosed_partial", "rejected", 1))
			es[1].Hash, _ = es[1].ComputeHash()
			return es
		}, "seq 3: prev"},
		{"delete middle", func(es []*Entry) []*Entry { return []*Entry{es[0], es[2]} }, "expected seq 2"},
		{"delete first", func(es []*Entry) []*Entry { return es[1:] }, "expected seq 1"},
		{"reorder", func(es []*Entry) []*Entry { return []*Entry{es[0], es[2], es[1]} }, "expected seq 2"},
		{"edit timestamp", func(es []*Entry) []*Entry { es[0].RecordedAt = "2020-01-01T00:00:00Z"; return es }, "seq 1: hash mismatch"},
	}
	for _, c := range cases {
		_, err := VerifyChain(c.mut(base()))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestAmendRules(t *testing.T) {
	sub := []byte(`{"effective":"2026-01-01","change":"remove","name":"X"}`)
	// amend of different type
	es := chain(t, TypeGovRequest, gov(nil), 0, TypeSubprocessor, sub, 0)
	es2 := chain(t, TypeGovRequest, gov(nil), 0)
	// Hand-build an invalid amendment (bypassing ledger checks).
	b, _ := DecodeBody(TypeSubprocessor, sub)
	bad, _ := New(2, es2[0].Hash, TypeSubprocessor, b, "", 1, t0)
	if _, err := VerifyChain([]*Entry{es2[0], bad}); err == nil || !strings.Contains(err.Error(), "different type") {
		t.Errorf("type mismatch: %v", err)
	}
	_ = es
	// amend of amendment
	es3 := chain(t, TypeGovRequest, gov(nil), 0, TypeGovRequest, gov(nil), 1)
	g, _ := DecodeBody(TypeGovRequest, gov(nil))
	bad2, _ := New(3, es3[1].Hash, TypeGovRequest, g, "", 2, t0)
	if _, err := VerifyChain(append(es3, bad2)); err == nil || !strings.Contains(err.Error(), "itself an amendment") {
		t.Errorf("amend of amendment: %v", err)
	}
}

func TestResolveAmendAndRetract(t *testing.T) {
	es := chain(t,
		TypeGovRequest, gov(func(g *GovRequest) { g.Outcome = "pending" }), 0, // 1
		TypeGovRequest, gov(nil), 0, // 2
		TypeGovRequest, gov(func(g *GovRequest) { g.Outcome = "rejected" }), 1, // 3 amends 1
		TypeGovRequest, gov(func(g *GovRequest) { g.Outcome = "withdrawn" }), 1, // 4 amends 1 again
		TypeRetraction, []byte(`{"seq":2,"reason":"duplicate"}`), 0, // 5
	)
	if _, err := VerifyChain(es); err != nil {
		t.Fatal(err)
	}
	eff, err := Resolve(es)
	if err != nil {
		t.Fatal(err)
	}
	if len(eff) != 1 || eff[0].Seq != 1 || eff[0].Body.(*GovRequest).Outcome != "withdrawn" {
		t.Fatalf("resolve = %+v", eff)
	}
}

func TestRetractionMustTargetOriginal(t *testing.T) {
	es := chain(t, TypeGovRequest, gov(nil), 0, TypeGovRequest, gov(nil), 1)
	r, _ := DecodeBody(TypeRetraction, []byte(`{"seq":2,"reason":"x"}`))
	bad, _ := New(3, es[1].Hash, TypeRetraction, r, "", 0, t0)
	if _, err := VerifyChain(append(es, bad)); err == nil || !strings.Contains(err.Error(), "not an original") {
		t.Fatalf("got %v", err)
	}
}
