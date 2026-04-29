package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleetoptions"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/domain/workername"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	tokenDomain "github.com/block/proto-fleet/server/internal/domain/token"
	sdk "github.com/block/proto-fleet/server/sdk/v1"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

const (
	dbWriteTimeout          = 10 * time.Second
	workerNameLookupTimeout = 5 * time.Second
)

//go:generate go run go.uber.org/mock/mockgen -source=execution_service.go -destination=mocks/mock_miner_getter.go -package=mocks MinerGetter,CachedMinerGetter
type MinerGetter interface {
	GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error)
}

// CachedMinerGetter extends MinerGetter with cache invalidation. Services that
// both fetch miners and need to evict stale handles should use this interface.
type CachedMinerGetter interface {
	MinerGetter
	// InvalidateMiner removes the cached miner handle for the given device identifier.
	// Call this when credentials change or a device is unpaired so subsequent lookups
	// fetch fresh state from DB.
	InvalidateMiner(deviceIdentifier models.DeviceIdentifier)
}

type ExecutionService struct {
	config *Config

	conn              *sql.DB
	messageQueue      queue.MessageQueue
	encryptService    *encrypt.Service
	tokenService      *tokenDomain.Service
	minerService      CachedMinerGetter
	deviceStore       stores.DeviceStore
	telemetryListener TelemetryListener
	filesService      *files.Service

	// optionsCache lets unpair commands invalidate stale model/firmware
	// dropdowns served by ListMinerStateSnapshots. Nil-safe.
	optionsCache *fleetoptions.Cache

	workerSemaphore chan struct{}

	queueProcessorMu      sync.Mutex
	queueProcessorRunning bool
	reaperCancel          context.CancelFunc
}

// WithOptionsCache wires the per-org fleet options cache so successful
// unpair commands can evict stale option arrays. Pass nil to disable.
func (es *ExecutionService) WithOptionsCache(cache *fleetoptions.Cache) {
	es.optionsCache = cache
}

func NewExecutionService(ctx context.Context, config *Config, conn *sql.DB, messageQueue queue.MessageQueue, encryptService *encrypt.Service, tokenService *tokenDomain.Service, minerService CachedMinerGetter, deviceStore stores.DeviceStore, telemetryListener TelemetryListener, filesService *files.Service) *ExecutionService {
	if config.StuckMessageTimeout <= 0 {
		config.StuckMessageTimeout = 5 * time.Minute
	}
	if config.ReaperInterval <= 0 {
		config.ReaperInterval = 30 * time.Second
	}
	if config.FirmwareUpdateTimeout <= 0 {
		config.FirmwareUpdateTimeout = 15 * time.Minute
	}
	if config.FirmwareUpdateStuckTimeout <= 0 {
		config.FirmwareUpdateStuckTimeout = 20 * time.Minute
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
			count, fwDeviceIDs, err := es.reapStuckMessages(reapCtx)
			reapCancel()
			if err != nil {
				slog.Error("stuck message reaper error", "error", err)
				continue
			}
			if count > 0 {
				slog.Warn("stuck message reaper moved messages to FAILED", "count", count)
			}
			for _, deviceID := range fwDeviceIDs {
				es.clearFirmwareUpdateStatus(ctx, deviceID)
			}
		}
	}
}

