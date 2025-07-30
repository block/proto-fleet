package command

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"

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
	filesService     *files.Service
}

const defaultPoolPriority uint32 = 0

type Command struct {
	commandType    commandtype.Type
	deviceSelector *pb.DeviceSelector
	payload        interface{}
}

// NewService creates a new command service instance
func NewService(config *Config, conn *sql.DB, executionService *ExecutionService, messageQueue queue.MessageQueue, statusService *StatusService, encryptService *encrypt.Service, filesService *files.Service) *Service {
	return &Service{
		config:           config,
		conn:             conn,
		executionService: executionService,
		messageQueue:     messageQueue,
		statusService:    statusService,
		encryptService:   encryptService,
		filesService:     filesService,
	}
}

func (s *Service) getDevicesCount(ctx context.Context, selector *pb.DeviceSelector) (int32, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error getting claims from ctx: %v", err)
	}

	switch x := selector.SelectionType.(type) {
	case *pb.DeviceSelector_AllDevices:
		return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (int32, error) {
			count, err := q.GetTotalPairedDevices(ctx, sqlc.GetTotalPairedDevicesParams{OrgID: claims.OrgID})
			if err != nil {
				return 0, err
			}
			// #nosec G115 - We know device identifiers len won't exceed int32 max value
			return int32(count), nil
		})
	case *pb.DeviceSelector_IncludeDevices:
		// #nosec G115 - We know device identifiers len won't exceed int32 max value
		return int32(len(x.IncludeDevices.DeviceIdentifiers)), nil
	default:
		return 0, fleeterror.NewInternalErrorf("getDevicesCount called with unknown type: %v", x)
	}
}

func (s *Service) saveCommandBatchLogToDB(ctx context.Context, userID int64, command *Command, payloadBytes []byte) (string, error) {
	devicesCount, err := s.getDevicesCount(ctx, command.deviceSelector)
	if err != nil {
		return "", err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (string, error) {
		timeNow := time.Now()
		newUUID := id.GenerateID()

		_, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:         newUUID,
			Type:         command.commandType.String(),
			CreatedBy:    userID,
			CreatedAt:    timeNow,
			Status:       sqlc.CommandBatchLogStatusPENDING,
			DevicesCount: devicesCount,
			Payload:      payloadBytes,
		})
		if err != nil {
			return "", fleeterror.NewInternalErrorf("error creating command batch log: %v", err)
		}

		return newUUID, nil
	})
}

func (s *Service) statusUpdateIsProcessingBranch(ctx context.Context, commandBatchLogUUID string) (bool, error) {
	isProcessing, err := s.messageQueue.IsBatchProcessing(ctx, commandBatchLogUUID)
	if err != nil {
		return false, fleeterror.NewInternalErrorf("error asking isProcessing: %v", err)
	}
	if isProcessing {
		err = db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
			return q.MarkCommandBatchProcessing(ctx, commandBatchLogUUID)
		})
		if err != nil {
			return false, fleeterror.NewInternalErrorf("error marking batch: %v", err)
		}
		return true, nil
	}
	return false, nil
}

func (s *Service) getMarkFinishedBatchFunction(processingMarkedInDB bool) func(ctx context.Context, commandBatchLogUUID string) error {
	return func(ctx context.Context, commandBatchLogUUID string) error {
		return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
			if processingMarkedInDB {
				return q.MarkCommandBatchFinished(ctx, commandBatchLogUUID)
			}
			return q.MarkCommandBatchFinishedWithStartedAt(ctx, commandBatchLogUUID)
		})
	}
}

func (s *Service) statusUpdateIsFinishedBranch(ctx context.Context, commandBatchLogUUID string, processingMarkedInDB bool) (bool, error) {
	isFinished, err := s.messageQueue.IsBatchFinished(ctx, commandBatchLogUUID)
	if err != nil {
		return false, fleeterror.NewInternalErrorf("error asking is finished: %v", err)
	}
	if isFinished {
		err = s.getMarkFinishedBatchFunction(processingMarkedInDB)(ctx, commandBatchLogUUID)
		if err != nil {
			return false, fleeterror.NewInternalErrorf("error marking batch: %v", err)
		}

		return true, nil
	}
	return false, nil
}

type onFinishedCallbackFunc func() error

func (s *Service) initializeStatusUpdateRoutine(commandBatchLogUUID string, onFinishedCallback onFinishedCallbackFunc) {
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
					isProcessing, err := s.statusUpdateIsProcessingBranch(ctx, commandBatchLogUUID)
					if err != nil {
						slog.Error("error in isProcessing branch", "error", err)
						return
					}
					processingMarkedInDB = isProcessing
				}
				isFinished, err := s.statusUpdateIsFinishedBranch(ctx, commandBatchLogUUID, processingMarkedInDB)
				if err != nil {
					slog.Error("error in isFinished branch", "error", err)
					return
				}
				if isFinished {
					if onFinishedCallback != nil {
						if callbackErr := onFinishedCallback(); callbackErr != nil {
							slog.Error("error in onFinished callback", "error", callbackErr)
						}
					}
					return
				}
			}
		}
	}()
}

