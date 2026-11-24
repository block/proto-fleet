package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

const (
	MaxFactMutationsPerRequest     = 200
	MaxFactMutationBytes           = 1 << 20
	MaxReportingConfigurationBytes = 1 << 20
)

type FactKey struct {
	Kind FactKind `json:"kind"`
	ID   string   `json:"id"`
}

// FactMutation identifies one upsert or deletion; projections are server derived.
type FactMutation struct {
	Kind    FactKind        `json:"kind"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Delete  bool            `json:"delete,omitempty"`
}

// PrepareFactMutations validates and canonicalizes a bounded operation envelope.
// Configuration-only operations may pass an empty mutation list.
func PrepareFactMutations(ctx context.Context, mutations []FactMutation) ([]FactMutation, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("prepare reporting mutations: %w", err)
	}
	if len(mutations) > MaxFactMutationsPerRequest {
		return nil, fmt.Errorf("invalid reporting fact mutation count")
	}
	out := make([]FactMutation, 0, len(mutations))
	seen := map[FactKey]bool{}
	for _, m := range mutations {
		if e := ctx.Err(); e != nil {
			return nil, fmt.Errorf("prepare reporting mutations: %w", e)
		}
		if !m.Kind.Valid() || !text(m.ID) || !utf8.ValidString(m.ID) {
			return nil, fmt.Errorf("invalid reporting fact mutation identity")
		}
		key := FactKey{m.Kind, m.ID}
		if seen[key] {
			return nil, fmt.Errorf("duplicate reporting fact mutation identity")
		}
		seen[key] = true
		if m.Delete {
			if len(m.Payload) != 0 {
				return nil, fmt.Errorf("deleted reporting fact must not have a payload")
			}
		} else {
			if len(m.Payload) > MaxFactMutationBytes {
				return nil, fmt.Errorf("reporting fact mutations exceed byte budget")
			}
			f, v, e := DecodeFact(m.Kind, m.Payload)
			if e != nil {
				return nil, e
			}
			if f.ID != m.ID {
				return nil, fmt.Errorf("reporting fact identity does not match payload")
			}
			if e = ValidateFactFields(v); e != nil {
				return nil, fmt.Errorf("%s %q: %w", m.Kind, m.ID, e)
			}
			m.Payload = f.Payload
		}
		out = append(out, m)
	}
	envelope, e := json.Marshal(out)
	if e != nil {
		return nil, fmt.Errorf("encode reporting mutations: %w", e)
	}
	if len(envelope) > MaxFactMutationBytes {
		return nil, fmt.Errorf("reporting fact mutations exceed byte budget")
	}
	return out, nil
}
