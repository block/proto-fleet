package antminer

import (
	"context"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

var _ interfaces.Miner = &Antminer{}

type Antminer struct {
	deviceID       int64
	connectionInfo networking.ConnectionInfo
	rpcPort        string
	username       string
	password       secrets.Text
	webClient      web.WebAPIClient
	rpcClient      rpc.RPCClient
}

func NewAntminer(deviceID int64, ipAddress string, port uint16, rpcPort string, username string, password secrets.Text, webClient web.WebAPIClient, rpcClient rpc.RPCClient) *Antminer {
	return &Antminer{
		deviceID: deviceID,
		connectionInfo: networking.ConnectionInfo{
			IPAddress: networking.IPAddress(ipAddress),
			Port:      networking.Port(port),
		},
		rpcPort:   rpcPort,
		username:  username,
		password:  password,
		webClient: webClient,
		rpcClient: rpcClient,
	}
}

func (a *Antminer) GetType() models.Type {
	return models.TypeAntminer
}

func (a *Antminer) GetID() int64 {
	return a.deviceID
}

func (a *Antminer) GetConnectionInfo() networking.ConnectionInfo {
	return a.connectionInfo
}

func (a *Antminer) StartMining(ctx context.Context) error {
	return a.webClient.SetMinerConfig(ctx, a.getWebConnectionInfo(), &web.MinerConfig{
		BitmainWorkMode: web.BitmainWorkModeStart,
	})
}

func (a *Antminer) StopMining(ctx context.Context) error {
	return a.webClient.SetMinerConfig(ctx, a.getWebConnectionInfo(), &web.MinerConfig{
		BitmainWorkMode: web.BitmainWorkModeSleep,
	})
}

func (a *Antminer) SetCoolingMode(_ context.Context, _ dto.CoolingModePayload) error {
	return fleeterror.NewInternalErrorf("Not implemented!") // TODO https://linear.app/squareup/issue/DASH-513
}

func (a *Antminer) UpdateMiningPools(ctx context.Context, poolsPayload dto.UpdateMiningPoolsPayload) error {
	pools := make([]web.Pool, 0, 3)

	pools = append(pools, toAntminerPool(&poolsPayload.DefaultPool))

	if poolsPayload.Backup1Pool != nil {
		pools = append(pools, toAntminerPool(poolsPayload.Backup1Pool))
	}

	if poolsPayload.Backup2Pool != nil {
		pools = append(pools, toAntminerPool(poolsPayload.Backup2Pool))
	}

	return a.webClient.SetMinerConfig(ctx, a.getWebConnectionInfo(), &web.MinerConfig{
		Pools: pools,
	})
}

func (a *Antminer) getWebConnectionInfo() *web.AntminerConnectionInfo {
	return &web.AntminerConnectionInfo{
		ConnectionInfo: a.connectionInfo,
		Username:       a.username,
		Password:       a.password,
	}
}

func (a *Antminer) getRPCConnectionInfo() *networking.ConnectionInfo {
	return &a.connectionInfo
}

//nolint:revive // GetTelemetry will be implemented in the future
func (a *Antminer) GetTelemetry(ctx context.Context, after time.Time) ([]telemetryModels.Telemetry, error) {
	return []telemetryModels.Telemetry{}, fleeterror.NewInternalErrorf("GetTelemetry not implemented for Antminer")
}

func toAntminerPool(payloadPool *dto.MiningPool) web.Pool {
	return web.Pool{
		URL:      payloadPool.URL,
		Username: payloadPool.Username,
		Password: payloadPool.Password,
	}
}
