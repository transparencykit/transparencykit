package record

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/exoscale/vitrine/internal/canon"
)

// Entry is a ledger envelope.
type Entry struct {
	V          int             `json:"v"`
	Seq        int64           `json:"seq"`
	Type       string          `json:"type"`
	RecordedAt string          `json:"recorded_at"`
	RecordedBy string          `json:"recorded_by,omitempty"`
	Amends     int64           `json:"amends,omitempty"`
	Prev       string          `json:"prev"`
	Body       json.RawMessage `json:"body"`
	Hash       string          `json:"hash,omitempty"`
}

// Key returns the object key for a sequence number.
func Key(seq int64) string { return fmt.Sprintf("records/%012d.json", seq) }

// ComputeHash returns the entry hash (envelope canonicalised without "hash").
func (e *Entry) ComputeHash() (string, error) {
	c := *e
	c.Hash = ""
	b, err := canon.Marshal(c)
	if err != nil {
		return "", err
	}
	return canon.Hash(b), nil
}

// New builds and seals entry seq after prev.
func New(seq int64, prev, typ string, body any, by string, amends int64, now time.Time) (*Entry, error) {
	raw, err := canon.Marshal(body)
	if err != nil {
		return nil, err
	}
	e := &Entry{V: 1, Seq: seq, Type: typ, RecordedAt: now.UTC().Format(time.RFC3339),
		RecordedBy: by, Amends: amends, Prev: prev, Body: raw}
	h, err := e.ComputeHash()
	if err != nil {
		return nil, err
	}
	e.Hash = h
	return e, nil
}

// Encode serialises an entry for storage (canonical form, including hash).
func (e *Entry) Encode() ([]byte, error) { return canon.Marshal(e) }

// Parse strictly decodes a stored entry.
func Parse(b []byte) (*Entry, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var e Entry
	if err := dec.Decode(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// Check verifies the entry's own hash, version and body schema.
func (e *Entry) Check() error {
	if e.V != 1 {
		return fmt.Errorf("unsupported entry version %d", e.V)
	}
	if _, err := time.Parse(time.RFC3339, e.RecordedAt); err != nil {
		return fmt.Errorf("recorded_at: %v", err)
	}
	h, err := e.ComputeHash()
	if err != nil {
		return err
	}
	if h != e.Hash {
		return fmt.Errorf("hash mismatch: stored %s, computed %s", e.Hash, h)
	}
	if _, err := DecodeBody(e.Type, e.Body); err != nil {
		return err
	}
	return nil
}

// Decoded returns the typed body.
func (e *Entry) Decoded() (any, error) { return DecodeBody(e.Type, e.Body) }

// VerifyChain checks a full ordered slice of entries (seq 1..n).
// It returns the head hash ("" for an empty ledger).
func VerifyChain(entries []*Entry) (string, error) {
	prev := canon.Zero
	byseq := map[int64]*Entry{}
	for i, e := range entries {
		want := int64(i + 1)
		if e.Seq != want {
			return "", fmt.Errorf("seq %d: expected seq %d (gap, duplicate or reorder)", e.Seq, want)
		}
		if err := e.Check(); err != nil {
			return "", fmt.Errorf("seq %d: %w", e.Seq, err)
		}
		if e.Prev != prev {
			return "", fmt.Errorf("seq %d: prev %s does not match hash of seq %d (%s)", e.Seq, e.Prev, e.Seq-1, prev)
		}
		if e.Amends != 0 && e.Type == TypeRetraction {
			return "", fmt.Errorf("seq %d: retractions cannot amend", e.Seq)
		}
		if e.Amends != 0 {
			t, ok := byseq[e.Amends]
			switch {
			case !ok:
				return "", fmt.Errorf("seq %d: amends unknown seq %d", e.Seq, e.Amends)
			case t.Type != e.Type:
				return "", fmt.Errorf("seq %d: amends seq %d of different type %s", e.Seq, e.Amends, t.Type)
			case t.Amends != 0:
				return "", fmt.Errorf("seq %d: amends seq %d which is itself an amendment; amend the original", e.Seq, e.Amends)
			}
		}
		if e.Type == TypeRetraction {
			b, _ := e.Decoded()
			r := b.(*Retraction)
			t, ok := byseq[r.Seq]
			if !ok {
				return "", fmt.Errorf("seq %d: retracts unknown seq %d", e.Seq, r.Seq)
			}
			if t.Type == TypeRetraction || t.Amends != 0 {
				return "", fmt.Errorf("seq %d: retracts seq %d which is not an original record", e.Seq, r.Seq)
			}
		}
		byseq[e.Seq] = e
		prev = e.Hash
	}
	if len(entries) == 0 {
		return "", nil
	}
	return prev, nil
}

// Effective is a resolved record: original seq, type and the latest body.
type Effective struct {
	Seq  int64
	Type string
	Body any
}

// Resolve applies amendments and retractions, returning effective records in
// original-seq order. Entries must already be chain-verified.
func Resolve(entries []*Entry) ([]Effective, error) {
	latest := map[int64]any{}
	order := []int64{}
	types := map[int64]string{}
	retracted := map[int64]bool{}
	for _, e := range entries {
		b, err := e.Decoded()
		if err != nil {
			return nil, fmt.Errorf("seq %d: %w", e.Seq, err)
		}
		switch {
		case e.Type == TypeRetraction:
			retracted[b.(*Retraction).Seq] = true
		case e.Amends != 0:
			latest[e.Amends] = b
		default:
			latest[e.Seq] = b
			types[e.Seq] = e.Type
			order = append(order, e.Seq)
		}
	}
	out := make([]Effective, 0, len(order))
	for _, s := range order {
		if retracted[s] {
			continue
		}
		out = append(out, Effective{Seq: s, Type: types[s], Body: latest[s]})
	}
	return out, nil
}
