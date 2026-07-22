/*
Package telemetry collects and stores metrics from mining devices.

# Architecture Overview

The telemetry system uses a producer-consumer pattern with three main components:

	┌─────────────────────┐
	│ gatherMetricsRoutine│ (producer)
	│ - Polls scheduler   │
	│ - Sends to tasks    │
	└─────────┬───────────┘
	          │
	          ▼
	   ┌──────────────┐
	   │ tasks channel│
	   └──────┬───────┘
	          │
	          ▼
	┌─────────────────────┐
	│ workers (N parallel)│ (consumer/producer)
	│ - Fetch from miner  │
	│ - Send to results   │
	└─────────┬───────────┘
	          │
	          ▼
	  ┌───────────────┐
	  │ statusResults │
	  │    channel    │
	  └───────┬───────┘
	          │
	          ▼
	┌─────────────────────┐
	│ statusWriterRoutine │ (consumer)
	│ - Batches updates   │
	│ - Writes to DB      │
	│ - Broadcasts changes│
	└─────────────────────┘

# Component Details

gatherMetricsRoutine: Periodically queries the scheduler for stale devices
(those needing telemetry refresh) and dispatches them to workers via the
tasks channel. Also handles new device polling to discover recently paired
devices.

workers: A pool of goroutines (sized by ConcurrencyLimit) that fetch
telemetry and status from individual miners. Each worker pulls a device
from the tasks channel, makes network calls to the miner, stores telemetry
in TimescaleDB, and sends the status result to statusResults. Workers are
simple and stateless - no batching logic.

statusWriterRoutine: A single goroutine that collects status updates from
all workers and batches them for efficient DB writes. It flushes on a
configurable interval (StatusFlushInterval) or when the context is
cancelled. After writing, it broadcasts status changes to connected
clients using in-memory state for change detection.

statusPollingRoutine: A separate routine that periodically checks failed
devices (those removed from the main scheduler after too many failures).
This allows devices to recover and rejoin the telemetry collection when
they come back online.

# Design Rationale

The architecture separates network I/O (inherently per-device) from DB
writes (benefits from batching). This avoids the "too many connections"
problem that occurs when each worker maintains its own DB connection for
individual writes. Instead, all DB writes flow through a single routine
that batches them efficiently.
*/
package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	// Default intervals
	defaultStatusUpdateInterval    = 1 * time.Second
	defaultFetchInterval           = 5 * time.Second
	defaultDevicePollInterval      = 10 * time.Minute
	defaultHeartbeatInterval       = 30 * time.Second
	defaultBroadcasterPollInterval = 5 * time.Second
	defaultStatusPollingInterval   = 10 * time.Second

	// Channel buffer sizes - prevent blocking on temporary consumer delays while limiting memory.

	// streamResponseChannelBuffer: gRPC streaming responses to clients.
	// Allows clients to lag briefly (network hiccups) without blocking the sender goroutine.
	streamResponseChannelBuffer = 100

	// statusUpdateChannelBuffer: miner state count updates for streaming.
	// Provides buffer for consumer processing delays at the configured update interval.
	statusUpdateChannelBuffer = 100

	// subscriberChannelBuffer: telemetry updates per subscriber.
	// Allows asynchronous processing without dropping updates during brief delays.
	subscriberChannelBuffer = 100

	// resultsChannelBuffer: status results from workers before batch DB writes.
	// Larger than others because all workers (ConcurrencyLimit) write here concurrently,
	// requiring headroom to avoid blocking workers while statusWriterRoutine flushes to DB.
	resultsChannelBuffer = 5000

	// Batch limits
	maxStatusBatchSize  = 500
	maxMetricsBatchSize = 500

	// Default status flush interval if not configured.
	defaultStatusFlushInterval = 1 * time.Second

	// Default metrics flush interval if not configured.
	defaultMetricsFlushInterval = 1 * time.Second

	defaultStateSnapshotInterval = 60 * time.Second
	fleetRollupInterval          = 30 * time.Second
	fleetRollupMaxBucketsPerTick = 40
	fleetRollupBackfillFloor     = 6 * time.Hour
	fleetRollupRewriteBuckets    = models.FleetMetricRollupRawTailBuckets

	// Context timeouts
	shutdownFlushTimeout = 5 * time.Second
)

var errTelemetryServiceInactive = errors.New("telemetry service is inactive or stopping")

const (
	defaultUpdateInterval = 1 * time.Minute

	// Page size for combined metrics query
	defaultCombinedMetricsPageSize = 100

	// combinedMetricsQuantum aligns combined-metrics time bounds before
	// singleflight keying: EndTime rounds down to this quantum and StartTime
	// shifts by the same delta. Clients stamp bounds from Date.now(), so raw
	// bounds never collide across viewers; quantized ones do. Worst case a
	// follower sees data under 15s stale, invisible at dashboard granularity.
	combinedMetricsQuantum = 15 * time.Second

	// combinedMetricsQuantizeMinInterval gates quantization: only queries whose
	// SlideInterval (bucket size) is at least this get coarsened, keeping the
	// sub-quantum shift confined to partial edge buckets. Finer queries keep
	// their exact bounds and coalesce only when identical. The dashboard's
	// smallest granularity is 90s, so its queries always quantize.
	combinedMetricsQuantizeMinInterval = time.Minute

	// combinedMetricsFlightTimeout bounds the shared singleflight query, which
	// runs detached from any individual caller's context. The store already
	// caps each query at its QueryTimeout but that config is not visible at
	// this layer, so mirror the largest configured value (90s in the Pi-class
	// host profiles) plus headroom; this exists only to avoid leaking the
	// flight goroutine if a connection wedges.
	combinedMetricsFlightTimeout = 95 * time.Second
)

//go:generate go run go.uber.org/mock/mockgen -source=service.go -destination=mocks/mock_service.go -package=mock UpdateScheduler,TelemetryDataStore,MinerGetter,CachedMinerGetter
type UpdateScheduler interface {
	AddNewDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error
	AddDevices(ctx context.Context, devices ...models.Device) error
	AddFailedDevices(ctx context.Context, devices ...models.Device) error
	FetchDevices(ctx context.Context, after time.Time) ([]models.Device, error)
	RemoveDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error
	IsFailedDevice(ctx context.Context, deviceID models.DeviceIdentifier) (bool, time.Time, error)
}

type TelemetryDataStore interface {
	StoreDeviceMetrics(ctx context.Context, data ...modelsV2.DeviceMetrics) error
	GetLatestDeviceMetricsBatch(ctx context.Context, deviceIDs []models.DeviceIdentifier) (map[models.DeviceIdentifier]modelsV2.DeviceMetrics, error)
	GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]modelsV2.DeviceMetrics, error)
	StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error)
	GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error)
	InsertMinerStateSnapshot(ctx context.Context, at time.Time) error
	UpsertFleetMetricRollups(ctx context.Context, startTime, endTime time.Time) error
	GetLatestFleetMetricRollupBucket(ctx context.Context) (time.Time, error)
	Ping(ctx context.Context) error
}

type MinerGetter interface {
	GetMinerFromDeviceIdentifier(ctx context.Context, deviceIdentifier models.DeviceIdentifier) (interfaces.Miner, error)
}

// CachedMinerGetter extends MinerGetter with cache invalidation. Services that
// both fetch miners and need to evict stale handles should use this interface.
type CachedMinerGetter interface {
	MinerGetter
	// InvalidateMiner removes the cached miner handle for the given device identifier.
	// Call this when an auth error occurs so the next lookup fetches fresh credentials.
	InvalidateMiner(deviceIdentifier models.DeviceIdentifier)
}

type deviceResult struct {
	device     models.Device
	metrics    modelsV2.DeviceMetrics
	metricsErr error
	// status and hasStatus are set when metricsErr == nil.
	// hasStatus is false for HealthHealthyInactive; see healthStatusToMinerStatus.
	status     mm.MinerStatus
	hasStatus  bool
	orgID      int64
	siteID     int64
	driverName string
}

// statusResult represents a status update result from a worker.
type statusResult struct {
	deviceIdentifier models.DeviceIdentifier
	status           mm.MinerStatus
	orgID            int64
	siteID           int64
	driverName       string
}

type statusFlushRequest struct {
	callerDeadline time.Time
	deviceID       *models.DeviceIdentifier
	done           chan error
}

type metricsFlushRequest struct {
	callerDeadline time.Time
	deviceID       *models.DeviceIdentifier
	done           chan error
}

type statusFlushResult struct {
	err     error
	devices map[models.DeviceIdentifier]bool
}

func (r statusFlushResult) errorForDevice(deviceID *models.DeviceIdentifier) error {
	if deviceID == nil {
		return r.err
	}
	if r.devices[*deviceID] {
		return r.err
	}
	return nil
}

func mergeStatusFlushResults(results ...statusFlushResult) statusFlushResult {
	merged := statusFlushResult{devices: make(map[models.DeviceIdentifier]bool)}
	for _, result := range results {
		merged.err = errors.Join(merged.err, result.err)
		for deviceID := range result.devices {
			merged.devices[deviceID] = true
		}
	}
	if len(merged.devices) == 0 {
		merged.devices = nil
	}
	return merged
}

type metricsFlushResult struct {
	err          error
	deviceErrors map[models.DeviceIdentifier]error
}

func (r metricsFlushResult) errorForDevice(deviceID *models.DeviceIdentifier) error {
	if deviceID == nil {
		return r.err
	}
	return r.deviceErrors[*deviceID]
}

func mergeMetricsFlushResults(results ...metricsFlushResult) metricsFlushResult {
	merged := metricsFlushResult{deviceErrors: make(map[models.DeviceIdentifier]error)}
	for _, result := range results {
		merged.err = errors.Join(merged.err, result.err)
		for deviceID, err := range result.deviceErrors {
			merged.deviceErrors[deviceID] = errors.Join(merged.deviceErrors[deviceID], err)
		}
	}
	if len(merged.deviceErrors) == 0 {
		merged.deviceErrors = nil
	}
	return merged
}

func boundedFlushContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, shutdownFlushTimeout)
}

