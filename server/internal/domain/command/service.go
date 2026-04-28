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
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/pools/preflight"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	id "github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
)

// TelemetryListener provides interface for telemetry registration/unregistration
type TelemetryListener interface {
	RemoveDevices(ctx context.Context, deviceIDs ...tmodels.DeviceIdentifier) error
}

// PluginCapabilitiesProvider feeds the SV1↔SV2 capability gate.
// Wired via SetPluginCapabilitiesProvider; nil disables the gate.
type PluginCapabilitiesProvider interface {
	GetRawCapabilitiesForDevice(ctx context.Context, driverName, manufacturer, model string) sdk.Capabilities
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
	pluginCaps          PluginCapabilitiesProvider
	capabilityChecker   *CapabilityChecker
	activitySvc         *activity.Service

	resolveDeviceIDsOverride func(context.Context, []string) ([]int64, error)

	// filters run in registration order before commands are enqueued. Registered
	// at startup only; the slice is not mutex-protected.
	filters []CommandFilter
}

// SetPluginCapabilitiesProvider wires the SV1↔SV2 gate's caps source.
// Nil provider leaves the gate open (used by tests).
func (s *Service) SetPluginCapabilitiesProvider(p PluginCapabilitiesProvider) {
	s.pluginCaps = p
}

const defaultPoolPriority uint32 = 0

// maxCallbackRetries bounds callback attempts before marking FINISHED.
// The callback gets up to maxCallbackRetries more post-finish attempts
// to prevent permanent audit gaps from transient DB failures.
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
		ActorType:      actorTypeFromSession(info),
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		BatchID:        &batchIDCopy,
		Metadata:       map[string]any{"batch_id": batchID},
	})
}

// actorTypeFromSession maps session.Info.Actor into the activity ActorType.
// Empty return falls back to the activity service's default (ActorUser).
func actorTypeFromSession(info *session.Info) activitymodels.ActorType {
	if info == nil {
		return ""
	}
	if info.Actor == session.ActorScheduler {
		return activitymodels.ActorScheduler
	}
	return ""
}

// isExternalCommand is true for user/API-key traffic. Internal orchestrators
// must set Actor or Source so preflight skips can use their domain semantics.
func isExternalCommand(info *session.Info) bool {
	return info.Actor == "" && info.Source.ScheduleID == 0
}

// activityEventType mirrors successful command activity event names.
func activityEventType(t commandtype.Type) string {
	switch t {
	case commandtype.StartMining:
		return "start_mining"
	case commandtype.StopMining:
		return "stop_mining"
	case commandtype.SetCoolingMode:
		return "set_cooling_mode"
	case commandtype.SetPowerTarget:
		return "set_power_target"
	case commandtype.UpdateMiningPools:
		return "update_mining_pools"
	case commandtype.DownloadLogs:
		return "download_logs"
	case commandtype.Reboot:
		return "reboot"
	case commandtype.BlinkLED:
		return "blink_led"
	case commandtype.FirmwareUpdate:
		return "firmware_update"
	case commandtype.Unpair:
		return "unpair"
	case commandtype.UpdateMinerPassword:
		return "update_miner_password"
	default:
		return t.String()
	}
}

// logPreflightBlockedStrict records an external preflight rejection. Audit
// failures are fatal so blocked commands always leave a durable trace. The
// store write uses a bounded background ctx so a client disconnect at the
// deny-point cannot suppress the row or degrade FailedPrecondition into
// Internal.
func (s *Service) logPreflightBlockedStrict(
	ctx context.Context,
	commandType commandtype.Type,
	requestedIdentifiers []string,
	skipped []SkippedDevice,
) error {
	if s.activitySvc == nil {
		return nil
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting session info from ctx: %v", err)
	}
	eventType := activityEventType(commandType)
	auditCtx, cancel := context.WithTimeout(context.Background(), finalizerDBTimeout)
	defer cancel()
	return s.activitySvc.LogStrict(auditCtx, activitymodels.Event{
		Category:       activitymodels.CategoryDeviceCommand,
		Type:           "command_preflight_blocked",
		Description:    fmt.Sprintf("Command %q blocked: %d of %d device(s) excluded by preflight filters", eventType, len(skipped), len(requestedIdentifiers)),
		Result:         activitymodels.ResultFailure,
		ActorType:      actorTypeFromSession(info),
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		Metadata:       skipMetadata(eventType, len(requestedIdentifiers), skipped),
	})
}

