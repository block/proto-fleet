package proto

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
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

func (d *Discoverer) Discover(ctx context.Context, ipAddress string, port string) (*minerdiscovery.DiscoveredDevice, error) {
	if port != requiredPort {
		return nil, minerdiscovery.MinerNotFoundFleetError
	}

	protocol := networking.ProtocolHTTPS
	pairingInfo, err := getPairingInfo(ctx, ipAddress, port, protocol)
	if err != nil {
		protocol = networking.ProtocolHTTP
		pairingInfo, err = getPairingInfo(ctx, ipAddress, port, protocol)
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
	return &minerdiscovery.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:    ipAddress,
			Port:         port,
			UrlScheme:    protocol.String(),
			MacAddress:   pairingInfo.Msg.Mac,
			SerialNumber: pairingInfo.Msg.CbSn,
			// TODO(DASH-331) Fetch model and manufacturer from miner
			Model:        "Rig",
			Manufacturer: "Proto",
			Type:         d.GetMinerType().String(),
		},
	}, nil
}

func getPairingInfo(ctx context.Context, ipAddress string, port string, protocol networking.Protocol) (*connect.Response[miner_system_api.GetPairingInfoResponse], error) {
	connectionInfo, err := networking.NewConnectionInfo(ipAddress, port, protocol)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create connection info: %v", err)
	}

	minerClient, err := client.CreateClient(
		miner_system_apiconnect.NewMinerPairingApiClient,
		*connectionInfo,
	)
	if err != nil {
		return nil, err
	}

	pairingInfo, err := minerClient.GetPairingInfo(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, err
	}
	return pairingInfo, nil
}

// GetMinerType returns the type of miner this discoverer handles
func (d *Discoverer) GetMinerType() miner.Type {
	return miner.TypeProto
}