func requestFlushContext(ctx context.Context, callerDeadline time.Time) (context.Context, context.CancelFunc) {
	if callerDeadline.IsZero() {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, callerDeadline)
}

type inFlightKind string

const (
	inFlightKindFullTelemetry inFlightKind = "full_telemetry"
	inFlightKindStatusOnly    inFlightKind = "status_only"
)

// metricsResult holds device metrics queued by a worker for batch DB writes.
type metricsResult struct {
	deviceID   models.DeviceIdentifier
	orgID      int64
	siteID     int64
	driverName string
	metrics    modelsV2.DeviceMetrics
}

type telemetryResults struct {
	status  chan statusResult
	metrics chan metricsResult
}

func newTelemetryResults() telemetryResults {
	return telemetryResults{
		status:  make(chan statusResult, resultsChannelBuffer),
		metrics: make(chan metricsResult, resultsChannelBuffer),
	}
}

// telemetryRun owns every channel consumed by one activation. A later Start
// always installs a new run so results and flush requests cannot cross runs.
type telemetryRun struct {
	results              telemetryResults
	statusFlushRequests  chan statusFlushRequest
	metricsFlushRequests chan metricsFlushRequest
	producerWG           sync.WaitGroup
	done                 <-chan struct{}
}

func newTelemetryRun(done <-chan struct{}) *telemetryRun {
	return &telemetryRun{
		results:              newTelemetryResults(),
		statusFlushRequests:  make(chan statusFlushRequest),
		metricsFlushRequests: make(chan metricsFlushRequest),
		done:                 done,
	}
}

func (r *telemetryRun) canceled() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// writerContext keeps result writers alive after activation cancellation until
// every background producer has exited. Writers can keep draining while slow
// producers unwind, then their existing cancellation path performs one final,
// stable drain and flush.
func (r *telemetryRun) writerContext(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		r.producerWG.Wait()
		cancel()
	}()
	return ctx
}

type TelemetryService struct {
	config             Config
	updateScheduler    UpdateScheduler
	telemetryDataStore TelemetryDataStore
	minerManager       CachedMinerGetter
	deviceStore        stores.DeviceStore
	errorPoller        ErrorPoller
	metricsObserver    *metricsObserver
	// tasks queues devices for full telemetry collection (metrics, telemetry, and status).
	// Buffer sized to ConcurrencyLimit to ensure at least one queued task per worker.
	tasks chan models.Device
	// statusTasks queues devices for status-only checks (no telemetry fetch).
	// Used by statusPollingRoutine to check failed devices for recovery.
	statusTasks      chan models.Device
	lookBackDuration time.Duration
	lifecycleMu      sync.Mutex
	runCancel        context.CancelFunc
	runWG            sync.WaitGroup
	runDone          <-chan struct{}
	run              *telemetryRun
	// devicesForStatusPolling tracks all paired devices that need periodic status checks.
	// This ensures failed devices (removed from scheduler after MaxConsecutiveFailures)
	// continue to be polled for status so they can recover when they come back online.
	devicesForStatusPolling sync.Map
	broadcasters            sync.Map // map[int64]*TelemetryBroadcaster - keyed by orgID
	// lastKnownStatuses tracks the most recent status written to DB for each device.
	// Used for change detection when broadcasting status updates. Using in-memory state
	// avoids a race condition between reading old statuses and writing new ones.
	lastKnownStatuses sync.Map // map[DeviceIdentifier]MinerStatus
	lastKnownFirmware sync.Map // map[DeviceIdentifier]string
	// lastDefaultPwActive caches the last-seen default-password flag per device so
	// the poll only checks for a pairing-status change on transitions, not every poll.
	lastDefaultPwActive sync.Map // map[DeviceIdentifier]bool
	// inFlight tracks devices currently being processed by workers or request-driven refreshes.
	// statusPollingRoutine skips devices in this map to avoid double-processing the same
	// device simultaneously across the full-telemetry and status-only paths.
	inFlight sync.Map // map[DeviceIdentifier]inFlightKind
	// combinedMetricsSingle collapses identical concurrent GetCombinedMetrics
	// calls (N dashboard viewers polling the same org) into one execution.
	combinedMetricsSingle singleflight.Group
	// combinedMetricsFlights counts the callers waiting on each singleflight
	// key so the detached shared query is cancelled as soon as the last
	// waiter gives up, instead of running against the DB until its timeout.
	combinedMetricsFlightsMu sync.Mutex
	combinedMetricsFlights   map[string]*combinedMetricsFlight
}

var _ runtimejobs.Lifecycle = (*TelemetryService)(nil)

func NewTelemetryService(config Config, telemetryDataStore TelemetryDataStore, minerManager CachedMinerGetter, scheduler UpdateScheduler, deviceStore stores.DeviceStore, errorPoller ErrorPoller) *TelemetryService {
	s := &TelemetryService{
		config:                 config,
		telemetryDataStore:     telemetryDataStore,
		minerManager:           minerManager,
		updateScheduler:        scheduler,
		deviceStore:            deviceStore,
		errorPoller:            errorPoller,
		lookBackDuration:       -1 * (config.StalenessThreshold - config.FetchInterval),
		metricsObserver:        newMetricsObserver(NoMetrics()),
		combinedMetricsFlights: make(map[string]*combinedMetricsFlight),
		tasks:                  make(chan models.Device, config.ConcurrencyLimit),
		statusTasks:            make(chan models.Device, config.ConcurrencyLimit),
	}
	return s
}

func (s *TelemetryService) WithMetricsEmitter(emitter MetricsEmitter) *TelemetryService {
	s.metricsObserver = newMetricsObserver(emitter)
	return s
}

func (s *TelemetryService) AddDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error {
	if len(deviceID) == 0 {
		return nil
	}
	for _, id := range deviceID {
		device := models.Device{ID: id, LastUpdatedAt: time.Now().Add(-s.config.NewDeviceLookback)}
		select {
		case s.tasks <- device:
		case <-ctx.Done():
			return fmt.Errorf("enqueue telemetry device %s: %w", id, ctx.Err())
		}
		s.devicesForStatusPolling.Store(id, struct{}{})
		s.lastDefaultPwActive.Delete(id)
	}
	return s.updateScheduler.AddNewDevices(ctx, deviceID...)
}

func (s *TelemetryService) RemoveDevices(ctx context.Context, deviceIDs ...models.DeviceIdentifier) error {
	if len(deviceIDs) == 0 {
		return nil
	}
	for _, id := range deviceIDs {
		s.devicesForStatusPolling.Delete(id)
		s.lastKnownStatuses.Delete(id)
		s.lastKnownFirmware.Delete(id)
		s.lastDefaultPwActive.Delete(id)
		s.metricsObserver.onDeviceRemoved(ctx, id)
	}
	return s.updateScheduler.RemoveDevices(ctx, deviceIDs...)
}

// RefreshDevice runs the same collection path used by scheduled telemetry for
// one device, then asks the writers to flush pending status and metrics updates.
func (s *TelemetryService) RefreshDevice(ctx context.Context, device models.Device) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled waiting for in-flight refresh for device %s: %w", device.ID, err)
	}
	run, err := s.activeRun()
	if err != nil {
		return err
	}

	waitCtx, cancelWait := context.WithTimeout(ctx, s.refreshDeviceOperationTimeout())
	claimed, err := s.claimDeviceForRefresh(waitCtx, device.ID)
	cancelWait()
	if err != nil {
		return err
	}

	operationCtx, cancelOperation := context.WithTimeout(ctx, s.refreshDeviceOperationTimeout())
	defer cancelOperation()

	var processErr error
	if claimed {
		defer s.inFlight.Delete(device.ID)
		processErr = s.processDeviceWithResults(operationCtx, device, run.results)
	}

	flushErr := s.flushStatusForRun(operationCtx, &device.ID, run)
	metricsFlushErr := s.flushMetricsForRun(operationCtx, &device.ID, run)
	return errors.Join(processErr, flushErr, metricsFlushErr)
}

func (s *TelemetryService) RefreshDeviceTimeout() time.Duration {
	return s.refreshDeviceOperationTimeout()
}

func (s *TelemetryService) refreshDeviceOperationTimeout() time.Duration {
	metricTimeout := s.config.MetricTimeout
	if metricTimeout <= 0 {
		metricTimeout = 5 * time.Second
	}
	return metricTimeout + 5*time.Second
}

func (s *TelemetryService) claimDeviceForRefresh(ctx context.Context, deviceID models.DeviceIdentifier) (bool, error) {
	if _, alreadyClaimed := s.inFlight.LoadOrStore(deviceID, inFlightKindFullTelemetry); !alreadyClaimed {
		return true, nil
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		_, stillInFlight := s.inFlight.Load(deviceID)
		if !stillInFlight {
			if _, alreadyClaimed := s.inFlight.LoadOrStore(deviceID, inFlightKindFullTelemetry); !alreadyClaimed {
				return true, nil
			}
			continue
		}

		select {
		case <-ctx.Done():
			return false, fmt.Errorf("context cancelled waiting for in-flight refresh for device %s: %w", deviceID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *TelemetryService) FlushStatusNow(ctx context.Context) error {
	return s.flushStatus(ctx, nil)
}

func (s *TelemetryService) FlushStatusForDevice(ctx context.Context, deviceID models.DeviceIdentifier) error {
	return s.flushStatus(ctx, &deviceID)
}

func (s *TelemetryService) flushStatus(ctx context.Context, deviceID *models.DeviceIdentifier) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled before status flush request was queued: %w", err)
	}
	run, err := s.activeRun()
	if err != nil {
		return err
	}
	return s.flushStatusForRun(ctx, deviceID, run)
}

func (s *TelemetryService) flushStatusForRun(ctx context.Context, deviceID *models.DeviceIdentifier, run *telemetryRun) error {
	deadline, _ := ctx.Deadline()
	req := statusFlushRequest{
		callerDeadline: deadline,
		deviceID:       deviceID,
		done:           make(chan error, 1),
	}

	select {
	case run.statusFlushRequests <- req:
	case <-run.done:
		return errTelemetryServiceInactive
	case <-ctx.Done():
		return fmt.Errorf("context cancelled before status flush request was queued: %w", ctx.Err())
	}

	select {
	case err := <-req.done:
		return err
	case <-run.done:
		return errTelemetryServiceInactive
	case <-ctx.Done():
		return fmt.Errorf("context cancelled waiting for status flush: %w", ctx.Err())
	}
}

func (s *TelemetryService) FlushMetricsNow(ctx context.Context) error {
	return s.flushMetrics(ctx, nil)
}

func (s *TelemetryService) FlushMetricsForDevice(ctx context.Context, deviceID models.DeviceIdentifier) error {
	return s.flushMetrics(ctx, &deviceID)
}

func (s *TelemetryService) flushMetrics(ctx context.Context, deviceID *models.DeviceIdentifier) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled before metrics flush request was queued: %w", err)
	}
	run, err := s.activeRun()
	if err != nil {
		return err
	}
	return s.flushMetricsForRun(ctx, deviceID, run)
}

