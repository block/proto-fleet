package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	fleetmanagementModels "github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// Conversion factor from MH/s to TH/s
	mhsToThsConversionFactor = 1e6
	// Conversion factor from watts to kilowatts
	wattsToKwConversionFactor = 1e3
)

// convertHashrateToThs converts hashrate from MH/s to TH/s
func convertHashrateToThs(valueInMhs float64) float64 {
	return valueInMhs / mhsToThsConversionFactor
}

// convertPowerToKw converts power from watts to kilowatts
func convertPowerToKw(valueInWatts float64) float64 {
	return valueInWatts / wattsToKwConversionFactor
}

const (
	defaultUpdateInterval = 1 * time.Minute
	defaultGranularity    = 1 * time.Minute
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
	Store(ctx context.Context, data ...models.Telemetry) error
	GetLatestTelemetry(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error)
	GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error)
	GetTelemetryMetadata(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error)
	StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error)
	GetAggregatedTelemetry(ctx context.Context, query models.AggregationQuery) ([]models.AggregatedTelemetry, error)
	GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error)
	Ping(ctx context.Context) error
	Close() error
}
type MinerGetter interface {
	GetMinerFromDeviceIdentifier(ctx context.Context, deviceIdentifier models.DeviceIdentifier) (interfaces.Miner, error)
}

type TelemetryService struct {
	config             Config
	updateScheduler    UpdateScheduler
	telemetryDataStore TelemetryDataStore
	minerManager       MinerGetter
	deviceStore        stores.DeviceStore
	mux                sync.Mutex
	tasks              chan models.Device
	statusTasks        chan models.Device
	cancelFunc         context.CancelFunc
	lookBackDuration   time.Duration
	trackedDevices     sync.Map
}

func NewTelemetryService(config Config, telemetryDataStore TelemetryDataStore, minerManager MinerGetter, scheduler UpdateScheduler, deviceStore stores.DeviceStore) *TelemetryService {
	return &TelemetryService{
		config:             config,
		telemetryDataStore: telemetryDataStore,
		minerManager:       minerManager,
		updateScheduler:    scheduler,
		deviceStore:        deviceStore,
		// channel for tasks to process telemetry data, it is set so that there is at least 1 queued task for every worker.
		tasks:            make(chan models.Device, config.ConcurrencyLimit),
		statusTasks:      make(chan models.Device, config.ConcurrencyLimit),
		lookBackDuration: -1 * (config.StalenessThreshold - config.FetchInterval),
	}
}

func (s *TelemetryService) AddDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error {
	if len(deviceID) == 0 {
		return nil
	}
	for _, id := range deviceID {
		s.tasks <- models.Device{ID: id, LastUpdatedAt: time.Now().Add(-s.config.NewDeviceLookback)} // Initialize with current time minus lookback duration
		s.trackedDevices.Store(id, struct{}{})
	}
	return s.updateScheduler.AddNewDevices(ctx, deviceID...)
}

func (s *TelemetryService) RemoveDevices(ctx context.Context, deviceIDs ...models.DeviceIdentifier) error {
	if len(deviceIDs) == 0 {
		return nil
	}
	for _, id := range deviceIDs {
		s.trackedDevices.Delete(id)
	}
	return s.updateScheduler.RemoveDevices(ctx, deviceIDs...)
}

func (s *TelemetryService) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	go s.gatherMetricsRoutine(ctx)
	go s.devicePollingRoutine(ctx)
	go s.gatherDeviceStatusRoutine(ctx)
	return nil
}

func (s *TelemetryService) Stop(ctx context.Context) error {
	s.cancelFunc()
	defer close(s.tasks)
	defer close(s.statusTasks)
	return nil
}

