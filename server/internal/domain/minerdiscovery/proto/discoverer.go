package proto

import (
	"context"
	"net"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
)

const requiredPort = "2121"

var _ minerdiscovery.Discoverer = &Discoverer{}

type Discoverer struct{}

func NewDiscoverer() *Discoverer {
	return &Discoverer{}
}

func (d *Discoverer) Discover(ctx context.Context, ipAddress string, port string) (*minerdiscovery.DiscoveredDevice, error) {
	if port != requiredPort {
		return nil, minerdiscovery.MinerNotFoundFleetError
	}

	url := net.JoinHostPort(ipAddress, port)

	minerClient, err := client.CreateClient(
		miner_system_apiconnect.NewMinerSystemApiClient,
		url,
	)
	if err != nil {
		return nil, err
	}

	pairingInfo, err := minerClient.GetPairingInfo(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, err
	}

	if len(pairingInfo.Msg.CbSn) == 0 {
		return nil, fleeterror.NewInternalErrorf("miner at '%s' does not have a serial number which is required for pairing", url)
	}

	if len(pairingInfo.Msg.Mac) == 0 {
		return nil, fleeterror.NewInternalErrorf("miner at '%s' does not have a mac address which is required for pairing", url)
	}

	// Create device information
	return &minerdiscovery.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:    ipAddress,
			Port:         port,
			MacAddress:   pairingInfo.Msg.Mac,
			SerialNumber: pairingInfo.Msg.CbSn,
			// TODO(DASH-331) Fetch model and manufacturer from miner
			Model:        "Proto Rig",
			Manufacturer: "Block, Inc",
		},
		Type: d.GetMinerType().String(),
	}, nil
}

// GetMinerType returns the type of miner this discoverer handles
func (d *Discoverer) GetMinerType() miner.Type {
	return miner.TypeProto
}
