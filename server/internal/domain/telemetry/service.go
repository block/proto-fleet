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
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

const (
	// Default intervals
	defaultStatusUpdateInterval    = 1 * time.Second
	defaultFetchInterval           = 10 * time.Second
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

	// Context timeouts
	shutdownFlushTimeout = 5 * time.Second
)

const (
	defaultUpdateInterval = 1 * time.Minute

	// Page size for combined metrics query
	defaultCombinedMetricsPageSize = 100
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
	status    mm.MinerStatus
	hasStatus bool
}

// statusResult represents a status update result from a worker.
type statusResult struct {
	deviceIdentifier models.DeviceIdentifier
	status           mm.MinerStatus
}

// metricsResult holds device metrics queued by a worker for batch DB writes.
type metricsResult struct {
	metrics modelsV2.DeviceMetrics
}

type TelemetryService struct {
	config             Config
	updateScheduler    UpdateScheduler
	telemetryDataStore TelemetryDataStore
	minerManager       CachedMinerGetter
	deviceStore        stores.DeviceStore
	errorPoller        ErrorPoller
	mux                sync.Mutex
	// tasks queues devices for full telemetry collection (metrics, telemetry, and status).
	// Buffer sized to ConcurrencyLimit to ensure at least one queued task per worker.
	tasks chan models.Device
	// statusTasks queues devices for status-only checks (no telemetry fetch).
	// Used by statusPollingRoutine to check failed devices for recovery.
	statusTasks chan models.Device
	// statusResults receives status updates from workers for batch DB writes.
	statusResults chan statusResult
	// metricsResults receives device metrics from workers for batch DB writes.
	// Uses a blocking send so metrics are never dropped; backpressure slows workers
	// if the DB falls behind rather than losing data.
	metricsResults   chan metricsResult
	cancelFunc       context.CancelFunc
	lookBackDuration time.Duration
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
	// inFlight tracks devices currently being processed by a worker via the tasks channel.
	// statusPollingRoutine skips devices in this map to avoid double-processing the same
	// device simultaneously in both the full-telemetry and status-only paths.
	inFlight sync.Map // map[DeviceIdentifier]struct{}
}

func NewTelemetryService(config Config, telemetryDataStore TelemetryDataStore, minerManager CachedMinerGetter, scheduler UpdateScheduler, deviceStore stores.DeviceStore, errorPoller ErrorPoller) *TelemetryService {
	return &TelemetryService{
		config:             config,
		telemetryDataStore: telemetryDataStore,
		minerManager:       minerManager,
		updateScheduler:    scheduler,
		deviceStore:        deviceStore,
		errorPoller:        errorPoller,
		tasks:              make(chan models.Device, config.ConcurrencyLimit),
		statusTasks:        make(chan models.Device, config.ConcurrencyLimit),
		statusResults:      make(chan statusResult, resultsChannelBuffer),
		metricsResults:     make(chan metricsResult, resultsChannelBuffer),
		lookBackDuration:   -1 * (config.StalenessThreshold - config.FetchInterval),
	}
}

func (s *TelemetryService) AddDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error {
	if len(deviceID) == 0 {
		return nil
	}
	for _, id := range deviceID {
		s.tasks <- models.Device{ID: id, LastUpdatedAt: time.Now().Add(-s.config.NewDeviceLookback)}
		s.devicesForStatusPolling.Store(id, struct{}{})
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
	}
	return s.updateScheduler.RemoveDevices(ctx, deviceIDs...)
}

func (s *TelemetryService) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	go s.gatherMetricsRoutine(ctx)
	go s.devicePollingRoutine(ctx)
	go s.statusPollingRoutine(ctx)
	go s.fleetStateSnapshotRoutine(ctx)
	return nil
}

