package rollout

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"connectrpc.com/authn"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const (
	rolloutActorName = "rollout-enforcement"

	// resendInterval is how long the enforcement loop waits before
	// re-issuing an update command to a miner that is still mismatched
	// (e.g. it was offline or the install failed).
	resendInterval = 10 * time.Minute

	holdStabilizing = "Stabilizing"
	// holdArtifactMissing is the hold reason while no uploaded file carries
	// the rollout's checksum.
	holdArtifactMissing = "Firmware file is unavailable. Upload the assigned firmware again to continue."
)

// EnforceTick runs one enforcement pass: it observes completed commands from
// terminal history, starts rollouts for assignments with mismatched members
// and drives every active rollout forward. Errors are logged per rollout so
// one bad rollout cannot stall the others.
func (s *Service) EnforceTick(ctx context.Context) {
	// Observe reservations even while every rollout is paused, at review, or
	// terminal. Otherwise a recovery between active ticks could be missed.
	q := s.store.GetQueries(ctx)
	if err := q.ObserveFirmwareRolloutReservationsOffline(ctx, sql.NullInt64{}); err != nil {
		slog.Error("rollout enforcement: observe offline reservations", "error", err)
		return
	}
	if err := q.DeleteReleasedFirmwareRolloutReservations(ctx); err != nil {
		slog.Error("rollout enforcement: release command reservations", "error", err)
		return
	}
	s.reconcileCompletedDispatches(ctx)
	s.startNeededRollouts(ctx)
	active, err := s.store.GetQueries(ctx).ListActiveFirmwareRollouts(ctx)
	if err != nil {
		slog.Error("rollout enforcement: list active rollouts", "error", err)
		return
	}
	// Reconcile every rollout's targets before enforcing its next stage.
	// Dispatch reads the channel's live offline budget under its lock.
	prepared := make([][]target, len(active))
	for i, row := range active {
		targets, err := s.prepareRollout(ctx, row.FirmwareRollout)
		if err != nil {
			slog.Error("rollout enforcement: reconcile targets", "rollout_id", row.FirmwareRollout.ID, "error", err)
			continue
		}
		prepared[i] = targets
	}
	for i, row := range active {
		if prepared[i] == nil {
			continue
		}
		if err := s.enforceRollout(ctx, row.FirmwareRollout, row.ChannelName, prepared[i]); err != nil {
			slog.Error("rollout enforcement", "rollout_id", row.FirmwareRollout.ID, "error", err)
		}
	}
}

