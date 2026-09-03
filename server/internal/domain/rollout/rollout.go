package rollout

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	// StatusActive marks a rollout that is still enforcing its firmware.
	StatusActive = "active"
	// StatusCompleted marks a rollout whose targets are all verified.
	StatusCompleted = "completed"
	// StatusCompletedWithFailures marks a rollout whose targets all settled
	// but some failed.
	StatusCompletedWithFailures = "completed_with_failures"
	// StatusCanceled marks a rollout that ended before completion.
	StatusCanceled = "canceled"

	// CancelReasonSuperseded: a newer assignment replaced it.
	CancelReasonSuperseded = "superseded"
	// CancelReasonCanceledRemaining: an operator canceled the remaining updates.
	CancelReasonCanceledRemaining = "canceled_remaining"
	// CancelReasonRolledBack: an operator rolled the model back.
	CancelReasonRolledBack = "rolled_back"
	// CancelReasonCleared: the model's assignment was cleared.
	CancelReasonCleared = "cleared"

	// StageBatch: only the current batch is being updated.
	StageBatch = "batch"
	// StageAwaitingReview: the current batch is done; enforcement holds
	// until the rollout is continued (manually or by auto-continue).
	StageAwaitingReview = "awaiting_review"
	// StageWaiting: the current batch is done; the next starts after the
	// configured wait.
	StageWaiting = "waiting"
	// StageRest: all remaining targets are being updated (the only stage of
	// all-at-once rollouts).
	StageRest = "rest"

	// Derived operator-facing states.
	StateInProgress            = "in_progress"
	StateStabilizingTelemetry  = "stabilizing_telemetry"
	StatePausedAtPilotGate     = "paused_at_pilot_gate"
	StatePausedAtBatchReview   = "paused_at_batch_review"
	StatePaused                = "paused"
	StateCompleted             = "completed"
	StateCompletedWithFailures = "completed_with_failures"
	StateCanceled              = "canceled"

	// Per-miner phases.
	PhaseQueued     = "queued"
	PhaseInProgress = "in_progress"
	PhaseRetrying   = "retrying"
	PhaseDone       = "done"
	PhaseFailed     = "failed"
	PhaseExcluded   = "excluded"

	// HaltReasonFailed: attempts exhausted without the miner verifying.
	HaltReasonFailed = "failed"
	// HaltReasonCanceled: the rollout was canceled before the miner updated.
	HaltReasonCanceled = "canceled"

	// Activity event types.
	EventRolloutStarted               = "rollout_started"
	EventRolloutReviewReady           = "rollout_review_ready"
	EventRolloutContinued             = "rollout_continued"
	EventRolloutPaused                = "rollout_paused"
	EventRolloutResumed               = "rollout_resumed"
	EventRolloutCanceled              = "rollout_canceled"
	EventRolloutCompleted             = "rollout_completed"
	EventRolloutCompletedWithFailures = "rollout_completed_with_failures"
	EventRolloutRetried               = "rollout_retried"

	// MaxAttempts is how many update commands a miner gets before it is
	// failed. With resendInterval that is roughly half an hour.
	MaxAttempts = 3
)

// Metric is one telemetry reading before the update (baseline) and now; a
// nil half means no sample was available.
type Metric struct {
	Baseline *float64
	Current  *float64
}

func metricFrom(baseline, current sql.NullFloat64) Metric {
	return Metric{Baseline: nullFloat(baseline), Current: nullFloat(current)}
}

// RolloutDevice is the live progress and health of one miner within a
// rollout, alongside the baseline captured when it was snapshotted.
type RolloutDevice struct {
	DeviceID         int64
	DeviceIdentifier string
	IPAddress        string
	FirmwareVersion  string
	Phase            string
	// 1-based batch number; 0 when not part of a snapshotted batch.
	Batch              int32
	Status             string
	Online             bool
	Hashing            bool
	HasBaseline        bool
	BaselineHashing    bool
	HashRateHs         Metric
	PowerW             Metric
	EfficiencyJh       Metric
	TempC              Metric
	OpenErrors         int32
	BaselineOpenErrors int32
	Attempts           int32
	LastSentAt         *time.Time
	LastError          string
}

