package interfaces

import (
	"context"
	"net/url"

	diagnosticsModels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

//go:generate go run go.uber.org/mock/mockgen -source=miner.go -destination=mocks/mock_miner.go -package=mocks Miner
type MinerInfo interface {
	GetDriverName() string
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

	// Curtailment operations.
	//
	// Curtail reduces device power output to the requested level. v1 honors
	// CurtailLevelFull only; higher levels are reserved for v4 plugin-specific
	// support and return ErrCurtailCapabilityNotSupported until then.
	// Capability gating in the curtailment selector ensures Curtail is only
	// dispatched to miners that report CapabilityCurtail.
	Curtail(ctx context.Context, level sdk.CurtailLevel) error
	Uncurtail(ctx context.Context) error

	// Configuration operations
	SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error
	GetCoolingMode(ctx context.Context) (commonpb.CoolingMode, error)
	SetPowerTarget(ctx context.Context, payload dto.PowerTargetPayload) error
	UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error
	UpdateMinerPassword(ctx context.Context, payload dto.UpdateMinerPasswordPayload) error
	BlinkLED(ctx context.Context) error

	DownloadLogs(ctx context.Context, batchLogUUID string) error

	FirmwareUpdate(ctx context.Context, firmware sdk.FirmwareFile) error

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

// FirmwareUpdateStatusProvider is an optional interface that miners can implement
// to report firmware installation progress. Used by the execution service to poll
// install status after uploading firmware to a device.
type FirmwareUpdateStatusProvider interface {
	GetFirmwareUpdateStatus(ctx context.Context) (*sdk.FirmwareUpdateStatus, error)
}

// MinerConfiguredPool represents a pool currently configured on a miner device
type MinerConfiguredPool struct {
	Priority int32
	URL      string
	Username string
}
