package fleetmanagement

import (
	"context"
	"time"

	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// TelemetryCollector defines the interface for collecting miner telemetry data
type TelemetryCollector interface {
	// RemoveDevices removes devices from the telemetry scheduler so they are no longer polled
	RemoveDevices(ctx context.Context, deviceID ...minerModels.DeviceIdentifier) error
	// GetLatestDeviceMetrics fetches the latest telemetry metrics for a batch of devices
	GetLatestDeviceMetrics(ctx context.Context, deviceIDs []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error)
	// RefreshDevice forces immediate telemetry/status collection for one device.
	RefreshDevice(ctx context.Context, device telemetryModels.Device) error
	// RefreshDeviceTimeout returns the configured timeout budget for one refresh operation.
	RefreshDeviceTimeout() time.Duration
}