// prepareRollout reconciles membership, provenance and convergence together,
// so this logical change advances the rollout's revision once. A terminal
// rollout returns nil: a stale active-row scan must not rewrite its history.
func (s *Service) prepareRollout(ctx context.Context, r sqlc.FirmwareRollout) ([]target, error) {
	var targets []target
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		targets = nil
		if _, err := s.store.GetQueries(ctx).GetReleaseChannelForUpdate(ctx, sqlc.GetReleaseChannelForUpdateParams{ChannelID: r.ChannelID, OrgID: r.OrgID}); err != nil {
			return channelLookupError(r.ChannelID, err)
		}
		current, _, err := s.lockRollout(ctx, r.OrgID, r.ID, 0)
		if err != nil {
			return err
		}
		if current.Status != StatusActive {
			return nil
		}
		targets, err = s.syncMembership(ctx, current)
		if err != nil {
			return err
		}
		targets, err = s.recordProvenance(ctx, current, targets)
		if err != nil {
			return err
		}
		targets, err = s.recordConvergence(ctx, current, targets)
		if err == nil && targets == nil {
			targets = []target{}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// offlineBudget is a channel's live max_concurrent_offline and the slots its
// current and historical targets hold right now. Each distinct target holds at most
// one slot: an offline slot while it is observed offline in any phase, or a
// reservation while an update command is outstanding for it and it has not
// yet been seen offline. Nil means unlimited.
type offlineBudget struct {
	limit int32
	held  map[int64]bool
}

func (b *offlineBudget) room() int {
	if b == nil || b.limit <= 0 {
		return -1
	}
	return max(int(b.limit)-len(b.held), 0)
}

func (b *offlineBudget) reserve(deviceID int64) {
	if b != nil {
		b.held[deviceID] = true
	}
}

// refreshOfflineBudget runs under the channel lock before dispatch. It counts
// current member outages and durable reservations for unresolved dispatches, so
// a departure releases only settled risk rather than an in-flight update.
func (s *Service) refreshOfflineBudget(ctx context.Context, channelID int64, limit int32) (*offlineBudget, error) {
	q := s.store.GetQueries(ctx)
	if err := q.ObserveFirmwareRolloutReservationsOffline(ctx, sql.NullInt64{Int64: channelID, Valid: true}); err != nil {
		return nil, fleeterror.NewInternalErrorf("observe rollout offline reservations: %w", err)
	}
	if limit <= 0 {
		return nil, nil
	}
	deviceIDs, err := q.ListFirmwareRolloutOfflineSlots(ctx, channelID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("load rollout offline slots: %w", err)
	}
	budget := &offlineBudget{limit: limit, held: map[int64]bool{}}
	for _, deviceID := range deviceIDs {
		budget.reserve(deviceID)
	}
	return budget, nil
}

// startNeededRollouts creates a rollout using the channel's current behavior
// for every assigned pair with mismatched, unsuppressed members and no active
// rollout: late joiners, re-entries and miners that drifted. Automatic
// enforcement preserves the channel's pacing and review policy. It carries the
// pair's generation and its saved assignment lineage, including assignments
// that originally needed no rollout because every member already matched.
func (s *Service) startNeededRollouts(ctx context.Context) {
	needed, err := s.store.GetQueries(ctx).ListReleaseChannelFirmwareNeedingRollout(ctx)
	if err != nil {
		slog.Error("rollout enforcement: find assignments needing rollout", "error", err)
		return
	}
	for _, n := range needed {
		err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
			q := s.store.GetQueries(ctx)
			// The scan only discovers candidates. Serialize with assignment
			// writers, then reload the assignment and need before creating a
			// rollout so a concurrent clear or replacement cannot be undone.
			channel, err := q.GetReleaseChannelForUpdate(ctx, sqlc.GetReleaseChannelForUpdateParams{ChannelID: n.ChannelID, OrgID: n.OrgID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return channelLookupError(n.ChannelID, err)
			}
			assignment, err := q.GetReleaseChannelFirmware(ctx, sqlc.GetReleaseChannelFirmwareParams{
				ChannelID: n.ChannelID, Manufacturer: n.Manufacturer, Model: n.Model,
			})
			if errors.Is(err, sql.ErrNoRows) || (err == nil && assignment.FirmwareChecksum == "") {
				return nil
			}
			if err != nil {
				return fleeterror.NewInternalErrorf("get firmware assignment: %w", err)
			}
			if _, err := q.GetActiveFirmwareRolloutForPair(ctx, sqlc.GetActiveFirmwareRolloutForPairParams{
				ChannelID: n.ChannelID, Manufacturer: n.Manufacturer, Model: n.Model,
			}); err == nil {
				return nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return fleeterror.NewInternalErrorf("get active firmware rollout: %w", err)
			}
			spec := rolloutSpec{
				OrgID: n.OrgID, ChannelID: n.ChannelID, ChannelName: channel.Name,
				Pair:             PairKey{Manufacturer: assignment.Manufacturer, Model: assignment.Model},
				FirmwareChecksum: assignment.FirmwareChecksum, FirmwareVersion: assignment.FirmwareVersion,
				AssignmentGeneration: assignment.AssignmentGeneration, Actor: SystemActor, Behavior: behaviorFromChannel(channel),
				PreviousFirmwareChecksum: assignment.PreviousFirmwareChecksum,
				PreviousFirmwareVersion:  assignment.PreviousFirmwareVersion,
			}
			r, err := s.startRollout(ctx, spec)
			if err != nil || r == nil {
				return err
			}
			s.logRolloutEvent(ctx, *r, channel.Name, EventRolloutStarted, true, map[string]any{"reconciliation": true})
			return nil
		})
		if err != nil {
			slog.Error("rollout enforcement: start rollout", "channel_id", n.ChannelID, "manufacturer", n.Manufacturer, "model", n.Model, "error", err)
		}
	}
}

// enforceRollout drives one active rollout forward according to its stage.
// Paused rollouts are left alone (membership is still synced so the view
// stays truthful). The batch stage updates only the current batch and parks
// the rollout at the review gate (or the between-batch wait) once the batch
// settles; the rest stage (and every all-at-once rollout) updates all
// remaining targets and finishes the rollout once every target settled.
func (s *Service) enforceRollout(ctx context.Context, r sqlc.FirmwareRollout, channelName string, targets []target) error {
	if r.PausedAt.Valid {
		return nil
	}
	// Dispatch owns its non-retryable transaction and refreshes targets under
	// lock. Preparation is only a hint about whether a send may be needed.
	if (r.Stage == StageBatch || r.Stage == StageRest) && !allSettled(reviewScope(r, targets), r) {
		halted, err := s.dispatchUpdates(ctx, r)
		if err != nil || halted == 0 {
			return err
		}
	}
	reopened, err := s.advanceRolloutProgress(ctx, r, channelName)
	if err != nil || reopened == nil {
		return err
	}
	// Drift reopening a review/wait stage can dispatch in the same tick,
	// after the progress transaction commits and releases its header lock.
	halted, err := s.dispatchUpdates(ctx, *reopened)
	if err != nil || halted == 0 {
		return err
	}
	_, err = s.advanceRolloutProgress(ctx, *reopened, channelName)
	return err
}

// advanceRolloutProgress serializes settlement with retry, cancellation and
// preparation. Every decision uses fresh targets while holding the header lock;
// an old all-settled snapshot cannot finish or advance newly queued work.
// It returns a reopened batch that may need dispatch outside this transaction.
func (s *Service) advanceRolloutProgress(ctx context.Context, observed sqlc.FirmwareRollout, channelName string) (*sqlc.FirmwareRollout, error) {
	var reopened *sqlc.FirmwareRollout
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		reopened = nil
		r, _, err := s.lockRollout(ctx, observed.OrgID, observed.ID, 0)
		if err != nil {
			return err
		}
		if r.Status != StatusActive || r.PausedAt.Valid {
			return nil
		}
		if r.Stage != observed.Stage || r.CurrentBatch != observed.CurrentBatch ||
			!r.StageChangedAt.Equal(observed.StageChangedAt) || r.StagePausedMicroseconds != observed.StagePausedMicroseconds {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonNotAtGate}, "rollout %d changed or was paused before its stage could advance", r.ID)
		}
		targets, err := s.listTargets(ctx, r)
		if err != nil {
			return err
		}
		scope := reviewScope(r, targets)
		if !allSettled(scope, r) {
			if r.Stage == StageAwaitingReview || r.Stage == StageWaiting {
				if err := s.transition(ctx, &r, r.Stage, StageBatch, nil); err != nil {
					return err
				}
				reopened = &r
			}
			return nil
		}
		behavior := behaviorFromRollout(r)
		switch r.Stage {
		case StageBatch:
			if behavior.gatesAfterBatch() {
				return s.transition(ctx, &r, StageBatch, StageAwaitingReview, func() {
					s.logRolloutEvent(ctx, r, channelName, EventRolloutReviewReady, true, map[string]any{
						"batch": r.CurrentBatch + 1, "batch_count": r.BatchCount,
					})
				})
			}
			if behavior.WaitBetweenBatchesSeconds > 0 {
				return s.transition(ctx, &r, StageBatch, StageWaiting, nil)
			}
			return s.advance(ctx, &r, StageBatch, nil)

		case StageAwaitingReview:
			if !r.BehaviorSnapshot.AutoContinue {
				return nil
			}
			ev := s.evaluate(r, scope)
			if !ev.ReadyToAdvance {
				return nil
			}
			if err := s.advance(ctx, &r, StageAwaitingReview, nil); err != nil {
				return err
			}
			extra := map[string]any{"auto_continued": true}
			if ev.HashrateChangePercent != nil {
				extra["hashrate_change_percent"] = *ev.HashrateChangePercent
			}
			s.logRolloutEvent(ctx, r, channelName, EventRolloutContinued, true, extra)
			return nil

		case StageWaiting:
			wait := time.Duration(behavior.WaitBetweenBatchesSeconds) * time.Second
			if s.stageElapsed(r) < wait {
				return nil
			}
			return s.advance(ctx, &r, StageWaiting, nil)

		default:
			status, event := StatusCompleted, EventRolloutCompleted
			if anyFailed(scope) {
				status, event = StatusCompletedWithFailures, EventRolloutCompletedWithFailures
			}
			n, err := s.store.GetQueries(ctx).FinishFirmwareRollout(ctx, sqlc.FinishFirmwareRolloutParams{RolloutID: r.ID, Status: status})
			if err != nil {
				return fleeterror.NewInternalErrorf("finish rollout: %w", err)
			}
			if n > 0 {
				r.Status = status
				s.logRolloutEvent(ctx, r, channelName, event, true, map[string]any{"failed": countFailed(scope)})
			}
			return nil
		}
	})
	return reopened, err
}