// reapStuckMessages atomically marks stuck PROCESSING messages as FAILED and
// writes the corresponding audit log entries in a single transaction.
// Firmware update messages use a longer cutoff since they include install polling.
// Returns the total count of reaped messages and the device IDs from reaped
// firmware update messages (so callers can clean up stuck device statuses).
func (es *ExecutionService) reapStuckMessages(ctx context.Context) (int, []int64, error) {
	cutoff := time.Now().Add(-es.config.StuckMessageTimeout)
	var count int
	var fwDeviceIDs []int64
	err := db.WithTransactionNoResult(ctx, es.conn, func(q *sqlc.Queries) error {
		reaped, err := q.ReapStuckProcessingMessages(ctx, sqlc.ReapStuckProcessingMessagesParams{
			Cutoff:    cutoff,
			ReapLimit: 100,
		})
		if err != nil {
			return err
		}

		fwCutoff := time.Now().Add(-es.config.FirmwareUpdateStuckTimeout)
		fwReaped, err := q.ReapStuckFirmwareUpdateMessages(ctx, sqlc.ReapStuckFirmwareUpdateMessagesParams{
			Cutoff:    fwCutoff,
			ReapLimit: 100,
		})
		if err != nil {
			return err
		}

		count = len(reaped) + len(fwReaped)
		for _, msg := range reaped {
			if err := q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
				Uuid:      msg.CommandBatchLogUuid,
				DeviceID:  msg.DeviceID,
				Status:    sqlc.DeviceCommandStatusEnumFAILED,
				UpdatedAt: time.Now(),
				ErrorInfo: msg.ErrorInfo,
			}); err != nil {
				return err
			}
		}
		for _, msg := range fwReaped {
			if err := q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
				Uuid:      msg.CommandBatchLogUuid,
				DeviceID:  msg.DeviceID,
				Status:    sqlc.DeviceCommandStatusEnumFAILED,
				UpdatedAt: time.Now(),
				ErrorInfo: msg.ErrorInfo,
			}); err != nil {
				return err
			}
			fwDeviceIDs = append(fwDeviceIDs, msg.DeviceID)
		}
		return nil
	})
	return count, fwDeviceIDs, err
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

					timeout := es.config.WorkerExecutionTimeout
					if msg.CommandType == commandtype.FirmwareUpdate {
						timeout = es.config.FirmwareUpdateTimeout
					}
					workerCtx, cancel := context.WithTimeout(ctx, timeout)
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
		// Persist a sanitized reason so the activity-log detail RPC can surface
		// it to org members without leaking raw plugin/device-controlled text.
		// The raw err.Error() is still logged below via slog.Error for admins.
		if err := q.UpsertCommandOnDeviceLog(dbCtx, sqlc.UpsertCommandOnDeviceLogParams{
			Uuid:      message.BatchLogUUID,
			DeviceID:  message.DeviceID,
			Status:    upsertCommandOnDeviceStatus(workerError),
			UpdatedAt: time.Now(),
			ErrorInfo: sanitizedErrorInfo(workerError),
		}); err != nil {
			return err
		}

		return nil
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
	case fleeterror.IsUnimplementedError(workerError),
		fleeterror.IsFailedPreconditionError(workerError):
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
		if err == nil {
			es.clearFirmwareUpdateStatus(ctx, message.DeviceID)
		}
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
		var workerNameToPersist string
		if p.ReapplyCurrentPoolsWithStoredWorkerName {
			var shouldUpdate bool
			p, workerNameToPersist, shouldUpdate, err = es.reapplyCurrentPoolsWithDesiredWorkerName(ctx, minerInfo, p)
			if err != nil {
				break
			}
			if !shouldUpdate {
				if workerNameToPersist == "" {
					return nil
				}
				return es.persistWorkerNameAfterPoolUpdate(ctx, message.DeviceID, minerInfo.GetID(), workerNameToPersist)
			}
		} else {
			p, err = es.applyMinerNameToPoolUsernames(ctx, minerInfo, p)
			if err != nil {
				break
			}
		}
		err = minerInfo.UpdateMiningPools(ctx, p)
		if err == nil && workerNameToPersist != "" {
			err = es.persistWorkerNameAfterPoolUpdate(ctx, message.DeviceID, minerInfo.GetID(), workerNameToPersist)
			if err != nil {
				err = fleeterror.NewInternalErrorf("failed to persist worker name after pool update: %v", err)
			}
		}
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
		if err != nil {
			break
		}
		err = es.pollFirmwareInstallStatus(ctx, minerInfo, message.DeviceID)
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

		// Only evict after a successful DB write: the cached handle already has the new
		// credentials and is the only valid session if DB sync fails.
		// Proto devices (asymmetric auth) store no password in DB, so always evict.
		if minerInfo.GetDriverName() != models.DriverNameProto {
			if dbErr := es.updateMinerPasswordInDB(ctx, message.DeviceID, p.NewPassword); dbErr != nil {
				slog.Error("device password updated but database sync failed",
					"device_id", message.DeviceID, "error", dbErr)
				break
			}
		}
		// Evict so the next lookup re-reads updated credentials from DB.
		es.minerService.InvalidateMiner(minerInfo.GetID())
	default:
		return fleeterror.NewInternalErrorf("unsupported command type: %v", commandType)
	}

	if err != nil {
		if fleeterror.IsAuthenticationError(err) {
			es.minerService.InvalidateMiner(minerInfo.GetID())
		}
		slog.Error("command execution failed", "command", commandType, "device_id", message.DeviceID, "batch_uuid", message.BatchLogUUID, "error", err)
	}
	return err
}

