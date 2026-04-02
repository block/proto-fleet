package fleetmanagement

import (
	"context"

	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// TelemetryCollector defines the interface for collecting miner telemetry data
type TelemetryCollector interface {
	// RemoveDevices removes devices from the telemetry scheduler so they are no longer polled
	RemoveDevices(ctx context.Context, deviceID ...minerModels.DeviceIdentifier) error
	// GetLatestDeviceMetrics fetches the latest telemetry metrics for a batch of devices
	GetLatestDeviceMetrics(ctx context.Context, deviceIDs []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error)
}

// MockTelemetryCollector provides a mock implementation of TelemetryCollector for testing
type MockTelemetryCollector struct{}

func NewMockTelemetryCollector() TelemetryCollector {
	return &MockTelemetryCollector{}
}

func (m *MockTelemetryCollector) RemoveDevices(_ context.Context, _ ...minerModels.DeviceIdentifier) error {
	return nil
}

func (m *MockTelemetryCollector) GetLatestDeviceMetrics(_ context.Context, _ []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	return nil, nil
}