// transition moves a rollout between stages of the same batch (batch ->
// awaiting_review / waiting); onDone runs only when this tick won the race.
func (s *Service) transition(ctx context.Context, r *sqlc.FirmwareRollout, from, to string, onDone func()) error {
	changedAt, err := s.store.GetQueries(ctx).AdvanceFirmwareRolloutStage(ctx, sqlc.AdvanceFirmwareRolloutStageParams{
		RolloutID: r.ID, FromStage: from, Stage: to, CurrentBatch: r.CurrentBatch,
		ExpectedStageChangedAt: r.StageChangedAt, ExpectedStagePausedMicroseconds: r.StagePausedMicroseconds,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonNotAtGate}, "rollout %d changed or was paused before its stage could advance", r.ID)
	}
	if err != nil {
		return fleeterror.NewInternalErrorf("transition rollout: %w", err)
	}
	r.Stage, r.StageChangedAt, r.StagePausedMicroseconds = to, changedAt, 0
	if onDone != nil {
		onDone()
	}
	return nil
}

// stageElapsed counts active time in the current stage while preserving its
// historical start timestamp. An ongoing pause freezes the clock immediately;
// completed pauses are accumulated by ResumeFirmwareRollout.
func (s *Service) stageElapsed(r sqlc.FirmwareRollout) time.Duration {
	until := s.now()
	if r.PausedAt.Valid {
		until = r.PausedAt.Time
	}
	elapsed := until.Sub(r.StageChangedAt)
	if elapsed <= 0 || r.StagePausedMicroseconds > elapsed.Microseconds() {
		return 0
	}
	return elapsed - time.Duration(r.StagePausedMicroseconds)*time.Microsecond
}

