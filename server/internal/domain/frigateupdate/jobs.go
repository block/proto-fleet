package frigateupdate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	defaultListPageSize = 20
	maxListPageSize     = 100
	maxIdempotencyKey   = 128
	// maxActionsPerJob bounds the per-key action records kept for one job.
	maxActionsPerJob = 64
)

// Job is one update of one container.
type Job struct {
	Row              sqlc.FrigateUpdateJob
	DeviceIdentifier string
	Package          Package
	Instances        []sqlc.FrigateUpdateInstance
}

// StartInput describes a StartUpdate request.
type StartInput struct {
	DeviceIdentifier string
	PackageID        uuid.UUID
	StageOnly        bool
	IdempotencyKey   string
}

func validIdempotencyKey(key string) bool {
	if key == "" || len(key) > maxIdempotencyKey {
		return false
	}
	for _, r := range key {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func isBlocking(j sqlc.FrigateUpdateJob) bool {
	switch j.State {
	case StateStaging, StateStaged, StateStarting, StateRunning:
		return true
	case StateFailed:
		return j.Retryable
	default:
		return false
	}
}

func isTerminal(j sqlc.FrigateUpdateJob) bool { return !isBlocking(j) }

// CancelPending reports whether an operator cancel was accepted but not yet
// carried out: the job is still starting and the runner will finish the cancel
// once any in-flight Start attempt has resolved.
func CancelPending(j sqlc.FrigateUpdateJob) bool {
	return j.State == StateStarting && j.CancelRequestedAt.Valid
}

// StartUpdate records an update job. The runner stages the package on every
// controller and starts the rollout. A repeat with the same idempotency key
// returns the existing job.
func (s *Service) StartUpdate(ctx context.Context, in StartInput) (Job, error) {
	info, err := principal(ctx)
	if err != nil {
		return Job{}, err
	}
	if !validIdempotencyKey(in.IdempotencyKey) {
		return Job{}, fleeterror.NewInvalidArgumentError("idempotency key must be 1 to 128 printable ASCII characters")
	}
	target, err := s.target(ctx, info.OrganizationID, in.DeviceIdentifier)
	if err != nil {
		return Job{}, err
	}
	if job, found, err := s.replayStart(ctx, info.OrganizationID, target.ID, in); err != nil || found {
		return job, err
	}

	pkgRow, err := s.queries.GetFrigateUpdatePackage(ctx, sqlc.GetFrigateUpdatePackageParams{OrgID: info.OrganizationID, ID: in.PackageID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, fleeterror.NewNotFoundError("package not found")
		}
		return Job{}, err
	}
	pkg := packageFromRow(pkgRow)
	if pkg.Deleted {
		return Job{}, fleeterror.NewNotFoundError("package not found")
	}
	if in.StageOnly && pkg.Kind.rules().commitInstalls {
		return Job{}, fleeterror.NewInvalidArgumentErrorf("stage_only is not available for a %s package: each controller installs its copy as soon as the copy is committed", pkg.Kind.rules().noun)
	}
	if !s.store.Exists(pkg.ID, pkg.SizeBytes) {
		return Job{}, fleeterror.NewFailedPreconditionErrorf("%s: the package is not stored on this Fleet host; upload it again", CodePackageMissing)
	}

	support, err := s.readSupport(ctx, target.ID, &pkg)
	if err != nil {
		return Job{}, err
	}
	if !support.Eligible {
		return Job{}, fleeterror.NewFailedPreconditionErrorf("%s: %s", support.IneligibleCode, support.IneligibleReason)
	}
	expected := instanceIDs(support.Instances)

	replayed := false
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.FrigateUpdateJob, error) {
		// Match allocation and pool readiness: organization, device, then fence.
		if _, err := q.LockPoolEquipmentOrganization(ctx, info.OrganizationID); err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		locked, err := q.LockFrigateUpdateTarget(ctx, sqlc.LockFrigateUpdateTargetParams{DeviceIdentifier: in.DeviceIdentifier, OrgID: info.OrganizationID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sqlc.FrigateUpdateJob{}, fleeterror.NewNotFoundError("container not found")
			}
			return sqlc.FrigateUpdateJob{}, err
		}
		if err := q.EnsureDeviceWriteFences(ctx, []int64{locked.ID}); err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		if _, err := q.LockDeviceWriteFences(ctx, []int64{locked.ID}); err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		// Re-authorize against the locked row: the container may have moved sites.
		if err := s.auth.AuthorizeSite(ctx, siteArg(locked.SiteID)); err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		// The locked row is authoritative: a re-upload may have replaced the
		// statement-derived fields since the package was read above.
		lockedPkg, err := q.LockFrigateUpdatePackage(ctx, sqlc.LockFrigateUpdatePackageParams{OrgID: info.OrganizationID, ID: in.PackageID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sqlc.FrigateUpdateJob{}, fleeterror.NewNotFoundError("package not found")
			}
			return sqlc.FrigateUpdateJob{}, err
		}
		// A concurrent request with the same key may have committed while this
		// one waited for the target lock; replay it rather than reporting the
		// job it created as a conflicting update.
		if existing, err := q.GetFrigateUpdateJobByRequestKey(ctx, sqlc.GetFrigateUpdateJobByRequestKeyParams{OrgID: info.OrganizationID, RequestKey: in.IdempotencyKey}); err == nil {
			if !sameStartRequest(existing, locked.ID, in) {
				return sqlc.FrigateUpdateJob{}, errIdempotencyKeyReused()
			}
			replayed = true
			return existing, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return sqlc.FrigateUpdateJob{}, err
		}
		if err := devicewrite.RequireNoMiningGroupDeactivation(ctx, q, info.OrganizationID, locked.ID); err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		if blocking, err := q.GetBlockingFrigateUpdateJobForDevice(ctx, locked.ID); err == nil {
			return sqlc.FrigateUpdateJob{}, fleeterror.NewFailedPreconditionErrorf("an update (%s, %s) is already in progress for this container", blocking.ID, blocking.State)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return sqlc.FrigateUpdateJob{}, err
		}
		job, err := q.InsertFrigateUpdateJob(ctx, sqlc.InsertFrigateUpdateJobParams{
			ID: uuid.New(), OrgID: info.OrganizationID, DeviceID: locked.ID, PackageID: in.PackageID, StageOnly: in.StageOnly,
			TargetVersion: lockedPkg.Version, ExpectedInstanceIds: expected, RequestKey: in.IdempotencyKey,
			ActorUserID: info.UserID, ActorExternalID: info.ExternalUserID, ActorUsername: info.Username,
		})
		if err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		for _, id := range expected {
			if err := q.InsertFrigateUpdateInstance(ctx, sqlc.InsertFrigateUpdateInstanceParams{JobID: job.ID, InstanceID: id, SizeBytes: lockedPkg.SizeBytes}); err != nil {
				return sqlc.FrigateUpdateJob{}, err
			}
		}
		return job, nil
	})
	if err != nil {
		if db.IsUniqueViolationError(err) {
			// A concurrent request created a job first: either the same operator
			// action (replay it) or another update for this container.
			if job, found, replayErr := s.replayStart(ctx, info.OrganizationID, target.ID, in); replayErr != nil || found {
				return job, replayErr
			}
			return Job{}, fleeterror.NewFailedPreconditionError("an update is already in progress for this container")
		}
		return Job{}, err
	}
	if replayed {
		return s.loadJob(ctx, row)
	}
	s.logJobEvent(ctx, row, pkg.Kind, "frigate_update_started",
		fmt.Sprintf("Started %s update to %s", pkg.Kind.rules().noun, row.TargetVersion), activitymodels.ResultSuccess, "")
	return s.loadJob(ctx, row)
}

