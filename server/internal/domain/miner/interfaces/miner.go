package interfaces

import (
	"context"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

//go:generate mockgen -source=miner.go -destination=mocks/mock_miner.go -package=mocks Miner
type Miner interface {
	// Basic identification
	GetType() models.Type
	GetID() int64
	GetConnectionInfo() networking.ConnectionInfo

	// Mining operations
	StartMining(ctx context.Context) error
	StopMining(ctx context.Context) error

	// Configuration operations
	SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error
	UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error

	// Telemetry operations
	GetTelemetry(ctx context.Context, after time.Time) ([]telemetryModels.Telemetry, error)
}
