package frigateupdate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// A ProtoOS job copies the proto-os package to each controller of one
// container. Each controller verifies and installs its copy alone as soon as
// the copy commits, and reports the result in its update support. Fleet sends
// no Start, Retry or Cancel to a controller. The job states are:
//
//   - staging: Fleet copies the package. An operator cancel is possible until
//     Fleet sends the first call that can commit a copy.
//   - running: every copy is committed. Fleet reads update support until each
//     controller reports this job as finished.
//   - succeeded: each controller reports this job as succeeded and has the
//     target version installed.
//   - failed: a controller reports this job as failed, or does not finish it
//     in time. The job does not block the container and cannot be retried; a
//     new job for the same package is the retry, and a controller that already
//     has the version installed does not install it again.
//
// Staging failures, cancel before the first commit, unreachable controllers
// and cleanup are the same as for a controller software job.

const nextStepProtoOSRestart = "Resolve the cause, then start the update again. A controller that already has this version installed does not install it again."

// processProtoOSJob advances a ProtoOS job by one step.
func (s *Service) processProtoOSJob(ctx context.Context, job sqlc.FrigateUpdateJob) {
	switch job.State {
	case StateStaging:
		if s.stageJob(ctx, job) {
			s.markProtoOSCommitted(ctx, job.ID)
		}
	case StateRunning:
		s.pollProtoOSJob(ctx, job)
	}
}

// closeCancelWindow ends the time in which an operator may cancel a job. Fleet
// calls it before the first call that can commit a copy of a package whose
// commit starts its install. The job lock orders it with an operator cancel,
// so a cancelled job never commits a copy, and a job with a committed copy is
// never cancelled. It returns errStopStaging when the job left staging.
func (s *Service) closeCancelWindow(ctx context.Context, id uuid.UUID) error {
	_, after, _, err := s.mutate(ctx, id, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		if cur.State != StateStaging || !cur.Cancellable {
			return false
		}
		p.Cancellable = false
		return true
	})
	if err != nil {
		return err
	}
	if after.State != StateStaging {
		return errStopStaging
	}
	return nil
}

// markProtoOSCommitted moves a job whose copies are all committed to running.
// The install deadline counts from start_attempted_at: the last commit is
// what starts the install, as a Start does for a controller software job.
func (s *Service) markProtoOSCommitted(ctx context.Context, id uuid.UUID) {
	before, after, changed, err := s.mutate(ctx, id, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		if cur.State != StateStaging {
			return false
		}
		p.State = StateRunning
		p.Cancellable = false
		p.StartAttempted = true
		p.StartAttemptedAt = nullTime(s.now())
		return true
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "mark protoos update running", "job_id", id, "error", err)
		}
		return
	}
	if changed {
		s.forget(id)
		s.logTransition(ctx, before, after)
	}
}

// pollProtoOSJob reads each controller's ProtoOS report and applies it.
func (s *Service) pollProtoOSJob(ctx context.Context, job sqlc.FrigateUpdateJob) {
	updater, err := s.updaterFor(ctx, job.DeviceID)
	if err != nil {
		s.markUnreachable(ctx, job.ID, err)
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	instances, err := updater.FrigateUpdateSupport(callCtx)
	cancel()
	if ctx.Err() != nil {
		return
	}
	reports := make(map[string]sdk.FrigateProtoOSSupport, len(instances))
	for _, in := range instances {
		if in.Err == "" && slices.Contains(job.ExpectedInstanceIds, in.InstanceID) {
			reports[in.InstanceID] = in.ProtoOS
		}
	}
	if err == nil && len(reports) == 0 {
		err = errors.New("no controller of the update reported its update support")
	}
	if err != nil {
		s.markUnreachable(ctx, job.ID, err)
		return
	}
	if err := s.applyProtoOSReports(ctx, job.ID, reports); err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "apply protoos update reports", "job_id", job.ID, "error", err)
	}
}

