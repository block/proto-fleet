package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/domain/workername"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/credentialblob"
	tokenDomain "github.com/block/proto-fleet/server/internal/domain/token"
	sdk "github.com/block/proto-fleet/server/sdk/v1"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	dbWriteTimeout          = 10 * time.Second
	workerNameLookupTimeout = 5 * time.Second
)

//go:generate go run go.uber.org/mock/mockgen -source=execution_service.go -destination=mocks/mock_miner_getter.go -package=mocks MinerGetter,CachedMinerGetter
type MinerGetter interface {
	GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error)
	GetMinerForPasswordUpdate(ctx context.Context, deviceID int64, currentPassword string) (interfaces.Miner, error)
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
	metricsEmitter    MetricsEmitter

	workerSemaphore chan struct{}

	lifecycleMu sync.Mutex
	run         *executionRun
}

type executionRun struct {
	admissionCtx    context.Context //nolint:containedctx // scoped to this activation's admission phase
	cancelAdmission context.CancelFunc
	workCtx         context.Context //nolint:containedctx // scoped to this activation's drain phase
	cancelWork      context.CancelFunc
	accepting       bool
	wg              sync.WaitGroup
	done            chan struct{}
}

var _ runtimejobs.Lifecycle = (*ExecutionService)(nil)

var errExecutionStoppedBeforeEnqueue = errors.New("command execution service stopped before enqueue")

func newExecutionRun(ctx context.Context) *executionRun {
	admissionCtx, cancelAdmission := context.WithCancel(ctx)
	workCtx, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	return &executionRun{
		admissionCtx:    admissionCtx,
		cancelAdmission: cancelAdmission,
		workCtx:         workCtx,
		cancelWork:      cancelWork,
		accepting:       true,
		done:            make(chan struct{}),
	}
}

func NewExecutionService(config *Config, conn *sql.DB, messageQueue queue.MessageQueue, encryptService *encrypt.Service, tokenService *tokenDomain.Service, minerService CachedMinerGetter, deviceStore stores.DeviceStore, telemetryListener TelemetryListener, filesService *files.Service) *ExecutionService {
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
		config:            config,
		conn:              conn,
		messageQueue:      messageQueue,
		encryptService:    encryptService,
		tokenService:      tokenService,
		minerService:      minerService,
		deviceStore:       deviceStore,
		telemetryListener: telemetryListener,
		filesService:      filesService,
		metricsEmitter:    NoCommandMetrics(),
		workerSemaphore:   make(chan struct{}, config.MaxWorkers),
	}
}

func (es *ExecutionService) WithMetricsEmitter(emitter MetricsEmitter) *ExecutionService {
	if emitter == nil {
		emitter = NoCommandMetrics()
	}
	es.metricsEmitter = emitter
	return es
}

// Start runs command execution for the lifetime of ctx if it is not already
// active.
func (es *ExecutionService) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start command execution service: %w", err)
	}

	es.lifecycleMu.Lock()
	if es.run != nil {
		if !es.run.accepting || es.run.admissionCtx.Err() != nil {
			es.lifecycleMu.Unlock()
			return fmt.Errorf("command execution service activation is still draining")
		}
		es.lifecycleMu.Unlock()
		return nil
	}

	run := newExecutionRun(ctx)
	es.run = run

	run.wg.Go(func() {
		es.startStuckMessageReaper(run.admissionCtx)
	})

	// Start the queue processor thread
	run.wg.Go(func() {
		err := es.startQueueProcessorThread(run)
		es.beginStop(run)

		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("message processing stopped with error", "error", err)
		}
	})
	context.AfterFunc(run.admissionCtx, func() {
		es.beginStop(run)
	})
	go es.finishRun(run)
	es.lifecycleMu.Unlock()

	return nil
}

func (es *ExecutionService) beginStop(run *executionRun) {
	es.lifecycleMu.Lock()
	if es.run == run {
		run.accepting = false
	}
	es.lifecycleMu.Unlock()
	run.cancelAdmission()
}

func (es *ExecutionService) finishRun(run *executionRun) {
	run.wg.Wait()
	run.cancelWork()
	es.lifecycleMu.Lock()
	if es.run == run {
		es.run = nil
	}
	es.lifecycleMu.Unlock()
	close(run.done)
}

