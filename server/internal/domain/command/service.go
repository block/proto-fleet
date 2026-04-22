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
	"time"

	"github.com/sqlc-dev/pqtype"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	id "github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
)

// TelemetryListener provides interface for telemetry registration/unregistration
type TelemetryListener interface {
	RemoveDevices(ctx context.Context, deviceIDs ...tmodels.DeviceIdentifier) error
}

// UserCredentialsVerifier provides interface for verifying user credentials
type UserCredentialsVerifier interface {
	VerifyCredentials(ctx context.Context, username, password string) error
}

// Service handles miner command operations
type Service struct {
	config *Config

	conn                *sql.DB
	executionService    *ExecutionService
	messageQueue        queue.MessageQueue
	statusService       *StatusService
	encryptService      *encrypt.Service
	filesService        *files.Service
	deviceStore         stores.DeviceStore
	userStore           stores.UserStore
	credentialsVerifier UserCredentialsVerifier
	telemetryListener   TelemetryListener
	capabilityChecker   *CapabilityChecker
	activitySvc         *activity.Service
}

const defaultPoolPriority uint32 = 0

// maxCallbackRetries is the maximum number of times the onFinished callback is retried
// before marking the batch finished anyway to unblock the client.
const maxCallbackRetries = 3

type Command struct {
	commandType    commandtype.Type
	deviceSelector *pb.DeviceSelector
	payload        interface{}
}

// NewService creates a new command service instance
func NewService(config *Config, conn *sql.DB, executionService *ExecutionService, messageQueue queue.MessageQueue, statusService *StatusService, encryptService *encrypt.Service, filesService *files.Service, deviceStore stores.DeviceStore, userStore stores.UserStore, credentialsVerifier UserCredentialsVerifier, telemetryListener TelemetryListener, capabilitiesProvider CapabilitiesProvider, activitySvc *activity.Service) *Service {
	return &Service{
		config:              config,
		conn:                conn,
		executionService:    executionService,
		messageQueue:        messageQueue,
		statusService:       statusService,
		encryptService:      encryptService,
		filesService:        filesService,
		deviceStore:         deviceStore,
		userStore:           userStore,
		credentialsVerifier: credentialsVerifier,
		telemetryListener:   telemetryListener,
		capabilityChecker:   NewCapabilityChecker(conn, capabilitiesProvider),
		activitySvc:         activitySvc,
	}
}

func (s *Service) logCommandActivity(ctx context.Context, eventType, description string, deviceCount int, batchID string) {
	if s.activitySvc == nil {
		return
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		slog.Warn("failed to log command activity: session info unavailable", "error", err)
		return
	}
	batchIDCopy := batchID
	s.activitySvc.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryDeviceCommand,
		Type:           eventType,
		Description:    description,
		ScopeCount:     &deviceCount,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		BatchID:        &batchIDCopy,
		Metadata:       map[string]any{"batch_id": batchID},
	})
}

// composeFinalizers chains multiple onFinished callbacks so every command RPC
// can layer its own side-effects (e.g. the DownloadLogs bundle builder) with
// the shared activity finalizer. Nil callbacks are skipped; if the resulting
// chain is empty the helper returns nil so initializeStatusUpdateRoutine can
// keep its zero-callback fast path.
func composeFinalizers(callbacks ...onFinishedCallbackFunc) onFinishedCallbackFunc {
	nonNil := make([]onFinishedCallbackFunc, 0, len(callbacks))
	for _, cb := range callbacks {
		if cb != nil {
			nonNil = append(nonNil, cb)
		}
	}
	switch len(nonNil) {
	case 0:
		return nil
	case 1:
		return nonNil[0]
	default:
		return func() error {
			for _, cb := range nonNil {
				if err := cb(); err != nil {
					return err
				}
			}
			return nil
		}
	}
}

// finalizerDBTimeout bounds the background transaction used by the activity
// finalizer. Independent of request ctx since the finalizer runs long after
// the originating RPC has returned.
const finalizerDBTimeout = 15 * time.Second

