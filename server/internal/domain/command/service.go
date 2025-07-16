package command

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"

	"log/slog"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/commandtype"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	id "github.com/btc-mining/proto-fleet/server/internal/infrastructure/id"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
)

// Service handles miner command operations
type Service struct {
	config *Config

	conn             *sql.DB
	executionService *ExecutionService
	messageQueue     queue.MessageQueue
	statusService    *StatusService
	encryptService   *encrypt.Service
}

const defaultPoolPriority uint32 = 0

type batchLogIdentifier struct {
	id   int64
	uuid string
}

// NewService creates a new command service instance
func NewService(config *Config, conn *sql.DB, executionService *ExecutionService, messageQueue queue.MessageQueue, statusService *StatusService, encryptService *encrypt.Service) *Service {
	return &Service{
		config:           config,
		conn:             conn,
		executionService: executionService,
		messageQueue:     messageQueue,
		statusService:    statusService,
		encryptService:   encryptService,
	}
}

func (s *Service) saveCommandBatchLogToDB(ctx context.Context, commandType commandtype.Type, userID int64, devicesCount int32, payload interface{}) (*batchLogIdentifier, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error marshalling payload: %v", err)
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (*batchLogIdentifier, error) {
		timeNow := time.Now()
		newUUID := id.GenerateID()
		result, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:         newUUID,
			Type:         commandType.String(),
			CreatedBy:    userID,
			CreatedAt:    timeNow,
			Status:       sqlc.CommandBatchLogStatusPENDING,
			DevicesCount: devicesCount,
			Payload:      payloadBytes,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error creating command batch log: %v", err)
		}
		lastInsertID, err := result.LastInsertId()
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting last insert ID: %v", err)
		}
		return &batchLogIdentifier{id: lastInsertID, uuid: newUUID}, nil
	})
}

func (s *Service) statusUpdateIsProcessingBranch(ctx context.Context, commandBatchLogID int64) (bool, error) {
	isProcessing, err := s.messageQueue.IsBatchProcessing(ctx, commandBatchLogID)
	if err != nil {
		return false, fleeterror.NewInternalErrorf("error asking isProcessing: %v", err)
	}
	if isProcessing {
		err = db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
			return q.MarkCommandBatchProcessing(ctx, commandBatchLogID)
		})
		if err != nil {
			return false, fleeterror.NewInternalErrorf("error marking batch: %v", err)
		}
		return true, nil
	}
	return false, nil
}

func (s *Service) getMarkFinishedBatchFunction(processingMarkedInDB bool) func(ctx context.Context, commandBatchLogID int64) error {
	return func(ctx context.Context, commandBatchLogID int64) error {
		return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
			if processingMarkedInDB {
				return q.MarkCommandBatchFinished(ctx, commandBatchLogID)
			}
			return q.MarkCommandBatchFinishedWithStartedAt(ctx, commandBatchLogID)
		})
	}
}

func (s *Service) statusUpdateIsFinishedBranch(ctx context.Context, commandBatchLogID int64, processingMarkedInDB bool) (bool, error) {
	isFinished, err := s.messageQueue.IsBatchFinished(ctx, commandBatchLogID)
	if err != nil {
		return false, fleeterror.NewInternalErrorf("error asking is finished: %v", err)
	}
	if isFinished {
		err = s.getMarkFinishedBatchFunction(processingMarkedInDB)(ctx, commandBatchLogID)
		if err != nil {
			return false, fleeterror.NewInternalErrorf("error marking batch: %v", err)
		}
		return true, nil
	}
	return false, nil
}

func (s *Service) initializeStatusUpdateRoutine(commandBatchLogID int64) {
	go func() {
		// TODO maybe integrate this with the execution service master thread ctx in the future
		ctx := context.Background()
		ticker := time.NewTicker(s.config.BatchStatusUpdatePollingInterval)
		defer ticker.Stop()

		processingMarkedInDB := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !processingMarkedInDB {
					isProcessing, err := s.statusUpdateIsProcessingBranch(ctx, commandBatchLogID)
					if err != nil {
						slog.Error("error in isProcessing branch", "error", err)
						return
					}
					processingMarkedInDB = isProcessing
				}
				isFinished, err := s.statusUpdateIsFinishedBranch(ctx, commandBatchLogID, processingMarkedInDB)
				if err != nil {
					slog.Error("error in isFinished branch", "error", err)
					return
				}
				if isFinished {
					return
				}
			}
		}
	}()
}

