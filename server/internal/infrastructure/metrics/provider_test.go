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

// when InsertSamples fails the flusher must not silently discard the
// batch. The retry buffer should keep failed samples around so that a
// later successful flush (after the transient error clears) still
// persists them.
func TestFailedFlushRetainsBatchForRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := NewInMemoryStore()
	// Fail the next flush. Shutdown flushes once with this error in
	// place, then we clear the error and re-flush via a second
	// provider that shares the same store-style assertion.
	store.SetError(fmt.Errorf("transient timescaledb outage"))

	provider := SetupWithStore(ctx, "test", Config{
		Enabled:         true,
		FlushInterval:   25 * time.Millisecond,
		BufferSize:      16,
		BatchSize:       4,
		RetryBufferSize: 64,
		MaxRetryBackoff: 50 * time.Millisecond,
	}, store)

	for range 4 {
		provider.EmitDeviceOnline(ctx, DeviceLabels{
			OrganizationID: "org-1", DeviceID: "device-1",
		}, true)
	}

	// Give the flusher a chance to attempt and fail at least once.
	require.Eventually(t, func() bool {
		return provider.FailedFlushes() > 0
	}, time.Second, 5*time.Millisecond, "expected at least one failed flush")

	// Clear the error, then wait for a successful flush to drain the
	// retry buffer into the store.
	store.SetError(nil)
	require.Eventually(t, func() bool {
		return len(store.Snapshot()) >= 4
	}, time.Second, 5*time.Millisecond, "expected retry buffer to drain after the error cleared")

	require.NoError(t, provider.Shutdown(ctx))
	require.Equal(t, uint64(0), provider.RetryDroppedSamples(),
		"no samples should have been dropped when the retry buffer was sized to hold them")
}

// when the retry buffer cap is exceeded during a sustained outage the
// oldest samples are dropped FIFO and counted in retryDropped (not in
// dropped — those are reserved for hot-path backpressure).
func TestRetryBufferOverflowCountsRetryDropped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := NewInMemoryStore()
	store.SetError(fmt.Errorf("permanent timescaledb outage"))

	provider := SetupWithStore(ctx, "test", Config{
		Enabled:         true,
		FlushInterval:   10 * time.Millisecond,
		BufferSize:      32,
		BatchSize:       4,
		RetryBufferSize: 4, // tiny — overflow on the second failed batch
		MaxRetryBackoff: 10 * time.Millisecond,
	}, store)

	for range 16 {
		provider.EmitDeviceOnline(ctx, DeviceLabels{
			OrganizationID: "org-1", DeviceID: "device-1",
		}, true)
	}

	require.Eventually(t, func() bool {
		return provider.RetryDroppedSamples() > 0
	}, time.Second, 5*time.Millisecond,
		"expected the retry buffer to overflow under a sustained store error")

	require.NoError(t, provider.Shutdown(ctx))
	require.Greater(t, provider.FailedFlushes(), uint64(0))
}

// chunkRecordingStore captures the size of every InsertSamples call so
// the chunked-flush regression test can assert each chunk stayed under
// the bind-parameter cap. The mutex guards both the recorded sizes and
// the error/sample state inherited from inMemoryStore (which has its
// own lock — we use a separate one here to keep the call-size slice in
// the same lock as the err-toggle).
type chunkRecordingStore struct {
	*inMemoryStore
	mu         sync.Mutex
	chunkSizes []int
}

func newChunkRecordingStore() *chunkRecordingStore {
	return &chunkRecordingStore{inMemoryStore: NewInMemoryStore()}
}

func (s *chunkRecordingStore) InsertSamples(ctx context.Context, samples []Sample) error {
	s.mu.Lock()
	s.chunkSizes = append(s.chunkSizes, len(samples))
	s.mu.Unlock()
	return s.inMemoryStore.InsertSamples(ctx, samples)
}

func (s *chunkRecordingStore) ChunkSizes() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int, len(s.chunkSizes))
	copy(out, s.chunkSizes)
	return out
}

// withMaxSamplesPerInsert shrinks the package-level bind-parameter cap
// for the duration of the test. Restoring on cleanup keeps parallel
// tests safe — they only need to opt in.
func withMaxSamplesPerInsert(t *testing.T, n int) {
	t.Helper()
	prev := maxSamplesPerInsert
	maxSamplesPerInsert = n
	t.Cleanup(func() { maxSamplesPerInsert = prev })
}