// applyProtoOSReports records what each controller that answered reports, and
// ends the job when the reports decide it.
func (s *Service) applyProtoOSReports(ctx context.Context, id uuid.UUID, reports map[string]sdk.FrigateProtoOSSupport) error {
	var before sqlc.FrigateUpdateJob
	after, err := runTx(ctx, s, func(q sqlc.Querier) (sqlc.FrigateUpdateJob, error) {
		cur, err := q.LockFrigateUpdateJob(ctx, id)
		if err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		before = cur
		if cur.State != StateRunning {
			return cur, nil
		}
		now := s.now()
		for instanceID, r := range reports {
			var state, code, message string
			if r.Job.JobID == cur.ID.String() {
				state = string(r.Job.State)
				// A controller reports a message in every state; only a failure is an error.
				if r.Job.State == sdk.FrigateProtoOSJobFailed {
					code, message = CodeProtoOSInstallFailed, r.Job.Message
				}
			}
			if err := q.UpdateFrigateUpdateInstanceProtoOS(ctx, sqlc.UpdateFrigateUpdateInstanceProtoOSParams{
				JobID: cur.ID, InstanceID: instanceID, ProtoosJobState: truncate(state, 32),
				ProtoosInstalledVersion: truncate(r.InstalledVersion, 128), ProtoosServedReleaseTag: truncate(r.Served.ReleaseTag, 128),
				LastContact: nullTime(now), ErrorCode: code, ErrorMessage: truncate(message, maxStoredMessage),
			}); err != nil {
				return sqlc.FrigateUpdateJob{}, err
			}
		}
		p := paramsFromJob(cur)
		p.LastPolledAt = nullTime(now)
		p.LastContactAt = nullTime(now)
		p.UnreachableSince = sql.NullTime{}
		if v := protoOSOutcome(cur, reports, now, s.cfg.StartTimeout); v.state != StateRunning {
			p.State = v.state
			p.ErrorCode = v.code
			p.ErrorMessage = truncate(v.message, maxStoredMessage)
			p.NextStep = v.nextStep
			p.Retryable = false
			p.Cancellable = false
			p.CompletedAt = nullTime(now)
		}
		if err := q.UpdateFrigateUpdateJob(ctx, p); err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		return q.LockFrigateUpdateJob(ctx, id)
	})
	if err != nil {
		return err
	}
	s.logTransition(ctx, before, after)
	if isTerminal(after) {
		s.forget(id)
	}
	return nil
}

// protoOSVerdict is the job state that the controllers' reports decide.
type protoOSVerdict struct {
	state, code, message, nextStep string
}

// protoOSOutcome decides a running job from the reports of the controllers
// that answered. A controller that did not answer never fails the job; the
// job is abandoned when one stays silent for unreachableGiveUp after the last
// commit. installTimeout bounds how long a controller that answers may take to
// finish this job after the last commit.
func protoOSOutcome(job sqlc.FrigateUpdateJob, reports map[string]sdk.FrigateProtoOSSupport, now time.Time, installTimeout time.Duration) protoOSVerdict {
	jobID := job.ID.String()
	expected := sortedCopy(job.ExpectedInstanceIds)
	finished := func(r sdk.FrigateProtoOSSupport) bool {
		return r.Job.JobID == jobID && r.Job.State == sdk.FrigateProtoOSJobSucceeded && r.InstalledVersion == job.TargetVersion
	}
	succeeded := 0
	for _, id := range expected {
		r, ok := reports[id]
		switch {
		case ok && r.Job.JobID == jobID && r.Job.State == sdk.FrigateProtoOSJobFailed:
			return protoOSVerdict{state: StateFailed, code: CodeProtoOSInstallFailed, nextStep: nextStepProtoOSRestart,
				message: withDetail(fmt.Sprintf("Controller %s could not install ProtoOS %s", id, job.TargetVersion), r.Job.Message)}
		case ok && finished(r):
			succeeded++
		}
	}
	if succeeded == len(expected) {
		return protoOSVerdict{state: StateSucceeded}
	}
	elapsed := now.Sub(job.StartAttemptedAt.Time)
	if elapsed <= installTimeout {
		return protoOSVerdict{state: StateRunning}
	}
	for _, id := range expected {
		r, ok := reports[id]
		switch {
		case !ok || finished(r):
		case r.Job.JobID != jobID:
			return protoOSVerdict{state: StateFailed, code: CodeProtoOSJobNotReported, nextStep: nextStepProtoOSRestart,
				message: fmt.Sprintf("Controller %s did not report the ProtoOS install of this update within %s after its copy was committed.", id, installTimeout)}
		default:
			detail := r.Job.Message
			if r.Job.State == sdk.FrigateProtoOSJobSucceeded {
				detail = fmt.Sprintf("it reports success, but has version %q installed", r.InstalledVersion)
			}
			return protoOSVerdict{state: StateFailed, code: CodeProtoOSInstallTimeout, nextStep: nextStepProtoOSRestart,
				message: withDetail(fmt.Sprintf("Controller %s did not finish installing ProtoOS %s within %s", id, job.TargetVersion, installTimeout), detail)}
		}
	}
	if elapsed >= unreachableGiveUp {
		return protoOSVerdict{state: StateAbandoned, code: CodeControllersLost,
			message:  fmt.Sprintf("A controller has not answered for %s after its copy was committed, so Fleet stopped tracking this update.", unreachableGiveUp),
			nextStep: "Check the controllers on site. Once they answer, start a new update if they do not have the target version installed."}
	}
	return protoOSVerdict{state: StateRunning}
}