// logFilterSkips best-effort records a dispatched command whose preflight
// filters excluded some devices. eventType is the snake_case activity name.
func (s *Service) logFilterSkips(
	ctx context.Context,
	eventType string,
	dispatchedCount int,
	skipped []SkippedDevice,
) {
	if s.activitySvc == nil {
		return
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		slog.Warn("failed to log filter skips: session info unavailable", "error", err)
		return
	}
	requestedCount := dispatchedCount + len(skipped)
	s.activitySvc.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryDeviceCommand,
		Type:           "command_filter_skip",
		Description:    fmt.Sprintf("Command %q dispatched with %d device(s) excluded by preflight filters", eventType, len(skipped)),
		Result:         activitymodels.ResultSuccess,
		ActorType:      actorTypeFromSession(info),
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		Metadata:       skipMetadata(eventType, requestedCount, skipped),
	})
}

// skipMetadata is shared by command_preflight_blocked and command_filter_skip.
func skipMetadata(eventType string, requestedCount int, skipped []SkippedDevice) map[string]any {
	skippedIDs := make([]string, 0, len(skipped))
	filterSet := make(map[string]struct{}, len(skipped))
	for _, sk := range skipped {
		skippedIDs = append(skippedIDs, sk.DeviceIdentifier)
		if sk.FilterName != "" {
			filterSet[sk.FilterName] = struct{}{}
		}
	}
	filters := make([]string, 0, len(filterSet))
	for name := range filterSet {
		filters = append(filters, name)
	}
	sort.Strings(filters)
	return map[string]any{
		"command_type":        eventType,
		"requested_count":     requestedCount,
		"skipped_count":       len(skipped),
		"skipped_identifiers": skippedIDs,
		"filters":             filters,
	}
}

// composeFinalizers chains onFinished callbacks so commands like DownloadLogs
// can layer a bundle builder alongside the activity finalizer. Nil callbacks
// are skipped; empty input returns nil. Best-effort: every callback runs even
// if earlier ones fail, so a bundle-builder failure cannot block the activity
// finalizer. The first error is returned so the retry loop in
// initializeStatusUpdateRoutine still knows to retry on the next tick.
// Already-succeeded callbacks are skipped on retry.
//
// NOT SAFE FOR CONCURRENT USE: initializeStatusUpdateRoutine is the only
// call site today and invokes the closure serially.
func composeFinalizers(callbacks ...onFinishedCallbackFunc) onFinishedCallbackFunc {
	type trackedCallback struct {
		fn   onFinishedCallbackFunc
		done bool
	}
	tracked := make([]*trackedCallback, 0, len(callbacks))
	for _, cb := range callbacks {
		if cb != nil {
			tracked = append(tracked, &trackedCallback{fn: cb})
		}
	}
	switch len(tracked) {
	case 0:
		return nil
	case 1:
		// Single callback: initializeStatusUpdateRoutine already guards it
		// with its own callbackDone flag so per-callback tracking is moot.
		return tracked[0].fn
	default:
		return func() error {
			var firstErr error
			for _, tc := range tracked {
				if tc.done {
					continue
				}
				if err := tc.fn(); err != nil {
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				tc.done = true
			}
			return firstErr
		}
	}
}

// finalizerDBTimeout bounds the background transaction used by the activity
// finalizer. Independent of request ctx since the finalizer runs long after
// the originating RPC has returned.
const finalizerDBTimeout = 15 * time.Second

// buildActivityCompletedCallback returns a finalizer that writes the
// '<event_type>.completed' activity row when the batch reaches FINISHED.
// The partial unique index on (batch_id, event_type) plus SQLActivityStore's
// duplicate swallow keep the finalizer's retry loop idempotent.
//
// Session info is captured at call time because the finalizer runs against a
// background context (the originating request ctx is long gone).
//
// Ordering: attempted BEFORE MarkCommandBatchFinished* (up to
// maxCallbackRetries). If pre-mark attempts exhaust, the batch is marked
// FINISHED anyway and the callback gets maxCallbackRetries more post-mark
// attempts before giving up.
func (s *Service) buildActivityCompletedCallback(ctx context.Context, batchID, eventType, description string) onFinishedCallbackFunc {
	if s.activitySvc == nil {
		return nil
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		slog.Warn("command activity finalizer: session info unavailable at command start",
			"error", err, "batch_id", batchID)
		return nil
	}
	userID := info.ExternalUserID
	username := info.Username
	organizationID := info.OrganizationID
	actorType := actorTypeFromSession(info)
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
		// LogStrict surfaces transient DB errors back to the status routine's
		// retry loop; the partial unique index keeps retries idempotent.
		if err := s.activitySvc.LogStrict(finCtx, activitymodels.Event{
			Category:       activitymodels.CategoryDeviceCommand,
			Type:           eventType + activitymodels.CompletedEventSuffix,
			Description:    completionDesc,
			Result:         result,
			ScopeCount:     &scopeCount,
			ActorType:      actorType,
			UserID:         &userID,
			Username:       &username,
			OrganizationID: &organizationID,
			BatchID:        &batchIDCopy,
			Metadata: map[string]any{
				"total_count":   counts.DevicesCount,
				"success_count": counts.SuccessfulDevices,
				"failure_count": counts.FailedDevices,
			},
		}); err != nil {
			return fleeterror.NewInternalErrorf("finalizer writing completion for %s: %v", batchID, err)
		}
		return nil
	}
}