// syncMembership reconciles the rollout's target set with the channel's
// live membership: miners that left the scope are excluded, miners that
// came back are re-included, and mismatched members not yet in the rollout
// are appended as late joiners (updated in the rest stage). Returns the
// refreshed targets. Firmware may rename the observed manufacturer/model;
// an enrolled target stays tied to its paired device so the update must still
// verify or fail. Its current pair is checked separately before dispatch.
func (s *Service) syncMembership(ctx context.Context, r sqlc.FirmwareRollout) ([]target, error) {
	q := s.store.GetQueries(ctx)
	targets, err := s.listTargets(ctx, r)
	if err != nil {
		return nil, err
	}
	targets, err = s.excludeDepartedTargets(ctx, r, targets)
	if err != nil {
		return nil, err
	}
	var returners []int64
	for _, t := range targets {
		if t.IsChannelMember && t.excluded() {
			returners = append(returners, t.DeviceID)
		}
	}
	changed := false
	if len(returners) > 0 {
		if err := q.ReincludeFirmwareRolloutDevices(ctx, sqlc.ReincludeFirmwareRolloutDevicesParams{RolloutID: r.ID, DeviceIds: returners}); err != nil {
			return nil, fleeterror.NewInternalErrorf("reinclude rollout devices: %w", err)
		}
		changed = true
	}
	joiners, err := q.ListReleaseChannelMismatchedMembers(ctx, s.mismatchedParams(rolloutSpec{
		OrgID: r.OrgID, ChannelID: r.ChannelID, Pair: PairKey{Manufacturer: r.Manufacturer, Model: r.Model},
		FirmwareVersion: r.FirmwareVersion, FirmwareChecksum: r.FirmwareChecksum,
		AssignmentGeneration: r.AssignmentGeneration,
	}, r.ID))
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list late joiners: %w", err)
	}
	if len(joiners) > 0 {
		ids := make([]int64, len(joiners))
		for i, j := range joiners {
			ids[i] = j.DeviceID
		}
		if err := q.AppendFirmwareRolloutDevices(ctx, sqlc.AppendFirmwareRolloutDevicesParams{RolloutID: r.ID, DeviceIds: ids}); err != nil {
			return nil, fleeterror.NewInternalErrorf("append rollout devices: %w", err)
		}
		changed = true
	}
	if !changed {
		return targets, nil
	}
	return s.listTargets(ctx, r)
}

// excludeDepartedTargets can also run immediately before dispatch: it records
// departures without re-including returners that still need convergence checks
// during the next preparation pass.
func (s *Service) excludeDepartedTargets(ctx context.Context, r sqlc.FirmwareRollout, targets []target) ([]target, error) {
	var leavers []int64
	for _, t := range targets {
		if !t.IsChannelMember && !t.excluded() {
			leavers = append(leavers, t.DeviceID)
		}
	}
	if len(leavers) == 0 {
		return targets, nil
	}
	if err := s.store.GetQueries(ctx).ExcludeFirmwareRolloutDevices(ctx, sqlc.ExcludeFirmwareRolloutDevicesParams{RolloutID: r.ID, DeviceIds: leavers}); err != nil {
		return nil, fleeterror.NewInternalErrorf("exclude rollout devices: %w", err)
	}
	return s.listTargets(ctx, r)
}

// recordProvenance writes managed-deployment provenance for targets that were
// dispatched to and now report the rollout's version, so they can verify and
// the pair's on-target count reflects them. A target with a foreign firmware
// command still outstanding waits: its report predates that command. The write
// compares the observed provenance atomically so a stale read cannot replace a
// concurrent deployment. Returns refreshed targets after attempting the write.
// Establishing missing provenance, replacing same-version provenance or accepting
// a reported pair change requires the exact dispatch's durable successful result:
// another artifact may report the same version, so queuing or failing the assigned
// update cannot prove that artifact was installed, even when the pair is unchanged.
func (s *Service) recordProvenance(ctx context.Context, r sqlc.FirmwareRollout, targets []target) ([]target, error) {
	return s.recordDeploymentProvenance(ctx, r, targets, false)
}

// recordTerminalProvenance observes completed commands from retained rollout
// history without reopening its lifecycle. Historical exclusions and halts do
// not erase a command's result, but only a successful current dispatch can
// establish its provenance; a version change by itself is insufficient.
func (s *Service) recordTerminalProvenance(ctx context.Context, r sqlc.FirmwareRollout, targets []target) ([]target, error) {
	return s.recordDeploymentProvenance(ctx, r, targets, true)
}

func (s *Service) recordDeploymentProvenance(ctx context.Context, r sqlc.FirmwareRollout, targets []target, terminal bool) ([]target, error) {
	var deployed []int64
	var expectedPresent []bool
	var expectedDeployedAt []time.Time
	var expectedChecksums []string
	for _, t := range targets {
		// Completion leaves an empty, timestamped row even when no previous
		// deployment exists. It invalidates stale observations atomically, but
		// provides no prior-version evidence; only current successful dispatch
		// evidence can replace that marker.
		knownDeployment := t.LastDeployedAt.Valid && t.LastDeployedFirmwareChecksum != "" && t.LastDeployedFirmwareVersion != ""
		if (terminal || (!t.excluded() && !t.halted())) && t.LastDispatchedAt.Valid && t.reportsTarget(r) && !t.foreignCommand &&
			(t.LastDispatchSucceeded || (!terminal && t.InScope.Valid && t.InScope.Bool && knownDeployment &&
				t.LastDeployedFirmwareVersion != r.FirmwareVersion)) &&
			(!t.LastDeployedAt.Valid || t.LastDeployedFirmwareChecksum == "" || !t.LastDeployedAt.Time.After(t.LastDispatchedAt.Time)) &&
			t.LastDeployedFirmwareChecksum != r.FirmwareChecksum {
			deployed = append(deployed, t.DeviceID)
			expectedPresent = append(expectedPresent, t.LastDeployedAt.Valid)
			expectedDeployedAt = append(expectedDeployedAt, t.LastDeployedAt.Time)
			expectedChecksums = append(expectedChecksums, t.LastDeployedFirmwareChecksum)
		}
	}
	if len(deployed) == 0 {
		return targets, nil
	}
	if err := s.store.GetQueries(ctx).RecordFirmwareDeployment(ctx, sqlc.RecordFirmwareDeploymentParams{
		DeviceIds: deployed, FirmwareChecksum: r.FirmwareChecksum, FirmwareVersion: r.FirmwareVersion,
		RolloutID:                 sql.NullInt64{Int64: r.ID, Valid: true},
		ExpectedDeploymentPresent: expectedPresent,
		ExpectedDeployedAts:       expectedDeployedAt,
		ExpectedFirmwareChecksums: expectedChecksums,
	}); err != nil {
		return nil, fleeterror.NewInternalErrorf("record firmware deployment: %w", err)
	}
	return s.listTargets(ctx, r)
}

