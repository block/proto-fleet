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
in InfluxDB, and sends the status result to statusResults. Workers are
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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	fleetmanagementModels "github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	maxStatusBatchSize = 500 // Flush early if batch reaches this size

	// Default status flush interval if not configured.
	defaultStatusFlushInterval = 1 * time.Second
)

// measurementTypeMapping maps protobuf measurement types to internal types.
var measurementTypeMapping = map[pb.MeasurementConfig_MeasurementType]models.MeasurementType{
	pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE: models.MeasurementTypePower,
	pb.MeasurementConfig_MEASUREMENT_TYPE_TEMPERATURE: models.MeasurementTypeTemperature,
	pb.MeasurementConfig_MEASUREMENT_TYPE_HASHRATE:    models.MeasurementTypeHashrate,
	pb.MeasurementConfig_MEASUREMENT_TYPE_EFFICIENCY:  models.MeasurementTypeEfficiency,
}

// parseTimeSeriesConfig converts a protobuf TimeSeriesConfig to a models.TimeRange
// This centralizes the logic for extracting time ranges from different config types:
// - LookbackPeriod: Calculates start/end based on current time minus duration
// - Interval: Uses explicit start/end times from config
// - Default: Falls back to last hour if no selection provided
func parseTimeSeriesConfig(config *commonpb.TimeSeriesConfig) models.TimeRange {
	var timeRange models.TimeRange

	switch ts := config.TimeSelection.(type) {
	case *commonpb.TimeSeriesConfig_LookbackPeriod:
		endTime := time.Now()
		startTime := endTime.Add(-ts.LookbackPeriod.AsDuration())
		timeRange.StartTime = &startTime
		timeRange.EndTime = &endTime

	case *commonpb.TimeSeriesConfig_Interval:
		if ts.Interval.StartTime != nil {
			startTime := ts.Interval.StartTime.AsTime()
			timeRange.StartTime = &startTime
		}
		if ts.Interval.EndTime != nil {
			endTime := ts.Interval.EndTime.AsTime()
			timeRange.EndTime = &endTime
		}

	default:
		// Default to last hour if no time selection
		endTime := time.Now()
		startTime := endTime.Add(-time.Hour)
		timeRange.StartTime = &startTime
		timeRange.EndTime = &endTime
	}

	return timeRange
}

const (
	defaultUpdateInterval = 1 * time.Minute

	// Page size for combined metrics query
	defaultCombinedMetricsPageSize = 100
)

//go:generate mockgen -source=service.go -destination=mocks/mock_service.go -package=mock UpdateScheduler,TelemetryDataStore,MinerGetter
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
	Ping(ctx context.Context) error
	Close() error
}
type MinerGetter interface {
	GetMinerFromDeviceIdentifier(ctx context.Context, deviceIdentifier models.DeviceIdentifier) (interfaces.Miner, error)
}

type deviceResult struct {
	device     models.Device
	metrics    modelsV2.DeviceMetrics
	metricsErr error
}

// statusResult represents a status update result from a worker.
type statusResult struct {
	deviceIdentifier models.DeviceIdentifier
	status           mm.MinerStatus
}

type TelemetryService struct {
	config             Config
	updateScheduler    UpdateScheduler
	telemetryDataStore TelemetryDataStore
	minerManager       MinerGetter
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
	statusResults    chan statusResult
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
}

func NewTelemetryService(config Config, telemetryDataStore TelemetryDataStore, minerManager MinerGetter, scheduler UpdateScheduler, deviceStore stores.DeviceStore, errorPoller ErrorPoller) *TelemetryService {
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
	}
	return s.updateScheduler.RemoveDevices(ctx, deviceIDs...)
}

