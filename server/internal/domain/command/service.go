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

	"connectrpc.com/connect"
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
	"github.com/block/proto-fleet/server/internal/domain/pools/rewriter"
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

	// sv2Caps nil means "treat every device as having no dynamic SV2
	// info", so SV2 pools assigned to SV1 devices only succeed via the
	// proxy. Set via SetStratumV2Resolvers so callers that don't care
	// about SV2 need not touch the constructor.
	sv2Caps   SV2CapabilityResolver
	sv2Proxy  rewriter.ProxyConfig
	sv2Health ProxyHealthChecker
}

// SV2CapabilityResolver supplies per-device capability snapshots to the
// pool-assignment preflight. Implementations typically read the latest
// telemetry scrape's StratumV2Support and overlay it on static/model
// driver caps via rewriter.MergeCapabilities. A nil resolver is valid and
// means "treat every device as SV1-only" — useful during the phased
// plugin rollout before every plugin reports dynamic SV2 support.
//
// orgID is passed in so the static-capability lookup can be tenant-scoped
// (the device store keys by org_id and we'd otherwise have to dig the
// session out of the context every call).
type SV2CapabilityResolver interface {
	ResolveCapabilities(ctx context.Context, orgID int64, deviceIdentifiers []string) map[string]rewriter.DeviceCapabilities
}

// ProxyHealthChecker reports whether the bundled tProxy is up. The
// preflight consults this before approving any proxied route — pushing
// the proxy URL to miners while the translator is down would take a
// fleet of SV1 miners off-pool until the operator notices. Up() and
// HasState() match sv2.HealthMonitor's surface, so the production
// wiring is just a direct hand-off; tests can plug in a fake.
type ProxyHealthChecker interface {
	Up() bool
	HasState() bool
}

// SetStratumV2Resolvers wires SV2 inputs onto an existing service. Kept
// out of NewService so the deployment wiring in main.go can install
// optional SV2 plumbing without breaking tests that construct Service
// positionally. A nil resolver + zero ProxyConfig is valid (SV1-only
// deployment) and is the default until explicitly overridden.
func (s *Service) SetStratumV2Resolvers(caps SV2CapabilityResolver, proxy rewriter.ProxyConfig, health ProxyHealthChecker) {
	s.sv2Caps = caps
	s.sv2Proxy = proxy
	s.sv2Health = health
}

