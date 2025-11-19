package plugins

import (
	"context"
	"math"
	"net/url"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/plugins/mappers"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

var _ interfaces.Miner = &PluginMiner{}
var _ interfaces.MinerInfo = &PluginMiner{}

// PluginMiner wraps an SDK Device to implement the interfaces.Miner interface.
//
// Lifecycle Management:
// SDK Devices have a Close() method that should be called to release resources, but the
// interfaces.Miner interface does not include a Close() method. Currently, SDK devices are
// cleaned up implicitly when the plugin process is killed during plugin manager shutdown.
//
// TODO(DASH-XXX): Consider adding explicit device lifecycle management:
//   - Option 1: Add Close() to interfaces.Miner interface (breaking change)
//   - Option 2: Track SDK devices in plugin manager and close them during shutdown
//   - Option 3: Document that plugin processes handle cleanup on exit
type PluginMiner struct {
	deviceID       models.DeviceIdentifier
	deviceType     models.Type
	serialNumber   string
	connectionInfo networking.ConnectionInfo
	sdkDevice      sdk.Device
	deviceInfo     sdk.DeviceInfo
}

// NewPluginMiner creates a new PluginMiner wrapper around an SDK Device
func NewPluginMiner(
	deviceID models.DeviceIdentifier,
	deviceType models.Type,
	serialNumber string,
	connectionInfo networking.ConnectionInfo,
	sdkDevice sdk.Device,
	deviceInfo sdk.DeviceInfo,
) *PluginMiner {
	return &PluginMiner{
		deviceID:       deviceID,
		deviceType:     deviceType,
		serialNumber:   serialNumber,
		connectionInfo: connectionInfo,
		sdkDevice:      sdkDevice,
		deviceInfo:     deviceInfo,
	}
}

// GetID implements interfaces.MinerInfo
func (p *PluginMiner) GetID() models.DeviceIdentifier {
	return p.deviceID
}

// GetType implements interfaces.MinerInfo
func (p *PluginMiner) GetType() models.Type {
	return p.deviceType
}

// GetSerialNumber implements interfaces.MinerInfo
func (p *PluginMiner) GetSerialNumber() string {
	return p.serialNumber
}

// GetConnectionInfo implements interfaces.MinerInfo
func (p *PluginMiner) GetConnectionInfo() networking.ConnectionInfo {
	return p.connectionInfo
}

// GetWebViewURL implements interfaces.MinerInfo
func (p *PluginMiner) GetWebViewURL() *url.URL {
	// Try to get the web view URL from the SDK device
	webViewURL, supported, err := p.sdkDevice.TryGetWebViewURL(context.Background())
	if err != nil || !supported || webViewURL == "" {
		// Fall back to constructing from connection info
		return p.connectionInfo.GetURL()
	}

	parsedURL, err := url.Parse(webViewURL)
	if err != nil {
		return nil
	}
	return parsedURL
}

// GetDeviceMetrics implements interfaces.Miner
// This is the critical method that bridges SDK metrics to Fleet's V2 format
func (p *PluginMiner) GetDeviceMetrics(ctx context.Context) (modelsV2.DeviceMetrics, error) {
	// Call the SDK Device's Status method
	sdkMetrics, err := p.sdkDevice.Status(ctx)
	if err != nil {
		return modelsV2.DeviceMetrics{}, fleeterror.NewInternalErrorf("failed to get SDK device metrics: %v", err)
	}

	// Use the SDK mapper to convert to V2 format
	v2Metrics := mappers.SDKDeviceMetricsToV2(sdkMetrics)

	return v2Metrics, nil
}

// GetTelemetry implements interfaces.Miner
// This is the legacy telemetry method, kept for backward compatibility
func (p *PluginMiner) GetTelemetry(ctx context.Context, _ time.Time) ([]telemetryModels.Telemetry, error) {
	// SDK devices don't support the legacy telemetry format
	// Return empty slice to indicate no legacy telemetry available
	return []telemetryModels.Telemetry{}, nil
}

// GetDeviceStatus implements interfaces.Miner
func (p *PluginMiner) GetDeviceStatus(ctx context.Context) (models.MinerStatus, error) {
	// Get device metrics to determine status
	metrics, err := p.sdkDevice.Status(ctx)
	if err != nil {
		return models.MinerStatusOffline, fleeterror.NewInternalErrorf("failed to get device status: %v", err)
	}

	// Map health status to miner status
	var status models.MinerStatus
	switch metrics.Health {
	case sdk.HealthHealthyActive:
		status = models.MinerStatusActive
	case sdk.HealthHealthyInactive:
		status = models.MinerStatusInactive
	case sdk.HealthWarning:
		status = models.MinerStatusActive // Still operational despite warning
	case sdk.HealthCritical:
		status = models.MinerStatusError
	case sdk.HealthUnknown:
		status = models.MinerStatusOffline
	case sdk.HealthStatusUnspecified:
		status = models.MinerStatusOffline
	default:
		status = models.MinerStatusOffline
	}

	return status, nil
}

// Reboot implements interfaces.Miner
func (p *PluginMiner) Reboot(ctx context.Context) error {
	if err := p.sdkDevice.Reboot(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to reboot device: %v", err)
	}
	return nil
}

// StartMining implements interfaces.Miner
func (p *PluginMiner) StartMining(ctx context.Context) error {
	if err := p.sdkDevice.StartMining(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to start mining: %v", err)
	}
	return nil
}

// StopMining implements interfaces.Miner
func (p *PluginMiner) StopMining(ctx context.Context) error {
	if err := p.sdkDevice.StopMining(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to stop mining: %v", err)
	}
	return nil
}

// SetCoolingMode implements interfaces.Miner
func (p *PluginMiner) SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error {
	// Convert protobuf cooling mode to SDK cooling mode
	var sdkMode sdk.CoolingMode
	switch payload.Mode {
	case pb.CoolingMode_COOLING_MODE_AIR_COOLED:
		sdkMode = sdk.CoolingModeAirCooled
	case pb.CoolingMode_COOLING_MODE_IMMERSION_COOLED:
		sdkMode = sdk.CoolingModeImmersionCooled
	case pb.CoolingMode_COOLING_MODE_UNSPECIFIED:
		sdkMode = sdk.CoolingModeUnspecified
	default:
		sdkMode = sdk.CoolingModeUnspecified
	}

	if err := p.sdkDevice.SetCoolingMode(ctx, sdkMode); err != nil {
		return fleeterror.NewInternalErrorf("failed to set cooling mode: %v", err)
	}
	return nil
}

// UpdateMiningPools implements interfaces.Miner
func (p *PluginMiner) UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
	// Convert Fleet pool configs to SDK pool configs
	sdkPools := []sdk.MiningPoolConfig{}

	// Add default pool
	poolConfig, err := validateAndConvertPoolConfig(payload.DefaultPool, "default")
	if err != nil {
		return err
	}
	sdkPools = append(sdkPools, poolConfig)

	// Add backup pools if present
	if payload.Backup1Pool != nil {
		poolConfig, err := validateAndConvertPoolConfig(*payload.Backup1Pool, "backup1")
		if err != nil {
			return err
		}
		sdkPools = append(sdkPools, poolConfig)
	}
	if payload.Backup2Pool != nil {
		poolConfig, err := validateAndConvertPoolConfig(*payload.Backup2Pool, "backup2")
		if err != nil {
			return err
		}
		sdkPools = append(sdkPools, poolConfig)
	}

	if err := p.sdkDevice.UpdateMiningPools(ctx, sdkPools); err != nil {
		return fleeterror.NewInternalErrorf("failed to update mining pools: %v", err)
	}
	return nil
}

