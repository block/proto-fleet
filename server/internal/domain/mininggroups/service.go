package mininggroups

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"

	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

const statusApplying = "applying"
const statusComplete = "complete"
const operationActivate = "activate"
const operationDeactivate = "deactivate"
const messageWaitingToApply = "Waiting to apply configuration"

type MinerGetter interface {
	GetMinerForCommand(ctx context.Context, deviceID int64) (interfaces.Miner, error)
}
type Service struct {
	removals         *poolapplications.Checker
	sharedReviewer   *poolapplications.Reviewer
	sharedRepository *poolconfigurations.Repository
	conn             *sql.DB
	queries          sqlc.Querier
	writes           *devicewrite.Service
	miners           MinerGetter
	pools            *hostedaccounts.PoolService
	secrets          *encrypt.Service
}

func NewService(conn *sql.DB, miners MinerGetter, pools *hostedaccounts.PoolService, secrets *encrypt.Service) *Service {
	return &Service{conn: conn, queries: db.NewFailoverResettingQuerier(db.NewRetryDB(conn)), writes: devicewrite.NewService(conn), miners: miners, pools: pools, secrets: secrets}
}

type State struct {
	Record                    sqlc.ContainerMiningGroupState
	Observed                  sdk.MiningGroupResult
	Desired                   []sqlc.HashboardPoolIntent
	DeactivationBlockedReason string
	CanReturnToDefault        bool
}
type binding struct {
	ConfigID   string `json:"config_id"`
	Configured bool   `json:"configured"`
	Inherited  bool   `json:"inherited,omitempty"`
}
type savedStep struct {
	Ciphertext   string   `json:"ciphertext"`
	ConfigID     string   `json:"config_id"`
	AffectedMACs []string `json:"affected_macs,omitempty"`
}

func active(status string) bool {
	return status == "pending" || status == statusApplying || status == "blocked"
}

func changesMode(kind string) bool {
	return kind == operationActivate || kind == operationDeactivate
}

func authorize(ctx context.Context, q sqlc.Querier, userID, orgID int64, site sql.NullInt64, permissions ...string) error {
	perms, err := authz.LoadEffectiveForShare(ctx, q, userID, orgID)
	if err != nil {
		return err
	}
	var siteID *int64
	if site.Valid {
		siteID = &site.Int64
	}
	resource := authz.ResourceContext{SiteID: siteID}
	for _, p := range append([]string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead}, permissions...) {
		if !perms.Has(p, resource) {
			return fleeterror.NewForbiddenError("permission to manage this container is required")
		}
	}
	return nil
}

