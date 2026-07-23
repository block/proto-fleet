package telemetry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics"
	minerInterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	mock "github.com/block/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// samplingHarness bundles a TelemetryService with strict gomock collaborators.
// gomock fails the test on any un-expected call, so every test doubles as a
// proof that sampling performs no persistence, scheduler, firmware, pairing,
// or diagnostics side effects beyond what it explicitly expects.
type samplingHarness struct {
	service     *TelemetryService
	minerGetter *mock.MockCachedMinerGetter
	miner       *minerMocks.MockMiner
	scheduler   *mock.MockUpdateScheduler
	dataStore   *mock.MockTelemetryDataStore
	deviceStore *storesMocks.MockDeviceStore
	errorPoller *mock.MockErrorPoller
}

func newSamplingHarness(t *testing.T, config Config) *samplingHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	h := &samplingHarness{
		minerGetter: mock.NewMockCachedMinerGetter(ctrl),
		miner:       minerMocks.NewMockMiner(ctrl),
		scheduler:   mock.NewMockUpdateScheduler(ctrl),
		dataStore:   mock.NewMockTelemetryDataStore(ctrl),
		deviceStore: storesMocks.NewMockDeviceStore(ctrl),
		errorPoller: mock.NewMockErrorPoller(ctrl),
	}
	h.service = NewTelemetryService(config, h.dataStore, h.minerGetter, h.scheduler, h.deviceStore, h.errorPoller)
	h.miner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	h.miner.EXPECT().GetSiteID().Return(int64(1)).AnyTimes()
	h.miner.EXPECT().GetDriverName().Return("test-driver").AnyTimes()
	return h
}

func samplingTestConfig() Config {
	return Config{
		StalenessThreshold: time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   2,
		MetricTimeout:      2 * time.Second,
	}
}

// startWorkers runs n shared pool workers until the test ends.
func (h *samplingHarness) startWorkers(t *testing.T, n int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	for range n {
		go h.service.worker(ctx)
	}
}

func sampleMetricsFixture(deviceID models.DeviceIdentifier, powerW float64) modelsV2.DeviceMetrics {
	return modelsV2.DeviceMetrics{
		DeviceIdentifier: string(deviceID),
		Timestamp:        time.Now(),
		Health:           modelsV2.HealthHealthyActive,
		PowerW:           &modelsV2.MetricValue{Value: powerW},
	}
}

// requireEventuallyReleased waits for the device's in-flight claim to clear.
func requireEventuallyReleased(t *testing.T, service *TelemetryService, deviceID models.DeviceIdentifier) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, held := service.inFlight.Load(deviceID)
		return !held
	}, 2*time.Second, 5*time.Millisecond, "in-flight claim for %s was not released", deviceID)
}

// A qualifying recently completed sample is reused without a second device call.
func TestSampleDeviceMetrics_ReusesFreshSample(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("reuse-device")

	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(sampleMetricsFixture(deviceID, 3200), nil).Times(1)

	bound := time.Now()
	first := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID, SampledAfter: bound}})
	require.Len(t, first, 1)
	require.NoError(t, first[0].Err)
	assert.Equal(t, SampleSourceDirect, first[0].Source)
	assert.True(t, first[0].FlightStart.After(bound))
	requireEventuallyReleased(t, h.service, deviceID)

	// Second request with the same bound: satisfied from retention, and the
	// Times(1) expectations above prove no second device call happened.
	second := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID, SampledAfter: bound}})
	require.Len(t, second, 1)
	require.NoError(t, second[0].Err)
	assert.Equal(t, SampleSourceReused, second[0].Source)
	assert.Equal(t, first[0].Metrics, second[0].Metrics)
	assert.Equal(t, first[0].FlightStart, second[0].FlightStart)
}