func (s *TelemetryService) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	go s.gatherMetricsRoutine(ctx)
	go s.devicePollingRoutine(ctx)
	go s.statusPollingRoutine(ctx)
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

	// Start routine that collects status results and periodically writes to DB
	go s.statusWriterRoutine(ctx)

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

				select {
				case s.statusTasks <- models.Device{ID: deviceID}:
				case <-ctx.Done():
					return false
				}
				return true
			})
		}
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

		case device := <-s.tasks:
			s.processDevice(ctx, device)

		case device := <-s.statusTasks:
			s.processStatusOnly(ctx, device)
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
	// Telemetry failure doesn't block status/error polling - we still want to track online state
	if err := s.GetTelemetryFromDevice(ctx, device); err != nil {
		slog.Warn("failed to get telemetry from device", "deviceID", device.ID, "error", err)

		if fleeterror.IsAuthenticationError(err) {
			if updateErr := s.handleAuthenticationFailure(ctx, device.ID); updateErr != nil {
				slog.Error("failed to update pairing status to AUTHENTICATION_NEEDED",
					"deviceID", device.ID, "error", updateErr)
			}
		}

		if addErr := s.updateScheduler.AddFailedDevices(ctx, device); addErr != nil {
			slog.Warn("failed to add failed device to scheduler", "deviceID", device.ID, "error", addErr)
		}
	}

	// Fetch status from miner (connection errors return MinerStatusOffline, nil).
	// Non-connection errors (auth failures, etc.) cause early return since we can't
	// reliably poll errors from a device we can't authenticate with.
	status, statusErr := s.fetchStatusFromMiner(ctx, device.ID)
	if statusErr != nil {
		slog.Warn("failed to get status for device", "deviceID", device.ID, "error", statusErr)

		if fleeterror.IsAuthenticationError(statusErr) {
			if updateErr := s.handleAuthenticationFailure(ctx, device.ID); updateErr != nil {
				slog.Error("failed to update pairing status to AUTHENTICATION_NEEDED",
					"deviceID", device.ID, "error", updateErr)
			}
		}
		return
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

		if fleeterror.IsAuthenticationError(statusErr) {
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

		// Convert map to slice for batch DB write
		statusUpdates := make([]stores.DeviceStatusUpdate, 0, len(pendingUpdates))
		for deviceID, status := range pendingUpdates {
			statusUpdates = append(statusUpdates, stores.DeviceStatusUpdate{
				DeviceIdentifier: deviceID,
				Status:           status,
			})
		}

		// Write new statuses to DB in a single bulk INSERT.
		// Each row is ~100 bytes. With maxStatusBatchSize=500, batches are ~50KB,
		// well under MySQL's default 64MB max_allowed_packet.
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
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
// when authentication with a device fails
func (s *TelemetryService) handleAuthenticationFailure(ctx context.Context, deviceID models.DeviceIdentifier) error {
	// Update pairing status to AUTHENTICATION_NEEDED using device identifier directly
	err := s.deviceStore.UpdateDevicePairingStatusByIdentifier(ctx, string(deviceID), pairing.StatusAuthenticationNeeded)
	if err != nil {
		return fmt.Errorf("failed to update pairing status for device %s: %w", deviceID, err)
	}

	return nil
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

func (s *TelemetryService) fetchTelemetryFromMiner(ctx context.Context, device models.Device) (*deviceResult, error) {
	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, device.ID)
	if err != nil {
		return nil, err
	}

	result := &deviceResult{device: device}
	result.metrics, result.metricsErr = miner.GetDeviceMetrics(ctx)
	return result, nil
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
		return mm.MinerStatusUnknown, err
	}
	status, err := miner.GetDeviceStatus(ctx)
	if err != nil {
		if fleeterror.IsConnectionError(err) {
			return mm.MinerStatusOffline, nil
		}
		return mm.MinerStatusUnknown, err
	}
	return status, nil
}

// GetTelemetryFromDevice fetches telemetry data from a device and stores it.
func (s *TelemetryService) GetTelemetryFromDevice(ctx context.Context, device models.Device) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.MetricTimeout)
	defer cancel()

	result, err := s.fetchTelemetryFromMiner(ctx, device)
	if err != nil {
		return fmt.Errorf("failed to get miner from device ID %s: %w", device.ID, err)
	}

	if result.metricsErr == nil {
		if err := s.telemetryDataStore.StoreDeviceMetrics(ctx, result.metrics); err != nil {
			slog.Error("Failed to store device metrics",
				"device_id", device.ID,
				"error", err)
		}
	}

	if err := s.updateScheduler.AddDevices(ctx, models.Device{
		ID:            device.ID,
		LastUpdatedAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("failed to update device last updated time for device %s: %w", device.ID, err)
	}
	return nil
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
						Type:         models.UpdateTypeDeviceStatus,
						DeviceID:     deviceID,
						Timestamp:    time.Now(),
						DeviceStatus: &status,
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

	select {
	case updateChan <- combinedMetrics:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	}
}

