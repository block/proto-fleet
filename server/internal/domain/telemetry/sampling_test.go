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
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerInterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
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
	activation  *telemetryActivation
	runContext  func() context.Context
	cancel      context.CancelFunc
	stopOnce    sync.Once
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
	runCtx, cancel := context.WithCancel(context.Background())
	h.runContext = func() context.Context { return runCtx }
	h.cancel = cancel
	h.activation = markTelemetryServiceActiveForTest(h.service, runCtx)
	t.Cleanup(h.stop)
	h.miner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	h.miner.EXPECT().GetSiteID().Return(int64(1)).AnyTimes()
	h.miner.EXPECT().GetDriverName().Return("test-driver").AnyTimes()
	return h
}

func (h *samplingHarness) stop() {
	h.stopOnce.Do(func() {
		h.cancel()
		h.activation.producerWG.Wait()
		h.service.finishActivation(h.activation)
	})
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
	for range n {
		h.activation.producerWG.Go(func() {
			h.service.worker(h.runContext(), h.activation)
		})
	}
}

func (h *samplingHarness) startWriters() {
	writerCtx := h.activation.writerContext(h.runContext())
	h.activation.background.Go(func() { h.service.statusWriterRoutine(writerCtx, h.activation) })
	h.activation.background.Go(func() { h.service.metricsWriterRoutine(writerCtx, h.activation) })
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

func requireQueuedSampleTask(t *testing.T, activation *telemetryActivation) sampleTask {
	t.Helper()
	select {
	case task := <-activation.sampleTasks:
		activation.sampleTasks <- task
		return task
	case <-time.After(time.Second):
		t.Fatal("sample was not queued")
		return sampleTask{}
	}
}

func TestSampleDeviceMetrics_InactiveReturnsPerDeviceErrorsWithoutAdmission(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.service.lifecycleMu.Lock()
	h.service.activation = nil
	h.service.lifecycleMu.Unlock()

	deviceIDs := []models.DeviceIdentifier{"inactive-1", "inactive-2"}
	results := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{
		{DeviceID: deviceIDs[0]},
		{DeviceID: deviceIDs[1]},
	})

	require.Len(t, results, len(deviceIDs))
	for i, result := range results {
		assert.Equal(t, deviceIDs[i], result.DeviceID)
		require.ErrorIs(t, result.Err, errTelemetryServiceInactive)
		_, claimed := h.service.inFlight.Load(deviceIDs[i])
		assert.False(t, claimed)
	}
	assert.Empty(t, h.activation.sampleTasks)
}

func TestSampleDeviceMetrics_ActivationShutdownReleasesQueuedSample(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	deviceID := models.DeviceIdentifier("queued-sample")
	sampleDone := make(chan SampleResult, 1)
	go func() {
		sampleDone <- h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})[0]
	}()

	queuedTask := requireQueuedSampleTask(t, h.activation)
	assert.Equal(t, deviceID, queuedTask.deviceID)
	value, claimed := h.service.inFlight.Load(deviceID)
	require.True(t, claimed)
	entry, ok := value.(*inFlightEntry)
	require.True(t, ok)
	assert.Equal(t, inFlightKindSample, entry.kind)

	h.stop()

	result := <-sampleDone
	require.ErrorIs(t, result.Err, errTelemetryServiceInactive)
	_, claimed = h.service.inFlight.Load(deviceID)
	assert.False(t, claimed)
	assert.Empty(t, h.activation.sampleTasks)
	select {
	case <-entry.metricsReady:
	default:
		t.Fatal("shutdown did not release the queued sample waiter")
	}
	select {
	case <-entry.claimDone:
	default:
		t.Fatal("shutdown did not release the queued sample claim")
	}
}

