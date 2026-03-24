package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/proto-at-block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
	tmodels "github.com/proto-at-block/proto-fleet/server/internal/domain/telemetry/models"

	"github.com/proto-at-block/proto-fleet/server/generated/sqlc"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/commandtype"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	tokenDomain "github.com/proto-at-block/proto-fleet/server/internal/domain/token"
	sdk "github.com/proto-at-block/proto-fleet/server/sdk/v1"

	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/db"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/files"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/queue"
)

const dbWriteTimeout = 10 * time.Second

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
	reaperCancel          context.CancelFunc
}

func NewExecutionService(ctx context.Context, config *Config, conn *sql.DB, messageQueue queue.MessageQueue, encryptService *encrypt.Service, tokenService *tokenDomain.Service, minerService MinerGetter, deviceStore stores.DeviceStore, telemetryListener TelemetryListener, filesService *files.Service) *ExecutionService {
	if config.StuckMessageTimeout <= 0 {
		config.StuckMessageTimeout = 5 * time.Minute
	}
	if config.ReaperInterval <= 0 {
		config.ReaperInterval = 30 * time.Second
	}
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

	if es.reaperCancel != nil {
		es.reaperCancel()
	}
	reaperCtx, reaperCancel := context.WithCancel(ctx)
	es.reaperCancel = reaperCancel

	go es.startStuckMessageReaper(reaperCtx)

	// Start the queue processor thread
	go func() {
		err := es.startQueueProcessorThread(ctx)
		reaperCancel()
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

func (es *ExecutionService) startStuckMessageReaper(ctx context.Context) {
	ticker := time.NewTicker(es.config.ReaperInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if es.conn == nil {
				continue
			}
			reapCtx, reapCancel := context.WithTimeout(ctx, dbWriteTimeout)
			count, err := es.reapStuckMessages(reapCtx)
			reapCancel()
			if err != nil {
				slog.Error("stuck message reaper error", "error", err)
				continue
			}
			if count > 0 {
				slog.Warn("stuck message reaper moved messages to FAILED", "count", count)
			}
		}
	}
}

// reapStuckMessages atomically marks stuck PROCESSING messages as FAILED and
// writes the corresponding audit log entries in a single transaction.
func (es *ExecutionService) reapStuckMessages(ctx context.Context) (int, error) {
	cutoff := time.Now().Add(-es.config.StuckMessageTimeout)
	var count int
	err := db.WithTransactionNoResult(ctx, es.conn, func(q *sqlc.Queries) error {
		reaped, err := q.ReapStuckProcessingMessages(ctx, sqlc.ReapStuckProcessingMessagesParams{
			Cutoff:    cutoff,
			ReapLimit: 100,
		})
		if err != nil {
			return err
		}
		count = len(reaped)
		for _, msg := range reaped {
			if err := q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
				Uuid:      msg.CommandBatchLogUuid,
				DeviceID:  msg.DeviceID,
				Status:    sqlc.DeviceCommandStatusEnumFAILED,
				UpdatedAt: time.Now(),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return count, err
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
	// Step 1: Execute the command (pure execution, no queue status side-effects).
	workerError := es.executeCommandOnDevice(ctx, message.CommandType, message)

	// Step 2: Atomically update queue status AND write device log in a single transaction.
	// If the queue row is no longer PROCESSING (reaped), the transaction commits
	// as a no-op and neither the queue status nor the device log is modified.
	dbCtx, dbCancel := context.WithTimeout(context.WithoutCancel(ctx), dbWriteTimeout)
	defer dbCancel()

	txErr := db.WithTransactionNoResult(dbCtx, es.conn, func(q *sqlc.Queries) error {
		// First: transition queue_message status (detects staleness via rowsAffected).
		updated, err := es.markQueueMessageStatus(dbCtx, q, message.ID, workerError)
		if err != nil {
			return err
		}
		if !updated {
			slog.Warn("skipping audit log for stale message",
				"message_id", message.ID, "device_id", message.DeviceID)
			return nil
		}

		// Second: write device log only if the queue transition succeeded.
		return q.UpsertCommandOnDeviceLog(dbCtx, sqlc.UpsertCommandOnDeviceLogParams{
			Uuid:      message.BatchLogUUID,
			DeviceID:  message.DeviceID,
			Status:    upsertCommandOnDeviceStatus(workerError),
			UpdatedAt: time.Now(),
		})
	})
	if txErr != nil {
		slog.Error("error in post-execution transaction",
			"message_id", message.ID, "error", txErr)
	}
}

// markQueueMessageStatus transitions the queue_message to its next state within an
// existing transaction. Returns (true, nil) on success, (false, nil) when the row
// is no longer PROCESSING (stale/reaped), or (false, err) on DB error.
func (es *ExecutionService) markQueueMessageStatus(ctx context.Context, q *sqlc.Queries, messageID int64, workerError error) (bool, error) {
	var result sql.Result
	var err error

	switch {
	case workerError == nil:
		result, err = q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{
			ID:     messageID,
			Status: sqlc.QueueStatusEnumSUCCESS,
		})
	case fleeterror.IsUnimplementedError(workerError):
		result, err = q.UpdateMessagePermanentlyFailed(ctx, sqlc.UpdateMessagePermanentlyFailedParams{
			ID:        messageID,
			ErrorInfo: sql.NullString{String: workerError.Error(), Valid: true},
		})
	default:
		result, err = q.UpdateMessageAfterFailure(ctx, sqlc.UpdateMessageAfterFailureParams{
			ID:         messageID,
			RetryCount: es.messageQueue.MaxFailureRetries(),
			ErrorInfo:  sql.NullString{String: workerError.Error(), Valid: true},
		})
	}

	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to update queue message status: %v", err)
	}
	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// executeCommandOnDevice runs the command and returns the execution error (if any).
// It does NOT mark queue message status — the caller is responsible for that.
func (es *ExecutionService) executeCommandOnDevice(ctx context.Context, commandType commandtype.Type, message queue.Message) error {
	minerInfo, err := es.minerService.GetMiner(ctx, message.DeviceID)
	if err != nil {
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
	}
	return err
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