// GetMinerTelemetry returns the latest telemetry data for a miner
func (s *TelemetryService) GetMinerTelemetry(ctx context.Context, deviceID string, dataMode pb.DataMode, timeSeriesConfig *commonpb.TimeSeriesConfig, measurementConfigs []*pb.MeasurementConfig) (*fleetmanagementModels.MinerTelemetry, error) {
	// Create a map of measurement type to its config for easy lookup
	configMap := make(map[pb.MeasurementConfig_MeasurementType]*pb.MeasurementConfig)
	for _, config := range measurementConfigs {
		configMap[config.MeasurementType] = config
	}

	// Helper function to get measurements based on type and config
	getMeasurements := func(mType pb.MeasurementConfig_MeasurementType) ([]*commonpb.Measurement, error) {
		// Convert protobuf measurement type to internal model
		internalMeasurementType := pbMeasurementTypeToInternal(mType)
		if internalMeasurementType == models.MeasurementTypeUnknown {
			return []*commonpb.Measurement{}, nil
		}

		// Check if there's a specific config for this measurement type
		if config, ok := configMap[mType]; ok {
			// Use measurement-specific config
			if config.DataMode == pb.DataMode_DATA_MODE_METADATA {
				return []*commonpb.Measurement{}, nil
			}
			if config.DataMode == pb.DataMode_DATA_MODE_TIME_SERIES && config.TimeSeriesConfig != nil {
				return s.getTimeSeriesMeasurements(ctx, deviceID, internalMeasurementType, config.TimeSeriesConfig)
			}
			// For SNAPSHOT or unspecified, return latest measurement
			return s.getLatestMeasurements(ctx, deviceID, internalMeasurementType)
		}

		// No specific config, use global settings
		if dataMode == pb.DataMode_DATA_MODE_METADATA {
			return []*commonpb.Measurement{}, nil
		}
		if dataMode == pb.DataMode_DATA_MODE_TIME_SERIES && timeSeriesConfig != nil {
			return s.getTimeSeriesMeasurements(ctx, deviceID, internalMeasurementType, timeSeriesConfig)
		}
		// Default to SNAPSHOT mode
		return s.getLatestMeasurements(ctx, deviceID, internalMeasurementType)
	}

	// Get measurements for each type
	powerUsage, err := getMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE)
	if err != nil {
		return nil, fmt.Errorf("failed to get power usage measurements: %w", err)
	}

	temperature, err := getMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_TEMPERATURE)
	if err != nil {
		return nil, fmt.Errorf("failed to get temperature measurements: %w", err)
	}

	hashrate, err := getMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_HASHRATE)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashrate measurements: %w", err)
	}

	efficiency, err := getMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_EFFICIENCY)
	if err != nil {
		return nil, fmt.Errorf("failed to get efficiency measurements: %w", err)
	}

	return &fleetmanagementModels.MinerTelemetry{
		PowerUsage:  powerUsage,
		Temperature: temperature,
		Hashrate:    hashrate,
		Efficiency:  efficiency,
		Timestamp:   timestamppb.Now(),
	}, nil
}

// GetBatchMinerTelemetry returns telemetry data for multiple miners in a single batch query.
// This is optimized to reduce N+1 query patterns by fetching telemetry for all requested devices
// in a single database query per measurement type, instead of per-device queries.
func (s *TelemetryService) GetBatchMinerTelemetry(ctx context.Context, deviceIDs []string, dataMode pb.DataMode, timeSeriesConfig *commonpb.TimeSeriesConfig, measurementConfigs []*pb.MeasurementConfig) (map[string]*fleetmanagementModels.MinerTelemetry, error) {
	if len(deviceIDs) == 0 {
		return make(map[string]*fleetmanagementModels.MinerTelemetry), nil
	}

	// Create a map of measurement type to its config for easy lookup
	configMap := make(map[pb.MeasurementConfig_MeasurementType]*pb.MeasurementConfig)
	for _, config := range measurementConfigs {
		configMap[config.MeasurementType] = config
	}

	// Helper function to get effective data mode and time series config for a measurement type
	getEffectiveConfig := func(mType pb.MeasurementConfig_MeasurementType) (pb.DataMode, *commonpb.TimeSeriesConfig) {
		if config, ok := configMap[mType]; ok {
			return config.DataMode, config.TimeSeriesConfig
		}
		return dataMode, timeSeriesConfig
	}

	// Initialize result map
	result := make(map[string]*fleetmanagementModels.MinerTelemetry, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		result[deviceID] = &fleetmanagementModels.MinerTelemetry{
			PowerUsage:  []*commonpb.Measurement{},
			Temperature: []*commonpb.Measurement{},
			Hashrate:    []*commonpb.Measurement{},
			Efficiency:  []*commonpb.Measurement{},
			Timestamp:   timestamppb.Now(),
		}
	}

	// Step 1: Populate all SNAPSHOT/latest values (1 query for all measurement types)
	s.populateLatestSnapshot(ctx, deviceIDs, result, getEffectiveConfig)

	// Step 2: For any measurements in TIME_SERIES mode, fetch and overwrite with time series data
	s.populateTimeSeriesData(ctx, deviceIDs, result, getEffectiveConfig)

	return result, nil
}