// Evidence summarizes post-update health of the miners under review versus
// their own baselines, and whether it clears the auto-continue conditions.
type Evidence struct {
	DevicesTotal                  int32
	Verified                      int32
	Online                        int32
	Hashing                       int32
	BaselineHashing               int32
	Failed                        int32
	HashrateChangePercent         float64
	EfficiencyChangePercent       float64
	TemperatureChangeC            float64
	NewErrors                     int32
	ReadyToAdvance                bool
	HoldReason                    string
	StabilizationRemainingSeconds int32
	// Aggregates over miners with both halves: total hashrate, total power,
	// mean efficiency, mean temperature.
	HashRateHs   Metric
	PowerW       Metric
	EfficiencyJh Metric
	TempC        Metric
}

// Rollout is one firmware change for one model within one channel.
type Rollout struct {
	ID                      int64
	ChannelID               int64
	ChannelName             string
	Model                   string
	FirmwareFileID          string
	FirmwareVersion         string
	Status                  string
	State                   string
	Stage                   string
	CancelReason            string
	Behavior                Behavior
	BatchCount              int32
	CurrentBatch            int32
	StageChangedAt          time.Time
	PausedAt                *time.Time
	CreatedAt               time.Time
	FinishedAt              *time.Time
	PreviousFirmwareFileID  string
	PreviousFirmwareVersion string
	Devices                 []RolloutDevice
	// Evidence for the miners under review; nil unless the rollout is active.
	Evidence *Evidence
}

// rolloutSpec is everything needed to create a rollout for one assignment.
type rolloutSpec struct {
	OrgID, ChannelID                                int64
	ChannelName, Model                              string
	FirmwareFileID, FirmwareVersion                 string
	PreviousFirmwareFileID, PreviousFirmwareVersion string
	CreatedBy                                       int64
	Behavior                                        Behavior
	// When the assignment was (last) made; miners halted for this version
	// by rollouts started since are not picked up again.
	AssignedAt time.Time
}

