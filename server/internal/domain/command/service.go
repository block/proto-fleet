package command

import (
	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"context"
	"database/sql"
	"fmt"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api"
	minerPbCommon "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"net"
	"strings"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/minerclient"
)

// Service handles miner command operations
type Service struct {
	conn        *sql.DB
	minerClient *minerclient.Service
}

// NewService creates a new command service instance
func NewService(conn *sql.DB, minerClient *minerclient.Service) *Service {
	return &Service{
		conn:        conn,
		minerClient: minerClient,
	}
}

type minerError struct {
	DeviceIdentifier string
	Error            string
}

// minerCommand defines a function type for executing specific miner commands
type minerCommand func(client *minerclient.Service, ctx context.Context, minerURL string) (*connect.Response[miner_command_api.CommandResponse], error)

// StopMining stops mining on the specified miners
func (s *Service) StopMining(ctx context.Context, deviceIDs []string) (*pb.StopMiningResponse, error) {
	stopMiningCommand := func(client *minerclient.Service, ctx context.Context, minerURL string) (*connect.Response[miner_command_api.CommandResponse], error) {
		return client.StopMining(ctx, minerURL)
	}

	err := s.executeMinerCommand(ctx, deviceIDs, "StopMining", stopMiningCommand)
	if err != nil {
		return nil, err
	}

	return &pb.StopMiningResponse{}, nil
}

// StartMining starts mining on the specified miners
func (s *Service) StartMining(ctx context.Context, deviceIDs []string) (*pb.StartMiningResponse, error) {
	startMiningCommand := func(client *minerclient.Service, ctx context.Context, minerURL string) (*connect.Response[miner_command_api.CommandResponse], error) {
		return client.StartMining(ctx, minerURL)
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
		minerURL, err := s.getMinerURL(ctx, deviceID)
		if err != nil {
			failedMiners = append(failedMiners, &minerError{
				DeviceIdentifier: deviceID,
				Error:            fmt.Sprintf("failed to get minerURL for miner '%s': %s", deviceID, err.Error()),
			})
		}

		response, err := command(s.minerClient, ctx, minerURL)
		if err != nil {
			failedMiners = append(failedMiners, &minerError{
				DeviceIdentifier: deviceID,
				Error:            err.Error(),
			})
		} else if response.Msg.Result != minerPbCommon.ApiResult_RESULT_SUCCESS {
			failedMiners = append(failedMiners, &minerError{
				DeviceIdentifier: deviceID,
				Error:            fmt.Sprintf("miner command returned error: %s", response.Msg.Message),
			})
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

// getMinerURL retrieves connection details for a single miner
func (s *Service) getMinerURL(ctx context.Context, deviceID string) (string, error) {
	claims, ok := authn.GetInfo(ctx).(tokenDomain.Claims)
	if !ok {
		return "", fmt.Errorf("invalid token")
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (string, error) {
		minerInfo, err := q.GetMinerApiNetworkInfoByDeviceID(ctx, sqlc.GetMinerApiNetworkInfoByDeviceIDParams{OrgID: claims.OrgID, DeviceIdentifier: deviceID})
		if err != nil {
			return "", fmt.Errorf("failed to get miner info for miner %s: %w", deviceID, err)
		}

		return net.JoinHostPort(minerInfo.IpAddress, minerInfo.Port), nil
	})
}