func TestSampleDeviceMetrics_RestartDoesNotExecuteOldActivationSample(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	deviceID := models.DeviceIdentifier("restart-sample")
	metrics := sampleMetricsFixture(deviceID, 3200)
	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(metrics, nil).Times(1)

	oldActivation := h.activation
	oldSampleDone := make(chan SampleResult, 1)
	go func() {
		oldSampleDone <- h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})[0]
	}()
	queuedTask := requireQueuedSampleTask(t, oldActivation)
	assert.Equal(t, deviceID, queuedTask.deviceID)

	h.stop()
	require.ErrorIs(t, (<-oldSampleDone).Err, errTelemetryServiceInactive)
	assert.Empty(t, oldActivation.sampleTasks)

	restartCtx, cancelRestart := context.WithCancel(context.Background())
	newActivation := markTelemetryServiceActiveForTest(h.service, restartCtx)
	newActivation.producerWG.Go(func() {
		h.service.worker(restartCtx, newActivation)
	})
	t.Cleanup(func() {
		cancelRestart()
		newActivation.producerWG.Wait()
		h.service.finishActivation(newActivation)
	})

	result := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID}})
	require.Len(t, result, 1)
	require.NoError(t, result[0].Err)
	assert.Equal(t, SampleSourceDirect, result[0].Source)
	assert.Equal(t, metrics, result[0].Metrics)
	assert.NotEqual(t, oldActivation.sampleTasks, newActivation.sampleTasks)
}

// This lifecycle-level regression uses the public Start/Stop API. A sample
// retained by one activation must not be reusable after the service restarts.
func TestSampleDeviceMetrics_RetainedSampleClearedAcrossStartStopRestart(t *testing.T) {
	ctrl := gomock.NewController(t)
	dataStore := mock.NewMockTelemetryDataStore(ctrl)
	minerGetter := mock.NewMockCachedMinerGetter(ctrl)
	miner := minerMocks.NewMockMiner(ctrl)
	scheduler := mock.NewMockUpdateScheduler(ctrl)
	deviceStore := storesMocks.NewMockDeviceStore(ctrl)

	// Keep Start's unrelated background routines quiet while exercising the
	// real lifecycle, matching the setup used by the service restart tests.
	scheduler.EXPECT().FetchDevices(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	deviceStore.EXPECT().GetAllPairedDeviceIdentifiers(gomock.Any()).Return(nil, nil).AnyTimes()
	dataStore.EXPECT().InsertMinerStateSnapshot(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	deviceID := models.DeviceIdentifier("retained-across-restart")
	firstMetrics := sampleMetricsFixture(deviceID, 3200)
	secondMetrics := sampleMetricsFixture(deviceID, 60)
	gomock.InOrder(
		minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(miner, nil),
		minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(miner, nil),
	)
	gomock.InOrder(
		miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(firstMetrics, nil),
		miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(secondMetrics, nil),
	)
	miner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	miner.EXPECT().GetSiteID().Return(int64(1)).AnyTimes()
	miner.EXPECT().GetDriverName().Return("test-driver").AnyTimes()

	service := NewTelemetryService(Config{
		StalenessThreshold:       time.Minute,
		FetchInterval:            time.Hour,
		ConcurrencyLimit:         2,
		MetricTimeout:            time.Second,
		DevicePollInterval:       time.Hour,
		DeviceStatusPollInterval: time.Hour,
		StateSnapshotInterval:    time.Hour,
		StatusFlushInterval:      time.Hour,
	}, dataStore, minerGetter, scheduler, deviceStore, mock.NewMockErrorPoller(ctrl))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.Stop(stopCtx)
	})

	require.NoError(t, service.Start(t.Context()))
	first := service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID}})
	require.Len(t, first, 1)
	require.NoError(t, first[0].Err)
	assert.Equal(t, SampleSourceDirect, first[0].Source)
	assert.Equal(t, firstMetrics, first[0].Metrics)
	_, retained := service.retainedSamples.Load(deviceID)
	require.True(t, retained)

	require.NoError(t, service.Stop(t.Context()))
	_, retained = service.retainedSamples.Load(deviceID)
	assert.False(t, retained, "stopped activation must clear retained samples")

	require.NoError(t, service.Start(t.Context()))
	second := service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID}})
	require.Len(t, second, 1)
	require.NoError(t, second[0].Err)
	assert.Equal(t, SampleSourceDirect, second[0].Source)
	assert.Equal(t, secondMetrics, second[0].Metrics)
	require.NoError(t, service.Stop(t.Context()))
}