// BlinkLED implements interfaces.Miner
func (p *PluginMiner) BlinkLED(ctx context.Context) error {
	if err := p.sdkDevice.BlinkLED(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to blink LED: %v", err)
	}
	return nil
}

// DownloadLogs implements interfaces.Miner
func (p *PluginMiner) DownloadLogs(ctx context.Context, batchLogUUID string) error {
	// Call SDK device's DownloadLogs method
	// The SDK returns (logData, moreData, error), but Fleet's interface doesn't use the return values
	// The logs are expected to be handled by the SDK/plugin implementation
	_, _, err := p.sdkDevice.DownloadLogs(ctx, nil, batchLogUUID)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to download logs: %v", err)
	}
	return nil
}

// FirmwareUpdate implements interfaces.Miner
func (p *PluginMiner) FirmwareUpdate(ctx context.Context) error {
	if err := p.sdkDevice.FirmwareUpdate(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to update firmware: %v", err)
	}
	return nil
}

// validateAndConvertPoolConfig validates and converts a mining pool config from Fleet format to SDK format.
// It ensures the priority value fits within int32 range before conversion.
func validateAndConvertPoolConfig(pool dto.MiningPool, poolName string) (sdk.MiningPoolConfig, error) {
	if pool.Priority > math.MaxInt32 {
		return sdk.MiningPoolConfig{}, fleeterror.NewInvalidArgumentErrorf(
			"%s pool priority %d exceeds int32 maximum", poolName, pool.Priority)
	}

	return sdk.MiningPoolConfig{
		Priority:   int32(pool.Priority), //nolint:gosec // G115: Priority validated above to fit in int32
		URL:        pool.URL,
		WorkerName: pool.Username,
	}, nil
}
