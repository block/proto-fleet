package rollout

import (
	"context"
	"database/sql"
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
)

const (
	rolloutActorName = "rollout-enforcement"

	// resendInterval is how long the enforcement loop waits before
	// re-issuing an update command to a miner that is still mismatched
	// (e.g. it was offline or the install failed).
	resendInterval = 10 * time.Minute

	holdStabilizing = "Stabilizing"
)

// EnforceTick runs one enforcement pass: it starts rollouts for assignments
// with mismatched members and drives every active rollout forward. Errors
// are logged per rollout so one bad rollout cannot stall the others.
func (s *Service) EnforceTick(ctx context.Context) {
	s.startNeededRollouts(ctx)
	active, err := s.store.Queries(ctx).ListActiveFirmwareRollouts(ctx)
	if err != nil {
		slog.Error("rollout enforcement: list active rollouts", "error", err)
		return
	}
	for _, row := range active {
		if err := s.enforceRollout(ctx, row.FirmwareRollout, row.ChannelName); err != nil {
			slog.Error("rollout enforcement", "rollout_id", row.FirmwareRollout.ID, "error", err)
		}
	}
}

// startNeededRollouts creates an all-at-once rollout for every assignment
// that has at least one mismatched, non-halted member and no active
// rollout: late joiners and miners that drifted off the assigned version.
// No operator is present to review a gate, so these never stage.
func (s *Service) startNeededRollouts(ctx context.Context) {
	needed, err := s.store.Queries(ctx).ListReleaseChannelFirmwareNeedingRollout(ctx)
	if err != nil {
		slog.Error("rollout enforcement: find assignments needing rollout", "error", err)
		return
	}
	for _, n := range needed {
		err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
			r, err := s.startRollout(ctx, rolloutSpec{
				OrgID: n.OrgID, ChannelID: n.ChannelID, Model: n.Model,
				FirmwareFileID: n.FirmwareFileID, FirmwareVersion: n.FirmwareVersion,
				// The assignment did not change, so "previous" is the
				// assignment itself; a rollback of a drift rollout is a no-op.
				PreviousFirmwareFileID: n.FirmwareFileID, PreviousFirmwareVersion: n.FirmwareVersion,
				CreatedBy: n.AssignedBy, Behavior: allAtOnce, AssignedAt: n.UpdatedAt,
			})
			if err != nil || r == nil {
				return err
			}
			s.logRolloutEvent(ctx, *r, "", EventRolloutStarted, true, map[string]any{"drift_correction": true})
			return nil
		})
		if err != nil {
			slog.Error("rollout enforcement: start rollout", "channel_id", n.ChannelID, "model", n.Model, "error", err)
		}
	}
}

// enforceRollout drives one active rollout forward according to its stage.
// Paused rollouts are left alone (membership is still synced so the view
// stays truthful). The batch stage updates only the current batch and parks
// the rollout at the review gate (or the between-batch wait) once the batch
// settles; the rest stage (and every all-at-once rollout) updates all
// remaining targets and finishes the rollout once every target settled.
func (s *Service) enforceRollout(ctx context.Context, r sqlc.FirmwareRollout, channelName string) error {
	targets, err := s.syncMembership(ctx, r)
	if err != nil {
		return err
	}
	if r.PausedAt.Valid {
		return nil
	}
	scope := reviewScope(r, targets)
	behavior := behaviorFromRollout(r)

	// Stages that update miners: dispatch first, and if that failed the last
	// outstanding miner, fall through to settle the stage in the same tick.
	if r.Stage == StageBatch || r.Stage == StageRest {
		if !allSettled(scope, r.FirmwareVersion) {
			halted, err := s.dispatchUpdates(ctx, r, scope)
			if err != nil || halted == 0 {
				return err
			}
			if targets, err = s.listTargets(ctx, r); err != nil {
				return err
			}
			if scope = reviewScope(r, targets); !allSettled(scope, r.FirmwareVersion) {
				return nil
			}
		}
	}

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
		return s.advance(ctx, &r, StageBatch)

	case StageAwaitingReview:
		if !r.AutoContinue {
			return nil
		}
		ev := s.evaluate(r, scope)
		if !ev.ReadyToAdvance {
			return nil
		}
		if err := s.advance(ctx, &r, StageAwaitingReview); err != nil {
			return err
		}
		s.logRolloutEvent(ctx, r, channelName, EventRolloutContinued, true, map[string]any{
			"auto_continued":          true,
			"hashrate_change_percent": ev.HashrateChangePercent,
		})
		return nil

	case StageWaiting:
		wait := time.Duration(behavior.WaitBetweenBatchesSeconds) * time.Second
		if s.now().Sub(r.StageChangedAt) < wait {
			return nil
		}
		return s.advance(ctx, &r, StageWaiting)

	default:
		status, event := StatusCompleted, EventRolloutCompleted
		if anyFailed(scope) {
			status, event = StatusCompletedWithFailures, EventRolloutCompletedWithFailures
		}
		n, err := s.store.Queries(ctx).FinishFirmwareRollout(ctx, sqlc.FinishFirmwareRolloutParams{RolloutID: r.ID, Status: status})
		if err != nil {
			return fleeterror.NewInternalErrorf("finish rollout: %v", err)
		}
		if n > 0 {
			r.Status = status
			s.logRolloutEvent(ctx, r, channelName, event, true, map[string]any{"failed": countFailed(scope)})
		}
		return nil
	}
}