func (es *ExecutionService) applyMinerNameToPoolUsernames(
	ctx context.Context,
	minerInfo interfaces.Miner,
	payload dto.UpdateMiningPoolsPayload,
) (dto.UpdateMiningPoolsPayload, error) {
	if !payloadRequiresMinerName(payload) {
		return payload, nil
	}

	minerName, err := es.getMinerWorkerName(ctx, minerInfo)
	if err != nil {
		return dto.UpdateMiningPoolsPayload{}, err
	}
	if minerName == "" {
		return payload, nil
	}

	payload.DefaultPool.Username = appendMinerNameToPoolUsername(payload.DefaultPool, minerName)
	if payload.Backup1Pool != nil {
		payload.Backup1Pool.Username = appendMinerNameToPoolUsername(*payload.Backup1Pool, minerName)
	}
	if payload.Backup2Pool != nil {
		payload.Backup2Pool.Username = appendMinerNameToPoolUsername(*payload.Backup2Pool, minerName)
	}

	return payload, nil
}

func (es *ExecutionService) reapplyCurrentPoolsWithDesiredWorkerName(
	ctx context.Context,
	minerInfo interfaces.Miner,
	payload dto.UpdateMiningPoolsPayload,
) (dto.UpdateMiningPoolsPayload, string, bool, error) {
	desiredWorkerName, err := es.getDesiredWorkerNameForPoolReapply(ctx, minerInfo, payload)
	if err != nil {
		return dto.UpdateMiningPoolsPayload{}, "", false, err
	}
	if desiredWorkerName == "" {
		return dto.UpdateMiningPoolsPayload{}, "", false, nil
	}

	currentPools, err := minerInfo.GetMiningPools(ctx)
	if err != nil {
		return dto.UpdateMiningPoolsPayload{}, "", false, fleeterror.NewInternalErrorf("failed to read current mining pools for worker-name reapply: %v", err)
	}
	if len(currentPools) == 0 {
		return dto.UpdateMiningPoolsPayload{}, desiredWorkerName, false, nil
	}

	return buildCurrentPoolsPayloadWithWorkerName(currentPools, desiredWorkerName), desiredWorkerName, true, nil
}

func payloadRequiresMinerName(payload dto.UpdateMiningPoolsPayload) bool {
	if shouldAppendMinerName(payload.DefaultPool) {
		return true
	}
	if payload.Backup1Pool != nil && shouldAppendMinerName(*payload.Backup1Pool) {
		return true
	}
	return payload.Backup2Pool != nil && shouldAppendMinerName(*payload.Backup2Pool)
}

func (es *ExecutionService) getMinerWorkerName(ctx context.Context, minerInfo interfaces.Miner) (string, error) {
	lookupCtx, cancel := workerNameLookupContext(ctx)
	defer cancel()

	if workerName, err := currentMinerWorkerName(lookupCtx, minerInfo); err != nil {
		slog.Debug("failed to read current mining pools for worker-name lookup",
			"device_id", minerInfo.GetID(),
			"error", err)
	} else if workerName != "" {
		return workerName, nil
	}

	props, err := es.deviceStore.GetDevicePropertiesForRename(
		ctx,
		minerInfo.GetOrgID(),
		[]string{string(minerInfo.GetID())},
		false,
	)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to get miner name for pool assignment: %v", err)
	}
	if len(props) == 0 {
		return "", fleeterror.NewNotFoundErrorf("device properties not found for device %s", minerInfo.GetID())
	}

	return storedMinerWorkerName(props[0]), nil
}