// OperatorPrincipal requires a host operator session for mining group RPCs.
func OperatorPrincipal(ctx context.Context) (*session.Info, error) {
	i, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	// Background workflows retain user authority, not API-key scope.
	if i.AccountKey || i.AuthMethod != session.AuthMethodSession {
		return nil, fleeterror.NewForbiddenError("operator access required")
	}
	return i, nil
}
func (s *Service) ensure(ctx context.Context, orgID, userID int64, identifier string) (sqlc.GetMiningGroupTargetRow, error) {
	err := db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		target, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{OrgID: orgID, DeviceIdentifier: identifier})
		if err != nil {
			return err
		}
		if err := authorize(ctx, q, userID, orgID, target.SiteID, authz.PermMinerRead); err != nil {
			return err
		}
		if err := q.EnsureDeviceWriteFences(ctx, []int64{target.ID}); err != nil {
			return err
		}
		return q.EnsureMiningGroupState(ctx, sqlc.EnsureMiningGroupStateParams{DeviceID: target.ID, OrganizationID: orgID})
	})
	if err != nil {
		return sqlc.GetMiningGroupTargetRow{}, err
	}
	return s.queries.GetMiningGroupTarget(ctx, sqlc.GetMiningGroupTargetParams{OrgID: orgID, DeviceIdentifier: identifier})
}
func (s *Service) GetState(ctx context.Context, identifier string) (State, error) {
	info, err := OperatorPrincipal(ctx)
	if err != nil {
		return State{}, err
	}
	target, err := s.ensure(ctx, info.OrganizationID, info.UserID, identifier)
	if err != nil {
		return State{}, err
	}
	var result State
	err = s.writes.WithHostObservation(ctx, info.OrganizationID, target.ID, target.Epoch, func(ctx context.Context) error {
		return s.writes.WithExclusiveBindingObservation(ctx, target.ID, func(q sqlc.Querier) error {
			fresh, err := q.GetMiningGroupTarget(ctx, sqlc.GetMiningGroupTargetParams{OrgID: info.OrganizationID, DeviceIdentifier: identifier})
			if err != nil {
				return err
			}
			if err := authorize(ctx, q, info.UserID, info.OrganizationID, fresh.SiteID, authz.PermMinerRead); err != nil {
				return err
			}
			return s.queries.MarkMiningGroupObservationPending(ctx, sqlc.MarkMiningGroupObservationPendingParams{OrganizationID: info.OrganizationID, DeviceID: target.ID})
		}, func(ctx context.Context) error {
			return s.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
				row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{DeviceID: target.ID, OrganizationID: info.OrganizationID})
				if err != nil {
					return err
				}
				var observed sdk.MiningGroupResult
				if err := decode(row.Observed, &observed); err != nil {
					return err
				}
				desired, err := q.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: info.OrganizationID, DeviceID: target.ID})
				if err != nil {
					return err
				}
				next, readErr := s.call(ctx, target.ID, sdk.MiningGroupRequest{Action: "observe"})
				if readErr != nil {
					if !observed.Supported {
						return readErr
					}
					// Keep the authorized display snapshot. The pending marker was
					// committed before this read, even if this transaction rolls back.
					row.ObservedAt = sql.NullTime{}
					result = State{Record: row, Observed: observed, Desired: desired, DeactivationBlockedReason: "Reconnect the controller and refresh its mining-group state before disabling mining groups."}
					return nil
				}
				observed = next
				row.Observed, err = encode(next)
				if err != nil {
					return err
				}
				row.ObservedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
				if err := save(ctx, q, row); err != nil {
					return err
				}
				var readiness deactivationReadiness
				if observed.Supported && observed.Enabled {
					readiness, err = s.deactivationReadiness(ctx, q, row, observed)
					if err != nil {
						return err
					}
				}
				result = State{Record: row, Observed: observed, Desired: desired, DeactivationBlockedReason: readiness.Reason, CanReturnToDefault: readiness.CanReturnToDefault}
				return nil
			})
		})
	})
	return result, err
}
func (s *Service) call(ctx context.Context, id int64, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	m, err := s.miners.GetMinerForCommand(ctx, id)
	if err == nil {
		if c, ok := m.(sdk.DeviceMiningGroups); ok {
			return c.MiningGroups(ctx, r)
		}
		err = fleeterror.NewUnimplementedError("controller does not support mining groups")
	}
	if !r.ReadOnly() {
		// Handle resolution and capability checks cannot start the requested write.
		err = devicewrite.NotDispatched(err)
	}
	return sdk.MiningGroupResult{}, err
}
func (s *Service) Activate(ctx context.Context, identifier, requestID string) (uuid.UUID, error) {
	return s.changeMode(ctx, identifier, requestID, operationActivate)
}

func (s *Service) Deactivate(ctx context.Context, identifier, requestID string) (uuid.UUID, error) {
	return s.changeMode(ctx, identifier, requestID, operationDeactivate)
}

func (s *Service) deactivationReadiness(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult) (deactivationReadiness, error) {
	readiness, err := deactivationBlocker(ctx, q, row, observed, false)
	if err != nil || readiness.Reason != "" {
		return readiness, err
	}
	miner, err := s.miners.GetMinerForCommand(ctx, row.DeviceID)
	if err != nil {
		return deactivationReadiness{}, err
	}
	if checker, ok := miner.(interface {
		CheckMiningGroupDeactivation(ctx context.Context) error
	}); ok {
		if err := checker.CheckMiningGroupDeactivation(ctx); err != nil {
			var refusal fleeterror.FleetError
			if fleeterror.IsFailedPreconditionError(err) && errors.As(err, &refusal) {
				return deactivationReadiness{Reason: refusal.DebugMessage}, nil
			}
			return deactivationReadiness{}, err
		}
	}
	return readiness, nil
}

