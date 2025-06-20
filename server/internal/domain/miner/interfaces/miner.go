package interfaces

import (
	"context"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

//go:generate mockgen -source=miner.go -destination=mocks/mock_miner.go -package=mocks Miner
type Miner interface {
	// Basic identification
	GetType() models.Type
	GetIdentifier() string
	GetConnectionInfo() networking.ConnectionInfo

	// Mining operations
	StartMining(ctx context.Context) error
	StopMining(ctx context.Context) error

	// System operations
	GetPairingInfo(ctx context.Context) (*models.PairingInfo, error)

	// Telemetry operations
	GetTelemetry(ctx context.Context, after time.Time) ([]telemetryModels.Telemetry, error)
}
