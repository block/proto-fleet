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
	// holdArtifactMissing is the hold reason while no uploaded file carries
	// the rollout's checksum.
	holdArtifactMissing = "Firmware file not uploaded"
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
	// Reconcile every rollout's targets first so the channel-wide offline
	// budgets see verified miners as settled before anything is dispatched.
	prepared := make([][]target, len(active))
	for i, row := range active {
		targets, err := s.prepareRollout(ctx, row.FirmwareRollout)
		if err != nil {
			slog.Error("rollout enforcement: reconcile targets", "rollout_id", row.FirmwareRollout.ID, "error", err)
			continue
		}
		prepared[i] = targets
	}
	budgets := s.offlineBudgets(active, prepared)
	for i, row := range active {
		if prepared[i] == nil {
			continue
		}
		if err := s.enforceRollout(ctx, row.FirmwareRollout, row.ChannelName, prepared[i], budgets[row.FirmwareRollout.ChannelID]); err != nil {
			slog.Error("rollout enforcement", "rollout_id", row.FirmwareRollout.ID, "error", err)
		}
	}
}

// prepareRollout syncs a rollout's target set with channel membership and
// records provenance for targets that verified since the last tick.
func (s *Service) prepareRollout(ctx context.Context, r sqlc.FirmwareRollout) ([]target, error) {
	targets, err := s.syncMembership(ctx, r)
	if err != nil {
		return nil, err
	}
	targets, err = s.recordProvenance(ctx, r, targets)
	if err != nil {
		return nil, err
	}
	if targets == nil {
		targets = []target{}
	}
	return targets, nil
}

// offlineBudget is a channel's live max_concurrent_offline and the slots its
// active rollouts' targets hold right now. Each distinct target holds at most
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

// offlineBudgets computes every channel's budget from all its active
// rollouts, so rollouts started together share it rather than each getting
// their own.
func (s *Service) offlineBudgets(active []sqlc.ListActiveFirmwareRolloutsRow, prepared [][]target) map[int64]*offlineBudget {
	budgets := map[int64]*offlineBudget{}
	cutoff := s.now().Add(-resendInterval)
	for i, row := range active {
		if row.ChannelMaxConcurrentOffline <= 0 || prepared[i] == nil {
			continue
		}
		b, ok := budgets[row.FirmwareRollout.ChannelID]
		if !ok {
			b = &offlineBudget{limit: row.ChannelMaxConcurrentOffline, held: map[int64]bool{}}
			budgets[row.FirmwareRollout.ChannelID] = b
		}
		for _, t := range prepared[i] {
			if t.excluded() {
				continue
			}
			outstanding := t.LastSentAt.Valid && !t.LastSentAt.Time.Before(cutoff) && !t.settled(row.FirmwareRollout)
			if !t.online() || outstanding {
				b.held[t.DeviceID] = true
			}
		}
	}
	return budgets
}