// A flight (and retained sample) that started before the caller's bound can
// never satisfy the request: the sampler waits the stale flight out and then
// performs one new read.
func TestSampleDeviceMetrics_PreBoundFlightTriggersNewRead(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("stale-flight-device")

	releaseFirstFetch := make(chan struct{})
	firstFetchStarted := make(chan struct{})
	gomock.InOrder(
		h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil),
		h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil),
	)
	gomock.InOrder(
		h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).DoAndReturn(func(context.Context) (modelsV2.DeviceMetrics, error) {
			close(firstFetchStarted)
			<-releaseFirstFetch
			return sampleMetricsFixture(deviceID, 3200), nil
		}),
		h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(sampleMetricsFixture(deviceID, 60), nil),
	)

	// Start a pre-bound direct flight and hold it open.
	staleResults := make(chan SampleResult, 1)
	go func() {
		res := h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})
		staleResults <- res[0]
	}()
	<-firstFetchStarted

	// The bound is after the first flight's start, so that flight and its
	// retained sample must be rejected; the sampler waits it out and issues
	// one new read.
	bound := time.Now()
	time.Sleep(time.Millisecond) // ensure the new flight starts strictly after bound
	freshResults := make(chan SampleResult, 1)
	go func() {
		res := h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID, SampledAfter: bound}})
		freshResults <- res[0]
	}()

	time.Sleep(20 * time.Millisecond) // let the bounded waiter observe and park on the stale flight
	close(releaseFirstFetch)

	stale := <-staleResults
	require.NoError(t, stale.Err)
	assert.Equal(t, SampleSourceDirect, stale.Source)

	fresh := <-freshResults
	require.NoError(t, fresh.Err)
	assert.Equal(t, SampleSourceDirect, fresh.Source)
	assert.True(t, fresh.FlightStart.After(bound), "fresh sample must come from a post-bound flight")
	require.NotNil(t, fresh.Metrics.PowerW)
	assert.Equal(t, float64(60), fresh.Metrics.PowerW.Value)
}

// A sampler joined to a scheduled full poll receives the sample as soon as
// the device fetch returns, before the poll's status/diagnostics side effects
// complete.
func TestSampleDeviceMetrics_JoinsScheduledPollBeforeSideEffects(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("join-device")
	device := models.Device{ID: deviceID}
	metrics := sampleMetricsFixture(deviceID, 3200)

	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	releaseSideEffects := make(chan struct{})
	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(2)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).DoAndReturn(func(context.Context) (modelsV2.DeviceMetrics, error) {
		close(fetchStarted)
		<-releaseFetch
		return metrics, nil
	}).Times(1)
	h.scheduler.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	// Diagnostics polling is a post-fetch side effect: block it until the
	// joined sampler has already returned.
	h.errorPoller.EXPECT().PollErrors(gomock.Any(), h.miner).DoAndReturn(func(context.Context, ...minerInterfaces.Miner) diagnostics.PollResult {
		<-releaseSideEffects
		return diagnostics.PollResult{}
	}).Times(1)

	h.service.tasks <- device // scheduled full poll claims the flight
	<-fetchStarted

	joinDone := make(chan SampleResult, 1)
	go func() {
		res := h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})
		joinDone <- res[0]
	}()
	time.Sleep(20 * time.Millisecond) // let the sampler park on the in-flight poll
	close(releaseFetch)

	// The joined sample must arrive while PollErrors is still blocked: metrics
	// publish immediately after the fetch, ahead of the poll's side effects.
	result := <-joinDone
	require.NoError(t, result.Err)
	assert.Equal(t, SampleSourceJoined, result.Source)
	assert.Equal(t, metrics, result.Metrics)

	close(releaseSideEffects) // only now may the scheduled poll finish
	requireEventuallyReleased(t, h.service, deviceID)

	// Drain the scheduled poll's queued writes so they are accounted for.
	select {
	case res := <-h.service.metricsResults:
		assert.Equal(t, string(deviceID), res.metrics.DeviceIdentifier)
	case <-time.After(time.Second):
		t.Fatal("scheduled poll never enqueued its metrics write")
	}
}

// Status-only and RefreshDevice claims are never joined: the sampler waits
// for the claim to complete and then performs its own read.
func TestSampleDeviceMetrics_WaitsOutStatusOnlyAndRefreshClaims(t *testing.T) {
	for _, kind := range []inFlightKind{inFlightKindStatusOnly, inFlightKindRefresh} {
		t.Run(string(kind), func(t *testing.T) {
			h := newSamplingHarness(t, samplingTestConfig())
			h.startWorkers(t, 1)
			deviceID := models.DeviceIdentifier("claimed-device")

			h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
			h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(sampleMetricsFixture(deviceID, 3200), nil).Times(1)

			entry := newInFlightEntry(kind)
			h.service.inFlight.Store(deviceID, entry)

			sampleDone := make(chan SampleResult, 1)
			go func() {
				res := h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})
				sampleDone <- res[0]
			}()

			// The sampler must not fetch while the claim is held.
			time.Sleep(30 * time.Millisecond)
			select {
			case res := <-sampleDone:
				t.Fatalf("sampler returned %+v while a %s claim was held", res, kind)
			default:
			}

			h.service.releaseInFlight(deviceID, entry)

			res := <-sampleDone
			require.NoError(t, res.Err)
			assert.Equal(t, SampleSourceDirect, res.Source)
		})
	}
}