func (s *TelemetryService) gatherDeviceStatusRoutine(ctx context.Context) {
	interval := s.config.DeviceStatusPollInterval
	if interval <= 0 {
		interval = 10 * time.Second // Default interval if not configured
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Fetch device statuses
			s.trackedDevices.Range(func(key, _ any) bool {
				if id, ok := key.(models.DeviceIdentifier); ok {
					s.statusTasks <- models.Device{ID: id}
				}
				return true
			})
		}
	}
}

func (s *TelemetryService) gatherMetricsRoutine(ctx context.Context) {
	if !s.mux.TryLock() {
		slog.Info("Telemetry gathering routine is already running")
		return // Another routine is already running
	}
	defer s.mux.Unlock()

	// Spin up workers to fetch telemetry data
	for range s.config.ConcurrencyLimit {
		go s.worker(ctx)
	}

	// Periodically fetch devices that need telemetry data
	fetchInterval := s.config.FetchInterval
	if fetchInterval <= 0 {
		fetchInterval = 10 * time.Second // Default interval if not configured
	}
	ticker := time.NewTicker(fetchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Fetch devices that need telemetry data, considering the staleness threshold and fetch interval
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
		pollInterval = 10 * time.Minute // Default interval if not configured
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Run once immediately on startup
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
		slog.Debug("no paired devices found to add to telemetry service")
		return nil
	}

	if err := s.AddDevices(ctx, deviceIDs...); err != nil {
		// failed to add devices is expected to happen from time to time.
		slog.Debug("failed to add paired devices to telemetry service", "error", err)
		return nil
	}

	slog.Debug("loaded paired devices into telemetry service", "count", len(deviceIDs))
	return nil
}

func (s *TelemetryService) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case device := <-s.tasks:
			if err := s.GetTelemetryFromDevice(ctx, device); err != nil {
				slog.Warn("failed to get telemetry from device", "deviceID", device.ID, "error", err)
				if err := s.updateScheduler.AddFailedDevices(ctx, device); err != nil {
					slog.Warn("failed to add failed telemetry device back into scheduler", "deviceID", device.ID, "error", err)
				}
			}
			if err := s.GetStatusForDevice(ctx, device); err != nil {
				slog.Warn("failed to get status for device", "deviceID", device.ID, "error", err)
			}

		case device := <-s.statusTasks:
			if err := s.GetStatusForDevice(ctx, device); err != nil {
				slog.Warn("failed to get status for device", "deviceID", device.ID, "error", err)
			}
		}
	}
}

func (s *TelemetryService) GetStatusForDevice(ctx context.Context, device models.Device) error {
	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, device.ID)
	if err != nil {
		return fmt.Errorf("failed to get miner from device ID %s: %w", device.ID, err)
	}

	status, err := miner.GetDeviceStatus(ctx)
	if err != nil {
		slog.Error("failed to get device status for miner", "deviceID", device.ID, "error", err)
	}

	err = s.deviceStore.UpsertDeviceStatus(ctx, device.ID, status, "")
	if err != nil {
		return fmt.Errorf("failed to upsert device status for device %s: %w", device.ID, err)
	}

	if status == mm.MinerStatusError || status == mm.MinerStatusOffline {
		return nil
	}

	failed, failedAt, err := s.updateScheduler.IsFailedDevice(ctx, device.ID)
	if err != nil {
		return fmt.Errorf("failed to check if device %s is failed: %w", device.ID, err)
	}
	if failed {
		err := s.updateScheduler.AddDevices(ctx, models.Device{
			ID:            device.ID,
			LastUpdatedAt: failedAt,
		})
		if err != nil {
			slog.Warn("failed to add failed device back into scheduler", "deviceID", device.ID, "error", err)
		}
	}

	return nil
}