// Stop closes admission and waits for admitted work to drain. If ctx expires,
// it cancels that work before returning.
func (es *ExecutionService) Stop(ctx context.Context) error {
	es.lifecycleMu.Lock()
	run := es.run
	if run == nil {
		es.lifecycleMu.Unlock()
		return nil
	}
	run.accepting = false
	es.lifecycleMu.Unlock()
	run.cancelAdmission()

	select {
	case <-run.done:
		return nil
	case <-ctx.Done():
		run.cancelWork()
		return fmt.Errorf("stop command execution service: %w", ctx.Err())
	}
}

func (es *ExecutionService) IsRunning() bool {
	es.lifecycleMu.Lock()
	defer es.lifecycleMu.Unlock()

	return es.run != nil && es.run.accepting && es.run.admissionCtx.Err() == nil
}

// withAdmission makes accepting an enqueue atomic with stopping. Once
// admitted, the operation may drain after Stop begins and is canceled only if
// the stop context expires.
func (es *ExecutionService) withAdmission(ctx context.Context, fn func(context.Context) error) error {
	es.lifecycleMu.Lock()
	run := es.run
	if run == nil || !run.accepting || run.admissionCtx.Err() != nil {
		es.lifecycleMu.Unlock()
		return errExecutionStoppedBeforeEnqueue
	}
	run.wg.Add(1)
	es.lifecycleMu.Unlock()
	defer run.wg.Done()

	workCtx, cancel := context.WithCancelCause(ctx)
	stopCancel := context.AfterFunc(run.workCtx, func() {
		cancel(errExecutionStoppedBeforeEnqueue)
	})
	defer func() {
		stopCancel()
		cancel(context.Canceled)
	}()
	if run.workCtx.Err() != nil {
		cancel(errExecutionStoppedBeforeEnqueue)
	}

	err := fn(workCtx)
	if err != nil && errors.Is(context.Cause(workCtx), errExecutionStoppedBeforeEnqueue) {
		return errExecutionStoppedBeforeEnqueue
	}
	return err
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
			reaped, fwDeviceIDs, err := es.reapStuckMessages(reapCtx)
			reapCancel()
			if err != nil {
				slog.Error("stuck message reaper error", "error", err)
				continue
			}
			if len(reaped) > 0 {
				slog.Warn("stuck message reaper moved messages to FAILED", "count", len(reaped))
			}
			es.emitReapedCommandMetrics(ctx, reaped)
			for _, deviceID := range fwDeviceIDs {
				es.clearFirmwareUpdateStatus(ctx, deviceID)
			}
		}
	}
}

type reapedCommand struct {
	orgID       int64
	commandType string
}

// reapStuckMessages atomically marks stuck PROCESSING messages as FAILED and
// writes the corresponding audit log entries in a single transaction.
// Firmware update messages use a longer cutoff since they include install polling.
// Returns the reaped commands' metric metadata and the device IDs from reaped
// firmware update messages (so callers can clean up stuck device statuses).
func (es *ExecutionService) reapStuckMessages(ctx context.Context) ([]reapedCommand, []int64, error) {
	cutoff := time.Now().Add(-es.config.StuckMessageTimeout)
	var reapedCmds []reapedCommand
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

		reapedCmds = make([]reapedCommand, 0, len(reaped)+len(fwReaped))
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
			reapedCmds = append(reapedCmds, reapedCommand{orgID: msg.OrgID, commandType: msg.CommandType})
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
			reapedCmds = append(reapedCmds, reapedCommand{orgID: msg.OrgID, commandType: msg.CommandType})
			fwDeviceIDs = append(fwDeviceIDs, msg.DeviceID)
		}
		return nil
	})
	return reapedCmds, fwDeviceIDs, err
}

var errReapedStuck = errors.New("reaped: stuck in PROCESSING beyond timeout")

// records a result="failure" sample for each reaped command.
func (es *ExecutionService) emitReapedCommandMetrics(ctx context.Context, reaped []reapedCommand) {
	if len(reaped) == 0 || es.metricsEmitter == nil {
		return
	}
	for _, r := range reaped {
		kind, err := commandtype.FromString(r.commandType)
		if err != nil {
			slog.Warn("skipping reaped command metric: unknown command_type",
				"command_type", r.commandType, "error", err)
			continue
		}
		emitTerminalCommand(ctx, es.metricsEmitter, r.orgID, 0, kind, errReapedStuck)
	}
}