// buildActivityCompletedCallback returns a finalizer that writes the
// '<event_type>.completed' activity row when the batch reaches FINISHED. The
// row is idempotent: the partial unique index on (batch_id, event_type) plus
// SQLActivityStore.Insert's duplicate-swallow let the crash-recovery
// reconciler (M7) re-run this callback safely.
//
// Session info is captured at call time because the finalizer runs against a
// background context (the originating request ctx is long gone).
func (s *Service) buildActivityCompletedCallback(ctx context.Context, batchID, eventType, description string) onFinishedCallbackFunc {
	if s.activitySvc == nil {
		return nil
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		// Without session info we cannot attribute the completion event. The
		// crash-recovery reconciler in M7 still produces a system-attributed
		// completion row later, so we fail open rather than blocking the batch.
		slog.Warn("command activity finalizer: session info unavailable at command start",
			"error", err, "batch_id", batchID)
		return nil
	}
	userID := info.ExternalUserID
	username := info.Username
	organizationID := info.OrganizationID
	return func() error {
		finCtx, cancel := context.WithTimeout(context.Background(), finalizerDBTimeout)
		defer cancel()
		counts, err := db.WithTransaction(finCtx, s.conn, func(q *sqlc.Queries) (sqlc.GetBatchStatusAndDeviceCountsRow, error) {
			return q.GetBatchStatusAndDeviceCounts(finCtx, batchID)
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("finalizer reading counts for %s: %v", batchID, err)
		}

		result := activitymodels.ResultSuccess
		if counts.FailedDevices > 0 {
			result = activitymodels.ResultFailure
		}

		// #nosec G115 -- devices_count is bounded by the batch size we create.
		scopeCount := int(counts.DevicesCount)
		batchIDCopy := batchID
		completionDesc := fmt.Sprintf("%s completed: %d succeeded, %d failed",
			description, counts.SuccessfulDevices, counts.FailedDevices)
		s.activitySvc.Log(finCtx, activitymodels.Event{
			Category:       activitymodels.CategoryDeviceCommand,
			Type:           eventType + activitymodels.CompletedEventSuffix,
			Description:    completionDesc,
			Result:         result,
			ScopeCount:     &scopeCount,
			ActorType:      activitymodels.ActorUser,
			UserID:         &userID,
			Username:       &username,
			OrganizationID: &organizationID,
			BatchID:        &batchIDCopy,
			Metadata: map[string]any{
				"batch_id":      batchID,
				"total_count":   counts.DevicesCount,
				"success_count": counts.SuccessfulDevices,
				"failure_count": counts.FailedDevices,
			},
		})
		return nil
	}
}

func (s *Service) getDevicesCount(ctx context.Context, selector *pb.DeviceSelector) (int32, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error getting session info from ctx: %v", err)
	}

	switch x := selector.SelectionType.(type) {
	case *pb.DeviceSelector_AllDevices:
		return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (int32, error) {
			count, err := q.GetTotalPairedDevices(ctx, sqlc.GetTotalPairedDevicesParams{OrgID: info.OrganizationID})
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

func (s *Service) saveCommandBatchLogToDB(ctx context.Context, userID, organizationID int64, command *Command, payloadBytes []byte) (string, error) {
	devicesCount, err := s.getDevicesCount(ctx, command.deviceSelector)
	if err != nil {
		return "", err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (string, error) {
		timeNow := time.Now()
		newUUID := id.GenerateID()

		_, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:           newUUID,
			Type:           command.commandType.String(),
			CreatedBy:      userID,
			CreatedAt:      timeNow,
			Status:         sqlc.BatchStatusEnumPENDING,
			DevicesCount:   devicesCount,
			Payload:        pqtype.NullRawMessage{RawMessage: payloadBytes, Valid: len(payloadBytes) > 0},
			OrganizationID: sql.NullInt64{Int64: organizationID, Valid: organizationID != 0},
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

func (s *Service) statusUpdateIsFinishedBranch(ctx context.Context, commandBatchLogUUID string) (bool, error) {
	isFinished, err := s.messageQueue.IsBatchFinished(ctx, commandBatchLogUUID)
	if err != nil {
		return false, fleeterror.NewInternalErrorf("error asking is finished: %v", err)
	}
	return isFinished, nil
}

type onFinishedCallbackFunc func() error

func (s *Service) initializeStatusUpdateRoutine(commandBatchLogUUID string, onFinishedCallback onFinishedCallbackFunc) {
	go func() {
		// TODO maybe integrate this with the execution service master thread ctx in the future
		ctx := context.Background()
		ticker := time.NewTicker(s.config.BatchStatusUpdatePollingInterval)
		defer ticker.Stop()

		processingMarkedInDB := false
		callbackRetryCount := 0
		callbackDone := false // true once callback succeeded or max retries exhausted
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
				isFinished, err := s.statusUpdateIsFinishedBranch(ctx, commandBatchLogUUID)
				if err != nil {
					slog.Error("error in isFinished branch", "error", err)
					return
				}
				if isFinished {
					// Run the callback before marking the batch finished in the DB.
					// This ensures any side-effects (e.g. ZIP creation for download-logs)
					// are complete before the stream sees FINISHED and the client fetches
					// the result, preventing a race where the client requests the bundle
					// before it exists.
					if onFinishedCallback != nil && !callbackDone {
						if callbackErr := onFinishedCallback(); callbackErr != nil {
							callbackRetryCount++
							if callbackRetryCount < maxCallbackRetries {
								// Retry on the next tick so the client doesn't see FINISHED
								// until side-effects (e.g. bundle creation) have succeeded.
								slog.Error("error in onFinished callback, will retry", "error", callbackErr, "retry", callbackRetryCount)
								continue
							}
							// Max retries exceeded — mark the batch finished anyway to unblock the
							// client. The bundle may be unavailable; the client will get an
							// appropriate error when it attempts to fetch it.
							slog.Error("onFinished callback failed after max retries, marking batch finished", "error", callbackErr)
						}
						callbackDone = true
					}
					if markErr := s.getMarkFinishedBatchFunction(processingMarkedInDB)(ctx, commandBatchLogUUID); markErr != nil {
						// Retry on the next tick; the batch must reach FINISHED in the DB
						// or the client stream will see it stuck in PROCESSING forever.
						slog.Error("error marking batch finished, will retry", "error", markErr)
						continue
					}
					return
				}
			}
		}
	}()
}

func (s *Service) getDeviceIDs(ctx context.Context, selector *pb.DeviceSelector) ([]int64, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info from context: %v", err)
	}

	switch x := selector.SelectionType.(type) {
	case *pb.DeviceSelector_AllDevices:
		filter := x.AllDevices
		if filter == nil {
			filter = &pb.DeviceFilter{}
		}

		return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]int64, error) {
			var deviceStatus sql.NullString
			var pairingStatus sql.NullString
			var modelFilter sql.NullString
			var manufacturerFilter sql.NullString

			if len(filter.DeviceStatus) > 0 {
				deviceStatus = sql.NullString{
					String: string(sqlstores.ProtoDeviceStatusToSQL(filter.DeviceStatus[0])),
					Valid:  true,
				}
			}

			if len(filter.PairingStatus) > 0 {
				pairingStatus = sql.NullString{
					String: string(sqlstores.ProtoPairingStatusToSQL(filter.PairingStatus[0])),
					Valid:  true,
				}
			}

			if len(filter.Models) > 0 {
				modelFilter = sql.NullString{
					String: strings.Join(filter.Models, ","),
					Valid:  true,
				}
			}

			if len(filter.Manufacturers) > 0 {
				manufacturerFilter = sql.NullString{
					String: strings.Join(filter.Manufacturers, ","),
					Valid:  true,
				}
			}

			return q.GetFilteredDeviceIds(ctx, sqlc.GetFilteredDeviceIdsParams{
				OrgID:              info.OrganizationID,
				DeviceStatus:       deviceStatus,
				PairingStatus:      pairingStatus,
				ModelFilter:        modelFilter,
				ManufacturerFilter: manufacturerFilter,
			})
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

func (s *Service) processCommand(ctx context.Context, command *Command) (string, int, error) {
	if !s.executionService.IsRunning() {
		slog.Error("command execution service is not running, attempting to start it")
		err := s.executionService.Start(ctx)
		if err != nil {
			return "", 0, fleeterror.NewInternalErrorf("failed to start command execution service: %v", err)
		}
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error getting session info from ctx: %v", err)
	}

	payloadBytes, err := json.Marshal(command.payload)
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error marshalling payload: %v", err)
	}

	batchLogIdentifier, err := s.saveCommandBatchLogToDB(ctx, info.UserID, info.OrganizationID, command, payloadBytes)
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error saving command batch log to db: %v", err)
	}
	deviceIDs, err := s.getDeviceIDs(ctx, command.deviceSelector)
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error getting device IDs from device selector: %v", err)
	}

	err = s.messageQueue.Enqueue(ctx, batchLogIdentifier, command.commandType, deviceIDs, command.payload)
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error enqueuing a batch of commands: %v", err)
	}

	return batchLogIdentifier, len(deviceIDs), nil
}