// startNeededRollouts creates an all-at-once rollout for every assigned pair
// that has at least one mismatched, unsuppressed member and no active
// rollout: late joiners, re-entries and miners that drifted. No operator is
// present to review a gate, so these never stage. The rollout carries the
// pair's generation and inherits the lineage of the generation's most recent
// rollout.
func (s *Service) startNeededRollouts(ctx context.Context) {
	needed, err := s.store.Queries(ctx).ListReleaseChannelFirmwareNeedingRollout(ctx)
	if err != nil {
		slog.Error("rollout enforcement: find assignments needing rollout", "error", err)
		return
	}
	for _, n := range needed {
		err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
			q := s.store.Queries(ctx)
			spec := rolloutSpec{
				OrgID: n.OrgID, ChannelID: n.ChannelID, Pair: PairKey{Manufacturer: n.Manufacturer, Model: n.Model},
				FirmwareChecksum: n.FirmwareChecksum, FirmwareVersion: n.FirmwareVersion,
				AssignmentGeneration: n.AssignmentGeneration, Actor: SystemActor, Behavior: allAtOnce,
			}
			latest, err := q.GetLatestFirmwareRolloutForPair(ctx, sqlc.GetLatestFirmwareRolloutForPairParams{
				ChannelID: n.ChannelID, Manufacturer: n.Manufacturer, Model: n.Model, AssignmentGeneration: n.AssignmentGeneration,
			})
			if err == nil {
				spec.PreviousFirmwareChecksum, spec.PreviousFirmwareVersion = latest.PreviousFirmwareChecksum, latest.PreviousFirmwareVersion
			}
			r, err := s.startRollout(ctx, spec)
			if err != nil || r == nil {
				return err
			}
			s.logRolloutEvent(ctx, *r, "", EventRolloutStarted, true, map[string]any{"reconciliation": true})
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
func (s *Service) enforceRollout(ctx context.Context, r sqlc.FirmwareRollout, channelName string, targets []target, budget *offlineBudget) error {
	if r.PausedAt.Valid {
		return nil
	}
	scope := reviewScope(r, targets)
	behavior := behaviorFromRollout(r)

	// Stages that update miners: dispatch first, and if that failed the last
	// outstanding miner, fall through to settle the stage in the same tick.
	if r.Stage == StageBatch || r.Stage == StageRest {
		if !allSettled(scope, r) {
			halted, err := s.dispatchUpdates(ctx, r, scope, budget)
			if err != nil || halted == 0 {
				return err
			}
			targets, err = s.listTargets(ctx, r)
			if err != nil {
				return err
			}
			if scope = reviewScope(r, targets); !allSettled(scope, r) {
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
		return s.advance(ctx, &r, StageBatch, nil)

	case StageAwaitingReview:
		if !r.AutoContinue {
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
		if s.now().Sub(r.StageChangedAt) < wait {
			return nil
		}
		return s.advance(ctx, &r, StageWaiting, nil)

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
		ChannelID: r.ChannelID, Manufacturer: r.Manufacturer, Model: r.Model,
		FirmwareVersion: r.FirmwareVersion, FirmwareChecksum: r.FirmwareChecksum,
		AssignmentGeneration: r.AssignmentGeneration, RolloutID: r.ID,
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

// recordProvenance writes managed-deployment provenance for targets that were
// dispatched to and now report the rollout's version, so they can verify and
// the pair's on-target count reflects them. Returns the refreshed targets
// when anything changed.
func (s *Service) recordProvenance(ctx context.Context, r sqlc.FirmwareRollout, targets []target) ([]target, error) {
	var deployed []int64
	for _, t := range targets {
		if !t.excluded() && t.Attempts > 0 && t.reportsTarget(r) && t.LastDeployedFirmwareChecksum != r.FirmwareChecksum {
			deployed = append(deployed, t.DeviceID)
		}
	}
	if len(deployed) == 0 {
		return targets, nil
	}
	if err := s.store.Queries(ctx).RecordFirmwareDeployment(ctx, sqlc.RecordFirmwareDeploymentParams{
		DeviceIds: deployed, FirmwareChecksum: r.FirmwareChecksum, FirmwareVersion: r.FirmwareVersion,
		RolloutID: sql.NullInt64{Int64: r.ID, Valid: true},
	}); err != nil {
		return nil, fleeterror.NewInternalErrorf("record firmware deployment: %v", err)
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

// dispatchUpdates sends the firmware update to every mismatched target in
// scope that is due (never sent, or not re-sent within resendInterval),
// failing miners whose attempts are exhausted first and honouring the
// channel-wide offline budget. Dispatch waits while no uploaded file carries
// the rollout's checksum. Returns how many miners it failed.
func (s *Service) dispatchUpdates(ctx context.Context, r sqlc.FirmwareRollout, scope []target, budget *offlineBudget) (int, error) {
	q := s.store.Queries(ctx)
	now := s.now()
	cutoff := now.Add(-resendInterval)

	var toHalt, due []int64
	for _, t := range scope {
		if t.settled(r) {
			continue
		}
		sentRecently := t.LastSentAt.Valid && !t.LastSentAt.Time.Before(cutoff)
		switch {
		case sentRecently:
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
	if room := budget.room(); room >= 0 {
		// Targets already holding a slot (offline, or outstanding) are not
		// due, so every due target needs a free slot.
		if len(due) > room {
			due = due[:room]
		}
	}
	if len(due) == 0 {
		return len(toHalt), nil
	}
	fileID, ok := s.files.FindFirmwareFileIDByChecksum(r.FirmwareChecksum)
	if !ok {
		slog.Warn("rollout enforcement: firmware artifact not uploaded", "rollout_id", r.ID, "checksum", r.FirmwareChecksum)
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
	result, err := s.commands.FirmwareUpdate(s.enforcementContext(ctx, r), selector, fileID)
	if err != nil {
		return len(toHalt), fleeterror.NewInternalErrorf("dispatch firmware update: %v", err)
	}
	for _, id := range due {
		budget.reserve(id)
	}
	slog.Info("rollout enforcement dispatched firmware updates",
		"rollout_id", r.ID, "channel_id", r.ChannelID, "manufacturer", r.Manufacturer, "model", r.Model, "stage", r.Stage,
		"dispatched", result.DispatchedCount, "skipped", len(result.Skipped))
	// Mark every attempted device (dispatched or preflight-skipped) so the
	// next attempt waits for resendInterval instead of retrying each tick.
	if err := q.MarkFirmwareRolloutDevicesSent(ctx, sqlc.MarkFirmwareRolloutDevicesSentParams{RolloutID: r.ID, DeviceIds: due}); err != nil {
		return len(toHalt), fleeterror.NewInternalErrorf("mark rollout devices sent: %v", err)
	}
	return len(toHalt), nil
}

// enforcementContext synthesizes a session for command dispatch, attributed
// to the actor who started the rollout.
func (s *Service) enforcementContext(ctx context.Context, r sqlc.FirmwareRollout) context.Context {
	return authn.SetInfo(ctx, &session.Info{
		SessionID:      rolloutActorName,
		UserID:         r.StartedByID,
		OrganizationID: r.OrgID,
		ExternalUserID: rolloutActorName,
		Username:       rolloutActorName,
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
	for _, t := range scope {
		if t.skipped() {
			ev.Skipped++
			continue
		}
		verified := t.verified(r)
		if verified {
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
		if t.BaselineOpenErrors.Valid && t.OpenErrors > t.BaselineOpenErrors.Int32 {
			ev.NewErrors += t.OpenErrors - t.BaselineOpenErrors.Int32
		}
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
		ev.HoldReason = "Batch in progress"
	case r.Stage == StageWaiting:
		ev.HoldReason = "Waiting before the next batch"
	case r.Stage == StageRest:
		ev.HoldReason = ""
	case !r.AutoContinue:
		ev.HoldReason = "Manual review"
	case ev.Failed > 0:
		ev.HoldReason = fmt.Sprintf("%d miners failed to update", ev.Failed)
	case ev.Verified+ev.Skipped < ev.DevicesTotal:
		ev.HoldReason = fmt.Sprintf("%d of %d miners not yet verified", ev.DevicesTotal-ev.Verified-ev.Skipped, ev.DevicesTotal)
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