// effectiveProxyConfig returns the static rewriter ProxyConfig with
// ProxyEnabled forced to false when the bundled translator is not
// known to be reachable. Without this, a successful UpdateMiningPools
// could push the proxy's miner-facing URL to every SV1-only miner
// while the proxy container is down, taking the affected fleet
// off-pool. Health-unknown is treated as "down" — preflight should
// fail closed until the first probe lands.
func (s *Service) effectiveProxyConfig() rewriter.ProxyConfig {
	if !s.sv2Proxy.ProxyEnabled {
		return s.sv2Proxy
	}
	if s.sv2Health == nil || !s.sv2Health.HasState() || !s.sv2Health.Up() {
		gated := s.sv2Proxy
		gated.ProxyEnabled = false
		return gated
	}
	return s.sv2Proxy
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
	// Guard ordering matters: this check must fire before getDevicesCount so
	// unit tests without a wired deviceStore still hit the org-id check
	// cleanly, and invalid inputs do not waste a store round-trip.
	if organizationID <= 0 {
		return "", fleeterror.NewInternalErrorf("cannot create command batch: session missing organization_id")
	}

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

		// Org-scope the lookup so a caller can't probe foreign-tenant
		// devices through include_devices selectors. The query already
		// drops cross-tenant identifiers; we additionally cross-check
		// the row count so a partial-match request fails closed instead
		// of silently operating on a strict subset.
		ids, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]int64, error) {
			return q.GetDeviceIDsByDeviceIdentifiersForOrg(ctx, sqlc.GetDeviceIDsByDeviceIdentifiersForOrgParams{
				DeviceIdentifiers: x.IncludeDevices.DeviceIdentifiers,
				OrgID:             info.OrganizationID,
			})
		})
		if err != nil {
			return nil, err
		}
		if len(ids) != len(x.IncludeDevices.DeviceIdentifiers) {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"include_devices: %d of %d identifiers are not in this organization or do not exist",
				len(x.IncludeDevices.DeviceIdentifiers)-len(ids),
				len(x.IncludeDevices.DeviceIdentifiers),
			)
		}
		return ids, nil
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
		Protocol:        sqlstores.DBProtocolToProto(pool.Protocol),
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
		// CEL on RawPoolInfo enforces the same scheme whitelist as
		// pools.v1.PoolConfig, but ProtocolFromURL is the authoritative
		// runtime check — surface a typed INVALID_ARGUMENT instead of
		// silently treating an unrecognised scheme as SV1, otherwise
		// the rewriter would skip the SV2 preflight for raw pools that
		// slipped past CEL (e.g. CEL not yet running for a stale client
		// in dev).
		protocol, err := rewriter.ProtocolFromURL(source.RawPool.Url)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentErrorf("raw pool url: %v", err)
		}
		return &dto.MiningPool{
			Priority:        defaultPoolPriority + priorityIncrement,
			URL:             source.RawPool.Url,
			Username:        source.RawPool.Username,
			Password:        password,
			AppendMinerName: shouldAppendMinerNameToUsername(source.RawPool.Username),
			Protocol:        protocol,
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

	// Resolve the device selector once — preflight, batch log, and
	// EnqueuePerDevice all consume this list against the same snapshot.
	deviceIDs, err := s.getDeviceIDs(ctx, deviceSelector)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting device IDs from device selector: %v", err)
	}
	if len(deviceIDs) > maxCommitDevices {
		// Commit-time preflight materializes per-device JSON payloads
		// in memory before writing queue rows. Without a cap a fleet-
		// wide update on a very large org turns one unary RPC into a
		// CPU/memory spike on the request path. The cap is high enough
		// to cover any realistic single-deployment fleet but bounded
		// enough that timing out / OOM-ing the API isn't a one-RPC
		// move from an authenticated user. Operators with larger
		// fleets need to scope the selector (manufacturer/model
		// filters, sites, etc.) and run multiple updates.
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"pool update supports up to %d devices per request; selector resolved to %d. Narrow the selector and run multiple updates.",
			maxCommitDevices, len(deviceIDs),
		)
	}

	// Synchronous preflight. The rewriter decides, per device, which slot
	// URLs would get pushed; mismatches (SV2 pool + SV1 device + proxy off,
	// or >1 SV2 slot routing through the single bundled proxy) surface as
	// typed FAILED_PRECONDITION details so the UI doesn't have to parse
	// strings. See docs/stratum-v2-plan.md "URL rewriting — the core logic".
	deviceIDByIdentifier, perDeviceBytes, mismatches, err := s.preflightAndSerializePayloads(ctx, pld, deviceIDs)
	if err != nil {
		return nil, err
	}
	if len(mismatches) > 0 {
		return nil, mismatchesToFailedPrecondition(mismatches)
	}

	// Batch-log payload is the template (pre-rewrite) payload, so activity
	// log readers see the operator's intent rather than N distinct per-device
	// payloads. Device rows carry the resolved bytes instead.
	templateBytes, err := json.Marshal(pld)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error marshalling payload: %v", err)
	}

	if !s.executionService.IsRunning() {
		slog.Error("command execution service is not running, attempting to start it")
		if startErr := s.executionService.Start(ctx); startErr != nil {
			return nil, fleeterror.NewInternalErrorf("failed to start command execution service: %v", startErr)
		}
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info from ctx: %v", err)
	}

	commandBatchLogUUID, err := s.saveCommandBatchLogToDB(ctx, info.UserID, info.OrganizationID,
		&Command{commandType: commandtype.UpdateMiningPools, deviceSelector: deviceSelector, payload: pld},
		templateBytes,
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error saving command batch log to db: %v", err)
	}

	// Re-key the per-device payload map onto internal device IDs, since
	// queue rows are keyed on id, not identifier.
	payloadsByDeviceID := make(map[int64][]byte, len(perDeviceBytes))
	for identifier, body := range perDeviceBytes {
		id, ok := deviceIDByIdentifier[identifier]
		if !ok {
			return nil, fleeterror.NewInternalErrorf("preflight returned payload for unknown device %q", identifier)
		}
		payloadsByDeviceID[id] = body
	}

	if err := s.messageQueue.EnqueuePerDevice(ctx, commandBatchLogUUID, commandtype.UpdateMiningPools, payloadsByDeviceID); err != nil {
		return nil, fleeterror.NewInternalErrorf("error enqueuing per-device mining-pool updates: %v", err)
	}

	s.logCommandActivity(ctx, "update_mining_pools", "Edit pool", len(deviceIDs), commandBatchLogUUID)
	s.initializeStatusUpdateRoutine(commandBatchLogUUID,
		s.buildActivityCompletedCallback(ctx, commandBatchLogUUID, "update_mining_pools", "Edit pool"))

	return &pb.UpdateMiningPoolsResponse{BatchIdentifier: commandBatchLogUUID}, nil
}