func (s *Service) saveCommandBatchLogToDB(ctx context.Context, userID, organizationID int64, command *Command, payloadBytes []byte, devicesCount int) (string, error) {
	if organizationID <= 0 {
		return "", fleeterror.NewInternalErrorf("cannot create command batch: session missing organization_id")
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
			DevicesCount:   int32(devicesCount), //nolint:gosec // bounded by fleet size
			Payload:        pqtype.NullRawMessage{RawMessage: payloadBytes, Valid: len(payloadBytes) > 0},
			OrganizationID: sql.NullInt64{Int64: organizationID, Valid: true},
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
		callbackDone := false
		batchMarkedFinished := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !batchMarkedFinished && !processingMarkedInDB {
					isProcessing, err := s.statusUpdateIsProcessingBranch(ctx, commandBatchLogUUID)
					if err != nil {
						slog.Error("error in isProcessing branch", "error", err)
						return
					}
					processingMarkedInDB = isProcessing
				}
				if !batchMarkedFinished {
					isFinished, err := s.statusUpdateIsFinishedBranch(ctx, commandBatchLogUUID)
					if err != nil {
						slog.Error("error in isFinished branch", "error", err)
						return
					}
					if !isFinished {
						continue
					}
				}

				if onFinishedCallback != nil && !callbackDone {
					if callbackErr := onFinishedCallback(); callbackErr != nil {
						callbackRetryCount++
						if !batchMarkedFinished && callbackRetryCount < maxCallbackRetries {
							slog.Error("onFinished callback failed, will retry before marking batch finished",
								"error", callbackErr, "retry", callbackRetryCount)
							continue
						}
						if callbackRetryCount >= maxCallbackRetries*2 {
							slog.Error("onFinished callback permanently failed",
								"error", callbackErr, "retries", callbackRetryCount)
							callbackDone = true
						} else {
							slog.Error("onFinished callback failed, will retry",
								"error", callbackErr, "retry", callbackRetryCount)
						}
					} else {
						callbackDone = true
					}
				}

				if !batchMarkedFinished {
					if markErr := s.getMarkFinishedBatchFunction(processingMarkedInDB)(ctx, commandBatchLogUUID); markErr != nil {
						slog.Error("error marking batch finished, will retry", "error", markErr)
						continue
					}
					batchMarkedFinished = true
				}

				if callbackDone || onFinishedCallback == nil {
					return
				}
			}
		}
	}()
}

// RegisterFilter appends to the startup-only preflight chain.
func (s *Service) RegisterFilter(f CommandFilter) {
	s.filters = append(s.filters, f)
}

