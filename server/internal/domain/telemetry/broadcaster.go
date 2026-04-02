package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// Subscription represents a subscriber to telemetry updates
type Subscription struct {
	id               string
	deviceIDs        map[models.DeviceIdentifier]bool // nil means subscribe to all devices
	measurementTypes map[models.MeasurementType]bool  // nil means subscribe to all measurement types
	eventTypes       map[models.UpdateType]bool       // nil means subscribe to all event types
	updateChan       chan models.TelemetryUpdate
	cancelFunc       context.CancelFunc
}

// SubscriptionConfig configures what events a subscription receives
type SubscriptionConfig struct {
	DeviceIDs        []models.DeviceIdentifier // Empty means all devices
	MeasurementTypes []models.MeasurementType  // Empty means all measurement types
	EventTypes       []models.UpdateType       // Empty means all event types (measurements, status, etc.)
	BufferSize       int                       // Channel buffer size, defaults to 100
}

// TelemetryBroadcaster manages a single polling loop that broadcasts telemetry updates
// to multiple subscribers, reducing database load and improving efficiency
type TelemetryBroadcaster struct {
	orgID              int64
	telemetryDataStore TelemetryDataStore
	pollInterval       time.Duration

	mu            sync.RWMutex
	subscriptions map[string]*Subscription
	running       bool
	cancelFunc    context.CancelFunc
}

// NewTelemetryBroadcaster creates a new broadcaster for an organization
func NewTelemetryBroadcaster(orgID int64, telemetryDataStore TelemetryDataStore, pollInterval time.Duration) *TelemetryBroadcaster {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second // default poll interval
	}

	return &TelemetryBroadcaster{
		orgID:              orgID,
		telemetryDataStore: telemetryDataStore,
		pollInterval:       pollInterval,
		subscriptions:      make(map[string]*Subscription),
	}
}

// Start begins the broadcaster's polling loop
func (b *TelemetryBroadcaster) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return nil // already running
	}

	pollCtx, cancelFunc := context.WithCancel(ctx)
	b.cancelFunc = cancelFunc
	b.running = true

	go b.pollLoop(pollCtx)

	return nil
}

// Stop stops the broadcaster and closes all subscriptions
func (b *TelemetryBroadcaster) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return
	}

	b.running = false
	if b.cancelFunc != nil {
		b.cancelFunc()
	}

	// Close all subscription channels
	for _, sub := range b.subscriptions {
		close(sub.updateChan)
	}
	b.subscriptions = make(map[string]*Subscription)
}

// Subscribe creates a new subscription and returns a channel for receiving updates
func (b *TelemetryBroadcaster) Subscribe(ctx context.Context, config SubscriptionConfig) (<-chan models.TelemetryUpdate, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	bufferSize := config.BufferSize
	if bufferSize <= 0 {
		bufferSize = 100
	}

	// Create device ID filter map
	var deviceIDFilter map[models.DeviceIdentifier]bool
	if len(config.DeviceIDs) > 0 {
		deviceIDFilter = make(map[models.DeviceIdentifier]bool, len(config.DeviceIDs))
		for _, id := range config.DeviceIDs {
			deviceIDFilter[id] = true
		}
	}

	// Create measurement type filter map
	var measurementTypeFilter map[models.MeasurementType]bool
	if len(config.MeasurementTypes) > 0 {
		measurementTypeFilter = make(map[models.MeasurementType]bool, len(config.MeasurementTypes))
		for _, mType := range config.MeasurementTypes {
			measurementTypeFilter[mType] = true
		}
	}

	// Create event type filter map
	var eventTypeFilter map[models.UpdateType]bool
	if len(config.EventTypes) > 0 {
		eventTypeFilter = make(map[models.UpdateType]bool, len(config.EventTypes))
		for _, eType := range config.EventTypes {
			eventTypeFilter[eType] = true
		}
	}

	subCtx, cancel := context.WithCancel(ctx)
	updateChan := make(chan models.TelemetryUpdate, bufferSize)

	sub := &Subscription{
		id:               uuid.New().String(),
		deviceIDs:        deviceIDFilter,
		measurementTypes: measurementTypeFilter,
		eventTypes:       eventTypeFilter,
		updateChan:       updateChan,
		cancelFunc:       cancel,
	}

	b.subscriptions[sub.id] = sub

	// Unsubscribe function
	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		if s, exists := b.subscriptions[sub.id]; exists {
			s.cancelFunc()
			close(s.updateChan)
			delete(b.subscriptions, sub.id)
		}
	}

	// Monitor context cancellation and auto-unsubscribe
	go func() {
		<-subCtx.Done()
		unsubscribe()
	}()

	return updateChan, unsubscribe, nil
}

// PublishStatusChange manually publishes a device status change event to all subscribers
// This bypasses the InfluxDB polling loop and immediately notifies subscribers of status changes
func (b *TelemetryBroadcaster) PublishStatusChange(deviceID models.DeviceIdentifier, deviceStatus mm.MinerStatus) {
	update := models.TelemetryUpdate{
		Type:             models.UpdateTypeDeviceStatus,
		DeviceIdentifier: deviceID,
		Timestamp:        time.Now(),
		DeviceStatus:     &deviceStatus,
	}

	slog.Info("[BROADCASTER] PublishStatusChange called",
		"orgID", b.orgID,
		"deviceID", deviceID,
		"status", deviceStatus,
		"updateType", update.Type)

	b.broadcast(update)
}