// when the retry queue grows past the bind-parameter cap, flush must
// chunk it into pieces small enough that the driver will accept, and
// must retry failed chunks individually rather than wedging on the
// whole buffer. Regression for the "metrics ingest permanently
// unflushable after a transient outage" path.
func TestRetryQueueChunkedBelowBindParameterLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shrink the cap so we don't need thousands of samples to exercise
	// the chunking path. With a cap of 4 and a 32-sample backlog we
	// expect eight chunks on the first successful drain.
	withMaxSamplesPerInsert(t, 4)

	store := newChunkRecordingStore()
	store.SetError(fmt.Errorf("transient timescaledb outage"))

	provider := SetupWithStore(ctx, "test", Config{
		Enabled:         true,
		FlushInterval:   25 * time.Millisecond,
		BufferSize:      128,
		BatchSize:       32, // > maxSamplesPerInsert: forces chunking on its own
		RetryBufferSize: 128,
		MaxRetryBackoff: 50 * time.Millisecond,
	}, store)

	const emitted = 32
	for range emitted {
		provider.EmitDeviceOnline(ctx, DeviceLabels{
			OrganizationID: "org-1", DeviceID: "device-1",
		}, true)
	}

	// Wait for at least one failed flush so the retry buffer fills.
	require.Eventually(t, func() bool {
		return provider.FailedFlushes() > 0
	}, time.Second, 5*time.Millisecond, "expected at least one failed flush")

	// Clear the error and wait for the retry buffer to drain through
	// successful chunks.
	store.SetError(nil)
	require.Eventually(t, func() bool {
		return len(store.Snapshot()) >= emitted
	}, 2*time.Second, 5*time.Millisecond, "expected all samples to drain after the error cleared")

	require.NoError(t, provider.Shutdown(ctx))
	require.Equal(t, uint64(0), provider.RetryDroppedSamples(),
		"no samples should have been dropped when the retry buffer was sized to hold them")

	// Every call to InsertSamples — both the failed attempts and the
	// successful drain — must respect the bind-parameter cap. This is
	// the actual regression assertion: a pre-chunking flush would send
	// the whole 32-sample retry buffer in one call and trip the limit.
	for i, n := range store.ChunkSizes() {
		require.LessOrEqualf(t, n, 4,
			"chunk %d had %d samples, exceeds maxSamplesPerInsert=%d",
			i, n, 4)
	}
}

// when the buffered channel is full, record() drops samples rather
// than blocking the caller. This is what protects the telemetry hot
// path under TimescaleDB outage.
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

// when the buffer is saturated, record() drops the *oldest* queued
// sample rather than the new one. Without this, a long TimescaleDB
// stall combined with a burst of gauge updates would persist the stale
// state and lose every recovery sample, leaving alerts firing on data
// that no longer reflects reality. Regression for the
// "BufferSize doc says oldest, code dropped newest" review finding.
func TestRecordDropsOldestNotNewest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newBlockingStore()
	provider := SetupWithStore(ctx, "test", Config{
		Enabled:       true,
		FlushInterval: 10 * time.Millisecond,
		BufferSize:    2,
		BatchSize:     1,
	}, store)

	// Prime the flusher with a single sample so it reads one off the
	// channel and parks inside store.InsertSamples. From that point
	// on the channel is the only buffer in front of the gate and
	// every drop has to come out of it.
	provider.EmitDeviceOnline(ctx, DeviceLabels{DeviceID: "priming"}, true)
	require.Eventually(t, func() bool {
		return len(provider.samples) == 0
	}, time.Second, 5*time.Millisecond,
		"expected the flusher to drain the priming sample and block in InsertSamples")

	// Emit a sequence of samples with distinct DeviceID labels. With
	// BufferSize=2 and the flusher blocked, only the two newest must
	// survive in the channel.
	const total = 16
	for i := 1; i <= total; i++ {
		provider.EmitDeviceOnline(ctx, DeviceLabels{DeviceID: fmt.Sprintf("d%02d", i)}, true)
	}

	require.GreaterOrEqual(t, provider.DroppedSamples(), uint64(total-2),
		"expected drop-oldest to discard everything but the two newest samples")

	// Release the blocked flusher and let Shutdown drain the rest.
	store.Release()
	require.NoError(t, provider.Shutdown(ctx))

	// The newest emit must reach the store. If record() were dropping
	// the newest sample on a full buffer (the bug we're guarding
	// against), the very last DeviceID would never have made it in.
	landedIDs := map[string]bool{}
	for _, sample := range store.inMemoryStore.Snapshot() {
		landedIDs[sample.Labels.DeviceID] = true
	}
	require.True(t, landedIDs[fmt.Sprintf("d%02d", total)],
		"newest device sample (d%02d) must survive — drop-oldest, not drop-newest", total)
}
