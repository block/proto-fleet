package devicewrite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
)

// DirectBindingHash binds the deletion permit to the exact handle resolved
// before retirement. Only its digest is persisted; credentials are never copied.
func DirectBindingHash(binding sqlc.GetDeviceWithCredentialsAndIPByDeviceIdentifierRow) (string, error) {
	encoded, err := json.Marshal(binding)
	if err != nil {
		return "", fleeterror.NewInternalError("miner connection identity could not be bound")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// PrepareDeletion runs in the same transaction as the soft delete. It rejects
// owned/reserved devices as a whole and invalidates every pre-delete epoch.
func PrepareDeletion(ctx context.Context, q sqlc.Querier, organizationID int64, identifiers []string) (resultErr error) {
	defer func() {
		var pgErr *pgconn.PgError
		if errors.As(resultErr, &pgErr) && pgErr.Code == "55P03" {
			resultErr = blocked()
		}
	}()
	if err := q.SetLocalDeviceDeletionLockTimeout(ctx); err != nil {
		return err
	}
	targets, err := q.GetDeviceWriteDeletionTargets(ctx, sqlc.GetDeviceWriteDeletionTargetsParams{OrgID: organizationID, DeviceIdentifiers: identifiers})
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(targets))
	allKeys := []string{}
	keysByID := make(map[int64][]string, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
		allKeys = append(allKeys, target.IdentityKeys...)
		keysByID[target.ID] = target.IdentityKeys
	}
	if err := q.LockDeviceWriteIdentities(ctx, allKeys); err != nil {
		return err
	}
	if err := q.EnsureDeviceWriteFences(ctx, ids); err != nil {
		return err
	}
	fences, err := q.LockDeviceWriteDeletionFences(ctx, ids)
	if err != nil {
		return err
	}
	if len(fences) != len(ids) {
		return blocked()
	}
	for _, fence := range fences {
		if fence.OrganizationID != organizationID || fence.State != "host" {
			return blocked()
		}
	}
	// Keep the admission lock order: identity stripes, fence, then binding rows.
	// Rediscovery can change a binding before these row locks are acquired. A
	// changed identity must restart retirement rather than acquire new stripes
	// after row locks, which could invert another admission's lock order.
	for _, lock := range []func(context.Context, []int64) error{
		q.LockDeviceWriteDeletionDevices,
		q.LockDeviceWriteDeletionDiscoveries,
		q.LockDeviceWriteDeletionPairings,
		q.LockDeviceWriteDeletionCredentials,
		q.LockDeviceWriteDeletionRoutes,
	} {
		if err := lock(ctx, ids); err != nil {
			return err
		}
	}
	lockedTargets, err := q.GetDeviceWriteDeletionTargets(ctx, sqlc.GetDeviceWriteDeletionTargetsParams{OrgID: organizationID, DeviceIdentifiers: identifiers})
	if err != nil {
		return err
	}
	if len(lockedTargets) != len(targets) {
		return blocked()
	}
	for _, target := range lockedTargets {
		keys, exists := keysByID[target.ID]
		if !exists || !slices.Equal(keys, target.IdentityKeys) {
			return blocked()
		}
	}
	// Only paired direct Proto adapters support the captured post-delete Unpair.
	bindings, err := q.GetDeviceWriteDeletionBindings(ctx, sqlc.GetDeviceWriteDeletionBindingsParams{DeviceIds: ids, OrganizationID: organizationID})
	if err != nil {
		return err
	}
	permits := make([]deletionPermit, 0, len(bindings))
	for _, binding := range bindings {
		hash, err := DirectBindingHash(sqlc.GetDeviceWithCredentialsAndIPByDeviceIdentifierRow(binding))
		if err != nil {
			return err
		}
		permits = append(permits, deletionPermit{DeviceID: binding.ID, OrganizationID: organizationID, BindingHash: hash, IdentityKeys: keysByID[binding.ID]})
	}
	epochs, err := q.AdvanceDeletedDeviceWriteFences(ctx, sqlc.AdvanceDeletedDeviceWriteFencesParams{DeviceIds: ids, OrganizationID: organizationID})
	if err != nil {
		return err
	}
	if len(epochs) != len(ids) {
		return blocked()
	}
	epochByID := make(map[int64]int64, len(epochs))
	for _, row := range epochs {
		epochByID[row.DeviceID] = row.Epoch
	}
	if err := q.CancelPendingDeviceWritesForDeletion(ctx, ids); err != nil {
		return err
	}
	if len(permits) != 0 {
		for i := range permits {
			permits[i].Epoch = epochByID[permits[i].DeviceID]
		}
		encoded, err := json.Marshal(permits)
		if err != nil {
			return fleeterror.NewInternalError("deletion permits could not be bound")
		}
		if err := q.CreateDeletionUnpairPermits(ctx, encoded); err != nil {
			return err
		}
	}
	return nil
}

type deletionPermit struct {
	DeviceID       int64    `json:"device_id"`
	OrganizationID int64    `json:"organization_id"`
	Epoch          int64    `json:"epoch"`
	BindingHash    string   `json:"binding_hash"`
	IdentityKeys   []string `json:"identity_keys"`
}

// RunDeletionUnpair claims a single post-delete operation. Waiting for an
// unpair worker does not count as physical dispatch. A successor supersedes an
// unclaimed permit; a claimed running/unknown attempt blocks physical reuse.
func (s *Service) RunDeletionUnpair(ctx context.Context, organizationID, deviceID int64, bindingHash string, call func(context.Context) error) (resultErr error) {
	if db.GetTxQueries(ctx) != nil {
		return fleeterror.NewFailedPreconditionError("device I/O requires committed admission")
	}
	attempt, err := db.WithTransactionTimeout(ctx, s.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) (*Attempt, error) {
		permit, err := q.GetDeletionUnpairPermit(ctx, sqlc.GetDeletionUnpairPermitParams{ID: deviceID, OrgID: organizationID})
		if err != nil {
			return nil, lookupError(err)
		}
		if permit.BindingHash != bindingHash {
			return nil, blocked()
		}
		if err := q.LockDeviceWriteIdentities(ctx, permit.IdentityKeys); err != nil {
			return nil, err
		}
		fence, err := q.LockDeviceWriteFenceRecord(ctx, sqlc.LockDeviceWriteFenceRecordParams{DeviceID: permit.DeviceID, OrganizationID: organizationID})
		if err != nil {
			return nil, err
		}
		if fence.State != "host" || fence.Epoch != permit.Epoch {
			return nil, blocked()
		}
		permit, err = q.LockDeletionUnpairPermit(ctx, sqlc.LockDeletionUnpairPermitParams{ID: deviceID, OrgID: organizationID})
		if err != nil {
			return nil, lookupError(err)
		}
		if permit.BindingHash != bindingHash {
			return nil, blocked()
		}
		current, err := q.IsDeletionUnpairIdentityCurrent(ctx, sqlc.IsDeletionUnpairIdentityCurrentParams{OrganizationID: organizationID, IdentityKeys: permit.IdentityKeys})
		if err != nil {
			return nil, err
		}
		if !current {
			return nil, blocked()
		}
		drained, err := q.IsRetiredDeviceWriteDrained(ctx, permit.IdentityKeys)
		if err != nil {
			return nil, err
		}
		if !drained {
			return nil, blocked()
		}
		attempt := &Attempt{service: s, id: uuid.New(), deviceID: permit.DeviceID, organizationID: organizationID, epoch: permit.Epoch, deletionBinding: permit.BindingHash}
		if err := q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{AttemptID: attempt.id, DeviceID: attempt.deviceID, OrganizationID: organizationID, Epoch: attempt.epoch, IdentityKeys: permit.IdentityKeys, OwnerInstanceID: processInstanceID, OperationKind: operationMutation, CommandType: "Unpair"}); err != nil {
			return nil, err
		}
		count, err := q.ClaimDeletionUnpairPermit(ctx, sqlc.ClaimDeletionUnpairPermitParams{AttemptID: uuid.NullUUID{UUID: attempt.id, Valid: true}, DeviceID: attempt.deviceID})
		if err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, blocked()
		}
		return attempt, nil
	})
	if err != nil {
		return err
	}
	returned := false
	defer func() {
		if !returned {
			// Keep the claimed attempt blocking physical reuse after a panic.
			return
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimepolicy.CommandTransactionBound)
		defer cancel()
		if err := attempt.Finish(finishCtx, resultErr); err != nil {
			// Preserve the unpair outcome and leave its claimed attempt
			// blocking physical reuse until recovery supplies evidence.
			slog.Error("failed to persist deleted miner unpair completion",
				"attempt_id", attempt.id, "device_id", deviceID, "error", err)
		}
	}()
	resultErr = call(attempt.Context(ctx))
	returned = true
	return resultErr
}

// DispatchDeletedUnpair recognizes only the private context minted by the
// claimed permit. It cannot authorize another device or another SDK method.
func (s *Service) DispatchDeletedUnpair(ctx context.Context, organizationID, deviceID int64, bindingHash string, call func(context.Context) error) (bool, error) {
	attempt, ok := ctx.Value(attemptContextKey{}).(*Attempt)
	if !ok || attempt.deletionBinding == "" {
		return false, nil
	}
	if attempt.service.conn != s.conn || attempt.organizationID != organizationID || attempt.deviceID != deviceID || attempt.deletionBinding != bindingHash {
		return true, blocked()
	}
	return true, Dispatch(ctx, call)
}

func lockIdentity(ctx context.Context, q sqlc.Querier, deviceID int64) ([]string, error) {
	keys, err := q.GetDeviceWriteIdentityKeys(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, blocked()
	}
	if err := q.LockDeviceWriteIdentities(ctx, keys); err != nil {
		return nil, err
	}
	drained, err := q.IsRetiredDeviceWriteDrained(ctx, keys)
	if err != nil {
		return nil, err
	}
	if !drained {
		return nil, blocked()
	}
	return keys, nil
}