// replayStart returns the job a repeated idempotency key already created.
func (s *Service) replayStart(ctx context.Context, orgID, deviceID int64, in StartInput) (Job, bool, error) {
	existing, err := s.queries.GetFrigateUpdateJobByRequestKey(ctx, sqlc.GetFrigateUpdateJobByRequestKeyParams{OrgID: orgID, RequestKey: in.IdempotencyKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, false, nil
		}
		return Job{}, false, err
	}
	if !sameStartRequest(existing, deviceID, in) {
		return Job{}, false, errIdempotencyKeyReused()
	}
	job, err := s.loadJob(ctx, existing)
	return job, true, err
}

// sameStartRequest reports whether a job created under a request key is the
// one this request asks for.
func sameStartRequest(existing sqlc.FrigateUpdateJob, deviceID int64, in StartInput) bool {
	return existing.DeviceID == deviceID && existing.PackageID == in.PackageID && existing.StageOnly == in.StageOnly
}

func errIdempotencyKeyReused() error {
	return fleeterror.NewInvalidArgumentError("the idempotency key was already used for a different update request")
}

// GetUpdate returns one job.
func (s *Service) GetUpdate(ctx context.Context, id uuid.UUID) (Job, error) {
	row, err := s.authorizedJob(ctx, id)
	if err != nil {
		return Job{}, err
	}
	return s.loadJob(ctx, row)
}