func (s *TelemetryService) flushMetricsForRun(ctx context.Context, deviceID *models.DeviceIdentifier, run *telemetryRun) error {
	deadline, _ := ctx.Deadline()
	req := metricsFlushRequest{
		callerDeadline: deadline,
		deviceID:       deviceID,
		done:           make(chan error, 1),
	}

	select {
	case run.metricsFlushRequests <- req:
	case <-run.done:
		return errTelemetryServiceInactive
	case <-ctx.Done():
		return fmt.Errorf("context cancelled before metrics flush request was queued: %w", ctx.Err())
	}

	select {
	case err := <-req.done:
		return err
	case <-run.done:
		return errTelemetryServiceInactive
	case <-ctx.Done():
		return fmt.Errorf("context cancelled waiting for metrics flush: %w", ctx.Err())
	}
}

func (s *TelemetryService) activeRun() (*telemetryRun, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.runCancel == nil || s.run == nil || s.run.canceled() {
		return nil, errTelemetryServiceInactive
	}
	return s.run, nil
}

// Start activates background telemetry collection for the lifetime of ctx.
func (s *TelemetryService) Start(ctx context.Context) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.runCancel != nil {
		if s.run != nil && s.run.canceled() {
			return fmt.Errorf("telemetry service is still stopping")
		}
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	runDone := make(chan struct{})
	run := newTelemetryRun(runCtx.Done())
	s.runCancel = cancel
	s.runDone = runDone
	s.run = run
	for range s.config.ConcurrencyLimit {
		run.producerWG.Go(func() { s.workerForActivation(runCtx, run.results) })
	}
	writerCtx := run.writerContext(runCtx)
	s.runWG.Go(func() { s.gatherMetricsRoutine(runCtx) })
	s.runWG.Go(func() { s.statusWriterRoutine(writerCtx, run) })
	s.runWG.Go(func() { s.metricsWriterRoutine(writerCtx, run) })
	s.runWG.Go(func() { s.devicePollingRoutine(runCtx) })
	s.runWG.Go(func() { s.statusPollingRoutine(runCtx) })
	s.runWG.Go(func() { s.fleetStateSnapshotRoutine(runCtx) })
	s.runWG.Go(func() { s.fleetMetricRollupRoutine(runCtx) })
	go s.finishRun(runDone, run)
	return nil
}

func (s *TelemetryService) finishRun(done chan struct{}, run *telemetryRun) {
	s.runWG.Wait()

	s.lifecycleMu.Lock()
	if s.runDone == (<-chan struct{})(done) && s.run == run {
		s.runCancel = nil
		s.runDone = nil
		s.run = nil
	}
	s.lifecycleMu.Unlock()
	close(done)
}

func (s *TelemetryService) Stop(ctx context.Context) error {
	s.lifecycleMu.Lock()
	if s.runCancel == nil {
		s.lifecycleMu.Unlock()
		return nil
	}
	s.runCancel()
	done := s.runDone
	s.lifecycleMu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop telemetry service: %w", ctx.Err())
	}
}

// Close stops polling and closes per-organization broadcasters during process
// teardown. Runtime demotion should call Stop so read-only streams can remain
// independent of the active polling lifecycle.
func (s *TelemetryService) Close(ctx context.Context) error {
	stopErr := s.Stop(ctx)
	s.broadcasters.Range(func(key, value any) bool {
		if broadcaster, ok := value.(*TelemetryBroadcaster); ok {
			broadcaster.Stop()
		}
		s.broadcasters.Delete(key)
		return true
	})
	return stopErr
}

// GetOrCreateBroadcaster returns the broadcaster for an organization, creating it if needed
func (s *TelemetryService) GetOrCreateBroadcaster(ctx context.Context, orgID int64) (*TelemetryBroadcaster, error) {
	if val, ok := s.broadcasters.Load(orgID); ok {
		broadcaster, ok := val.(*TelemetryBroadcaster)
		if !ok {
			return nil, fmt.Errorf("invalid broadcaster type for org %d", orgID)
		}
		return broadcaster, nil
	}

	pollInterval := defaultBroadcasterPollInterval
	if s.config.FetchInterval > 0 {
		pollInterval = s.config.FetchInterval
	}

	broadcaster := NewTelemetryBroadcaster(orgID, s.telemetryDataStore, pollInterval)

	actual, loaded := s.broadcasters.LoadOrStore(orgID, broadcaster)
	if loaded {
		actualBroadcaster, ok := actual.(*TelemetryBroadcaster)
		if !ok {
			return nil, fmt.Errorf("invalid broadcaster type for org %d", orgID)
		}
		return actualBroadcaster, nil
	}

	if err := broadcaster.Start(ctx); err != nil {
		s.broadcasters.Delete(orgID)
		return nil, fmt.Errorf("failed to start broadcaster for org %d: %w", orgID, err)
	}

	return broadcaster, nil
}

func (s *TelemetryService) gatherMetricsRoutine(ctx context.Context) {
	fetchInterval := s.config.FetchInterval
	if fetchInterval <= 0 {
		fetchInterval = defaultFetchInterval
	}
	ticker := time.NewTicker(fetchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lookback := time.Now().Add(s.lookBackDuration)
			devices, err := s.updateScheduler.FetchDevices(ctx, lookback)
			if err != nil {
				slog.Error("failed to fetch devices for telemetry", "error", err)
				continue
			}
			for _, device := range devices {
				select {
				case s.tasks <- device:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func (s *TelemetryService) devicePollingRoutine(ctx context.Context) {
	pollInterval := s.config.DevicePollInterval
	if pollInterval <= 0 {
		pollInterval = defaultDevicePollInterval
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	if err := s.loadPairedDevices(ctx); err != nil {
		slog.Error("failed to load paired devices on startup", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.loadPairedDevices(ctx); err != nil {
				slog.Error("failed to load paired devices", "error", err)
			}
		}
	}
}

func (s *TelemetryService) loadPairedDevices(ctx context.Context) error {
	deviceIDs, err := s.deviceStore.GetAllPairedDeviceIdentifiers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get paired device identifiers: %w", err)
	}

	if len(deviceIDs) == 0 {
		return nil
	}

	// AddDevices errors are expected to happen from time to time and are not critical.
	// We intentionally ignore them to allow the service to continue.
	_ = s.AddDevices(ctx, deviceIDs...)

	return nil
}

// statusPollingRoutine sends all paired devices to the statusTasks channel at regular intervals.
// This is essential for recovering failed devices: when a device exceeds MaxConsecutiveFailures,
// the scheduler stops including it in telemetry fetches. This routine ensures we continue
// checking status so devices can be restored when they come back online.
// Status tasks are processed by workers in parallel, enabling efficient handling of large fleets.
func (s *TelemetryService) statusPollingRoutine(ctx context.Context) {
	interval := s.config.DeviceStatusPollInterval
	if interval <= 0 {
		interval = defaultStatusPollingInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.devicesForStatusPolling.Range(func(key, _ any) bool {
				deviceID, ok := key.(models.DeviceIdentifier)
				if !ok {
					return true
				}

				// Skip devices that are healthy — the main telemetry loop already updates them.
				// statusPollingRoutine exists to recover failed/offline devices, not to re-poll healthy ones.
				// However, a device can be marked failed by the scheduler while its cached status is still
				// ACTIVE (set during its last successful poll before it started failing). We must not skip
				// such devices — they need recovery polling to re-enter the scheduler.
				if statusVal, ok := s.lastKnownStatuses.Load(deviceID); ok {
					if status, ok := statusVal.(mm.MinerStatus); ok && status == mm.MinerStatusActive {
						// Don't skip failed devices even if they have a cached ACTIVE status —
						// they need recovery polling to re-enter the scheduler.
						if failed, _, err := s.updateScheduler.IsFailedDevice(ctx, deviceID); err == nil && !failed {
							return true // skip: healthy and not failed
						}
					}
				}

				// Atomically claim the device; skip if already queued or processing.
				if _, alreadyClaimed := s.inFlight.LoadOrStore(deviceID, inFlightKindStatusOnly); alreadyClaimed {
					return true
				}

				select {
				case s.statusTasks <- models.Device{ID: deviceID}:
				case <-ctx.Done():
					s.inFlight.Delete(deviceID) // release claim on context cancellation
					return false
				}
				return true
			})
		}
	}
}

func (s *TelemetryService) fleetStateSnapshotRoutine(ctx context.Context) {
	interval := s.config.StateSnapshotInterval
	if interval <= 0 {
		interval = defaultStateSnapshotInterval
	}

	// Populate the live bar within seconds of startup instead of a full tick.
	s.writeFleetStateSnapshot(ctx, time.Now())

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case tickTime := <-ticker.C:
			s.writeFleetStateSnapshot(ctx, tickTime)
		}
	}
}

func (s *TelemetryService) writeFleetStateSnapshot(ctx context.Context, at time.Time) {
	if err := s.telemetryDataStore.InsertMinerStateSnapshot(ctx, at); err != nil {
		slog.Warn("snapshot routine: insert failed", "error", err)
	}
}

func (s *TelemetryService) fleetMetricRollupRoutine(ctx context.Context) {
	ticker := time.NewTicker(fleetRollupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case tickTime := <-ticker.C:
			s.writeFleetMetricRollups(ctx, tickTime)
		}
	}
}