// startRollout creates a rollout for one assignment and snapshots every
// mismatched member with its baseline health, ordered per the behavior and
// split into batches for staged methods. Returns nil when no member is
// mismatched (or every mismatched member is halted for this version).
func (s *Service) startRollout(ctx context.Context, spec rolloutSpec) (*sqlc.FirmwareRollout, error) {
	q := s.store.Queries(ctx)
	rows, err := q.ListReleaseChannelMismatchedMembers(ctx, sqlc.ListReleaseChannelMismatchedMembersParams{
		ChannelID: spec.ChannelID, Model: spec.Model, FirmwareVersion: spec.FirmwareVersion,
		RolloutID: 0, AssignedAt: spec.AssignedAt,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list mismatched members: %v", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ordered := s.orderMembers(rows, spec.Behavior.Order)

	b := spec.Behavior
	var batches [][]int64
	var rest []int64
	switch b.Method {
	case MethodPilotThenContinue:
		n := min(int(b.PilotSize), len(ordered))
		batches = [][]int64{ordered[:n]}
		rest = ordered[n:]
	case MethodBatched:
		size := int(b.BatchSize)
		for start := 0; start < len(ordered); start += size {
			batches = append(batches, ordered[start:min(start+size, len(ordered))])
		}
	default:
		rest = ordered
	}
	stage := StageRest
	if len(batches) > 0 {
		stage = StageBatch
	}

	r, err := q.CreateFirmwareRollout(ctx, sqlc.CreateFirmwareRolloutParams{
		OrgID:                        spec.OrgID,
		ChannelID:                    spec.ChannelID,
		Model:                        spec.Model,
		FirmwareFileID:               spec.FirmwareFileID,
		FirmwareVersion:              spec.FirmwareVersion,
		PreviousFirmwareFileID:       spec.PreviousFirmwareFileID,
		PreviousFirmwareVersion:      spec.PreviousFirmwareVersion,
		Stage:                        stage,
		CreatedBy:                    spec.CreatedBy,
		Method:                       b.Method,
		OrderBy:                      b.Order,
		BatchSize:                    b.BatchSize,
		PilotSize:                    b.PilotSize,
		WaitBetweenBatchesSeconds:    b.WaitBetweenBatchesSeconds,
		ReviewAfterEachBatch:         b.ReviewAfterEachBatch,
		AutoContinue:                 b.AutoContinue,
		StabilizationSeconds:         b.StabilizationSeconds,
		MaxHashrateDropPercent:       toNullFloat(b.Thresholds.MaxHashrateDropPercent),
		MaxEfficiencyIncreasePercent: toNullFloat(b.Thresholds.MaxEfficiencyIncreasePercent),
		MaxTempIncreaseC:             toNullFloat(b.Thresholds.MaxTempIncreaseC),
		MaxNewErrors:                 toNullInt(b.Thresholds.MaxNewErrors),
		MaxConcurrentOffline:         b.MaxConcurrentOffline,
		BatchCount:                   int32(len(batches)), // #nosec G115 -- bounded by the member count
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("create rollout: %v", err)
	}

	offset := 0
	for i, batch := range batches {
		index := sql.NullInt32{Int32: int32(i), Valid: true} // #nosec G115 -- bounded by the member count
		if err := s.snapshot(ctx, r.ID, batch, index, offset); err != nil {
			return nil, err
		}
		offset += len(batch)
	}
	if len(rest) > 0 {
		if err := s.snapshot(ctx, r.ID, rest, sql.NullInt32{}, offset); err != nil {
			return nil, err
		}
	}
	return &r, nil
}

// snapshot records miners into a rollout. A negative offset marks late
// joiners, which carry no position and sort last.
func (s *Service) snapshot(ctx context.Context, rolloutID int64, deviceIDs []int64, batch sql.NullInt32, offset int) error {
	params := sqlc.SnapshotFirmwareRolloutDevicesParams{RolloutID: rolloutID, DeviceIds: deviceIDs, BatchIndex: batch}
	if offset >= 0 {
		params.PositionOffset = sql.NullInt32{Int32: int32(offset), Valid: true} // #nosec G115 -- bounded by the member count
	}
	if err := s.store.Queries(ctx).SnapshotFirmwareRolloutDevices(ctx, params); err != nil {
		return fleeterror.NewInternalErrorf("snapshot rollout devices: %v", err)
	}
	return nil
}

// orderMembers returns device ids in the order the rollout works through
// them: worst efficiency first (no sample last), or shuffled.
func (s *Service) orderMembers(rows []sqlc.ListReleaseChannelMismatchedMembersRow, order string) []int64 {
	sorted := append([]sqlc.ListReleaseChannelMismatchedMembersRow(nil), rows...)
	switch order {
	case OrderRandom:
		s.shuffle(len(sorted), func(i, j int) { sorted[i], sorted[j] = sorted[j], sorted[i] })
	default:
		sort.SliceStable(sorted, func(i, j int) bool {
			a, b := sorted[i].EfficiencyJh, sorted[j].EfficiencyJh
			switch {
			case a.Valid && !b.Valid:
				return true
			case !a.Valid && b.Valid:
				return false
			case a.Valid && b.Valid && a.Float64 != b.Float64:
				return a.Float64 > b.Float64
			}
			return sorted[i].DeviceIdentifier < sorted[j].DeviceIdentifier
		})
	}
	ids := make([]int64, len(sorted))
	for i, r := range sorted {
		ids[i] = r.DeviceID
	}
	return ids
}

func defaultShuffle(n int, swap func(i, j int)) { rand.Shuffle(n, swap) }

// ContinueRollout releases the review gate of a staged rollout: the next
// batch starts, or the rest stage when the last batch was under review.
func (s *Service) ContinueRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	row, channelName, err := s.activeRollout(ctx, orgID, rolloutID)
	if err != nil {
		return nil, err
	}
	if row.Stage != StageAwaitingReview {
		return nil, fleeterror.NewInvalidArgumentErrorf("rollout %d is not awaiting review", rolloutID)
	}
	if err := s.advance(ctx, &row, StageAwaitingReview); err != nil {
		return nil, err
	}
	s.logRolloutEvent(ctx, row, channelName, EventRolloutContinued, false, nil)
	return s.rolloutView(ctx, row, channelName)
}

// advance moves a rollout from the given stage to its next batch or, after
// the last batch, to the rest stage. row is updated in place.
func (s *Service) advance(ctx context.Context, row *sqlc.FirmwareRollout, from string) error {
	stage, batch := StageRest, row.CurrentBatch
	if next := row.CurrentBatch + 1; next < row.BatchCount {
		stage, batch = StageBatch, next
	}
	n, err := s.store.Queries(ctx).AdvanceFirmwareRolloutStage(ctx, sqlc.AdvanceFirmwareRolloutStageParams{
		RolloutID: row.ID, FromStage: from, Stage: stage, CurrentBatch: batch,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("advance rollout: %v", err)
	}
	if n == 0 {
		return fleeterror.NewInvalidArgumentErrorf("rollout %d is no longer in the %s stage", row.ID, from)
	}
	row.Stage, row.CurrentBatch, row.StageChangedAt = stage, batch, s.now()
	return nil
}

// PauseRollout holds an active rollout: no new commands, no transitions.
func (s *Service) PauseRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	row, channelName, err := s.activeRollout(ctx, orgID, rolloutID)
	if err != nil {
		return nil, err
	}
	n, err := s.store.Queries(ctx).PauseFirmwareRollout(ctx, rolloutID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("pause rollout: %v", err)
	}
	if n == 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf("rollout %d is already paused", rolloutID)
	}
	row.PausedAt = sql.NullTime{Time: s.now(), Valid: true}
	s.logRolloutEvent(ctx, row, channelName, EventRolloutPaused, false, nil)
	return s.rolloutView(ctx, row, channelName)
}

// ResumeRollout lets a paused rollout continue where it left off.
func (s *Service) ResumeRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	row, channelName, err := s.activeRollout(ctx, orgID, rolloutID)
	if err != nil {
		return nil, err
	}
	n, err := s.store.Queries(ctx).ResumeFirmwareRollout(ctx, rolloutID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("resume rollout: %v", err)
	}
	if n == 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf("rollout %d is not paused", rolloutID)
	}
	row.PausedAt = sql.NullTime{}
	s.logRolloutEvent(ctx, row, channelName, EventRolloutResumed, false, nil)
	return s.rolloutView(ctx, row, channelName)
}