func (s *Service) Reboot(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.RebootResponse, error) {
	commandBatchLogUUID, deviceCount, err := s.processCommand(ctx, &Command{commandType: commandtype.Reboot, deviceSelector: deviceSelector, payload: nil})
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "reboot", "Reboot", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "reboot", "Reboot"))

	return &pb.RebootResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

// StopMining stops mining on the specified miners
func (s *Service) StopMining(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.StopMiningResponse, error) {
	commandBatchLogUUID, deviceCount, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.StopMining, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "stop_mining", "Sleep", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "stop_mining", "Sleep"))

	return &pb.StopMiningResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

// StartMining starts mining on the specified miners
func (s *Service) StartMining(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.StartMiningResponse, error) {
	commandBatchLogUUID, deviceCount, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.StartMining, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "start_mining", "Wake up", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "start_mining", "Wake up"))

	return &pb.StartMiningResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

func (s *Service) SetCoolingMode(ctx context.Context, deviceSelector *pb.DeviceSelector, modeType commonpb.CoolingMode) (*pb.SetCoolingModeResponse, error) {
	cm := dto.CoolingModePayload{Mode: modeType}
	commandBatchLogUUID, deviceCount, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.SetCoolingMode, deviceSelector: deviceSelector, payload: cm},
	)
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "set_cooling_mode", "Cooling mode changed", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "set_cooling_mode", "Cooling mode changed"))

	return &pb.SetCoolingModeResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