// resolveSelectorIdentifiers expands selectors to device_identifier strings for
// preflight filtering.
func (s *Service) resolveSelectorIdentifiers(ctx context.Context, selector *pb.DeviceSelector) ([]string, error) {
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

		return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]string, error) {
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

			return q.GetFilteredDeviceIdentifiers(ctx, sqlc.GetFilteredDeviceIdentifiersParams{
				OrgID:              info.OrganizationID,
				DeviceStatus:       deviceStatus,
				PairingStatus:      pairingStatus,
				ModelFilter:        modelFilter,
				ManufacturerFilter: manufacturerFilter,
			})
		})
	case *pb.DeviceSelector_IncludeDevices:
		if x.IncludeDevices == nil {
			return []string{}, nil
		}
		// Isolate caller-owned proto slices from filter implementations.
		out := make([]string, len(x.IncludeDevices.DeviceIdentifiers))
		copy(out, x.IncludeDevices.DeviceIdentifiers)
		return out, nil
	default:
		return nil, fleeterror.NewInternalErrorf("resolveSelectorIdentifiers called with unknown selector type: %v", x)
	}
}

// resolveIdentifiersToDeviceIDs converts post-filter identifiers for the queue.
func (s *Service) resolveIdentifiersToDeviceIDs(ctx context.Context, identifiers []string) ([]int64, error) {
	if len(identifiers) == 0 {
		return []int64{}, nil
	}
	if s.resolveDeviceIDsOverride != nil {
		return s.resolveDeviceIDsOverride(ctx, identifiers)
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]int64, error) {
		return q.GetDeviceIDsByDeviceIdentifiers(ctx, identifiers)
	})
}

// processCommand resolves selectors, filters, writes the batch row, and
// enqueues work. External callers fail on skips; internal callers may inspect
// CommandResult.Skipped.
func (s *Service) processCommand(ctx context.Context, command *Command) (*CommandResult, error) {
	if !s.executionService.IsRunning() {
		slog.Error("command execution service is not running, attempting to start it")
		err := s.executionService.Start(ctx)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to start command execution service: %v", err)
		}
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info from ctx: %v", err)
	}

	// Selector resolution now runs before batch creation, so validate org first.
	if info.OrganizationID <= 0 {
		return nil, fleeterror.NewInternalErrorf("cannot create command batch: session missing organization_id")
	}

	identifiers, err := s.resolveSelectorIdentifiers(ctx, command.deviceSelector)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error resolving device identifiers: %v", err)
	}

	kept, skipped, err := applyFilters(ctx, s.filters, CommandFilterInput{
		CommandType:       command.commandType,
		OrganizationID:    info.OrganizationID,
		Actor:             info.Actor,
		Source:            info.Source,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("preflight filter failed: %v", err)
	}

	// External callers fail-closed on any skip. Resolve original identifiers
	// first so a stale selector with zero live devices returns InvalidArgument
	// regardless of how the kept/skipped partition fell.
	if isExternalCommand(info) && len(skipped) > 0 {
		deviceIDs, err := s.resolveIdentifiersToDeviceIDs(ctx, identifiers)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error resolving identifiers to device IDs: %v", err)
		}
		if len(deviceIDs) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("no devices matched selector")
		}
		if err := s.logPreflightBlockedStrict(ctx, command.commandType, identifiers, skipped); err != nil {
			return nil, fleeterror.NewInternalErrorf("logging preflight block: %v", err)
		}
		return nil, fleeterror.NewFailedPreconditionErrorf(
			"command blocked: %d of %d device(s) excluded by preflight filters",
			len(skipped), len(identifiers))
	}

	if len(kept) == 0 && len(skipped) > 0 {
		return &CommandResult{Skipped: skipped}, nil
	}
	if len(kept) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("no devices matched selector")
	}

	payloadBytes, err := json.Marshal(command.payload)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error marshalling payload: %v", err)
	}

	deviceIDs, err := s.resolveIdentifiersToDeviceIDs(ctx, kept)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error resolving identifiers to device IDs: %v", err)
	}
	if len(deviceIDs) == 0 {
		if !isExternalCommand(info) {
			return &CommandResult{Skipped: skipped}, nil
		}
		return nil, fleeterror.NewInvalidArgumentError("no devices matched selector")
	}

	batchLogIdentifier, err := s.saveCommandBatchLogToDB(ctx, info.UserID, info.OrganizationID, command, payloadBytes, len(deviceIDs))
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error saving command batch log to db: %v", err)
	}

	err = s.messageQueue.Enqueue(ctx, batchLogIdentifier, command.commandType, deviceIDs, command.payload)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error enqueuing a batch of commands: %v", err)
	}

	return &CommandResult{
		BatchIdentifier: batchLogIdentifier,
		DispatchedCount: len(deviceIDs),
		Skipped:         skipped,
	}, nil
}