// CancelRollout cancels the remaining work of an active rollout. Miners
// already updated keep the new firmware; miners not yet updated are halted
// so drift correction does not simply restart the change. Returns the
// canceled rollout and its channel.
func (s *Service) CancelRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, *Channel, error) {
	var (
		view    *Rollout
		channel *Channel
	)
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		row, channelName, err := s.activeRollout(ctx, orgID, rolloutID)
		if err != nil {
			return err
		}
		q := s.store.Queries(ctx)
		n, err := q.CancelFirmwareRollout(ctx, rolloutID)
		if err != nil {
			return fleeterror.NewInternalErrorf("cancel rollout: %v", err)
		}
		if n == 0 {
			return fleeterror.NewInvalidArgumentErrorf("rollout %d is not active", rolloutID)
		}
		row.Status, row.CancelReason = StatusCanceled, CancelReasonCanceledRemaining
		row.FinishedAt = sql.NullTime{Time: s.now(), Valid: true}

		targets, err := s.listTargets(ctx, row)
		if err != nil {
			return err
		}
		var remaining []int64
		for _, t := range targets {
			if !t.excluded() && !t.halted() && !t.verified(row.FirmwareVersion) {
				remaining = append(remaining, t.DeviceID)
			}
		}
		if len(remaining) > 0 {
			if err := q.HaltFirmwareRolloutDevices(ctx, sqlc.HaltFirmwareRolloutDevicesParams{
				RolloutID: rolloutID, DeviceIds: remaining, HaltReason: HaltReasonCanceled, LastError: "Update canceled by operator",
			}); err != nil {
				return fleeterror.NewInternalErrorf("halt remaining devices: %v", err)
			}
		}
		s.logRolloutEvent(ctx, row, channelName, EventRolloutCanceled, false, map[string]any{"remaining": len(remaining)})

		view, err = s.rolloutView(ctx, row, channelName)
		if err != nil {
			return err
		}
		channel, err = s.GetChannel(ctx, orgID, row.ChannelID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return view, channel, nil
}

// RetryFailedDevices re-queues the halted miners of a rollout. An active
// rollout retries them in place; a finished rollout gets a new all-at-once
// rollout for them (started here so the caller can show it). Returns the
// rollout now carrying the miners.
func (s *Service) RetryFailedDevices(ctx context.Context, orgID, userID, rolloutID int64) (*Rollout, error) {
	var view *Rollout
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		q := s.store.Queries(ctx)
		row, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rolloutID, OrgID: orgID})
		if err != nil {
			return fleeterror.NewNotFoundErrorf("rollout not found: %d", rolloutID)
		}
		channel, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: row.ChannelID, OrgID: orgID})
		if err != nil {
			return fleeterror.NewNotFoundErrorf("release channel not found: %d", row.ChannelID)
		}

		if row.Status == StatusActive {
			requeued, err := q.RequeueFirmwareRolloutDevices(ctx, rolloutID)
			if err != nil {
				return fleeterror.NewInternalErrorf("requeue devices: %v", err)
			}
			if len(requeued) == 0 {
				return fleeterror.NewInvalidArgumentErrorf("rollout %d has no failed miners", rolloutID)
			}
			s.logRolloutEvent(ctx, row, channel.Name, EventRolloutRetried, false, map[string]any{"retried": len(requeued)})
			view, err = s.rolloutView(ctx, row, channel.Name)
			return err
		}

		released, err := q.ReleaseFirmwareRolloutDeviceHalts(ctx, rolloutID)
		if err != nil {
			return fleeterror.NewInternalErrorf("release device halts: %v", err)
		}
		if len(released) == 0 {
			return fleeterror.NewInvalidArgumentErrorf("rollout %d has no failed miners", rolloutID)
		}
		// The assignment must still be the one this rollout enforced,
		// otherwise there is nothing to retry towards.
		assignments, err := q.ListReleaseChannelFirmware(ctx, orgID)
		if err != nil {
			return fleeterror.NewInternalErrorf("list channel firmware: %v", err)
		}
		var assignment *sqlc.ReleaseChannelFirmware
		for i := range assignments {
			f := &assignments[i]
			if f.ChannelID == row.ChannelID && f.Model == row.Model && f.FirmwareFileID == row.FirmwareFileID {
				assignment = f
			}
		}
		if assignment == nil {
			return fleeterror.NewFailedPreconditionErrorf("%s in %s is no longer assigned %s", row.Model, channel.Name, row.FirmwareVersion)
		}
		s.logRolloutEvent(ctx, row, channel.Name, EventRolloutRetried, false, map[string]any{"retried": len(released)})
		started, err := s.startRollout(ctx, rolloutSpec{
			OrgID: orgID, ChannelID: channel.ID, ChannelName: channel.Name, Model: row.Model,
			FirmwareFileID: row.FirmwareFileID, FirmwareVersion: row.FirmwareVersion,
			PreviousFirmwareFileID: row.PreviousFirmwareFileID, PreviousFirmwareVersion: row.PreviousFirmwareVersion,
			CreatedBy: userID, Behavior: allAtOnce, AssignedAt: assignment.UpdatedAt,
		})
		if err != nil {
			return err
		}
		if started == nil {
			// Everything verified in the meantime; show the original.
			view, err = s.rolloutView(ctx, row, channel.Name)
			return err
		}
		s.logRolloutEvent(ctx, *started, channel.Name, EventRolloutStarted, false, map[string]any{"retry_of": rolloutID})
		view, err = s.rolloutView(ctx, *started, channel.Name)
		return err
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