func TestPublishFlightSample_DoesNotPublishIntoReplacementClaim(t *testing.T) {
	service := &TelemetryService{}
	deviceID := models.DeviceIdentifier("replacement-claim")
	claimA := newInFlightEntry(inFlightKindFullTelemetry)
	claimB := newInFlightEntry(inFlightKindFullTelemetry)
	service.inFlight.Store(deviceID, claimA)
	service.inFlight.Store(deviceID, claimB)

	service.publishFlightSample(deviceID, claimA, &deviceResult{
		metrics: sampleMetricsFixture(deviceID, 3200),
	}, nil)

	select {
	case <-claimA.metricsReady:
		t.Fatal("superseded claim A received a sample")
	default:
	}
	select {
	case <-claimB.metricsReady:
		t.Fatal("replacement claim B received claim A's sample")
	default:
	}
	_, retained := service.retainedSamples.Load(deviceID)
	assert.False(t, retained, "claim A's sample must not be retained after claim B replaces it")
}

func TestEvictExpiredRetainedSamples_RemovesOnlyExpiredIdentity(t *testing.T) {
	service := &TelemetryService{}
	now := time.Now()
	expiredDevice := models.DeviceIdentifier("expired")
	freshDevice := models.DeviceIdentifier("fresh")
	expired := &retainedSample{completedAt: now.Add(-sampleReuseWindow)}
	fresh := &retainedSample{completedAt: now.Add(-sampleReuseWindow + time.Second)}
	service.retainedSamples.Store(expiredDevice, expired)
	service.retainedSamples.Store(freshDevice, fresh)

	service.evictExpiredRetainedSamples(now)

	_, ok := service.retainedSamples.Load(expiredDevice)
	assert.False(t, ok)
	value, ok := service.retainedSamples.Load(freshDevice)
	require.True(t, ok)
	assert.Same(t, fresh, value)
}

func TestRetainSample_RejectsFlightFromRemovedGeneration(t *testing.T) {
	service := &TelemetryService{}
	deviceID := models.DeviceIdentifier("removed-during-flight")
	entry := service.newDeviceInFlightEntry(deviceID, inFlightKindFullTelemetry)
	service.advanceSampleGeneration(deviceID)

	service.retainSample(deviceID, 1, sampleMetricsFixture(deviceID, 3200), entry.flightStart, entry.sampleGeneration)

	_, retained := service.retainedSamples.Load(deviceID)
	assert.False(t, retained, "a flight admitted before removal must not repopulate retention")
}

func TestSampleDeviceMetrics_RejectsWrongOrganizationBeforeDirectRead(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("moved-device")
	h.minerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(h.miner, nil).
		Times(1)

	results := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{
		DeviceID: deviceID,
		OrgID:    42,
	}})

	require.Len(t, results, 1)
	require.Error(t, results[0].Err)
	assert.Contains(t, results[0].Err.Error(), "expected org 42")
	assert.Equal(t, int64(42), results[0].OrgID)
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

func TestSampleDeviceMetrics_ExpiredRetainedSampleTriggersFreshDirectRead(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("expired-retained-device")
	expired := &retainedSample{
		metrics:     sampleMetricsFixture(deviceID, 3200),
		flightStart: time.Now().Add(-sampleReuseWindow),
		completedAt: time.Now().Add(-sampleReuseWindow - time.Second),
	}
	h.service.retainedSamples.Store(deviceID, expired)

	freshMetrics := sampleMetricsFixture(deviceID, 60)
	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(freshMetrics, nil).Times(1)

	results := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID}})
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	assert.Equal(t, SampleSourceDirect, results[0].Source)
	assert.Equal(t, freshMetrics, results[0].Metrics)

	value, retained := h.service.retainedSamples.Load(deviceID)
	require.True(t, retained)
	assert.NotSame(t, expired, value, "the expired identity must not remain retained")
	fresh, ok := value.(*retainedSample)
	require.True(t, ok)
	assert.Equal(t, freshMetrics, fresh.metrics)
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

	h.activation.tasks <- device // scheduled full poll claims the flight
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
	case res := <-h.activation.results.metrics:
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
	h.activation.tasks <- models.Device{ID: deviceIDs[0]}
	h.activation.tasks <- models.Device{ID: deviceIDs[1]}
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
		case <-h.activation.results.metrics:
		case <-time.After(time.Second):
			t.Fatal("scheduled poll never enqueued its metrics write")
		}
	}
	assert.LessOrEqual(t, peak.Load(), int64(config.ConcurrencyLimit),
		"combined scheduled+sample fetch concurrency must respect ConcurrencyLimit")
}

