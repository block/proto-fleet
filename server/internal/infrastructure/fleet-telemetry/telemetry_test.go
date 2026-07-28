package fleet_telemetry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	fleet_telemetry "github.com/block/proto-fleet/server/internal/infrastructure/fleet-telemetry"
)

// startWithRemoteSampledParent runs Setup with the given sample rate and starts a span
// under a remote sampled parent, returning whether the SDK sampled it.
func startWithRemoteSampledParent(t *testing.T, sampleRate float64) bool {
	t.Helper()

	shutdown, err := fleet_telemetry.Setup(context.Background(), "test", fleet_telemetry.Config{
		Enabled:    true,
		Endpoint:   "http://127.0.0.1:0",
		SampleRate: sampleRate,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		// Reset the globals Setup installed; shutdown errors are expected (nothing listens on the endpoint).
		_ = shutdown(context.Background())
		otel.SetTracerProvider(noop.NewTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	})

	traceID, err := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("b7ad6b7169203331")
	require.NoError(t, err)
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})

	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
	_, span := otel.GetTracerProvider().Tracer("test").Start(ctx, "op")
	defer span.End()
	return span.SpanContext().IsSampled()
}

func TestSetupSampleRateCapsRemoteSampledParents(t *testing.T) {
	// A client sampled flag must not bypass the configured rate (RUM/attacker-controlled input).
	require.False(t, startWithRemoteSampledParent(t, 0))
}

func TestSetupSampleRateOneKeepsRemoteSampledParents(t *testing.T) {
	require.True(t, startWithRemoteSampledParent(t, 1.0))
}