func (s *Service) SetPowerTarget(ctx context.Context, deviceSelector *pb.DeviceSelector, performanceMode pb.PerformanceMode) (*pb.SetPowerTargetResponse, error) {
	pt := dto.PowerTargetPayload{
		PerformanceMode: performanceMode,
	}
	commandBatchLogUUID, deviceCount, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.SetPowerTarget, deviceSelector: deviceSelector, payload: pt},
	)
	if err != nil {
		return nil, err
	}

	description := fmt.Sprintf("Power target changed to %s", performanceMode.String())
	s.logCommandActivity(ctx, "set_power_target", description, deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "set_power_target", description))

	return &pb.SetPowerTargetResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

func (s *Service) createMiningPoolDTO(ctx context.Context, poolID int64, priorityIncrement uint32) (*dto.MiningPool, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}

	pool, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.Pool, error) {
		p, err := q.GetPool(ctx, sqlc.GetPoolParams{ID: poolID, OrgID: info.OrganizationID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return p, fleeterror.NewNotFoundErrorf("pool not found: %d", poolID)
			}
			return p, err
		}
		return p, nil
	})
	if err != nil {
		return nil, err
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
		Priority:        defaultPoolPriority + priorityIncrement,
		URL:             pool.Url,
		Username:        pool.Username,
		Password:        password,
		AppendMinerName: shouldAppendMinerNameToUsername(pool.Username),
	}, nil
}

// createMiningPoolDTOFromSlotConfig creates a MiningPool DTO from a PoolSlotConfig.
// It handles both known pools (by ID lookup) and unknown pools (raw URL/username).
func (s *Service) createMiningPoolDTOFromSlotConfig(ctx context.Context, config *pb.PoolSlotConfig, priorityIncrement uint32) (*dto.MiningPool, error) {
	if config == nil {
		return nil, nil
	}

	switch source := config.PoolSource.(type) {
	case *pb.PoolSlotConfig_PoolId:
		return s.createMiningPoolDTO(ctx, source.PoolId, priorityIncrement)
	case *pb.PoolSlotConfig_RawPool:
		var password string
		if source.RawPool.Password != nil {
			password = *source.RawPool.Password
		}
		return &dto.MiningPool{
			Priority:        defaultPoolPriority + priorityIncrement,
			URL:             source.RawPool.Url,
			Username:        source.RawPool.Username,
			Password:        password,
			AppendMinerName: shouldAppendMinerNameToUsername(source.RawPool.Username),
		}, nil
	default:
		return nil, fleeterror.NewInternalErrorf("invalid pool source type")
	}
}