func (es *ExecutionService) dequeueWithRetry(ctx context.Context, limit int32) ([]queue.Message, error) {
	messages, err := es.messageQueue.Dequeue(ctx, limit)
	if err == nil {
		return messages, nil
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("dequeue context canceled: %w", ctx.Err())
	}

	delay := es.config.MasterPollingInterval

	for i := range es.config.DequeueRetries {
		slog.Warn("dequeue error, retrying", "attempt", i+1, "error", err)

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("dequeue retry context canceled: %w", ctx.Err())
		case <-time.After(delay):
			// Continue with retry
		}

		// simple backoff for next attempt
		delay *= 2

		messages, err = es.messageQueue.Dequeue(ctx, limit)
		if err == nil {
			return messages, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("dequeue context canceled after retry: %w", ctx.Err())
		}
	}

	return nil, err
}

func (es *ExecutionService) startQueueProcessorThread(run *executionRun) error {
	ctx := run.admissionCtx
	for {
		reservedSlots, err := es.reserveWorkerSlots(ctx)
		if err != nil {
			return err
		}
		messages, err := es.dequeueWithRetry(ctx, reservedSlots)
		if err != nil {
			es.releaseWorkerSlots(int(reservedSlots))
			if ctx.Err() != nil {
				return fmt.Errorf("queue processor context canceled while dequeuing: %w", ctx.Err())
			}
			slog.Error("dequeue failed after retries; command execution service remains active", "error", err)
			timer := time.NewTimer(es.config.MasterPollingInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("queue processor context canceled after dequeue failure: %w", ctx.Err())
			case <-timer.C:
				continue
			}
		}
		if len(messages) > int(reservedSlots) {
			es.releaseWorkerSlots(int(reservedSlots))
			return fleeterror.NewInternalErrorf("dequeue returned %d messages for %d reserved worker slots", len(messages), reservedSlots)
		}
		es.releaseWorkerSlots(int(reservedSlots) - len(messages))

		if len(messages) == 0 {
			timer := time.NewTimer(es.config.MasterPollingInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("queue processor context canceled while idle: %w", ctx.Err())
			case <-timer.C:
				continue
			}
		}

		for _, message := range messages {
			// The processor remains in the run wait group while it adds workers, so Wait
			// cannot finish before all worker registrations are complete.
			run.wg.Go(func() {
				defer func() { <-es.workerSemaphore }()

				timeout := es.config.WorkerExecutionTimeout
				if message.CommandType == commandtype.FirmwareUpdate {
					timeout = es.config.FirmwareUpdateTimeout
				}
				workerCtx, cancel := context.WithTimeout(run.workCtx, timeout)
				defer cancel()

				es.workerProcessCommand(workerCtx, message)
			})
		}
	}
}

func (es *ExecutionService) reserveWorkerSlots(ctx context.Context) (int32, error) {
	select {
	case es.workerSemaphore <- struct{}{}:
	case <-ctx.Done():
		return 0, fmt.Errorf("queue processor context canceled waiting for worker slot: %w", ctx.Err())
	}

	reserved := int32(1)
	for int(reserved) < cap(es.workerSemaphore) {
		select {
		case es.workerSemaphore <- struct{}{}:
			reserved++
		default:
			return reserved, nil
		}
	}
	return reserved, nil
}