// maxPreviewDevices caps the number of devices a single
// PreviewMiningPoolAssignment call can evaluate. The preview is the
// only RPC that materializes a per-device result for an entire fleet
// in a single unary response, and the UI auto-fires it on every pool
// edit, so an unbounded call against a large org turns a dry run into
// a synchronous resource-exhaustion path. The cap is high enough to
// cover any realistic single-deployment fleet but bounded enough that
// CPU/memory/response size stay predictable. Direct RPC callers
// targeting larger fleets need to scope their selectors (or use the
// commit RPC, which already runs preflight and returns a typed
// FAILED_PRECONDITION on mismatches without materializing per-device
// detail).
const maxPreviewDevices = 1000

// maxCommitDevices caps the number of devices a single
// UpdateMiningPools call can target. The commit path runs synchronous
// preflight + per-device payload marshal before writing queue rows,
// all in one unary RPC, so an unbounded fleet-wide update is a
// straightforward request-path DoS lever. Higher than the preview cap
// because operators legitimately push pool changes to large fleets
// (commits are deliberate; the preview cap exists because the UI
// auto-fires it). Operators above this need to scope the selector
// (manufacturer/model/site filters) and run multiple updates.
const maxCommitDevices = 5000

// PreviewResult is the typed return of PreviewMiningPoolAssignment.
// Previews is the per-device detail (empty when the preview was
// short-circuited). SkipReason tells callers why no detail was
// returned: UNSPECIFIED on a clean run, SIZE_EXCEEDED when the
// selector exceeded the cap. The handler maps SkipReason onto the
// proto response so the UI can distinguish "no mismatches" from
// "preview not run; commit-time preflight is authoritative."
type PreviewResult struct {
	Previews   []*pb.DevicePoolPreview
	SkipReason pb.PreviewSkipReason
}

// PreviewMiningPoolAssignment runs the same preflight UpdateMiningPools
// uses, but returns the per-device resolution instead of enqueuing
// anything. The UI calls it before enabling Save; the CLI exposes it as
// a dry-run mode. Read-only — no credentials check, no batch log, no
// queue writes.
func (s *Service) PreviewMiningPoolAssignment(
	ctx context.Context,
	deviceSelector *pb.DeviceSelector,
	defaultPool, backup1Pool, backup2Pool *pb.PoolSlotConfig,
) (PreviewResult, error) {
	pld, err := s.createUpdateMiningPoolsPayload(ctx, defaultPool, backup1Pool, backup2Pool)
	if err != nil {
		return PreviewResult{}, err
	}

	deviceIDs, err := s.getDeviceIDs(ctx, deviceSelector)
	if err != nil {
		return PreviewResult{}, fleeterror.NewInternalErrorf("error getting device IDs from device selector: %v", err)
	}
	if len(deviceIDs) == 0 {
		return PreviewResult{}, nil
	}
	if len(deviceIDs) > maxPreviewDevices {
		// Returning a typed skip rather than an error keeps Save
		// reachable on huge fleets — the commit path runs the same
		// preflight server-side and rejects with FAILED_PRECONDITION
		// if there's a real mismatch. Blocking Save on a missing
		// preview would lock operators out of pool assignment for
		// any selection over the cap.
		slog.Warn("preview skipped (size exceeded)", "device_count", len(deviceIDs), "cap", maxPreviewDevices)
		return PreviewResult{SkipReason: pb.PreviewSkipReason_PREVIEW_SKIP_REASON_SIZE_EXCEEDED}, nil
	}

	idByIdentifier, err := s.resolveDeviceIdentifiers(ctx, deviceIDs)
	if err != nil {
		return PreviewResult{}, err
	}
	capsByIdentifier := s.resolveSV2Capabilities(ctx, idByIdentifier)

	devices := make([]preflight.Device, 0, len(idByIdentifier))
	for identifier := range idByIdentifier {
		devices = append(devices, preflight.Device{
			Identifier:   identifier,
			Capabilities: capsByIdentifier[identifier],
		})
	}

	out, err := preflight.Run(preflight.Input{
		Slots:   buildPreflightSlotAssignments(pld),
		Devices: devices,
		Proxy:   s.effectiveProxyConfig(),
	})
	if err != nil {
		return PreviewResult{}, fleeterror.NewInternalErrorf("pool-assignment preflight: %v", err)
	}

	return PreviewResult{Previews: previewsToProto(out.Devices)}, nil
}

