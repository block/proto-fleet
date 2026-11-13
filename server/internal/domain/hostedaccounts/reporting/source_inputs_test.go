package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSourceInputsPreserveUsedValuesAndOmitOtherPeriods(t *testing.T) {
	p, in := calculationFixture()
	in.Configuration.SharedAllocationRef = "host allocation worksheet"
	meter := MeterInterval{ID: "used", MeterID: "raw-meter", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.Start.Add(time.Hour), KWh: "100.000000000000000001", AllocationFraction: "0.25", Quality: "measured", SourceRef: "host original"}
	other := meter
	other.ID = "future"
	other.Start = p.End
	other.End = p.End.Add(time.Hour)
	in.Meters = []MeterInterval{meter, other}
	in.PoolObservations = []PoolObservation{{ID: "pool", Kind: 6, AssetID: 1, SourceID: "pool", SourceAt: p.Start, ReceivedAt: p.Start, State: "unknown", SourceRef: "pool evidence"}}
	original, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := sourceInputs(t.Context(), p, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Meters) != 1 || selected.Meters[0] != meter {
		t.Fatalf("source values changed: %#v", selected.Meters)
	}
	if !reflect.DeepEqual(calculate(t, p, in), calculate(t, p, selected)) {
		t.Fatal("selected sources changed the calculated result")
	}
	after, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Fatal("source selection mutated current inputs")
	}
	var file bytes.Buffer
	if err := writeFixtureSourceInputs(t.Context(), &file, p, in); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(file.Bytes(), []byte("100.000000000000000001")) || bytes.Contains(file.Bytes(), []byte(`"future"`)) {
		t.Fatalf("unexpected frozen evidence: %s", file.Bytes())
	}
}

func TestSourceInputsKeepPriorYearAndEpisodeContext(t *testing.T) {
	p, in := calculationFixture()
	p.Kind = "annual"
	p.End = p.Start.AddDate(1, 0, 0)
	p.AsOf = p.End.Add(time.Hour)
	in.Coverage[0].Start = p.Start.AddDate(-1, 0, 0)
	in.Meters = []MeterInterval{{ID: "prior", Start: p.Start.AddDate(-1, 0, 0), End: p.Start}, {ID: "earlier", Start: p.Start.AddDate(-2, 0, 0), End: p.Start.AddDate(-1, 0, 0)}}
	in.Obligations = []ObligationInterval{{ID: "episode-start", EpisodeID: "episode", Kind: 6, AssetID: 1, Start: p.Start.AddDate(-2, 0, 0), End: p.Start.AddDate(-1, 0, 0), ReceivedAt: p.Start.AddDate(-2, 0, 0)}}
	selected, err := sourceInputs(t.Context(), p, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Meters) != 1 || selected.Meters[0].ID != "prior" || len(selected.Obligations) != 1 {
		t.Fatal("lost comparison or true episode-start context")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := writeFixtureSourceInputs(ctx, &bytes.Buffer{}, p, in); err == nil {
		t.Fatal("cancelled publication retained sources")
	}
}

type sourceStreamSink struct{ total, maxWrite int }

func (s *sourceStreamSink) Write(p []byte) (int, error) {
	s.total += len(p)
	s.maxWrite = max(s.maxWrite, len(p))
	return len(p), nil
}

func TestSourceInputsStreamBoundsBufferingAndStopsOnCancellation(t *testing.T) {
	p, in := calculationFixture()
	payload := json.RawMessage(`{"id":"fact","sourceRef":"` + strings.Repeat("s", 4096) + `"}`)
	sink := &sourceStreamSink{}
	ctx, cancel := context.WithCancel(t.Context())
	err := WriteSourceInputsStream(ctx, sink, p, in.Configuration, func(kind FactKind, yield func(json.RawMessage) error) error {
		if kind != FactFee {
			return nil
		}
		for i := range 4096 {
			if err := yield(payload); err != nil {
				return err
			}
			if i == 4095 {
				cancel()
			}
		}
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Greater(t, sink.total, 16<<20)
	require.LessOrEqual(t, sink.maxWrite, len(payload), "writer never encodes a register or complete artifact")
}

func TestSourceInputsStreamEmitsEmptyArraysAndPreservesOriginalJSON(t *testing.T) {
	p, in := calculationFixture()
	var file bytes.Buffer
	original := json.RawMessage(`{"id":"original","amount":"1.00000000000000000001","start":"2026-09-01T00:00:00.000000123Z"}`)
	err := WriteSourceInputsStream(t.Context(), &file, p, in.Configuration, func(kind FactKind, yield func(json.RawMessage) error) error {
		if kind == FactFee {
			return yield(original)
		}
		return nil
	})
	require.NoError(t, err)
	var envelope struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(file.Bytes(), &envelope))
	for _, name := range []string{"orders", "coverage", "moduleCoverage", "meters", "incidents", "hashrates", "poolObservations", "obligations", "externalResults"} {
		require.JSONEq(t, "[]", string(envelope.Inputs[name]))
	}
	require.JSONEq(t, "["+string(original)+"]", string(envelope.Inputs["fees"]))
}