func (s *Service) statusUpdateRoutineOnFinishedCallback(commandType commandtype.Type, batchLogUUID string) onFinishedCallbackFunc {
	switch commandType {
	case commandtype.DownloadLogs:
		return s.filesService.DownloadLogsOnFinishedCallback(batchLogUUID)
	case commandtype.StopMining, commandtype.StartMining, commandtype.SetCoolingMode, commandtype.UpdateMiningPools, commandtype.Reboot, commandtype.BlinkLED:
		return nil
	default:
		return nil
	}
}

func (s *Service) getDeviceIDs(ctx context.Context, selector *pb.DeviceSelector) ([]int64, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting claims from context: %v", err)
	}

	switch x := selector.SelectionType.(type) {
	case *pb.DeviceSelector_AllDevices:
		return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]int64, error) {
			return q.GetPairedDevicesIds(ctx, claims.OrgID)
		})
	case *pb.DeviceSelector_IncludeDevices:
		if len(x.IncludeDevices.DeviceIdentifiers) == 0 {
			return []int64{}, nil
		}

		return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]int64, error) {
			return q.GetDeviceIDsByDeviceIdentifiers(ctx, x.IncludeDevices.DeviceIdentifiers)
		})
	default:
		return nil, fleeterror.NewInternalErrorf("getDeviceIDs called with unknown type: %v", x)
	}
}

func (s *Service) processCommand(ctx context.Context, command *Command) (string, error) {
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

	payloadBytes, err := json.Marshal(command.payload)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error marshalling payload: %v", err)
	}

	batchLogIdentifier, err := s.saveCommandBatchLogToDB(ctx, claims.UserID, command, payloadBytes)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error saving command batch log to db: %v", err)
	}
	deviceIDs, err := s.getDeviceIDs(ctx, command.deviceSelector)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error getting device IDs from device selector: %v", err)
	}

	err = s.messageQueue.Enqueue(ctx, batchLogIdentifier, command.commandType, deviceIDs, command.payload)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error enqueuing a batch of commands: %v", err)
	}

	onFinishedCallback := s.statusUpdateRoutineOnFinishedCallback(command.commandType, batchLogIdentifier)
	s.initializeStatusUpdateRoutine(batchLogIdentifier, onFinishedCallback)

	return batchLogIdentifier, nil
}

func (s *Service) Reboot(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.RebootResponse, error) {
	commandBatchLogUUID, err := s.processCommand(ctx, &Command{commandType: commandtype.Reboot, deviceSelector: deviceSelector, payload: nil})
	if err != nil {
		return nil, err
	}

	return &pb.RebootResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

// StopMining stops mining on the specified miners
func (s *Service) StopMining(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.StopMiningResponse, error) {
	commandBatchLogUUID, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.StopMining, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}

	return &pb.StopMiningResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

// StartMining starts mining on the specified miners
func (s *Service) StartMining(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.StartMiningResponse, error) {
	commandBatchLogUUID, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.StartMining, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}

	return &pb.StartMiningResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

func (s *Service) SetCoolingMode(ctx context.Context, deviceSelector *pb.DeviceSelector, modeType pb.CoolingMode) (*pb.SetCoolingModeResponse, error) {
	cm := dto.CoolingModePayload{Mode: modeType}
	commandBatchLogUUID, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.SetCoolingMode, deviceSelector: deviceSelector, payload: cm},
	)
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

func (s *Service) UpdateMiningPools(ctx context.Context, deviceSelector *pb.DeviceSelector, defaultPoolID int64, backup1PoolID *int64, backup2PoolID *int64) (*pb.UpdateMiningPoolsResponse, error) {
	pld, err := s.createUpdateMiningPoolsPayload(ctx, defaultPoolID, backup1PoolID, backup2PoolID)
	if err != nil {
		return nil, err
	}

	commandBatchLogUUID, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.UpdateMiningPools, deviceSelector: deviceSelector, payload: pld},
	)
	if err != nil {
		return nil, err
	}
	return &pb.UpdateMiningPoolsResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

func (s *Service) DownloadLogs(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.DownloadLogsResponse, error) {
	commandBatchLogUUID, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.DownloadLogs, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}

	return &pb.DownloadLogsResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

func (s *Service) BlinkLED(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.BlinkLEDResponse, error) {
	commandBatchLogUUID, err := s.processCommand(ctx, &Command{commandType: commandtype.BlinkLED, deviceSelector: deviceSelector, payload: nil})
	if err != nil {
		return nil, err
	}
	return &pb.BlinkLEDResponse{BatchIdentifier: commandBatchLogUUID}, nil
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

func (s *Service) GetCommandBatchLogBundle(batchUUID string) (*pb.GetCommandBatchLogBundleResponse, error) {
	file, err := s.filesService.GetBatchLogBundleFile(batchUUID)
	if err != nil {
		return nil, err
	}

	s.filesService.ScheduleBatchLogCleanup(batchUUID, 30*time.Minute)

	return &pb.GetCommandBatchLogBundleResponse{
		Filename:  file.Filename,
		ChunkData: file.Data,
	}, nil
}