// previewsToProto projects preflight.DeviceResult onto the proto response
// shape. Lives here rather than in the preflight package so the preflight
// stays free of RPC-shape concerns and remains reusable by CLIs and tests.
func previewsToProto(devs []preflight.DeviceResult) []*pb.DevicePoolPreview {
	out := make([]*pb.DevicePoolPreview, 0, len(devs))
	for _, d := range devs {
		slots := make([]*pb.SlotPreview, 0, len(d.Slots))
		for _, s := range d.Slots {
			slots = append(slots, &pb.SlotPreview{
				Slot:              s.ProtoSlot,
				EffectiveProtocol: s.Protocol,
				EffectiveUrl:      s.EffectiveURL,
				RewriteReason:     s.ProtoReason,
				Warning:           s.Warning,
			})
		}
		out = append(out, &pb.DevicePoolPreview{
			DeviceIdentifier: d.DeviceIdentifier,
			Slots:            slots,
			DeviceWarning:    d.DeviceWarning,
		})
	}
	return out
}

// preflightAndSerializePayloads runs the pool-assignment preflight for a
// concrete device-ID set and returns (deviceID-by-identifier map,
// per-device marshaled payloads, mismatches). When there are mismatches
// the per-device payloads are nil — the caller rejects the whole batch
// synchronously and never enqueues.
func (s *Service) preflightAndSerializePayloads(
	ctx context.Context,
	template *dto.UpdateMiningPoolsPayload,
	deviceIDs []int64,
) (map[string]int64, map[string][]byte, []preflight.Mismatch, error) {
	if len(deviceIDs) == 0 {
		return nil, nil, nil, nil
	}

	idByIdentifier, err := s.resolveDeviceIdentifiers(ctx, deviceIDs)
	if err != nil {
		return nil, nil, nil, err
	}

	capsByIdentifier := s.resolveSV2Capabilities(ctx, idByIdentifier)

	devices := make([]preflight.Device, 0, len(idByIdentifier))
	for identifier := range idByIdentifier {
		devices = append(devices, preflight.Device{
			Identifier:   identifier,
			Capabilities: capsByIdentifier[identifier],
		})
	}

	slots := buildPreflightSlotAssignments(template)
	out, err := preflight.Run(preflight.Input{
		Slots:   slots,
		Devices: devices,
		Proxy:   s.effectiveProxyConfig(),
	})
	if err != nil {
		return nil, nil, nil, fleeterror.NewInternalErrorf("pool-assignment preflight: %v", err)
	}

	if out.HasMismatch {
		return idByIdentifier, nil, out.Mismatches(), nil
	}

	// Fast path: on an SV1-only fleet (or any mix where no device gets
	// proxy-rewritten) every device's payload equals the template. We
	// marshal once and share the bytes rather than json.Marshal per
	// device, which matters on wide updates (hundreds of miners).
	templateBytes, err := json.Marshal(template)
	if err != nil {
		return nil, nil, nil, fleeterror.NewInternalErrorf("error marshalling pool template: %v", err)
	}

	perDevice := make(map[string][]byte, len(out.Devices))
	for _, d := range out.Devices {
		if slotsMatchTemplate(template, d) {
			perDevice[d.DeviceIdentifier] = templateBytes
			continue
		}
		body, marshalErr := marshalPerDevicePayload(template, d)
		if marshalErr != nil {
			return nil, nil, nil, marshalErr
		}
		perDevice[d.DeviceIdentifier] = body
	}

	return idByIdentifier, perDevice, nil, nil
}

