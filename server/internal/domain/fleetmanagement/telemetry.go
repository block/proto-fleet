package fleetmanagement

import (
	"context"
	"math/rand"
	"time"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TelemetryCollector defines the interface for collecting miner telemetry data
type TelemetryCollector interface {
	// GetMinerTelemetry returns the latest telemetry data for a miner
	GetMinerTelemetry(ctx context.Context, deviceID string, dataMode pb.DataMode, timeSeriesConfig *commonpb.TimeSeriesConfig, measurementConfigs []*pb.MeasurementConfig) (*models.MinerTelemetry, error)

	// GetMinerComponentStatus returns the latest component status for a miner
	GetMinerComponentStatus(ctx context.Context, deviceID string) (*pb.MinerComponentStatus, error)

	// StreamMeasurements streams measurement updates for the specified miners and measurement types
	StreamMeasurements(ctx context.Context, deviceIDs []string, measurementTypes []pb.MeasurementConfig_MeasurementType) (<-chan *pb.StreamMinerUpdatesResponse, error)

	// StreamComponentStatus streams component status updates for the specified miners
	StreamComponentStatus(ctx context.Context, deviceIDs []string) (<-chan *pb.StreamMinerUpdatesResponse, error)

	// SubscribeToTelemetryUpdates subscribes to raw telemetry updates for an organization
	// This allows consumers to receive telemetry events without the conversion to protobuf responses
	// eventTypes filters which event types to receive (empty means all types)
	SubscribeToTelemetryUpdates(ctx context.Context, orgID int64, deviceIDs []string, eventTypes []telemetryModels.UpdateType) (<-chan telemetryModels.TelemetryUpdate, func(), error)
}

// MockTelemetryCollector provides a mock implementation of TelemetryCollector for testing
type MockTelemetryCollector struct{}

func NewMockTelemetryCollector() TelemetryCollector {
	return &MockTelemetryCollector{}
}

func (m *MockTelemetryCollector) GetMinerTelemetry(ctx context.Context, _ string, dataMode pb.DataMode, timeSeriesConfig *commonpb.TimeSeriesConfig, measurementConfigs []*pb.MeasurementConfig) (*models.MinerTelemetry, error) {
	now := timestamppb.Now()

	// Create a map of measurement type to its config for easy lookup
	configMap := make(map[pb.MeasurementConfig_MeasurementType]*pb.MeasurementConfig)
	for _, config := range measurementConfigs {
		configMap[config.MeasurementType] = config
	}

	// Helper function to get measurements based on type and config
	getMeasurements := func(mType pb.MeasurementConfig_MeasurementType) []*commonpb.Measurement {
		// Check if there's a specific config for this measurement type
		if config, ok := configMap[mType]; ok {
			// Use measurement-specific config
			if config.DataMode == pb.DataMode_DATA_MODE_METADATA {
				return []*commonpb.Measurement{}
			}
			if config.DataMode == pb.DataMode_DATA_MODE_TIME_SERIES && config.TimeSeriesConfig != nil {
				return generateTimeSeriesMeasurements(mType, config.TimeSeriesConfig)
			}
			// For SNAPSHOT or unspecified, return single measurement
			return []*commonpb.Measurement{generateSnapshotMeasurement(mType, now)}
		}

		// No specific config, use global settings
		if dataMode == pb.DataMode_DATA_MODE_METADATA {
			return []*commonpb.Measurement{}
		}
		if dataMode == pb.DataMode_DATA_MODE_TIME_SERIES && timeSeriesConfig != nil {
			return generateTimeSeriesMeasurements(mType, timeSeriesConfig)
		}
		// Default to SNAPSHOT mode
		return []*commonpb.Measurement{generateSnapshotMeasurement(mType, now)}
	}

	return &models.MinerTelemetry{
		PowerUsage:  getMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE),
		Temperature: getMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_TEMPERATURE),
		Hashrate:    getMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_HASHRATE),
		Efficiency:  getMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_EFFICIENCY),
		Timestamp:   now,
	}, nil
}

// Helper function to generate a single measurement
func generateSnapshotMeasurement(mType pb.MeasurementConfig_MeasurementType, timestamp *timestamppb.Timestamp) *commonpb.Measurement {
	return &commonpb.Measurement{
		Value:     generateMockValue(mType),
		Timestamp: timestamp,
	}
}

// Helper function to generate time series measurements
func generateTimeSeriesMeasurements(mType pb.MeasurementConfig_MeasurementType, config *commonpb.TimeSeriesConfig) []*commonpb.Measurement {
	var startTime, endTime time.Time

	// Determine time range based on config
	switch ts := config.TimeSelection.(type) {
	case *commonpb.TimeSeriesConfig_LookbackPeriod:
		endTime = time.Now()
		startTime = endTime.Add(-ts.LookbackPeriod.AsDuration())
	case *commonpb.TimeSeriesConfig_Interval:
		if ts.Interval.StartTime != nil {
			startTime = ts.Interval.StartTime.AsTime()
		} else {
			startTime = time.Now().Add(-time.Hour) // Default to last hour
		}
		if ts.Interval.EndTime != nil {
			endTime = ts.Interval.EndTime.AsTime()
		} else {
			endTime = time.Now()
		}
	default:
		// Default to last hour if no time selection
		endTime = time.Now()
		startTime = endTime.Add(-time.Hour)
	}

	// Generate data points based on resolution
	resolution := config.Resolution
	if resolution <= 0 {
		resolution = 60 // Default to 1 minute resolution
	}

	var measurements []*commonpb.Measurement
	for t := startTime; t.Before(endTime); t = t.Add(time.Duration(resolution) * time.Second) {
		measurements = append(measurements, &commonpb.Measurement{
			Value:     generateMockValue(mType),
			Timestamp: timestamppb.New(t),
		})
	}

	return measurements
}

