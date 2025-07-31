package proto

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api"

	"connectrpc.com/connect"
	"golang.org/x/sync/errgroup"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api/miner_data_apiconnect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

var _ interfaces.Miner = &ProtoMiner{}
var _ interfaces.MinerInfo = &ProtoMiner{}
var _ interfaces.MinerInfo = &ProtoMinerInfo{}

const minerViewPort = 80
const DownloadLogsLines uint32 = 10000

type ProtoMinerInfo struct {
	deviceIdentifier    miner.DeviceIdentifier
	minerAuthPrivateKey []byte
	connectionInfo      networking.ConnectionInfo
}

type ProtoMiner struct {
	minerInfo         *ProtoMinerInfo
	dataClient        miner_data_apiconnect.MinerDataApiClient
	commandClient     miner_command_apiconnect.MinerCommandApiClient
	systemClient      miner_system_apiconnect.MinerSystemApiClient
	commandAuthClient miner_system_apiconnect.MinerPairingApiClient
	filesService      *files.Service
	tokenService      *token.Service
	encryptService    *encrypt.Service
}

func NewProtoMiner(protoMinerInfo *ProtoMinerInfo, filesService *files.Service, tokenService *token.Service, encryptService *encrypt.Service) (*ProtoMiner, error) {
	// Create individual clients using the new CreateClient function
	dataClient, err := client.CreateClient(
		miner_data_apiconnect.NewMinerDataApiClient,
		protoMinerInfo.GetConnectionInfo(),
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create data client: %v", err)
	}

	commandClient, err := client.CreateClient(
		miner_command_apiconnect.NewMinerCommandApiClient,
		protoMinerInfo.GetConnectionInfo(),
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create command client: %v", err)
	}

	systemClient, err := client.CreateClient(
		miner_system_apiconnect.NewMinerSystemApiClient,
		protoMinerInfo.GetConnectionInfo(),
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create system client: %v", err)
	}

	commandAuthClient, err := client.CreateClient(
		miner_system_apiconnect.NewMinerPairingApiClient,
		protoMinerInfo.GetConnectionInfo(),
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create auth client: %v", err)
	}

	return &ProtoMiner{
		minerInfo:         protoMinerInfo,
		dataClient:        dataClient,
		commandClient:     commandClient,
		systemClient:      systemClient,
		commandAuthClient: commandAuthClient,
		filesService:      filesService,
		tokenService:      tokenService,
		encryptService:    encryptService,
	}, nil
}

func NewProtoMinerInfo(deviceIdentifier miner.DeviceIdentifier, ipAddress string, port uint16, scheme networking.Protocol, minerAuthPrivateKey []byte) (*ProtoMinerInfo, error) {
	connectionInfo, err := networking.NewConnectionInfo(ipAddress, fmt.Sprintf("%d", port), scheme)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create connection info: %v", err)
	}

	return &ProtoMinerInfo{
		deviceIdentifier:    deviceIdentifier,
		connectionInfo:      *connectionInfo,
		minerAuthPrivateKey: minerAuthPrivateKey,
	}, nil
}

func (p *ProtoMinerInfo) GetType() miner.Type {
	return miner.TypeProto
}

func (p *ProtoMiner) GetType() miner.Type {
	return p.minerInfo.GetType()
}

func (p *ProtoMinerInfo) GetID() miner.DeviceIdentifier {
	return p.deviceIdentifier
}

func (p *ProtoMiner) GetID() miner.DeviceIdentifier {
	return p.minerInfo.GetID()
}

func (p *ProtoMinerInfo) GetConnectionInfo() networking.ConnectionInfo {
	return p.connectionInfo
}

func (p *ProtoMiner) GetConnectionInfo() networking.ConnectionInfo {
	return p.minerInfo.GetConnectionInfo()
}

func (p *ProtoMinerInfo) GetWebViewURL() *url.URL {
	return networking.ConnectionInfo{
		Protocol:  p.connectionInfo.Protocol,
		IPAddress: p.connectionInfo.IPAddress,
		Port:      networking.Port(minerViewPort),
	}.GetURL()
}