// authorizedJob loads a job in the caller's organization and checks the caller
// may manage its container.
func (s *Service) authorizedJob(ctx context.Context, id uuid.UUID) (sqlc.FrigateUpdateJob, error) {
	info, err := principal(ctx)
	if err != nil {
		return sqlc.FrigateUpdateJob{}, err
	}
	row, err := s.queries.GetFrigateUpdateJob(ctx, sqlc.GetFrigateUpdateJobParams{OrgID: info.OrganizationID, ID: id})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.FrigateUpdateJob{}, fleeterror.NewNotFoundError("update not found")
		}
		return sqlc.FrigateUpdateJob{}, err
	}
	device, err := s.queries.GetFrigateUpdateDeviceSite(ctx, row.DeviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.FrigateUpdateJob{}, fleeterror.NewNotFoundError("update not found")
		}
		return sqlc.FrigateUpdateJob{}, err
	}
	if err := s.auth.AuthorizeSite(ctx, siteArg(device.SiteID)); err != nil {
		return sqlc.FrigateUpdateJob{}, err
	}
	return row, nil
}

// ListUpdates returns a container's jobs, newest first.
func (s *Service) ListUpdates(ctx context.Context, deviceIdentifier string, pageSize int32) ([]Job, error) {
	info, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	target, err := s.target(ctx, info.OrganizationID, deviceIdentifier)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 {
		pageSize = defaultListPageSize
	}
	if pageSize > maxListPageSize {
		pageSize = maxListPageSize
	}
	rows, err := s.queries.ListFrigateUpdateJobsForDevice(ctx, sqlc.ListFrigateUpdateJobsForDeviceParams{OrgID: info.OrganizationID, DeviceID: target.ID, Limit: pageSize})
	if err != nil {
		return nil, err
	}
	return s.loadJobs(ctx, rows, target.DeviceIdentifier)
}

func (s *Service) loadJob(ctx context.Context, row sqlc.FrigateUpdateJob) (Job, error) {
	device, err := s.queries.GetFrigateUpdateDeviceSite(ctx, row.DeviceID)
	identifier := ""
	switch {
	case err == nil:
		identifier = device.DeviceIdentifier
	case !errors.Is(err, sql.ErrNoRows):
		return Job{}, err
	}
	jobs, err := s.loadJobs(ctx, []sqlc.FrigateUpdateJob{row}, identifier)
	if err != nil {
		return Job{}, err
	}
	return jobs[0], nil
}

func (s *Service) loadJobs(ctx context.Context, rows []sqlc.FrigateUpdateJob, identifier string) ([]Job, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	instances, err := s.queries.ListFrigateUpdateInstancesForJobs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byJob := make(map[uuid.UUID][]sqlc.FrigateUpdateInstance, len(rows))
	for _, in := range instances {
		byJob[in.JobID] = append(byJob[in.JobID], in)
	}
	packages := make(map[uuid.UUID]Package)
	out := make([]Job, 0, len(rows))
	for _, r := range rows {
		pkg, ok := packages[r.PackageID]
		if !ok {
			pr, err := s.queries.GetFrigateUpdatePackage(ctx, sqlc.GetFrigateUpdatePackageParams{OrgID: r.OrgID, ID: r.PackageID})
			if err != nil {
				return nil, err
			}
			pkg = packageFromRow(pr)
			packages[r.PackageID] = pkg
		}
		out = append(out, Job{Row: r, DeviceIdentifier: identifier, Package: pkg, Instances: byJob[r.ID]})
	}
	return out, nil
}

// RetryUpdate asks Frigate to resume a rollout that failed after the
// controllers were stopped.
func (s *Service) RetryUpdate(ctx context.Context, id uuid.UUID, key string) (Job, error) {
	return s.act(ctx, id, key, actionRetry)
}

