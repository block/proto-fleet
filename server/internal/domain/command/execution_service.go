package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	tmodels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/commandtype"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"
)

//go:generate go run go.uber.org/mock/mockgen -source=execution_service.go -destination=mocks/mock_miner_getter.go -package=mocks MinerGetter
type MinerGetter interface {
	GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error)
}

type ExecutionService struct {
	config *Config

	conn              *sql.DB
	messageQueue      queue.MessageQueue
	encryptService    *encrypt.Service
	tokenService      *tokenDomain.Service
	minerService      MinerGetter
	deviceStore       stores.DeviceStore
	telemetryListener TelemetryListener
	filesService      *files.Service

	workerSemaphore chan struct{}

	queueProcessorMu      sync.Mutex
	queueProcessorRunning bool
}

func NewExecutionService(ctx context.Context, config *Config, conn *sql.DB, messageQueue queue.MessageQueue, encryptService *encrypt.Service, tokenService *tokenDomain.Service, minerService MinerGetter, deviceStore stores.DeviceStore, telemetryListener TelemetryListener, filesService *files.Service) *ExecutionService {
	return &ExecutionService{
		config:                config,
		conn:                  conn,
		messageQueue:          messageQueue,
		encryptService:        encryptService,
		tokenService:          tokenService,
		minerService:          minerService,
		deviceStore:           deviceStore,
		telemetryListener:     telemetryListener,
		filesService:          filesService,
		workerSemaphore:       make(chan struct{}, config.MaxWorkers),
		queueProcessorRunning: false,
	}
}

// Start starts the queue processor thread if it is not already running.
func (es *ExecutionService) Start(ctx context.Context) error {
	es.queueProcessorMu.Lock()
	defer es.queueProcessorMu.Unlock()

	if es.queueProcessorRunning {
		return nil
	}

	es.queueProcessorRunning = true

	// Start the queue processor thread
	go func() {
		err := es.startQueueProcessorThread(ctx)
		es.queueProcessorMu.Lock()
		es.queueProcessorRunning = false
		es.queueProcessorMu.Unlock()

		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("message processing stopped with error", "error", err)
		}
	}()

	return nil
}

func (es *ExecutionService) IsRunning() bool {
	es.queueProcessorMu.Lock()
	defer es.queueProcessorMu.Unlock()

	return es.queueProcessorRunning
}

func (es *ExecutionService) dequeueWithRetry(ctx context.Context) ([]queue.Message, error) {
	messages, err := es.messageQueue.Dequeue(ctx)
	if err == nil {
		return messages, nil
	}

	delay := es.config.MasterPollingInterval

	for i := range es.config.DequeueRetries {
		slog.Warn("dequeue error, retrying", "attempt", i+1, "error", err)

		select {
		case <-ctx.Done():
			return nil, fleeterror.NewInternalErrorf("context cancelled: %v", ctx.Err())
		case <-time.After(delay):
			// Continue with retry
		}

		// simple backoff for next attempt
		delay *= 2

		messages, err = es.messageQueue.Dequeue(ctx)
		if err == nil {
			return messages, nil
		}
	}

	slog.Error("dequeue failed after retries", "error", err)
	return nil, err
}

func (es *ExecutionService) startQueueProcessorThread(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return fleeterror.NewInternalErrorf("error queue processor thread ctx DONE: %v", ctx.Err())
		default:
			messages, err := es.dequeueWithRetry(ctx)

			if err != nil {
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

func upsertCommandOnDeviceStatus(workerError error) sqlc.DeviceCommandStatusEnum {
	if workerError != nil {
		return sqlc.DeviceCommandStatusEnumFAILED
	}
	return sqlc.DeviceCommandStatusEnumSUCCESS
}

func (es *ExecutionService) workerProcessCommand(ctx context.Context, message queue.Message) {
	workerError := es.workerExecuteCommand(ctx, message.CommandType, message)
	timeNow := time.Now()
	dbError := db.WithTransactionNoResult(ctx, es.conn, func(q *sqlc.Queries) error {
		return q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
			Uuid:      message.BatchLogUUID,
			DeviceID:  message.DeviceID,
			Status:    upsertCommandOnDeviceStatus(workerError),
			UpdatedAt: timeNow,
		})
	})
	if dbError != nil {
		// TODO what to do if commandOnDeviceLog fails
		slog.Error("error creating command on device log", "error", dbError)
	}
}

