package reporting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFactEncodingDerivesIndexFieldsFromEveryRegister(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 123, time.UTC)
	end := start.Add(time.Hour)
	received := end.Add(time.Hour)
	for _, tc := range []struct {
		value       any
		kind        FactKind
		site, asset int64
		assetKind   int32
	}{
		{Order{ID: "id", Start: start, End: end}, FactOrder, 0, 0, 0},
		{CoverageSegment{ID: "id", AllocationIntervalID: 9, Start: start, End: end, SiteID: 1, AssetID: 2, Kind: 7}, FactCoverage, 1, 2, 7},
		{ModuleCoverageSegment{ID: "id", Start: start, End: end, SiteID: 1, ModuleID: 2, BoardCoverageIDs: []string{"board"}}, FactModuleCoverage, 1, 2, 6},
		{MeterInterval{ID: "id", Start: start, End: end, SiteID: 1, MeterID: "physical"}, FactMeter, 1, 0, 0},
		{FeeLine{ID: "id", Start: start, End: end, SiteID: 1}, FactFee, 1, 0, 0},
		{Incident{ID: "id", Start: start, SiteID: 1}, FactIncident, 1, 0, 0},
		{HashrateInterval{ID: "id", Start: start, End: end, SiteID: 1, AssetID: 2, Kind: 7}, FactHashrate, 1, 2, 7},
		{PoolObservation{ID: "id", SourceAt: start, ReceivedAt: received, AssetID: 2, Kind: 7}, FactPoolObservation, 0, 2, 7},
		{ObligationInterval{ID: "id", EpisodeID: "修理", Start: start, End: end, ReceivedAt: received, SiteID: 1, AssetID: 2, Kind: 6}, FactObligation, 1, 2, 6},
		{ContractResult{ID: "id", Start: start, End: end, SiteID: 1}, FactExternalResult, 1, 0, 0},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			encoded, err := EncodeFact(tc.value)
			require.NoError(t, err)
			require.Equal(t, tc.kind, encoded.Kind)
			require.Equal(t, "id", encoded.ID)
			require.Equal(t, start, encoded.Start)
			require.Equal(t, tc.site, encoded.SiteID)
			require.Equal(t, tc.asset, encoded.AssetID)
			require.Equal(t, tc.assetKind, encoded.AssetKind)
			if tc.kind == FactCoverage {
				require.EqualValues(t, 9, encoded.AllocationIntervalID)
			}
			if tc.kind == FactObligation {
				require.Equal(t, "修理", encoded.EpisodeID)
			}
			decoded, value, err := DecodeFact(tc.kind, encoded.Payload)
			require.NoError(t, err)
			require.Equal(t, tc.value, value)
			require.Equal(t, encoded, decoded)
			if tc.kind == FactPoolObservation || tc.kind == FactObligation {
				require.Equal(t, received, encoded.ReceivedAt)
			}
		})
	}
}

func TestFactDecodingRejectsMalformedAndMismatchedPayloads(t *testing.T) {
	for _, tc := range []struct {
		kind    FactKind
		payload string
	}{
		{"unknown", `{}`},
		{FactOrder, `null`},
		{FactOrder, `[]`},
		{FactOrder, `{"id":"a","start":"2026-01-01T00:00:00Z","unknown":1}`},
		{FactOrder, `{"id":"a","start":"2026-01-01T00:00:00Z"}{}`},
		{FactOrder, `{"id":"","start":"2026-01-01T00:00:00Z"}`},
		{FactOrder, `{"id":"a"}`},
	} {
		_, _, err := DecodeFact(tc.kind, []byte(tc.payload))
		require.Error(t, err, "%s: %s", tc.kind, tc.payload)
	}
	_, err := EncodeFact("unsupported")
	require.Error(t, err)
}