func (p *ProtoMiner) GetWebViewURL() *url.URL {
	return p.minerInfo.GetWebViewURL()
}

func (p *ProtoMiner) Reboot(ctx context.Context) error {
	ctx, err := p.contextWithAuth(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting auth context: %v", err)
	}
	resp, err := p.systemClient.Reboot(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return err
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("reboot failed: %s", resp.Msg.Result)
	}

	return nil
}

func (p *ProtoMiner) getJWT() (string, error) {
	jwt, _, err := p.tokenService.GenerateMinerAuthJWT(p.minerInfo.GetID().String(), p.minerInfo.minerAuthPrivateKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error generating miner auth JWT: %v", err)
	}

	return jwt, nil
}

func (p *ProtoMiner) contextWithAuth(ctx context.Context) (context.Context, error) {
	jwt, err := p.getJWT()
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting jwt: %v", err)
	}

	return client.ContextWithAuth(ctx, secrets.NewText(jwt)), nil
}

func (p *ProtoMiner) StartMining(ctx context.Context) error {
	ctx, err := p.contextWithAuth(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting auth context: %v", err)
	}
	resp, err := p.commandClient.StartMining(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return err // Error mapping handled by interceptor
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("start mining failed: %s", resp.Msg.Message)
	}

	return nil
}

func (p *ProtoMiner) StopMining(ctx context.Context) error {
	ctx, err := p.contextWithAuth(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting auth context: %v", err)
	}
	resp, err := p.commandClient.StopMining(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return err // Error mapping handled by interceptor
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("stop mining failed: %s", resp.Msg.Message)
	}

	return nil
}

func mapCoolingModeType(pbMode pb.CoolingMode) (miner_data_api.CoolingMode, error) {
	switch pbMode {
	case pb.CoolingMode_COOLING_MODE_UNSPECIFIED:
		return miner_data_api.CoolingMode_COOLING_MODE_UNKNOWN, nil
	case pb.CoolingMode_COOLING_MODE_AIR_COOLED:
		return miner_data_api.CoolingMode_COOLING_MODE_AUTO, nil
	case pb.CoolingMode_COOLING_MODE_IMMERSION_COOLED:
		return miner_data_api.CoolingMode_COOLING_MODE_OFF, nil
	default:
		return 0, fleeterror.NewInternalErrorf("unsupported cooling mode type: %v", pbMode)
	}
}

func (p *ProtoMiner) SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error {
	ctx, err := p.contextWithAuth(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting auth context: %v", err)
	}

	protoMinerMode, err := mapCoolingModeType(payload.Mode)
	if err != nil {
		return fleeterror.NewInternalErrorf("error mapping cooling mode to proto miner type: %v", err)
	}
	resp, err := p.commandClient.SetCoolingMode(ctx, connect.NewRequest(&miner_command_api.CoolingModeRequest{Mode: protoMinerMode}))
	if err != nil {
		return err
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("set cooling mode failed: %s", resp.Msg.String())
	}

	return nil
}

func toMinerDataPool(payloadPool *dto.MiningPool) *miner_data_api.Pool {
	return &miner_data_api.Pool{
		Priority: payloadPool.Priority,
		Url:      payloadPool.URL,
		Username: payloadPool.Username,
		Password: payloadPool.Password,
	}
}

func getMinerDataPoolsToSet(payload dto.UpdateMiningPoolsPayload) []*miner_data_api.Pool {
	poolsToSet := make([]*miner_data_api.Pool, 0, 3)

	poolsToSet = append(poolsToSet, toMinerDataPool(&payload.DefaultPool))

	if payload.Backup1Pool != nil {
		poolsToSet = append(poolsToSet, toMinerDataPool(payload.Backup1Pool))
	}

	if payload.Backup2Pool != nil {
		poolsToSet = append(poolsToSet, toMinerDataPool(payload.Backup2Pool))
	}

	return poolsToSet
}

