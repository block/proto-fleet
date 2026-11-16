// Package devicewrite retains command epochs and actual dispatched attempts.
// Queue completion, retries, and timeouts are not proof of physical drain.
package devicewrite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
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
	service            *Service
	id                 uuid.UUID
	deviceID           int64
	organizationID     int64
	epoch              int64
	dispatched         atomic.Bool // Invocations without explicit terminal evidence.
	ordered            atomic.Bool
	deletionBinding    string
	observation        bool
	curtailmentCommand string
	writeEvidence      atomic.Pointer[errdetails.ErrorInfo]
}

// BeginQueuedHost checks persisted host work. Curtailment control is independent
// of configuration ownership; other commands must retain their captured epoch.
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
		curtailmentCommand := ""
		if message.CommandType.IsCurtailmentControl() {
			curtailmentCommand = message.CommandType.String()
		}
		if curtailmentCommand == "" && (fence.State != "host" || fence.Epoch != message.WriteEpoch) {
			return nil, blocked()
		}
		if MayAffectPools(operationMutation, message.CommandType.String()) {
			if err := requirePoolRemovalAdmission(ctx, q, message.OrgID, message.DeviceID, message.CommandType); err != nil {
				return nil, err
			}
		}
		persisted, err := q.LockQueuedDeviceWrite(ctx, message.ID)
		if err != nil {
			return nil, lookupError(err)
		}
		if persisted.DeviceID != message.DeviceID || persisted.OrgID != message.OrgID || persisted.WriteEpoch != message.WriteEpoch || persisted.CommandType != message.CommandType.String() || persisted.CommandBatchLogUuid != message.BatchLogUUID {
			return nil, blocked()
		}
		attempt := &Attempt{service: s, id: uuid.New(), deviceID: message.DeviceID, organizationID: message.OrgID, epoch: fence.Epoch, curtailmentCommand: curtailmentCommand}
		attempt.ordered.Store(isOrderedWrite(message.CommandType))
		if err := q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{AttemptID: attempt.id, DeviceID: message.DeviceID, OrganizationID: message.OrgID, Epoch: fence.Epoch, QueueMessageID: sql.NullInt64{Int64: message.ID, Valid: true}, IdentityKeys: keys, OwnerInstanceID: processInstanceID, OperationKind: operationMutation, CommandType: message.CommandType.String(), CurtailmentCommand: sql.NullString{String: curtailmentCommand, Valid: curtailmentCommand != ""}}); err != nil {
			return nil, fmt.Errorf("record device write attempt: %w", err)
		}
		return attempt, nil
	})
}

func (a *Attempt) Recheck(ctx context.Context) error {
	// Recheck on the connection already holding dispatch locks. Taking a second
	// pool slot here could deadlock a fleet batch filling the pool.
	valid, err := a.service.bindingQueries(ctx).CheckDeviceWriteAttempt(ctx, a.id)
	if err != nil {
		return fmt.Errorf("check device write attempt: %w", err)
	}
	if !valid {
		return fleeterror.NewFailedPreconditionErrorf("device write admission changed; resolve fresh state before dispatch: %w", ErrStaleBinding)
	}
	if a.curtailmentCommand != "" {
		kind, err := commandtype.FromString(a.curtailmentCommand)
		if err != nil {
			return err
		}
		return requirePoolRemovalAdmission(ctx, a.service.bindingQueries(ctx), a.organizationID, a.deviceID, kind)
	}
	return nil
}

// Accepted records a successful request even when an adapter does not use Dispatch.
// The command's completion policy determines whether acknowledgement proves drain.
func (a *Attempt) Accepted() {
	a.dispatched.Store(true)
}