func (es *ExecutionService) releaseWorkerSlots(count int) {
	for range count {
		<-es.workerSemaphore
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
	orgID, siteID, workerError := es.executeCommandOnDevice(ctx, message.CommandType, message)

	// Step 2: Atomically update queue status AND write device log in a single transaction.
	// If the queue row is no longer PROCESSING (reaped), the transaction commits
	// as a no-op and neither the queue status nor the device log is modified.
	dbCtx, dbCancel := context.WithTimeout(context.WithoutCancel(ctx), dbWriteTimeout)
	defer dbCancel()

	var (
		queueUpdated  bool
		queueTerminal bool
	)
	txErr := db.WithTransactionNoResult(dbCtx, es.conn, func(q *sqlc.Queries) error {
		// First: transition queue_message status (detects staleness via rowsAffected).
		updated, terminal, err := es.markQueueMessageStatus(dbCtx, q, message, workerError)
		if err != nil {
			return err
		}
		queueUpdated = updated
		queueTerminal = terminal
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
		return
	}

	// Only emit fleet_command_total for terminal outcomes (SUCCESS / FAILED) that
	// actually landed: retries leaving the row PENDING and stale/reaped messages
	// are not terminal command results.
	if queueUpdated && queueTerminal {
		emitTerminalCommand(ctx, es.metricsEmitter, orgID, siteID, message.CommandType, workerError)
	}
}

// markQueueMessageStatus transitions the queue_message to its next state within an
// existing transaction. Returns (updated, terminal, err) where:
//   - updated is true when rowsAffected > 0 (the row was still PROCESSING),
//   - terminal is true when the resulting queue status is SUCCESS or FAILED
//
// (false, _, nil) means the row is no longer PROCESSING (stale/reaped)
func (es *ExecutionService) markQueueMessageStatus(ctx context.Context, q *sqlc.Queries, message queue.Message, workerError error) (bool, bool, error) {
	var (
		result   sql.Result
		err      error
		terminal bool
	)

	switch {
	case workerError == nil:
		result, err = q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{
			ID:     message.ID,
			Status: sqlc.QueueStatusEnumSUCCESS,
		})
		terminal = true
	case fleeterror.IsUnimplementedError(workerError),
		fleeterror.IsFailedPreconditionError(workerError):
		result, err = q.UpdateMessagePermanentlyFailed(ctx, sqlc.UpdateMessagePermanentlyFailedParams{
			ID:        message.ID,
			ErrorInfo: sql.NullString{String: workerError.Error(), Valid: true},
		})
		terminal = true
	default:
		maxRetries := es.messageQueue.MaxFailureRetries()
		result, err = q.UpdateMessageAfterFailure(ctx, sqlc.UpdateMessageAfterFailureParams{
			ID:         message.ID,
			RetryCount: maxRetries,
			ErrorInfo:  sql.NullString{String: workerError.Error(), Valid: true},
		})
		// Mirrors the SQL CASE: retry_count + 1 >= maxRetries leaves the row FAILED
		// (terminal); otherwise it goes back to PENDING for another attempt.
		terminal = message.RetryCount+1 >= maxRetries
	}

	if err != nil {
		return false, false, fleeterror.NewInternalErrorf("failed to update queue message status: %v", err)
	}
	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, terminal, nil
}

// executeCommandOnDevice runs the command and returns the resolved owning org
// id along with the execution error (if any). It does NOT mark queue message
// status — the caller is responsible for that.
func (es *ExecutionService) executeCommandOnDevice(ctx context.Context, commandType commandtype.Type, message queue.Message) (int64, int64, error) {
	var passwordPayload *dto.UpdateMinerPasswordPayload
	if commandType == commandtype.UpdateMinerPassword {
		var p dto.UpdateMinerPasswordPayload
		if err := json.Unmarshal(message.Payload, &p); err != nil {
			return message.OrgID, 0, fleeterror.NewInternalErrorf("error unmarshalling command payload: %v", err)
		}
		passwordPayload = &p
	}

	minerInfo, err := es.resolveMinerForCommand(ctx, commandType, message.DeviceID, passwordPayload)
	if err != nil {
		if fleeterror.IsFailedPreconditionError(err) {
			return message.OrgID, 0, err
		}
		if commandType == commandtype.UpdateMinerPassword && fleeterror.IsAuthenticationError(err) {
			return message.OrgID, 0, fleeterror.NewFailedPreconditionErrorf("error getting miner connection info for deviceID: %d, %v", message.DeviceID, err)
		}
		return message.OrgID, 0, fleeterror.NewInternalErrorf("error getting miner connection info for deviceID: %d, %v", message.DeviceID, err)
	}
	orgID := minerInfo.GetOrgID()
	siteID := minerInfo.GetSiteID()

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
			return orgID, siteID, fleeterror.NewInternalErrorf("error unmarshalling command payload: %v", coolingExtractErr)
		}
		err = minerInfo.SetCoolingMode(ctx, p)
	case commandtype.SetPowerTarget:
		var p dto.PowerTargetPayload
		powerExtractErr := json.Unmarshal(message.Payload, &p)
		if powerExtractErr != nil {
			return orgID, siteID, fleeterror.NewInternalErrorf("error unmarshalling command payload: %v", powerExtractErr)
		}
		err = minerInfo.SetPowerTarget(ctx, p)
	case commandtype.UpdateMiningPools:
		var p dto.UpdateMiningPoolsPayload
		updateExtractErr := json.Unmarshal(message.Payload, &p)
		if updateExtractErr != nil {
			return orgID, siteID, fleeterror.NewInternalErrorf("error unmarshalling command payload: %v", updateExtractErr)
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
					return orgID, siteID, nil
				}
				return orgID, siteID, es.persistWorkerNameAfterPoolUpdate(ctx, message.DeviceID, minerInfo.GetID(), workerNameToPersist)
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
		reader, info, openErr := es.filesService.OpenFirmwareFileWithInfo(p.FirmwareFileID)
		if openErr != nil {
			err = fleeterror.NewInternalErrorf("error opening firmware file: %v", openErr)
			break
		}
		defer reader.Close()
		err = minerInfo.FirmwareUpdate(ctx, sdk.FirmwareFile{
			Reader:   reader,
			ID:       info.ID,
			Filename: info.Filename,
			Size:     info.Size,
			SHA256:   info.SHA256,
			FilePath: info.FilePath,
		})
		if err != nil {
			break
		}
		shouldReboot, pollErr := es.pollFirmwareInstallStatus(ctx, minerInfo, message.DeviceID)
		if pollErr != nil {
			err = pollErr
			break
		}
		if shouldReboot {
			err = es.rebootAfterFirmwareInstall(ctx, minerInfo, message.DeviceID)
		}
	case commandtype.Unpair:
		err = minerInfo.Unpair(ctx)
		if err == nil {
			if unpairErr := es.handleUnpairPostProcessing(ctx, message.DeviceID); unpairErr != nil {
				slog.Error("unpair post-processing failed", "device_id", message.DeviceID, "error", unpairErr)
				err = unpairErr
			}
		}
	case commandtype.Curtail:
		var p dto.CurtailPayload
		if curtailExtractErr := json.Unmarshal(message.Payload, &p); curtailExtractErr != nil {
			// FailedPrecondition fails permanently on the first attempt;
			// Internal would burn MaxFailureRetries on a deterministic bug.
			return orgID, siteID, fleeterror.NewFailedPreconditionErrorf("error unmarshalling curtail payload: %v", curtailExtractErr)
		}
		if p.Level < int32(sdk.CurtailLevelEfficiency) || p.Level > int32(sdk.CurtailLevelFull) {
			return orgID, siteID, fleeterror.NewFailedPreconditionErrorf("invalid curtail level %d: must be %d (Efficiency) or %d (Full)", p.Level, sdk.CurtailLevelEfficiency, sdk.CurtailLevelFull)
		}
		err = minerInfo.Curtail(ctx, sdk.CurtailRequest{Level: sdk.CurtailLevel(p.Level)})
	case commandtype.Uncurtail:
		err = minerInfo.Uncurtail(ctx, sdk.UncurtailRequest{})
	case commandtype.UpdateMinerPassword:
		p := *passwordPayload

		var encryptedCredentials *gatewaypb.EncryptedCredentials
		if updater, ok := minerInfo.(interfaces.MinerPasswordCredentialUpdater); ok {
			encryptedCredentials, err = updater.UpdateMinerPasswordWithCredentials(ctx, p)
		} else {
			err = minerInfo.UpdateMinerPassword(ctx, p)
		}
		if err != nil {
			if fleeterror.IsAuthenticationError(err) {
				err = fleeterror.NewFailedPreconditionErrorf("miner rejected current password: %v", err)
			}
			break
		}

		// Surface a persistence failure as a command error: the on-device password
		// already changed, so a silent success would leave Fleet with stale credentials.
		if dbErr := es.persistUpdatedMinerPassword(ctx, message.DeviceID, minerInfo, p.NewPassword, encryptedCredentials); dbErr != nil {
			slog.Error("device password updated but database sync failed",
				"device_id", message.DeviceID, "error", dbErr)
			err = fleeterror.NewFailedPreconditionErrorf(
				"device %d password changed on-device but credential persistence failed: %v",
				message.DeviceID, dbErr)
			es.minerService.InvalidateMiner(minerInfo.GetID())
			break
		}

		// Clear the DEFAULT_PASSWORD remediation state (no-op for already-PAIRED rows).
		if resetErr := es.deviceStore.UpdateDevicePairingStatusByIdentifier(
			ctx, string(minerInfo.GetID()), string(sqlc.PairingStatusEnumPAIRED)); resetErr != nil {
			slog.Error("password updated but pairing-status reset failed",
				"device_id", message.DeviceID, "error", resetErr)
		}

		// Evict so the next lookup re-reads updated credentials from DB.
		es.minerService.InvalidateMiner(minerInfo.GetID())
	default:
		return orgID, siteID, fleeterror.NewInternalErrorf("unsupported command type: %v", commandType)
	}

	if err != nil {
		if fleeterror.IsAuthenticationError(err) {
			es.minerService.InvalidateMiner(minerInfo.GetID())
		}
		slog.Error("command execution failed", "command", commandType, "device_id", message.DeviceID, "batch_uuid", message.BatchLogUUID, "error", err)
	}
	return orgID, siteID, err
}