func (es *ExecutionService) workerExecuteCommand(ctx context.Context, commandType commandtype.Type, message queue.Message) error {
	minerInfo, err := es.minerService.GetMiner(ctx, message.DeviceID)
	if err != nil {
		markFailedErr := es.messageQueue.MarkFailed(ctx, message.ID, err.Error())
		if markFailedErr != nil {
			return fleeterror.NewInternalErrorf("error getting miner connection info for deviceID: %d, %v, and also error marking as failed: %v", message.DeviceID, err, markFailedErr)
		}
		return fleeterror.NewInternalErrorf("error getting miner connection info for deviceID: %d, %v", message.DeviceID, err)
	}

	switch commandType {
	case commandtype.Reboot:
		err = minerInfo.Reboot(ctx)
	case commandtype.StartMining:
		err = minerInfo.StartMining(ctx)
	case commandtype.StopMining:
		err = minerInfo.StopMining(ctx)
	case commandtype.SetCoolingMode:
		var p dto.CoolingModePayload
		coolingExtractErr := json.Unmarshal(message.Payload, &p)
		if coolingExtractErr != nil {
			return fleeterror.NewInternalErrorf("error unmarshalling command payload: %v", coolingExtractErr)
		}
		err = minerInfo.SetCoolingMode(ctx, p)
	case commandtype.SetPowerTarget:
		var p dto.PowerTargetPayload
		powerExtractErr := json.Unmarshal(message.Payload, &p)
		if powerExtractErr != nil {
			return fleeterror.NewInternalErrorf("error unmarshalling command payload: %v", powerExtractErr)
		}
		err = minerInfo.SetPowerTarget(ctx, p)
	case commandtype.UpdateMiningPools:
		var p dto.UpdateMiningPoolsPayload
		updateExtractErr := json.Unmarshal(message.Payload, &p)
		if updateExtractErr != nil {
			return fleeterror.NewInternalErrorf("error unmarshalling command payload: %v", updateExtractErr)
		}
		err = minerInfo.UpdateMiningPools(ctx, p)
	case commandtype.DownloadLogs:
		err = minerInfo.DownloadLogs(ctx, message.BatchLogUUID)
	case commandtype.BlinkLED:
		err = minerInfo.BlinkLED(ctx)
	case commandtype.FirmwareUpdate:
		var p dto.FirmwareUpdatePayload
		if fwErr := json.Unmarshal(message.Payload, &p); fwErr != nil {
			err = fleeterror.NewInternalErrorf("error unmarshalling firmware update payload: %v", fwErr)
			break
		}
		reader, filename, size, openErr := es.filesService.OpenFirmwareFile(p.FirmwareFileID)
		if openErr != nil {
			err = fleeterror.NewInternalErrorf("error opening firmware file: %v", openErr)
			break
		}
		defer reader.Close()
		filePath, pathErr := es.filesService.GetFirmwareFilePath(p.FirmwareFileID)
		if pathErr != nil {
			err = fleeterror.NewInternalErrorf("error resolving firmware file path: %v", pathErr)
			break
		}
		err = minerInfo.FirmwareUpdate(ctx, sdk.FirmwareFile{
			Reader:   reader,
			Filename: filename,
			Size:     size,
			FilePath: filePath,
		})
	case commandtype.Unpair:
		err = minerInfo.Unpair(ctx)
		if err == nil {
			if unpairErr := es.handleUnpairPostProcessing(ctx, message.DeviceID); unpairErr != nil {
				slog.Error("unpair post-processing failed", "device_id", message.DeviceID, "error", unpairErr)
				err = unpairErr
			}
		}
	case commandtype.UpdateMinerPassword:
		var p dto.UpdateMinerPasswordPayload
		credExtractErr := json.Unmarshal(message.Payload, &p)
		if credExtractErr != nil {
			return fleeterror.NewInternalErrorf("error unmarshalling command payload: %v", credExtractErr)
		}

		// Update device via plugin
		err = minerInfo.UpdateMinerPassword(ctx, p)
		if err != nil {
			break
		}

		// Store updated credentials for devices that use basic auth (not asymmetric/JWT auth)
		if minerInfo.GetDriverName() != models.DriverNameProto {
			if dbErr := es.updateMinerPasswordInDB(ctx, message.DeviceID, p.NewPassword); dbErr != nil {
				slog.Error("device password updated but database sync failed",
					"device_id", message.DeviceID, "error", dbErr)
			}
		}
	default:
		return fleeterror.NewInternalErrorf("unsupported command type: %v", commandType)
	}

	if err != nil {
		slog.Error("command execution failed", "command", commandType, "device_id", message.DeviceID, "batch_uuid", message.BatchLogUUID, "error", err)
		if markErr := es.messageQueue.MarkFailed(ctx, message.ID, err.Error()); markErr != nil {
			return fleeterror.NewInternalErrorf("error setting message as failed on queue: %v (original error: %v)", markErr, err)
		}
		return err
	}
	err = es.messageQueue.MarkSuccess(ctx, message.ID)
	if err != nil {
		return fleeterror.NewInternalErrorf("error setting message as success on queue: %v", err)
	}
	return nil
}

