package command

import (
	"connectrpc.com/connect"
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api"
	minerPb "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	minerPbCommon "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"golang.org/x/net/http2"
	"net"
	"net/http"
	"strings"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
)

// Service handles miner command operations
type Service struct {
	conn *sql.DB
}

// NewService creates a new command service instance
func NewService(conn *sql.DB) *Service {
	return &Service{
		conn: conn,
	}
}

// minerDetails contains the information needed to communicate with a miner
type minerDetails struct {
	DeviceIdentifier string
	URL              string
}

type minerError struct {
	DeviceIdentifier string
	Error            string
}

// minerCommand defines a function type for executing specific miner commands
type minerCommand func(ctx context.Context, client minerPb.MinerCommandApiClient) (*connect.Response[miner_command_api.CommandResponse], error)

var httpClient = &http.Client{
	Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	},
}

var httpsClient = http.DefaultClient

// StopMining stops mining on the specified miners
func (s *Service) StopMining(ctx context.Context, deviceIDs []string) (*pb.StopMiningResponse, error) {
	stopMiningCommand := func(ctx context.Context, client minerPb.MinerCommandApiClient) (*connect.Response[miner_command_api.CommandResponse], error) {
		return client.StopMining(ctx, connect.NewRequest(&minerPbCommon.EmptyRequest{}))
	}

	err := s.executeMinerCommand(ctx, deviceIDs, "StopMining", stopMiningCommand)
	if err != nil {
		return nil, err
	}

	return &pb.StopMiningResponse{}, nil
}

// StartMining starts mining on the specified miners
func (s *Service) StartMining(ctx context.Context, deviceIDs []string) (*pb.StartMiningResponse, error) {
	startMiningCommand := func(ctx context.Context, client minerPb.MinerCommandApiClient) (*connect.Response[miner_command_api.CommandResponse], error) {
		return client.StartMining(ctx, connect.NewRequest(&minerPbCommon.EmptyRequest{}))
	}

	err := s.executeMinerCommand(ctx, deviceIDs, "StartMining", startMiningCommand)
	if err != nil {
		return nil, err
	}

	return &pb.StartMiningResponse{}, nil
}

// executeMinerCommand is a universal function that executes a given command on multiple miners
func (s *Service) executeMinerCommand(ctx context.Context, deviceIDs []string, commandName string, command minerCommand) error {
	var failedMiners []*minerError
	for _, deviceID := range deviceIDs {
		minerError := s.executeMinerCommandByDeviceID(ctx, deviceID, command)
		if minerError != nil {
			failedMiners = append(failedMiners, minerError)
		}
	}

	if len(failedMiners) > 0 {
		// Build error message with details about failed miners
		var errs []string
		for _, fm := range failedMiners {
			errs = append(errs, fmt.Sprintf("Miner %s: %s", fm.DeviceIdentifier, fm.Error))
		}

		return fmt.Errorf("failed to execute command '%s' miners:\n%s", commandName, strings.Join(errs, "\n"))
	}

	return nil
}

func (s *Service) executeMinerCommandByDeviceID(ctx context.Context, deviceID string, command minerCommand) *minerError {
	minerDetails, err := s.getMinerDetails(ctx, deviceID)
	if err != nil {
		return &minerError{
			DeviceIdentifier: deviceID,
			Error:            fmt.Sprintf("failed to get miner details: %v", err),
		}
	}

	err = s.executeCommandOnMiner(ctx, minerDetails, command)
	if err != nil {
		return &minerError{
			DeviceIdentifier: deviceID,
			Error:            err.Error(),
		}
	}

	return nil
}

// getMinerDetails retrieves details for a single miner
func (s *Service) getMinerDetails(ctx context.Context, deviceID string) (minerDetails, error) {
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (minerDetails, error) {
		minerInfo, err := q.GetMinerApiNetworkInfoByDeviceID(ctx, deviceID)
		if err != nil {
			return minerDetails{}, fmt.Errorf("failed to get miner %s: %w", deviceID, err)
		}

		return minerDetails{
			DeviceIdentifier: deviceID,
			URL:              fmt.Sprintf("%s:%s", minerInfo.IpAddress, minerInfo.Port),
		}, nil
	})
}

// executeCommandOnMiner executes a command on a single miner
func (s *Service) executeCommandOnMiner(ctx context.Context, miner minerDetails, executor minerCommand) error {
	// TODO Check if the controlled miners belong to this user organization

	response, err := s.executeCommandOnMinerWithProtocol(ctx, httpsClient, miner, executor, "https")
	if err != nil {
		response, err = s.executeCommandOnMinerWithProtocol(ctx, httpClient, miner, executor, "http")

		if err != nil {
			return err
		}
	}

	if response.Msg.Result != minerPbCommon.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("miner command returned error: %s", response.Msg.Message)
	}

	return nil
}

func (s *Service) executeCommandOnMinerWithProtocol(
	ctx context.Context,
	httpClient *http.Client,
	miner minerDetails,
	executor minerCommand,
	protocol string,
) (*connect.Response[miner_command_api.CommandResponse], error) {
	minerClient := minerPb.NewMinerCommandApiClient(
		httpClient,
		protocol+"://"+miner.URL,
		connect.WithGRPC(),
	)

	resp, err := executor(ctx, minerClient)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %v", err)
	}

	return resp, nil
}