func (m *MockTelemetryCollector) GetMinerComponentStatus(ctx context.Context, _ string) (*pb.MinerComponentStatus, error) {
	statuses := []pb.ComponentStatus{
		pb.ComponentStatus_COMPONENT_STATUS_OK,
		pb.ComponentStatus_COMPONENT_STATUS_WARNING,
		pb.ComponentStatus_COMPONENT_STATUS_ERROR,
	}
	return &pb.MinerComponentStatus{
		ControlBoard: statuses[rand.Intn(len(statuses))],
		Fans:         statuses[rand.Intn(len(statuses))],
		HashBoards:   statuses[rand.Intn(len(statuses))],
		Psu:          statuses[rand.Intn(len(statuses))],
	}, nil
}

func (m *MockTelemetryCollector) StreamMeasurements(ctx context.Context, deviceIDs []string, measurementTypes []pb.MeasurementConfig_MeasurementType) (<-chan *pb.StreamMinerUpdatesResponse, error) {
	ch := make(chan *pb.StreamMinerUpdatesResponse, 100)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, deviceID := range deviceIDs {
					for _, mType := range measurementTypes {
						measurement := &pb.MeasurementUpdate{
							MeasurementType: mType,
							Measurement: &commonpb.Measurement{
								Value:     generateMockValue(mType),
								Timestamp: timestamppb.Now(),
							},
						}
						resp := &pb.StreamMinerUpdatesResponse{
							Timestamp:        timestamppb.Now(),
							DeviceIdentifier: deviceID,
							Update: &pb.StreamMinerUpdatesResponse_Measurement{
								Measurement: measurement,
							},
						}
						select {
						case <-ctx.Done():
							return
						case ch <- resp:
						}
					}
				}
			}
		}
	}()

	return ch, nil
}

func (m *MockTelemetryCollector) StreamComponentStatus(ctx context.Context, deviceIDs []string) (<-chan *pb.StreamMinerUpdatesResponse, error) {
	ch := make(chan *pb.StreamMinerUpdatesResponse, 100)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(5 * time.Second) // Status changes less frequently
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, deviceID := range deviceIDs {
					components := []pb.ComponentStatusUpdate_Component{
						pb.ComponentStatusUpdate_COMPONENT_CONTROL_BOARD,
						pb.ComponentStatusUpdate_COMPONENT_FANS,
						pb.ComponentStatusUpdate_COMPONENT_HASH_BOARDS,
						pb.ComponentStatusUpdate_COMPONENT_PSU,
					}

					for _, component := range components {
						update := &pb.ComponentStatusUpdate{
							Component: component,
							Status:    generateMockStatus(),
						}

						resp := &pb.StreamMinerUpdatesResponse{
							Timestamp:        timestamppb.Now(),
							DeviceIdentifier: deviceID,
							Update: &pb.StreamMinerUpdatesResponse_Status{
								Status: update,
							},
						}

						select {
						case <-ctx.Done():
							return
						case ch <- resp:
						}
					}
				}
			}
		}
	}()

	return ch, nil
}

func generateMockValue(mType pb.MeasurementConfig_MeasurementType) float64 {
	switch mType {
	case pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE:
		return rand.Float64() * 3000 // 0-3000W
	case pb.MeasurementConfig_MEASUREMENT_TYPE_TEMPERATURE:
		return rand.Float64()*40 + 40 // 40-80°C
	case pb.MeasurementConfig_MEASUREMENT_TYPE_HASHRATE:
		return rand.Float64()*50 + 100 // 100-150 TH/s
	case pb.MeasurementConfig_MEASUREMENT_TYPE_EFFICIENCY:
		return rand.Float64()*10 + 30 // 30-40 J/TH
	case pb.MeasurementConfig_MEASUREMENT_TYPE_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

func generateMockStatus() pb.ComponentStatus {
	statuses := []pb.ComponentStatus{
		pb.ComponentStatus_COMPONENT_STATUS_OK,
		pb.ComponentStatus_COMPONENT_STATUS_WARNING,
		pb.ComponentStatus_COMPONENT_STATUS_ERROR,
	}
	return statuses[rand.Intn(len(statuses))]
}

func (m *MockTelemetryCollector) SubscribeToTelemetryUpdates(ctx context.Context, _ int64, _ []string, _ []telemetryModels.UpdateType) (<-chan telemetryModels.TelemetryUpdate, func(), error) {
	ch := make(chan telemetryModels.TelemetryUpdate, 100)

	// Use a cancel context to coordinate cleanup
	subCtx, cancel := context.WithCancel(ctx)

	// Mock implementation - just return an empty channel that closes when context is done
	go func() {
		<-subCtx.Done()
		close(ch)
	}()

	unsubscribe := func() {
		cancel() // Signal the goroutine to close the channel
	}

	return ch, unsubscribe, nil
}
