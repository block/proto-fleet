package metrics

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	collectormetricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"
)

type fakeOTLPMetricsServer struct {
	collectormetricpb.UnimplementedMetricsServiceServer

	mu       sync.Mutex
	requests []*collectormetricpb.ExportMetricsServiceRequest
	received chan struct{}
}

func newFakeOTLPMetricsServer() *fakeOTLPMetricsServer {
	return &fakeOTLPMetricsServer{
		received: make(chan struct{}, 1),
	}
}

func (s *fakeOTLPMetricsServer) Export(_ context.Context, req *collectormetricpb.ExportMetricsServiceRequest) (*collectormetricpb.ExportMetricsServiceResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	select {
	case s.received <- struct{}{}:
	default:
	}
	return &collectormetricpb.ExportMetricsServiceResponse{}, nil
}

func (s *fakeOTLPMetricsServer) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var names []string
	for _, req := range s.requests {
		for _, rm := range req.GetResourceMetrics() {
			for _, sm := range rm.GetScopeMetrics() {
				for _, m := range sm.GetMetrics() {
					names = append(names, m.GetName())
				}
			}
		}
	}
	return names
}

// startFakeServer brings up the receiver on a random port and returns the endpoint URL.
func startFakeServer(t *testing.T) (*fakeOTLPMetricsServer, string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	fake := newFakeOTLPMetricsServer()
	collectormetricpb.RegisterMetricsServiceServer(srv, fake)

	go func() { _ = srv.Serve(lis) }()

	endpoint := "http://" + lis.Addr().String()
	cleanup := func() {
		srv.GracefulStop()
		_ = lis.Close()
	}
	return fake, endpoint, cleanup
}

func TestSetupExportsContractMetrics(t *testing.T) {
	fake, endpoint, stop := startFakeServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		Enabled:  true,
		Endpoint: endpoint,
		Interval: 200 * time.Millisecond,
	}

	provider, err := Setup(ctx, "test", cfg)
	require.NoError(t, err)
	require.True(t, provider.Enabled())

	labels := DeviceLabels{
		OrganizationID: "org-1",
		DeviceID:       "device-1",
		DeviceGroup:    "group-a",
		Driver:         "virtual",
	}

	provider.EmitDeviceOnline(ctx, labels, true)
	provider.EmitDeviceHashrate(ctx, labels, 110.5, 115.0)
	provider.EmitDeviceTemperature(ctx, labels, SensorKindBoard, 75.0, 70.0)
	provider.EmitDevicePoolConnected(ctx, labels, true)
	provider.EmitCommand(ctx, CommandLabels{
		OrganizationID: labels.OrganizationID,
		Kind:           "reboot",
		Result:         ResultSuccess,
	})
	provider.EmitTelemetryPoll(ctx, TelemetryPollLabels{
		OrganizationID: labels.OrganizationID,
		DeviceID:       labels.DeviceID,
		Result:         ResultSuccess,
	})

	// Wait for the periodic reader to push at least once.
	select {
	case <-fake.received:
	case <-time.After(5 * time.Second):
		t.Fatal("OTLP receiver never received an export")
	}

	require.NoError(t, provider.Shutdown(ctx))

	got := fake.names()
	want := []string{
		MetricDeviceOnline,
		MetricDeviceHashrateTerahash,
		MetricDeviceHashrateExpectedTerahash,
		MetricDeviceTemperatureMaxCelsius,
		MetricDeviceTemperatureAvgCelsius,
		MetricDevicePoolConnected,
		MetricCommandTotal,
		MetricTelemetryPollTotal,
	}
	for _, name := range want {
		require.Contains(t, got, name, "expected metric %q in OTLP export", name)
	}
}

// disabling the metrics package leaves the rest of the system functional
func TestSetupDisabledIsNoOp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	provider, err := Setup(ctx, "test", Config{Enabled: false})
	require.NoError(t, err)
	require.False(t, provider.Enabled())

	// These must not panic and must not block.
	labels := DeviceLabels{OrganizationID: "org-1", DeviceID: "device-1"}
	provider.EmitDeviceOnline(ctx, labels, false)
	provider.EmitDeviceHashrate(ctx, labels, 0, 0)
	provider.EmitDeviceTemperature(ctx, labels, SensorKindBoard, 0, 0)
	provider.EmitDevicePoolConnected(ctx, labels, false)
	provider.EmitCommand(ctx, CommandLabels{Kind: "reboot", Result: ResultSuccess})
	provider.EmitTelemetryPoll(ctx, TelemetryPollLabels{Result: ResultSuccess})

	require.NoError(t, provider.Shutdown(ctx))
}
