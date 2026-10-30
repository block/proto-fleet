package poolapplications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	resultUnverified        = "applied_unverified"
	resultDifferent         = "different"
	codeReadbackUnavailable = "readback_unavailable"
	codeSettingsDiffer      = "settings_differ"
)

type MinerGetter interface {
	GetMinerForCommand(ctx context.Context, deviceID int64) (interfaces.Miner, error)
}

type Worker struct {
	conn       *sql.DB
	queries    sqlc.Querier
	repository *poolconfigurations.Repository
	reviewer   *Reviewer
	miners     MinerGetter
	writes     *devicewrite.Service
	secrets    *encrypt.Service
}

func NewWorker(conn *sql.DB, repository *poolconfigurations.Repository, reviewer *Reviewer, miners MinerGetter, secrets *encrypt.Service) *Worker {
	return &Worker{conn: conn, queries: db.NewFailoverResettingQuerier(db.NewRetryDB(conn)), repository: repository, reviewer: reviewer, miners: miners, writes: devicewrite.NewService(conn), secrets: secrets}
}

// Run never resumes an applying target or blindly retries an uncertain write.
// Claims commit before I/O. A crash therefore leaves visible unfinished work,
// rather than making the same request eligible for an automatic second dispatch.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	var pending sync.WaitGroup
	defer pending.Wait()
	var mu sync.Mutex
	running := map[int64]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.queries.RecoverSettledPoolConfigurationWork(ctx); err != nil {
				slog.WarnContext(ctx, "unable to reconcile settled pool updates")
				continue
			}
			rows, err := w.queries.ListPendingPoolConfigurationWork(ctx)
			if err != nil {
				slog.WarnContext(ctx, "unable to list pending pool updates")
				continue
			}
			for _, row := range rows {
				mu.Lock()
				if len(running) >= 8 {
					mu.Unlock()
					break
				}
				if running[row.DeviceID] {
					mu.Unlock()
					continue
				}
				running[row.DeviceID] = true
				mu.Unlock()
				pending.Add(1)
				go func() {
					defer pending.Done()
					defer func() { mu.Lock(); delete(running, row.DeviceID); mu.Unlock() }()
					attemptCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
					defer cancel()
					if err := w.Execute(attemptCtx, row.OrganizationID, row.OperationID, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID}); err != nil {
						// Never log a driver error or a payload: either may contain credentials.
						slog.WarnContext(ctx, "pool update bookkeeping did not complete", "operation_id", row.OperationID, "device_id", row.DeviceID)
					}
				}()
			}
		}
	}
}

