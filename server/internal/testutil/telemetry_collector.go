package testutil

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// MockTelemetryCollector provides a mock implementation of fleetmanagement.TelemetryCollector for testing
type MockTelemetryCollector struct{}

func NewMockTelemetryCollector() fleetmanagement.TelemetryCollector {
	return &MockTelemetryCollector{}
}

func (m *MockTelemetryCollector) RemoveDevices(_ context.Context, _ ...minerModels.DeviceIdentifier) error {
	return nil
}

func (m *MockTelemetryCollector) GetLatestDeviceMetrics(_ context.Context, _ []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	return nil, nil
}

func (m *MockTelemetryCollector) RefreshDevice(_ context.Context, _ telemetryModels.Device) error {
	return nil
}

func (m *MockTelemetryCollector) RefreshDeviceTimeout() time.Duration {
	return 10 * time.Second
}
