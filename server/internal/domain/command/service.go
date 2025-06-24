package command

import (
	"context"
	"database/sql"
	"errors"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/commandtype"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"log/slog"
	"strings"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
)

// Service handles miner command operations
type Service struct {
	config *Config

	conn             *sql.DB
	executionService *ExecutionService
	messageQueue     queue.MessageQueue
	statusService    *StatusService
}

// NewService creates a new command service instance
func NewService(config *Config, conn *sql.DB, executionService *ExecutionService, messageQueue queue.MessageQueue, statusService *StatusService) *Service {
	return &Service{
		config:           config,
		conn:             conn,
		executionService: executionService,
		messageQueue:     messageQueue,
		statusService:    statusService,
	}
}

func isDuplicateKeyError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	// Non mysql approach
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate")
}

func (s *Service) saveCommandBatchLogToDB(ctx context.Context, commandType commandtype.Type, userID int64) (int64, error) {
	return db.WithTransaction[int64](ctx, s.conn, func(q *sqlc.Queries) (int64, error) {
		timeNow := time.Now()
		newUUID := uuid.New().String()
		result, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:      newUUID,
			Type:      commandType.String(),
			CreatedBy: userID,
			CreatedAt: timeNow,
			Status:    sqlc.CommandBatchLogStatusPENDING,
		})
		if err != nil {
			return 0, fleeterror.NewInternalErrorf("error creating command batch log: %v", err)
		}
		lastInsertID, err := result.LastInsertId()
		if err != nil {
			return 0, fleeterror.NewInternalErrorf("error getting last insert ID: %v", err)
		}
		return lastInsertID, nil
	})
}

type MarkCommandBatchFunc func(ctx context.Context, id int64) error

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

func (s *Service) processCommand(ctx context.Context, commandType commandtype.Type, deviceIdentifiers []string) error {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting claims from ctx: %v", err)
	}
	commandBatchLogID, err := s.saveCommandBatchLogToDB(ctx, commandType, claims.UserID)
	if err != nil {
		return fleeterror.NewInternalErrorf("error saving command batch log to db: %v", err)
	}
	deviceIDs, err := db.WithTransaction[[]int64](ctx, s.conn, func(q *sqlc.Queries) ([]int64, error) {
		return q.GetDeviceIDsByDeviceIdentifiers(ctx, deviceIdentifiers)
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting device IDs from device identifiers: %v", err)
	}

	err = s.messageQueue.Enqueue(ctx, commandBatchLogID, commandType, deviceIDs)
	if err != nil {
		return fleeterror.NewInternalErrorf("error enqueuing a batch of commands: %v", err)
	}

	s.initializeStatusUpdateRoutine(commandBatchLogID)

	return nil
}

// StopMining stops mining on the specified miners
func (s *Service) StopMining(ctx context.Context, deviceIDs []string) (*pb.StopMiningResponse, error) {
	err := s.processCommand(ctx, commandtype.StopMining, deviceIDs)
	if err != nil {
		return nil, err
	}

	return &pb.StopMiningResponse{}, nil
}

// StartMining starts mining on the specified miners
func (s *Service) StartMining(ctx context.Context, deviceIDs []string) (*pb.StartMiningResponse, error) {
	err := s.processCommand(ctx, commandtype.StartMining, deviceIDs)
	if err != nil {
		return nil, err
	}

	return &pb.StartMiningResponse{}, nil
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

		select {
		case <-ctx.Done():
			return
		case status := <-statusChan:
			select {
			case <-ctx.Done():
				return
			case responseChan <- status:
			}
		}
	}()

	return responseChan, nil
}