// recordConvergence latches DONE only when all live criteria are met. Firmware
// drift reopens active work; later health changes alone do not rewrite a phase.
func (s *Service) recordConvergence(ctx context.Context, r sqlc.FirmwareRollout, targets []target) ([]target, error) {
	var verified, drifted []int64
	for _, t := range targets {
		if t.excluded() || t.halted() {
			continue
		}
		if t.VerifiedAt.Valid {
			if !t.reportsTarget(r) || t.LastDeployedFirmwareChecksum != r.FirmwareChecksum || t.foreignCommand {
				drifted = append(drifted, t.DeviceID)
			}
		} else if t.meetsConvergence(r) {
			verified = append(verified, t.DeviceID)
		}
	}
	q := s.store.GetQueries(ctx)
	if len(drifted) > 0 {
		if err := q.UnverifyFirmwareRolloutDevices(ctx, sqlc.UnverifyFirmwareRolloutDevicesParams{RolloutID: r.ID, DeviceIds: drifted}); err != nil {
			return nil, fleeterror.NewInternalErrorf("reopen rollout convergence: %w", err)
		}
	}
	if len(verified) > 0 {
		if err := q.MarkFirmwareRolloutDevicesVerified(ctx, sqlc.MarkFirmwareRolloutDevicesVerifiedParams{RolloutID: r.ID, DeviceIds: verified}); err != nil {
			return nil, fleeterror.NewInternalErrorf("verify rollout devices: %w", err)
		}
	}
	if len(drifted)+len(verified) == 0 {
		return targets, nil
	}
	return s.listTargets(ctx, r)
}

func allSettled(scope []target, r sqlc.FirmwareRollout) bool {
	for _, t := range scope {
		if !t.settled(r) {
			return false
		}
	}
	return true
}

func anyFailed(scope []target) bool { return countFailed(scope) > 0 }

func countFailed(scope []target) int {
	n := 0
	for _, t := range scope {
		if t.failed() {
			n++
		}
	}
	return n
}

// dispatchHoldReason exposes actionable reasons dispatch cannot start. It is a
// read-only view of the same eligibility and live channel budget used by
// dispatch; already-sent commands and retry cooldowns are normal progress.
func (s *Service) dispatchHoldReason(ctx context.Context, r sqlc.FirmwareRollout, targets []target, firmwareAvailable bool) (string, error) {
	if r.Status != StatusActive || r.PausedAt.Valid || (r.Stage != StageBatch && r.Stage != StageRest) {
		return "", nil
	}
	cutoff := s.now().Add(-resendInterval)
	due := false
	for _, t := range reviewScope(r, targets) {
		if t.settled(r) || len(t.PendingFirmwareChecksums) > 0 || len(t.PendingLegacyFirmwareFileIds) > 0 ||
			(t.LastSentAt.Valid && !t.LastSentAt.Time.Before(cutoff)) ||
			!t.InScope.Valid || !t.InScope.Bool || t.Attempts >= MaxAttempts {
			continue
		}
		due = true
		break
	}
	if !due {
		return "", nil
	}
	if !firmwareAvailable {
		return holdArtifactMissing, nil
	}
	q := s.store.GetQueries(ctx)
	channel, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: r.ChannelID, OrgID: r.OrgID})
	if err != nil {
		return "", channelLookupError(r.ChannelID, err)
	}
	if channel.MaxConcurrentOffline <= 0 {
		return "", nil
	}
	slots, err := q.ListFirmwareRolloutOfflineSlots(ctx, r.ChannelID)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("load rollout offline slots: %w", err)
	}
	if len(slots) >= int(channel.MaxConcurrentOffline) {
		return fmt.Sprintf("Waiting for offline capacity: %d of %d slots are in use. Wait for miners to recover or change the channel's offline limit.", len(slots), channel.MaxConcurrentOffline), nil
	}
	return "", nil
}