func (s *Service) createUpdateMiningPoolsPayload(ctx context.Context, defaultPool, backup1Pool, backup2Pool *pb.PoolSlotConfig) (*dto.UpdateMiningPoolsPayload, error) {
	defaultPoolDTO, err := s.createMiningPoolDTOFromSlotConfig(ctx, defaultPool, 0)
	if err != nil {
		return nil, err
	}
	if defaultPoolDTO == nil {
		return nil, fleeterror.NewInvalidArgumentError("default pool is required")
	}

	pld := &dto.UpdateMiningPoolsPayload{
		DefaultPool: *defaultPoolDTO,
	}

	if backup1Pool != nil {
		pool, err := s.createMiningPoolDTOFromSlotConfig(ctx, backup1Pool, 1)
		if err != nil {
			return nil, err
		}
		pld.Backup1Pool = pool
	}

	if backup2Pool != nil {
		pool, err := s.createMiningPoolDTOFromSlotConfig(ctx, backup2Pool, 2)
		if err != nil {
			return nil, err
		}
		pld.Backup2Pool = pool
	}

	return pld, nil
}

func (s *Service) UpdateMiningPools(
	ctx context.Context,
	deviceSelector *pb.DeviceSelector,
	defaultPool, backup1Pool, backup2Pool *pb.PoolSlotConfig,
	userUsername string,
	userPassword string,
) (*pb.UpdateMiningPoolsResponse, error) {
	if err := s.verifyUserCredentials(ctx, userUsername, userPassword); err != nil {
		return nil, err
	}

	pld, err := s.createUpdateMiningPoolsPayload(ctx, defaultPool, backup1Pool, backup2Pool)
	if err != nil {
		return nil, err
	}

	commandBatchLogUUID, deviceCount, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.UpdateMiningPools, deviceSelector: deviceSelector, payload: pld},
	)
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "update_mining_pools", "Edit pool", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "update_mining_pools", "Edit pool"))

	return &pb.UpdateMiningPoolsResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

func (s *Service) VerifyCredentials(ctx context.Context, username string, password string) error {
	return s.verifyUserCredentials(ctx, username, password)
}

func (s *Service) ReapplyCurrentPoolsWithWorkerNames(
	ctx context.Context,
	desiredWorkerNamesByDeviceIdentifier map[string]string,
) (string, error) {
	if len(desiredWorkerNamesByDeviceIdentifier) == 0 {
		return "", nil
	}

	if !s.executionService.IsRunning() {
		slog.Error("command execution service is not running, attempting to start it")
		err := s.executionService.Start(ctx)
		if err != nil {
			return "", fleeterror.NewInternalErrorf("failed to start command execution service: %v", err)
		}
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error getting session info from ctx: %v", err)
	}

	deviceIdentifiers := make([]string, 0, len(desiredWorkerNamesByDeviceIdentifier))
	for deviceIdentifier := range desiredWorkerNamesByDeviceIdentifier {
		deviceIdentifiers = append(deviceIdentifiers, deviceIdentifier)
	}
	sort.Strings(deviceIdentifiers)

	command := &Command{
		commandType: commandtype.UpdateMiningPools,
		deviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonpb.DeviceIdentifierList{
					DeviceIdentifiers: deviceIdentifiers,
				},
			},
		},
		payload: dto.UpdateMiningPoolsPayload{
			ReapplyCurrentPoolsWithStoredWorkerName: true,
		},
	}

	payloadBytes, err := json.Marshal(command.payload)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error marshalling payload: %v", err)
	}

	commandBatchLogUUID, err := s.saveCommandBatchLogToDB(ctx, info.UserID, info.OrganizationID, command, payloadBytes)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error saving command batch log to db: %v", err)
	}

	deviceIDsByIdentifier, err := s.getDeviceIDsWithIdentifiers(ctx, deviceIdentifiers)
	if err != nil {
		return "", err
	}

	if err := s.enqueueWorkerNameReapplyMessages(ctx, commandBatchLogUUID, deviceIdentifiers, deviceIDsByIdentifier, desiredWorkerNamesByDeviceIdentifier); err != nil {
		return "", err
	}

	s.initializeStatusUpdateRoutine(commandBatchLogUUID, nil)
	return commandBatchLogUUID, nil
}

