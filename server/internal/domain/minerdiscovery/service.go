package minerdiscovery

import (
	"context"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
)

var MinerNotFoundFleetError = fleeterror.NewPlainError("miner not found at the specified address and port", connect.CodeNotFound).WithCallerStackTrace()

// Discoverer defines the interface for discovering mining devices on the network.
type Discoverer interface {
	Discover(ctx context.Context, ipAddress string, port string) (*models.DiscoveredDevice, error)
}