// slotsMatchTemplate reports whether every resolved slot's URL equals
// the template's slot URL. True means the rewriter decided no rewrite
// was needed for this device — we can skip the marshal and share the
// template bytes.
func slotsMatchTemplate(template *dto.UpdateMiningPoolsPayload, d preflight.DeviceResult) bool {
	for _, s := range d.Slots {
		switch s.Slot {
		case rewriter.SlotDefault:
			if s.EffectiveURL != template.DefaultPool.URL {
				return false
			}
		case rewriter.SlotBackup1:
			if template.Backup1Pool == nil || s.EffectiveURL != template.Backup1Pool.URL {
				return false
			}
		case rewriter.SlotBackup2:
			if template.Backup2Pool == nil || s.EffectiveURL != template.Backup2Pool.URL {
				return false
			}
		case rewriter.SlotUnspecified:
			// preflight.Run rejects SlotUnspecified at input validation;
			// reaching here would be a programming error. Treat as a
			// non-match so the caller takes the slow per-device marshal
			// path rather than silently dropping the slot.
			return false
		}
	}
	return true
}

func (s *Service) resolveDeviceIdentifiers(ctx context.Context, deviceIDs []int64) (map[string]int64, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info from context: %v", err)
	}
	rows, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]sqlc.GetDeviceIdentifiersByIDsForOrgRow, error) {
		return q.GetDeviceIdentifiersByIDsForOrg(ctx, sqlc.GetDeviceIdentifiersByIDsForOrgParams{
			DeviceIds: deviceIDs,
			OrgID:     info.OrganizationID,
		})
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error resolving device identifiers: %v", err)
	}
	// Fail closed when fewer rows come back than were requested: a
	// device disappearing between getDeviceIDs and this lookup (race
	// against a delete, or a row that was never owned by this org)
	// must not silently shrink the target set. Without this check the
	// preflight + commit path would proceed against a subset while the
	// activity log still recorded the original count, hiding a partial
	// pool repoint behind a "success" response.
	if len(rows) != len(deviceIDs) {
		return nil, fleeterror.NewFailedPreconditionErrorf(
			"device set changed during pool update: %d of %d devices are missing or no longer in this organization",
			len(deviceIDs)-len(rows), len(deviceIDs),
		)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.DeviceIdentifier] = r.ID
	}
	return out, nil
}

func (s *Service) resolveSV2Capabilities(ctx context.Context, idByIdentifier map[string]int64) map[string]rewriter.DeviceCapabilities {
	if s.sv2Caps == nil {
		return nil
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		// No session in this code path is a programmer bug — every
		// preflight/commit caller goes through an authenticated handler.
		// Returning nil keeps the request safe (every device falls back
		// to SV1-only routing) while the panic-free Warn surfaces the
		// programming error in logs.
		slog.Warn("sv2 capability resolver: no session in context; treating fleet as SV1-only", "error", err)
		return nil
	}
	identifiers := make([]string, 0, len(idByIdentifier))
	for k := range idByIdentifier {
		identifiers = append(identifiers, k)
	}
	return s.sv2Caps.ResolveCapabilities(ctx, info.OrganizationID, identifiers)
}

// buildPreflightSlotAssignments extracts the (slot, pool) pairs from the
// shared payload template. The template's protocol field comes from the
// DB read (createMiningPoolDTO) or the raw-pool path
// (createMiningPoolDTOFromSlotConfig), so it faithfully represents the
// operator's intent at commit time.
func buildPreflightSlotAssignments(template *dto.UpdateMiningPoolsPayload) []preflight.SlotAssignment {
	slots := []preflight.SlotAssignment{{
		Slot: rewriter.SlotDefault,
		Pool: rewriter.Pool{URL: template.DefaultPool.URL, Protocol: template.DefaultPool.Protocol},
	}}
	if template.Backup1Pool != nil {
		slots = append(slots, preflight.SlotAssignment{
			Slot: rewriter.SlotBackup1,
			Pool: rewriter.Pool{URL: template.Backup1Pool.URL, Protocol: template.Backup1Pool.Protocol},
		})
	}
	if template.Backup2Pool != nil {
		slots = append(slots, preflight.SlotAssignment{
			Slot: rewriter.SlotBackup2,
			Pool: rewriter.Pool{URL: template.Backup2Pool.URL, Protocol: template.Backup2Pool.Protocol},
		})
	}
	return slots
}