// activeRollout loads an active rollout of the org together with its channel
// name, or returns a not-found / invalid-argument error.
func (s *Service) activeRollout(ctx context.Context, orgID, rolloutID int64) (sqlc.FirmwareRollout, string, error) {
	q := s.store.Queries(ctx)
	row, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rolloutID, OrgID: orgID})
	if err != nil {
		return sqlc.FirmwareRollout{}, "", fleeterror.NewNotFoundErrorf("rollout not found: %d", rolloutID)
	}
	if row.Status != StatusActive {
		return sqlc.FirmwareRollout{}, "", fleeterror.NewInvalidArgumentErrorf("rollout %d is not active", rolloutID)
	}
	channel, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: row.ChannelID, OrgID: orgID})
	if err != nil {
		return sqlc.FirmwareRollout{}, "", fleeterror.NewNotFoundErrorf("release channel not found: %d", row.ChannelID)
	}
	return row, channel.Name, nil
}

// ListRollouts returns rollouts for an org (optionally one channel) with
// live per-device progress.
func (s *Service) ListRollouts(ctx context.Context, orgID int64, channelID int64) ([]Rollout, error) {
	q := s.store.Queries(ctx)
	var filter sql.NullInt64
	if channelID != 0 {
		filter = sql.NullInt64{Int64: channelID, Valid: true}
	}
	rows, err := q.ListFirmwareRollouts(ctx, sqlc.ListFirmwareRolloutsParams{OrgID: orgID, ChannelID: filter})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list rollouts: %v", err)
	}
	rollouts := make([]Rollout, 0, len(rows))
	for _, row := range rows {
		view, err := s.rolloutView(ctx, row.FirmwareRollout, row.ChannelName)
		if err != nil {
			return nil, err
		}
		rollouts = append(rollouts, *view)
	}
	return rollouts, nil
}

