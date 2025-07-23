package antminer

import (
	"context"
	"strings"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	antminerRPC "github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

// discovery constants
const (
	versionTypePrefix = "Antminer"
	requiredPort      = "4028"
	manufacturer      = "Bitmain"
)

var _ minerdiscovery.Discoverer = &Discoverer{}

type Discoverer struct {
	minerRPCClient antminerRPC.RPCClient
}

func NewDiscoverer(rpcClient antminerRPC.RPCClient) *Discoverer {
	return &Discoverer{
		minerRPCClient: rpcClient,
	}
}

func (d *Discoverer) Discover(ctx context.Context, ipAddress string, port string) (*minerdiscovery.DiscoveredDevice, error) {
	if port != requiredPort {
		return nil, minerdiscovery.MinerNotFoundFleetError
	}

	connInfo, err := networking.NewConnectionInfo(ipAddress, port, networking.ProtocolTCP)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create connection info: %v", err)
	}

	result, err := d.minerRPCClient.GetVersion(ctx, connInfo)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get version info from %s:%s: %v", ipAddress, port, err)
	}

	if len(result.Version) == 0 {
		return nil, fleeterror.NewInternalErrorf("empty version info from %s:%s", ipAddress, port)
	}

	versionInfo := result.Version[0]
	if !strings.HasPrefix(versionInfo.Type, versionTypePrefix) {
		return nil, minerdiscovery.MinerNotFoundFleetError
	}

	model := versionInfo.Miner
	if model == "" {
		model = "Unknown Antminer"
	}

	// Create device information
	return &minerdiscovery.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:    ipAddress,
			Port:         port,
			UrlScheme:    networking.ProtocolHTTP.String(),
			Model:        model,
			Manufacturer: manufacturer,
		},
		Type: d.GetMinerType().String(),
	}, nil
}

func (d *Discoverer) GetMinerType() models.Type {
	return models.TypeAntminer
}
