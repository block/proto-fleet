package frigateupdate

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// rolloutObservation is one answer from Frigate about a rollout.
type rolloutObservation struct {
	Rollout sdk.FrigateRollout
	// ServedByMaintenance is true when Frigate's maintenance service answered
	// because the regular daemon is stopped for the install.
	ServedByMaintenance bool
}

func sdkAction(id uuid.UUID, key, actor string) sdk.FrigateAction {
	return sdk.FrigateAction{JobID: id.String(), IdempotencyKey: key, Actor: actor}
}

// stateForPhase maps Frigate's rollout phase to the Fleet job state.
func stateForPhase(p sdk.FrigateRolloutPhase) string {
	switch p {
	case sdk.FrigatePhaseSucceeded:
		return StateSucceeded
	case sdk.FrigatePhaseFailed:
		return StateFailed
	case sdk.FrigatePhaseCancelled:
		return StateCancelled
	case sdk.FrigatePhaseVerified:
		return StateVerified
	case sdk.FrigatePhaseAbandoned:
		return StateAbandoned
	case sdk.FrigatePhaseUnspecified, sdk.FrigatePhaseVerifying, sdk.FrigatePhaseStopping, sdk.FrigatePhaseInstalling,
		sdk.FrigatePhaseMigrating, sdk.FrigatePhaseStarting, sdk.FrigatePhaseUnknown:
		return StateRunning
	default:
		return StateRunning
	}
}

// finalObservation reports whether a Frigate answer ends the rollout for
// Fleet: a phase Frigate never leaves, or a failure that cannot be retried.
// Such an answer is applied even when its updated_at is older than the newest
// one applied, because Frigate stamps transitions with the active driver's
// clock and a driver change can move that clock backwards. An answer that
// leaves the rollout running, including a retryable failure, keeps the
// updated_at ordering: a delayed failure from before Retry must not overwrite
// the resumed rollout, and it cannot be final because Retry was accepted.
func finalObservation(ro sdk.FrigateRollout) bool {
	switch state := stateForPhase(ro.Phase); state {
	case StateRunning:
		return false
	case StateFailed:
		return !ro.Retryable
	default:
		return true
	}
}

// applyRollout folds a Frigate answer into the job. The job lock serializes it
// against operator actions and other runner passes. A finished job is never
// changed, and an answer older than the newest one already applied (a lagging
// controller) only refreshes contact times, so the newest updated_at wins,
// unless the answer is final (see finalObservation).
func (s *Service) applyRollout(ctx context.Context, id uuid.UUID, obs rolloutObservation) (sqlc.FrigateUpdateJob, error) {
	var before sqlc.FrigateUpdateJob
	after, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.FrigateUpdateJob, error) {
		cur, err := q.LockFrigateUpdateJob(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sqlc.FrigateUpdateJob{}, fleeterror.NewNotFoundError("update not found")
			}
			return sqlc.FrigateUpdateJob{}, err
		}
		before = cur
		if isTerminal(cur) {
			return cur, nil
		}
		ro := obs.Rollout
		if ro.JobID != "" && ro.JobID != cur.ID.String() {
			return sqlc.FrigateUpdateJob{}, fleeterror.NewInternalErrorf("Frigate answered for rollout %s while tracking %s", ro.JobID, cur.ID)
		}
		now := s.now()
		p := paramsFromJob(cur)
		p.LastPolledAt = nullTime(now)
		p.LastContactAt = nullTime(now)
		p.UnreachableSince = sql.NullTime{}

		stale := !ro.UpdatedAt.IsZero() && cur.RolloutUpdatedAt.Valid && ro.UpdatedAt.Before(cur.RolloutUpdatedAt.Time) &&
			!finalObservation(ro)
		if !stale {
			state := stateForPhase(ro.Phase)
			p.State = state
			p.RolloutPhase = string(ro.Phase)
			if ro.FromVersion != "" {
				p.FromVersion = truncate(ro.FromVersion, 128)
			}
			if ro.DriverInstanceID != "" {
				p.DriverInstanceID = truncate(ro.DriverInstanceID, 256)
			}
			p.ErrorCode = truncate(ro.ErrorCode, 128)
			p.ErrorMessage = truncate(ro.ErrorMessage, maxStoredMessage)
			p.NextStep = truncate(ro.NextStep, maxStoredMessage)
			p.Retryable = state == StateFailed && ro.Retryable
			p.Cancellable = (state == StateRunning || state == StateFailed) && ro.Cancellable
			p.DisruptionStarted = cur.DisruptionStarted || ro.DisruptionStarted
			p.ServedByMaintenance = obs.ServedByMaintenance
			if !ro.UpdatedAt.IsZero() && (!cur.RolloutUpdatedAt.Valid || !ro.UpdatedAt.Before(cur.RolloutUpdatedAt.Time)) {
				p.RolloutUpdatedAt = nullTime(ro.UpdatedAt)
			}
			if state == StateRunning {
				p.CompletedAt = sql.NullTime{}
			} else if !p.Retryable {
				p.CompletedAt = nullTime(now)
			}
			// Staged copies are Fleet's to clean up only when Frigate never took
			// the job over. A rollout Frigate ran to CANCELLED, VERIFIED, or a
			// terminal state removes its own package: its root helper discards the
			// request and package when the rollout exits (the self-coordinated
			// follower's finish path for verified, cancelled and pre-disruption
			// ends, and removal after a successful start). The one case it keeps
			// the package is a failure after the controllers were stopped, which
			// needs the operator.
			if state == StateFailed && !p.Retryable && !p.DisruptionStarted {
				// Nothing was installed anywhere; make sure no staged copy lingers.
				p.CleanupPending = true
			}
		}
		if err := q.UpdateFrigateUpdateJob(ctx, p); err != nil {
			return sqlc.FrigateUpdateJob{}, err
		}
		if !stale {
			if err := applyInstances(ctx, q, cur.ID, ro.Instances); err != nil {
				return sqlc.FrigateUpdateJob{}, err
			}
		}
		return q.LockFrigateUpdateJob(ctx, id)
	})
	if err != nil {
		return sqlc.FrigateUpdateJob{}, err
	}
	s.logTransition(ctx, before, after)
	if isTerminal(after) {
		s.forget(id)
	}
	return after, nil
}

// applyInstances records each controller's helper progress, keeping the
// staging columns Fleet owns.
func applyInstances(ctx context.Context, q sqlc.Querier, jobID uuid.UUID, progress []sdk.FrigateInstanceProgress) error {
	if len(progress) == 0 {
		return nil
	}
	rows, err := q.ListFrigateUpdateInstances(ctx, jobID)
	if err != nil {
		return err
	}
	byID := make(map[string]sqlc.FrigateUpdateInstance, len(rows))
	for _, r := range rows {
		byID[r.InstanceID] = r
	}
	for _, in := range progress {
		row, ok := byID[in.InstanceID]
		if !ok {
			continue
		}
		if err := q.UpdateFrigateUpdateInstance(ctx, sqlc.UpdateFrigateUpdateInstanceParams{
			JobID: jobID, InstanceID: row.InstanceID,
			StageState: row.StageState, ManifestSha256: row.ManifestSha256, StagedBytes: row.StagedBytes,
			AgentPhase: string(in.Phase), LastContact: nullTime(in.LastContact), ContactStale: in.ContactStale,
			RunningVersion: truncate(in.RunningVersion, 128), MigrationExecuted: in.MigrationExecuted,
			ErrorCode: truncate(in.ErrorCode, 128), ErrorMessage: truncate(in.ErrorMessage, maxStoredMessage),
		}); err != nil {
			return err
		}
	}
	return nil
}