func (es *ExecutionService) resolveMinerForCommand(ctx context.Context, commandType commandtype.Type, deviceID int64, passwordPayload *dto.UpdateMinerPasswordPayload) (interfaces.Miner, error) {
	var (
		miner interfaces.Miner
		err   error
	)
	if commandType == commandtype.UpdateMinerPassword && passwordPayload != nil && passwordPayload.EncryptedPasswordUpdate == nil {
		miner, err = es.minerService.GetMinerForPasswordUpdate(ctx, deviceID, passwordPayload.CurrentPassword)
	} else {
		miner, err = es.minerService.GetMiner(ctx, deviceID)
	}
	if err != nil {
		return nil, err
	}
	return miner, nil
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
		return dto.UpdateMiningPoolsPayload{}, "", false, err
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
	deviceIdentifier, err := db.WithTransaction(ctx, es.conn, func(q *sqlc.Queries) (string, error) {
		return q.GetDeviceIdentifierByID(ctx, deviceID)
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to get device identifier by ID: %v", err)
	}

	if err := es.deviceStore.UpdateDevicePairingStatusByIdentifier(ctx, deviceIdentifier, string(sqlc.PairingStatusEnumUNPAIRED)); err != nil {
		return fleeterror.NewInternalErrorf("failed to update device pairing status to UNPAIRED: %v", err)
	}

	slog.Info("device pairing status updated to UNPAIRED", "device_identifier", deviceIdentifier)

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

// clearFirmwareUpdateStatus resets stale firmware statuses back to ACTIVE after
// a reboot command or firmware cleanup path, allowing telemetry to take over
// status management.
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

	es.clearFirmwareUpdateStatusForDevice(cleanupCtx, deviceID, devID)
}

func (es *ExecutionService) clearFirmwareUpdateStatusForDevice(ctx context.Context, deviceID int64, devID tmodels.DeviceIdentifier) {
	currentStatuses, err := es.deviceStore.GetDeviceStatusForDeviceIdentifiers(ctx, []tmodels.DeviceIdentifier{devID})
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
		upsertErr = es.deviceStore.UpsertDeviceStatus(ctx, devID, models.MinerStatusActive, "")
		if upsertErr == nil {
			slog.Info("cleared firmware update status after reboot", "device_id", deviceID, "previous_status", currentStatus)
			return
		}
		slog.Warn("failed to clear firmware update status after reboot, retrying",
			"device_id", deviceID, "attempt", attempt+1, "error", upsertErr)
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
	slog.Error("permanently failed to clear firmware update status after reboot",
		"device_id", deviceID, "error", upsertErr)
}

// pollFirmwareInstallStatus polls the rig's install status after a successful firmware
// upload until installation completes or fails. Devices without install-status
// reporting are still reboot-ready after upload success; capability dispatch has
// already verified reboot support for firmware updates. For polling-capable
// devices, status transitions to UPDATING while installation runs, then
// REBOOT_REQUIRED on success.
func (es *ExecutionService) pollFirmwareInstallStatus(ctx context.Context, minerInfo interfaces.Miner, deviceID int64) (bool, error) {
	provider, canPoll := minerInfo.(interfaces.FirmwareUpdateStatusProvider)
	if !canPoll {
		slog.Info("firmware update status provider unavailable, rebooting after upload", "device_id", deviceID)
		es.markFirmwareRebootRequired(ctx, deviceID)
		return true, nil
	}

	probeStatus, probeErr := provider.GetFirmwareUpdateStatus(ctx)
	if probeErr == nil && probeStatus == nil {
		slog.Info("firmware update status provider does not report install status, rebooting after upload", "device_id", deviceID)
		es.markFirmwareRebootRequired(ctx, deviceID)
		return true, nil
	}

	deviceIdentifier, err := db.WithTransaction(ctx, es.conn, func(q *sqlc.Queries) (string, error) {
		return q.GetDeviceIdentifierByID(ctx, deviceID)
	})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to resolve device identifier for firmware status polling: %v", err)
	}

	devID := tmodels.DeviceIdentifier(deviceIdentifier)

	installVerified, pollResult := es.doPollFirmwareInstall(ctx, provider, devID, deviceID, probeStatus, probeErr)

	if pollResult != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if upsertErr := es.deviceStore.UpsertDeviceStatus(cleanupCtx, devID, models.MinerStatusActive, ""); upsertErr != nil {
			slog.Warn("failed to clear firmware update status after polling failure", "device_id", deviceID, "error", upsertErr)
		}
	}
	return installVerified, pollResult
}