// populateLatestSnapshot populates all SNAPSHOT/latest values for each device.
func (s *TelemetryService) populateLatestSnapshot(
	ctx context.Context,
	deviceIDs []string,
	result map[string]*fleetmanagementModels.MinerTelemetry,
	getEffectiveConfig func(pb.MeasurementConfig_MeasurementType) (pb.DataMode, *commonpb.TimeSeriesConfig),
) {
	deviceMetricsMap, err := s.telemetryDataStore.GetLatestDeviceMetricsBatch(ctx, models.ToDeviceIdentifiers(deviceIDs))
	if err != nil {
		slog.Warn("failed to get batch device metrics", slog.Any("error", err))
		return
	}

	for deviceID, metrics := range deviceMetricsMap {
		telemetry, ok := result[string(deviceID)]
		if !ok {
			continue
		}
		telemetry.Timestamp = timestamppb.New(metrics.Timestamp)

		for pbType, internalType := range measurementTypeMapping {
			if dataMode, _ := getEffectiveConfig(pbType); dataMode == pb.DataMode_DATA_MODE_METADATA {
				continue
			}
			if value, timestamp, ok := metrics.ExtractRawMeasurement(internalType); ok {
				telemetry.SetMeasurements(pbType, []*commonpb.Measurement{{
					Value:     value,
					Timestamp: timestamppb.New(timestamp),
				}})
			}
		}
	}
}

// populateTimeSeriesData fetches TIME_SERIES data for measurements that need it, overwriting SNAPSHOT values.
func (s *TelemetryService) populateTimeSeriesData(
	ctx context.Context,
	deviceIDs []string,
	result map[string]*fleetmanagementModels.MinerTelemetry,
	getEffectiveConfig func(pb.MeasurementConfig_MeasurementType) (pb.DataMode, *commonpb.TimeSeriesConfig),
) {
	for pbType, internalType := range measurementTypeMapping {
		dataMode, tsConfig := getEffectiveConfig(pbType)
		if dataMode != pb.DataMode_DATA_MODE_TIME_SERIES || tsConfig == nil {
			continue
		}

		measurementsByDevice, err := s.getBatchTimeSeriesMeasurements(ctx, deviceIDs, internalType, tsConfig)
		if err != nil {
			slog.Warn("failed to get batch time series telemetry",
				slog.String("measurement_type", internalType.String()),
				slog.Any("error", err))
			continue
		}

		for deviceID, measurements := range measurementsByDevice {
			if telemetry, ok := result[deviceID]; ok {
				telemetry.SetMeasurements(pbType, measurements)
			}
		}
	}
}

// getBatchTimeSeriesMeasurements retrieves time series measurements for multiple devices in a single query.
func (s *TelemetryService) getBatchTimeSeriesMeasurements(ctx context.Context, deviceIDs []string, measurementType models.MeasurementType, timeSeriesConfig *commonpb.TimeSeriesConfig) (map[string][]*commonpb.Measurement, error) {
	query := models.TimeSeriesTelemetryQuery{
		DeviceIDs:        models.ToDeviceIdentifiers(deviceIDs),
		MeasurementTypes: []models.MeasurementType{measurementType},
		TimeRange:        parseTimeSeriesConfig(timeSeriesConfig),
	}
	deviceMetrics, err := s.telemetryDataStore.GetTimeSeriesTelemetry(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch time series telemetry: %w", err)
	}

	// Group results by device ID
	result := make(map[string][]*commonpb.Measurement)
	for _, dm := range deviceMetrics {
		value, timestamp, ok := dm.ExtractRawMeasurement(measurementType)
		if !ok {
			continue
		}
		result[dm.DeviceID] = append(result[dm.DeviceID], &commonpb.Measurement{
			Value:     value,
			Timestamp: timestamppb.New(timestamp),
		})
	}

	return result, nil
}