// Concurrent sampling requests for one device share a single device call, and
// duplicate device IDs within one batch collapse to one result.
func TestSampleDeviceMetrics_ConcurrentRequestsDeduplicate(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 2)
	deviceID := models.DeviceIdentifier("dedup-device")

	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).DoAndReturn(func(context.Context) (modelsV2.DeviceMetrics, error) {
		close(fetchStarted)
		<-releaseFetch
		return sampleMetricsFixture(deviceID, 3200), nil
	}).Times(1)

	// One batch with duplicate IDs returns a single deduplicated result.
	batchDone := make(chan []SampleResult, 1)
	go func() {
		batchDone <- h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{
			{DeviceID: deviceID},
			{DeviceID: deviceID},
		})
	}()
	<-fetchStarted

	// A second concurrent caller joins the same in-flight sample read.
	joinDone := make(chan []SampleResult, 1)
	go func() {
		joinDone <- h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})
	}()

	time.Sleep(10 * time.Millisecond)
	close(releaseFetch)

	batch := <-batchDone
	require.Len(t, batch, 1, "duplicate device IDs must collapse to one result")
	require.NoError(t, batch[0].Err)

	joined := <-joinDone
	require.Len(t, joined, 1)
	require.NoError(t, joined[0].Err)
	assert.Equal(t, SampleSourceJoined, joined[0].Source)
	assert.Equal(t, batch[0].Metrics, joined[0].Metrics)
}

// Scheduled full telemetry plus direct confirmation reads are executed by the
// same worker pool, so combined concurrent device fetches never exceed
// ConcurrencyLimit.
func TestSampleDeviceMetrics_SharesConcurrencyLimitWithScheduledPolls(t *testing.T) {
	config := samplingTestConfig()
	config.ConcurrencyLimit = 2
	h := newSamplingHarness(t, config)
	h.startWorkers(t, config.ConcurrencyLimit)

	var current, peak atomic.Int64
	observeFetch := func() {
		now := current.Add(1)
		for {
			prevPeak := peak.Load()
			if now <= prevPeak || peak.CompareAndSwap(prevPeak, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		current.Add(-1)
	}

	deviceIDs := []models.DeviceIdentifier{"cap-1", "cap-2", "cap-3", "cap-4", "cap-5", "cap-6"}
	for i, id := range deviceIDs {
		// Scheduled polls (the first two devices) resolve the miner twice:
		// once for the fetch and once for diagnostics polling.
		times := 1
		if i < 2 {
			times = 2
		}
		h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), id).Return(h.miner, nil).Times(times)
	}
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).DoAndReturn(func(context.Context) (modelsV2.DeviceMetrics, error) {
		observeFetch()
		return modelsV2.DeviceMetrics{Health: modelsV2.HealthHealthyActive}, nil
	}).Times(len(deviceIDs))
	// Scheduled polls run their persistence side effects.
	h.scheduler.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	h.errorPoller.EXPECT().PollErrors(gomock.Any(), h.miner).Return(diagnostics.PollResult{}).Times(2)

	// Two scheduled polls plus four direct samples, all in flight together.
	h.service.tasks <- models.Device{ID: deviceIDs[0]}
	h.service.tasks <- models.Device{ID: deviceIDs[1]}
	requests := make([]SampleRequest, 0, 4)
	for _, id := range deviceIDs[2:] {
		requests = append(requests, SampleRequest{DeviceID: id})
	}
	results := h.service.SampleDeviceMetrics(t.Context(), requests)

	for _, res := range results {
		require.NoError(t, res.Err)
	}
	// Wait for the scheduled polls to finish too.
	requireEventuallyReleased(t, h.service, deviceIDs[0])
	requireEventuallyReleased(t, h.service, deviceIDs[1])
	for range 2 {
		select {
		case <-h.service.metricsResults:
		case <-time.After(time.Second):
			t.Fatal("scheduled poll never enqueued its metrics write")
		}
	}
	assert.LessOrEqual(t, peak.Load(), int64(config.ConcurrencyLimit),
		"combined scheduled+sample fetch concurrency must respect ConcurrencyLimit")
}