// Finish retires ordered configuration calls once their adapter returns, whether
// the response succeeded or was lost. Firmware processes these writes in order;
// handoff must apply and verify the incoming configuration before activation.
// Asynchronous operations and unresolved node delivery remain unknown. A panic
// or bookkeeping failure leaves the separately committed attempt running.
func (a *Attempt) Finish(ctx context.Context, outcome error) error {
	classification := classifyOutcome(outcome)
	return db.WithTransactionTimeoutNoResult(ctx, a.service.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) error {
		if _, err := q.LockDeviceWriteFenceRecord(ctx, sqlc.LockDeviceWriteFenceRecordParams{DeviceID: a.deviceID, OrganizationID: a.organizationID}); err != nil {
			return err
		}
		status := statusComplete
		if a.dispatched.Load() && (!a.ordered.Load() || classification.kind == sdk.WriteUnknown) {
			status = "unknown"
		}
		evidence := a.writeEvidence.Load()
		if evidence == nil {
			evidence = classification.evidence
		}
		if status == "unknown" {
			if evidence.GetReason() != sdk.WriteUnknown {
				evidence = sdk.NewWriteEvidence(sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonUnknown)
			}
		} else if a.dispatched.Load() {
			// Ordering can retire calls without an SDK receipt. A later call's
			// terminal evidence cannot certify those earlier invocations.
			evidence = nil
		}
		diagnostic := []byte("{}")
		if evidence != nil {
			// The SDK accepts only fixed identifiers. Never persist error text,
			// credentials, firmware replies, or request bodies here.
			diagnostic, _ = json.Marshal(map[string]string{"outcome": evidence.Reason, "stage": evidence.Metadata["stage"], "reason": evidence.Metadata["reason"]})
		}
		changed, err := q.FinishDeviceWriteAttempt(ctx, sqlc.FinishDeviceWriteAttemptParams{Status: status, AttemptID: a.id, WriteOutcome: diagnostic})
		if err != nil {
			return fmt.Errorf("complete device write attempt: %w", err)
		}
		if changed != 1 {
			return blocked()
		}
		// Unknown attempts block transfer. Accepted ones preserve ordinary host
		// operation; a failed unknown call also quarantines custody.
		if status == "unknown" && outcome != nil {
			return q.QuarantineDeviceWriteFence(ctx, sqlc.QuarantineDeviceWriteFenceParams{DeviceID: a.deviceID, OrganizationID: a.organizationID})
		}
		if !a.dispatched.Load() {
			// The owning callback and its response lifetime have ended. This is
			// transient direct admission, not durable physical-outcome history.
			// Queue and claimed deletion-permit rows remain retained by the query.
			if err := q.DeleteUndispatchedDirectAttempt(ctx, sqlc.DeleteUndispatchedDirectAttemptParams{AttemptID: a.id, DeviceID: a.deviceID, OrganizationID: a.organizationID}); err != nil {
				return fmt.Errorf("retire undispatched direct admission: %w", err)
			}
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
		if err := requireNoActivePoolRemoval(ctx, q, organizationID, deviceID); err != nil {
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

// Dispatch checks ownership immediately before a mutation. The enclosing
// attempt classifies whether a returned call can establish completion.
func Dispatch(ctx context.Context, operation func(context.Context) error) error {
	return dispatch(ctx, operation, true)
}

// Observe retains the same durable ownership check as Dispatch without marking
// a physical mutation. It never clears evidence of an earlier mutation in the
// same attempt; callers must retain admission through the response lifetime.
func Observe(ctx context.Context, operation func(context.Context) error) error {
	return dispatch(ctx, operation, false)
}

func dispatch(ctx context.Context, operation func(context.Context) error, mutation bool) error {
	attempt, ok := ctx.Value(attemptContextKey{}).(*Attempt)
	if ok {
		if mutation && attempt.observation {
			return blocked()
		}
		if err := attempt.Recheck(ctx); err != nil {
			return err
		}
	}
	outcome := operation(ctx)
	if sdk.IsBackendGenerationRejected(outcome) {
		outcome = NotDispatched(outcome)
	}
	if !ok || !mutation {
		return outcome
	}
	// The durable attempt remains running during the operation, including if
	// the process crashes. A confirmed admission rejection cannot erase a
	// previous invocation that may already have reached the device.
	classification := classifyOutcome(outcome)
	if classification.evidence != nil {
		attempt.recordWriteEvidence(classification.evidence)
	}
	// A finished invocation adds no outstanding work. It must never clear an
	// earlier invocation that lacks completion evidence in the same attempt.
	if classification.kind == sdk.WriteNotDispatched || classification.kind == sdk.WriteCompleted {
		return outcome
	}
	attempt.dispatched.Store(true)
	// Preserve uncertainty even if an outer handler sanitizes the error.
	if classification.kind == sdk.WriteUnknown {
		attempt.ordered.Store(false)
	}
	return outcome
}

func (a *Attempt) recordWriteEvidence(evidence *errdetails.ErrorInfo) {
	for {
		previous := a.writeEvidence.Load()
		// Unknown work stays unresolved. A refusal cannot erase a completed
		// mutation when every invocation has authoritative terminal evidence.
		if previous.GetReason() == sdk.WriteUnknown || (previous.GetReason() == sdk.WriteCompleted && evidence.Reason == sdk.WriteNotDispatched) {
			return
		}
		if a.writeEvidence.CompareAndSwap(previous, evidence) {
			return
		}
	}
}

// BeginHost commits an attempt before direct device I/O. The captured epoch
// prevents a cached handle from reviving after ownership returns to the host.
func (s *Service) BeginHost(ctx context.Context, organizationID, deviceID, epoch int64) (*Attempt, error) {
	return s.beginHost(ctx, organizationID, deviceID, epoch, commandtype.Type(-1), false, "", nil)
}

func (s *Service) beginHost(ctx context.Context, organizationID, deviceID, epoch int64, commandKind commandtype.Type, observation bool, curtailmentCommand string, record func(sqlc.Querier, uuid.UUID) error) (*Attempt, error) {
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
		if curtailmentCommand == "" && (fence.State != "host" || fence.Epoch != epoch) {
			return nil, blocked()
		}
		if curtailmentCommand != "" {
			epoch = fence.Epoch
		}
		kind := operationMutation
		if observation {
			kind = operationObservation
		}
		if !observation && MayAffectPools(operationMutation, commandKind.String()) {
			if err := requirePoolRemovalAdmission(ctx, q, organizationID, deviceID, commandKind); err != nil {
				return nil, err
			}
		}
		attempt := &Attempt{service: s, id: uuid.New(), deviceID: deviceID, organizationID: organizationID, epoch: epoch, observation: observation, curtailmentCommand: curtailmentCommand}
		if err := q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{AttemptID: attempt.id, DeviceID: deviceID, OrganizationID: organizationID, Epoch: epoch, IdentityKeys: keys, OwnerInstanceID: processInstanceID, OperationKind: kind, CommandType: commandKind.String(), CurtailmentCommand: sql.NullString{String: curtailmentCommand, Valid: curtailmentCommand != ""}}); err != nil {
			return nil, fmt.Errorf("record direct device write: %w", err)
		}
		if record != nil {
			if err := record(q, attempt.id); err != nil {
				return nil, err
			}
		}
		return attempt, nil
	})
}

// WithHostAttempt retains the whole operation lifetime after committed admission.
// The callback uses Dispatch for mutations and Observe for reads after validation.
// A carried attempt may authorize only its exact database, device and epoch.
func (s *Service) WithHostAttempt(ctx context.Context, organizationID, deviceID, epoch int64, kind commandtype.Type, call func(context.Context) error) (resultErr error) {
	return s.withHostAttempt(ctx, organizationID, deviceID, epoch, kind, false, "", nil, call)
}

// WithRecordedHostAttempt commits a caller's work claim and its physical
// admission atomically. The record callback is database-only and may be retried;
// call performs I/O only after both records commit. It cannot nest in an existing
// attempt, which would lack a new durable admission for this claim.
func (s *Service) WithRecordedHostAttempt(ctx context.Context, organizationID, deviceID, epoch int64, kind commandtype.Type, record func(sqlc.Querier, uuid.UUID) error, call func(context.Context) error) error {
	if record == nil {
		return blocked()
	}
	return s.withHostAttempt(ctx, organizationID, deviceID, epoch, kind, false, "", record, call)
}

// WithHostObservation durably declares a read before I/O. Its context cannot
// dispatch mutations, so interrupted reads require only owner termination evidence.
func (s *Service) WithHostObservation(ctx context.Context, organizationID, deviceID, epoch int64, call func(context.Context) error) error {
	return s.withHostAttempt(ctx, organizationID, deviceID, epoch, commandtype.Type(-1), true, "", nil, call)
}

// WithCurtailmentAttempt retains host fleet authority for exactly one curtailment
// command; it cannot authorize configuration writes or another control command.
func (s *Service) WithCurtailmentAttempt(ctx context.Context, organizationID, deviceID, epoch int64, kind commandtype.Type, call func(context.Context) error) error {
	if !kind.IsCurtailmentControl() {
		return blocked()
	}
	return s.withHostAttempt(ctx, organizationID, deviceID, epoch, kind, false, kind.String(), nil, call)
}

func (s *Service) withHostAttempt(ctx context.Context, organizationID, deviceID, epoch int64, kind commandtype.Type, observation bool, curtailmentCommand string, record func(sqlc.Querier, uuid.UUID) error, call func(context.Context) error) (resultErr error) {
	if db.GetTxQueries(ctx) != nil {
		return fleeterror.NewFailedPreconditionError("device I/O requires committed admission")
	}
	if attempt, ok := ctx.Value(attemptContextKey{}).(*Attempt); ok {
		if record != nil {
			return blocked()
		}
		if attempt.deletionBinding != "" || attempt.service.conn != s.conn || attempt.organizationID != organizationID || attempt.deviceID != deviceID || attempt.curtailmentCommand != curtailmentCommand || (curtailmentCommand == "" && attempt.epoch != epoch) {
			return blocked()
		}
		// An asynchronous step cannot inherit a synchronous outer command's policy.
		// An unclassified RunHost callback preserves its enclosing attempt's policy.
		if kind != commandtype.Type(-1) && !isOrderedWrite(kind) {
			attempt.ordered.Store(false)
		}
		return call(ctx)
	}
	attempt, err := s.beginHost(ctx, organizationID, deviceID, epoch, kind, observation, curtailmentCommand, record)
	if err != nil {
		return err
	}
	attempt.ordered.Store(isOrderedWrite(kind))
	returned := false
	defer func() {
		if !returned {
			// A recovered panic cannot establish the physical operation's outcome.
			return
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimepolicy.CommandTransactionBound)
		defer cancel()
		if err := attempt.Finish(finishCtx, resultErr); err != nil {
			// Keep the transport outcome: callers must not replay an accepted
			// command because completion bookkeeping failed. The durable
			// unresolved attempt continues to block ownership transfer.
			slog.Error("failed to persist direct device write completion",
				"attempt_id", attempt.id, "device_id", deviceID, "error", err)
			// Read-only admissions can report cleanup failure without replaying
			// a physical mutation. Their retained owner still needs recovery.
			if observation && resultErr == nil {
				resultErr = err
			}
		}
	}()
	resultErr = call(attempt.Context(ctx))
	returned = true
	return resultErr
}

// RunHost admits a single direct physical call. Multi-step transports use
// WithHostAttempt and mark each actual dispatch after their own validation.
func (s *Service) RunHost(ctx context.Context, organizationID, deviceID, epoch int64, call func(context.Context) error) error {
	return s.WithHostAttempt(ctx, organizationID, deviceID, epoch, commandtype.Type(-1), func(ctx context.Context) error { return Dispatch(ctx, call) })
}

// Only the known synchronous configuration/control writes use firmware ordering.
// New command types default to unresolved until their semantics are classified.
func isOrderedWrite(kind commandtype.Type) bool {
	switch kind {
	case commandtype.StartMining, commandtype.StopMining, commandtype.SetCoolingMode,
		commandtype.SetPowerTarget, commandtype.UpdateMiningPools, commandtype.UpdateMinerPassword,
		commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig,
		commandtype.SetContainerControl, commandtype.ContainerAlarmAction, commandtype.SetTankMining, commandtype.SetModuleMining, commandtype.ContainerMining:
		return true
	case commandtype.FrigateServerUpdate:
		// Each Frigate update call is synchronous and idempotent under its job
		// ID and idempotency key, and Fleet reconciles every job from Frigate's
		// own rollout status. A returned call therefore leaves no unresolved
		// physical write, even when its reply was lost. The rollout itself
		// outlives these calls; PrepareDeletion keeps the container from being
		// retired while its job is blocking.
		return true
	case commandtype.DownloadLogs, commandtype.Reboot, commandtype.BlinkLED, commandtype.SetBoardIdentify,
		commandtype.FirmwareUpdate, commandtype.Unpair:
		return false
	default:
		return false
	}
}