func (s *TelemetryService) writeFleetMetricRollups(ctx context.Context, at time.Time) {
	latest, err := s.telemetryDataStore.GetLatestFleetMetricRollupBucket(ctx)
	if err != nil {
		slog.Warn("fleet metric rollup routine: latest bucket lookup failed", "error", err)
		return
	}
	startTime, endTime, ok := fleetMetricRollupWriteWindow(at, latest)
	if !ok {
		return
	}
	if err := s.telemetryDataStore.UpsertFleetMetricRollups(ctx, startTime, endTime); err != nil {
		slog.Warn("fleet metric rollup routine: upsert failed",
			"start_time", startTime,
			"end_time", endTime,
			"error", err)
	}
}

func fleetMetricRollupWriteWindow(now, latest time.Time) (startTime, endTime time.Time, ok bool) {
	endTime = models.TruncateToFleetRollupBucket(now).Add(-time.Duration(models.FleetMetricRollupRawTailBuckets) * models.FleetMetricRollupBucketDuration)
	startTime = latest.Add(models.FleetMetricRollupBucketDuration)
	if !startTime.Before(endTime) {
		return time.Time{}, time.Time{}, false
	}
	if !latest.Equal(time.Unix(0, 0).UTC()) {
		startTime = startTime.Add(-time.Duration(fleetRollupRewriteBuckets) * models.FleetMetricRollupBucketDuration)
	}

	floor := endTime.Add(-fleetRollupBackfillFloor)
	if startTime.Before(floor) {
		startTime = floor
	}
	if !startTime.Before(endTime) {
		return time.Time{}, time.Time{}, false
	}

	maxEnd := startTime.Add(time.Duration(fleetRollupMaxBucketsPerTick) * models.FleetMetricRollupBucketDuration)
	if endTime.After(maxEnd) {
		endTime = maxEnd
	}
	return startTime, endTime, true
}

// worker processes devices from task channels one at a time.
// It fetches telemetry/status from miners and sends results to the statusResults channel
// for periodic DB writes by statusWriterRoutine.
func (s *TelemetryService) worker(ctx context.Context) {
	run, err := s.activeRun()
	if err != nil {
		return
	}
	s.workerForActivation(ctx, run.results)
}

func (s *TelemetryService) workerForActivation(ctx context.Context, results telemetryResults) {
	for {
		select {
		case <-ctx.Done():
			return

		case device, ok := <-s.tasks:
			if !ok {
				return
			}
			if _, alreadyClaimed := s.inFlight.LoadOrStore(device.ID, inFlightKindFullTelemetry); alreadyClaimed {
				if err := s.updateScheduler.AddDevices(ctx, device); err != nil {
					slog.Warn("failed to requeue skipped in-flight telemetry task", "deviceID", device.ID, "error", err)
				}
				continue
			}
			_ = s.processDeviceWithResults(ctx, device, results)
			s.inFlight.Delete(device.ID)

		case device, ok := <-s.statusTasks:
			if !ok {
				return
			}
			s.processStatusOnlyWithResults(ctx, device, results.status)
			s.inFlight.Delete(device.ID)
		}
	}
}

// processDevice handles full telemetry collection for a device.
//
// Flow:
//  1. Telemetry fetch - continues on failure (we still want status updates)
//  2. Status fetch - returns early on non-connection errors (can't reliably poll errors)
//  3. Error polling - only runs if status fetch succeeded
//
// Connection errors during status fetch are converted to MinerStatusOffline (not errors),
// so the flow continues. Only auth failures and other non-connection errors cause early return.
func (s *TelemetryService) processDevice(ctx context.Context, device models.Device) error {
	run, err := s.activeRun()
	if err != nil {
		return err
	}
	return s.processDeviceWithResults(ctx, device, run.results)
}

func (s *TelemetryService) processDeviceWithResults(ctx context.Context, device models.Device, results telemetryResults) error {
	// Telemetry failure doesn't block status/error polling - we still want to track online state.
	// When metrics succeed, status is derived from the health field — no second RPC needed.
	metricsStatus, hasMetricsStatus, orgID, driverName, siteID, pollSuccess, telemetryErr := s.getTelemetryFromDevice(ctx, device, results.metrics)
	var collectionErr error
	s.metricsObserver.onPollResult(
		ctx,
		orgID,
		siteID,
		device.ID,
		pollSuccess,
	)
	if telemetryErr != nil {
		collectionErr = telemetryErr
		slog.Warn("failed to get telemetry from device", "deviceID", device.ID, "error", telemetryErr)

		if requiresCredentialRemediation(telemetryErr) {
			if updateErr := s.handleCredentialRemediation(ctx, device.ID, telemetryErr); updateErr != nil {
				slog.Error("failed to update pairing status for credential remediation",
					"deviceID", device.ID, "error", updateErr)
			}
		}

		if addErr := s.updateScheduler.AddFailedDevices(ctx, device); addErr != nil {
			slog.Warn("failed to add failed device to scheduler", "deviceID", device.ID, "error", addErr)
		}
	}

	// When metrics were fetched successfully, derive status from them to avoid a second RPC.
	// When metrics failed (device unreachable or auth error), fetch status explicitly so we can
	// detect offline state and handle auth failures in the status path.
	var status mm.MinerStatus
	if hasMetricsStatus {
		status = metricsStatus
	} else {
		var (
			statusErr        error
			statusOrg        int64
			statusSite       int64
			statusDriverName string
		)
		status, statusOrg, statusDriverName, statusSite, statusErr = s.fetchStatusFromMiner(ctx, device.ID)
		if statusErr != nil {
			slog.Warn("failed to get status for device", "deviceID", device.ID, "error", statusErr)

			if requiresCredentialRemediation(statusErr) {
				if updateErr := s.handleCredentialRemediation(ctx, device.ID, statusErr); updateErr != nil {
					slog.Error("failed to update pairing status for credential remediation",
						"deviceID", device.ID, "error", updateErr)
				}
			}
			return statusErr
		}
		// The telemetry path may have failed before resolving org/driver/site; if
		// so, fill them in from the status fetch which already has the miner handle.
		if orgID == 0 {
			orgID = statusOrg
		}
		if driverName == "" {
			driverName = statusDriverName
		}
		if siteID == 0 {
			siteID = statusSite
		}
	}

	// Send status result to writer (non-blocking to prevent worker stalls)
	select {
	case results.status <- statusResult{
		deviceIdentifier: device.ID,
		status:           status,
		orgID:            orgID,
		siteID:           siteID,
		driverName:       driverName,
	}:
	case <-ctx.Done():
		return fmt.Errorf("context cancelled enqueueing status for device %s: %w", device.ID, ctx.Err())
	default:
		slog.Error("status results channel full, dropping update", "deviceID", device.ID)
		if collectionErr == nil {
			collectionErr = fmt.Errorf("status results channel full for device %s", device.ID)
		}
	}

	s.pollErrorsForDevice(ctx, device)
	if status == mm.MinerStatusOffline && fleeterror.IsConnectionError(collectionErr) {
		return nil
	}
	return collectionErr
}

// processStatusOnly handles status-only checks for a device.
//
// This function is the recovery mechanism for failed devices. When a device exceeds
// MaxConsecutiveFailures in the main telemetry loop, the scheduler marks it as "failed"
// and stops including it in regular telemetry fetches. However, statusPollingRoutine
// continues to send ALL paired devices here for status checks.
//
// Recovery logic:
//   - A device is considered "recovered" when it returns a healthy status (not offline/error).
//   - If the device was marked as failed in the scheduler and now reports healthy, we re-add
//     it to the scheduler with its original failedAt timestamp. This ensures the scheduler
//     prioritizes it for immediate telemetry collection.
//   - Devices that remain offline/error stay in the failed state. They continue to be polled
//     here but aren't re-added to the scheduler until they report a healthy status.
//
// This design ensures devices can automatically rejoin telemetry collection when they
// come back online, without manual intervention.
func (s *TelemetryService) processStatusOnly(ctx context.Context, device models.Device) {
	run, err := s.activeRun()
	if err != nil {
		return
	}
	s.processStatusOnlyWithResults(ctx, device, run.results.status)
}

func (s *TelemetryService) processStatusOnlyWithResults(ctx context.Context, device models.Device, results chan<- statusResult) {
	status, orgID, driverName, siteID, statusErr := s.fetchStatusFromMiner(ctx, device.ID)
	if statusErr != nil {
		// Non-connection errors (e.g., auth failures) - device stays in failed state.
		// Connection errors don't reach here; they return (MinerStatusOffline, nil).
		slog.Debug("status polling failed for device", "deviceID", device.ID, "error", statusErr)

		if requiresCredentialRemediation(statusErr) {
			if updateErr := s.handleCredentialRemediation(ctx, device.ID, statusErr); updateErr != nil {
				slog.Error("failed to update pairing status for credential remediation",
					"deviceID", device.ID, "error", updateErr)
			}
		}
		return
	}

	// Only attempt recovery if device reports a healthy status.
	// Offline/error devices should not be re-added to the scheduler - they'll just fail again.
	if status != mm.MinerStatusOffline && status != mm.MinerStatusError {
		failed, failedAt, err := s.updateScheduler.IsFailedDevice(ctx, device.ID)
		if err != nil {
			slog.Warn("failed to check if device is failed", "deviceID", device.ID, "error", err)
		} else if failed {
			// Re-add with original failedAt timestamp so scheduler prioritizes it
			// for immediate telemetry collection (stale devices are fetched first).
			err := s.updateScheduler.AddDevices(ctx, models.Device{
				ID:            device.ID,
				LastUpdatedAt: failedAt,
			})
			if err != nil {
				slog.Warn("failed to re-add recovered device to scheduler", "deviceID", device.ID, "error", err)
			} else {
				slog.Info("device recovered, re-added to scheduler", "deviceID", device.ID)
			}
		}
	}

	// Always send status to DB for UI visibility (even for offline devices)
	select {
	case results <- statusResult{
		deviceIdentifier: device.ID,
		status:           status,
		orgID:            orgID,
		siteID:           siteID,
		driverName:       driverName,
	}:
	case <-ctx.Done():
		return
	default:
		slog.Error("status results channel full, dropping update", "deviceID", device.ID)
	}
}