// Failed devices in a batch never invalidate their successful siblings.
func TestSampleDeviceMetrics_MixedBatchPreservesSuccessfulSiblings(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 2)
	goodID := models.DeviceIdentifier("good-device")
	badID := models.DeviceIdentifier("bad-device")
	fetchErr := errors.New("miner unreachable")

	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), goodID).Return(h.miner, nil).Times(1)
	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), badID).Return(nil, fetchErr).Times(1)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(sampleMetricsFixture(goodID, 3200), nil).Times(1)

	results := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{
		{DeviceID: goodID},
		{DeviceID: badID},
	})
	require.Len(t, results, 2)

	byDevice := map[models.DeviceIdentifier]SampleResult{}
	for _, res := range results {
		byDevice[res.DeviceID] = res
	}
	require.NoError(t, byDevice[goodID].Err)
	require.NotNil(t, byDevice[goodID].Metrics.PowerW)
	require.Error(t, byDevice[badID].Err)
	assert.ErrorIs(t, byDevice[badID].Err, fetchErr)
}

// MetricTimeout expiry stops the waiter with an error while the pool worker
// still finishes the fetch and releases the claim.
func TestSampleDeviceMetrics_TimeoutReleasesWaiterAndClaim(t *testing.T) {
	config := samplingTestConfig()
	config.MetricTimeout = 50 * time.Millisecond
	h := newSamplingHarness(t, config)
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("slow-device")

	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).DoAndReturn(func(ctx context.Context) (modelsV2.DeviceMetrics, error) {
		<-ctx.Done() // slower than the waiter's budget; ends at the fetch's own MetricTimeout
		return modelsV2.DeviceMetrics{}, ctx.Err()
	}).Times(1)

	start := time.Now()
	results := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID}})
	require.Len(t, results, 1)
	require.Error(t, results[0].Err)
	assert.ErrorIs(t, results[0].Err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second, "waiter must give up at MetricTimeout")

	requireEventuallyReleased(t, h.service, deviceID)
}

// A cancelled caller context fails fast and leaves no claim behind.
func TestSampleDeviceMetrics_CancelledContextReleasesClaim(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	// No workers: a claimed task would sit in the queue, so cancellation must
	// release the claim taken during admission.
	deviceID := models.DeviceIdentifier("cancelled-device")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results := h.service.SampleDeviceMetrics(ctx, []SampleRequest{{DeviceID: deviceID}})
	require.Len(t, results, 1)
	require.Error(t, results[0].Err)

	requireEventuallyReleased(t, h.service, deviceID)
}

// Direct sampling performs zero telemetry/status/firmware/scheduler/
// diagnostics writes: the strict mocks expect only the miner resolution and
// the metrics fetch, and the writer queues stay empty.
func TestSampleDeviceMetrics_DirectReadHasNoSideEffects(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("pure-device")

	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(sampleMetricsFixture(deviceID, 3200), nil).Times(1)

	results := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID}})
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	requireEventuallyReleased(t, h.service, deviceID)

	select {
	case res := <-h.service.metricsResults:
		t.Fatalf("direct sampling enqueued a metrics write: %+v", res)
	default:
	}
	select {
	case res := <-h.service.statusResults:
		t.Fatalf("direct sampling enqueued a status write: %+v", res)
	default:
	}
}

// The sampler validates plugin-reported identifiers exactly like the
// scheduled path: a mismatched identifier is an error, not a sample.
func TestSampleDeviceMetrics_RejectsMismatchedDeviceIdentifier(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("trusted-device")

	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(modelsV2.DeviceMetrics{
		DeviceIdentifier: "some-other-device",
		Health:           modelsV2.HealthHealthyActive,
	}, nil).Times(1)

	results := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID}})
	require.Len(t, results, 1)
	require.Error(t, results[0].Err)
	assert.Contains(t, results[0].Err.Error(), "mismatched device identifier")
}

// Concurrent batch sampling with scheduled claims and refresh churn is safe
// under the race detector.
func TestSampleDeviceMetrics_RaceWithScheduledAndRefresh(t *testing.T) {
	config := samplingTestConfig()
	config.ConcurrencyLimit = 4
	h := newSamplingHarness(t, config)
	h.startWorkers(t, config.ConcurrencyLimit)
	deviceID := models.DeviceIdentifier("race-device")

	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).AnyTimes()
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(sampleMetricsFixture(deviceID, 3200), nil).AnyTimes()
	h.scheduler.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	h.errorPoller.EXPECT().PollErrors(gomock.Any(), h.miner).Return(diagnostics.PollResult{}).AnyTimes()

	drainCtx, stopDrain := context.WithCancel(context.Background())
	defer stopDrain()
	go func() { // keep writer queues from filling
		for {
			select {
			case <-drainCtx.Done():
				return
			case <-h.service.metricsResults:
			case <-h.service.statusResults:
			}
		}
	}()

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				res := h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})
				require.Len(t, res, 1)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 10 {
			h.service.tasks <- models.Device{ID: deviceID}
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()

	requireEventuallyReleased(t, h.service, deviceID)
}