func (s *TelemetryService) StreamMeasurements(ctx context.Context, deviceIDs []string, measurementTypes []pb.MeasurementConfig_MeasurementType) (<-chan *pb.StreamMinerUpdatesResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get session info: %w", err)
	}

	responseChan := make(chan *pb.StreamMinerUpdatesResponse, streamResponseChannelBuffer)

	// Convert measurement types to internal models
	internalMeasurementTypes := make([]models.MeasurementType, 0, len(measurementTypes))
	for _, mType := range measurementTypes {
		internalType := pbMeasurementTypeToInternal(mType)
		if internalType != models.MeasurementTypeUnknown {
			internalMeasurementTypes = append(internalMeasurementTypes, internalType)
		}
	}

	// Get or create broadcaster for this organization
	broadcaster, err := s.GetOrCreateBroadcaster(ctx, info.OrganizationID)
	if err != nil {
		close(responseChan)
		return nil, fmt.Errorf("failed to get broadcaster: %w", err)
	}

	updateChan, unsubscribe, err := broadcaster.Subscribe(ctx, SubscriptionConfig{
		DeviceIDs:        models.ToDeviceIdentifiers(deviceIDs),
		MeasurementTypes: internalMeasurementTypes,
		BufferSize:       subscriberChannelBuffer,
	})
	if err != nil {
		close(responseChan)
		return nil, fmt.Errorf("failed to subscribe to broadcaster: %w", err)
	}

	go func() {
		defer close(responseChan)
		defer unsubscribe()

		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-updateChan:
				if !ok {
					return
				}

				// Convert internal update to protobuf response
				response := s.convertTelemetryUpdateToResponse(update, measurementTypes)
				if response != nil {
					select {
					case responseChan <- response:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return responseChan, nil
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

// Helper functions for type conversions

// pbMeasurementTypeToInternal converts protobuf measurement type to internal model
func pbMeasurementTypeToInternal(pbType pb.MeasurementConfig_MeasurementType) models.MeasurementType {
	switch pbType {
	case pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE:
		return models.MeasurementTypePower
	case pb.MeasurementConfig_MEASUREMENT_TYPE_TEMPERATURE:
		return models.MeasurementTypeTemperature
	case pb.MeasurementConfig_MEASUREMENT_TYPE_HASHRATE:
		return models.MeasurementTypeHashrate
	case pb.MeasurementConfig_MEASUREMENT_TYPE_EFFICIENCY:
		return models.MeasurementTypeEfficiency
	case pb.MeasurementConfig_MEASUREMENT_TYPE_UNSPECIFIED:
		fallthrough
	default:
		return models.MeasurementTypeUnknown
	}
}

// internalMeasurementTypeToPb converts internal measurement type to protobuf
func internalMeasurementTypeToPb(internalType models.MeasurementType) pb.MeasurementConfig_MeasurementType {
	//nolint:exhaustive // there are only a few types to match at this time
	switch internalType {
	case models.MeasurementTypePower:
		return pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE
	case models.MeasurementTypeTemperature:
		return pb.MeasurementConfig_MEASUREMENT_TYPE_TEMPERATURE
	case models.MeasurementTypeHashrate:
		return pb.MeasurementConfig_MEASUREMENT_TYPE_HASHRATE
	case models.MeasurementTypeEfficiency:
		return pb.MeasurementConfig_MEASUREMENT_TYPE_EFFICIENCY
	case models.MeasurementTypeUnknown:
		fallthrough
	default:
		return pb.MeasurementConfig_MEASUREMENT_TYPE_UNSPECIFIED
	}
}

// getLatestMeasurements retrieves the latest measurements for a device and measurement type
func (s *TelemetryService) getLatestMeasurements(ctx context.Context, deviceID string, measurementType models.MeasurementType) ([]*commonpb.Measurement, error) {
	metricsMap, err := s.telemetryDataStore.GetLatestDeviceMetricsBatch(ctx, []models.DeviceIdentifier{models.DeviceIdentifier(deviceID)})
	if err != nil {
		return nil, fmt.Errorf("failed to get latest telemetry: %w", err)
	}

	var measurements []*commonpb.Measurement
	if metrics, ok := metricsMap[models.DeviceIdentifier(deviceID)]; ok {
		value, timestamp, found := metrics.ExtractRawMeasurement(measurementType)
		if found {
			measurements = append(measurements, &commonpb.Measurement{
				Value:     value,
				Timestamp: timestamppb.New(timestamp),
			})
		}
	}

	return measurements, nil
}

// getTimeSeriesMeasurements retrieves time series measurements for a device and measurement type
func (s *TelemetryService) getTimeSeriesMeasurements(ctx context.Context, deviceID string, measurementType models.MeasurementType, timeSeriesConfig *commonpb.TimeSeriesConfig) ([]*commonpb.Measurement, error) {
	query := models.TimeSeriesTelemetryQuery{
		DeviceIDs:        []models.DeviceIdentifier{models.DeviceIdentifier(deviceID)},
		MeasurementTypes: []models.MeasurementType{measurementType},
		TimeRange:        parseTimeSeriesConfig(timeSeriesConfig),
	}

	deviceMetrics, err := s.telemetryDataStore.GetTimeSeriesTelemetry(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get time series telemetry: %w", err)
	}

	var measurements []*commonpb.Measurement
	for _, dm := range deviceMetrics {
		value, timestamp, ok := dm.ExtractRawMeasurement(measurementType)
		if !ok {
			continue
		}
		measurements = append(measurements, &commonpb.Measurement{
			Value:     value,
			Timestamp: timestamppb.New(timestamp),
		})
	}

	return measurements, nil
}

// convertTelemetryUpdateToResponse converts internal telemetry update to protobuf response
func (s *TelemetryService) convertTelemetryUpdateToResponse(update models.TelemetryUpdate, measurementTypes []pb.MeasurementConfig_MeasurementType) *pb.StreamMinerUpdatesResponse {
	//nolint:exhaustive // we handle all a few measurements for now
	switch update.Type {
	case models.UpdateTypeTelemetry:
		if update.MeasurementName == "" {
			return nil
		}

		measurementName := update.MeasurementName
		var internalMeasurementType models.MeasurementType

		internalMeasurementType = models.MeasurementNameToType(measurementName)
		if internalMeasurementType == models.MeasurementTypeUnknown {
			return nil
		}

		pbMeasurementType := internalMeasurementTypeToPb(internalMeasurementType)

		typeRequested := slices.Contains(measurementTypes, pbMeasurementType)
		if !typeRequested {
			return nil
		}

		// Note: StreamTelemetryUpdates returns already-converted values (via convertDeviceMetricsToTelemetry)
		value := update.MeasurementValue

		return &pb.StreamMinerUpdatesResponse{
			Timestamp:        timestamppb.New(update.Timestamp),
			DeviceIdentifier: string(update.DeviceID),
			Update: &pb.StreamMinerUpdatesResponse_Measurement{
				Measurement: &pb.MeasurementUpdate{
					MeasurementType: pbMeasurementType,
					Measurement: &commonpb.Measurement{
						Value:     value,
						Timestamp: timestamppb.New(update.Timestamp),
					},
				},
			},
		}

	case models.UpdateTypeHeartbeat:
		return &pb.StreamMinerUpdatesResponse{
			Timestamp:        timestamppb.New(update.Timestamp),
			DeviceIdentifier: string(update.DeviceID),
			Update: &pb.StreamMinerUpdatesResponse_Heartbeat{
				Heartbeat: &pb.Heartbeat{},
			},
		}

	case models.UpdateTypeError:
		if update.Error != nil {
			slog.Warn("telemetry stream error", "error", *update.Error, "deviceID", update.DeviceID)
		}
		return nil

	default:
		return nil
	}
}

func (s *TelemetryService) StreamMinerStateCounts(ctx context.Context, orgID int64, updateInterval time.Duration) (<-chan models.TelemetryUpdate, error) {
	ch := make(chan models.TelemetryUpdate, statusUpdateChannelBuffer)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(defaultStatusUpdateInterval)
		if updateInterval > 0 {
			ticker = time.NewTicker(updateInterval)
		}
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				counts, err := s.deviceStore.GetMinerStateCounts(ctx, orgID, nil)
				if err != nil {
					slog.Error("failed to get miner state counts", "error", err)
					continue
				}
				resp := models.TelemetryUpdate{
					Type:      models.UpdateTypeMinerStateCounts,
					Timestamp: time.Now(),
					MinerStateCounts: &models.MinerStateCounts{
						Hashing:  counts.HashingCount,
						Broken:   counts.BrokenCount,
						Offline:  counts.OfflineCount,
						Sleeping: counts.SleepingCount,
					},
				}
				select {
				case <-ctx.Done():
					return
				case ch <- resp:
				}
			}
		}
	}()

	return ch, nil
}