// This deterministic admission test intentionally starts with no workers.
// Separate concurrent SampleDeviceMetrics calls must share one activation-wide
// half-pool reservation cap, rather than each admitting half the pool.
func TestSampleDeviceMetrics_ActivationWideSlotCapAcrossConcurrentCalls(t *testing.T) {
	config := samplingTestConfig()
	config.ConcurrencyLimit = 4
	h := newSamplingHarness(t, config)
	deviceIDs := []models.DeviceIdentifier{"global-cap-1", "global-cap-2", "global-cap-3", "global-cap-4"}

	for _, deviceID := range deviceIDs {
		h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(1)
	}
	h.miner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(modelsV2.DeviceMetrics{Health: modelsV2.HealthHealthyActive}, nil).
		Times(len(deviceIDs))

	results := make(chan SampleResult, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		go func() {
			results <- h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})[0]
		}()
	}

	claimCount := func() int {
		count := 0
		h.service.inFlight.Range(func(_, _ any) bool {
			count++
			return true
		})
		return count
	}
	slotCap := config.ConcurrencyLimit / 2
	require.Equal(t, slotCap, cap(h.activation.sampleSlots))
	require.Eventually(t, func() bool {
		return len(h.activation.sampleSlots) == slotCap &&
			len(h.activation.sampleTasks) == slotCap &&
			claimCount() == slotCap
	}, time.Second, time.Millisecond)

	h.startWorkers(t, config.ConcurrencyLimit)
	for range deviceIDs {
		result := <-results
		require.NoError(t, result.Err)
		assert.Equal(t, SampleSourceDirect, result.Source)
	}
	require.Eventually(t, func() bool {
		return len(h.activation.sampleSlots) == 0 && claimCount() == 0
	}, time.Second, time.Millisecond)
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

// This deterministic queue-unit test starts without workers so the caller's
// operation expires while its admitted task is still queued. A later worker
// must skip the abandoned RPC and release both admission resources.
func TestSampleDeviceMetrics_QueuedTimeoutIsSkippedAndReleasesClaimAndSlot(t *testing.T) {
	config := samplingTestConfig()
	config.MetricTimeout = 40 * time.Millisecond
	h := newSamplingHarness(t, config)
	deviceID := models.DeviceIdentifier("queued-timeout-device")

	sampleDone := make(chan SampleResult, 1)
	go func() {
		sampleDone <- h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})[0]
	}()

	task := requireQueuedSampleTask(t, h.activation)
	assert.Equal(t, deviceID, task.deviceID)
	result := <-sampleDone
	require.ErrorIs(t, result.Err, context.DeadlineExceeded)
	select {
	case <-task.done:
	default:
		t.Fatal("queued task did not retain the caller operation's done signal")
	}
	_, claimed := h.service.inFlight.Load(deviceID)
	require.True(t, claimed, "queued task owns the claim until a worker drains it")
	require.Len(t, h.activation.sampleSlots, 1, "queued task owns one activation-wide slot")

	// Strict mocks have no miner expectations: any RPC would fail the test.
	h.startWorkers(t, 1)
	require.Eventually(t, func() bool {
		_, claimed := h.service.inFlight.Load(deviceID)
		return !claimed && len(h.activation.sampleSlots) == 0 && len(h.activation.sampleTasks) == 0
	}, time.Second, time.Millisecond)
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
	case res := <-h.activation.results.metrics:
		t.Fatalf("direct sampling enqueued a metrics write: %+v", res)
	default:
	}
	select {
	case res := <-h.activation.results.status:
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
			case <-h.activation.results.metrics:
			case <-h.activation.results.status:
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
			h.activation.tasks <- models.Device{ID: deviceID}
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()

	requireEventuallyReleased(t, h.service, deviceID)
}