// finalizeDispatch starts batch tracking for real dispatches and records any
// best-effort filter skips. Blocked external commands are audited in
// processCommand before this helper is reached.
func (s *Service) finalizeDispatch(ctx context.Context, result *CommandResult, eventType, description string) {
	if result.BatchIdentifier == "" {
		return
	}
	s.logCommandActivity(ctx, eventType, description, result.DispatchedCount, result.BatchIdentifier)
	s.initializeStatusUpdateRoutine(result.BatchIdentifier,
		s.buildActivityCompletedCallback(ctx, result.BatchIdentifier, eventType, description))
	if len(result.Skipped) > 0 {
		s.logFilterSkips(ctx, eventType, result.DispatchedCount, result.Skipped)
	}
}

func (s *Service) Reboot(ctx context.Context, deviceSelector *pb.DeviceSelector) (*CommandResult, error) {
	result, err := s.processCommand(ctx, &Command{commandType: commandtype.Reboot, deviceSelector: deviceSelector, payload: nil})
	if err != nil {
		return nil, err
	}
	s.finalizeDispatch(ctx, result, "reboot", "Reboot")
	return result, nil
}

// StopMining stops mining on the specified miners
func (s *Service) StopMining(ctx context.Context, deviceSelector *pb.DeviceSelector) (*CommandResult, error) {
	result, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.StopMining, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}
	s.finalizeDispatch(ctx, result, "stop_mining", "Sleep")
	return result, nil
}

// StartMining starts mining on the specified miners
func (s *Service) StartMining(ctx context.Context, deviceSelector *pb.DeviceSelector) (*CommandResult, error) {
	result, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.StartMining, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}
	s.finalizeDispatch(ctx, result, "start_mining", "Wake up")
	return result, nil
}

func (s *Service) SetCoolingMode(ctx context.Context, deviceSelector *pb.DeviceSelector, modeType commonpb.CoolingMode) (*CommandResult, error) {
	cm := dto.CoolingModePayload{Mode: modeType}
	result, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.SetCoolingMode, deviceSelector: deviceSelector, payload: cm},
	)
	if err != nil {
		return nil, err
	}
	s.finalizeDispatch(ctx, result, "set_cooling_mode", "Cooling mode changed")
	return result, nil
}

func (s *Service) SetPowerTarget(ctx context.Context, deviceSelector *pb.DeviceSelector, performanceMode pb.PerformanceMode) (*CommandResult, error) {
	pt := dto.PowerTargetPayload{
		PerformanceMode: performanceMode,
	}
	result, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.SetPowerTarget, deviceSelector: deviceSelector, payload: pt},
	)
	if err != nil {
		return nil, err
	}
	s.finalizeDispatch(ctx, result, "set_power_target", fmt.Sprintf("Power target changed to %s", performanceMode.String()))
	return result, nil
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

// SV2Skips summarizes the SV1 miners excluded from an UpdateMiningPools
// dispatch when the assigned URL is Stratum V2.
type SV2Skips struct {
	SkippedCount      int
	SelectedCount     int
	IncompatibleTypes []string
}

// sv2PreflightOutcome describes a partial-dispatch result. The dispatch
// selector is replaced with IncludeDevices(KeptIdentifiers); Skips is
// surfaced on the response.
type sv2PreflightOutcome struct {
	KeptIdentifiers []string
	Skips           *SV2Skips
}

