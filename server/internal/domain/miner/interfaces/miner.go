package interfaces

import (
	"context"
	"net/url"

	diagnosticsModels "github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

//go:generate mockgen -source=miner.go -destination=mocks/mock_miner.go -package=mocks Miner
type MinerInfo interface {
	GetType() models.Type
	GetID() models.DeviceIdentifier
	GetOrgID() int64
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
	GetCoolingMode(ctx context.Context) (commonpb.CoolingMode, error)
	SetPowerTarget(ctx context.Context, payload dto.PowerTargetPayload) error
	UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error
	BlinkLED(ctx context.Context) error

	DownloadLogs(ctx context.Context, batchLogUUID string) error

	FirmwareUpdate(ctx context.Context) error

	// Unpair clears device credentials and unregisters from fleet
	Unpair(ctx context.Context) error

	// Telemetry operations
	GetDeviceMetrics(ctx context.Context) (modelsV2.DeviceMetrics, error)

	// GetDeviceStatus
	GetDeviceStatus(ctx context.Context) (models.MinerStatus, error)

	// Diagnostics operations
	GetErrors(ctx context.Context) (diagnosticsModels.DeviceErrors, error)

	// Pool configuration
	GetMiningPools(ctx context.Context) ([]MinerConfiguredPool, error)
}

// MinerConfiguredPool represents a pool currently configured on a miner device
type MinerConfiguredPool struct {
	Priority int32
	URL      string
	Username string
}