// marshalPerDevicePayload clones the template and replaces each slot's
// URL and Protocol with the rewriter's resolved values for this device.
// Both fields can differ per device because of proxied routing: an SV2
// pool rewritten to the SV1-facing translator URL is on-the-wire SV1,
// and downstream drivers branch on Protocol. Keeping the template's
// Protocol intact would make preview show effective_protocol=SV1 while
// dispatch sent protocol=SV2 alongside a stratum+tcp:// URL — exactly
// the parity break the preflight is supposed to prevent.
func marshalPerDevicePayload(template *dto.UpdateMiningPoolsPayload, d preflight.DeviceResult) ([]byte, error) {
	deviceCopy := *template
	for _, s := range d.Slots {
		switch s.Slot {
		case rewriter.SlotDefault:
			deviceCopy.DefaultPool.URL = s.EffectiveURL
			deviceCopy.DefaultPool.Protocol = s.Protocol
		case rewriter.SlotBackup1:
			if deviceCopy.Backup1Pool != nil {
				backup := *deviceCopy.Backup1Pool
				backup.URL = s.EffectiveURL
				backup.Protocol = s.Protocol
				deviceCopy.Backup1Pool = &backup
			}
		case rewriter.SlotBackup2:
			if deviceCopy.Backup2Pool != nil {
				backup := *deviceCopy.Backup2Pool
				backup.URL = s.EffectiveURL
				backup.Protocol = s.Protocol
				deviceCopy.Backup2Pool = &backup
			}
		case rewriter.SlotUnspecified:
			// preflight rejects this at validateInput; reach here only on
			// a programming error, in which case skipping the slot
			// preserves whatever URL the template already had.
		}
	}
	body, err := json.Marshal(&deviceCopy)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error marshalling per-device mining pools payload: %v", err)
	}
	return body, nil
}

// maxMismatchDetails caps the per-error detail payload size for
// commit-time FAILED_PRECONDITION responses. A bad pool assignment
// against a large fleet could otherwise serialize thousands of
// UpdateMiningPoolsMismatch protobuf details into a single Connect-RPC
// response, blowing up CPU/memory on both ends and risking
// intermediary size limits. The summary string still reflects the
// total count so operators know how many devices failed.
const maxMismatchDetails = 100

// mismatchesToFailedPrecondition wraps preflight mismatches in a typed
// FAILED_PRECONDITION error. The UI treats FAILED_PRECONDITION on this
// RPC as "pool assignment would reject some device" and renders the
// typed detail — it never parses the error message. The first
// maxMismatchDetails mismatches are attached as
// UpdateMiningPoolsMismatch proto details; remaining mismatches are
// counted in the summary message but not materialized to keep the
// response bounded.
func mismatchesToFailedPrecondition(mismatches []preflight.Mismatch) error {
	base := fleeterror.NewFailedPreconditionErrorf(
		"pool assignment would fail preflight for %d device(s): %v",
		len(mismatches), summarizeMismatches(mismatches),
	)
	connectErr := base.ConnectError()
	limit := len(mismatches)
	if limit > maxMismatchDetails {
		limit = maxMismatchDetails
	}
	for _, m := range mismatches[:limit] {
		detail, err := connect.NewErrorDetail(&pb.UpdateMiningPoolsMismatch{
			DeviceIdentifier: m.DeviceIdentifier,
			Slot:             m.Slot,
			SlotWarning:      m.SlotWarning,
			DeviceWarning:    m.DeviceWarning,
		})
		if err != nil {
			// connect.NewErrorDetail only fails on proto-marshal errors;
			// our inputs are pure proto messages, so this should never
			// fire. Log and continue rather than swap the error type out
			// from under the caller — preserving the FAILED_PRECONDITION
			// + summary message is more important than the missing
			// detail entry.
			slog.Warn("failed to encode UpdateMiningPoolsMismatch detail; sending without it", "error", err)
			continue
		}
		connectErr.AddDetail(detail)
	}
	if len(mismatches) > maxMismatchDetails {
		slog.Warn("truncated UpdateMiningPoolsMismatch detail payload",
			"total_mismatches", len(mismatches),
			"detail_cap", maxMismatchDetails)
	}
	return connectErr
}

func summarizeMismatches(mismatches []preflight.Mismatch) string {
	if len(mismatches) == 0 {
		return ""
	}
	// A stable short summary, surfaced in logs alongside the typed detail
	// the handler attaches on the Connect error path.
	first := mismatches[0]
	if first.DeviceWarning != 0 {
		return fmt.Sprintf("%s: %s", first.DeviceIdentifier, first.DeviceWarning.String())
	}
	return fmt.Sprintf("%s slot=%s: %s",
		first.DeviceIdentifier, first.Slot.String(), first.SlotWarning.String())
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