func (es *ExecutionService) markFirmwareRebootRequired(ctx context.Context, deviceID int64) {
	if es.conn == nil || es.deviceStore == nil {
		return
	}
	deviceIdentifier, err := db.WithTransaction(ctx, es.conn, func(q *sqlc.Queries) (string, error) {
		return q.GetDeviceIdentifierByID(ctx, deviceID)
	})
	if err != nil {
		slog.Warn("failed to resolve device identifier for firmware reboot status", "device_id", deviceID, "error", err)
		return
	}

	devID := tmodels.DeviceIdentifier(deviceIdentifier)
	if upsertErr := es.deviceStore.UpsertDeviceStatus(ctx, devID, models.MinerStatusRebootRequired, ""); upsertErr != nil {
		slog.Warn("failed to set firmware status to REBOOT_REQUIRED before automatic reboot", "device_id", deviceID, "error", upsertErr)
	}
}

func (es *ExecutionService) doPollFirmwareInstall(ctx context.Context, provider interfaces.FirmwareUpdateStatusProvider, devID tmodels.DeviceIdentifier, deviceID int64, probeStatus *sdk.FirmwareUpdateStatus, probeErr error) (bool, error) {
	if upsertErr := es.deviceStore.UpsertDeviceStatus(ctx, devID, models.MinerStatusUpdating, ""); upsertErr != nil {
		slog.Warn("failed to set device status to UPDATING", "device_id", deviceID, "error", upsertErr)
	}

	ticker := time.NewTicker(firmwareInstallPollInterval)
	defer ticker.Stop()
	uploadCompletedAt := time.Now()

	handleStatus := func(status *sdk.FirmwareUpdateStatus, pollErr error) (bool, error) {
		if pollErr != nil {
			slog.Warn("firmware install status poll failed", "device_id", deviceID, "error", pollErr)
			return false, nil
		}
		if status == nil {
			return false, nil
		}

		switch status.State {
		case "installed", "success", "confirming":
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbWriteTimeout)
			defer cancel()
			if upsertErr := es.deviceStore.UpsertDeviceStatus(cleanupCtx, devID, models.MinerStatusRebootRequired, ""); upsertErr != nil {
				slog.Error("firmware install completed but failed to persist REBOOT_REQUIRED, treating as success to avoid re-upload", "device_id", deviceID, "error", upsertErr)
			}
			slog.Info("firmware install completed, rebooting device", "device_id", deviceID)
			return true, nil

		case "installing", "downloaded":
			slog.Debug("firmware install in progress", "device_id", deviceID, "state", status.State, "progress", status.Progress)

		case "current":
			if status.Error != nil && *status.Error != "" {
				return false, fleeterror.NewInternalErrorf("firmware install failed on device %d: %s", deviceID, *status.Error)
			}
			if time.Since(uploadCompletedAt) > firmwareInstallGraceWindow {
				return false, fleeterror.NewInternalErrorf("firmware install reverted to 'current' on device %d (install may have failed silently)", deviceID)
			}
			slog.Debug("firmware install not started yet (grace window)", "device_id", deviceID)

		case "error":
			errMsg := "unknown error"
			if status.Error != nil && *status.Error != "" {
				errMsg = *status.Error
			}
			return false, fleeterror.NewInternalErrorf("firmware install failed on device %d: %s", deviceID, errMsg)

		default:
			slog.Debug("unexpected firmware install state", "device_id", deviceID, "state", status.State)
		}
		return false, nil
	}

	if installVerified, result := handleStatus(probeStatus, probeErr); installVerified || result != nil {
		return installVerified, result
	}

	for {
		select {
		case <-ctx.Done():
			return false, fleeterror.NewInternalErrorf("firmware install polling timed out for device %d", deviceID)
		case <-ticker.C:
			status, pollErr := provider.GetFirmwareUpdateStatus(ctx)
			if installVerified, result := handleStatus(status, pollErr); installVerified || result != nil {
				return installVerified, result
			}
		}
	}
}

