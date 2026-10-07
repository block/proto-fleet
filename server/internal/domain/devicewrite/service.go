// Package devicewrite retains command epochs and actual dispatched attempts.
// Queue completion, retries, and timeouts are not proof of physical drain.
package devicewrite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
)

// One immutable owner ID is shared by every service instance in this process.
const (
	operationMutation    = "mutation"
	operationObservation = "observation"
)

var processInstanceID = uuid.New()
var logProcessOwner sync.Once

type Service struct {
	conn    *sql.DB
	queries sqlc.Querier
}

func NewService(conn *sql.DB) *Service {
	logProcessOwner.Do(func() {
		hostname, _ := os.Hostname()
		slog.Info("device-write process owner", "owner_instance_id", processInstanceID, "hostname", hostname, "pid", os.Getpid())
	})
	return &Service{conn: conn, queries: db.NewFailoverResettingQuerier(db.NewRetryDB(conn))}
}

type Attempt struct {
	service         *Service
	id              uuid.UUID
	deviceID        int64
	organizationID  int64
	epoch           int64
	dispatched      atomic.Bool
	deletionBinding string
}

// BeginQueuedHost checks the persisted message as well as its current epoch.
// The device lock precedes the queue lock, matching reservation/enqueue order.
func (s *Service) BeginQueuedHost(ctx context.Context, message queue.Message) (*Attempt, error) {
	return db.WithTransactionTimeout(ctx, s.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) (*Attempt, error) {
		keys, err := lockIdentity(ctx, q, message.DeviceID)
		if err != nil {
			return nil, err
		}
		fence, err := lockFence(ctx, q, message.OrgID, message.DeviceID)
		if err != nil {
			return nil, err
		}
		if fence.State != "host" || fence.Epoch != message.WriteEpoch {
			return nil, blocked()
		}
		persisted, err := q.LockQueuedDeviceWrite(ctx, message.ID)
		if err != nil {
			return nil, lookupError(err)
		}
		if persisted.DeviceID != message.DeviceID || persisted.OrgID != message.OrgID || persisted.WriteEpoch != fence.Epoch || persisted.CommandType != message.CommandType.String() || persisted.CommandBatchLogUuid != message.BatchLogUUID {
			return nil, blocked()
		}
		attempt := &Attempt{service: s, id: uuid.New(), deviceID: message.DeviceID, organizationID: message.OrgID, epoch: fence.Epoch}
		if err := q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{AttemptID: attempt.id, DeviceID: message.DeviceID, OrganizationID: message.OrgID, Epoch: fence.Epoch, QueueMessageID: sql.NullInt64{Int64: message.ID, Valid: true}, IdentityKeys: keys, OwnerInstanceID: processInstanceID, OperationKind: operationMutation}); err != nil {
			return nil, fmt.Errorf("record device write attempt: %w", err)
		}
		return attempt, nil
	})
}

func (a *Attempt) Recheck(ctx context.Context) error {
	valid, err := a.service.queries.CheckDeviceWriteAttempt(ctx, a.id)
	if err != nil {
		return fmt.Errorf("check device write attempt: %w", err)
	}
	if !valid {
		return blocked()
	}
	return nil
}

// Accepted records command acknowledgement, not physical completion. Legacy
// adapters return only an error, so a successful command needs this record even
// when its adapter does not participate in Dispatch.
func (a *Attempt) Accepted() {
	a.dispatched.Store(true)
}

// Finish completes only admissions that never dispatched a mutation. An adapter
// returning nil acknowledges its request; it does not prove device-side work has
// ended. Keep dispatched attempts unresolved until audited recovery supplies
// completion evidence. Duplicate completion cannot erase that evidence.
func (a *Attempt) Finish(ctx context.Context, outcome error) error {
	return db.WithTransactionTimeoutNoResult(ctx, a.service.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) error {
		if _, err := q.LockDeviceWriteFenceRecord(ctx, sqlc.LockDeviceWriteFenceRecordParams{DeviceID: a.deviceID, OrganizationID: a.organizationID}); err != nil {
			return err
		}
		status := "complete"
		if a.dispatched.Load() {
			status = "unknown"
		}
		changed, err := q.FinishDeviceWriteAttempt(ctx, sqlc.FinishDeviceWriteAttemptParams{Status: status, AttemptID: a.id})
		if err != nil {
			return fmt.Errorf("complete device write attempt: %w", err)
		}
		if changed != 1 {
			return blocked()
		}
		// An accepted command keeps ordinary host operation available. Only a
		// failed physical call quarantines custody; both outcomes block transfer.
		if status == "unknown" && outcome != nil {
			return q.QuarantineDeviceWriteFence(ctx, sqlc.QuarantineDeviceWriteFenceParams{DeviceID: a.deviceID, OrganizationID: a.organizationID})
		}
		return nil
	})
}

