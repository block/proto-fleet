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
	deviceIdentifier models.DeviceIdentifier
	connectionInfo   networking.ConnectionInfo
	username         string
	password         secrets.Text
	webClient        web.WebAPIClient
	rpcClient        rpc.RPCClient
}

func NewAntminer(deviceIdentifier models.DeviceIdentifier, ipAddress string, port uint16, username string, password secrets.Text, webClient web.WebAPIClient, rpcClient rpc.RPCClient) *Antminer {
	return &Antminer{
		deviceIdentifier: deviceIdentifier,
		connectionInfo: networking.ConnectionInfo{
			IPAddress: networking.IPAddress(ipAddress),
			Port:      networking.Port(port),
		},
		username:  username,
		password:  password,
		webClient: webClient,
		rpcClient: rpcClient,
	}
}

func (a *Antminer) GetType() models.Type {
	return models.TypeAntminer
}

func (a *Antminer) GetID() models.DeviceIdentifier {
	return a.deviceIdentifier
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
	// While we can control the fan speed, we can't turn off the fans completely.
	return fleeterror.NewInternalErrorf("Cooling mode control is not supported for Antminer devices")
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
		ConnectionInfo: networking.ConnectionInfo{
			IPAddress: a.connectionInfo.IPAddress,
			Port:      a.connectionInfo.Port,
			Protocol:  networking.ProtocolHTTP,
		},
		Username: a.username,
		Password: a.password,
	}
}

func (a *Antminer) getRPCConnectionInfo() *networking.ConnectionInfo {
	return &networking.ConnectionInfo{
		IPAddress: a.connectionInfo.IPAddress,
		Port:      a.connectionInfo.Port,
		Protocol:  networking.ProtocolTCP,
	}
}

func (a *Antminer) GetTelemetry(ctx context.Context, _ time.Time) ([]telemetryModels.Telemetry, error) {
	telemetryMapper := NewTelemetryMapper(a.deviceIdentifier)

	summary, err := a.rpcClient.GetSummary(ctx, a.getRPCConnectionInfo())
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get summary from Antminer: %v", err)
	}

	devs, err := a.rpcClient.GetDevs(ctx, a.getRPCConnectionInfo())
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get device info from Antminer: %v", err)
	}

	telemetry := telemetryMapper.ToTelemetry(summary, devs, time.Now())

	return telemetry, nil
}

func toAntminerPool(payloadPool *dto.MiningPool) web.Pool {
	return web.Pool{
		URL:      payloadPool.URL,
		Username: payloadPool.Username,
		Password: payloadPool.Password,
	}
}
