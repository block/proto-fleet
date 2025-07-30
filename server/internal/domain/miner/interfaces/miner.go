package interfaces

import (
	"context"
	"net/url"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

//go:generate mockgen -source=miner.go -destination=mocks/mock_miner.go -package=mocks Miner
type MinerInfo interface {
	GetType() models.Type
	GetID() models.DeviceIdentifier
	GetConnectionInfo() networking.ConnectionInfo
	GetWebViewURL() *url.URL
}
type Miner interface {
	MinerInfo

	Reboot(ctx context.Context) error

	// Mining operations
	StartMining(ctx context.Context) error
	StopMining(ctx context.Context) error

	// Configuration operations
	SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error
	UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error
	BlinkLED(ctx context.Context) error

	DownloadLogs(ctx context.Context, batchLogUUID string) error

	// Telemetry operations
	GetTelemetry(ctx context.Context, after time.Time) ([]telemetryModels.Telemetry, error)
}