// statusWriterRoutine collects status results from workers and writes them to DB periodically.
// This centralizes DB writes to reduce connection usage and improve throughput.
func (s *TelemetryService) statusWriterRoutine(ctx context.Context, run *telemetryRun) {
	flushInterval := s.config.StatusFlushInterval
	if flushInterval <= 0 {
		flushInterval = defaultStatusFlushInterval
	}

	type pendingStatusUpdate struct {
		status     mm.MinerStatus
		orgID      int64
		siteID     int64
		driverName string
	}
	pendingUpdates := make(map[models.DeviceIdentifier]pendingStatusUpdate)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	addPendingUpdate := func(result statusResult) {
		pendingUpdates[result.deviceIdentifier] = pendingStatusUpdate{
			status:     result.status,
			orgID:      result.orgID,
			siteID:     result.siteID,
			driverName: result.driverName,
		}
	}

	var flush func(flushCtx context.Context) statusFlushResult
	drainReadyStatusResults := func(flushCtx context.Context) statusFlushResult {
		var drainResult statusFlushResult
		for {
			select {
			case result, ok := <-run.results.status:
				if !ok {
					return drainResult
				}
				addPendingUpdate(result)
				if len(pendingUpdates) >= maxStatusBatchSize {
					drainResult = mergeStatusFlushResults(drainResult, flush(flushCtx))
				}
			default:
				return drainResult
			}
		}
	}

	flush = func(flushCtx context.Context) statusFlushResult {
		if len(pendingUpdates) == 0 {
			return statusFlushResult{}
		}
		flushCtx, cancel := boundedFlushContext(flushCtx)
		defer cancel()
		result := statusFlushResult{devices: make(map[models.DeviceIdentifier]bool)}

		// Check current DB statuses to avoid overwriting firmware update states
		// (UPDATING, REBOOT_REQUIRED) that are managed by the command execution service.
		// REBOOT_REQUIRED persists until the user triggers a reboot command from Fleet.
		deviceIDs := make([]models.DeviceIdentifier, 0, len(pendingUpdates))
		for deviceID := range pendingUpdates {
			deviceIDs = append(deviceIDs, deviceID)
			result.devices[deviceID] = true
		}
		currentStatuses, err := s.deviceStore.GetDeviceStatusForDeviceIdentifiers(flushCtx, deviceIDs)
		if err != nil {
			slog.Warn("failed to check current device statuses for firmware update guard, skipping flush", "error", err)
			result.err = err
			return result
		}

		statusUpdates := make([]stores.DeviceStatusUpdate, 0, len(pendingUpdates))
		result.devices = make(map[models.DeviceIdentifier]bool)
		for deviceID, pending := range pendingUpdates {
			if currentStatuses != nil {
				if currentStatus, ok := currentStatuses[deviceID]; ok {
					if currentStatus == mm.MinerStatusUpdating || currentStatus == mm.MinerStatusRebootRequired {
						continue
					}
				}
			}
			statusUpdates = append(statusUpdates, stores.DeviceStatusUpdate{
				DeviceIdentifier: deviceID,
				Status:           pending.status,
			})
			result.devices[deviceID] = true
		}

		// Write new statuses to DB in a single bulk INSERT.
		// Each row is ~100 bytes. With maxStatusBatchSize=500, batches are ~50KB.
		upsertOK := true
		if len(statusUpdates) > 0 {
			if err := s.deviceStore.UpsertDeviceStatuses(flushCtx, statusUpdates); err != nil {
				slog.Error("status upsert failed", "count", len(statusUpdates), "error", err)
				upsertOK = false
				result.err = err
			}
		}

		if upsertOK {
			// Broadcast status changes using in-memory state for change detection.
			for _, u := range statusUpdates {
				oldStatus, hadOldStatus := s.lastKnownStatuses.Load(u.DeviceIdentifier)
				oldStatusTyped, validType := oldStatus.(mm.MinerStatus)
				statusChanged := !hadOldStatus || !validType || oldStatusTyped != u.Status

				if statusChanged {
					// Store BEFORE broadcasting to ensure in-memory state is current
					// before any broadcast handlers execute.
					s.lastKnownStatuses.Store(u.DeviceIdentifier, u.Status)
					s.broadcasters.Range(func(_, value any) bool {
						if broadcaster, ok := value.(*TelemetryBroadcaster); ok {
							broadcaster.PublishStatusChange(u.DeviceIdentifier, u.Status)
						}
						return true
					})
				}
			}
		}

		for deviceID, pending := range pendingUpdates {
			s.metricsObserver.onDeviceStatus(
				flushCtx,
				pending.orgID,
				pending.siteID,
				pending.driverName,
				deviceID,
				pending.status,
			)
		}

		clear(pendingUpdates)
		return result
	}

	for {
		select {
		case <-ctx.Done():
			// Use a fresh context with timeout for final flush to ensure pending
			// updates are written even after the parent context is cancelled.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
			_ = drainReadyStatusResults(shutdownCtx)
			_ = flush(shutdownCtx)
			cancel()
			return

		case result, ok := <-run.results.status:
			if !ok {
				return
			}
			addPendingUpdate(result)
			if len(pendingUpdates) >= maxStatusBatchSize {
				_ = flush(ctx)
			}

		case <-ticker.C:
			_ = flush(ctx)

		case req := <-run.statusFlushRequests:
			requestCtx, cancel := requestFlushContext(ctx, req.callerDeadline)
			result := mergeStatusFlushResults(drainReadyStatusResults(requestCtx), flush(requestCtx))
			cancel()
			req.done <- result.errorForDevice(req.deviceID)
		}
	}
}

// handleCredentialRemediation sets the pairing state matching the failure:
// DEFAULT_PASSWORD for a default-password rig, otherwise AUTHENTICATION_NEEDED.
func (s *TelemetryService) handleCredentialRemediation(ctx context.Context, deviceID models.DeviceIdentifier, cause error) error {
	if isDefaultPasswordRemediationError(cause) {
		eligible, updated, err := s.deviceStore.ReconcileDefaultPasswordPairingStatusByIdentifier(ctx, string(deviceID), pairing.StatusDefaultPassword)
		if err != nil {
			return fmt.Errorf("failed to reconcile default-password pairing status for device %s: %w", deviceID, err)
		}
		if updated {
			s.minerManager.InvalidateMiner(deviceID)
		}
		if !eligible {
			slog.Debug("skipping default-password credential remediation for non paired-like device",
				"device_id", deviceID)
		}
		return nil
	}

	eligible, updated, err := s.deviceStore.ReconcileAuthenticationNeededPairingStatusByIdentifier(ctx, string(deviceID))
	if err != nil {
		return fmt.Errorf("failed to reconcile auth-needed pairing status for device %s: %w", deviceID, err)
	}
	if updated {
		s.minerManager.InvalidateMiner(deviceID)
	}
	if !eligible {
		slog.Debug("skipping auth-needed credential remediation for non eligible device",
			"device_id", deviceID)
	}

	return nil
}

func requiresCredentialRemediation(err error) bool {
	return fleeterror.IsAuthenticationError(err) || isDefaultPasswordRemediationError(err)
}

func isDefaultPasswordRemediationError(err error) bool {
	if !fleeterror.IsForbiddenError(err) {
		return false
	}
	// Substrings match what Proto firmware emits today. Extending coverage to a
	// second driver belongs here — the shared SDK intentionally doesn't encode
	// firmware-specific response text.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "default password must be changed") ||
		strings.Contains(msg, "default_password_active")
}

// pollErrorsForDevice polls errors from a device alongside telemetry collection.
// If no errorPoller is configured, this is a no-op.
func (s *TelemetryService) pollErrorsForDevice(ctx context.Context, device models.Device) {
	if s.errorPoller == nil {
		return
	}

	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, device.ID)
	if err != nil {
		slog.Debug("failed to get miner for error polling", "deviceID", device.ID, "error", err)
		return
	}

	result := s.errorPoller.PollErrors(ctx, miner)
	if result.UpsertsFailed > 0 {
		slog.Debug("error polling had upsert failures",
			"deviceID", device.ID,
			"upsertsFailed", result.UpsertsFailed,
			"errorsUpserted", result.ErrorsUpserted)
	}
}

// persistFirmwareVersionIfChanged updates the discovered_device table when the
// firmware version reported by the device differs from the last known value.
//
// Telemetry firmware_version comes from a proto3 string without field presence,
// so an empty string is ambiguous: the driver may have omitted the field rather
// than explicitly reporting "no firmware version". We therefore treat empty
// telemetry values as "no update" instead of clearing stored firmware.
func (s *TelemetryService) persistFirmwareVersionIfChanged(ctx context.Context, deviceID models.DeviceIdentifier, firmwareVersion string) {
	if firmwareVersion == "" {
		return
	}
	oldFW, _ := s.lastKnownFirmware.Load(deviceID)
	if oldFW == firmwareVersion {
		return
	}
	if err := s.deviceStore.UpdateFirmwareVersion(ctx, deviceID, firmwareVersion); err != nil {
		slog.Error("failed to update firmware version", "device_id", deviceID, "error", err)
		return
	}
	s.lastKnownFirmware.Store(deviceID, firmwareVersion)
}