func (s *Service) getDeviceIDsWithIdentifiers(ctx context.Context, deviceIdentifiers []string) (map[string]int64, error) {
	rows, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]sqlc.GetDeviceIDsWithIdentifiersRow, error) {
		return q.GetDeviceIDsWithIdentifiers(ctx, deviceIdentifiers)
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting device IDs from device identifiers: %v", err)
	}
	if len(rows) != len(deviceIdentifiers) {
		return nil, fleeterror.NewNotFoundErrorf("one or more devices not found for worker-name reapply")
	}

	deviceIDsByIdentifier := make(map[string]int64, len(rows))
	for _, row := range rows {
		deviceIDsByIdentifier[row.DeviceIdentifier] = row.ID
	}
	return deviceIDsByIdentifier, nil
}

func (s *Service) enqueueWorkerNameReapplyMessages(
	ctx context.Context,
	commandBatchLogUUID string,
	deviceIdentifiers []string,
	deviceIDsByIdentifier map[string]int64,
	desiredWorkerNamesByDeviceIdentifier map[string]string,
) error {
	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		commandType := commandtype.UpdateMiningPools
		for _, deviceIdentifier := range deviceIdentifiers {
			payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
				ReapplyCurrentPoolsWithStoredWorkerName: true,
				DesiredWorkerName:                       desiredWorkerNamesByDeviceIdentifier[deviceIdentifier],
			})
			if err != nil {
				return fleeterror.NewInternalErrorf("failed to marshal worker-name reapply payload: %v", err)
			}

			if err := q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
				CommandBatchLogUuid: commandBatchLogUUID,
				CommandType:         commandType.String(),
				DeviceID:            deviceIDsByIdentifier[deviceIdentifier],
				Status:              sqlc.QueueStatusEnumPENDING,
				RetryCount:          0,
				Payload:             pqtype.NullRawMessage{RawMessage: payloadBytes, Valid: true},
			}); err != nil {
				return fleeterror.NewInternalErrorf("failed to enqueue worker-name reapply message: %v", err)
			}
		}
		return nil
	})
}

func (s *Service) DownloadLogs(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.DownloadLogsResponse, error) {
	commandBatchLogUUID, deviceCount, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.DownloadLogs, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}

	// Bundle callback runs first so the ZIP is on disk before the activity log
	// marks the batch as completed; the activity finalizer then writes the
	// completion row. Both are chained through composeFinalizers.
	bundleCb := s.filesService.DownloadLogsOnFinishedCallback(commandBatchLogUUID)
	activityCb := s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "download_logs", "Download logs")
	s.logCommandActivity(ctx, "download_logs", "Download logs", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID, composeFinalizers(bundleCb, activityCb))

	return &pb.DownloadLogsResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

func (s *Service) BlinkLED(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.BlinkLEDResponse, error) {
	commandBatchLogUUID, deviceCount, err := s.processCommand(ctx, &Command{commandType: commandtype.BlinkLED, deviceSelector: deviceSelector, payload: nil})
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "blink_led", "Blink LEDs", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "blink_led", "Blink LEDs"))

	return &pb.BlinkLEDResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

func (s *Service) FirmwareUpdate(ctx context.Context, deviceSelector *pb.DeviceSelector, firmwareFileID string) (*pb.FirmwareUpdateResponse, error) {
	if _, err := s.filesService.GetFirmwareFilePath(firmwareFileID); err != nil {
		return nil, fleeterror.NewInvalidArgumentError(fmt.Sprintf("invalid firmware_file_id: %v", err))
	}

	payload := dto.FirmwareUpdatePayload{FirmwareFileID: firmwareFileID}
	commandBatchLogUUID, deviceCount, err := s.processCommand(ctx, &Command{
		commandType:    commandtype.FirmwareUpdate,
		deviceSelector: deviceSelector,
		payload:        payload,
	})
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "firmware_update", "Update firmware", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "firmware_update", "Update firmware"))

	return &pb.FirmwareUpdateResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