func (es *ExecutionService) getStoredWorkerName(ctx context.Context, minerInfo interfaces.Miner) (string, error) {
	props, err := es.deviceStore.GetDevicePropertiesForRename(
		ctx,
		minerInfo.GetOrgID(),
		[]string{string(minerInfo.GetID())},
		false,
	)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to get stored worker name for pool reapply: %v", err)
	}
	if len(props) == 0 {
		return "", fleeterror.NewNotFoundErrorf("device properties not found for device %s", minerInfo.GetID())
	}

	return strings.TrimSpace(props[0].WorkerName), nil
}

func (es *ExecutionService) getDesiredWorkerNameForPoolReapply(
	ctx context.Context,
	minerInfo interfaces.Miner,
	payload dto.UpdateMiningPoolsPayload,
) (string, error) {
	if workerName := strings.TrimSpace(payload.DesiredWorkerName); workerName != "" {
		return workerName, nil
	}

	return es.getStoredWorkerName(ctx, minerInfo)
}

func currentMinerWorkerName(ctx context.Context, minerInfo interfaces.Miner) (string, error) {
	pools, err := minerInfo.GetMiningPools(ctx)
	if err != nil {
		return "", err
	}

	return configuredMinerWorkerName(pools), nil
}

func workerNameLookupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := workerNameLookupTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.WithCancel(ctx)
		}

		// Keep at least half of the remaining worker deadline for the actual pool update.
		lookupBudget := remaining / 2
		if lookupBudget <= 0 {
			lookupBudget = remaining
		}
		if lookupBudget < timeout {
			timeout = lookupBudget
		}
	}

	return context.WithTimeout(ctx, timeout)
}

func configuredMinerWorkerName(pools []interfaces.MinerConfiguredPool) string {
	primaryPool, ok := primaryConfiguredMinerPool(pools)
	if !ok {
		return ""
	}

	return workername.FromPoolUsername(primaryPool.Username)
}

func primaryConfiguredMinerPool(pools []interfaces.MinerConfiguredPool) (interfaces.MinerConfiguredPool, bool) {
	if len(pools) == 0 {
		return interfaces.MinerConfiguredPool{}, false
	}

	primaryPool := pools[0]
	for _, pool := range pools[1:] {
		if pool.Priority < primaryPool.Priority {
			primaryPool = pool
		}
	}

	return primaryPool, true
}

func storedMinerWorkerName(props stores.DeviceRenameProperties) string {
	if workerName := strings.TrimSpace(props.WorkerName); workerName != "" {
		return workerName
	}

	return strings.TrimSpace(props.MacAddress)
}