// reconcileDefaultPasswordState syncs pairing status with the rig's
// default-password flag (PAIRED <-> DEFAULT_PASSWORD), writing only when the flag
// differs from the last value seen for the device (cached in memory). A nil
// activePtr means undetermined (older plugin or failed probe), so the status is
// left untouched rather than demoting a still-default-password device.
func (s *TelemetryService) reconcileDefaultPasswordState(ctx context.Context, deviceID models.DeviceIdentifier, activePtr *bool) {
	if activePtr == nil {
		return
	}
	active := *activePtr

	prev, seen := s.lastDefaultPwActive.Load(deviceID)
	prevActive, _ := prev.(bool)
	if seen && prevActive == active {
		return
	}

	status := pairing.StatusPaired
	if active {
		status = pairing.StatusDefaultPassword
	}
	eligible, updated, err := s.deviceStore.ReconcileDefaultPasswordPairingStatusByIdentifier(ctx, string(deviceID), status)
	if err != nil {
		slog.Error("failed to reconcile default-password pairing status",
			"device_id", deviceID, "default_password_active", active, "error", err)
		return
	}
	if !eligible {
		s.lastDefaultPwActive.Store(deviceID, active)
		slog.Debug("skipping default-password pairing reconciliation for non paired-like device",
			"device_id", deviceID, "default_password_active", active, "target_status", status)
		return
	}
	if updated {
		s.minerManager.InvalidateMiner(deviceID)
	}
	s.lastDefaultPwActive.Store(deviceID, active)
}

func (s *TelemetryService) fetchTelemetryFromMiner(ctx context.Context, device models.Device) (*deviceResult, error) {
	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, device.ID)
	if err != nil {
		return nil, err
	}

	result := &deviceResult{
		device:     device,
		orgID:      miner.GetOrgID(),
		siteID:     miner.GetSiteID(),
		driverName: miner.GetDriverName(),
	}
	result.metrics, result.metricsErr = miner.GetDeviceMetrics(ctx)
	if result.metricsErr == nil {
		trustedID := string(device.ID)
		if result.metrics.DeviceIdentifier != "" && result.metrics.DeviceIdentifier != trustedID {
			slog.Warn("dropping telemetry sample with plugin-reported device identifier that does not match trusted ID",
				"requested_device_id", trustedID,
				"reported_device_id", result.metrics.DeviceIdentifier,
				"driver", result.driverName,
			)
			return result, fmt.Errorf("plugin returned mismatched device identifier %q for device %s", result.metrics.DeviceIdentifier, device.ID)
		}
		result.metrics.DeviceIdentifier = trustedID
		result.status, result.hasStatus = healthStatusToMinerStatus(result.metrics.Health)
	}
	return result, nil
}

// healthStatusToMinerStatus converts a V2 HealthStatus from fetched metrics into a MinerStatus.
// Returns false when the status is ambiguous and GetDeviceStatus must be used instead.
// HealthHealthyInactive is ambiguous because the V2 model collapses sdk.HealthNeedsMiningPool
// into it, making it impossible to distinguish MinerStatusInactive from MinerStatusNeedsMiningPool.
func healthStatusToMinerStatus(health modelsV2.HealthStatus) (mm.MinerStatus, bool) {
	switch health {
	case modelsV2.HealthHealthyActive:
		return mm.MinerStatusActive, true
	case modelsV2.HealthWarning:
		return mm.MinerStatusActive, true // Still operational despite warning
	case modelsV2.HealthCritical:
		return mm.MinerStatusError, true
	case modelsV2.HealthUnknown:
		return mm.MinerStatusOffline, true
	case modelsV2.HealthHealthyInactive:
		return mm.MinerStatusUnknown, false
	}
	return mm.MinerStatusOffline, true
}

// fetchStatusFromMiner gets the status from a miner device.
// Connection errors are treated as a valid "offline" state and return (MinerStatusOffline, orgID, driver, nil).
// Only non-connection errors (e.g., authentication failures) return an error.
func (s *TelemetryService) fetchStatusFromMiner(ctx context.Context, deviceID models.DeviceIdentifier) (mm.MinerStatus, int64, string, int64, error) {
	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, deviceID)
	if err != nil {
		if fleeterror.IsConnectionError(err) {
			orgID, driverName, siteID := s.resolveTrustedDeviceMetadata(ctx, deviceID)
			return mm.MinerStatusOffline, orgID, driverName, siteID, nil
		}
		if fleeterror.IsAuthenticationError(err) {
			s.minerManager.InvalidateMiner(deviceID)
		}
		return mm.MinerStatusUnknown, 0, "", 0, err
	}
	orgID, driverName, siteID := miner.GetOrgID(), miner.GetDriverName(), miner.GetSiteID()
	status, err := miner.GetDeviceStatus(ctx)
	if err != nil {
		if fleeterror.IsConnectionError(err) {
			return mm.MinerStatusOffline, orgID, driverName, siteID, nil
		}
		if fleeterror.IsAuthenticationError(err) {
			s.minerManager.InvalidateMiner(deviceID)
		}
		return mm.MinerStatusUnknown, orgID, driverName, siteID, err
	}
	return status, orgID, driverName, siteID, nil
}

// resolveTrustedDeviceMetadata reads (org_id, driver_name, site_id) from the device store.
// Errors are logged at debug and silently downgrade to (0, "", 0) — the caller is already on a
// degraded path and a missing fallback should not propagate further.
func (s *TelemetryService) resolveTrustedDeviceMetadata(ctx context.Context, deviceID models.DeviceIdentifier) (int64, string, int64) {
	orgID, driverName, siteID, err := s.deviceStore.GetDeviceOrgDriverAndSite(ctx, deviceID)
	if err != nil {
		slog.Debug("failed to resolve trusted org/driver/site for device",
			"device_id", deviceID, "error", err)
		return 0, "", 0
	}
	return orgID, driverName, siteID
}

// GetTelemetryFromDevice fetches telemetry data from a device and stores it.
// Returns the derived MinerStatus, whether it is unambiguous, the resolved org ID,
// the driver name, whether the underlying metrics fetch (miner.GetDeviceMetrics)
// succeeded, and any error. The first bool is false when the health status is
// ambiguous; see healthStatusToMinerStatus.
func (s *TelemetryService) GetTelemetryFromDevice(ctx context.Context, device models.Device) (mm.MinerStatus, bool, int64, string, int64, bool, error) {
	run, err := s.activeRun()
	if err != nil {
		return mm.MinerStatusUnknown, false, 0, "", 0, false, err
	}
	return s.getTelemetryFromDevice(ctx, device, run.results.metrics)
}