func (s *Service) Unpair(ctx context.Context, deviceSelector *pb.DeviceSelector) (*pb.UnpairResponse, error) {
	commandBatchLogUUID, deviceCount, err := s.processCommand(ctx, &Command{commandType: commandtype.Unpair, deviceSelector: deviceSelector, payload: nil})
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "unpair", "Unpair", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "unpair", "Unpair"))

	return &pb.UnpairResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

// verifyUserCredentials verifies the provided username and password match the current authenticated user
// This provides an additional security layer for sensitive operations
func (s *Service) verifyUserCredentials(ctx context.Context, username string, password string) error {
	// Validate required fields
	if username == "" {
		return fleeterror.NewInvalidArgumentError("user_username is required")
	}
	if password == "" {
		return fleeterror.NewInvalidArgumentError("user_password is required")
	}

	// Use auth service to verify credentials are valid
	if err := s.credentialsVerifier.VerifyCredentials(ctx, username, password); err != nil {
		return err
	}

	// Verify the username matches the current authenticated session user
	// This prevents a logged-in user from providing another user's credentials
	user, err := s.userStore.GetUserByUsername(ctx, username)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting user: %v", err)
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}

	if user.ID != info.UserID {
		return fleeterror.NewForbiddenErrorf("username does not match authenticated user")
	}

	return nil
}

func (s *Service) UpdateMinerPassword(
	ctx context.Context,
	deviceSelector *pb.DeviceSelector,
	newPassword string,
	currentPassword string,
	userUsername string,
	userPassword string,
) (*pb.UpdateMinerPasswordResponse, error) {
	// Validate required fields
	if newPassword == "" {
		return nil, fleeterror.NewInvalidArgumentError("new_password is required")
	}
	if currentPassword == "" {
		return nil, fleeterror.NewInvalidArgumentError("current_password is required")
	}

	// Verify user credentials before allowing password change
	if err := s.verifyUserCredentials(ctx, userUsername, userPassword); err != nil {
		return nil, err
	}

	payload := dto.UpdateMinerPasswordPayload{
		NewPassword:     newPassword,
		CurrentPassword: currentPassword,
	}

	commandBatchLogUUID, deviceCount, err := s.processCommand(
		ctx,
		&Command{
			commandType:    commandtype.UpdateMinerPassword,
			deviceSelector: deviceSelector,
			payload:        payload,
		},
	)
	if err != nil {
		return nil, err
	}

	s.logCommandActivity(ctx, "update_miner_password", "Manage security", deviceCount, commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "update_miner_password", "Manage security"))

	return &pb.UpdateMinerPasswordResponse{
		BatchIdentifier: commandBatchLogUUID,
	}, nil
}

func (s *Service) StreamCommandBatchUpdates(ctx context.Context, msg *pb.StreamCommandBatchUpdatesRequest) (<-chan *pb.StreamCommandBatchUpdatesResponse, error) {
	_, err := session.GetInfo(ctx)
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

// CheckCommandCapabilities validates command support for selected devices.
// Returns capability check results with unsupported miners grouped by model/firmware.
func (s *Service) CheckCommandCapabilities(ctx context.Context, req *pb.CheckCommandCapabilitiesRequest) (*pb.CheckCommandCapabilitiesResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}

	return s.capabilityChecker.CheckCapabilities(ctx, req.DeviceSelector, req.CommandType, info.OrganizationID)
}

// maxBatchDeviceResults caps the number of per-device rows returned by
// GetCommandBatchDeviceResults. The activity-log drill-down only needs a
// bounded slice; larger batches can be fetched page-by-page in a follow-up.
const maxBatchDeviceResults = 5000

