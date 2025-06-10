package proto

import (
	"context"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

type ProtoMiner struct {
	deviceID       string
	connectionInfo networking.ConnectionInfo
	authToken      string
	minerClient    *client.Service
}

func NewProtoMiner(deviceID string, ipAddress string, port uint16, minerClient *client.Service, authToken string) *ProtoMiner {
	return &ProtoMiner{
		deviceID: deviceID,
		connectionInfo: networking.ConnectionInfo{
			IPAddress: networking.IPAddress(ipAddress),
			Port:      networking.Port(port),
			Protocol:  networking.ProtocolHTTPS, // client will try both HTTP and HTTPS
		},
		minerClient: minerClient,
		authToken:   authToken,
	}
}

func (p *ProtoMiner) GetType() miner.Type {
	return miner.TypeProto
}

func (p *ProtoMiner) GetIdentifier() string {
	return p.deviceID
}

func (p *ProtoMiner) GetConnectionInfo() networking.ConnectionInfo {
	return p.connectionInfo
}

func (p *ProtoMiner) StartMining(ctx context.Context) error {
	resp, err := p.minerClient.StartMining(ctx, p.getMinerConnectionInfo())
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to start mining: %v", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("failed to start mining: %s", resp.Msg.Message)
	}

	return nil
}

func (p *ProtoMiner) StopMining(ctx context.Context) error {
	resp, err := p.minerClient.StopMining(ctx, p.getMinerConnectionInfo())
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to stop mining: %v", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("failed to stop mining: %s", resp.Msg.Message)
	}

	return nil
}

func (p *ProtoMiner) GetPairingInfo(ctx context.Context) (*miner.PairingInfo, error) {
	resp, err := p.minerClient.GetPairingInfo(ctx, p.connectionInfo.GetHostPort().String())
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get pairing info: %v", err)
	}

	return &miner.PairingInfo{
		DeviceID:     p.deviceID,
		SerialNumber: resp.Msg.CbSn,
		MacAddress:   resp.Msg.Mac,
		// TODO(DASH-331) Fetch model and manufacturer from miner
		Model:        "Proto Rig",
		Manufacturer: "Block, Inc",
	}, nil
}

func (p *ProtoMiner) getMinerConnectionInfo() *client.MinerConnectionInfo {
	clientConnectionInfo := &client.MinerConnectionInfo{
		URL:       p.connectionInfo.GetHostPort(),
		AuthToken: p.authToken,
	}
	return clientConnectionInfo
}
