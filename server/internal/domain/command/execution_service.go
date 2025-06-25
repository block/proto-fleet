package command

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/commandtype"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

type ExecutionService struct {
	config *Config

	conn           *sql.DB
	messageQueue   queue.MessageQueue
	encryptService *encrypt.Service
	tokenService   *tokenDomain.Service

	workerSemaphore chan struct{}
}

func NewExecutionService(ctx context.Context, config *Config, conn *sql.DB, messageQueue queue.MessageQueue, encryptService *encrypt.Service, tokenService *tokenDomain.Service) *ExecutionService {
	executionService := &ExecutionService{
		config:          config,
		conn:            conn,
		messageQueue:    messageQueue,
		encryptService:  encryptService,
		tokenService:    tokenService,
		workerSemaphore: make(chan struct{}, config.MaxWorkers),
	}
	go func() {
		err := executionService.masterThread(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("message processing stopped with error", "error", err)
		}
	}()

	return executionService
}

func (es *ExecutionService) masterThread(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return fleeterror.NewInternalErrorf("error master thread ctx DONE: %v", ctx.Err())
		default:
			messages, err := es.messageQueue.Dequeue(ctx)
			if err != nil {
				// TODO do we return here, or implement some type of sleep and retry mechanism?
				return fleeterror.NewInternalErrorf("error dequeueing messages: %v", err)
			}

			if len(messages) == 0 {
				time.Sleep(es.config.MasterPollingInterval)
				continue
			}

			for _, message := range messages {
				es.workerSemaphore <- struct{}{}

				go func(msg queue.Message) {
					defer func() { <-es.workerSemaphore }()

					workerCtx, cancel := context.WithTimeout(ctx, es.config.WorkerExecutionTimeout)
					defer cancel()

					es.workerProcessCommand(workerCtx, msg)
				}(message)
			}
		}
	}
}

func upsertCommandOnDeviceStatus(workerError error) sqlc.CommandOnDeviceLogStatus {
	if workerError != nil {
		return sqlc.CommandOnDeviceLogStatusFAILED
	}
	return sqlc.CommandOnDeviceLogStatusSUCCESS
}

func (es *ExecutionService) workerProcessCommand(ctx context.Context, message queue.Message) {
	workerError := es.workerExecuteCommand(ctx, message.CommandType, message)
	timeNow := time.Now()
	dbError := db.WithTransactionNoResult(ctx, es.conn, func(q *sqlc.Queries) error {
		return q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
			CommandBatchLogID: message.BatchLogID,
			DeviceID:          message.DeviceID,
			Status:            upsertCommandOnDeviceStatus(workerError),
			UpdatedAt:         timeNow,
		})
	})
	if dbError != nil {
		// TODO what to do if commandOnDeviceLog fails
		slog.Error("error creating command on device log", "error", dbError)
	}
}

func (es *ExecutionService) workerExecuteCommand(ctx context.Context, commandType commandtype.Type, message queue.Message) error {
	minerInfo, err := es.GetMinerConnectionInfo(ctx, message.DeviceID)
	if err != nil {
		// TODO do we markFailed here?
		markFailedErr := es.messageQueue.MarkFailed(ctx, message.ID, err.Error())
		if markFailedErr != nil {
			return fleeterror.NewInternalErrorf("error getting miner connection info for deviceID: %d, %v, and also error marking as failed: %v", message.DeviceID, err, markFailedErr)
		}
		return fleeterror.NewInternalErrorf("error getting miner connection info for deviceID: %d, %v", message.DeviceID, err)
	}

	err = interfaces.GetMinerCommandFunc(commandType, minerInfo)(ctx)
	if err != nil {
		err = es.messageQueue.MarkFailed(ctx, message.ID, err.Error())
		return fleeterror.NewInternalErrorf("error setting message as failed on queue: %v", err)
	}
	err = es.messageQueue.MarkSuccess(ctx, message.ID)
	if err != nil {
		return fleeterror.NewInternalErrorf("error setting message as success on queue: %v", err)
	}
	return nil
}

// GetMinerConnectionInfo retrieves connection details for a single miner
func (es *ExecutionService) GetMinerConnectionInfo(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	return db.WithTransaction(ctx, es.conn, func(q *sqlc.Queries) (interfaces.Miner, error) {
		minerInfo, err := q.GetMinerApiNetworkInfoByDeviceID(ctx, deviceID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get miner info for miner %d: %v", deviceID, err)
		}

		encryptedOrganizationPrivateKey, err := q.GetOrganizationPrivateKey(ctx, minerInfo.OrgID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get organization private key for org id %d: %v", minerInfo.OrgID, err)
		}
		decryptedOrganizationPrivateKey, err := es.encryptService.Decrypt(encryptedOrganizationPrivateKey)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error decrypting organization private key: %v", err)
		}
		authToken, _, err := es.tokenService.GenerateMinerAuthJWT(minerInfo.DeviceIdentifier, decryptedOrganizationPrivateKey)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to generate miner auth token: %v", err)
		}

		port, err := strconv.ParseUint(minerInfo.Port, 10, 16)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("invalid port for miner %d: %v", deviceID, err)
		}

		return proto.NewProtoMiner(
			deviceID,
			minerInfo.IpAddress,
			uint16(port),
			*secrets.NewText(authToken),
		)
	})
}
