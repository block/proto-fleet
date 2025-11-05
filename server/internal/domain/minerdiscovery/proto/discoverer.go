package proto

import (
	"context"

	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

const requiredPort = "2121"

var _ minerdiscovery.Discoverer = &Discoverer{}

type Discoverer struct{}

func NewDiscoverer() *Discoverer {
	return &Discoverer{}
}

func (d *Discoverer) Discover(ctx context.Context, ipAddress string, port string) (*discoverymodels.DiscoveredDevice, error) {
	if port != requiredPort {
		return nil, minerdiscovery.MinerNotFoundFleetError
	}

	protocol := networking.ProtocolHTTPS
	pairingInfo, err := client.GetPairingInfo(ctx, ipAddress, port, protocol)
	if err != nil {
		protocol = networking.ProtocolHTTP
		pairingInfo, err = client.GetPairingInfo(ctx, ipAddress, port, protocol)
	}
	if err != nil {
		return nil, err
	}

	if len(pairingInfo.Msg.CbSn) == 0 {
		return nil, fleeterror.NewInternalErrorf("miner at '%s' does not have a serial number which is required for pairing", ipAddress)
	}

	if len(pairingInfo.Msg.Mac) == 0 {
		return nil, fleeterror.NewInternalErrorf("miner at '%s' does not have a mac address which is required for pairing", ipAddress)
	}

	// Create device information
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:    ipAddress,
			Port:         port,
			UrlScheme:    protocol.String(),
			SerialNumber: pairingInfo.Msg.CbSn,
			MacAddress:   pairingInfo.Msg.Mac,
			// TODO(DASH-331) Fetch model and manufacturer from miner
			Model:        "Rig",
			Manufacturer: "Proto",
			Type:         d.GetMinerType().String(),
		},
	}, nil
}

// GetMinerType returns the type of miner this discoverer handles
func (d *Discoverer) GetMinerType() miner.Type {
	return miner.TypeProto
}