func (es *ExecutionService) persistWorkerNameAfterPoolUpdate(
	ctx context.Context,
	deviceID int64,
	deviceIdentifier models.DeviceIdentifier,
	workerName string,
) error {
	if es.conn == nil {
		return es.deviceStore.UpdateWorkerName(ctx, deviceIdentifier, workerName)
	}

	return db.WithTransactionNoResult(ctx, es.conn, func(q *sqlc.Queries) error {
		affected, err := q.UpdateDeviceWorkerName(ctx, sqlc.UpdateDeviceWorkerNameParams{
			DeviceIdentifier: string(deviceIdentifier),
			WorkerName:       sql.NullString{String: workerName, Valid: workerName != ""},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to update worker name for device %s: %v", deviceIdentifier, err)
		}
		if affected == 0 {
			return fleeterror.NewNotFoundErrorf("device not found for worker name update with identifier=%s", deviceIdentifier)
		}

		return q.UpdateDeviceWorkerNamePoolSyncStatusByID(ctx, sqlc.UpdateDeviceWorkerNamePoolSyncStatusByIDParams{
			ID: deviceID,
			WorkerNamePoolSyncStatus: sqlc.NullWorkerNamePoolSyncStatusEnum{
				WorkerNamePoolSyncStatusEnum: sqlc.WorkerNamePoolSyncStatusEnum(workername.PoolSyncStatusPoolUpdatedSuccessfully),
				Valid:                        true,
			},
		})
	})
}

func buildCurrentPoolsPayloadWithWorkerName(
	currentPools []interfaces.MinerConfiguredPool,
	desiredWorkerName string,
) dto.UpdateMiningPoolsPayload {
	sortedPools := sortedConfiguredPoolsByPriority(currentPools)
	payload := dto.UpdateMiningPoolsPayload{
		DefaultPool: configuredPoolToPayload(sortedPools[0], desiredWorkerName),
	}

	if len(sortedPools) > 1 {
		backup := configuredPoolToPayload(sortedPools[1], desiredWorkerName)
		payload.Backup1Pool = &backup
	}
	if len(sortedPools) > 2 {
		backup := configuredPoolToPayload(sortedPools[2], desiredWorkerName)
		payload.Backup2Pool = &backup
	}

	return payload
}

func sortedConfiguredPoolsByPriority(currentPools []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
	sortedPools := append([]interfaces.MinerConfiguredPool(nil), currentPools...)
	sort.SliceStable(sortedPools, func(i, j int) bool {
		return sortedPools[i].Priority < sortedPools[j].Priority
	})
	return sortedPools
}

func configuredPoolToPayload(
	pool interfaces.MinerConfiguredPool,
	desiredWorkerName string,
) dto.MiningPool {
	priority := uint32(0)
	if pool.Priority > 0 {
		priority = uint32(pool.Priority)
	}

	return dto.MiningPool{
		Priority: priority,
		URL:      pool.URL,
		Username: rewritePoolUsernameWithStoredWorkerName(pool.Username, desiredWorkerName),
	}
}

func rewritePoolUsernameWithStoredWorkerName(username string, desiredWorkerName string) string {
	trimmedUsername := strings.TrimSpace(username)
	if trimmedUsername == "" || desiredWorkerName == "" {
		return trimmedUsername
	}

	baseUsername := normalizePoolUsernameBase(trimmedUsername)
	if baseUsername == "" {
		return trimmedUsername
	}

	return baseUsername + "." + desiredWorkerName
}

func appendMinerNameToPoolUsername(pool dto.MiningPool, minerName string) string {
	if !shouldAppendMinerName(pool) {
		return pool.Username
	}

	baseUsername := normalizePoolUsernameBase(pool.Username)
	if baseUsername == "" {
		return pool.Username
	}

	return baseUsername + "." + minerName
}

func shouldAppendMinerName(pool dto.MiningPool) bool {
	return pool.AppendMinerName && shouldAppendMinerNameToUsername(pool.Username)
}

func shouldAppendMinerNameToUsername(username string) bool {
	trimmed := strings.TrimSpace(username)
	return trimmed != "" && !strings.Contains(trimmed, ".")
}

func normalizePoolUsernameBase(username string) string {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return ""
	}

	firstSeparator := strings.Index(trimmed, ".")
	if firstSeparator <= 0 || firstSeparator == len(trimmed)-1 {
		return trimmed
	}

	return strings.TrimSpace(trimmed[:firstSeparator])
}