// --- Targets ---

// target is one miner of a rollout with derived health.
type target struct {
	sqlc.ListFirmwareRolloutDevicesRow
}

// deviceStatusActive is the device_status value of a hashing miner.
const deviceStatusActive = "ACTIVE"

// online: the miner is reachable. UPDATING counts as not yet back, since a
// miner mid-flash has not proven it survived the update.
func (t target) online() bool {
	switch t.Status {
	case "", "OFFLINE", "UNKNOWN", "UPDATING":
		return false
	}
	return true
}

func (t target) hashing() bool { return t.Status == deviceStatusActive }

func (t target) baselineHashing() bool {
	return t.BaselineStatus.Valid && t.BaselineStatus.String == deviceStatusActive
}

func (t target) inBatch(index int32) bool {
	return t.BatchIndex.Valid && t.BatchIndex.Int32 == index
}

func (t target) excluded() bool { return t.ExcludedAt.Valid }

func (t target) halted() bool { return t.HaltedAt.Valid }

func (t target) failed() bool { return t.halted() && t.HaltReason == HaltReasonFailed }

// verified: on the target version, back online, and hashing if it was
// hashing before the update. A miner that was not hashing before (e.g. no
// pool configured) is not held to a standard the update cannot meet.
func (t target) verified(version string) bool {
	return t.FirmwareVersion == version && t.online() && (t.hashing() || !t.baselineHashing())
}

