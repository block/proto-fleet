package fleetmanagement

import (
	"context"

	minerModels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
)

// TelemetryCollector defines the interface for collecting miner telemetry data
type TelemetryCollector interface {
	// RemoveDevices removes devices from the telemetry scheduler so they are no longer polled
	RemoveDevices(ctx context.Context, deviceID ...minerModels.DeviceIdentifier) error
}

// MockTelemetryCollector provides a mock implementation of TelemetryCollector for testing
type MockTelemetryCollector struct{}

func NewMockTelemetryCollector() TelemetryCollector {
	return &MockTelemetryCollector{}
}

func (m *MockTelemetryCollector) RemoveDevices(_ context.Context, _ ...minerModels.DeviceIdentifier) error {
	return nil
}
