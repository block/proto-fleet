package interfaces

import (
	"context"
	"database/sql"
	"github.com/btc-mining/proto-fleet/server/internal/domain/commandtype"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

type CommandFunc func(ctx context.Context) error

func GetMinerCommandFunc(t commandtype.Type, miner Miner) CommandFunc {
	switch t {
	case commandtype.StartMining:
		return miner.StartMining
	case commandtype.StopMining:
		return miner.StopMining
	default:
		return nil
	}
}

//go:generate mockgen -source=miner.go -destination=mocks/mock_miner.go -package=mocks Miner
type Miner interface {
	// Basic identification
	GetType() models.Type
	GetID() int64
	GetConnectionInfo() networking.ConnectionInfo

	// Mining operations
	StartMining(ctx context.Context) error
	StopMining(ctx context.Context) error

	// System operations
	GetPairingInfo(ctx context.Context, conn *sql.DB) (*models.PairingInfo, error)

	// Telemetry operations
	GetTelemetry(ctx context.Context, after time.Time) ([]telemetryModels.Telemetry, error)
}
