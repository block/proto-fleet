package fleet_telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestSetupUsesExpectedOTLPTracePath(t *testing.T) {
	tests := []struct {
		name         string
		endpointPath string
		wantPath     string
	}{
		{name: "base endpoint", wantPath: "/v1/traces"},
		{name: "base endpoint with trailing slash", endpointPath: "/", wantPath: "/v1/traces"},
		{name: "explicit trace endpoint", endpointPath: "/custom/traces", wantPath: "/custom/traces"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestPaths := make(chan string, 1)
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestPaths <- r.URL.Path
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			t.Cleanup(collector.Close)

			shutdown, err := fleet_telemetry.Setup(t.Context(), "test", fleet_telemetry.Config{
				Enabled:     true,
				Endpoint:    collector.URL + test.endpointPath,
				ServiceName: "test",
				SampleRate:  1.0,
			})
			require.NoError(t, err)
			t.Cleanup(func() {
				otel.SetTracerProvider(noop.NewTracerProvider())
				otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
			})

			_, span := otel.Tracer("test").Start(t.Context(), "operation")
			span.End()
			require.NoError(t, shutdown(t.Context()))
			select {
			case requestPath := <-requestPaths:
				require.Equal(t, test.wantPath, requestPath)
			default:
				t.Fatal("collector received no trace request")
			}
		})
	}
}