// GetCommandBatchDeviceResults returns the per-device outcome for a command
// batch so the activity-log UI can drill into which miners succeeded or
// failed. Org-scoped via the session user.
//
// details_pruned semantics:
//   - header missing: the batch is unknown in this org -- return NotFound.
//   - header present but no per-device rows: either the command is still
//     PENDING (no worker has written a row yet) or retention has purged them.
//     In both cases we return an empty list and set details_pruned=true when
//     the batch is FINISHED; for PENDING/PROCESSING we leave it false so the
//     UI knows to keep streaming live updates.
func (s *Service) GetCommandBatchDeviceResults(ctx context.Context, req *pb.GetCommandBatchDeviceResultsRequest) (*pb.GetCommandBatchDeviceResultsResponse, error) {
	if req == nil || strings.TrimSpace(req.BatchIdentifier) == "" {
		return nil, fleeterror.NewInvalidArgumentError("batch_identifier is required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}

	header, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.GetBatchHeaderForOrgRow, error) {
		return q.GetBatchHeaderForOrg(ctx, sqlc.GetBatchHeaderForOrgParams{
			Uuid: req.BatchIdentifier,
			// NullInt64 lets sqlc round-trip the optional column; Valid is always
			// true here because session info always carries a concrete org.
			OrganizationID: sql.NullInt64{Int64: info.OrganizationID, Valid: true},
		})
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("command batch %s not found", req.BatchIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("error loading batch header: %v", err)
	}

	// Authoritative counts come from the aggregate query so they remain
	// consistent with total_count even when device_results is capped by
	// maxBatchDeviceResults.
	counts, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.GetBatchStatusAndDeviceCountsRow, error) {
		return q.GetBatchStatusAndDeviceCounts(ctx, req.BatchIdentifier)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("command batch %s not found", req.BatchIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("error loading batch counts: %v", err)
	}

	rows, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]sqlc.ListBatchDeviceResultsRow, error) {
		return q.ListBatchDeviceResults(ctx, req.BatchIdentifier)
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error loading batch device results: %v", err)
	}

	truncated := len(rows) > maxBatchDeviceResults
	capped := rows
	if truncated {
		capped = capped[:maxBatchDeviceResults]
	}

	results := make([]*pb.CommandBatchDeviceResult, 0, len(capped))
	for _, row := range capped {
		entry := &pb.CommandBatchDeviceResult{
			Status:    deviceCommandStatusToProto(row.Status),
			UpdatedAt: timestamppb.New(row.UpdatedAt),
		}
		if row.DeviceIdentifier.Valid {
			entry.DeviceIdentifier = row.DeviceIdentifier.String
		}
		if row.ErrorInfo.Valid {
			msg := row.ErrorInfo.String
			entry.ErrorMessage = &msg
		}
		results = append(results, entry)
	}

	// #nosec G115 -- counts come from SUM over command_on_device_log, bounded by
	// devices_count which itself fits in int32.
	successCount := int32(counts.SuccessfulDevices)
	// #nosec G115 -- same bound as successCount.
	failureCount := int32(counts.FailedDevices)

	// Pruned only when the batch had devices to begin with, is FINISHED, and
	// every per-device row is gone. An empty-selector batch (devices_count=0)
	// never had details to prune, so we keep details_pruned=false for it.
	// Mid-run PENDING/PROCESSING batches with partial rows also stay
	// details_pruned=false so the UI knows to keep polling.
	detailsPruned := header.DevicesCount > 0 &&
		header.Status == sqlc.BatchStatusEnumFINISHED &&
		len(rows) == 0

	return &pb.GetCommandBatchDeviceResultsResponse{
		BatchIdentifier: header.Uuid,
		CommandType:     header.Type,
		Status:          string(header.Status),
		TotalCount:      header.DevicesCount,
		SuccessCount:    successCount,
		FailureCount:    failureCount,
		DeviceResults:   results,
		DetailsPruned:   detailsPruned,
		Truncated:       truncated,
	}, nil
}

func deviceCommandStatusToProto(s sqlc.DeviceCommandStatusEnum) string {
	switch s {
	case sqlc.DeviceCommandStatusEnumSUCCESS:
		return "success"
	case sqlc.DeviceCommandStatusEnumFAILED:
		return "failed"
	default:
		return strings.ToLower(string(s))
	}
}