func (s *TelemetryService) getTelemetryFromDevice(ctx context.Context, device models.Device, results chan<- metricsResult) (mm.MinerStatus, bool, int64, string, int64, bool, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, s.config.MetricTimeout)
	defer cancel()

	result, err := s.fetchTelemetryFromMiner(fetchCtx, device)
	if err != nil {
		var orgID, siteID int64
		var driverName string
		if result != nil {
			orgID, driverName, siteID = result.orgID, result.driverName, result.siteID
		} else {
			orgID, driverName, siteID = s.resolveTrustedDeviceMetadata(ctx, device.ID)
		}
		return mm.MinerStatusUnknown, false, orgID, driverName, siteID, false, fmt.Errorf("failed to fetch telemetry from device ID %s: %w", device.ID, err)
	}

	pollSuccess := result.metricsErr == nil

	if pollSuccess {
		// Use the caller's ctx (not fetchCtx) so that MetricTimeout expiry does not
		// prevent enqueueing metrics we already fetched. Only give up if the service
		// itself is shutting down (ctx cancelled by the root context).
		select {
		case results <- metricsResult{
			deviceID:   device.ID,
			orgID:      result.orgID,
			siteID:     result.siteID,
			driverName: result.driverName,
			metrics:    result.metrics,
		}:
		case <-ctx.Done():
			return mm.MinerStatusUnknown, false, result.orgID, result.driverName, result.siteID, pollSuccess, fmt.Errorf("context cancelled enqueueing metrics for device %s: %w", device.ID, ctx.Err())
		}

		s.persistFirmwareVersionIfChanged(ctx, device.ID, result.metrics.FirmwareVersion)
		s.reconcileDefaultPasswordState(ctx, device.ID, result.metrics.DefaultPasswordActive)
	}

	if err := s.updateScheduler.AddDevices(ctx, models.Device{
		ID:            device.ID,
		LastUpdatedAt: time.Now(),
	}); err != nil {
		return mm.MinerStatusUnknown, false, result.orgID, result.driverName, result.siteID, pollSuccess, fmt.Errorf("failed to update device last updated time for device %s: %w", device.ID, err)
	}
	return result.status, result.hasStatus, result.orgID, result.driverName, result.siteID, pollSuccess, nil
}
func (s *TelemetryService) metricsWriterRoutine(ctx context.Context, run *telemetryRun) {
	flushInterval := s.config.StatusFlushInterval
	if flushInterval <= 0 {
		flushInterval = defaultMetricsFlushInterval
	}

	pending := make([]modelsV2.DeviceMetrics, 0, maxMetricsBatchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func(flushCtx context.Context) metricsFlushResult {
		if len(pending) == 0 {
			return metricsFlushResult{}
		}
		flushCtx, cancel := boundedFlushContext(flushCtx)
		defer cancel()
		result := metricsFlushResult{deviceErrors: make(map[models.DeviceIdentifier]error)}
		if err := s.telemetryDataStore.StoreDeviceMetrics(flushCtx, pending...); err != nil {
			// The store wraps the batch in a single transaction, so one bad row fails
			// the whole batch. Retry individually so only the offending sample is dropped.
			slog.Warn("batch metrics write failed, retrying individually", "count", len(pending), "error", err)
			for _, m := range pending {
				if err := s.telemetryDataStore.StoreDeviceMetrics(flushCtx, m); err != nil {
					slog.Error("failed to store device metrics", "device_id", m.DeviceIdentifier, "error", err)
					deviceID := models.DeviceIdentifier(m.DeviceIdentifier)
					result.err = errors.Join(result.err, err)
					result.deviceErrors[deviceID] = errors.Join(result.deviceErrors[deviceID], err)
				}
			}
		}
		pending = pending[:0]
		return result
	}

	forwardMetrics := func(result metricsResult) {
		pending = append(pending, result.metrics)
		s.metricsObserver.onDeviceMetrics(
			ctx,
			result.orgID,
			result.siteID,
			result.driverName,
			result.deviceID,
			result.metrics,
		)
	}

	drainReadyMetricsResults := func(flushCtx context.Context) metricsFlushResult {
		var drainResult metricsFlushResult
		for {
			select {
			case result, ok := <-run.results.metrics:
				if !ok {
					return drainResult
				}
				forwardMetrics(result)
				if len(pending) >= maxMetricsBatchSize {
					drainResult = mergeMetricsFlushResults(drainResult, flush(flushCtx))
				}
			default:
				return drainResult
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			// Drain already-queued metrics into pending before the final flush.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
			_ = drainReadyMetricsResults(shutdownCtx)
			_ = flush(shutdownCtx)
			cancel()
			return
		case result := <-run.results.metrics:
			forwardMetrics(result)
			if len(pending) >= maxMetricsBatchSize {
				_ = flush(ctx)
			}
		case <-ticker.C:
			_ = flush(ctx)
		case req := <-run.metricsFlushRequests:
			requestCtx, cancel := requestFlushContext(ctx, req.callerDeadline)
			result := mergeMetricsFlushResults(drainReadyMetricsResults(requestCtx), flush(requestCtx))
			cancel()
			req.done <- result.errorForDevice(req.deviceID)
		}
	}
}

func (s *TelemetryService) StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
	return s.telemetryDataStore.StreamTelemetryUpdates(ctx, query)
}

func (s *TelemetryService) StreamDeviceStatusUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
	updateChan := make(chan models.TelemetryUpdate)

	go func() {
		defer close(updateChan)
		heartbeatInterval := *query.HeartbeatInterval
		if heartbeatInterval <= 0 {
			heartbeatInterval = defaultHeartbeatInterval
		}
		ticker := time.NewTicker(heartbeatInterval)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				statuses, err := s.deviceStore.GetDeviceStatusForDeviceIdentifiers(ctx, query.DeviceIDs)
				if err != nil {
					slog.Error("failed to get device status", "deviceIDs", query.DeviceIDs, "error", err)
					continue
				}
				for deviceID, status := range statuses {
					update := models.TelemetryUpdate{
						Type:             models.UpdateTypeDeviceStatus,
						DeviceIdentifier: deviceID,
						Timestamp:        time.Now(),
						DeviceStatus:     &status,
					}
					select {
					case updateChan <- update:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return updateChan, nil
}

// GetCombinedMetrics collapses identical concurrent queries into a single
// execution via singleflight: N dashboard viewers polling the same org would
// otherwise each run the same heavy aggregation every refresh.
//
// The shared query runs on a context detached from any individual caller
// (context.WithoutCancel + a fixed timeout) so that a cancellation of
// whichever caller raced into singleflight first does not poison the result
// for siblings whose own contexts are still valid. Each caller then selects
// between the shared result and its own ctx independently. Waiters are
// counted per flight; when the last one gives up the shared query is
// cancelled rather than left burning the DB until its timeout.
func (s *TelemetryService) GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	// The store resolves nil bounds against time.Now() at execution, so two
	// concurrent nil-bound queries are not interchangeable: a follower could
	// receive a window anchored to the leader's execution time. Run them
	// without singleflight on the caller's own context.
	if query.TimeRange.StartTime == nil || query.TimeRange.EndTime == nil {
		return s.fetchCombinedMetrics(ctx, query)
	}

	query = quantizeCombinedMetricsWindow(query)
	key := combinedMetricsFlightKey(query)
	flight := s.joinCombinedMetricsFlight(key)
	defer s.leaveCombinedMetricsFlight(key, flight)

	ch := s.combinedMetricsSingle.DoChan(key, func() (any, error) {
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), combinedMetricsFlightTimeout)
		defer cancel()
		s.armCombinedMetricsFlight(flight, cancel)
		return s.fetchCombinedMetrics(flightCtx, query)
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			// Errors come from fetchCombinedMetrics, which returns store and
			// domain errors unchanged. Pass through as before.
			return models.CombinedMetric{}, res.Err //nolint:wrapcheck
		}
		result, ok := res.Val.(models.CombinedMetric)
		if !ok {
			return models.CombinedMetric{}, fleeterror.NewInternalErrorf("unexpected type from combined metrics singleflight: %T", res.Val)
		}
		return result, nil
	case <-ctx.Done():
		// This caller gave up. The deferred leave decrements the waiter count;
		// siblings still in the flight keep the detached query alive, and the
		// last one out cancels it.
		return models.CombinedMetric{}, ctx.Err() //nolint:wrapcheck
	}
}

// combinedMetricsFlight tracks how many callers are waiting on one
// singleflight execution, plus the cancel func of the running flight.
type combinedMetricsFlight struct {
	waiters int
	cancel  context.CancelFunc
}

func (s *TelemetryService) joinCombinedMetricsFlight(key string) *combinedMetricsFlight {
	s.combinedMetricsFlightsMu.Lock()
	defer s.combinedMetricsFlightsMu.Unlock()
	flight := s.combinedMetricsFlights[key]
	if flight == nil {
		flight = &combinedMetricsFlight{}
		s.combinedMetricsFlights[key] = flight
	}
	flight.waiters++
	return flight
}

// leaveCombinedMetricsFlight is called once per caller on every exit path.
// The last waiter out cancels the shared query and forgets the key so later
// callers start a fresh flight instead of joining a cancelled one.
func (s *TelemetryService) leaveCombinedMetricsFlight(key string, flight *combinedMetricsFlight) {
	s.combinedMetricsFlightsMu.Lock()
	defer s.combinedMetricsFlightsMu.Unlock()
	flight.waiters--
	if flight.waiters > 0 {
		return
	}
	if flight.cancel != nil {
		flight.cancel()
	}
	delete(s.combinedMetricsFlights, key)
	s.combinedMetricsSingle.Forget(key)
}

// armCombinedMetricsFlight registers the running flight's cancel func so the
// last departing waiter can stop it. If every waiter already left before the
// flight got scheduled, cancel immediately.
func (s *TelemetryService) armCombinedMetricsFlight(flight *combinedMetricsFlight, cancel context.CancelFunc) {
	s.combinedMetricsFlightsMu.Lock()
	flight.cancel = cancel
	abandoned := flight.waiters == 0
	s.combinedMetricsFlightsMu.Unlock()
	if abandoned {
		cancel()
	}
}

func (s *TelemetryService) fetchCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	// Site scope is applied by resolving the in-scope device identifiers and
	// feeding the existing device-list paths: the telemetry continuous
	// aggregates have no site_id column, so we cannot filter them directly.
	// This scopes line metrics, status counts, and the live uptime bar
	// uniformly to the site's current devices.
	hadExplicitDevices := len(query.DeviceIDs) > 0
	if len(query.SiteIDs) > 0 || query.IncludeUnassigned {
		identifiers, err := s.deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, query.OrganizationID, &stores.MinerFilter{
			SiteIDs:           query.SiteIDs,
			IncludeUnassigned: query.IncludeUnassigned,
			// Resolve the same paired-like set the dashboard counts (PAIRED +
			// AUTHENTICATION_NEEDED + DEFAULT_PASSWORD); the resolver otherwise
			// defaults to PAIRED-only and would drop auth-needed/default-password
			// devices that FleetHealth still counts.
			PairingStatuses: pairedLikeStatuses,
		})
		if err != nil {
			return models.CombinedMetric{}, err
		}
		// Site scope is AND'd with any explicit device selector: intersect the
		// resolved in-scope devices with an existing device list rather than
		// replacing it. No devices in scope (empty resolution or empty
		// intersection) returns empty rather than falling through to the
		// "empty device list = all devices" path.
		scoped := intersectDeviceIDs(query.DeviceIDs, models.ToDeviceIdentifiers(identifiers))
		if len(scoped) == 0 {
			return models.CombinedMetric{}, nil
		}
		query.DeviceIDs = scoped
		query.DeviceListFromSiteScope = !hadExplicitDevices
	}

	// Returns raw values (H/s, W, J/H) - conversion to display units happens in the handler layer
	result, err := s.telemetryDataStore.GetCombinedMetrics(ctx, query)
	if err != nil {
		return result, err
	}
	if models.ShouldIncludeUptimeStatusCounts(query.MeasurementTypes) {
		s.appendLiveUptimeBar(ctx, query.OrganizationID, query.DeviceIDs, &result)
	}
	return result, nil
}

// appendLiveUptimeBar tacks a synthetic "now" bucket onto UptimeStatusCounts
// built from a live CountMinersByState call, and populates MinerStateCounts.
// Without this the right-most chart bar lags by up to one snapshot interval
// because it's read from the miner_state_snapshots table.
func (s *TelemetryService) appendLiveUptimeBar(ctx context.Context, orgID int64, deviceIDs []models.DeviceIdentifier, result *models.CombinedMetric) {
	if orgID == 0 {
		return
	}
	counts, err := s.deviceStore.GetMinerStateCounts(ctx, orgID, minerFilterForDeviceIDs(deviceIDs))
	if err != nil {
		slog.Warn("failed to compute live miner state counts", "error", err)
		return
	}
	result.MinerStateCounts = &models.MinerStateCounts{
		Hashing:  counts.HashingCount,
		Broken:   counts.BrokenCount,
		Offline:  counts.OfflineCount,
		Sleeping: counts.SleepingCount,
	}
	result.UptimeStatusCounts = append(result.UptimeStatusCounts, models.UptimeStatusCount{
		Timestamp:       time.Now(),
		HashingCount:    counts.HashingCount,
		BrokenCount:     counts.BrokenCount,
		NotHashingCount: counts.OfflineCount + counts.SleepingCount,
	})
}

// pairedLikeStatuses is the fleet-visible "paired-like" set the dashboard
// reports on (matches GetMinerStateCounts / the site-stats device resolution).
var pairedLikeStatuses = []fm.PairingStatus{
	fm.PairingStatus_PAIRING_STATUS_PAIRED,
	fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
	fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
}

