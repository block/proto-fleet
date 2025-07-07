package pairing

import (
	"context"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

// pairing statuses
const (
	StatusPaired   = "PAIRED"
	StatusUnpaired = "UNPAIRED"
)

type Config struct {
	SecretKey string `help:"Secret key for signing the pairing tokens" env:"SECRET_KEY" required:""`
}

type Pairer interface {
	// PairDevice handles the entire pairing process including saving the device to the database
	PairDevice(ctx context.Context, device *minerdiscovery.DiscoveredDevice, credentials *pb.Credentials) error
	GetMinerType() models.Type
}