// pollLoop is the main polling loop that fetches telemetry and broadcasts to subscribers
func (b *TelemetryBroadcaster) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()

	lastTimestamp := time.Now()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			b.mu.RLock()
			if len(b.subscriptions) == 0 {
				b.mu.RUnlock()
				continue // no subscribers, skip polling
			}

			// Collect all unique device IDs and measurement types from all subscriptions
			deviceIDs, measurementTypes := b.collectSubscriptionFilters()
			b.mu.RUnlock()

			if len(deviceIDs) == 0 || len(measurementTypes) == 0 {
				continue // nothing to poll
			}

			// Query telemetry store
			streamQuery := models.StreamQuery{
				DeviceIDs:        deviceIDs,
				MeasurementTypes: measurementTypes,
				IncludeHeartbeat: true,
			}

			// Fetch updates since last timestamp
			updates := b.fetchUpdates(ctx, streamQuery, lastTimestamp)

			// Broadcast updates to subscribers
			for _, update := range updates {
				if update.Timestamp.After(lastTimestamp) {
					lastTimestamp = update.Timestamp
				}
				b.broadcast(update)
			}
		}
	}
}

// collectSubscriptionFilters aggregates all device IDs and measurement types from active subscriptions
func (b *TelemetryBroadcaster) collectSubscriptionFilters() ([]models.DeviceIdentifier, []models.MeasurementType) {
	deviceIDSet := make(map[models.DeviceIdentifier]bool)
	measurementTypeSet := make(map[models.MeasurementType]bool)
	hasAllDevices := false
	hasAllMeasurements := false

	for _, sub := range b.subscriptions {
		// Check device filters
		if sub.deviceIDs == nil {
			hasAllDevices = true
		} else {
			for deviceID := range sub.deviceIDs {
				deviceIDSet[deviceID] = true
			}
		}

		// Check measurement type filters
		if sub.measurementTypes == nil {
			hasAllMeasurements = true
		} else {
			for mType := range sub.measurementTypes {
				measurementTypeSet[mType] = true
			}
		}
	}

	// If any subscriber wants all devices, we need to query all devices for this org
	// For now, we'll just query the specific devices requested
	// TODO: Add org-wide device query support if hasAllDevices is true

	var deviceIDs []models.DeviceIdentifier
	if !hasAllDevices {
		for deviceID := range deviceIDSet {
			deviceIDs = append(deviceIDs, deviceID)
		}
	}

	var measurementTypes []models.MeasurementType
	if !hasAllMeasurements {
		for mType := range measurementTypeSet {
			measurementTypes = append(measurementTypes, mType)
		}
	} else {
		// Subscribe to all common measurement types
		measurementTypes = []models.MeasurementType{
			models.MeasurementTypePower,
			models.MeasurementTypeHashrate,
			models.MeasurementTypeTemperature,
			models.MeasurementTypeEfficiency,
		}
	}

	return deviceIDs, measurementTypes
}

// fetchUpdates queries the telemetry store for updates since lastTimestamp
func (b *TelemetryBroadcaster) fetchUpdates(ctx context.Context, query models.StreamQuery, sinceTimestamp time.Time) []models.TelemetryUpdate {
	// For now, we'll use the existing StreamTelemetryUpdates and read from it once
	// In a production implementation, we'd want to make a direct query instead
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	updateChan, err := b.telemetryDataStore.StreamTelemetryUpdates(timeoutCtx, query)
	if err != nil {
		slog.Error("failed to stream telemetry updates in broadcaster", "error", err, "orgID", b.orgID)
		return nil
	}

	var updates []models.TelemetryUpdate

	// Read available updates with timeout
	pollTimeout := time.After(b.pollInterval / 2) // use half the poll interval
	for {
		select {
		case update, ok := <-updateChan:
			if !ok {
				return updates
			}
			// Only include updates newer than our last timestamp
			if update.Type == models.UpdateTypeTelemetry && update.Timestamp.After(sinceTimestamp) {
				updates = append(updates, update)
			}
		case <-pollTimeout:
			return updates
		case <-ctx.Done():
			return updates
		}
	}
}

// broadcast sends an update to all matching subscribers
func (b *TelemetryBroadcaster) broadcast(update models.TelemetryUpdate) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	matchingSubscribers := 0
	sentCount := 0
	skippedCount := 0

	for _, sub := range b.subscriptions {
		// Check if subscriber is interested in this event type
		if sub.eventTypes != nil && !sub.eventTypes[update.Type] {
			continue
		}

		// Check if subscriber is interested in this device
		if sub.deviceIDs != nil && !sub.deviceIDs[update.DeviceIdentifier] {
			continue
		}

		// Check if subscriber is interested in this measurement type (only for telemetry updates)
		if update.Type == models.UpdateTypeTelemetry && update.MeasurementName != "" {
			if sub.measurementTypes != nil {
				// Determine measurement type from the data
				mType := models.MeasurementNameToType(update.MeasurementName)
				if !sub.measurementTypes[mType] {
					continue
				}
			}
		}

		matchingSubscribers++

		// Send update to subscriber (non-blocking)
		select {
		case sub.updateChan <- update:
			sentCount++
		default:
			skippedCount++
			// Channel full, subscriber is slow - log and skip
			slog.Warn("subscriber channel full, dropping update",
				"subscriptionID", sub.id,
				"deviceIdentifier", update.DeviceIdentifier,
				"updateType", update.Type)
		}
	}

	slog.Info("[BROADCASTER] Broadcast complete",
		"orgID", b.orgID,
		"deviceIdentifier", update.DeviceIdentifier,
		"updateType", update.Type,
		"totalSubscriptions", len(b.subscriptions),
		"matchingSubscribers", matchingSubscribers,
		"sentCount", sentCount,
		"skippedCount", skippedCount)
}