// settled: nothing more will happen to this miner in the rollout.
func (t target) settled(version string) bool {
	return t.verified(version) || t.halted()
}

// phase derives the operator-facing phase. Finished rollouts keep their
// record: a miner failed in a completed-with-failures rollout stays failed
// even after its halt was released for a retry.
func (t target) phase(r sqlc.FirmwareRollout) string {
	switch {
	case t.excluded():
		return PhaseExcluded
	case t.verified(r.FirmwareVersion):
		return PhaseDone
	case t.HaltReason == HaltReasonFailed && (t.halted() || r.Status != StatusActive):
		return PhaseFailed
	case r.Status == StatusCompleted:
		// Completed rollouts verified every miner; comparing against live
		// versions would misreport history after a later change.
		return PhaseDone
	case t.HaltReason == HaltReasonCanceled && r.Status != StatusActive:
		if t.Attempts == 0 {
			return PhaseQueued
		}
		return PhaseInProgress
	case t.Attempts >= 2:
		return PhaseRetrying
	case t.Attempts == 1:
		return PhaseInProgress
	}
	return PhaseQueued
}

func (t target) view(r sqlc.FirmwareRollout) RolloutDevice {
	d := RolloutDevice{
		DeviceID:           t.DeviceID,
		DeviceIdentifier:   t.DeviceIdentifier,
		IPAddress:          t.IpAddress,
		FirmwareVersion:    t.FirmwareVersion,
		Phase:              t.phase(r),
		Status:             t.Status,
		Online:             t.online(),
		Hashing:            t.hashing(),
		HasBaseline:        t.BaselineStatus.Valid,
		BaselineHashing:    t.baselineHashing(),
		HashRateHs:         metricFrom(t.BaselineHashRateHs, t.HashRateHs),
		PowerW:             metricFrom(t.BaselinePowerW, t.PowerW),
		EfficiencyJh:       metricFrom(t.BaselineEfficiencyJh, t.EfficiencyJh),
		TempC:              metricFrom(t.BaselineTempC, t.TempC),
		OpenErrors:         t.OpenErrors,
		BaselineOpenErrors: t.BaselineOpenErrors.Int32,
		Attempts:           t.Attempts,
		LastError:          t.LastError,
	}
	if t.BatchIndex.Valid {
		d.Batch = t.BatchIndex.Int32 + 1
	}
	if t.LastSentAt.Valid {
		ts := t.LastSentAt.Time
		d.LastSentAt = &ts
	}
	return d
}

func (s *Service) listTargets(ctx context.Context, r sqlc.FirmwareRollout) ([]target, error) {
	rows, err := s.store.Queries(ctx).ListFirmwareRolloutDevices(ctx, r.ID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list rollout devices: %v", err)
	}
	targets := make([]target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, target{row})
	}
	return targets, nil
}

// activeTargets drops miners that left the channel scope.
func activeTargets(targets []target) []target {
	out := make([]target, 0, len(targets))
	for _, t := range targets {
		if !t.excluded() {
			out = append(out, t)
		}
	}
	return out
}

// reviewScope is the set of targets whose evidence governs the rollout right
// now: the current batch while batching, at the gate or waiting; everything
// in the rest stage.
func reviewScope(r sqlc.FirmwareRollout, targets []target) []target {
	targets = activeTargets(targets)
	if r.Stage == StageRest {
		return targets
	}
	var scope []target
	for _, t := range targets {
		if t.inBatch(r.CurrentBatch) {
			scope = append(scope, t)
		}
	}
	return scope
}

// --- Views ---