// preflightSV2Capabilities returns nil when no filtering is needed,
// an outcome when partial dispatch should proceed, or a
// FAILED_PRECONDITION error when every selected miner is incompatible.
func (s *Service) preflightSV2Capabilities(ctx context.Context, selector *pb.DeviceSelector, pld *dto.UpdateMiningPoolsPayload) (*sv2PreflightOutcome, error) {
	if s.pluginCaps == nil {
		slog.Warn("SV2 preflight skipped: plugin caps provider not wired", "default_pool_url", pld.DefaultPool.URL)
		return nil, nil
	}
	slots := preflightSlotsFromPayload(pld)
	if !anySV2(slots) {
		return nil, nil
	}

	identifiers, err := s.resolveSelectorIdentifiers(ctx, selector)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error resolving devices for SV2 preflight: %v", err)
	}
	deviceIDs, err := s.resolveIdentifiersToDeviceIDs(ctx, identifiers)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error resolving device IDs for SV2 preflight: %v", err)
	}
	if len(deviceIDs) == 0 {
		slog.Info("SV2 preflight: selector resolved to zero devices", "default_pool_url", pld.DefaultPool.URL)
		return nil, nil
	}

	rows, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]sqlc.GetDeviceIdentifiersByIDsRow, error) {
		return q.GetDeviceIdentifiersByIDs(ctx, deviceIDs)
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error resolving device identifiers for SV2 preflight: %v", err)
	}

	type capsKey struct{ driver, manufacturer, model string }
	nativeSV2 := map[capsKey]bool{}
	devices := make([]preflight.Device, 0, len(rows))
	for _, r := range rows {
		key := capsKey{r.DriverName, r.Manufacturer.String, r.Model.String}
		native, cached := nativeSV2[key]
		if !cached {
			caps := s.pluginCaps.GetRawCapabilitiesForDevice(ctx, key.driver, key.manufacturer, key.model)
			native = caps[sdk.CapabilityNativeStratumV2]
			nativeSV2[key] = native
		}
		devices = append(devices, preflight.Device{
			Identifier:      r.DeviceIdentifier,
			Make:            r.Manufacturer.String,
			Model:           r.Model.String,
			NativeStratumV2: native,
		})
	}

	mismatches := preflight.Run(devices, slots)
	slog.Info("SV2 preflight evaluated",
		"device_count", len(devices),
		"slot_count", len(slots),
		"mismatches", len(mismatches),
	)
	if len(mismatches) == 0 {
		return nil, nil
	}

	skippedIDs := make(map[string]struct{}, len(mismatches))
	typeSet := make(map[string]struct{}, len(mismatches))
	for _, m := range mismatches {
		skippedIDs[m.DeviceIdentifier] = struct{}{}
		typeSet[formatMinerType(m.Make, m.Model)] = struct{}{}
	}
	sortedTypes := make([]string, 0, len(typeSet))
	for t := range typeSet {
		sortedTypes = append(sortedTypes, t)
	}
	sort.Strings(sortedTypes)

	if len(skippedIDs) >= len(devices) {
		return nil, sv2AllIncompatibleError(len(skippedIDs), sortedTypes)
	}

	kept := make([]string, 0, len(devices)-len(skippedIDs))
	for _, d := range devices {
		if _, skip := skippedIDs[d.Identifier]; !skip {
			kept = append(kept, d.Identifier)
		}
	}
	return &sv2PreflightOutcome{
		KeptIdentifiers: kept,
		Skips: &SV2Skips{
			SkippedCount:      len(skippedIDs),
			SelectedCount:     len(devices),
			IncompatibleTypes: sortedTypes,
		},
	}, nil
}

// sv2AllIncompatibleError is returned when every selected miner is
// SV2-incompatible.
func sv2AllIncompatibleError(deviceCount int, sortedTypes []string) error {
	plural := "s"
	if deviceCount == 1 {
		plural = ""
	}
	return fleeterror.NewFailedPreconditionErrorf(
		"%d miner%s can't use this Stratum V2 pool. Incompatible types: %s",
		deviceCount, plural, strings.Join(sortedTypes, ", "),
	)
}

// preflightSlotsFromPayload projects the UpdateMiningPools payload onto
// the (slot, URL) pairs preflight needs.
func preflightSlotsFromPayload(pld *dto.UpdateMiningPoolsPayload) []preflight.SlotAssignment {
	slots := []preflight.SlotAssignment{{Slot: preflight.SlotDefault, URL: pld.DefaultPool.URL}}
	if pld.Backup1Pool != nil {
		slots = append(slots, preflight.SlotAssignment{Slot: preflight.SlotBackup1, URL: pld.Backup1Pool.URL})
	}
	if pld.Backup2Pool != nil {
		slots = append(slots, preflight.SlotAssignment{Slot: preflight.SlotBackup2, URL: pld.Backup2Pool.URL})
	}
	return slots
}

func anySV2(slots []preflight.SlotAssignment) bool {
	for _, s := range slots {
		if strings.HasPrefix(s.URL, "stratum2+") {
			return true
		}
	}
	return false
}