func (s *Service) changeMode(ctx context.Context, identifier, requestID, kind string) (uuid.UUID, error) {
	info, err := OperatorPrincipal(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(requestID)
	if err != nil {
		return uuid.Nil, fleeterror.NewInvalidArgumentError("request ID must be a UUID")
	}
	raw, err := encode([]any{identifier, kind, "", []int64(nil), int64(0)})
	if err != nil {
		return uuid.Nil, err
	}
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	target, err := s.ensure(ctx, info.OrganizationID, info.UserID, identifier)
	if err != nil {
		return uuid.Nil, err
	}
	// Check replay before creating a new physical admission. The insert below is
	// also checked under the device lock to serialize concurrent retries.
	replay := func(q sqlc.Querier) (bool, error) {
		r, e := q.GetMiningGroupRequest(ctx, sqlc.GetMiningGroupRequestParams{OrganizationID: info.OrganizationID, RequestID: id})
		if errors.Is(e, sql.ErrNoRows) {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		if r.Fingerprint != fingerprint || r.DeviceID != target.ID {
			return false, fleeterror.NewInvalidArgumentError("request ID already used for different settings")
		}
		return true, nil
	}
	if done, e := replay(s.queries); e != nil || done {
		return id, e
	}
	err = s.writes.BeginPoolWorkflow(ctx, info.OrganizationID, target.ID, func(ctx context.Context, q sqlc.Querier, attempt uuid.UUID, epoch int64) error {
		current, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{OrgID: info.OrganizationID, DeviceIdentifier: identifier})
		if err != nil {
			return err
		}
		permission := authz.PermMinerActivateMiningGroups
		if kind == operationDeactivate {
			permission = authz.PermMinerDeactivateMiningGroups
		}
		if err := authorize(ctx, q, info.UserID, info.OrganizationID, current.SiteID, permission, authz.PermMinerStartMining, authz.PermMinerStopMining); err != nil {
			return err
		}
		row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{DeviceID: target.ID, OrganizationID: info.OrganizationID})
		if err != nil {
			return err
		}
		if done, e := replay(q); e != nil {
			return e
		} else if done {
			return errReplay
		}
		if active(row.Status) {
			return fleeterror.NewFailedPreconditionError("a mining group change is already pending")
		}
		if !row.ObservedAt.Valid || time.Since(row.ObservedAt.Time) > 2*time.Minute {
			return fleeterror.NewFailedPreconditionError("refresh the container before changing mining-group mode")
		}
		var observation sdk.MiningGroupResult
		if err := decode(row.Observed, &observation); err != nil {
			return err
		}
		if !observation.Supported {
			return fleeterror.NewFailedPreconditionError("controller firmware does not support mining groups")
		}
		if kind == operationDeactivate {
			reason, err := s.deactivationReadiness(ctx, q, row, observation)
			if err != nil {
				return err
			}
			if reason.Reason != "" {
				return fleeterror.NewFailedPreconditionError(reason.Reason)
			}
		}
		row.OperationID = uuid.NullUUID{UUID: id, Valid: true}
		row.AttemptID = uuid.NullUUID{UUID: attempt, Valid: true}
		row.ActorID = sql.NullInt64{Int64: info.UserID, Valid: true}
		row.OperationKind = kind
		row.ActivationResumeMining = sql.NullBool{}
		row.ActivationStopRequested = false
		row.Status = "pending"
		row.Message = messageWaitingToApply
		row.Step = pqtype.NullRawMessage{}
		row.StepKey = uuid.NullUUID{}
		row.ControllerOperationID = ""
		row.WriteEpoch = epoch
		row.Revision++
		row.RetryCount = 0
		row.TransferFailureCount = 0
		row.NextReconcileAt = time.Now().UTC()
		if err := q.RecordMiningGroupRequest(ctx, sqlc.RecordMiningGroupRequestParams{OrganizationID: info.OrganizationID, RequestID: id, DeviceID: target.ID, Fingerprint: fingerprint}); err != nil {
			return err
		}
		boardIDs := []int64{}
		poolRevision := ""
		if kind == operationActivate {
			capture, err := s.captureActivationPools(ctx, q, row, observation)
			if err != nil {
				return err
			}
			for _, board := range capture.Boards {
				boardIDs = append(boardIDs, board.Binding.HashboardID)
			}
			poolRevision = capture.Source.RevisionID
		}
		if err := recordAcceptedActivity(ctx, q, row, identifier, current.SiteID, poolRevision, boardIDs); err != nil {
			return err
		}
		return save(ctx, q, row)
	})
	if errors.Is(err, errReplay) {
		err = nil
	}
	return id, err
}

var errReplay = errors.New("already submitted")

func save(ctx context.Context, q sqlc.Querier, r sqlc.ContainerMiningGroupState) error {
	return q.SaveMiningGroupState(ctx, sqlc.SaveMiningGroupStateParams{DeviceID: r.DeviceID, OrganizationID: r.OrganizationID, Observed: r.Observed, ObservedAt: r.ObservedAt, Bindings: r.Bindings, OperationID: r.OperationID, ActorID: r.ActorID, OperationKind: r.OperationKind, Status: r.Status, Message: r.Message, Step: r.Step, StepKey: r.StepKey, ControllerOperationID: r.ControllerOperationID, WriteEpoch: r.WriteEpoch, AttemptID: r.AttemptID, Revision: r.Revision, RetryCount: r.RetryCount, NextReconcileAt: r.NextReconcileAt, ActivationResumeMining: r.ActivationResumeMining, ActivationStopRequested: r.ActivationStopRequested, TransferFailureCount: r.TransferFailureCount})
}