func (s *Service) rolloutView(ctx context.Context, r sqlc.FirmwareRollout, channelName string) (*Rollout, error) {
	targets, err := s.listTargets(ctx, r)
	if err != nil {
		return nil, err
	}
	view := &Rollout{
		ID:                      r.ID,
		ChannelID:               r.ChannelID,
		ChannelName:             channelName,
		Model:                   r.Model,
		FirmwareFileID:          r.FirmwareFileID,
		FirmwareVersion:         r.FirmwareVersion,
		Status:                  r.Status,
		Stage:                   r.Stage,
		CancelReason:            r.CancelReason,
		Behavior:                behaviorFromRollout(r),
		BatchCount:              r.BatchCount,
		CurrentBatch:            r.CurrentBatch,
		StageChangedAt:          r.StageChangedAt,
		CreatedAt:               r.CreatedAt,
		PreviousFirmwareFileID:  r.PreviousFirmwareFileID,
		PreviousFirmwareVersion: r.PreviousFirmwareVersion,
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time
		view.FinishedAt = &t
	}
	if r.PausedAt.Valid {
		t := r.PausedAt.Time
		view.PausedAt = &t
	}
	for _, t := range targets {
		view.Devices = append(view.Devices, t.view(r))
	}
	var ev *Evidence
	if r.Status == StatusActive {
		e := s.evaluate(r, reviewScope(r, targets))
		ev = &e
		view.Evidence = ev
	}
	view.State = deriveState(r, ev)
	return view, nil
}

// deriveState maps status, stage and pause onto the operator-facing state.
func deriveState(r sqlc.FirmwareRollout, ev *Evidence) string {
	switch r.Status {
	case StatusCompleted:
		return StateCompleted
	case StatusCompletedWithFailures:
		return StateCompletedWithFailures
	case StatusCanceled:
		return StateCanceled
	}
	if r.PausedAt.Valid {
		return StatePaused
	}
	if r.Stage == StageAwaitingReview {
		if r.AutoContinue && ev != nil && ev.HoldReason == holdStabilizing {
			return StateStabilizingTelemetry
		}
		if r.Method == MethodPilotThenContinue && r.CurrentBatch == 0 {
			return StatePausedAtPilotGate
		}
		return StatePausedAtBatchReview
	}
	return StateInProgress
}

// logRolloutEvent records a rollout lifecycle event. system marks events
// raised by the enforcement loop rather than an operator; operator events
// take their actor from the request session.
func (s *Service) logRolloutEvent(ctx context.Context, r sqlc.FirmwareRollout, channelName, eventType string, system bool, extra map[string]any) {
	if s.activity == nil {
		return
	}
	orgID := r.OrgID
	metadata := map[string]any{
		"rollout_id":       r.ID,
		"channel_id":       r.ChannelID,
		"channel_name":     channelName,
		"model":            r.Model,
		"firmware_version": r.FirmwareVersion,
		"method":           r.Method,
		"stage":            r.Stage,
	}
	for k, v := range extra {
		metadata[k] = v
	}
	event := activitymodels.Event{
		Category:       activitymodels.CategoryDeviceCommand,
		Type:           eventType,
		Description:    fmt.Sprintf("%s: %s %s → %s", eventDescriptions[eventType], channelName, r.Model, r.FirmwareVersion),
		OrganizationID: &orgID,
		Metadata:       metadata,
	}
	if system {
		event.ActorType = activitymodels.ActorSystem
	} else {
		activity.StampActor(ctx, &event)
	}
	s.activity.Log(ctx, event)
}

var eventDescriptions = map[string]string{
	EventRolloutStarted:               "Started firmware update",
	EventRolloutReviewReady:           "Firmware update ready for review",
	EventRolloutContinued:             "Continued firmware update",
	EventRolloutPaused:                "Paused firmware update",
	EventRolloutResumed:               "Resumed firmware update",
	EventRolloutCanceled:              "Canceled remaining firmware updates",
	EventRolloutCompleted:             "Completed firmware update",
	EventRolloutCompletedWithFailures: "Completed firmware update with failures",
	EventRolloutRetried:               "Retried failed firmware updates",
}