// GetTelemetryFromDevice fetches telemetry data from a specific device,
// and stores it in the telemetry data store
func (s *TelemetryService) GetTelemetryFromDevice(ctx context.Context, device models.Device) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.MetricTimeout)
	defer cancel()
	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, device.ID)
	// TODO(DASH-446): update to handle dor unique miner discovery errors.
	if err != nil {
		return fmt.Errorf("failed to get miner from device ID %s: %w", device.ID, err)
	}
	telemetry, err := miner.GetTelemetry(ctx, device.LastUpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to get telemetry measurements for device %s: %w", device.ID, err)
	}
	err = s.telemetryDataStore.Store(ctx, telemetry...)
	if err != nil {
		slog.Error("failed to store telemetry data", "deviceID", device.ID, "error", err)
		return fmt.Errorf("failed to store telemetry data for device %s: %w", device.ID, err)
	}
	err = s.updateScheduler.AddDevices(ctx, models.Device{
		ID:            device.ID,
		LastUpdatedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("failed to update device last updated time for device %s: %w", device.ID, err)
	}
	return nil
}

// GetLatestTelemetry delegates to the datastore to retrieve latest telemetry data
func (s *TelemetryService) GetLatestTelemetry(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error) {
	telemetryData, err := s.telemetryDataStore.GetLatestTelemetry(ctx, query)
	if err != nil {
		return nil, err
	}

	// Convert hashrate values from MH/s to TH/s and power values from watts to kW
	for i := range telemetryData {
		if telemetryData[i].Measurement == models.MeasurementTypeHashrate.InfluxMeasurementName() {
			if value, ok := telemetryData[i].Fields["value"].(float64); ok {
				telemetryData[i].Fields["value"] = convertHashrateToThs(value)
			}
		} else if telemetryData[i].Measurement == models.MeasurementTypePower.InfluxMeasurementName() {
			if value, ok := telemetryData[i].Fields["value"].(float64); ok {
				telemetryData[i].Fields["value"] = convertPowerToKw(value)
			}
		}
	}

	return telemetryData, nil
}

func (s *TelemetryService) GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error) {
	telemetryData, err := s.telemetryDataStore.GetTimeSeriesTelemetry(ctx, query)
	if err != nil {
		return nil, err
	}

	// Convert hashrate values from MH/s to TH/s and power values from watts to kW
	for i := range telemetryData {
		if telemetryData[i].Measurement == models.MeasurementTypeHashrate.InfluxMeasurementName() {
			if value, ok := telemetryData[i].Fields["value"].(float64); ok {
				telemetryData[i].Fields["value"] = convertHashrateToThs(value)
			}
		} else if telemetryData[i].Measurement == models.MeasurementTypePower.InfluxMeasurementName() {
			if value, ok := telemetryData[i].Fields["value"].(float64); ok {
				telemetryData[i].Fields["value"] = convertPowerToKw(value)
			}
		}
	}

	return telemetryData, nil
}

func (s *TelemetryService) GetTelemetryMetadata(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error) {
	return s.telemetryDataStore.GetTelemetryMetadata(ctx, query)
}

func (s *TelemetryService) StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
	return s.telemetryDataStore.StreamTelemetryUpdates(ctx, query)
}

