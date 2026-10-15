// Package ledger appends to and verifies the hash-chained record ledger.
package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/transparencykit/transparencykit/internal/canon"
	"github.com/transparencykit/transparencykit/internal/record"
	"github.com/transparencykit/transparencykit/internal/store"
)

// Ledger wraps a store.
type Ledger struct {
	S         store.Store
	Retention *store.Retention
	Now       func() time.Time
}

// Head is the advisory head pointer.
type Head struct {
	Seq       int64  `json:"seq"`
	Hash      string `json:"hash"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

var keyRe = regexp.MustCompile(`^records/(\d{12})\.json$`)

func (l *Ledger) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// seqs lists sequence numbers present in the store, ascending.
func (l *Ledger) seqs(ctx context.Context) ([]int64, error) {
	keys, err := l.S.List(ctx, "records/")
	if err != nil {
		return nil, err
	}
	var out []int64
	for _, k := range keys {
		m := keyRe.FindStringSubmatch(k)
		if m == nil {
			return nil, fmt.Errorf("unexpected object in records/: %s", k)
		}
		n, _ := strconv.ParseInt(m[1], 10, 64)
		out = append(out, n)
	}
	return out, nil
}

// LoadAll returns all entries in order (no verification).
func (l *Ledger) LoadAll(ctx context.Context) ([]*record.Entry, error) {
	seqs, err := l.seqs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*record.Entry, 0, len(seqs))
	for _, n := range seqs {
		b, err := l.S.Get(ctx, record.Key(n))
		if err != nil {
			return nil, fmt.Errorf("seq %d: %w", n, err)
		}
		e, err := record.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("seq %d: parse: %w", n, err)
		}
		if e.Seq != n {
			return nil, fmt.Errorf("object %s contains seq %d", record.Key(n), e.Seq)
		}
		out = append(out, e)
	}
	return out, nil
}

// Verify loads and verifies the full chain, returning entries and head.
func (l *Ledger) Verify(ctx context.Context) ([]*record.Entry, Head, error) {
	es, err := l.LoadAll(ctx)
	if err != nil {
		return nil, Head{}, err
	}
	h, err := record.VerifyChain(es)
	if err != nil {
		return es, Head{}, err
	}
	head := Head{Hash: h}
	if len(es) > 0 {
		head.Seq = es[len(es)-1].Seq
	}
	return es, head, nil
}

// Append adds a new entry. The body is validated before writing.
func (l *Ledger) Append(ctx context.Context, typ string, rawBody []byte, by string, amends int64) (*record.Entry, error) {
	body, err := record.DecodeBody(typ, rawBody)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		seqs, err := l.seqs(ctx)
		if err != nil {
			return nil, err
		}
		prev := canon.Zero
		var last int64
		if len(seqs) > 0 {
			last = seqs[len(seqs)-1]
			if int64(len(seqs)) != last {
				return nil, fmt.Errorf("ledger has gaps (%d objects, highest seq %d); run `transparencykit ledger verify`", len(seqs), last)
			}
			b, err := l.S.Get(ctx, record.Key(last))
			if err != nil {
				return nil, err
			}
			le, err := record.Parse(b)
			if err != nil {
				return nil, err
			}
			if err := le.Check(); err != nil {
				return nil, fmt.Errorf("head seq %d invalid: %w", last, err)
			}
			prev = le.Hash
		}
		if amends != 0 {
			if err := l.checkAmends(ctx, typ, amends, last); err != nil {
				return nil, err
			}
		}
		if r, ok := body.(*record.Retraction); ok {
			if err := l.checkRetract(ctx, r.Seq, last); err != nil {
				return nil, err
			}
		}
		e, err := record.New(last+1, prev, typ, body, by, amends, l.now())
		if err != nil {
			return nil, err
		}
		data, err := e.Encode()
		if err != nil {
			return nil, err
		}
		err = l.S.Put(ctx, record.Key(e.Seq), data, store.PutOptions{
			ContentType: "application/json", CreateOnly: true, Retention: l.Retention})
		if errors.Is(err, store.ErrExists) {
			continue // lost a race; retry on the new head
		}
		if err != nil {
			return nil, err
		}
		hb, _ := json.Marshal(Head{Seq: e.Seq, Hash: e.Hash, UpdatedAt: l.now().UTC().Format(time.RFC3339)})
		_ = l.S.Put(ctx, "head.json", hb, store.PutOptions{ContentType: "application/json"})
		return e, nil
	}
	return nil, fmt.Errorf("append: too many concurrent writers, giving up")
}

func (l *Ledger) get(ctx context.Context, seq, last int64) (*record.Entry, error) {
	if seq < 1 || seq > last {
		return nil, fmt.Errorf("seq %d does not exist (head is %d)", seq, last)
	}
	b, err := l.S.Get(ctx, record.Key(seq))
	if err != nil {
		return nil, err
	}
	return record.Parse(b)
}

func (l *Ledger) checkAmends(ctx context.Context, typ string, seq, last int64) error {
	t, err := l.get(ctx, seq, last)
	if err != nil {
		return fmt.Errorf("amends: %w", err)
	}
	if t.Type != typ {
		return fmt.Errorf("amends: seq %d is a %s, not a %s", seq, t.Type, typ)
	}
	if t.Amends != 0 {
		return fmt.Errorf("amends: seq %d is itself an amendment of %d; amend %d", seq, t.Amends, t.Amends)
	}
	if typ == record.TypeRetraction {
		return fmt.Errorf("retractions cannot be amended")
	}
	return nil
}

func (l *Ledger) checkRetract(ctx context.Context, seq, last int64) error {
	t, err := l.get(ctx, seq, last)
	if err != nil {
		return fmt.Errorf("retract: %w", err)
	}
	if t.Type == record.TypeRetraction || t.Amends != 0 {
		return fmt.Errorf("retract: seq %d must be an original record (not a retraction or amendment)", seq)
	}
	return nil
}