// transition moves a rollout between stages of the same batch (batch ->
// awaiting_review / waiting); onDone runs only when this tick won the race.
func (s *Service) transition(ctx context.Context, r *sqlc.FirmwareRollout, from, to string, onDone func()) error {
	n, err := s.store.Queries(ctx).AdvanceFirmwareRolloutStage(ctx, sqlc.AdvanceFirmwareRolloutStageParams{
		RolloutID: r.ID, FromStage: from, Stage: to, CurrentBatch: r.CurrentBatch,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("transition rollout: %v", err)
	}
	if n > 0 {
		r.Stage, r.StageChangedAt = to, s.now()
		if onDone != nil {
			onDone()
		}
	}
	return nil
}

// syncMembership reconciles the rollout's target set with the channel's
// live membership: miners that left the scope are excluded, miners that
// came back are re-included, and mismatched members not yet in the rollout
// are appended as late joiners (updated in the rest stage). Returns the
// refreshed targets.
func (s *Service) syncMembership(ctx context.Context, r sqlc.FirmwareRollout) ([]target, error) {
	q := s.store.Queries(ctx)
	targets, err := s.listTargets(ctx, r)
	if err != nil {
		return nil, err
	}
	var leavers, returners []int64
	for _, t := range targets {
		inScope := t.InScope.Valid && t.InScope.Bool
		switch {
		case !inScope && !t.excluded():
			leavers = append(leavers, t.DeviceID)
		case inScope && t.excluded():
			returners = append(returners, t.DeviceID)
		}
	}
	changed := false
	if len(leavers) > 0 {
		if err := q.ExcludeFirmwareRolloutDevices(ctx, sqlc.ExcludeFirmwareRolloutDevicesParams{RolloutID: r.ID, DeviceIds: leavers}); err != nil {
			return nil, fleeterror.NewInternalErrorf("exclude rollout devices: %v", err)
		}
		changed = true
	}
	if len(returners) > 0 {
		if err := s.snapshot(ctx, r.ID, returners, sql.NullInt32{}, -1); err != nil {
			return nil, err
		}
		changed = true
	}
	joiners, err := q.ListReleaseChannelMismatchedMembers(ctx, sqlc.ListReleaseChannelMismatchedMembersParams{
		ChannelID: r.ChannelID, Model: r.Model, FirmwareVersion: r.FirmwareVersion,
		RolloutID: r.ID, AssignedAt: r.CreatedAt,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list late joiners: %v", err)
	}
	if len(joiners) > 0 {
		ids := make([]int64, len(joiners))
		for i, j := range joiners {
			ids[i] = j.DeviceID
		}
		if err := s.snapshot(ctx, r.ID, ids, sql.NullInt32{}, -1); err != nil {
			return nil, err
		}
		changed = true
	}
	if !changed {
		return targets, nil
	}
	return s.listTargets(ctx, r)
}

func allSettled(scope []target, version string) bool {
	for _, t := range scope {
		if !t.settled(version) {
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

// dispatchUpdates sends the firmware update to every mismatched target in
// scope that is due (never sent, or not re-sent within resendInterval),
// failing miners whose attempts are exhausted first and honouring the
// rollout's ceiling on miners mid-update. Returns how many miners it failed.
func (s *Service) dispatchUpdates(ctx context.Context, r sqlc.FirmwareRollout, scope []target) (int, error) {
	q := s.store.Queries(ctx)
	now := s.now()
	cutoff := now.Add(-resendInterval)

	var toHalt, due []int64
	inFlight := 0
	for _, t := range scope {
		if t.settled(r.FirmwareVersion) {
			continue
		}
		sentRecently := t.LastSentAt.Valid && !t.LastSentAt.Time.Before(cutoff)
		switch {
		case sentRecently:
			inFlight++
		case t.Attempts >= MaxAttempts:
			toHalt = append(toHalt, t.DeviceID)
		default:
			due = append(due, t.DeviceID)
		}
	}
	if len(toHalt) > 0 {
		if err := q.HaltFirmwareRolloutDevices(ctx, sqlc.HaltFirmwareRolloutDevicesParams{
			RolloutID: r.ID, DeviceIds: toHalt, HaltReason: HaltReasonFailed,
			LastError: fmt.Sprintf("Did not report %s after %d update attempts", r.FirmwareVersion, MaxAttempts),
		}); err != nil {
			return 0, fleeterror.NewInternalErrorf("fail rollout devices: %v", err)
		}
		slog.Warn("rollout enforcement failed devices", "rollout_id", r.ID, "devices", len(toHalt))
	}
	if r.MaxConcurrentOffline > 0 {
		room := int(r.MaxConcurrentOffline) - inFlight
		if room < 0 {
			room = 0
		}
		if len(due) > room {
			due = due[:room]
		}
	}
	if len(due) == 0 {
		return len(toHalt), nil
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
	result, err := s.commands.FirmwareUpdate(s.enforcementContext(ctx, r), selector, r.FirmwareFileID)
	if err != nil {
		return len(toHalt), fleeterror.NewInternalErrorf("dispatch firmware update: %v", err)
	}
	slog.Info("rollout enforcement dispatched firmware updates",
		"rollout_id", r.ID, "channel_id", r.ChannelID, "model", r.Model, "stage", r.Stage,
		"dispatched", result.DispatchedCount, "skipped", len(result.Skipped))
	// Mark every attempted device (dispatched or preflight-skipped) so the
	// next attempt waits for resendInterval instead of retrying each tick.
	if err := q.MarkFirmwareRolloutDevicesSent(ctx, sqlc.MarkFirmwareRolloutDevicesSentParams{RolloutID: r.ID, DeviceIds: due}); err != nil {
		return len(toHalt), fleeterror.NewInternalErrorf("mark rollout devices sent: %v", err)
	}
	return len(toHalt), nil
}

// enforcementContext synthesizes a session for command dispatch, attributed
// to the user who assigned the firmware.
func (s *Service) enforcementContext(ctx context.Context, r sqlc.FirmwareRollout) context.Context {
	return authn.SetInfo(ctx, &session.Info{
		SessionID:      rolloutActorName,
		UserID:         r.CreatedBy,
		OrganizationID: r.OrgID,
		ExternalUserID: rolloutActorName,
		Username:       rolloutActorName,
		Actor:          session.ActorRolloutEnforcement,
	})
}

// evaluate summarizes the scope's post-update health against baseline and
// decides whether the rollout may auto-continue. Failed miners, missing
// samples for a checked threshold, and degraded evidence never advance a
// rollout on their own.
func (s *Service) evaluate(r sqlc.FirmwareRollout, scope []target) Evidence {
	ev := Evidence{DevicesTotal: int32(len(scope))} // #nosec G115 -- bounded by the member count
	var hash, power, efficiency, temp metricAggregate
	for _, t := range scope {
		hash.add(t.BaselineHashRateHs, t.HashRateHs)
		power.add(t.BaselinePowerW, t.PowerW)
		efficiency.add(t.BaselineEfficiencyJh, t.EfficiencyJh)
		temp.add(t.BaselineTempC, t.TempC)
		if t.verified(r.FirmwareVersion) {
			ev.Verified++
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
		if t.BaselineOpenErrors.Valid && t.OpenErrors > t.BaselineOpenErrors.Int32 {
			ev.NewErrors += t.OpenErrors - t.BaselineOpenErrors.Int32
		}
	}
	ev.HashRateHs = hash.sum()
	ev.PowerW = power.sum()
	ev.EfficiencyJh = efficiency.mean()
	ev.TempC = temp.mean()
	ev.HashrateChangePercent = percentChange(ev.HashRateHs)
	ev.EfficiencyChangePercent = percentChange(ev.EfficiencyJh)
	if ev.TempC.Baseline != nil && ev.TempC.Current != nil {
		ev.TemperatureChangeC = *ev.TempC.Current - *ev.TempC.Baseline
	}

	th := behaviorFromRollout(r).Thresholds
	switch {
	case r.Status != StatusActive:
		return ev
	case r.PausedAt.Valid:
		ev.HoldReason = "Paused"
	case r.Stage == StageBatch:
		ev.HoldReason = "Batch in progress"
	case r.Stage == StageWaiting:
		ev.HoldReason = "Waiting before the next batch"
	case r.Stage == StageRest:
		ev.HoldReason = ""
	case !r.AutoContinue:
		ev.HoldReason = "Manual review"
	case ev.Failed > 0:
		ev.HoldReason = fmt.Sprintf("%d miners failed to update", ev.Failed)
	case ev.Verified < ev.DevicesTotal:
		ev.HoldReason = fmt.Sprintf("%d of %d miners not yet verified", ev.DevicesTotal-ev.Verified, ev.DevicesTotal)
	case th.MaxHashrateDropPercent != nil && hash.missing > 0:
		ev.HoldReason = fmt.Sprintf("No recent hashrate sample for %d miners", hash.missing)
	case th.MaxHashrateDropPercent != nil && hash.n > 0 && ev.HashrateChangePercent < -*th.MaxHashrateDropPercent:
		ev.HoldReason = fmt.Sprintf("Hashrate down %.1f%% (limit %.0f%%)", -ev.HashrateChangePercent, *th.MaxHashrateDropPercent)
	case th.MaxEfficiencyIncreasePercent != nil && efficiency.missing > 0:
		ev.HoldReason = fmt.Sprintf("No recent efficiency sample for %d miners", efficiency.missing)
	case th.MaxEfficiencyIncreasePercent != nil && efficiency.n > 0 && ev.EfficiencyChangePercent > *th.MaxEfficiencyIncreasePercent:
		ev.HoldReason = fmt.Sprintf("Efficiency worse by %.1f%% (limit %.0f%%)", ev.EfficiencyChangePercent, *th.MaxEfficiencyIncreasePercent)
	case th.MaxTempIncreaseC != nil && temp.missing > 0:
		ev.HoldReason = fmt.Sprintf("No recent temperature sample for %d miners", temp.missing)
	case th.MaxTempIncreaseC != nil && temp.n > 0 && ev.TemperatureChangeC > *th.MaxTempIncreaseC:
		ev.HoldReason = fmt.Sprintf("Temperature up %.1f°C (limit %.0f°C)", ev.TemperatureChangeC, *th.MaxTempIncreaseC)
	case th.MaxNewErrors != nil && ev.NewErrors > *th.MaxNewErrors:
		ev.HoldReason = fmt.Sprintf("%d new errors since the update (limit %d)", ev.NewErrors, *th.MaxNewErrors)
	default:
		remaining := time.Duration(r.StabilizationSeconds)*time.Second - s.now().Sub(r.StageChangedAt)
		if r.StabilizationSeconds > 0 && remaining > 0 {
			ev.StabilizationRemainingSeconds = int32(math.Ceil(remaining.Seconds())) // #nosec G115 -- bounded by StabilizationSeconds, an int32
			ev.HoldReason = holdStabilizing
		} else {
			ev.ReadyToAdvance = true
		}
	}
	return ev
}

func percentChange(m Metric) float64 {
	if m.Baseline == nil || m.Current == nil || *m.Baseline == 0 {
		return 0
	}
	return (*m.Current - *m.Baseline) / *m.Baseline * 100
}

// metricAggregate folds per-device before/after samples, counting only
// devices that have both halves so the comparison is like for like, and
// remembering how many baselined devices lack a current sample.
type metricAggregate struct {
	baseline, current float64
	n, missing        int
}

func (a *metricAggregate) add(baseline, current sql.NullFloat64) {
	if !baseline.Valid {
		return
	}
	if !current.Valid {
		a.missing++
		return
	}
	a.baseline += baseline.Float64
	a.current += current.Float64
	a.n++
}

func (a *metricAggregate) sum() Metric {
	if a.n == 0 {
		return Metric{}
	}
	b, c := a.baseline, a.current
	return Metric{Baseline: &b, Current: &c}
}

func (a *metricAggregate) mean() Metric {
	if a.n == 0 {
		return Metric{}
	}
	b, c := a.baseline/float64(a.n), a.current/float64(a.n)
	return Metric{Baseline: &b, Current: &c}
}
