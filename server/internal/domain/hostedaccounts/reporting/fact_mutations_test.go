package reporting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFactMutationEnvelopeRejectsInvalidOperations(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 123, time.UTC)
	f, e := EncodeFact(Order{ID: "界", Name: "Order", Start: at, SourceRef: "signed"})
	require.NoError(t, e)
	valid := FactMutation{Kind: FactOrder, ID: f.ID, Payload: f.Payload}
	good, e := PrepareFactMutations(t.Context(), []FactMutation{valid})
	require.NoError(t, e)
	require.Equal(t, valid, good[0])
	for _, test := range []struct {
		name    string
		changes []FactMutation
	}{
		{"wrong register", []FactMutation{{Kind: "unknown", ID: "x", Payload: f.Payload}}},
		{"mismatched identity", []FactMutation{{Kind: FactOrder, ID: "other", Payload: f.Payload}}},
		{"unknown", []FactMutation{{Kind: FactOrder, ID: "x", Payload: json.RawMessage(`{"id":"x","start":"2026-01-01T00:00:00Z","unknown":1}`)}}},
		{"delete payload", []FactMutation{{Kind: FactOrder, ID: "x", Payload: json.RawMessage(`null`), Delete: true}}},
		{"duplicate", []FactMutation{valid, valid}},
		{"whitespace identity", []FactMutation{{Kind: FactOrder, ID: "\u2003", Delete: true}}},
		{"invalid UTF8 identity", []FactMutation{{Kind: FactOrder, ID: string([]byte{255}), Delete: true}}},
		{"count", make([]FactMutation, MaxFactMutationsPerRequest+1)},
	} {
		t.Run(test.name, func(t *testing.T) { _, e := PrepareFactMutations(t.Context(), test.changes); require.Error(t, e) })
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = PrepareFactMutations(ctx, []FactMutation{valid})
	require.ErrorIs(t, e, context.Canceled)
}

func TestFactMutationBudgetMeasuresCanonicalEnvelope(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f, e := EncodeFact(PoolObservation{ID: "pool", Kind: 7, AssetID: 1, SourceID: "source", SourceAt: at, ReceivedAt: at, State: "unknown", SourceRef: "reviewed", Reason: strings.Repeat("<", MaxFactMutationBytes/6)})
	require.NoError(t, e)
	_, e = PrepareFactMutations(t.Context(), []FactMutation{{Kind: f.Kind, ID: f.ID, Payload: f.Payload}})
	require.ErrorContains(t, e, "byte budget")
	// Escaped IDs and the complete structural envelope count even for deletions.
	changes := []FactMutation{}
	for i := range MaxFactMutationsPerRequest {
		changes = append(changes, FactMutation{Kind: FactOrder, ID: strings.Repeat("<", 1800) + string(rune(0x4e00+i)), Delete: true})
	}
	_, e = PrepareFactMutations(t.Context(), changes)
	require.ErrorContains(t, e, "byte budget")
}

func TestFactMutationLocalValidationIncludesEveryRegister(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	values := []any{
		Order{ID: "id", Start: at}, CoverageSegment{ID: "id", Start: at}, ModuleCoverageSegment{ID: "id", Start: at}, MeterInterval{ID: "id", Start: at}, FeeLine{ID: "id", Start: at}, Incident{ID: "id", Start: at}, HashrateInterval{ID: "id", Start: at}, PoolObservation{ID: "id", SourceAt: at}, ObligationInterval{ID: "id", Start: at}, ContractResult{ID: "id", Start: at},
	}
	for _, v := range values {
		f, e := EncodeFact(v)
		require.NoError(t, e)
		t.Run(string(f.Kind), func(t *testing.T) {
			_, e := PrepareFactMutations(t.Context(), []FactMutation{{Kind: f.Kind, ID: f.ID, Payload: f.Payload}})
			require.Error(t, e)
		})
	}
}