func (s *Service) processCommand(ctx context.Context, commandType commandtype.Type, deviceIdentifiers []string, payload interface{}) (string, error) {
	if !s.executionService.IsRunning() {
		slog.Error("command execution service is not running, attempting to start it")
		err := s.executionService.Start(ctx)
		if err != nil {
			return "", fleeterror.NewInternalErrorf("failed to start command execution service: %v", err)
		}
	}

	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error getting claims from ctx: %v", err)
	}
	// #nosec G115 - We know device identifiers len won't exceed int32 max value
	batchLogIdentifier, err := s.saveCommandBatchLogToDB(ctx, commandType, claims.UserID, int32(len(deviceIdentifiers)), payload)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error saving command batch log to db: %v", err)
	}
	deviceIDs, err := db.WithTransaction[[]int64](ctx, s.conn, func(q *sqlc.Queries) ([]int64, error) {
		return q.GetDeviceIDsByDeviceIdentifiers(ctx, deviceIdentifiers)
	})
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error getting device IDs from device identifiers: %v", err)
	}

	err = s.messageQueue.Enqueue(ctx, batchLogIdentifier.id, commandType, deviceIDs, payload)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error enqueuing a batch of commands: %v", err)
	}

	s.initializeStatusUpdateRoutine(batchLogIdentifier.id)

	return batchLogIdentifier.uuid, nil
}

// StopMining stops mining on the specified miners
func (s *Service) StopMining(ctx context.Context, deviceIDs []string) (*pb.StopMiningResponse, error) {
	commandBatchLogUUID, err := s.processCommand(ctx, commandtype.StopMining, deviceIDs, nil)
	if err != nil {
		return nil, err
	}

	return &pb.StopMiningResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

// StartMining starts mining on the specified miners
func (s *Service) StartMining(ctx context.Context, deviceIDs []string) (*pb.StartMiningResponse, error) {
	commandBatchLogUUID, err := s.processCommand(ctx, commandtype.StartMining, deviceIDs, nil)
	if err != nil {
		return nil, err
	}

	return &pb.StartMiningResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

func (s *Service) SetCoolingMode(ctx context.Context, deviceIDs []string, modeType pb.CoolingMode) (*pb.SetCoolingModeResponse, error) {
	cm := dto.CoolingModePayload{Mode: modeType}
	commandBatchLogUUID, err := s.processCommand(ctx, commandtype.SetCoolingMode, deviceIDs, cm)
	if err != nil {
		return nil, err
	}

	return &pb.SetCoolingModeResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

func (s *Service) createMiningPoolDTO(ctx context.Context, poolID int64, priorityIncrement uint32) (*dto.MiningPool, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting auth JWT claims: %v", err)
	}

	pool, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.Pool, error) {
		return q.GetPool(ctx, sqlc.GetPoolParams{ID: poolID, OrgID: claims.OrgID})
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting default pool: %v", err)
	}

	var password string
	if pool.PasswordEnc != "" {
		decryptedPassBytes, err := s.encryptService.Decrypt(pool.PasswordEnc)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error decrypting pass: %v", err)
		}
		password = string(decryptedPassBytes)
	}

	return &dto.MiningPool{
		Priority: defaultPoolPriority + priorityIncrement,
		URL:      pool.Url,
		Username: pool.Username,
		Password: password,
	}, nil
}

func (s *Service) createUpdateMiningPoolsPayload(ctx context.Context, defaultPoolID int64, backup1PoolID *int64, backup2PoolID *int64) (*dto.UpdateMiningPoolsPayload, error) {
	defaultPool, err := s.createMiningPoolDTO(ctx, defaultPoolID, 0)
	if err != nil {
		return nil, err
	}

	pld := &dto.UpdateMiningPoolsPayload{
		DefaultPool: *defaultPool,
	}

	if backup1PoolID != nil {
		pool, err := s.createMiningPoolDTO(ctx, *backup1PoolID, 1)
		if err != nil {
			return nil, err
		}
		pld.Backup1Pool = pool
	}

	if backup2PoolID != nil {
		pool, err := s.createMiningPoolDTO(ctx, *backup2PoolID, 2)
		if err != nil {
			return nil, err
		}
		pld.Backup2Pool = pool
	}

	return pld, nil
}

func (s *Service) UpdateMiningPools(ctx context.Context, deviceIDs []string, defaultPoolID int64, backup1PoolID *int64, backup2PoolID *int64) (*pb.UpdateMiningPoolsResponse, error) {
	pld, err := s.createUpdateMiningPoolsPayload(ctx, defaultPoolID, backup1PoolID, backup2PoolID)
	if err != nil {
		return nil, err
	}

	commandBatchLogUUID, err := s.processCommand(ctx, commandtype.UpdateMiningPools, deviceIDs, pld)
	if err != nil {
		return nil, err
	}
	return &pb.UpdateMiningPoolsResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

func (s *Service) StreamCommandBatchUpdates(ctx context.Context, msg *pb.StreamCommandBatchUpdatesRequest) (<-chan *pb.StreamCommandBatchUpdatesResponse, error) {
	_, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	responseChan := make(chan *pb.StreamCommandBatchUpdatesResponse, 100)

	statusChan, err := s.statusService.StreamCommandBatchUpdates(ctx, msg.BatchIdentifier)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error creating stream: %v", err)
	}

	// Start goroutine to handle the batch updates stream
	go func() {
		defer close(responseChan)

		for {
			select {
			case <-ctx.Done():
				return
			case status, ok := <-statusChan:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				case responseChan <- status:
				}
			}
		}

	}()

	return responseChan, nil
}
