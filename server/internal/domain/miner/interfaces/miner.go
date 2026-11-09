package interfaces

import (
	"context"
	"fmt"
	"net/url"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	diagnosticsModels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// MinerWriteGate lets server factories admit and track an actual SDK call or
// node send. Invocation spans the operation and returns its result.
type MinerWriteGate func(context.Context, commandtype.Type, func(context.Context) error) error

//go:generate go run go.uber.org/mock/mockgen -source=miner.go -destination=mocks/mock_miner.go -package=mocks Miner
type MinerInfo interface {
	GetDriverName() string
	GetID() models.DeviceIdentifier
	GetOrgID() int64
	GetSiteID() int64
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

	// Curtailment requests must match the device's advertised curtail capabilities.
	Curtail(ctx context.Context, req sdk.CurtailRequest) error
	Uncurtail(ctx context.Context, req sdk.UncurtailRequest) error

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

// PoolClearVerifier proves stored and runtime emptiness at the device boundary.
// Remote adapters use it to verify every slot without transporting credentials.
// Missing/partial readback must return an error, never a successful empty result.
type PoolClearVerifier interface {
	VerifyPoolsCleared(ctx context.Context) (bool, error)
}

// StoredMiningPoolReader reads complete private firmware configuration. It is
// deliberately separate from the runtime GetMiningPools view.
type StoredMiningPoolReader interface {
	GetStoredMiningPools(ctx context.Context) ([]MinerStoredPool, error)
}

// FirmwareUpdateStatusProvider is an optional interface that miners can implement
// to report firmware installation progress. Used by the execution service to poll
// install status after uploading firmware to a device.
type FirmwareUpdateStatusProvider interface {
	GetFirmwareUpdateStatus(ctx context.Context) (*sdk.FirmwareUpdateStatus, error)
}

// MinerPasswordCredentialUpdater returns node-encrypted credentials after a miner password update.
type MinerPasswordCredentialUpdater interface {
	UpdateMinerPasswordWithCredentials(ctx context.Context, payload dto.UpdateMinerPasswordPayload) (*gatewaypb.EncryptedCredentials, error)
}

// MinerCurtailmentConfigurator is implemented by miners that can replace a
// rig-local curtailment fallback configuration.
type MinerCurtailmentConfigurator interface {
	ApplyCurtailmentConfig(ctx context.Context, payload dto.ApplyCurtailmentConfigPayload) error
}

// MinerConfiguredPool represents a pool currently configured on a miner device
type MinerConfiguredPool struct {
	Priority int32
	URL      string
	Username string
}

// MinerStoredPool is a private observation of a stored firmware slot.
// Password is nil when unavailable or masked; a pointer to "" proves empty.
// Never infer this value from desired configuration or a write acknowledgement.
type MinerStoredPool struct {
	Priority int32
	URL      string
	Username string
	Password *string `json:"-"`
}

func (p MinerStoredPool) String() string {
	return fmt.Sprintf("MinerStoredPool{Priority:%d URL:%q Username:%q Password:[redacted]}", p.Priority, p.URL, p.Username)
}

func (p MinerStoredPool) GoString() string { return p.String() }