func (es *ExecutionService) rebootAfterFirmwareInstall(ctx context.Context, minerInfo interfaces.Miner, deviceID int64) error {
	if err := minerInfo.Reboot(ctx); err != nil {
		slog.Error("firmware install completed but automatic reboot failed",
			"device_id", deviceID, "error", err)
		return fleeterror.NewFailedPreconditionErrorf(
			"firmware installed on device %d but automatic reboot failed: %v",
			deviceID, err)
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	es.clearFirmwareUpdateStatusForDevice(cleanupCtx, deviceID, minerInfo.GetID())
	return nil
}

var errMinerCredentialsMissing = errors.New("no miner credentials row for device")

// persistMinerPassword updates the device's stored password, inserting a defensive
// row for Proto if the command reached persistence before credentials existed.
func (es *ExecutionService) persistUpdatedMinerPassword(ctx context.Context, deviceID int64, minerInfo interfaces.Miner, password string, encrypted *gatewaypb.EncryptedCredentials) error {
	if encrypted != nil {
		return es.persistFleetNodeMinerCredentials(ctx, deviceID, encrypted)
	}
	return es.persistMinerPassword(ctx, deviceID, minerInfo.GetDriverName(), password)
}

func (es *ExecutionService) persistFleetNodeMinerCredentials(ctx context.Context, deviceID int64, encrypted *gatewaypb.EncryptedCredentials) error {
	encodedUsername, encodedPassword, err := credentialblob.EncodeValid(encrypted)
	if errors.Is(err, credentialblob.ErrMissingCredentials) {
		return fleeterror.NewInternalErrorf("fleet node password update returned empty encrypted credentials")
	}
	if errors.Is(err, credentialblob.ErrMalformedCredentials) {
		return fleeterror.NewInternalErrorf("fleet node password update returned malformed encrypted credentials")
	}
	if err != nil {
		return fleeterror.NewInternalErrorf("encode fleet node password update credentials: %v", err)
	}
	return db.WithTransactionNoResult(ctx, es.conn, func(q *sqlc.Queries) error {
		return q.UpsertMinerCredentials(ctx, sqlc.UpsertMinerCredentialsParams{
			DeviceID:    deviceID,
			UsernameEnc: encodedUsername,
			PasswordEnc: encodedPassword,
		})
	})
}

func (es *ExecutionService) persistMinerPassword(ctx context.Context, deviceID int64, driverName, password string) error {
	err := es.updateMinerPasswordInDB(ctx, deviceID, password)
	if errors.Is(err, errMinerCredentialsMissing) && driverName == models.DriverNameProto {
		return es.insertProtoCredentials(ctx, deviceID, password)
	}
	return err
}

func (es *ExecutionService) insertProtoCredentials(ctx context.Context, deviceID int64, password string) error {
	usernameEnc, err := es.encryptService.Encrypt([]byte(models.ProtoDefaultUsername))
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to encrypt username: %v", err)
	}
	passwordEnc, err := es.encryptService.Encrypt([]byte(password))
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to encrypt password: %v", err)
	}

	return db.WithTransactionNoResult(ctx, es.conn, func(q *sqlc.Queries) error {
		return q.UpsertMinerCredentials(ctx, sqlc.UpsertMinerCredentialsParams{
			DeviceID:    deviceID,
			UsernameEnc: usernameEnc,
			PasswordEnc: passwordEnc,
		})
	})
}

// updateMinerPasswordInDB encrypts and stores the miner password in the database
// after successful password update on the device. Username remains unchanged.
// Returns errMinerCredentialsMissing when no credentials row exists.
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

	if rowsAffected == 0 {
		return errMinerCredentialsMissing
	}

	return nil
}
