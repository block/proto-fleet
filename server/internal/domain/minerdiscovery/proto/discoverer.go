package proto

import (
	"context"
	"net"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	protoMinerClient "github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
)

type Discoverer struct {
	minerClient *protoMinerClient.Service
}

func NewDiscoverer(minerClient *protoMinerClient.Service) *Discoverer {
	return &Discoverer{
		minerClient: minerClient,
	}
}

func (d *Discoverer) Discover(ctx context.Context, ipAddress string, port string) (*pb.Device, error) {
	url := net.JoinHostPort(ipAddress, port)

	pairingInfo, err := d.minerClient.GetPairingInfo(ctx, url)
	if err != nil {
		return nil, err
	}

	if len(pairingInfo.Msg.CbSn) == 0 {
		return nil, fleeterror.NewInternalErrorf("miner at '%s' does not have a serial number which is required for pairing", url)
	}

	// Create device information
	return &pb.Device{
		IpAddress:    ipAddress,
		Port:         port,
		MacAddress:   pairingInfo.Msg.Mac,
		SerialNumber: pairingInfo.Msg.CbSn,
		// TODO(DASH-331) Fetch model and manufacturer from miner
		Model:        "Proto Rig",
		Manufacturer: "Block, Inc",
		DiscoveredAt: time.Now().Unix(),
	}, nil
}

// GetMinerType returns the type of miner this discoverer handles
func (d *Discoverer) GetMinerType() miner.Type {
	return miner.TypeProto
}
