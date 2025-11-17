package antminer

import (
	"context"
	"net/url"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

var _ interfaces.Miner = &Antminer{}
var _ interfaces.MinerInfo = &Antminer{}
var _ interfaces.MinerInfo = &AntminerInfo{}

const (
	// antminerWebPort is the HTTP port for the Antminer web interface and API
	antminerWebPort networking.Port = 80
	// antminerRPCPort is the TCP port for the Antminer CGMiner RPC API used for status checks and telemetry
	antminerRPCPort networking.Port = 4028
)

type AntminerInfo struct {
	deviceIdentifier models.DeviceIdentifier
	serialNumber     string
	connectionInfo   networking.ConnectionInfo
}

type Antminer struct {
	interfaces.MinerInfo
	username  string
	password  secrets.Text
	webClient web.WebAPIClient
	rpcClient rpc.RPCClient
}

func NewAntminer(antminerInfo interfaces.MinerInfo, username string, password secrets.Text, webClient web.WebAPIClient, rpcClient rpc.RPCClient) *Antminer {
	return &Antminer{
		MinerInfo: antminerInfo,
		username:  username,
		password:  password,
		webClient: webClient,
		rpcClient: rpcClient,
	}
}

func NewAntminerInfo(deviceIdentifier models.DeviceIdentifier, ipAddress string, port uint16, serialNumber string) *AntminerInfo {
	return &AntminerInfo{
		deviceIdentifier: deviceIdentifier,
		connectionInfo: networking.ConnectionInfo{
			IPAddress: networking.IPAddress(ipAddress),
			Port:      networking.Port(port),
		},
		serialNumber: serialNumber,
	}
}

func (a *AntminerInfo) GetType() models.Type {
	return models.TypeAntminer
}

func (a *AntminerInfo) GetID() models.DeviceIdentifier {
	return a.deviceIdentifier
}

func (a *AntminerInfo) GetSerialNumber() string {
	return a.serialNumber
}

func (a *AntminerInfo) GetConnectionInfo() networking.ConnectionInfo {
	return a.connectionInfo
}

func (a *AntminerInfo) GetWebViewURL() *url.URL {
	return networking.ConnectionInfo{
		Protocol:  networking.ProtocolHTTP,
		IPAddress: a.connectionInfo.IPAddress,
	}.GetURL()
}

func (a *Antminer) Reboot(ctx context.Context) error {
	return a.webClient.Reboot(ctx, a.getWebConnectionInfo())
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

func (a *Antminer) DownloadLogs(_ context.Context, _ string) error {
	return fleeterror.NewInternalErrorf("Not implemented!") // TODO https://linear.app/squareup/issue/DASH-540
}

func (a *Antminer) BlinkLED(_ context.Context) error {
	return fleeterror.NewInternalErrorf("Not implemented!") // TODO https://linear.app/squareup/issue/DASH-535
}

func (a *Antminer) FirmwareUpdate(_ context.Context) error {
	return fleeterror.NewInternalErrorf("Not implemented!") // TODO https://linear.app/squareup/issue/DASH-575
}

func (a *Antminer) getWebConnectionInfo() *web.AntminerConnectionInfo {
	return &web.AntminerConnectionInfo{
		ConnectionInfo: networking.ConnectionInfo{
			IPAddress: a.GetConnectionInfo().IPAddress,
			Port:      a.GetConnectionInfo().Port,
			Protocol:  networking.ProtocolHTTP,
		},
		Username: a.username,
		Password: a.password,
	}
}

func (a *Antminer) getRPCConnectionInfo() *networking.ConnectionInfo {
	return &networking.ConnectionInfo{
		IPAddress: a.GetConnectionInfo().IPAddress,
		Port:      antminerRPCPort, // Use RPC port (4028) instead of stored web port (80)
		Protocol:  networking.ProtocolTCP,
	}
}

func (a *Antminer) GetTelemetry(ctx context.Context, _ time.Time) ([]telemetryModels.Telemetry, error) {
	telemetryMapper := NewTelemetryMapper(a.GetID())

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

func (a *Antminer) GetDeviceMetrics(ctx context.Context) (modelsV2.DeviceMetrics, error) {
	return modelsV2.DeviceMetrics{}, fleeterror.NewInternalErrorf("GetDeviceMetrics is not yet implemented for Antminer")
}

func toAntminerPool(payloadPool *dto.MiningPool) web.Pool {
	return web.Pool{
		URL:      payloadPool.URL,
		Username: payloadPool.Username,
		Password: payloadPool.Password,
	}
}

func (a *Antminer) GetDeviceStatus(ctx context.Context) (models.MinerStatus, error) {
	// Try to get summary from the device to check if it's reachable
	_, err := a.rpcClient.GetSummary(ctx, a.getRPCConnectionInfo())
	if err != nil {
		// If we can't reach the device, it's offline (not an error condition)
		return models.MinerStatusOffline, nil //nolint:nilerr // Connection error means offline status, not a method error
	}

	// Device is reachable, consider it active
	return models.MinerStatusActive, nil
}