// A sample completed by RefreshDevice's real claim/fetch/publish path is
// retained and reused by a later SampleDeviceMetrics call, exactly as the
// package doc promises. This drives the full path end-to-end: RefreshDevice
// -> claimDeviceForRefresh -> processDevice -> GetTelemetryFromDevice ->
// publishFlightSample -> retainSample, then SampleDeviceMetrics reuses the
// retained sample instead of issuing a second device read.
func TestSampleDeviceMetrics_ReusesSampleRetainedByRefreshDevice(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	deviceID := models.DeviceIdentifier("refresh-retained-device")
	metrics := sampleMetricsFixture(deviceID, 3200)

	// RefreshDevice's full path: one miner resolution plus fetch for
	// telemetry, and one more miner resolution for error polling.
	h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil).Times(2)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(metrics, nil).Times(1)
	h.scheduler.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	h.errorPoller.EXPECT().PollErrors(gomock.Any(), h.miner).Return(diagnostics.PollResult{}).Times(1)
	h.deviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []models.DeviceIdentifier{deviceID}).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		Times(1)

	h.startWriters()

	h.dataStore.EXPECT().StoreDeviceMetrics(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	h.deviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)

	bound := time.Now()
	require.NoError(t, h.service.RefreshDevice(h.runContext(), models.Device{ID: deviceID}))
	requireEventuallyReleased(t, h.service, deviceID)

	// The refresh's flight started after bound, so the retained sample must
	// satisfy a SampleDeviceMetrics request bounded by it, without a second
	// device call (enforced by the GetDeviceMetrics Times(1) expectation above).
	results := h.service.SampleDeviceMetrics(t.Context(), []SampleRequest{{DeviceID: deviceID, SampledAfter: bound}})
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	assert.Equal(t, SampleSourceReused, results[0].Source)
	assert.Equal(t, metrics, results[0].Metrics)
	assert.True(t, results[0].FlightStart.After(bound))
}

// The joined-flight error path (an in-flight scheduled poll whose fetch
// fails) must surface the fetch error to the joiner, not a false-success
// zero-value sample. This is a variant of
// TestSampleDeviceMetrics_JoinsScheduledPollBeforeSideEffects where the
// scheduled poll's GetDeviceMetrics fails instead of succeeding.
func TestSampleDeviceMetrics_JoinsScheduledPollErrorBeforeSideEffects(t *testing.T) {
	h := newSamplingHarness(t, samplingTestConfig())
	h.startWorkers(t, 1)
	deviceID := models.DeviceIdentifier("join-error-device")
	device := models.Device{ID: deviceID}
	fetchErr := errors.New("miner unreachable")
	// Subsequent miner resolutions (status fallback, error polling) fail with
	// a connection error so the flight completes without needing to mock the
	// full status-fetch/error-poll side-effect chain; that chain is exercised
	// by TestSampleDeviceMetrics_JoinsScheduledPollBeforeSideEffects already.
	connErr := fleeterror.NewConnectionError(string(deviceID), errors.New("connection refused"))

	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	gomock.InOrder(
		h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(h.miner, nil),
		h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(nil, connErr),
		h.minerGetter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).Return(nil, connErr),
	)
	h.miner.EXPECT().GetDeviceMetrics(gomock.Any()).DoAndReturn(func(context.Context) (modelsV2.DeviceMetrics, error) {
		close(fetchStarted)
		<-releaseFetch
		return modelsV2.DeviceMetrics{}, fetchErr
	}).Times(1)
	h.deviceStore.EXPECT().GetDeviceOrgDriverAndSite(gomock.Any(), deviceID).Return(int64(0), "", int64(0), nil).Times(1)
	h.scheduler.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	h.activation.tasks <- device // scheduled full poll claims the flight
	<-fetchStarted

	joinDone := make(chan SampleResult, 1)
	go func() {
		res := h.service.SampleDeviceMetrics(context.Background(), []SampleRequest{{DeviceID: deviceID}})
		joinDone <- res[0]
	}()
	time.Sleep(20 * time.Millisecond) // let the sampler park on the in-flight poll
	close(releaseFetch)

	// The joiner must see the fetch error immediately, not a zero-value
	// "success" sample.
	result := <-joinDone
	require.Error(t, result.Err)
	assert.ErrorIs(t, result.Err, fetchErr)
	assert.Equal(t, SampleSourceJoined, result.Source)
	assert.Equal(t, modelsV2.DeviceMetrics{}, result.Metrics)

	requireEventuallyReleased(t, h.service, deviceID)
}