// dispatchUpdates sends the firmware update to every mismatched target in
// scope that is due (never sent, or not re-sent within resendInterval),
// failing miners whose attempts are exhausted first and honouring the
// channel-wide offline budget. Dispatch waits while no uploaded file carries
// the rollout's checksum. Returns how many miners it failed.
func (s *Service) dispatchUpdates(ctx context.Context, r sqlc.FirmwareRollout) (int, error) {
	var halted int
	err := s.tx.RunInTxNoRetry(ctx, func(ctx context.Context) error {
		q := s.store.GetQueries(ctx)
		// Match assignment and rollback lock ordering. Holding both locks
		// through enqueue serializes dispatch with assignment changes,
		// pause, cancel and other operator controls.
		channel, err := q.GetReleaseChannelForUpdate(ctx, sqlc.GetReleaseChannelForUpdateParams{
			ChannelID: r.ChannelID, OrgID: r.OrgID,
		})
		if err != nil {
			return channelLookupError(r.ChannelID, err)
		}
		budget, err := s.refreshOfflineBudget(ctx, r.ChannelID, channel.MaxConcurrentOffline)
		if err != nil {
			return err
		}
		current, _, err := s.lockRollout(ctx, r.OrgID, r.ID, 0)
		if err != nil {
			return err
		}
		if current.Status != StatusActive || current.PausedAt.Valid ||
			current.Stage != r.Stage || current.CurrentBatch != r.CurrentBatch || !current.StageChangedAt.Equal(r.StageChangedAt) ||
			(current.Stage != StageBatch && current.Stage != StageRest) {
			return nil
		}
		// Use fresh evidence even when another tick changed the revision.
		targets, err := s.listTargets(ctx, current)
		if err != nil {
			return err
		}
		// A scope edit can commit after preparation. Persist departures under
		// the channel lock before classifying hardware incompatibility. Leave
		// returners and joiners for preparation, which also checks convergence.
		targets, err = s.excludeDepartedTargets(ctx, current, targets)
		if err != nil {
			return err
		}
		halted, err = s.dispatchLockedUpdates(ctx, current, reviewScope(current, targets), budget)
		return err
	})
	return halted, err
}

// dispatchLockedUpdates runs with the channel and rollout locked until the
// command service has queued the batch and its dispatch evidence is saved.
func (s *Service) dispatchLockedUpdates(ctx context.Context, r sqlc.FirmwareRollout, scope []target, budget *offlineBudget) (int, error) {
	q := s.store.GetQueries(ctx)
	now := s.now()
	cutoff := now.Add(-resendInterval)

	var toHalt, incompatible, due []int64
	for _, t := range scope {
		if t.settled(r) {
			continue
		}
		sentRecently := t.LastSentAt.Valid && !t.LastSentAt.Time.Before(cutoff)
		switch {
		case len(t.PendingFirmwareChecksums) > 0 || len(t.PendingLegacyFirmwareFileIds) > 0:
			// Queue state, not retry age, determines when another command may
			// be sent or the prior attempt declared exhausted.
		case sentRecently:
		case !t.InScope.Valid || !t.InScope.Bool:
			incompatible = append(incompatible, t.DeviceID)
		case t.Attempts >= MaxAttempts:
			toHalt = append(toHalt, t.DeviceID)
		default:
			due = append(due, t.DeviceID)
		}
	}
	if len(incompatible) > 0 {
		if err := q.HaltFirmwareRolloutDevices(ctx, sqlc.HaltFirmwareRolloutDevicesParams{
			RolloutID: r.ID, DeviceIds: incompatible, HaltReason: HaltReasonFailed,
			LastError: "Reported manufacturer or model changed before the update verified; no further update was sent. Review this miner's identity and firmware compatibility before retrying.",
		}); err != nil {
			return 0, fleeterror.NewInternalErrorf("fail incompatible rollout devices: %w", err)
		}
	}
	halted := len(toHalt) + len(incompatible)
	if len(toHalt) > 0 {
		if err := q.HaltFirmwareRolloutDevices(ctx, sqlc.HaltFirmwareRolloutDevicesParams{
			RolloutID: r.ID, DeviceIds: toHalt, HaltReason: HaltReasonFailed,
			LastError: fmt.Sprintf("Did not report %s after %d update attempts", r.FirmwareVersion, MaxAttempts),
		}); err != nil {
			return 0, fleeterror.NewInternalErrorf("fail rollout devices: %w", err)
		}
		slog.Warn("rollout enforcement failed devices", "rollout_id", r.ID, "devices", len(toHalt))
	}
	if room := budget.room(); room >= 0 {
		// Bound this dispatch by the channel's current free capacity.
		if len(due) > room {
			due = due[:room]
		}
	}
	if len(due) == 0 {
		return halted, nil
	}
	_, ok := s.files.FindFirmwareFileIDByChecksum(r.FirmwareChecksum)
	if !ok {
		slog.Warn("rollout enforcement: firmware artifact not uploaded", "rollout_id", r.ID, "checksum", r.FirmwareChecksum)
		return halted, nil
	}
	assignment, err := q.GetReleaseChannelFirmware(ctx, sqlc.GetReleaseChannelFirmwareParams{
		ChannelID: r.ChannelID, Manufacturer: r.Manufacturer, Model: r.Model,
	})
	if err != nil {
		return halted, fleeterror.NewInternalErrorf("load firmware assignment: %w", err)
	}
	if assignment.AssignmentGeneration != r.AssignmentGeneration || assignment.FirmwareChecksum != r.FirmwareChecksum || assignment.FirmwareVersion != r.FirmwareVersion {
		return halted, nil // a concurrent assignment change superseded this rollout
	}

	identifiers := make([]string, 0, len(due))
	byID := map[int64]string{}
	for _, t := range scope {
		byID[t.DeviceID] = t.DeviceIdentifier
	}
	for _, id := range due {
		identifiers = append(identifiers, byID[id])
	}
	selector := &commandpb.DeviceSelector{
		SelectionType: &commandpb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: identifiers},
		},
	}
	result, err := s.commands.FirmwareUpdateArtifact(s.enforcementContext(ctx, r, assignment.AssignedBy), selector, r.FirmwareChecksum, files.FirmwareMetadata{
		TargetManufacturer: assignment.FirmwareTargetManufacturer,
		TargetModel:        assignment.FirmwareTargetModel,
		FirmwareVersion:    assignment.FirmwareVersion,
	})
	if err != nil {
		return halted, fleeterror.NewInternalErrorf("dispatch firmware update: %w", err)
	}
	for _, id := range due {
		budget.reserve(id)
	}
	slog.Info("rollout enforcement dispatched firmware updates",
		"rollout_id", r.ID, "channel_id", r.ChannelID, "manufacturer", r.Manufacturer, "model", r.Model, "stage", r.Stage,
		"dispatched", result.DispatchedCount, "skipped", len(result.Skipped))
	// Keep retry pacing for every attempt, but only actual dispatches may
	// authorize provenance when a device later reports the target version.
	// In particular, an identity change caught by command preflight cannot
	// turn a skipped attempt into evidence that this artifact was installed.
	dispatchedIdentifiers := make(map[string]bool, len(result.DispatchedDeviceIdentifiers))
	for _, identifier := range result.DispatchedDeviceIdentifiers {
		dispatchedIdentifiers[identifier] = true
	}
	var dispatched []int64
	for _, id := range due {
		if dispatchedIdentifiers[byID[id]] {
			dispatched = append(dispatched, id)
		}
	}
	if err := q.MarkFirmwareRolloutDevicesSent(ctx, sqlc.MarkFirmwareRolloutDevicesSentParams{
		RolloutID: r.ID, DeviceIds: due, DispatchedDeviceIds: dispatched, BatchUuid: result.BatchIdentifier,
	}); err != nil {
		return halted, fleeterror.NewInternalErrorf("mark rollout devices sent: %w", err)
	}
	return halted, nil
}

