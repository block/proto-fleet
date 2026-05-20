package metrics

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// blockingStore lets a test pause inside InsertSamples so the
// backpressure-drop path becomes observable.
type blockingStore struct {
	*inMemoryStore
	gate chan struct{}
	once sync.Once
}

func newBlockingStore() *blockingStore {
	return &blockingStore{
		inMemoryStore: NewInMemoryStore(),
		gate:          make(chan struct{}),
	}
}

func (s *blockingStore) Release() {
	s.once.Do(func() { close(s.gate) })
}

func (s *blockingStore) InsertSamples(ctx context.Context, samples []Sample) error {
	select {
	case <-s.gate:
	case <-ctx.Done():
		return fmt.Errorf("failed to insert samples: %w", ctx.Err())
	}
	return s.inMemoryStore.InsertSamples(ctx, samples)
}

// every Emit* method lands a row in TimescaleDB for the matching
// metric name. This is the contract-coverage test that used to assert
// against the OTLP export path.
func TestEmitsPersistContractMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := NewInMemoryStore()
	provider := SetupWithStore(ctx, "test", Config{
		Enabled:       true,
		FlushInterval: 50 * time.Millisecond,
		BufferSize:    64,
		BatchSize:     32,
	}, store)
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

	// Shutdown flushes the buffer. Don't rely on a tick — we want the
	// test to fail loudly if the drain path regresses.
	require.NoError(t, provider.Shutdown(ctx))

	got := map[string]int{}
	for _, sample := range store.Snapshot() {
		got[sample.Metric]++
	}

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
		require.GreaterOrEqual(t, got[name], 1, "expected at least one sample for %q", name)
	}
}

// labels and the recorded value match what callers passed in.
func TestEmitPreservesLabelsAndValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := NewInMemoryStore()
	provider := SetupWithStore(ctx, "test", Config{
		Enabled:       true,
		FlushInterval: 25 * time.Millisecond,
		BufferSize:    16,
		BatchSize:     8,
	}, store)

	provider.EmitDeviceOnline(ctx, DeviceLabels{
		OrganizationID: "org-7",
		DeviceID:       "device-42",
		DeviceGroup:    "rack-3",
		Driver:         "antminer",
	}, false)
	require.NoError(t, provider.Shutdown(ctx))

	samples := store.Snapshot()
	require.Len(t, samples, 1)
	require.Equal(t, MetricDeviceOnline, samples[0].Metric)
	require.Equal(t, "org-7", samples[0].Labels.OrganizationID)
	require.Equal(t, "device-42", samples[0].Labels.DeviceID)
	require.Equal(t, "rack-3", samples[0].Labels.DeviceGroup)
	require.Equal(t, "antminer", samples[0].Labels.Driver)
	require.Equal(t, 0.0, samples[0].Value)
	require.False(t, samples[0].Time.IsZero(), "Provider.record should stamp Sample.Time")
}

// when notifications are disabled the provider stays a fast no-op and
// never reaches for the store.
func TestSetupDisabledIsNoOp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	provider, err := Setup(ctx, "test", Config{Enabled: false}, nil)
	require.NoError(t, err)
	require.False(t, provider.Enabled())

	labels := DeviceLabels{OrganizationID: "org-1", DeviceID: "device-1"}
	provider.EmitDeviceOnline(ctx, labels, false)
	provider.EmitDeviceHashrate(ctx, labels, 0, 0)
	provider.EmitDeviceTemperature(ctx, labels, SensorKindBoard, 0, 0)
	provider.EmitDevicePoolConnected(ctx, labels, false)
	provider.EmitCommand(ctx, CommandLabels{Kind: "reboot", Result: ResultSuccess})
	provider.EmitTelemetryPoll(ctx, TelemetryPollLabels{Result: ResultSuccess})

	require.NoError(t, provider.Shutdown(ctx))
}

// Setup refuses to start an enabled provider with a nil DB. Catching
// this at startup is much friendlier than a NullPointer panic on the
// first emit.
func TestSetupEnabledRequiresDB(t *testing.T) {
	_, err := Setup(context.Background(), "test", Config{Enabled: true}, nil)
	require.Error(t, err)
}

// when the buffered channel is full, record() drops the new sample
// rather than blocking the caller. This is what protects the telemetry
// hot path under TimescaleDB outage.
func TestRecordDropsOnFullBuffer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newBlockingStore()
	provider := SetupWithStore(ctx, "test", Config{
		Enabled:       true,
		FlushInterval: 10 * time.Millisecond,
		BufferSize:    2,
		BatchSize:     1,
	}, store)

	// Fill the buffer + force at least one drop. The flusher is
	// blocked inside store.InsertSamples until we release it.
	for range 32 {
		provider.EmitDeviceOnline(ctx, DeviceLabels{DeviceID: "x"}, true)
	}
	require.Greater(t, provider.DroppedSamples(), uint64(0),
		"expected at least one dropped sample when the buffer is saturated")

	store.Release()
	require.NoError(t, provider.Shutdown(ctx))
}
