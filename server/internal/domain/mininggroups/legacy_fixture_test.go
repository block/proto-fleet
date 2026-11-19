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
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

// submitLegacyRequest models admission by a pre-cutover server solely to create
// historical workflow fixtures. Production only admits canonical pool reviews;
// these fixtures continue to exercise recovery of existing legacy workflows.
func (s *Service) submitLegacyRequest(ctx context.Context, identifier, requestID, configID string, boards []int64, expected int64) (uuid.UUID, error) {
	if configID == "" {
		return s.Activate(ctx, identifier, requestID)
	}
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
	err = s.writes.BeginWorkflow(ctx, info.OrganizationID, target.ID, func(ctx context.Context, q sqlc.Querier, attempt uuid.UUID, epoch int64) error {
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
			disabled, err := q.LegacyPoolWritersDisabled(ctx, info.OrganizationID)
			if err != nil {
				return err
			}
			if disabled {
				return fleeterror.NewFailedPreconditionError("Use reviewed pool configurations to assign board pools.")
			}

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
			snapshot, err := s.plannerSnapshot(ctx, q, row, observation, desired)
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