// enforcementContext supplies the assignment's persisted user owner for the
// command batch, while activity identifies background dispatch as a system
// action. No human identity is synthesized for the enforcement loop.
func (s *Service) enforcementContext(ctx context.Context, r sqlc.FirmwareRollout, ownerUserID int64) context.Context {
	return authn.SetInfo(ctx, &session.Info{
		SessionID:      rolloutActorName,
		UserID:         ownerUserID,
		OrganizationID: r.OrgID,
		Actor:          session.ActorRolloutEnforcement,
	})
}

// evaluate summarizes the scope's post-update health against baseline and
// decides whether the rollout may auto-continue. Failed miners, a set limit
// whose aggregate lacks the required sample coverage, and degraded evidence
// never advance a rollout on their own; excluded and skipped miners are
// neutral.
func (s *Service) evaluate(r sqlc.FirmwareRollout, scope []target) Evidence {
	ev := Evidence{DevicesTotal: int32(len(scope))} // #nosec G115 -- bounded by the member count
	var hash, power, efficiency, temp metricAggregate
	var offline, notHashing int
	for _, t := range scope {
		if t.excluded() {
			ev.Excluded++
			continue
		}
		if t.skipped() {
			ev.Skipped++
			continue
		}
		verified := t.verified(r)
		if verified {
			// DONE is historical progress. Releasing an automatic gate still
			// requires live recovery, including baseline hashing expectations.
			if !t.online() {
				offline++
			} else if !t.hashing() && (!t.BaselineAt.Valid || t.baselineHashing()) {
				notHashing++
			}
			ev.Verified++
			hash.add(t.BaselineHashRateHs, t.HashRateHs)
			power.add(t.BaselinePowerW, t.PowerW)
			efficiency.add(t.BaselineEfficiencyJh, t.EfficiencyJh)
			temp.add(t.BaselineTempC, t.TempC)
		}
		if t.failed() {
			ev.Failed++
		}
		if t.online() {
			ev.Online++
		}
		if t.hashing() {
			ev.Hashing++
		}
		if t.baselineHashing() {
			ev.BaselineHashing++
		}
		ev.NewErrors += t.ErrorsSinceBaseline
	}
	ev.HashRateHs = hash.sum()
	ev.PowerW = power.sum()
	ev.EfficiencyJh = efficiency.mean()
	ev.TempC = temp.mean()
	ev.HashrateChangePercent = percentChange(ev.HashRateHs.Metric)
	ev.EfficiencyChangePercent = percentChange(ev.EfficiencyJh.Metric)
	if ev.TempC.Baseline != nil && ev.TempC.Current != nil {
		delta := *ev.TempC.Current - *ev.TempC.Baseline
		ev.TemperatureChangeC = &delta
	}

	th := behaviorFromRollout(r).Thresholds
	// covered reports whether an aggregate samples enough verified miners
	// for its limit to be judged; the gate fails closed otherwise.
	covered := func(a Aggregate) bool {
		return a.SampledDevices > 0 && float64(a.SampledDevices) >= th.coverage()*float64(ev.Verified)
	}
	switch {
	case r.Status != StatusActive:
		return ev
	case r.PausedAt.Valid:
		ev.HoldReason = "Paused"
	case r.Stage == StageBatch:
		ev.HoldReason = ""
	case r.Stage == StageWaiting:
		ev.HoldReason = "Waiting before the next batch"
	case r.Stage == StageRest:
		ev.HoldReason = ""
	case !r.BehaviorSnapshot.AutoContinue:
		ev.HoldReason = "Manual review"
	case ev.Failed > 0:
		ev.HoldReason = fmt.Sprintf("%d miners failed to update", ev.Failed)
	case ev.Verified+ev.Excluded+ev.Skipped < ev.DevicesTotal:
		ev.HoldReason = fmt.Sprintf("%d of %d miners not yet verified", ev.DevicesTotal-ev.Verified-ev.Excluded-ev.Skipped, ev.DevicesTotal)
	case offline > 0:
		ev.HoldReason = fmt.Sprintf("Waiting for %d updated miners to come back online", offline)
	case notHashing > 0:
		ev.HoldReason = fmt.Sprintf("Waiting for %d updated miners to resume hashing", notHashing)
	case th.MaxHashrateDropPercent != nil && !covered(ev.HashRateHs):
		ev.HoldReason = coverageHold("hashrate", ev.HashRateHs, ev.Verified)
	case th.MaxHashrateDropPercent != nil && ev.HashrateChangePercent == nil:
		ev.HoldReason = "Hashrate change undefined: baseline hashrate was zero"
	case th.MaxHashrateDropPercent != nil && *ev.HashrateChangePercent < -*th.MaxHashrateDropPercent:
		ev.HoldReason = fmt.Sprintf("Hashrate down %.1f%% (limit %.0f%%)", -*ev.HashrateChangePercent, *th.MaxHashrateDropPercent)
	case th.MaxEfficiencyIncreasePercent != nil && !covered(ev.EfficiencyJh):
		ev.HoldReason = coverageHold("efficiency", ev.EfficiencyJh, ev.Verified)
	case th.MaxEfficiencyIncreasePercent != nil && ev.EfficiencyChangePercent == nil:
		ev.HoldReason = "Efficiency change undefined: baseline efficiency was zero"
	case th.MaxEfficiencyIncreasePercent != nil && *ev.EfficiencyChangePercent > *th.MaxEfficiencyIncreasePercent:
		ev.HoldReason = fmt.Sprintf("Efficiency worse by %.1f%% (limit %.0f%%)", *ev.EfficiencyChangePercent, *th.MaxEfficiencyIncreasePercent)
	case th.MaxTempIncreaseC != nil && !covered(ev.TempC):
		ev.HoldReason = coverageHold("temperature", ev.TempC, ev.Verified)
	case th.MaxTempIncreaseC != nil && *ev.TemperatureChangeC > *th.MaxTempIncreaseC:
		ev.HoldReason = fmt.Sprintf("Temperature up %.1f°C (limit %.0f°C)", *ev.TemperatureChangeC, *th.MaxTempIncreaseC)
	case th.MaxNewErrors != nil && ev.NewErrors > *th.MaxNewErrors:
		ev.HoldReason = fmt.Sprintf("%d new errors since the update (limit %d)", ev.NewErrors, *th.MaxNewErrors)
	default:
		remaining := time.Duration(r.BehaviorSnapshot.StabilizationSeconds)*time.Second - s.stageElapsed(r)
		if r.BehaviorSnapshot.StabilizationSeconds > 0 && remaining > 0 {
			ev.StabilizationRemainingSeconds = int32(math.Ceil(remaining.Seconds())) // #nosec G115 -- bounded by StabilizationSeconds, an int32
			ev.HoldReason = holdStabilizing
		} else {
			ev.ReadyToAdvance = true
		}
	}
	return ev
}