// Reserve blocks new old-epoch writes before waiting for dispatched calls.
// Its caller owns account authority and incoming configuration selection.
func (s *Service) Reserve(ctx context.Context, organizationID, deviceID int64) (int64, error) {
	reserve := func(q sqlc.Querier) (int64, error) {
		if _, err := lockIdentity(ctx, q, deviceID); err != nil {
			return 0, err
		}
		if _, err := lockFence(ctx, q, organizationID, deviceID); err != nil {
			return 0, err
		}
		epoch, err := q.ReserveDeviceWriteFence(ctx, sqlc.ReserveDeviceWriteFenceParams{DeviceID: deviceID, OrganizationID: organizationID})
		if err != nil {
			return 0, lookupError(err)
		}
		// Cancellation also records terminal device results in this transaction.
		if err := q.CancelPendingDeviceWrites(ctx, deviceID); err != nil {
			return 0, fmt.Errorf("cancel superseded device commands: %w", err)
		}
		return epoch, nil
	}
	if q := db.GetTxQueries(ctx); q != nil {
		return reserve(q)
	}
	return db.WithTransactionTimeout(ctx, s.conn, runtimepolicy.CommandTransactionBound, reserve)
}

func (s *Service) Drained(ctx context.Context, organizationID, deviceID, epoch int64) (bool, error) {
	q := db.GetTxQueries(ctx)
	if q == nil {
		q = s.queries
	}
	drained, err := q.IsDeviceWriteFenceDrained(ctx, sqlc.IsDeviceWriteFenceDrainedParams{DeviceID: deviceID, OrganizationID: organizationID, Epoch: epoch})
	if err != nil {
		return false, fmt.Errorf("check physical write drain: %w", err)
	}
	return drained, nil
}

// ReleaseHost does not reset the epoch. The allocation controller must verify
// the host configuration (or reliable stopped state) before calling this.
func (s *Service) ReleaseHost(ctx context.Context, organizationID, deviceID, epoch int64) error {
	release := func(q sqlc.Querier) error {
		if _, err := lockFence(ctx, q, organizationID, deviceID); err != nil {
			return err
		}
		count, err := q.ReleaseDeviceWriteFenceToHost(ctx, sqlc.ReleaseDeviceWriteFenceToHostParams{DeviceID: deviceID, OrganizationID: organizationID, Epoch: epoch})
		if err != nil {
			return fmt.Errorf("release device write fence: %w", err)
		}
		if count != 1 {
			return blocked()
		}
		return nil
	}
	if q := db.GetTxQueries(ctx); q != nil {
		return release(q)
	}
	return db.WithTransactionTimeoutNoResult(ctx, s.conn, runtimepolicy.CommandTransactionBound, release)
}

func lockFence(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64) (sqlc.DeviceWriteFence, error) {
	if err := q.EnsureDeviceWriteFences(ctx, []int64{deviceID}); err != nil {
		return sqlc.DeviceWriteFence{}, fmt.Errorf("ensure device write fence: %w", err)
	}
	rows, err := q.LockDeviceWriteFences(ctx, []int64{deviceID})
	if err != nil {
		return sqlc.DeviceWriteFence{}, fmt.Errorf("lock device write fence: %w", err)
	}
	if len(rows) != 1 || rows[0].OrganizationID != organizationID {
		return sqlc.DeviceWriteFence{}, blocked()
	}
	return rows[0], nil
}