// handleUnpairPostProcessing updates device pairing status and unregisters from telemetry after successful unpair
func (es *ExecutionService) handleUnpairPostProcessing(ctx context.Context, deviceID int64) error {
	row, err := db.WithTransaction(ctx, es.conn, func(q *sqlc.Queries) (sqlc.GetDeviceIdentifierAndOrgIDByIDRow, error) {
		return q.GetDeviceIdentifierAndOrgIDByID(ctx, deviceID)
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to get device identifier by ID: %v", err)
	}
	deviceIdentifier := row.DeviceIdentifier

	err = es.deviceStore.UpdateDevicePairingStatusByIdentifier(ctx, deviceIdentifier, string(sqlc.PairingStatusEnumUNPAIRED))
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update device pairing status to UNPAIRED: %v", err)
	}

	slog.Info("device pairing status updated to UNPAIRED", "device_identifier", deviceIdentifier)

	// Drop the org's cached option arrays — UNPAIRED leaves the PAIRED set
	// scanned by GetAvailableModels / GetAvailableFirmwareVersions, so the
	// dropdown could otherwise show ghost values until TTL expiry.
	es.optionsCache.Invalidate(row.OrgID)

	// Evict the cached miner immediately. This is unconditional so that any
	// command queued after this point always fetches fresh state from the DB,
	// regardless of whether the telemetry cleanup below succeeds.
	es.minerService.InvalidateMiner(models.DeviceIdentifier(deviceIdentifier))

	if es.telemetryListener != nil {
		// Hard failure: if the scheduler cannot remove the device it will keep
		// polling an UNPAIRED device, and continued auth failures can flip the
		// pairing status away from UNPAIRED. Return an error so the command queue
		// retries until cleanup succeeds.
		if err := es.telemetryListener.RemoveDevices(ctx, tmodels.DeviceIdentifier(deviceIdentifier)); err != nil {
			return fleeterror.NewInternalErrorf("failed to unregister device from telemetry after unpair: %v", err)
		}
		slog.Info("device unregistered from telemetry", "device_identifier", deviceIdentifier)
	}

	return nil
}

const (
	firmwareInstallPollInterval = 10 * time.Second
	firmwareInstallGraceWindow  = 60 * time.Second
)

var errPollComplete = errors.New("polling complete")