func (s *TelemetryService) StreamDeviceStatusUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
	// Create a new channel for device status updates
	updateChan := make(chan models.TelemetryUpdate)

	go func() {
		defer close(updateChan)
		heartbeatInterval := *query.HeartbeatInterval
		if heartbeatInterval <= 0 {
			heartbeatInterval = 30 * time.Second // Default heartbeat interval if not configured
		}
		ticker := time.NewTicker(heartbeatInterval)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Fetch device status updates from the telemetry data store
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

func (s *TelemetryService) GetAggregatedTelemetry(ctx context.Context, query models.AggregationQuery) ([]models.AggregatedTelemetry, error) {
	aggregatedData, err := s.telemetryDataStore.GetAggregatedTelemetry(ctx, query)
	if err != nil {
		return nil, err
	}

	// Convert hashrate values from MH/s to TH/s and power values from watts to kW
	for i := range aggregatedData {
		if aggregatedData[i].MeasurementType == models.MeasurementTypeHashrate {
			aggregatedData[i].Value = convertHashrateToThs(aggregatedData[i].Value)
		} else if aggregatedData[i].MeasurementType == models.MeasurementTypePower {
			aggregatedData[i].Value = convertPowerToKw(aggregatedData[i].Value)
		}
	}

	return aggregatedData, nil
}

func (s *TelemetryService) GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	combinedMetrics, err := s.telemetryDataStore.GetCombinedMetrics(ctx, query)
	if err != nil {
		return models.CombinedMetric{}, err
	}

	// Convert hashrate values from MH/s to TH/s and power values from watts to kW
	for i := range combinedMetrics.Metrics {
		if combinedMetrics.Metrics[i].MeasurementType == models.MeasurementTypeHashrate {
			for j := range combinedMetrics.Metrics[i].AggregatedValues {
				combinedMetrics.Metrics[i].AggregatedValues[j].Value = convertHashrateToThs(combinedMetrics.Metrics[i].AggregatedValues[j].Value)
			}
		} else if combinedMetrics.Metrics[i].MeasurementType == models.MeasurementTypePower {
			for j := range combinedMetrics.Metrics[i].AggregatedValues {
				combinedMetrics.Metrics[i].AggregatedValues[j].Value = convertPowerToKw(combinedMetrics.Metrics[i].AggregatedValues[j].Value)
			}
		}
	}

	return combinedMetrics, nil
}

func (s *TelemetryService) StreamCombinedMetrics(ctx context.Context, query models.StreamCombinedMetricsQuery) (<-chan models.CombinedMetric, error) {
	updateChan := make(chan models.CombinedMetric)

	updateInterval := query.UpdateInterval
	if updateInterval == 0 {
		updateInterval = query.Granularity
		if updateInterval == 0 {
			updateInterval = defaultUpdateInterval
		}
	}

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
		Granularity:      query.Granularity,
		PageSize:         100,
	}

	now := time.Now()

	intervalNanos := updateInterval.Nanoseconds()
	alignedEndTime := time.Unix(0, (now.UnixNano()/intervalNanos)*intervalNanos)

	if alignedEndTime.After(now) {
		alignedEndTime = alignedEndTime.Add(-updateInterval)
	}

	startTime := alignedEndTime.Add(-updateInterval)

	combinedQuery.TimeRange = models.TimeRange{
		StartTime: &startTime,
		EndTime:   &alignedEndTime,
	}

	combinedMetrics, err := s.GetCombinedMetrics(ctx, combinedQuery)
	if err != nil {
		return fmt.Errorf("failed to get combined metrics: %w", err)
	}

	select {
	case updateChan <- combinedMetrics:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	}
}