func coverageHold(metric string, a Aggregate, verified int32) string {
	return fmt.Sprintf("Recent %s sample covers %d of %d verified miners", metric, a.SampledDevices, verified)
}

// percentChange is nil when either half is missing or the baseline is zero,
// so an absent aggregate is never read as "no change".
func percentChange(m Metric) *float64 {
	if m.Baseline == nil || m.Current == nil || *m.Baseline == 0 {
		return nil
	}
	v := (*m.Current - *m.Baseline) / *m.Baseline * 100
	return &v
}

// metricAggregate folds per-device before/after samples, counting only
// devices that have both halves so the comparison is like for like.
type metricAggregate struct {
	baseline, current float64
	n                 int
}

func (a *metricAggregate) add(baseline, current sql.NullFloat64) {
	if !baseline.Valid || !current.Valid {
		return
	}
	a.baseline += baseline.Float64
	a.current += current.Float64
	a.n++
}

func (a *metricAggregate) sum() Aggregate {
	if a.n == 0 {
		return Aggregate{}
	}
	b, c := a.baseline, a.current
	return Aggregate{Metric: Metric{Baseline: &b, Current: &c}, SampledDevices: int32(a.n)} // #nosec G115 -- bounded by the member count
}

func (a *metricAggregate) mean() Aggregate {
	if a.n == 0 {
		return Aggregate{}
	}
	b, c := a.baseline/float64(a.n), a.current/float64(a.n)
	return Aggregate{Metric: Metric{Baseline: &b, Current: &c}, SampledDevices: int32(a.n)} // #nosec G115 -- bounded by the member count
}
