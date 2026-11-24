package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReportingInputsHaveNoLifetimeFactCountCeiling(t *testing.T) {
	p, in := calculationFixture()
	in.Coverage = nil
	for i := range 12000 {
		in.Orders = append(in.Orders, Order{ID: fmt.Sprint(i), Name: "order", Start: p.Start, SourceRef: "source"})
	}
	require.NoError(t, in.ValidateContext(t.Context()))
}

func TestReportBreakdownLimitRejectsBeforeMaterializingMetrics(t *testing.T) {
	p, in := calculationFixture()
	in.Coverage = nil
	in.Orders = nil
	for i := range MaxReportBreakdowns + 1 {
		in.Orders = append(in.Orders, Order{ID: fmt.Sprint(i), Name: "order", Start: p.Start, SourceRef: "source"})
	}
	require.NoError(t, in.ValidateContext(t.Context()), "this is within the fact budget")
	result, err := CalculateContext(t.Context(), p, in)
	require.ErrorContains(t, err, "1024 supported breakdowns")
	require.Empty(t, result.Metrics)
	require.Empty(t, result.Orders)
	// Both alternative views consume the same materialization budget.
	empty := Result{Containers: make([]Breakdown, MaxReportBreakdowns)}
	_, err = MarshalResultContext(t.Context(), empty)
	require.NoError(t, err)
	empty.Orders = []Breakdown{{}}
	_, err = MarshalResultContext(t.Context(), empty)
	require.ErrorContains(t, err, "1024 supported breakdowns")
}

func TestCalculateRejectsSourceExpansionBeforeRetainingEveryBreakdown(t *testing.T) {
	measure := func(count int) uint64 {
		p, base := calculationFixture()
		in := Inputs{Configuration: base.Configuration}
		in.Configuration.Registers = nil
		for i := range 1000 {
			in.Configuration.Registers = append(in.Configuration.Registers, RegisterCompleteness{Kind: "incidents", Start: p.Start, End: p.End, SourceRef: fmt.Sprintf("register-%04d-%s", i, strings.Repeat("evidence", 10))})
		}
		for i := range count {
			id := fmt.Sprint(i)
			in.Orders = append(in.Orders, Order{ID: id, Name: "order", Start: p.Start, SourceRef: "order"})
			in.Coverage = append(in.Coverage, CoverageSegment{ID: id, AllocationIntervalID: int64(i + 1), Kind: 6, AssetID: int64(i + 1), SiteID: 1, ContainerID: int64(i + 1), OrderID: id, Start: p.Start, End: p.End, CapacityTHS: "100", SourceRef: "coverage"})
		}
		require.NoError(t, in.ValidateContext(t.Context()), "the expansion starts from admitted facts and configuration")
		payload, err := json.Marshal(in)
		require.NoError(t, err)
		require.Less(t, len(payload), 8<<20, "the input itself is within the saved-input byte limit")
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		result, err := CalculateContext(t.Context(), p, in)
		runtime.ReadMemStats(&after)
		require.ErrorContains(t, err, "supported byte limit")
		require.Empty(t, result, "an oversized calculation returns no partial result")
		return after.TotalAlloc - before.TotalAlloc
	}
	// Both estates exceed the same result budget. Eight times as many possible
	// breakdowns must not cause eight times as much retained construction work.
	small, large := measure(64), measure(512)
	t.Logf("source-expansion calculation allocated %d bytes for 128 possible breakdowns and %d for 1024", small, large)
	require.Less(t, large, 2*small, "stop constructing additional breakdowns when the cumulative result budget is spent")
}

func TestReportResultSerializationRejectsExpansionAndCancellation(t *testing.T) {
	result := Result{Metrics: map[string]Metric{"energy": {Sources: make([]string, 300)}}}
	reference := strings.Repeat("evidence", 4096)
	for i := range result.Metrics["energy"].Sources {
		result.Metrics["energy"].Sources[i] = reference
	}
	data, err := MarshalResultContext(t.Context(), result)
	require.ErrorContains(t, err, "supported byte limit")
	require.Nil(t, data, "oversized JSON must not be materialized")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = MarshalResultContext(ctx, Result{})
	require.ErrorIs(t, err, context.Canceled)
	// Escaping is included in the budget, not merely the source string length.
	result = Result{Sources: []string{strings.Repeat("<", MaxResultJSONBytes/6+1)}}
	_, err = MarshalResultContext(t.Context(), result)
	require.ErrorContains(t, err, "supported byte limit")
}

func TestCalculateContextCancelsHeavyMetricWork(t *testing.T) {
	p, in := calculationFixture()
	in.Coverage = nil
	for i := range 4000 {
		in.Coverage = append(in.Coverage, CoverageSegment{ID: fmt.Sprint(i), AllocationIntervalID: int64(i + 1), Kind: 6, AssetID: int64(i + 1), SiteID: 1, ContainerID: 1, OrderID: "a", Start: p.Start, CapacityTHS: "100", SourceRef: "coverage"})
		in.Hashrates = append(in.Hashrates, HashrateInterval{ID: fmt.Sprint(i), Kind: 6, AssetID: int64(i + 1), SiteID: 1, ContainerID: 1, OrderID: "a", SourceID: "hash", Start: p.Start, End: p.End, HashrateHS: "100000000000000", Complete: true, SourceRef: "source"})
	}
	require.NoError(t, in.ValidateContext(t.Context()))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := CalculateContext(ctx, p, in); done <- err }()
	timer := time.AfterFunc(5*time.Millisecond, cancel)
	defer timer.Stop()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("calculation ignored cancellation")
	}
}

type cancelReportWriter struct{ cancel context.CancelFunc }

func (w cancelReportWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }

func TestWrittenReportOutputLimitAndCancellation(t *testing.T) {
	r, _, id, _ := exportFixture()
	// Quotes cost two bytes in JSON and five when HTML-escaped.
	r.Interruptions[0].RootCause = strings.Repeat("\"", MaxWrittenReportBytes/5+1024)
	var out bytes.Buffer
	err := WriteWrittenReport(t.Context(), &out, r, id)
	require.ErrorContains(t, err, "supported byte limit")
	require.LessOrEqual(t, out.Len(), MaxWrittenReportBytes)
	r, _, id, _ = exportFixture()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.ErrorIs(t, WriteWrittenReport(ctx, cancelReportWriter{cancel}, r, id), context.Canceled)
	bounded := outputWriter{checkCtx: t.Context().Err, writer: io.Discard, remaining: 4, bounded: true}
	_, err = bounded.Write([]byte("1234"))
	require.NoError(t, err)
	_, err = bounded.Write([]byte("5"))
	require.ErrorContains(t, err, "supported byte limit")
}

func TestEvidenceRenderingHonorsCancellationDuringObservationStream(t *testing.T) {
	r, in, id, observations := exportFixture()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := WriteEvidenceContext(ctx, io.Discard, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error {
		require.NoError(t, yield(observations[0]))
		cancel()
		next := observations[0]
		next.Bucket = next.Bucket.Add(time.Hour)
		return yield(next)
	}})
	require.ErrorIs(t, err, context.Canceled)
}