// Ping checks the health of the telemetry datastore
func (s *TelemetryService) Ping(ctx context.Context) error {
	return s.telemetryDataStore.Ping(ctx)
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

// GetMinerComponentStatus returns the latest component status for a miner
func (s *TelemetryService) GetMinerComponentStatus(ctx context.Context, _ string) (*pb.MinerComponentStatus, error) {
	return &pb.MinerComponentStatus{
		ControlBoard: pb.ComponentStatus_COMPONENT_STATUS_UNSPECIFIED,
		Fans:         pb.ComponentStatus_COMPONENT_STATUS_UNSPECIFIED,
		HashBoards:   pb.ComponentStatus_COMPONENT_STATUS_UNSPECIFIED,
		Psu:          pb.ComponentStatus_COMPONENT_STATUS_UNSPECIFIED,
	}, nil
}

// StreamMeasurements streams measurement updates for the specified miners and measurement types
func (s *TelemetryService) StreamMeasurements(ctx context.Context, deviceIDs []string, measurementTypes []pb.MeasurementConfig_MeasurementType) (<-chan *pb.StreamMinerUpdatesResponse, error) {
	responseChan := make(chan *pb.StreamMinerUpdatesResponse, 100)

	// Convert device IDs and measurement types to internal models
	internalDeviceIDs := make([]models.DeviceIdentifier, len(deviceIDs))
	for i, id := range deviceIDs {
		internalDeviceIDs[i] = models.DeviceIdentifier(id)
	}

	internalMeasurementTypes := make([]models.MeasurementType, 0, len(measurementTypes))
	for _, mType := range measurementTypes {
		internalType := pbMeasurementTypeToInternal(mType)
		if internalType != models.MeasurementTypeUnknown {
			internalMeasurementTypes = append(internalMeasurementTypes, internalType)
		}
	}

	// Create stream query
	streamQuery := models.StreamQuery{
		DeviceIDs:        internalDeviceIDs,
		MeasurementTypes: internalMeasurementTypes,
		IncludeHeartbeat: true,
	}

	// Start streaming from the telemetry data store
	updateChan, err := s.telemetryDataStore.StreamTelemetryUpdates(ctx, streamQuery)
	if err != nil {
		close(responseChan)
		return nil, fmt.Errorf("failed to start telemetry stream: %w", err)
	}

	go func() {
		defer close(responseChan)

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

// StreamComponentStatus streams component status updates for the specified miners
func (s *TelemetryService) StreamComponentStatus(ctx context.Context, deviceIDs []string) (<-chan *pb.StreamMinerUpdatesResponse, error) {
	responseChan := make(chan *pb.StreamMinerUpdatesResponse, 100)

	// Convert device IDs to internal models
	internalDeviceIDs := make([]models.DeviceIdentifier, len(deviceIDs))
	for i, id := range deviceIDs {
		internalDeviceIDs[i] = models.DeviceIdentifier(id)
	}

	// Create stream query for device status updates
	streamQuery := models.StreamQuery{
		DeviceIDs:        internalDeviceIDs,
		MeasurementTypes: []models.MeasurementType{}, // Empty for status updates
		IncludeHeartbeat: true,
	}

	// Start streaming from the telemetry data store
	updateChan, err := s.telemetryDataStore.StreamTelemetryUpdates(ctx, streamQuery)
	if err != nil {
		close(responseChan)
		return nil, fmt.Errorf("failed to start component status stream: %w", err)
	}

	go func() {
		defer close(responseChan)

		ticker := time.NewTicker(5 * time.Second) // Status updates every 5 seconds
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Periodically check component status for each device
				for _, deviceID := range deviceIDs {
					status, err := s.GetMinerComponentStatus(ctx, deviceID)
					if err != nil {
						slog.Warn("failed to get component status for streaming", "deviceID", deviceID, "error", err)
						continue
					}

					// Create status update responses for each component
					components := []struct {
						component pb.ComponentStatusUpdate_Component
						status    pb.ComponentStatus
					}{
						{pb.ComponentStatusUpdate_COMPONENT_CONTROL_BOARD, status.ControlBoard},
						{pb.ComponentStatusUpdate_COMPONENT_FANS, status.Fans},
						{pb.ComponentStatusUpdate_COMPONENT_HASH_BOARDS, status.HashBoards},
						{pb.ComponentStatusUpdate_COMPONENT_PSU, status.Psu},
					}

					for _, comp := range components {
						response := &pb.StreamMinerUpdatesResponse{
							Timestamp:        timestamppb.Now(),
							DeviceIdentifier: deviceID,
							Update: &pb.StreamMinerUpdatesResponse_Status{
								Status: &pb.ComponentStatusUpdate{
									Component: comp.component,
									Status:    comp.status,
								},
							},
						}

						select {
						case responseChan <- response:
						case <-ctx.Done():
							return
						}
					}
				}
			case update, ok := <-updateChan:
				if !ok {
					return
				}

				// Handle device status updates from the telemetry stream
				if update.Type == models.UpdateTypeDeviceStatus && update.Status != nil {
					pbStatus := internalComponentStatusToPb(*update.Status)

					// Create status updates for all components (simplified approach)
					components := []pb.ComponentStatusUpdate_Component{
						pb.ComponentStatusUpdate_COMPONENT_CONTROL_BOARD,
						pb.ComponentStatusUpdate_COMPONENT_FANS,
						pb.ComponentStatusUpdate_COMPONENT_HASH_BOARDS,
						pb.ComponentStatusUpdate_COMPONENT_PSU,
					}

					for _, component := range components {
						response := &pb.StreamMinerUpdatesResponse{
							Timestamp:        timestamppb.New(update.Timestamp),
							DeviceIdentifier: string(update.DeviceID),
							Update: &pb.StreamMinerUpdatesResponse_Status{
								Status: &pb.ComponentStatusUpdate{
									Component: component,
									Status:    pbStatus,
								},
							},
						}

						select {
						case responseChan <- response:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()

	return responseChan, nil
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

// internalComponentStatusToPb converts internal component status to protobuf
func internalComponentStatusToPb(internalStatus models.ComponentStatus) pb.ComponentStatus {
	//nolint:exhaustive // there are only a few status to match at this time
	switch internalStatus {
	case models.ComponentStatusHealthy:
		return pb.ComponentStatus_COMPONENT_STATUS_OK
	case models.ComponentStatusWarning:
		return pb.ComponentStatus_COMPONENT_STATUS_WARNING
	case models.ComponentStatusCritical:
		return pb.ComponentStatus_COMPONENT_STATUS_ERROR
	case models.ComponentStatusOffline:
		return pb.ComponentStatus_COMPONENT_STATUS_ERROR
	case models.ComponentStatusUnknown:
		fallthrough
	default:
		return pb.ComponentStatus_COMPONENT_STATUS_UNSPECIFIED
	}
}

// getLatestMeasurements retrieves the latest measurements for a device and measurement type
func (s *TelemetryService) getLatestMeasurements(ctx context.Context, deviceID string, measurementType models.MeasurementType) ([]*commonpb.Measurement, error) {
	query := models.LatestTelemetryQuery{
		DeviceIDs:        []models.DeviceIdentifier{models.DeviceIdentifier(deviceID)},
		MeasurementTypes: []models.MeasurementType{measurementType},
	}

	telemetryData, err := s.telemetryDataStore.GetLatestTelemetry(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest telemetry: %w", err)
	}

	var measurements []*commonpb.Measurement
	for _, data := range telemetryData {
		if value, ok := data.Fields["value"].(float64); ok {
			// Convert hashrate from MH/s to TH/s and power from watts to kW
			if measurementType == models.MeasurementTypeHashrate {
				value = convertHashrateToThs(value)
			} else if measurementType == models.MeasurementTypePower {
				value = convertPowerToKw(value)
			}
			measurements = append(measurements, &commonpb.Measurement{
				Value:     value,
				Timestamp: timestamppb.New(data.Timestamp),
			})
		}
	}

	return measurements, nil
}

// getTimeSeriesMeasurements retrieves time series measurements for a device and measurement type
func (s *TelemetryService) getTimeSeriesMeasurements(ctx context.Context, deviceID string, measurementType models.MeasurementType, timeSeriesConfig *commonpb.TimeSeriesConfig) ([]*commonpb.Measurement, error) {
	// Convert time series config to internal query
	query := models.TimeSeriesTelemetryQuery{
		DeviceIDs:        []models.DeviceIdentifier{models.DeviceIdentifier(deviceID)},
		MeasurementTypes: []models.MeasurementType{measurementType},
	}

	// Set time range based on config
	switch ts := timeSeriesConfig.TimeSelection.(type) {
	case *commonpb.TimeSeriesConfig_LookbackPeriod:
		endTime := time.Now()
		startTime := endTime.Add(-ts.LookbackPeriod.AsDuration())
		query.TimeRange = models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		}
	case *commonpb.TimeSeriesConfig_Interval:
		if ts.Interval.StartTime != nil {
			startTime := ts.Interval.StartTime.AsTime()
			query.TimeRange.StartTime = &startTime
		}
		if ts.Interval.EndTime != nil {
			endTime := ts.Interval.EndTime.AsTime()
			query.TimeRange.EndTime = &endTime
		}
	default:
		// Default to last hour if no time selection
		endTime := time.Now()
		startTime := endTime.Add(-time.Hour)
		query.TimeRange = models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		}
	}

	telemetryData, err := s.telemetryDataStore.GetTimeSeriesTelemetry(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get time series telemetry: %w", err)
	}

	var measurements []*commonpb.Measurement
	for _, data := range telemetryData {
		if value, ok := data.Fields["value"].(float64); ok {
			// Convert hashrate from MH/s to TH/s and power from watts to kW
			if measurementType == models.MeasurementTypeHashrate {
				value = convertHashrateToThs(value)
			} else if measurementType == models.MeasurementTypePower {
				value = convertPowerToKw(value)
			}
			measurements = append(measurements, &commonpb.Measurement{
				Value:     value,
				Timestamp: timestamppb.New(data.Timestamp),
			})
		}
	}

	return measurements, nil
}

// convertTelemetryUpdateToResponse converts internal telemetry update to protobuf response
func (s *TelemetryService) convertTelemetryUpdateToResponse(update models.TelemetryUpdate, measurementTypes []pb.MeasurementConfig_MeasurementType) *pb.StreamMinerUpdatesResponse {
	//nolint:exhaustive // we handle all a few measurements for now
	switch update.Type {
	case models.UpdateTypeTelemetry:
		if update.Data == nil {
			return nil
		}

		// Extract measurement type from the telemetry data
		measurementName := update.Data.Measurement
		var internalMeasurementType models.MeasurementType

		// Map measurement name to internal type
		//nolint:exhaustive // we handle all a few measurements for now
		switch measurementName {
		case "power_w":
			internalMeasurementType = models.MeasurementTypePower
		case "temperature_c":
			internalMeasurementType = models.MeasurementTypeTemperature
		case "hashrate_mhs":
			internalMeasurementType = models.MeasurementTypeHashrate
		case "efficiency_jth":
			internalMeasurementType = models.MeasurementTypeEfficiency

		default:
			return nil // Unknown measurement type
		}

		// Convert to protobuf measurement type
		pbMeasurementType := internalMeasurementTypeToPb(internalMeasurementType)

		// Check if this measurement type is requested
		typeRequested := false
		for _, requestedType := range measurementTypes {
			if requestedType == pbMeasurementType {
				typeRequested = true
				break
			}
		}
		if !typeRequested {
			return nil
		}

		// Extract value from fields
		var value float64
		if val, ok := update.Data.Fields["value"].(float64); ok {
			value = val
			// Convert hashrate from MH/s to TH/s and power from watts to kW
			if internalMeasurementType == models.MeasurementTypeHashrate {
				value = convertHashrateToThs(value)
			} else if internalMeasurementType == models.MeasurementTypePower {
				value = convertPowerToKw(value)
			}
		}

		return &pb.StreamMinerUpdatesResponse{
			Timestamp:        timestamppb.New(update.Timestamp),
			DeviceIdentifier: string(update.DeviceID),
			Update: &pb.StreamMinerUpdatesResponse_Measurement{
				Measurement: &pb.MeasurementUpdate{
					MeasurementType: pbMeasurementType,
					Measurement: &commonpb.Measurement{
						Value:     value,
						Timestamp: timestamppb.New(update.Data.Timestamp),
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
		// For error updates, we could log them but not necessarily send them to clients
		if update.Error != nil {
			slog.Warn("telemetry stream error", "error", *update.Error, "deviceID", update.DeviceID)
		}
		return nil

	default:
		return nil
	}
}
