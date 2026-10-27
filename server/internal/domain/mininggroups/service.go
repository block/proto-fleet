package mininggroups

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"

	"errors"
	"slices"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
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

type MinerGetter interface {
	GetMinerForCommand(ctx context.Context, deviceID int64) (interfaces.Miner, error)
}
type Service struct {
	conn    *sql.DB
	queries sqlc.Querier
	writes  *devicewrite.Service
	miners  MinerGetter
	pools   *hostedaccounts.PoolService
	secrets *encrypt.Service
}

func NewService(conn *sql.DB, miners MinerGetter, pools *hostedaccounts.PoolService, secrets *encrypt.Service) *Service {
	return &Service{conn: conn, queries: db.NewFailoverResettingQuerier(db.NewRetryDB(conn)), writes: devicewrite.NewService(conn), miners: miners, pools: pools, secrets: secrets}
}

type State struct {
	Record   sqlc.ContainerMiningGroupState
	Observed sdk.MiningGroupResult
	Desired  []sqlc.HashboardPoolIntent
}
type binding struct {
	ConfigID   string `json:"config_id"`
	Configured bool   `json:"configured"`
}
type savedStep struct {
	Ciphertext   string   `json:"ciphertext"`
	ConfigID     string   `json:"config_id"`
	AffectedMACs []string `json:"affected_macs,omitempty"`
}

func active(status string) bool {
	return status == "pending" || status == statusApplying || status == "blocked"
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
		return s.writes.WithBindingObservation(ctx, target.ID, func(q sqlc.Querier) error {
			fresh, err := q.GetMiningGroupTarget(ctx, sqlc.GetMiningGroupTargetParams{OrgID: info.OrganizationID, DeviceIdentifier: identifier})
			if err != nil {
				return err
			}
			return authorize(ctx, q, info.UserID, info.OrganizationID, fresh.SiteID, authz.PermMinerRead)
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
				if readErr != nil && !row.ObservedAt.Valid {
					return readErr
				}
				if readErr == nil {
					observed = next
					row.Observed, err = encode(next)
					if err != nil {
						return err
					}
					row.ObservedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
					if err := save(ctx, q, row); err != nil {
						return err
					}
				}
				// Return the authorized snapshot captured under the same binding lock,
				// including its original freshness when the controller is disconnected.
				result = State{Record: row, Observed: observed, Desired: desired}
				return nil
			})
		})
	})
	return result, err
}
func (s *Service) call(ctx context.Context, id int64, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	m, err := s.miners.GetMinerForCommand(ctx, id)
	if err != nil {
		return sdk.MiningGroupResult{}, err
	}
	c, ok := m.(sdk.DeviceMiningGroups)
	if !ok {
		return sdk.MiningGroupResult{}, fleeterror.NewUnimplementedError("controller does not support mining groups")
	}
	return c.MiningGroups(ctx, r)
}
func (s *Service) Submit(ctx context.Context, identifier, requestID, configID string, boards []int64, expected int64) (uuid.UUID, error) {
	info, err := OperatorPrincipal(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(requestID)
	if err != nil {
		return uuid.Nil, fleeterror.NewInvalidArgumentError("request ID must be a UUID")
	}
	boards = slices.Clone(boards)
	slices.Sort(boards)
	kind, permission := "pools", authz.PermMinerUpdatePools
	if configID == "" {
		kind, permission = operationActivate, authz.PermMinerActivateMiningGroups
		if len(boards) != 0 {
			return uuid.Nil, fleeterror.NewInvalidArgumentError("activation cannot select boards")
		}
	} else if len(boards) == 0 || len(boards) > 288 || len(slices.Compact(slices.Clone(boards))) != len(boards) {
		return uuid.Nil, fleeterror.NewInvalidArgumentError("select unique hashboards")
	}
	raw, err := encode([]any{identifier, kind, configID, boards, expected})
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
	err = s.writes.BeginWorkflow(ctx, info.OrganizationID, target.ID, func(q sqlc.Querier, attempt uuid.UUID, epoch int64) error {
		current, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{OrgID: info.OrganizationID, DeviceIdentifier: identifier})
		if err != nil {
			return err
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
			return fleeterror.NewFailedPreconditionError("refresh the container before changing pools")
		}
		var observation sdk.MiningGroupResult
		if err := decode(row.Observed, &observation); err != nil {
			return err
		}
		if !observation.Supported {
			return fleeterror.NewFailedPreconditionError("controller firmware does not support mining groups")
		}
		if kind == "pools" {
			if err := q.PruneDetachedHashboardPoolIntents(ctx, sqlc.PruneDetachedHashboardPoolIntentsParams{OrganizationID: info.OrganizationID, DeviceID: target.ID}); err != nil {
				return err
			}
			if !observation.Enabled || row.Revision != expected {
				return fleeterror.NewFailedPreconditionError("refresh the mining group configuration and try again")
			}
			profile, err := q.GetHostMiningGroupPoolRevision(ctx, sqlc.GetHostMiningGroupPoolRevisionParams{OrganizationID: info.OrganizationID, RevisionID: configID})
			if err != nil {
				return fleeterror.NewNotFoundError("pool configuration not found")
			}
			if _, err := s.pools.HostGroupCredentials(ctx, q, info.OrganizationID, configID); err != nil {
				return err
			}
			resolved, err := q.GetMiningGroupBoards(ctx, sqlc.GetMiningGroupBoardsParams{OrganizationID: info.OrganizationID, HashboardIds: boards})
			if err != nil {
				return err
			}
			if len(resolved) != len(boards) {
				return fleeterror.NewFailedPreconditionError("selected boards changed or their inventory is stale")
			}
			for _, b := range resolved {
				if b.DeviceID != target.ID {
					return fleeterror.NewInvalidArgumentError("boards must belong to this container")
				}
				if err := q.SetHashboardPoolIntent(ctx, sqlc.SetHashboardPoolIntentParams{OrganizationID: info.OrganizationID, HashboardID: b.HashboardID, DeviceID: target.ID, InstallationID: b.InstallationID, MacAddress: b.MacAddress, AccountID: profile.AccountID, PoolRevisionID: configID, OperationID: id}); err != nil {
					return err
				}
			}
			// These intents and admission are still uncommitted. A failed full
			// preflight rolls them back, including replacements of earlier intent.
			intents, err := q.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: info.OrganizationID, DeviceID: target.ID})
			if err != nil {
				return err
			}
			desired := map[string]string{}
			for _, intent := range intents {
				desired[intent.MacAddress] = intent.PoolRevisionID
			}
			snapshot, err := s.plannerSnapshot(ctx, q, row, observation)
			if err != nil {
				return err
			}
			if err := ValidateAssignment(desired, snapshot); err != nil {
				return fleeterror.NewFailedPreconditionError(err.Error())
			}
		}
		row.OperationID = uuid.NullUUID{UUID: id, Valid: true}
		row.AttemptID = uuid.NullUUID{UUID: attempt, Valid: true}
		row.ActorID = sql.NullInt64{Int64: info.UserID, Valid: true}
		row.OperationKind = kind
		row.ActivationResumeMining = sql.NullBool{}
		row.ActivationStopRequested = false
		row.Status = "pending"
		row.Message = "Waiting to apply configuration"
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
		if err := recordAcceptedActivity(ctx, q, row, identifier, current.SiteID, configID, boards); err != nil {
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