// intersectDeviceIDs returns the device IDs present in both sets. When the
// caller supplied no explicit device list (selected == empty), the site scope
// alone applies and inScope is returned as-is.
func intersectDeviceIDs(selected, inScope []models.DeviceIdentifier) []models.DeviceIdentifier {
	if len(selected) == 0 {
		return inScope
	}
	allowed := make(map[models.DeviceIdentifier]struct{}, len(inScope))
	for _, id := range inScope {
		allowed[id] = struct{}{}
	}
	result := make([]models.DeviceIdentifier, 0, len(selected))
	for _, id := range selected {
		if _, ok := allowed[id]; ok {
			result = append(result, id)
		}
	}
	return result
}

// quantizeCombinedMetricsWindow rounds EndTime down to combinedMetricsQuantum
// and shifts StartTime by the same delta, preserving duration. Applied before
// keying and before executing so every follower's key matches the query the
// leader actually ran. Queries requesting resolution finer than
// combinedMetricsQuantizeMinInterval pass through untouched: the shift would
// be visible at their bucket size, so they keep exact bounds and only
// coalesce with identical queries. Nil bounds never get here;
// GetCombinedMetrics runs them outside singleflight.
func quantizeCombinedMetricsWindow(query models.CombinedMetricsQuery) models.CombinedMetricsQuery {
	if query.TimeRange.EndTime == nil {
		return query
	}
	if query.SlideInterval == nil || *query.SlideInterval < combinedMetricsQuantizeMinInterval {
		return query
	}
	end := query.TimeRange.EndTime.Truncate(combinedMetricsQuantum)
	delta := query.TimeRange.EndTime.Sub(end)
	query.TimeRange.EndTime = &end
	if query.TimeRange.StartTime != nil {
		start := query.TimeRange.StartTime.Add(-delta)
		query.TimeRange.StartTime = &start
	}
	return query
}

// combinedMetricsFlightKey builds the singleflight key for a quantized query.
// Every field of models.CombinedMetricsQuery that changes the result
// participates. MeasurementTypes and AggregationTypes keep caller order
// because the store emits metrics and aggregated values in request slice
// order, so only identically ordered requests may share a response. DeviceIDs
// and SiteIDs are pure filters; they are sorted on copies so set-equal
// selections collapse. PageSize and PaginationToken are excluded because the
// combined-metrics path ignores them; add them back if pagination is ever
// implemented.
func combinedMetricsFlightKey(query models.CombinedMetricsQuery) string {
	var b strings.Builder
	b.WriteString(strconv.FormatInt(query.OrganizationID, 10))
	b.WriteByte('|')
	writeKeyTime(&b, query.TimeRange.StartTime)
	b.WriteByte('|')
	writeKeyTime(&b, query.TimeRange.EndTime)
	b.WriteByte('|')
	writeKeyDuration(&b, query.WindowDuration)
	b.WriteByte('|')
	writeKeyDuration(&b, query.SlideInterval)
	b.WriteByte('|')
	writeInts(&b, query.MeasurementTypes)
	b.WriteByte('|')
	writeInts(&b, query.AggregationTypes)
	b.WriteByte('|')
	b.WriteString(deviceIDsKeyHash(query.DeviceIDs))
	b.WriteByte('|')
	writeSortedInts(&b, query.SiteIDs)
	b.WriteByte('|')
	b.WriteString(strconv.FormatBool(query.IncludeUnassigned))
	b.WriteByte('|')
	b.WriteString(strconv.FormatBool(query.DeviceListFromSiteScope))
	return b.String()
}

func writeKeyTime(b *strings.Builder, t *time.Time) {
	if t == nil {
		b.WriteString("nil")
		return
	}
	b.WriteString(strconv.FormatInt(t.UnixNano(), 10))
}

func writeKeyDuration(b *strings.Builder, d *time.Duration) {
	if d == nil {
		b.WriteString("nil")
		return
	}
	b.WriteString(strconv.FormatInt(int64(*d), 10))
}

func writeInts[T ~int | ~int64](b *strings.Builder, vals []T) {
	for i, v := range vals {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(int64(v), 10))
	}
}

func writeSortedInts[T ~int | ~int64](b *strings.Builder, vals []T) {
	sorted := slices.Clone(vals)
	slices.Sort(sorted)
	writeInts(b, sorted)
}

// deviceIDsKeyHash hashes the sorted device list so keys stay bounded for
// multi-thousand-device selections. A NUL separator keeps the hash injective
// over the ID sequence.
func deviceIDsKeyHash(ids []models.DeviceIdentifier) string {
	if len(ids) == 0 {
		return "all"
	}
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	h := sha256.New()
	for _, id := range sorted {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func minerFilterForDeviceIDs(deviceIDs []models.DeviceIdentifier) *stores.MinerFilter {
	if len(deviceIDs) == 0 {
		return nil
	}
	identifiers := make([]string, len(deviceIDs))
	for i, id := range deviceIDs {
		identifiers[i] = string(id)
	}
	return &stores.MinerFilter{DeviceIdentifiers: identifiers}
}

func (s *TelemetryService) StreamCombinedMetrics(ctx context.Context, query models.StreamCombinedMetricsQuery) (<-chan models.CombinedMetric, error) {
	updateChan := make(chan models.CombinedMetric)

	// Ensure granularity is set to avoid divide-by-zero
	granularity := query.Granularity
	if granularity == 0 {
		granularity = defaultUpdateInterval
	}

	updateInterval := query.UpdateInterval
	if updateInterval == 0 {
		updateInterval = granularity
	}

	// Update query with defaulted values
	query.Granularity = granularity
	query.UpdateInterval = updateInterval

	go func() {
		defer close(updateChan)

		if err := s.sendCombinedMetricUpdate(ctx, updateChan, query, updateInterval); err != nil {
			slog.Error("failed to send initial combined metric update", "error", err)
			return
		}

		now := time.Now()
		intervalNanos := updateInterval.Nanoseconds()
		nextAlignedTime := time.Unix(0, ((now.UnixNano()/intervalNanos)+1)*intervalNanos)

		initialDelay := nextAlignedTime.Sub(now)
		initialTimer := time.NewTimer(initialDelay)

		select {
		case <-ctx.Done():
			initialTimer.Stop()
			return
		case <-initialTimer.C:
			if err := s.sendCombinedMetricUpdate(ctx, updateChan, query, updateInterval); err != nil {
				slog.Error("failed to send aligned combined metric update", "error", err)
				return
			}
		}

		ticker := time.NewTicker(updateInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.sendCombinedMetricUpdate(ctx, updateChan, query, updateInterval); err != nil {
					slog.Error("failed to send combined metric update", "error", err)
					return
				}
			}
		}
	}()

	return updateChan, nil
}

func (s *TelemetryService) sendCombinedMetricUpdate(ctx context.Context, updateChan chan<- models.CombinedMetric, query models.StreamCombinedMetricsQuery, updateInterval time.Duration) error {
	combinedQuery := models.CombinedMetricsQuery{
		DeviceIDs:        query.DeviceIDs,
		MeasurementTypes: query.MeasurementTypes,
		AggregationTypes: query.AggregationTypes,
		SlideInterval:    &query.Granularity,
		PageSize:         defaultCombinedMetricsPageSize,
		OrganizationID:   query.OrganizationID,
	}

	now := time.Now()

	// IMPORTANT: The time window must be at least as wide as the granularity (bucket size)
	// to ensure we capture complete buckets of data. If updateInterval < granularity,
	// using updateInterval for the window width would result in no complete buckets.
	//
	// Example problem:
	//   - Granularity (bucket size): 5 minutes
	//   - UpdateInterval: 100ms
	//   - Window using updateInterval: [now-100ms, now] - captures 0 complete 5-min buckets!
	//
	// Solution: Use granularity as the minimum window width
	windowWidth := max(query.Granularity, updateInterval)

	// Align end time to bucket boundaries for consistent results
	granularityNanos := query.Granularity.Nanoseconds()
	alignedEndTime := time.Unix(0, (now.UnixNano()/granularityNanos)*granularityNanos)

	if alignedEndTime.After(now) {
		alignedEndTime = alignedEndTime.Add(-query.Granularity)
	}

	startTime := alignedEndTime.Add(-windowWidth)

	combinedQuery.TimeRange = models.TimeRange{
		StartTime: &startTime,
		EndTime:   &alignedEndTime,
	}

	combinedMetrics, err := s.telemetryDataStore.GetCombinedMetrics(ctx, combinedQuery)
	if err != nil {
		if strings.Contains(err.Error(), "no combined metrics found") {
			combinedMetrics = models.CombinedMetric{
				Metrics: []models.Metric{},
			}
		} else {
			return fmt.Errorf("failed to get combined metrics: %w", err)
		}
	}

	if models.ShouldIncludeUptimeStatusCounts(query.MeasurementTypes) {
		s.appendLiveUptimeBar(ctx, query.OrganizationID, query.DeviceIDs, &combinedMetrics)
	}

	select {
	case updateChan <- combinedMetrics:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	}
}

// SubscribeToTelemetryUpdates subscribes to raw telemetry updates for an organization
// This allows consumers to receive telemetry events without the conversion to protobuf responses
// eventTypes filters which event types to receive (empty means all types)
func (s *TelemetryService) SubscribeToTelemetryUpdates(ctx context.Context, orgID int64, deviceIDs []string, eventTypes []models.UpdateType) (<-chan models.TelemetryUpdate, func(), error) {
	broadcaster, err := s.GetOrCreateBroadcaster(ctx, orgID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get broadcaster: %w", err)
	}

	updateChan, unsubscribe, err := broadcaster.Subscribe(ctx, SubscriptionConfig{
		DeviceIDs:        models.ToDeviceIdentifiers(deviceIDs),
		MeasurementTypes: nil,
		EventTypes:       eventTypes,
		BufferSize:       subscriberChannelBuffer,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to subscribe to broadcaster: %w", err)
	}

	return updateChan, unsubscribe, nil
}

// GetLatestDeviceMetrics retrieves the latest telemetry metrics for a batch of devices.
// This is used by the fleet management service to populate telemetry data in list responses.
func (s *TelemetryService) GetLatestDeviceMetrics(ctx context.Context, deviceIDs []models.DeviceIdentifier) (map[models.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	return s.telemetryDataStore.GetLatestDeviceMetricsBatch(ctx, deviceIDs)
}