// checkProtoOSAction is checkActionAllowed for a ProtoOS job.
func checkProtoOSAction(j sqlc.FrigateUpdateJob, kind string) error {
	switch {
	case kind == actionRetry:
		return fleeterror.NewFailedPreconditionError("A ProtoOS update cannot be retried. Start a new update with the same package: " +
			"a controller that already has this version installed does not install it again.")
	case isTerminal(j):
		return fleeterror.NewFailedPreconditionError("this update has already finished")
	case j.State == StateStaging && j.Cancellable:
		return nil
	default:
		return fleeterror.NewFailedPreconditionError("A controller has committed its copy of the ProtoOS package, so its install runs to its end. " +
			"This update can no longer be cancelled.")
	}
}

// protoOSJobActive reports whether a controller is still working on a ProtoOS
// job. A state this Fleet does not know is not final.
func protoOSJobActive(j sdk.FrigateProtoOSJob) bool {
	switch j.State {
	case sdk.FrigateProtoOSJobQueued, sdk.FrigateProtoOSJobInstalling, sdk.FrigateProtoOSJobUnknown:
		return j.JobID != ""
	case sdk.FrigateProtoOSJobUnspecified, sdk.FrigateProtoOSJobSucceeded, sdk.FrigateProtoOSJobFailed:
		return false
	default:
		return j.JobID != ""
	}
}

// evaluateProtoOSSupport is evaluateSupport for a ProtoOS package, after the
// checks that every update needs. A ProtoOS install does not stop Frigate and
// does not involve the HA peer, so self coordination, etcd health, the HA
// cluster, the architecture and the Frigate release keys do not apply.
func evaluateProtoOSSupport(instances []sdk.FrigateInstanceSupport, pkg *Package) (eligible bool, code, reason string) {
	for _, in := range instances {
		switch {
		case !in.ProtoOS.SelfSupported:
			return false, CodeProtoOSUnsupported, withDetail(fmt.Sprintf("controller %s does not accept ProtoOS packages from Fleet; update its controller software first", in.InstanceID), in.ProtoOS.SupportMessage)
		case !in.RemoteTriggerAllowed:
			return false, CodeRemoteTriggerDisabled, remoteTriggerDisabledReason(in.InstanceID)
		case !slices.Contains(in.ProtoOS.ReleaseRepositories, pkg.Repository):
			return false, CodeProtoOSRepositoryUntrusted, fmt.Sprintf("controller %s does not accept ProtoOS releases from %s", in.InstanceID, pkg.Repository)
		}
	}
	if reason := unknownPeerReason(instances); reason != "" {
		return false, CodeInstanceUnreachable, reason
	}
	return true, "", ""
}