// handleUnpairPostProcessing updates device pairing status and unregisters from telemetry after successful unpair
func (es *ExecutionService) handleUnpairPostProcessing(ctx context.Context, deviceID int64) error {
	deviceIdentifier, err := db.WithTransaction(ctx, es.conn, func(q *sqlc.Queries) (string, error) {
		return q.GetDeviceIdentifierByID(ctx, deviceID)
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to get device identifier by ID: %v", err)
	}

	err = es.deviceStore.UpdateDevicePairingStatusByIdentifier(ctx, deviceIdentifier, string(sqlc.PairingStatusEnumUNPAIRED))
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update device pairing status to UNPAIRED: %v", err)
	}

	slog.Info("device pairing status updated to UNPAIRED", "device_identifier", deviceIdentifier)

	if es.telemetryListener != nil {
		if err := es.telemetryListener.RemoveDevices(ctx, tmodels.DeviceIdentifier(deviceIdentifier)); err != nil {
			return fleeterror.NewInternalErrorf("failed to unregister device from telemetry: %v", err)
		}
		slog.Info("device unregistered from telemetry", "device_identifier", deviceIdentifier)
	}

	return nil
}

// updateMinerPasswordInDB encrypts and stores the miner password in the database
// after successful password update on the device. Username remains unchanged.
func (es *ExecutionService) updateMinerPasswordInDB(ctx context.Context, deviceID int64, password string) error {
	passwordEnc, err := es.encryptService.Encrypt([]byte(password))
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to encrypt password: %v", err)
	}

	rowsAffected, err := db.WithTransaction(ctx, es.conn, func(q *sqlc.Queries) (int64, error) {
		return q.UpdateMinerPassword(ctx, sqlc.UpdateMinerPasswordParams{
			PasswordEnc: passwordEnc,
			DeviceID:    deviceID,
		})
	})
	if err != nil {
		return err
	}

	// If no rows were affected, credentials don't exist for this device (data integrity issue)
	if rowsAffected == 0 {
		return fleeterror.NewInternalErrorf("no credentials found for device %d - cannot update password", deviceID)
	}

	return nil
}