func (s *TelemetryService) Stop(ctx context.Context) error {
	s.cancelFunc()
	defer close(s.tasks)
	defer close(s.statusTasks)
	defer close(s.statusResults)

	s.broadcasters.Range(func(_, value any) bool {
		if broadcaster, ok := value.(*TelemetryBroadcaster); ok {
			broadcaster.Stop()
		}
		return true
	})

	return nil
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
	if !s.mux.TryLock() {
		return
	}
	defer s.mux.Unlock()

	// Start workers that fetch telemetry/status from miners
	for range s.config.ConcurrencyLimit {
		go s.worker(ctx)
	}

	// Start routines that collect results from workers and periodically write to DB
	go s.statusWriterRoutine(ctx)
	go s.metricsWriterRoutine(ctx)

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
				s.tasks <- device
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
				if _, alreadyClaimed := s.inFlight.LoadOrStore(deviceID, struct{}{}); alreadyClaimed {
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

// worker processes devices from task channels one at a time.
// It fetches telemetry/status from miners and sends results to the statusResults channel
// for periodic DB writes by statusWriterRoutine.
func (s *TelemetryService) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case device, ok := <-s.tasks:
			if !ok {
				return
			}
			s.inFlight.Store(device.ID, struct{}{})
			s.processDevice(ctx, device)
			s.inFlight.Delete(device.ID)

		case device, ok := <-s.statusTasks:
			if !ok {
				return
			}
			s.processStatusOnly(ctx, device)
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
func (s *TelemetryService) processDevice(ctx context.Context, device models.Device) {
	// Telemetry failure doesn't block status/error polling - we still want to track online state.
	// When metrics succeed, status is derived from the health field — no second RPC needed.
	metricsStatus, hasMetricsStatus, telemetryErr := s.GetTelemetryFromDevice(ctx, device)
	if telemetryErr != nil {
		slog.Warn("failed to get telemetry from device", "deviceID", device.ID, "error", telemetryErr)

		if requiresCredentialRemediation(telemetryErr) {
			if updateErr := s.handleAuthenticationFailure(ctx, device.ID); updateErr != nil {
				slog.Error("failed to update pairing status to AUTHENTICATION_NEEDED",
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
		var statusErr error
		status, statusErr = s.fetchStatusFromMiner(ctx, device.ID)
		if statusErr != nil {
			slog.Warn("failed to get status for device", "deviceID", device.ID, "error", statusErr)

			if requiresCredentialRemediation(statusErr) {
				if updateErr := s.handleAuthenticationFailure(ctx, device.ID); updateErr != nil {
					slog.Error("failed to update pairing status to AUTHENTICATION_NEEDED",
						"deviceID", device.ID, "error", updateErr)
				}
			}
			return
		}
	}

	// Send status result to writer (non-blocking to prevent worker stalls)
	select {
	case s.statusResults <- statusResult{deviceIdentifier: device.ID, status: status}:
	case <-ctx.Done():
		return
	default:
		slog.Error("status results channel full, dropping update", "deviceID", device.ID)
	}

	s.pollErrorsForDevice(ctx, device)
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
	status, statusErr := s.fetchStatusFromMiner(ctx, device.ID)
	if statusErr != nil {
		// Non-connection errors (e.g., auth failures) - device stays in failed state.
		// Connection errors don't reach here; they return (MinerStatusOffline, nil).
		slog.Debug("status polling failed for device", "deviceID", device.ID, "error", statusErr)

		if requiresCredentialRemediation(statusErr) {
			if updateErr := s.handleAuthenticationFailure(ctx, device.ID); updateErr != nil {
				slog.Error("failed to update pairing status to AUTHENTICATION_NEEDED",
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
	case s.statusResults <- statusResult{deviceIdentifier: device.ID, status: status}:
	case <-ctx.Done():
		return
	default:
		slog.Error("status results channel full, dropping update", "deviceID", device.ID)
	}
}

// statusWriterRoutine collects status results from workers and writes them to DB periodically.
// This centralizes DB writes to reduce connection usage and improve throughput.
func (s *TelemetryService) statusWriterRoutine(ctx context.Context) {
	flushInterval := s.config.StatusFlushInterval
	if flushInterval <= 0 {
		flushInterval = defaultStatusFlushInterval
	}

	pendingUpdates := make(map[models.DeviceIdentifier]mm.MinerStatus)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func(flushCtx context.Context) {
		if len(pendingUpdates) == 0 {
			return
		}

		// Check current DB statuses to avoid overwriting firmware update states
		// (UPDATING, REBOOT_REQUIRED) that are managed by the command execution service.
		// REBOOT_REQUIRED persists until the user triggers a reboot command from Fleet.
		deviceIDs := make([]models.DeviceIdentifier, 0, len(pendingUpdates))
		for deviceID := range pendingUpdates {
			deviceIDs = append(deviceIDs, deviceID)
		}
		currentStatuses, err := s.deviceStore.GetDeviceStatusForDeviceIdentifiers(flushCtx, deviceIDs)
		if err != nil {
			slog.Warn("failed to check current device statuses for firmware update guard, skipping flush", "error", err)
			return
		}

		statusUpdates := make([]stores.DeviceStatusUpdate, 0, len(pendingUpdates))
		for deviceID, status := range pendingUpdates {
			if currentStatuses != nil {
				if currentStatus, ok := currentStatuses[deviceID]; ok {
					if currentStatus == mm.MinerStatusUpdating || currentStatus == mm.MinerStatusRebootRequired {
						delete(pendingUpdates, deviceID)
						continue
					}
				}
			}
			statusUpdates = append(statusUpdates, stores.DeviceStatusUpdate{
				DeviceIdentifier: deviceID,
				Status:           status,
			})
		}

		if len(statusUpdates) == 0 {
			clear(pendingUpdates)
			return
		}

		// Write new statuses to DB in a single bulk INSERT.
		// Each row is ~100 bytes. With maxStatusBatchSize=500, batches are ~50KB.
		if err := s.deviceStore.UpsertDeviceStatuses(flushCtx, statusUpdates); err != nil {
			slog.Error("status upsert failed", "count", len(statusUpdates), "error", err)
			clear(pendingUpdates)
			return
		}

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

		clear(pendingUpdates)
	}

	for {
		select {
		case <-ctx.Done():
			// Use a fresh context with timeout for final flush to ensure pending
			// updates are written even after the parent context is cancelled.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
			flush(shutdownCtx)
			cancel()
			return

		case result := <-s.statusResults:
			pendingUpdates[result.deviceIdentifier] = result.status
			if len(pendingUpdates) >= maxStatusBatchSize {
				flush(ctx)
			}

		case <-ticker.C:
			flush(ctx)
		}
	}
}

// handleAuthenticationFailure updates the pairing status to AUTHENTICATION_NEEDED
// when the device requires credential remediation (for example auth failure or
// default-password rotation before normal operations).
func (s *TelemetryService) handleAuthenticationFailure(ctx context.Context, deviceID models.DeviceIdentifier) error {
	// Update pairing status to AUTHENTICATION_NEEDED using device identifier directly
	err := s.deviceStore.UpdateDevicePairingStatusByIdentifier(ctx, string(deviceID), pairing.StatusAuthenticationNeeded)
	if err != nil {
		return fmt.Errorf("failed to update pairing status for device %s: %w", deviceID, err)
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

func (s *TelemetryService) fetchTelemetryFromMiner(ctx context.Context, device models.Device) (*deviceResult, error) {
	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, device.ID)
	if err != nil {
		return nil, err
	}

	result := &deviceResult{device: device}
	result.metrics, result.metricsErr = miner.GetDeviceMetrics(ctx)
	if result.metricsErr == nil {
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
// Connection errors are treated as a valid "offline" state and return (MinerStatusOffline, nil).
// Only non-connection errors (e.g., authentication failures) return an error.
func (s *TelemetryService) fetchStatusFromMiner(ctx context.Context, deviceID models.DeviceIdentifier) (mm.MinerStatus, error) {
	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, deviceID)
	if err != nil {
		if fleeterror.IsConnectionError(err) {
			return mm.MinerStatusOffline, nil
		}
		if fleeterror.IsAuthenticationError(err) {
			s.minerManager.InvalidateMiner(deviceID)
		}
		return mm.MinerStatusUnknown, err
	}
	status, err := miner.GetDeviceStatus(ctx)
	if err != nil {
		if fleeterror.IsConnectionError(err) {
			return mm.MinerStatusOffline, nil
		}
		if fleeterror.IsAuthenticationError(err) {
			s.minerManager.InvalidateMiner(deviceID)
		}
		return mm.MinerStatusUnknown, err
	}
	return status, nil
}

// GetTelemetryFromDevice fetches telemetry data from a device and stores it.
// Returns the derived MinerStatus and whether it is unambiguous.
// The bool is false when the health status is ambiguous; see healthStatusToMinerStatus.
func (s *TelemetryService) GetTelemetryFromDevice(ctx context.Context, device models.Device) (mm.MinerStatus, bool, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, s.config.MetricTimeout)
	defer cancel()

	result, err := s.fetchTelemetryFromMiner(fetchCtx, device)
	if err != nil {
		return mm.MinerStatusUnknown, false, fmt.Errorf("failed to get miner from device ID %s: %w", device.ID, err)
	}

	if result.metricsErr == nil {
		// Use the caller's ctx (not fetchCtx) so that MetricTimeout expiry does not
		// prevent enqueueing metrics we already fetched. Only give up if the service
		// itself is shutting down (ctx cancelled by the root context).
		select {
		case s.metricsResults <- metricsResult{metrics: result.metrics}:
		case <-ctx.Done():
			return mm.MinerStatusUnknown, false, fmt.Errorf("context cancelled enqueueing metrics for device %s: %w", device.ID, ctx.Err())
		}

		s.persistFirmwareVersionIfChanged(ctx, device.ID, result.metrics.FirmwareVersion)
	}

	if err := s.updateScheduler.AddDevices(ctx, models.Device{
		ID:            device.ID,
		LastUpdatedAt: time.Now(),
	}); err != nil {
		return mm.MinerStatusUnknown, false, fmt.Errorf("failed to update device last updated time for device %s: %w", device.ID, err)
	}
	return result.status, result.hasStatus, nil
}
func (s *TelemetryService) metricsWriterRoutine(ctx context.Context) {
	flushInterval := s.config.StatusFlushInterval
	if flushInterval <= 0 {
		flushInterval = defaultMetricsFlushInterval
	}

	pending := make([]modelsV2.DeviceMetrics, 0, maxMetricsBatchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func(flushCtx context.Context) {
		if len(pending) == 0 {
			return
		}
		if err := s.telemetryDataStore.StoreDeviceMetrics(flushCtx, pending...); err != nil {
			// The store wraps the batch in a single transaction, so one bad row fails
			// the whole batch. Retry individually so only the offending sample is dropped.
			slog.Warn("batch metrics write failed, retrying individually", "count", len(pending), "error", err)
			for _, m := range pending {
				if err := s.telemetryDataStore.StoreDeviceMetrics(flushCtx, m); err != nil {
					slog.Error("failed to store device metrics", "device_id", m.DeviceIdentifier, "error", err)
				}
			}
		}
		pending = pending[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Drain already-queued metrics into pending before the final flush.
			for {
				select {
				case result := <-s.metricsResults:
					pending = append(pending, result.metrics)
				default:
					goto done
				}
			}
		done:
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
			flush(shutdownCtx)
			cancel()
			return
		case result := <-s.metricsResults:
			pending = append(pending, result.metrics)
			if len(pending) >= maxMetricsBatchSize {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
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

func (s *TelemetryService) GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	// Returns raw values (H/s, W, J/H) - conversion to display units happens in the handler layer
	return s.telemetryDataStore.GetCombinedMetrics(ctx, query)
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

	combinedMetrics, err := s.GetCombinedMetrics(ctx, combinedQuery)
	if err != nil {
		if strings.Contains(err.Error(), "no combined metrics found") {
			combinedMetrics = models.CombinedMetric{
				Metrics: []models.Metric{},
			}
		} else {
			return fmt.Errorf("failed to get combined metrics: %w", err)
		}
	}

	// Fetch miner state counts scoped to the requested devices (or all if no filter)
	var minerFilter *stores.MinerFilter
	if len(query.DeviceIDs) > 0 {
		deviceIdentifiers := make([]string, len(query.DeviceIDs))
		for i, id := range query.DeviceIDs {
			deviceIdentifiers[i] = string(id)
		}
		minerFilter = &stores.MinerFilter{
			DeviceIdentifiers: deviceIdentifiers,
		}
	}
	counts, err := s.deviceStore.GetMinerStateCounts(ctx, query.OrganizationID, minerFilter)
	if err != nil {
		return fmt.Errorf("failed to get miner state counts: %w", err)
	}
	combinedMetrics.MinerStateCounts = &models.MinerStateCounts{
		Hashing:  counts.HashingCount,
		Broken:   counts.BrokenCount,
		Offline:  counts.OfflineCount,
		Sleeping: counts.SleepingCount,
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
