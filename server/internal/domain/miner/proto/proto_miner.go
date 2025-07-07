package proto

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/sync/errgroup"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api/miner_data_apiconnect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

var _ interfaces.Miner = &ProtoMiner{}

type ProtoMiner struct {
	deviceID       int64
	connectionInfo networking.ConnectionInfo
	authToken      *secrets.Text
	dataClient     miner_data_apiconnect.MinerDataApiClient
	commandClient  miner_command_apiconnect.MinerCommandApiClient
	systemClient   miner_system_apiconnect.MinerSystemApiClient
}

func NewProtoMiner(deviceID int64, ipAddress string, port uint16, authToken secrets.Text) (*ProtoMiner, error) {
	connectionInfo, err := networking.NewConnectionInfo(ipAddress, fmt.Sprintf("%d", port), networking.ProtocolHTTPS)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create connection info: %v", err)
	}

	// Create individual clients using the new CreateClient function
	dataClient, err := client.CreateClient(
		miner_data_apiconnect.NewMinerDataApiClient,
		*connectionInfo,
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create data client: %v", err)
	}

	commandClient, err := client.CreateClient(
		miner_command_apiconnect.NewMinerCommandApiClient,
		*connectionInfo,
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create command client: %v", err)
	}

	systemClient, err := client.CreateClient(
		miner_system_apiconnect.NewMinerSystemApiClient,
		*connectionInfo,
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create system client: %v", err)
	}

	return &ProtoMiner{
		deviceID:      deviceID,
		authToken:     &authToken,
		dataClient:    dataClient,
		commandClient: commandClient,
		systemClient:  systemClient,
	}, nil
}

func (p *ProtoMiner) GetType() miner.Type {
	return miner.TypeProto
}

func (p *ProtoMiner) GetID() int64 {
	return p.deviceID
}

func (p *ProtoMiner) GetConnectionInfo() networking.ConnectionInfo {
	return p.connectionInfo
}

func (p *ProtoMiner) StartMining(ctx context.Context) error {
	ctx = client.ContextWithAuth(ctx, p.authToken)
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
	ctx = client.ContextWithAuth(ctx, p.authToken)
	resp, err := p.commandClient.StopMining(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return err // Error mapping handled by interceptor
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fleeterror.NewInternalErrorf("stop mining failed: %s", resp.Msg.Message)
	}

	return nil
}

func (p *ProtoMiner) GetPairingInfo(ctx context.Context, conn *sql.DB) (*miner.PairingInfo, error) {
	ctx = client.ContextWithAuth(ctx, p.authToken)
	resp, err := p.systemClient.GetPairingInfo(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get pairing info: %v", err)
	}

	deviceIdentifier, err := db.WithTransaction[string](ctx, conn, func(q *sqlc.Queries) (string, error) {
		return q.GetDeviceIdentifierByID(ctx, p.deviceID)
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get device identifier from ID: %v", err)
	}

	return &miner.PairingInfo{
		DeviceID:     miner.DeviceID(deviceIdentifier),
		SerialNumber: resp.Msg.CbSn,
		MacAddress:   resp.Msg.Mac,
		// TODO(DASH-331) Fetch model and manufacturer from miner
		Model:        "Proto Rig",
		Manufacturer: "Block, Inc",
	}, nil
}

func (p *ProtoMiner) GetTelemetry(ctx context.Context, after time.Time) ([]telemetryModels.Telemetry, error) {
	// Create telemetry mapper
	mapper := NewTelemetryMapper(p.deviceID)

	// Generate time series requests for all data types
	requests := mapper.MapToTimeSeriesRequests(after)

	// Execute requests concurrently
	responses := make([]*miner_data_api.TimeSeriesDataResponse, len(requests))
	errGroup, ctx := errgroup.WithContext(ctx)

	// Add auth token to context
	ctx = client.ContextWithAuth(ctx, p.authToken)

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