// clearFirmwareUpdateStatus resets the device status from REBOOT_REQUIRED back to ACTIVE
// after a successful reboot command, allowing telemetry to take over status management.
func (es *ExecutionService) clearFirmwareUpdateStatus(ctx context.Context, deviceID int64) {
	if es.conn == nil {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	deviceIdentifier, err := db.WithTransaction(cleanupCtx, es.conn, func(q *sqlc.Queries) (string, error) {
		return q.GetDeviceIdentifierByID(cleanupCtx, deviceID)
	})
	if err != nil {
		slog.Warn("failed to resolve device identifier for firmware status cleanup", "device_id", deviceID, "error", err)
		return
	}
	devID := tmodels.DeviceIdentifier(deviceIdentifier)

	currentStatuses, err := es.deviceStore.GetDeviceStatusForDeviceIdentifiers(cleanupCtx, []tmodels.DeviceIdentifier{devID})
	if err != nil {
		slog.Warn("failed to read current device status for firmware status cleanup", "device_id", deviceID, "error", err)
		return
	}
	currentStatus, ok := currentStatuses[devID]
	if !ok || (currentStatus != models.MinerStatusRebootRequired && currentStatus != models.MinerStatusUpdating) {
		return
	}

	var upsertErr error
	for attempt := range 3 {
		upsertErr = es.deviceStore.UpsertDeviceStatus(cleanupCtx, devID, models.MinerStatusActive, "")
		if upsertErr == nil {
			slog.Info("cleared firmware update status after reboot", "device_id", deviceID, "previous_status", currentStatus)
			return
		}
		slog.Warn("failed to clear firmware update status after reboot, retrying",
			"device_id", deviceID, "attempt", attempt+1, "error", upsertErr)
		select {
		case <-cleanupCtx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
	slog.Error("permanently failed to clear firmware update status after reboot",
		"device_id", deviceID, "error", upsertErr)
}

// pollFirmwareInstallStatus polls the rig's install status after a successful firmware
// upload until installation completes or fails. The device status is set to UPDATING
// only after the probe confirms the device supports install status reporting, then
// transitions to REBOOT_REQUIRED on success.
// Returns nil on successful install, or an error if installation fails or times out.
// For miners that don't support install status polling, returns nil immediately.
func (es *ExecutionService) pollFirmwareInstallStatus(ctx context.Context, minerInfo interfaces.Miner, deviceID int64) error {
	provider, canPoll := minerInfo.(interfaces.FirmwareUpdateStatusProvider)
	if !canPoll {
		return nil
	}

	probeStatus, probeErr := provider.GetFirmwareUpdateStatus(ctx)
	if probeErr == nil && probeStatus == nil {
		slog.Info("firmware update status provider does not report install status, skipping polling", "device_id", deviceID)
		return nil
	}

	deviceIdentifier, err := db.WithTransaction(ctx, es.conn, func(q *sqlc.Queries) (string, error) {
		return q.GetDeviceIdentifierByID(ctx, deviceID)
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to resolve device identifier for firmware status polling: %v", err)
	}

	devID := tmodels.DeviceIdentifier(deviceIdentifier)

	pollResult := es.doPollFirmwareInstall(ctx, provider, devID, deviceID, probeStatus, probeErr)

	if pollResult != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if upsertErr := es.deviceStore.UpsertDeviceStatus(cleanupCtx, devID, models.MinerStatusActive, ""); upsertErr != nil {
			slog.Warn("failed to clear firmware update status after polling failure", "device_id", deviceID, "error", upsertErr)
		}
	}
	return pollResult
}

func (es *ExecutionService) doPollFirmwareInstall(ctx context.Context, provider interfaces.FirmwareUpdateStatusProvider, devID tmodels.DeviceIdentifier, deviceID int64, probeStatus *sdk.FirmwareUpdateStatus, probeErr error) error {
	if upsertErr := es.deviceStore.UpsertDeviceStatus(ctx, devID, models.MinerStatusUpdating, ""); upsertErr != nil {
		slog.Warn("failed to set device status to UPDATING", "device_id", deviceID, "error", upsertErr)
	}

	ticker := time.NewTicker(firmwareInstallPollInterval)
	defer ticker.Stop()
	uploadCompletedAt := time.Now()

	handleStatus := func(status *sdk.FirmwareUpdateStatus, pollErr error) error {
		if pollErr != nil {
			slog.Warn("firmware install status poll failed", "device_id", deviceID, "error", pollErr)
			return nil
		}
		if status == nil {
			return nil
		}

		switch status.State {
		case "installed", "success", "confirming":
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbWriteTimeout)
			defer cancel()
			if upsertErr := es.deviceStore.UpsertDeviceStatus(cleanupCtx, devID, models.MinerStatusRebootRequired, ""); upsertErr != nil {
				slog.Error("firmware install completed but failed to persist REBOOT_REQUIRED, treating as success to avoid re-upload", "device_id", deviceID, "error", upsertErr)
			}
			slog.Info("firmware install completed, reboot required", "device_id", deviceID)
			return errPollComplete

		case "installing", "downloaded":
			slog.Debug("firmware install in progress", "device_id", deviceID, "state", status.State, "progress", status.Progress)

		case "current":
			if status.Error != nil && *status.Error != "" {
				return fleeterror.NewInternalErrorf("firmware install failed on device %d: %s", deviceID, *status.Error)
			}
			if time.Since(uploadCompletedAt) > firmwareInstallGraceWindow {
				return fleeterror.NewInternalErrorf("firmware install reverted to 'current' on device %d (install may have failed silently)", deviceID)
			}
			slog.Debug("firmware install not started yet (grace window)", "device_id", deviceID)

		case "error":
			errMsg := "unknown error"
			if status.Error != nil && *status.Error != "" {
				errMsg = *status.Error
			}
			return fleeterror.NewInternalErrorf("firmware install failed on device %d: %s", deviceID, errMsg)

		default:
			slog.Debug("unexpected firmware install state", "device_id", deviceID, "state", status.State)
		}
		return nil
	}

	if result := handleStatus(probeStatus, probeErr); result != nil {
		if errors.Is(result, errPollComplete) {
			return nil
		}
		return result
	}

	for {
		select {
		case <-ctx.Done():
			return fleeterror.NewInternalErrorf("firmware install polling timed out for device %d", deviceID)
		case <-ticker.C:
			status, pollErr := provider.GetFirmwareUpdateStatus(ctx)
			if result := handleStatus(status, pollErr); result != nil {
				if errors.Is(result, errPollComplete) {
					return nil
				}
				return result
			}
		}
	}
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