func toMinerCommandPoolsRequest(pld dto.UpdateMiningPoolsPayload) *miner_command_api.PoolsRequest {
	return &miner_command_api.PoolsRequest{Pools: getMinerDataPoolsToSet(pld)}
}

func (p *ProtoMiner) UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
	// TODO rewrite to a single setMiningPools call on miner once FW supports this (link the linear task here once created)
	ctx, err := p.contextWithAuth(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting auth context: %v", err)
	}

	poolsResp, err := p.dataClient.GetPools(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return err
	}

	if poolsResp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("error getting current pools set up: %s", poolsResp.Msg.String())
	}

	resp, err := p.commandClient.RemovePools(ctx, connect.NewRequest(&miner_command_api.PoolsRequest{Pools: poolsResp.Msg.Pools}))
	if err != nil {
		return err
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("remove mining pools failed: %s", resp.Msg.String())
	}

	resp, err = p.commandClient.AddPools(ctx, connect.NewRequest(toMinerCommandPoolsRequest(payload)))
	if err != nil {
		return err
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("add mining pools failed: %s", resp.Msg.String())
	}

	return nil
}

func (p *ProtoMiner) DownloadLogs(ctx context.Context, batchLogUUID string) error {
	ctx, err := p.contextWithAuth(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting auth context: %v", err)
	}

	lines := DownloadLogsLines
	downloadResp, err := p.systemClient.GetLogs(ctx, connect.NewRequest(&miner_system_api.GetLogsRequest{
		Lines:  &lines,
		Source: miner_system_api.LogSource_LOG_SOURCE_MINER_SW,
	}))
	if err != nil {
		return err
	}

	deviceIdentifier := p.GetID()
	logData := downloadResp.Msg.Content

	_, err = p.filesService.SaveLogs(batchLogUUID, &deviceIdentifier, logData)

	if err != nil {
		return fleeterror.NewInternalErrorf("error saving logs: %v", err)
	}

	return nil
}

func (p *ProtoMiner) SetAuthKey(ctx context.Context, key string) error {
	_, err := p.commandAuthClient.SetAuthKey(ctx, connect.NewRequest(&miner_system_api.SetAuthKeyRequest{PublicKey: key}))
	if err != nil {
		return fleeterror.NewInternalErrorf("error setting auth key: %v", err)
	}

	return nil
}

func (p *ProtoMiner) BlinkLED(ctx context.Context) error {
	ctx, err := p.contextWithAuth(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting auth context: %v", err)
	}
	resp, err := p.commandClient.PlayLocateSequence(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return err // Error mapping handled by interceptor
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("blink LEDs failed: %s", resp.Msg.Result)
	}

	return nil
}

func (p *ProtoMiner) FirmwareUpdate(ctx context.Context) error {
	_, err := p.systemClient.Update(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return fleeterror.NewInternalErrorf("error on install command: %v", err)
	}

	return nil
}

func (p *ProtoMiner) GetTelemetry(ctx context.Context, after time.Time) ([]telemetryModels.Telemetry, error) {
	// Create telemetry mapper
	mapper := NewTelemetryMapper(p.GetID())

	// Generate time series requests for all data types
	requests := mapper.MapToTimeSeriesRequests(after)

	// Execute requests concurrently
	responses := make([]*miner_data_api.TimeSeriesDataResponse, len(requests))
	errGroup, ctx := errgroup.WithContext(ctx)

	// Add auth token to context
	ctx, err := p.contextWithAuth(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting auth context: %v", err)
	}

	for i, req := range requests {
		goI, goReq := i, req // Capture loop variables
		errGroup.Go(func() error {
			resp, err := p.dataClient.GetTimeSeriesData(ctx, connect.NewRequest(goReq))
			if err != nil {
				return err
			}
			responses[goI] = resp.Msg
			return nil
		})
	}

	if err := errGroup.Wait(); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to fetch telemetry data: %v", err)
	}

	// Convert responses to telemetry models
	telemetryData := mapper.MapToTelemetryModels(responses)

	return telemetryData, nil
}