// Execute performs one pending target. The immutable operation revision supplies
// the credentials; the reviewed binding supplies the rendered worker names.
func (w *Worker) Execute(ctx context.Context, orgID int64, operationID string, target poolconfigurations.AssignmentTarget) error {
	work, err := w.queries.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: orgID, OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if work.State != "pending" || work.DeviceID != target.DeviceID {
		return nil
	}
	scope := poolconfigurations.Scope{OrganizationID: orgID, AccountID: work.AccountID.Int64}
	var attempt int32
	state, code := "failed", "rejected"
	finish := func() error {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return db.WithTransactionNoResult(finishCtx, w.conn, func(q sqlc.Querier) error {
			// Rejections before physical admission claim and finish atomically.
			// They cannot leave an orphaned applying target after a crash.
			if attempt == 0 {
				claimed, err := w.repository.ClaimTarget(finishCtx, q, scope, operationID, target)
				if err != nil {
					return err
				}
				return w.repository.FinishTarget(finishCtx, q, scope, operationID, target, claimed, state, code)
			}
			return w.repository.FinishTarget(finishCtx, q, scope, operationID, target, attempt, state, code)
		})
	}
	var binding Binding
	if json.Unmarshal([]byte(work.ReviewedBinding), &binding) != nil || binding.Version != 1 || binding.Device.ID != target.DeviceID || binding.MiningGroups || target.Kind != poolconfigurations.TargetDevice {
		return finish()
	}
	slots, err := poolconfigurations.DecodeSlots(work.Slots)
	if err != nil || len(slots) < 1 || len(slots) > 3 || len(binding.Usernames) != len(slots) {
		return finish()
	}
	endpoints := make([]poolconfigurations.Endpoint, len(slots))
	for i, slot := range slots {
		endpoints[i] = poolconfigurations.Endpoint{ID: slot.ID, URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, HasPassword: slot.HasPassword}
	}
	// The actor is recorded by an authenticated confirmation. Live membership,
	// permissions, account suspension and ownership are still checked below.
	ctx = authn.SetInfo(ctx, &session.Info{UserID: work.ActorID, OrganizationID: orgID, ExternalUserID: work.UserID, Username: work.Username, AuthMethod: session.AuthMethodSession})
	if scope.AccountID != 0 {
		ctx, err = hostedaccounts.WithRequestAccountID(ctx, work.ExternalAccountID)
		if err != nil {
			return finish()
		}
	}
	admitted, invoked := false, false
	resultRecorded := false
	var recordingErr error
	err = w.writes.WithRecordedHostAttempt(ctx, orgID, target.DeviceID, binding.Device.Epoch, commandtype.UpdateMiningPools, func(q sqlc.Querier, physicalID uuid.UUID) error {
		claimed, err := w.repository.ClaimTarget(ctx, q, scope, operationID, target)
		if err != nil {
			return err
		}
		changed, err := q.LinkPoolConfigurationAttempt(ctx, sqlc.LinkPoolConfigurationAttemptParams{OrganizationID: orgID, OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID, Attempt: claimed, AttemptID: uuid.NullUUID{UUID: physicalID, Valid: true}})
		if err != nil {
			return err
		}
		if changed != 1 {
			return fleeterror.NewFailedPreconditionError("pool update admission changed")
		}
		attempt = claimed
		return nil
	}, func(ctx context.Context) error {
		// The record callback can run and then roll back, so its local attempt
		// counter is not proof of admission. This callback runs only after COMMIT.
		admitted = true
		return w.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
			// Review rechecks committed identity admission and retains organization
			// and device binding locks through dispatch to exclude recall and supersession.
			approvals, err := w.reviewer.ReviewTargets(ctx, q, scope, work.ActorID, []poolconfigurations.AssignmentTarget{target}, endpoints)
			if err != nil {
				return err
			}
			if len(approvals) != 1 || approvals[0].Binding != work.ReviewedBinding {
				return fleeterror.NewFailedPreconditionError("reviewed equipment binding changed")
			}
			fresh, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: orgID, OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID})
			if err != nil {
				return err
			}
			if fresh.State != "applying" || fresh.Attempts != attempt {
				return fleeterror.NewFailedPreconditionError("pool update was superseded")
			}
			dispatchErr := w.writes.WithBindingDispatch(ctx, target.DeviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
				miner, err := w.miners.GetMinerForCommand(ctx, target.DeviceID)
				if err != nil {
					code = "unavailable"
					return devicewrite.NotDispatched(err)
				}
				desired := make([]dto.MiningPool, len(slots))
				for i, slot := range slots {
					password, err := w.secrets.Decrypt(slot.PasswordCiphertext)
					if err != nil {
						return fleeterror.NewInternalError("read approved pool credential failed")
					}
					desired[i] = dto.MiningPool{Priority: uint32(i), URL: slot.URL, Username: binding.Usernames[i], Password: string(password)} // #nosec G115 -- at most three slots.
				}
				payload := dto.UpdateMiningPoolsPayload{DefaultPool: desired[0]}
				if len(desired) > 1 {
					payload.Backup1Pool = &desired[1]
				}
				if len(desired) > 2 {
					payload.Backup2Pool = &desired[2]
				}
				invoked = true
				if err := miner.UpdateMiningPools(ctx, payload); err != nil {
					return err
				}
				state, code = verifyPools(ctx, miner, desired)
				return nil
			})
			if dispatchErr == nil {
				// Commit the result before releasing the locks that exclude transfer.
				// finish commits in its own bounded transaction and reports commit
				// failures; the binding transaction only guarantees lock lifetime.
				recordingErr = finish()
				resultRecorded = recordingErr == nil
			}
			// Bookkeeping failure must not change the physical write's outcome.
			return dispatchErr
		})
	})
	if resultRecorded || recordingErr != nil {
		return recordingErr
	}
	if err != nil {
		if !invoked && temporaryPreparationFailure(err) {
			if !admitted {
				// Leave database state untouched. Rolled-back claims remain pending
				// for a later poll. An ambiguous COMMIT may have left applying work;
				// it must retain its admission and follow conservative recovery.
				return err
			}
			// Admission already committed. Report interruption accurately without
			// resetting the claim or automatically replaying the operation.
			code = "interrupted"
		}
		state = "failed"
		if invoked && !devicewrite.IsNotDispatched(err) {
			code = "write_uncertain"
		} else if invoked {
			code = "unavailable"
		}
	}
	return finish()
}

// Only recognized transient preparation errors qualify. A canceled caller
// must not turn a separate permission, ownership or validation error into a
// retry. Inspect the error chain, including the shorter admission timeout.
func temporaryPreparationFailure(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		devicewrite.IsMutationConflict(err) || db.IsRetryablePostgresError(err) || db.IsFailoverPostgresError(err) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "55P03", "57014", "25P04": // lock unavailable, query canceled, transaction timeout
			return true
		}
	}
	return false
}

func verifyPools(ctx context.Context, miner interfaces.Miner, desired []dto.MiningPool) (string, string) {
	runtime, err := miner.GetMiningPools(ctx)
	if err != nil {
		return resultUnverified, codeReadbackUnavailable
	}
	slices.SortFunc(runtime, func(a, b interfaces.MinerConfiguredPool) int { return int(a.Priority) - int(b.Priority) })
	if len(runtime) != len(desired) {
		return resultDifferent, codeSettingsDiffer
	}
	for i, pool := range runtime {
		if pool.Priority != int32(i) || pool.URL != desired[i].URL || pool.Username != desired[i].Username {
			return resultDifferent, codeSettingsDiffer
		}
	} // #nosec G115 -- at most three slots.
	reader, ok := miner.(interfaces.StoredMiningPoolReader)
	if !ok {
		return resultUnverified, codeReadbackUnavailable
	}
	stored, err := reader.GetStoredMiningPools(ctx)
	if err != nil {
		return resultUnverified, codeReadbackUnavailable
	}
	slices.SortFunc(stored, func(a, b interfaces.MinerStoredPool) int { return int(a.Priority) - int(b.Priority) })
	if len(stored) < len(desired) || len(stored) > 3 {
		return resultDifferent, codeSettingsDiffer
	}
	verified := true
	for i, pool := range stored {
		// Firmware may retain unused backup positions. Their stored fields must
		// be explicitly empty; an unreadable password is still unverified.
		expected := dto.MiningPool{}
		if i < len(desired) {
			expected = desired[i]
		}
		if pool.Priority != int32(i) || pool.URL != expected.URL || pool.Username != expected.Username || pool.Password != nil && *pool.Password != expected.Password {
			return resultDifferent, codeSettingsDiffer
		} // #nosec G115 -- at most three slots.
		verified = verified && pool.Password != nil
	}
	if !verified {
		return resultUnverified, codeReadbackUnavailable
	}
	return "verified", ""
}