// CancelUpdate abandons a job that has not stopped any controller.
func (s *Service) CancelUpdate(ctx context.Context, id uuid.UUID, key string) (Job, error) {
	return s.act(ctx, id, key, actionCancel)
}

type actionPlan struct {
	job     sqlc.FrigateUpdateJob
	counter int32
	// local is true when the action finished without involving Frigate.
	local bool
}

func (s *Service) act(ctx context.Context, id uuid.UUID, key, kind string) (Job, error) {
	info, err := principal(ctx)
	if err != nil {
		return Job{}, err
	}
	if !validIdempotencyKey(key) {
		return Job{}, fleeterror.NewInvalidArgumentError("idempotency key must be 1 to 128 printable ASCII characters")
	}
	row, err := s.authorizedJob(ctx, id)
	if err != nil {
		return Job{}, err
	}

	var before sqlc.FrigateUpdateJob
	plan, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (actionPlan, error) {
		cur, err := q.LockFrigateUpdateJob(ctx, row.ID)
		if err != nil {
			return actionPlan{}, err
		}
		before = cur
		// Re-authorize against the container's current site under the job lock.
		device, err := q.GetFrigateUpdateDeviceSite(ctx, cur.DeviceID)
		if errors.Is(err, sql.ErrNoRows) {
			return actionPlan{}, fleeterror.NewNotFoundError("update not found")
		}
		if err != nil {
			return actionPlan{}, err
		}
		if err := s.auth.AuthorizeSite(ctx, siteArg(device.SiteID)); err != nil {
			return actionPlan{}, err
		}
		// Every key the job has seen replays its own outcome, not only the latest.
		prior, err := q.GetFrigateUpdateAction(ctx, sqlc.GetFrigateUpdateActionParams{JobID: cur.ID, Kind: kind, RequestKey: key})
		switch {
		case err == nil:
			// A replay of a cancel that is still pending answers from the record
			// without another Frigate call: the runner owns the outcome now.
			if prior.Local || (kind == actionCancel && CancelPending(cur)) {
				return actionPlan{job: cur, local: true}, nil
			}
			return actionPlan{job: cur, counter: prior.Counter}, nil
		case !errors.Is(err, sql.ErrNoRows):
			return actionPlan{}, err
		}
		pkgKind, err := q.GetFrigateUpdateJobPackageKind(ctx, cur.ID)
		if err != nil {
			return actionPlan{}, err
		}
		if err := checkActionAllowed(cur, PackageKind(pkgKind), kind); err != nil {
			return actionPlan{}, err
		}
		recorded, err := q.CountFrigateUpdateActions(ctx, cur.ID)
		if err != nil {
			return actionPlan{}, err
		}
		if recorded >= maxActionsPerJob {
			return actionPlan{}, fleeterror.NewFailedPreconditionError("this update has had too many retry and cancel requests")
		}
		if kind == actionCancel && CancelPending(cur) {
			// Another operator key arrived while a cancel is pending. It joins that
			// cancel: record the key as finished locally and return the pending job.
			if err := q.InsertFrigateUpdateAction(ctx, sqlc.InsertFrigateUpdateActionParams{JobID: cur.ID, Kind: kind, RequestKey: key, Counter: 0}); err != nil {
				return actionPlan{}, err
			}
			if err := q.MarkFrigateUpdateActionLocal(ctx, sqlc.MarkFrigateUpdateActionLocalParams{JobID: cur.ID, Kind: kind, RequestKey: key}); err != nil {
				return actionPlan{}, err
			}
			return actionPlan{job: cur, local: true}, nil
		}
		p := paramsFromJob(cur)
		if kind == actionCancel && !cur.StartAttempted && (cur.State == StateStaging || cur.State == StateStaged) {
			// Nothing was sent to Frigate, so no controller can be running this
			// job. The runner stops staging when it sees the state change and
			// aborts the staged copies.
			p.State = StateCancelled
			p.Cancellable = false
			p.Retryable = false
			p.CleanupPending = true
			p.CompletedAt = nullTime(s.now())
			if err := q.UpdateFrigateUpdateJob(ctx, p); err != nil {
				return actionPlan{}, err
			}
			if err := q.InsertFrigateUpdateAction(ctx, sqlc.InsertFrigateUpdateActionParams{JobID: cur.ID, Kind: kind, RequestKey: key, Counter: 0}); err != nil {
				return actionPlan{}, err
			}
			if err := q.MarkFrigateUpdateActionLocal(ctx, sqlc.MarkFrigateUpdateActionLocalParams{JobID: cur.ID, Kind: kind, RequestKey: key}); err != nil {
				return actionPlan{}, err
			}
			after, err := q.LockFrigateUpdateJob(ctx, cur.ID)
			return actionPlan{job: after, local: true}, err
		}
		// Persist the operator action before calling Frigate so a retried
		// request replays the same Frigate idempotency key.
		p.ActionCounter++
		if err := q.UpdateFrigateUpdateJob(ctx, p); err != nil {
			return actionPlan{}, err
		}
		if err := q.InsertFrigateUpdateAction(ctx, sqlc.InsertFrigateUpdateActionParams{JobID: cur.ID, Kind: kind, RequestKey: key, Counter: p.ActionCounter}); err != nil {
			return actionPlan{}, err
		}
		cur.ActionCounter = p.ActionCounter
		return actionPlan{job: cur, counter: p.ActionCounter}, nil
	})
	if err != nil {
		return Job{}, err
	}
	if plan.local {
		s.logTransition(ctx, before, plan.job)
		if isTerminal(plan.job) {
			s.forget(plan.job.ID)
		}
		return s.loadJob(ctx, plan.job)
	}

	updater, err := s.updaterFor(ctx, plan.job.DeviceID)
	if err != nil {
		return Job{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	defer cancel()
	action := sdkAction(plan.job.ID, derivedKey(kind, plan.job.ID, plan.counter), actorName(info.Username, info.ExternalUserID, info.UserID))
	var rollout rolloutObservation
	if kind == actionRetry {
		r, err := updater.FrigateUpdateRetry(callCtx, action)
		if err != nil {
			return Job{}, callError(err)
		}
		rollout = rolloutObservation{Rollout: r}
	} else {
		r, err := updater.FrigateUpdateCancel(callCtx, action)
		if err != nil {
			if isNotFoundRefusal(err) && plan.job.State == StateStarting {
				return s.cancelUnstarted(ctx, updater, plan.job, key, actorName(info.Username, info.ExternalUserID, info.UserID))
			}
			return Job{}, callError(err)
		}
		rollout = rolloutObservation{Rollout: r}
	}
	applied, err := s.applyRollout(ctx, plan.job.ID, rollout)
	if err != nil {
		return Job{}, err
	}
	return s.loadJob(ctx, applied)
}

// cancelUnstarted completes an operator cancel locally for a job whose Start
// never reached Frigate: Frigate has no record of it, so there is nothing to
// cancel there. A fresh Status read confirms that, and the start lease fences
// the runner. A runner that holds the lease may still have a Start in flight,
// so the cancel is recorded as a durable intent instead of racing it: the runner
// takes no new lease, waits for the lease to expire, re-reads Frigate's status
// and finishes the cancel (see finishPendingCancel). The returned job shows the
// pending cancel through CancelPending.
func (s *Service) cancelUnstarted(ctx context.Context, updater interfaces.FrigateUpdater, job sqlc.FrigateUpdateJob, key, actor string) (Job, error) {
	statusCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	status, err := updater.FrigateUpdateStatus(statusCtx, job.ID.String())
	cancel()
	switch {
	case err == nil:
		// Start won the race and Frigate has the rollout now.
		if _, err := s.applyRollout(ctx, job.ID, rolloutObservation{Rollout: status.Rollout, ServedByMaintenance: status.ServedByMaintenance}); err != nil {
			return Job{}, err
		}
		return Job{}, fleeterror.NewFailedPreconditionError("The controller accepted the update while it was being cancelled; cancel it again")
	case !isNotFoundRefusal(err):
		return Job{}, callError(err)
	}

	type outcome struct {
		before, after sqlc.FrigateUpdateJob
		refusal       error
	}
	res, err := runTx(ctx, s, func(q sqlc.Querier) (outcome, error) {
		cur, err := q.LockFrigateUpdateJob(ctx, job.ID)
		if err != nil {
			return outcome{}, err
		}
		if cur.State != StateStarting {
			return outcome{before: cur, after: cur, refusal: fleeterror.NewFailedPreconditionError("the update changed state while it was being cancelled; check its status and try again")}, nil
		}
		if CancelPending(cur) {
			return outcome{before: cur, after: cur}, nil
		}
		if cur.StartLeaseUntil.Valid && cur.StartLeaseUntil.Time.After(s.now()) {
			// A Start may still be in flight. Record the intent; the runner
			// finishes the cancel once the lease has expired.
			p := paramsFromJob(cur)
			p.CancelRequestedAt = nullTime(s.now())
			p.CancelRequestKey = key
			p.CancelRequestActor = actor
			p.Cancellable = false
			if err := q.UpdateFrigateUpdateJob(ctx, p); err != nil {
				return outcome{}, err
			}
			after, err := q.LockFrigateUpdateJob(ctx, cur.ID)
			return outcome{before: cur, after: after}, err
		}
		after, err := completeCancelLocally(ctx, q, cur, key, s.now())
		return outcome{before: cur, after: after}, err
	})
	if err != nil {
		return Job{}, err
	}
	if res.refusal != nil {
		return Job{}, res.refusal
	}
	s.logTransition(ctx, res.before, res.after)
	if isTerminal(res.after) {
		s.forget(job.ID)
	}
	return s.loadJob(ctx, res.after)
}

// completeCancelLocally ends a locked job as CANCELLED without involving
// Frigate and marks the operator's cancel key as finished locally, so a replay
// of the key answers from the record. The caller holds the job lock and has
// confirmed no Start can still be in flight.
func completeCancelLocally(ctx context.Context, q sqlc.Querier, cur sqlc.FrigateUpdateJob, key string, now time.Time) (sqlc.FrigateUpdateJob, error) {
	p := paramsFromJob(cur)
	p.State = StateCancelled
	p.Cancellable = false
	p.Retryable = false
	p.CleanupPending = true
	p.CompletedAt = nullTime(now)
	if err := q.UpdateFrigateUpdateJob(ctx, p); err != nil {
		return sqlc.FrigateUpdateJob{}, err
	}
	if key != "" {
		if err := q.MarkFrigateUpdateActionLocal(ctx, sqlc.MarkFrigateUpdateActionLocalParams{JobID: cur.ID, Kind: actionCancel, RequestKey: key}); err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
	}
	return q.LockFrigateUpdateJob(ctx, cur.ID)
}

func checkActionAllowed(j sqlc.FrigateUpdateJob, pkgKind PackageKind, kind string) error {
	if pkgKind == PackageKindProtoOS {
		return checkProtoOSAction(j, kind)
	}
	switch kind {
	case actionRetry:
		if j.State != StateFailed || !j.Retryable {
			return fleeterror.NewFailedPreconditionError("only an update that failed after the controllers were stopped can be retried")
		}
	case actionCancel:
		switch j.State {
		case StateStaging, StateStaged, StateStarting:
		case StateRunning, StateFailed:
			if !j.Cancellable {
				return fleeterror.NewFailedPreconditionError("this update can no longer be cancelled")
			}
		default:
			return fleeterror.NewFailedPreconditionError("this update has already finished")
		}
	}
	return nil
}

// jobKind returns the kind of a job's package.
func (s *Service) jobKind(ctx context.Context, id uuid.UUID) (PackageKind, error) {
	kind, err := s.queries.GetFrigateUpdateJobPackageKind(ctx, id)
	return PackageKind(kind), err
}

// logJobEvent records an activity entry for a job. The activity log is
// best-effort: a failure here never affects the update.
func (s *Service) logJobEvent(ctx context.Context, job sqlc.FrigateUpdateJob, kind PackageKind, eventType, description string, result activitymodels.ResultType, errMsg string) {
	if s.activity == nil {
		return
	}
	var siteID *int64
	identifier := ""
	if device, err := s.queries.GetFrigateUpdateDeviceSite(ctx, job.DeviceID); err == nil {
		identifier = device.DeviceIdentifier
		siteID = siteArg(device.SiteID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("frigate update activity lookup failed", "job_id", job.ID, "error", err)
	}
	orgID := job.OrgID
	userID := job.ActorExternalID
	username := job.ActorUsername
	scopeType := "container"
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventType,
		Description:    description,
		Result:         result,
		ActorType:      activitymodels.ActorUser,
		UserID:         &userID,
		Username:       &username,
		OrganizationID: &orgID,
		ScopeType:      &scopeType,
		ScopeLabel:     &identifier,
		SiteID:         siteID,
		Metadata: map[string]any{
			"job_id":            job.ID.String(),
			"package_kind":      string(kind),
			"device_identifier": identifier,
			"target_version":    job.TargetVersion,
			"from_version":      job.FromVersion,
			"state":             job.State,
			"rollout_phase":     job.RolloutPhase,
			"stage_only":        job.StageOnly,
			"error_code":        job.ErrorCode,
		},
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", eventType, job.ID, job.ActionCounter),
	}
	if errMsg != "" {
		event.ErrorMessage = &errMsg
	}
	s.activity.Log(ctx, event)
}

// logTransition records the activity entry for a state change.
func (s *Service) logTransition(ctx context.Context, before, after sqlc.FrigateUpdateJob) {
	if before.State == after.State || s.activity == nil {
		return
	}
	kind, err := s.jobKind(ctx, after.ID)
	if err != nil {
		slog.Warn("frigate update activity lookup failed", "job_id", after.ID, "error", err)
		kind = PackageKindFrigate
	}
	rules := kind.rules()
	noun := rules.noun
	switch after.State {
	case StateRunning:
		s.logJobEvent(ctx, after, kind, "frigate_update_running",
			fmt.Sprintf(rules.runningEntry, after.TargetVersion), activitymodels.ResultSuccess, "")
	case StateSucceeded:
		s.logJobEvent(ctx, after, kind, "frigate_update_succeeded",
			fmt.Sprintf("%s updated to %s", sentenceCase(noun), after.TargetVersion), activitymodels.ResultSuccess, "")
	case StateVerified:
		s.logJobEvent(ctx, after, kind, "frigate_update_verified",
			fmt.Sprintf("Verified the package for %s on every controller", after.TargetVersion), activitymodels.ResultSuccess, "")
	case StateFailed:
		s.logJobEvent(ctx, after, kind, "frigate_update_failed",
			fmt.Sprintf("%s update to %s failed", sentenceCase(noun), after.TargetVersion), activitymodels.ResultFailure, after.ErrorMessage)
	case StateCancelled:
		s.logJobEvent(ctx, after, kind, "frigate_update_cancelled",
			fmt.Sprintf("Cancelled the %s update to %s", noun, after.TargetVersion), activitymodels.ResultSuccess, "")
	case StateAbandoned:
		s.logJobEvent(ctx, after, kind, "frigate_update_abandoned",
			fmt.Sprintf("The %s update to %s was abandoned", noun, after.TargetVersion), activitymodels.ResultFailure, after.ErrorMessage)
	}
}

// sentenceCase makes the first letter of a noun upper case, for a noun that
// starts a sentence.
func sentenceCase(noun string) string {
	if noun == "" {
		return noun
	}
	return strings.ToUpper(noun[:1]) + noun[1:]
}

// paramsFromJob copies a job into update parameters so callers change only the
// fields they own.
func paramsFromJob(j sqlc.FrigateUpdateJob) sqlc.UpdateFrigateUpdateJobParams {
	return sqlc.UpdateFrigateUpdateJobParams{
		ID: j.ID, State: j.State, RolloutPhase: j.RolloutPhase, FromVersion: j.FromVersion,
		DriverInstanceID: j.DriverInstanceID, ErrorCode: j.ErrorCode, ErrorMessage: j.ErrorMessage, NextStep: j.NextStep,
		Retryable: j.Retryable, Cancellable: j.Cancellable, DisruptionStarted: j.DisruptionStarted,
		ServedByMaintenance: j.ServedByMaintenance, StartAttempted: j.StartAttempted, ActionCounter: j.ActionCounter,
		StartAttemptedAt: j.StartAttemptedAt, StartLeaseUntil: j.StartLeaseUntil, CleanupPending: j.CleanupPending,
		CancelRequestedAt: j.CancelRequestedAt, CancelRequestKey: j.CancelRequestKey, CancelRequestActor: j.CancelRequestActor,
		RolloutUpdatedAt: j.RolloutUpdatedAt, LastPolledAt: j.LastPolledAt, LastContactAt: j.LastContactAt,
		UnreachableSince: j.UnreachableSince, CompletedAt: j.CompletedAt,
	}
}