// formatMinerType renders make/model as a human-readable type label.
// Falls back to "unknown type" when both are empty so the toast never
// reads "Incompatible types: , ".
func formatMinerType(make, model string) string {
	make = strings.TrimSpace(make)
	model = strings.TrimSpace(model)
	switch {
	case make != "" && model != "":
		return make + " " + model
	case make != "":
		return make
	case model != "":
		return model
	default:
		return "unknown type"
	}
}

func (s *Service) UpdateMiningPools(
	ctx context.Context,
	deviceSelector *pb.DeviceSelector,
	defaultPool, backup1Pool, backup2Pool *pb.PoolSlotConfig,
	userUsername string,
	userPassword string,
) (*CommandResult, error) {
	if err := s.verifyUserCredentials(ctx, userUsername, userPassword); err != nil {
		return nil, err
	}

	pld, err := s.createUpdateMiningPoolsPayload(ctx, defaultPool, backup1Pool, backup2Pool)
	if err != nil {
		return nil, err
	}

	outcome, err := s.preflightSV2Capabilities(ctx, deviceSelector, pld)
	if err != nil {
		return nil, err
	}
	if outcome != nil {
		deviceSelector = &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: outcome.KeptIdentifiers},
			},
		}
	}

	result, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.UpdateMiningPools, deviceSelector: deviceSelector, payload: pld},
	)
	if err != nil {
		return nil, err
	}
	if outcome != nil {
		result.SV2Skips = outcome.Skips
	}
	s.finalizeDispatch(ctx, result, "update_mining_pools", "Edit pool")
	return result, nil
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

	commandBatchLogUUID, err := s.saveCommandBatchLogToDB(ctx, info.UserID, info.OrganizationID, command, payloadBytes, len(deviceIdentifiers))
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

func (s *Service) DownloadLogs(ctx context.Context, deviceSelector *pb.DeviceSelector) (*CommandResult, error) {
	result, err := s.processCommand(
		ctx,
		&Command{commandType: commandtype.DownloadLogs, deviceSelector: deviceSelector, payload: nil},
	)
	if err != nil {
		return nil, err
	}

	if result.BatchIdentifier != "" {
		// Bundle callback runs first so the ZIP is on disk before the activity
		// log marks the batch as completed; the activity finalizer then writes
		// the completion row. Both are chained through composeFinalizers.
		bundleCb := s.filesService.DownloadLogsOnFinishedCallback(result.BatchIdentifier)
		activityCb := s.buildActivityCompletedCallback(ctx, result.BatchIdentifier, "download_logs", "Download logs")
		s.logCommandActivity(ctx, "download_logs", "Download logs", result.DispatchedCount, result.BatchIdentifier)
		s.initializeStatusUpdateRoutine(result.BatchIdentifier, composeFinalizers(bundleCb, activityCb))
	}

	return result, nil
}

func (s *Service) BlinkLED(ctx context.Context, deviceSelector *pb.DeviceSelector) (*CommandResult, error) {
	result, err := s.processCommand(ctx, &Command{commandType: commandtype.BlinkLED, deviceSelector: deviceSelector, payload: nil})
	if err != nil {
		return nil, err
	}
	s.finalizeDispatch(ctx, result, "blink_led", "Blink LEDs")
	return result, nil
}

func (s *Service) FirmwareUpdate(ctx context.Context, deviceSelector *pb.DeviceSelector, firmwareFileID string) (*CommandResult, error) {
	if _, err := s.filesService.GetFirmwareFilePath(firmwareFileID); err != nil {
		return nil, fleeterror.NewInvalidArgumentError(fmt.Sprintf("invalid firmware_file_id: %v", err))
	}

	payload := dto.FirmwareUpdatePayload{FirmwareFileID: firmwareFileID}
	result, err := s.processCommand(ctx, &Command{
		commandType:    commandtype.FirmwareUpdate,
		deviceSelector: deviceSelector,
		payload:        payload,
	})
	if err != nil {
		return nil, err
	}
	s.finalizeDispatch(ctx, result, "firmware_update", "Update firmware")
	return result, nil
}

