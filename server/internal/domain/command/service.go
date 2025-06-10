package command

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto"
	protoMinerClient "github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"

	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
)

// Service handles miner command operations
type Service struct {
	conn             *sql.DB
	protoMinerClient *protoMinerClient.Service
	tokenService     *tokenDomain.Service
	encryptService   *encrypt.Service
}

// NewService creates a new command service instance
func NewService(conn *sql.DB, protoMinerClient *protoMinerClient.Service, tokenService *tokenDomain.Service, encryptService *encrypt.Service) *Service {
	return &Service{
		conn:             conn,
		protoMinerClient: protoMinerClient,
		tokenService:     tokenService,
		encryptService:   encryptService,
	}
}

type minerError struct {
	DeviceIdentifier string
	Error            string
}

// minerCommand defines a function type for executing specific miner commands
type minerCommand func(ctx context.Context, miner miner.Miner) error

// StopMining stops mining on the specified miners
func (s *Service) StopMining(ctx context.Context, deviceIDs []string) (*pb.StopMiningResponse, error) {
	stopMiningCommand := func(ctx context.Context, miner miner.Miner) error {
		return miner.StopMining(ctx)
	}

	err := s.executeMinerCommand(ctx, deviceIDs, "StopMining", stopMiningCommand)
	if err != nil {
		return nil, err
	}

	return &pb.StopMiningResponse{}, nil
}

// StartMining starts mining on the specified miners
func (s *Service) StartMining(ctx context.Context, deviceIDs []string) (*pb.StartMiningResponse, error) {
	startMiningCommand := func(ctx context.Context, miner miner.Miner) error {
		return miner.StartMining(ctx)
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
		miner, err := s.getMiner(ctx, deviceID)
		if err != nil {
			failedMiners = append(failedMiners, &minerError{
				DeviceIdentifier: deviceID,
				Error:            fmt.Sprintf("failed to get minerURL for miner '%s': %s", deviceID, err.Error()),
			})
		}

		err = command(ctx, miner)
		if err != nil {
			failedMiners = append(failedMiners, &minerError{
				DeviceIdentifier: deviceID,
				Error:            err.Error(),
			})
		}
	}

	if len(failedMiners) > 0 {
		// Build error message with details about failed miners
		var errs []string
		for _, fm := range failedMiners {
			errs = append(errs, fmt.Sprintf("Miner %s: %s", fm.DeviceIdentifier, fm.Error))
		}

		return fleeterror.NewInternalErrorf("failed to execute command '%s' miners:\n%s", commandName, strings.Join(errs, "\n"))
	}

	return nil
}

// getMinerConnectionInfo retrieves connection details for a single miner
func (s *Service) getMiner(ctx context.Context, deviceID string) (miner.Miner, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (miner.Miner, error) {
		minerInfo, err := q.GetMinerApiNetworkInfoByDeviceID(ctx, sqlc.GetMinerApiNetworkInfoByDeviceIDParams{OrgID: claims.OrgID, DeviceIdentifier: deviceID})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get miner info for miner %s: %v", deviceID, err)
		}

		encryptedOrganizationPrivateKey, err := q.GetOrganizationPrivateKey(ctx, claims.OrgID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get organization private key for org id %d: %v", claims.OrgID, err)
		}
		decryptedOrganizationPrivateKey, err := s.encryptService.Decrypt(encryptedOrganizationPrivateKey)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error decrypting organization private key: %v", err)
		}
		authToken, _, err := s.tokenService.GenerateMinerAuthJWT(deviceID, decryptedOrganizationPrivateKey)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to generate miner auth token: %v", err)
		}

		port, err := strconv.ParseUint(minerInfo.Port, 10, 16)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("invalid port for miner %s: %v", deviceID, err)
		}

		// TODO DASH-429: add miner type to the database and construct the appropriate miner type
		return proto.NewProtoMiner(
			deviceID,
			minerInfo.IpAddress,
			uint16(port),
			s.protoMinerClient,
			authToken,
		), nil
	})
}