func blocked() error {
	return fleeterror.NewFailedPreconditionError("device write is fenced by configuration ownership or an unresolved operation")
}
func lookupError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return blocked()
	}
	return fmt.Errorf("read device write state: %w", err)
}

// Context carries an unforgeable attempt for follow-on dispatch checks.
// Only a successfully persisted attempt can produce this context.
func (a *Attempt) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, attemptContextKey{}, a)
}

type attemptContextKey struct{}

func RecheckContext(ctx context.Context) error {
	attempt, ok := ctx.Value(attemptContextKey{}).(*Attempt)
	if !ok {
		return nil
	}
	return attempt.Recheck(ctx)
}

// Dispatch checks ownership immediately before invoking a device operation and
// records whether an error may represent an ambiguous physical result.
func Dispatch(ctx context.Context, operation func(context.Context) error) error {
	attempt, ok := ctx.Value(attemptContextKey{}).(*Attempt)
	if ok {
		if err := attempt.Recheck(ctx); err != nil {
			return err
		}
	}
	outcome := operation(ctx)
	// The durable attempt remains running during the operation, including if
	// the process crashes. A confirmed admission rejection cannot erase a
	// previous invocation that may already have reached the device.
	if ok && !IsNotDispatched(outcome) {
		attempt.dispatched.Store(true)
	}
	return outcome
}

// BeginHost commits an attempt before direct device I/O. The captured epoch
// prevents a cached handle from reviving after ownership returns to the host.
func (s *Service) BeginHost(ctx context.Context, organizationID, deviceID, epoch int64) (*Attempt, error) {
	if db.GetTxQueries(ctx) != nil {
		return nil, fleeterror.NewFailedPreconditionError("device I/O requires committed admission")
	}
	return db.WithTransactionTimeout(ctx, s.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) (*Attempt, error) {
		keys, err := lockIdentity(ctx, q, deviceID)
		if err != nil {
			return nil, err
		}
		fence, err := lockFence(ctx, q, organizationID, deviceID)
		if err != nil {
			return nil, err
		}
		if fence.State != "host" || fence.Epoch != epoch {
			return nil, blocked()
		}
		attempt := &Attempt{service: s, id: uuid.New(), deviceID: deviceID, organizationID: organizationID, epoch: epoch}
		if err := q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{AttemptID: attempt.id, DeviceID: deviceID, OrganizationID: organizationID, Epoch: epoch, IdentityKeys: keys, OwnerInstanceID: processInstanceID, OperationKind: operationMutation}); err != nil {
			return nil, fmt.Errorf("record direct device write: %w", err)
		}
		return attempt, nil
	})
}

// WithHostAttempt retains the whole operation lifetime after committed admission.
// The callback marks only actual transport calls with Dispatch after validation.
// A carried attempt may authorize only its exact database, device and epoch.
func (s *Service) WithHostAttempt(ctx context.Context, organizationID, deviceID, epoch int64, call func(context.Context) error) (resultErr error) {
	if db.GetTxQueries(ctx) != nil {
		return fleeterror.NewFailedPreconditionError("device I/O requires committed admission")
	}
	if attempt, ok := ctx.Value(attemptContextKey{}).(*Attempt); ok {
		if attempt.deletionBinding != "" || attempt.service.conn != s.conn || attempt.organizationID != organizationID || attempt.deviceID != deviceID || attempt.epoch != epoch {
			return blocked()
		}
		return call(ctx)
	}
	attempt, err := s.BeginHost(ctx, organizationID, deviceID, epoch)
	if err != nil {
		return err
	}
	defer func() {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimepolicy.CommandTransactionBound)
		defer cancel()
		if err := attempt.Finish(finishCtx, resultErr); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	return call(attempt.Context(ctx))
}

// RunHost admits a single direct physical call. Multi-step transports use
// WithHostAttempt and mark each actual dispatch after their own validation.
func (s *Service) RunHost(ctx context.Context, organizationID, deviceID, epoch int64, call func(context.Context) error) error {
	return s.WithHostAttempt(ctx, organizationID, deviceID, epoch, func(ctx context.Context) error { return Dispatch(ctx, call) })
}