func (s *Service) Unpair(ctx context.Context, deviceSelector *pb.DeviceSelector) (*CommandResult, error) {
	result, err := s.processCommand(ctx, &Command{commandType: commandtype.Unpair, deviceSelector: deviceSelector, payload: nil})
	if err != nil {
		return nil, err
	}
	s.finalizeDispatch(ctx, result, "unpair", "Unpair")
	return result, nil
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
) (*CommandResult, error) {
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

	result, err := s.processCommand(
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
	s.finalizeDispatch(ctx, result, "update_miner_password", "Manage security")
	return result, nil
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
// failed. Org-scoped via command_batch_log.organization_id.
//
// details_pruned is true only when the batch is FINISHED with devices_count>0
// and no per-device rows remain. PENDING/PROCESSING batches keep it false so
// the UI knows to keep polling; empty-selector batches (devices_count=0) also
// keep it false because they never had details to prune.
func (s *Service) GetCommandBatchDeviceResults(ctx context.Context, req *pb.GetCommandBatchDeviceResultsRequest) (*pb.GetCommandBatchDeviceResultsResponse, error) {
	if req == nil || strings.TrimSpace(req.BatchIdentifier) == "" {
		return nil, fleeterror.NewInvalidArgumentError("batch_identifier is required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}

	// All three queries share a single transaction so header/counts/rows
	// remain consistent with each other. sql.ErrNoRows must be translated
	// inside the callback: WithTransaction's retry wrapper reformats any
	// non-FleetError with %v, so the sentinel can't be recovered by
	// errors.Is at the call site.
	type resultsBundle struct {
		header sqlc.GetBatchHeaderForOrgRow
		counts sqlc.GetBatchStatusAndDeviceCountsRow
		rows   []sqlc.ListBatchDeviceResultsRow
	}
	// REPEATABLE READ + ReadOnly so header/counts/rows share one snapshot;
	// the default READ COMMITTED would let concurrent worker writes to
	// command_on_device_log produce inconsistent counts vs device_results.
	bundle, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (resultsBundle, error) {
		var b resultsBundle
		header, hErr := q.GetBatchHeaderForOrg(ctx, sqlc.GetBatchHeaderForOrgParams{
			Uuid:           req.BatchIdentifier,
			OrganizationID: sql.NullInt64{Int64: info.OrganizationID, Valid: true},
		})
		if errors.Is(hErr, sql.ErrNoRows) {
			return b, fleeterror.NewNotFoundErrorf("command batch %s not found", req.BatchIdentifier)
		}
		if hErr != nil {
			return b, hErr
		}
		b.header = header

		counts, cErr := q.GetBatchStatusAndDeviceCounts(ctx, req.BatchIdentifier)
		if cErr != nil {
			return b, cErr
		}
		b.counts = counts

		// Pass (cap + 1) so Go can detect truncation via len(rows) > cap
		// without pulling the full table through the driver first.
		rows, rErr := q.ListBatchDeviceResults(ctx, sqlc.ListBatchDeviceResultsParams{
			Uuid:    req.BatchIdentifier,
			MaxRows: int32(maxBatchDeviceResults + 1),
		})
		if rErr != nil {
			return b, rErr
		}
		b.rows = rows
		return b, nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, err
		}
		return nil, fleeterror.NewInternalErrorf("error loading batch results: %v", err)
	}

	header := bundle.header
	counts := bundle.counts
	rows := bundle.rows

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
		// Compose the display name from the raw captured fields using the same
		// rule the live fleet read path uses (see fleetmanagement.ComposeDeviceName).
		// Historical rows (pre-migration) have all three NULL → name is "" → leave
		// DeviceName unset so the frontend falls back to the UUID.
		if name := fleetmanagement.ComposeDeviceName(
			row.CustomName.String,
			row.Manufacturer.String,
			row.Model.String,
		); name != "" {
			entry.DeviceName = &name
		}
		if row.IpAddress.Valid {
			ip := row.IpAddress.String
			entry.IpAddress = &ip
		}
		if row.MacAddress.Valid {
			mac := row.MacAddress.String
			entry.MacAddress = &mac
		}
		results = append(results, entry)
	}

	// #nosec G115 -- counts come from SUM over command_on_device_log, bounded by
	// devices_count which itself fits in int32.
	successCount := int32(counts.SuccessfulDevices)
	// #nosec G115 -- same bound as successCount.
	failureCount := int32(counts.FailedDevices)

	detailsPruned := header.DevicesCount > 0 &&
		header.Status == sqlc.BatchStatusEnumFINISHED &&
		len(rows) == 0

	return &pb.GetCommandBatchDeviceResultsResponse{
		BatchIdentifier: header.Uuid,
		CommandType:     header.Type,
		Status:          strings.ToLower(string(header.Status)),
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
