package interfaces

import (
	"context"
	"net/url"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

//go:generate mockgen -source=miner.go -destination=mocks/mock_miner.go -package=mocks Miner
type MinerInfo interface {
	GetType() models.Type
	GetID() models.DeviceIdentifier
	GetSerialNumber() string
	GetConnectionInfo() networking.ConnectionInfo
	GetWebViewURL() *url.URL
}

//nolint:interfacebloat // Miner defines the interface for miner operations. We are okay with bloat at this time.
type Miner interface {
	MinerInfo

	Reboot(ctx context.Context) error

	// Mining operations
	StartMining(ctx context.Context) error
	StopMining(ctx context.Context) error

	// Configuration operations
	SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error
	SetPowerTarget(ctx context.Context, payload dto.PowerTargetPayload) error
	UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error
	BlinkLED(ctx context.Context) error

	DownloadLogs(ctx context.Context, batchLogUUID string) error

	FirmwareUpdate(ctx context.Context) error

	// Unpair clears device credentials and unregisters from fleet
	Unpair(ctx context.Context) error

	// Telemetry operations
	GetTelemetry(ctx context.Context, after time.Time) ([]telemetryModels.Telemetry, error)
	GetDeviceMetrics(ctx context.Context) (modelsV2.DeviceMetrics, error)

	// GetDeviceStatus
	GetDeviceStatus(ctx context.Context) (models.MinerStatus, error)
}
