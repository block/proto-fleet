package rollout

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

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
	// StateWaitingForController is reserved for delegated rollouts.
	StateWaitingForController = "waiting_for_controller"

	// Per-miner phases.
	PhaseQueued     = "queued"
	PhaseInProgress = "in_progress"
	PhaseRetrying   = "retrying"
	PhaseDone       = "done"
	PhaseFailed     = "failed"
	PhaseExcluded   = "excluded"
	PhaseSkipped    = "skipped"

	// HaltReasonFailed: attempts exhausted without the miner verifying.
	HaltReasonFailed = "failed"
	// HaltReasonCanceled: the rollout was canceled before the miner updated.
	HaltReasonCanceled = "canceled"
	// HaltReasonSkipped: a caller settled the miner without updating it.
	HaltReasonSkipped = "skipped"

	// Actor types, mirroring rollout.v1.RolloutActorType.
	ActorTypeUser   = "user"
	ActorTypeAPIKey = "api_key"
	ActorTypeSystem = "system"

	// Error reasons, mirroring rollout.v1.RolloutErrorReason; carried by
	// ErrorInfo inside FAILED_PRECONDITION and INVALID_ARGUMENT errors.
	ReasonStaleRevision     = "stale_revision"
	ReasonStaleGeneration   = "stale_generation"
	ReasonNotActive         = "not_active"
	ReasonNotAtGate         = "not_at_gate"
	ReasonNotDelegated      = "not_delegated"
	ReasonPaused            = "paused"
	ReasonOfflineBudgetFull = "offline_budget_full"
	ReasonDeviceNotQueued   = "device_not_queued"
	ReasonScopeOverlap      = "scope_overlap"
	ReasonArtifactMissing   = "artifact_missing"
	ReasonRolloutActive     = "rollout_active"
	ReasonUpdatesInFlight   = "updates_in_flight"
	ReasonArtifactMismatch  = "artifact_mismatch"

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

// Actor is who performed a rollout action: an operator, an API key, or the
// server itself (id 0).
type Actor struct {
	Type string
	ID   int64
	Name string
}

// SystemActor attributes actions the server takes on its own.
var SystemActor = Actor{Type: ActorTypeSystem}

// ErrorInfo is the machine-readable cause of a rollout failure. Wrap it into
// a fleeterror with %w and read it back with errors.As.
type ErrorInfo struct {
	Reason string
	// CurrentRevision is set with ReasonStaleRevision.
	CurrentRevision int64
	// DeviceIdentifiers names the targets the failure concerns.
	DeviceIdentifiers []string
}

func (e *ErrorInfo) Error() string { return e.Reason }

// Reason wraps a fleeterror constructor result so callers can branch on the
// cause: reason(fleeterror.NewFailedPreconditionErrorf, ReasonNotActive, "rollout %d is not active", id).
func reason(newf func(string, ...any) fleeterror.FleetError, info ErrorInfo, format string, a ...any) error {
	return newf("%s: %w", fmt.Sprintf(format, a...), &info)
}

// ReasonOf returns the ErrorInfo carried by err, if any.
func ReasonOf(err error) (*ErrorInfo, bool) {
	var info *ErrorInfo
	if errors.As(err, &info) {
		return info, true
	}
	return nil, false
}

// isNotFound reports whether err is a NotFound fleet error.
func isNotFound(err error) bool {
	var fe fleeterror.FleetError
	return errors.As(err, &fe) && fe.GRPCCode == connect.CodeNotFound
}

// Metric is one telemetry reading before the update (baseline) and now; a
// nil half means no sample was available.
type Metric struct {
	Baseline *float64
	Current  *float64
}

// Aggregate is a metric summed or averaged over the sampled miners under
// review, with how many miners it samples.
type Aggregate struct {
	Metric
	SampledDevices int32
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
	// SkipNote is the caller's note when the miner was SKIPPED.
	SkipNote string
	// LastDeployedFirmwareChecksum is the miner's managed-deployment
	// provenance; empty when none is recorded.
	LastDeployedFirmwareChecksum string
}

// Evidence summarizes post-update health of the miners under review versus
// their own baselines, and whether it clears the auto-continue conditions.
type Evidence struct {
	DevicesTotal    int32
	Verified        int32
	Online          int32
	Hashing         int32
	BaselineHashing int32
	Failed          int32
	Excluded        int32
	Skipped         int32
	// Change fields are set only when derivable from the aggregates below
	// (percent changes also need a nonzero baseline).
	HashrateChangePercent         *float64
	EfficiencyChangePercent       *float64
	TemperatureChangeC            *float64
	NewErrors                     int32
	ReadyToAdvance                bool
	HoldReason                    string
	StabilizationRemainingSeconds int32
	// Aggregates over verified miners with both halves: total hashrate,
	// total power, mean efficiency, mean temperature.
	HashRateHs   Aggregate
	PowerW       Aggregate
	EfficiencyJh Aggregate
	TempC        Aggregate
}

// Rollout is one firmware change for one manufacturer/model pair within one
// channel.
type Rollout struct {
	ID           int64
	ChannelID    int64
	ChannelName  string
	Manufacturer string
	Model        string
	// FirmwareChecksum identifies the artifact; FirmwareFileID is the
	// uploaded file that currently carries it, empty while none does.
	FirmwareChecksum string
	FirmwareFileID   string
	FirmwareVersion  string
	// Lineage: the assignment this rollout replaced; empty for a first one.
	PreviousFirmwareChecksum string
	PreviousFirmwareVersion  string
	AssignmentGeneration     int64
	Status                   string
	State                    string
	Stage                    string
	CancelReason             string
	Behavior                 Behavior
	BatchCount               int32
	CurrentBatch             int32
	StageChangedAt           time.Time
	PausedAt                 *time.Time
	CreatedAt                time.Time
	FinishedAt               *time.Time
	// Revision advances on every change under the revision rule.
	Revision     int64
	UpdatedAt    time.Time
	StartedBy    Actor
	LastActionBy Actor
	// Devices is every snapshotted miner with live progress. The API pages
	// them separately (ListRolloutDevices) and sends the counts below.
	Devices []RolloutDevice
	// DeviceCounts tallies Devices by phase.
	DeviceCounts DeviceCounts
	// CurrentBatchCounts tallies the batch in flight or under review; zero
	// in the rest stage.
	CurrentBatchCounts DeviceCounts
	// Evidence for the miners under review; nil unless the rollout is active.
	Evidence *Evidence
}

// DeviceCounts is how many miners of a rollout (or one batch) are in each
// phase.
type DeviceCounts struct {
	Queued     int32
	InProgress int32
	Retrying   int32
	Done       int32
	Failed     int32
	Excluded   int32
	Skipped    int32
}

func countPhases(devices []RolloutDevice) DeviceCounts {
	var c DeviceCounts
	for _, d := range devices {
		switch d.Phase {
		case PhaseQueued:
			c.Queued++
		case PhaseInProgress:
			c.InProgress++
		case PhaseRetrying:
			c.Retrying++
		case PhaseDone:
			c.Done++
		case PhaseFailed:
			c.Failed++
		case PhaseExcluded:
			c.Excluded++
		case PhaseSkipped:
			c.Skipped++
		}
	}
	return c
}

// rolloutSpec is everything needed to create a rollout for one assignment.
type rolloutSpec struct {
	OrgID, ChannelID                                  int64
	ChannelName                                       string
	Pair                                              PairKey
	FirmwareChecksum, FirmwareVersion                 string
	PreviousFirmwareChecksum, PreviousFirmwareVersion string
	AssignmentGeneration                              int64
	Actor                                             Actor
	Behavior                                          Behavior
	// Members, when set, are the exact targets to snapshot (a retry);
	// otherwise the pair's mismatched, unsuppressed members are targeted.
	Members []sqlc.ListReleaseChannelMismatchedMembersRow
}

// mismatchedParams builds the query parameters selecting a pair's mismatched,
// unsuppressed members under the current generation. Queued checksums identify
// artifacts directly; uploaded file IDs resolve legacy commands without one.
func (s *Service) mismatchedParams(spec rolloutSpec, rolloutID int64) sqlc.ListReleaseChannelMismatchedMembersParams {
	return sqlc.ListReleaseChannelMismatchedMembersParams{
		OrgID: spec.OrgID, ChannelID: spec.ChannelID, Manufacturer: spec.Pair.Manufacturer, Model: spec.Pair.Model,
		FirmwareVersion: spec.FirmwareVersion, FirmwareChecksum: spec.FirmwareChecksum,
		AssignedFileIds:      s.files.FirmwareFileIDsByChecksum(spec.FirmwareChecksum),
		AssignmentGeneration: spec.AssignmentGeneration, RolloutID: rolloutID,
	}
}

// startRollout creates a rollout for one assignment and snapshots every
// mismatched member with its baseline health, ordered per the behavior and
// split into batches for staged methods. Returns nil when no member is
// mismatched (or every mismatched member is suppressed in this generation).
func (s *Service) startRollout(ctx context.Context, spec rolloutSpec) (*sqlc.FirmwareRollout, error) {
	q := s.store.GetQueries(ctx)
	rows := spec.Members
	if rows == nil {
		var err error
		rows, err = q.ListReleaseChannelMismatchedMembers(ctx, s.mismatchedParams(spec, 0))
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("list mismatched members: %v", err)
		}
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
		OrgID:                    spec.OrgID,
		ChannelID:                spec.ChannelID,
		Manufacturer:             spec.Pair.Manufacturer,
		Model:                    spec.Pair.Model,
		FirmwareChecksum:         spec.FirmwareChecksum,
		FirmwareVersion:          spec.FirmwareVersion,
		PreviousFirmwareChecksum: spec.PreviousFirmwareChecksum,
		PreviousFirmwareVersion:  spec.PreviousFirmwareVersion,
		AssignmentGeneration:     spec.AssignmentGeneration,
		Stage:                    stage,
		ActorType:                spec.Actor.Type,
		ActorID:                  spec.Actor.ID,
		ActorName:                spec.Actor.Name,
		BehaviorSnapshot:         b.snapshot(),
		BatchCount:               int32(len(batches)), // #nosec G115 -- bounded by the member count
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

// snapshot records initial miners with their baseline and position.
func (s *Service) snapshot(ctx context.Context, rolloutID int64, deviceIDs []int64, batch sql.NullInt32, offset int) error {
	params := sqlc.SnapshotFirmwareRolloutDevicesParams{
		RolloutID: rolloutID, DeviceIds: deviceIDs, BatchIndex: batch,
		PositionOffset: int32(offset), // #nosec G115 -- bounded by the member count
	}
	if err := s.store.GetQueries(ctx).SnapshotFirmwareRolloutDevices(ctx, params); err != nil {
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

// Mutation is what every operator or controller action on a rollout carries:
// who acts, the revision the caller last saw (0 to skip the check), and an
// optional rationale for the audit trail.
type Mutation struct {
	Actor            Actor
	ExpectedRevision int64
	Note             string
}

func (m Mutation) actorParams() (sql.NullString, sql.NullInt64, sql.NullString) {
	return sql.NullString{String: m.Actor.Type, Valid: true},
		sql.NullInt64{Int64: m.Actor.ID, Valid: true},
		sql.NullString{String: m.Actor.Name, Valid: true}
}

// ContinueRollout releases the review gate of a staged rollout: the next
// batch starts, or the rest stage when the last batch was under review.
func (s *Service) ContinueRollout(ctx context.Context, orgID, rolloutID int64, m Mutation) (*Rollout, error) {
	return s.mutateActive(ctx, orgID, rolloutID, m, func(ctx context.Context, row *sqlc.FirmwareRollout, channelName string) error {
		if row.Stage != StageAwaitingReview {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonNotAtGate}, "rollout %d is not awaiting review", rolloutID)
		}
		if err := s.advance(ctx, row, StageAwaitingReview, &m); err != nil {
			return err
		}
		s.logRolloutEvent(ctx, *row, channelName, EventRolloutContinued, false, m.extra(nil))
		return nil
	})
}

// PauseRollout holds an active rollout: no new commands, no transitions.
func (s *Service) PauseRollout(ctx context.Context, orgID, rolloutID int64, m Mutation) (*Rollout, error) {
	return s.mutateActive(ctx, orgID, rolloutID, m, func(ctx context.Context, row *sqlc.FirmwareRollout, channelName string) error {
		n, err := s.store.GetQueries(ctx).PauseFirmwareRollout(ctx, sqlc.PauseFirmwareRolloutParams{
			RolloutID: rolloutID, ActorType: m.Actor.Type, ActorID: m.Actor.ID, ActorName: m.Actor.Name,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("pause rollout: %v", err)
		}
		if n == 0 {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonPaused}, "rollout %d is already paused", rolloutID)
		}
		row.PausedAt = sql.NullTime{Time: s.now(), Valid: true}
		s.logRolloutEvent(ctx, *row, channelName, EventRolloutPaused, false, m.extra(nil))
		return nil
	})
}

// ResumeRollout lets a paused rollout continue where it left off.
func (s *Service) ResumeRollout(ctx context.Context, orgID, rolloutID int64, m Mutation) (*Rollout, error) {
	return s.mutateActive(ctx, orgID, rolloutID, m, func(ctx context.Context, row *sqlc.FirmwareRollout, channelName string) error {
		n, err := s.store.GetQueries(ctx).ResumeFirmwareRollout(ctx, sqlc.ResumeFirmwareRolloutParams{
			RolloutID: rolloutID, ActorType: m.Actor.Type, ActorID: m.Actor.ID, ActorName: m.Actor.Name,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("resume rollout: %v", err)
		}
		if n == 0 {
			return fleeterror.NewFailedPreconditionErrorf("rollout %d is not paused", rolloutID)
		}
		row.PausedAt = sql.NullTime{}
		s.logRolloutEvent(ctx, *row, channelName, EventRolloutResumed, false, m.extra(nil))
		return nil
	})
}

// mutateActive runs change against an ACTIVE rollout inside one transaction,
// with the row locked and the revision rule checked first, and returns the
// refreshed view.
func (s *Service) mutateActive(ctx context.Context, orgID, rolloutID int64, m Mutation, change func(ctx context.Context, row *sqlc.FirmwareRollout, channelName string) error) (*Rollout, error) {
	var view *Rollout
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		row, channelName, err := s.lockRollout(ctx, orgID, rolloutID, m.ExpectedRevision)
		if err != nil {
			return err
		}
		if row.Status != StatusActive {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonNotActive}, "rollout %d is not active", rolloutID)
		}
		if err := change(ctx, &row, channelName); err != nil {
			return err
		}
		view, err = s.refreshView(ctx, orgID, rolloutID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

// refreshView re-reads a rollout after a mutation so the view carries the
// revision, timestamps and actor the change produced.
func (s *Service) refreshView(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	row, err := s.store.GetQueries(ctx).GetFirmwareRolloutWithChannel(ctx, sqlc.GetFirmwareRolloutWithChannelParams{RolloutID: rolloutID, OrgID: orgID})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("reload rollout %d: %v", rolloutID, err)
	}
	fileID, _ := s.files.FindFirmwareFileIDByChecksum(row.FirmwareRollout.FirmwareChecksum)
	return s.rolloutView(ctx, row.FirmwareRollout, row.ChannelName, fileID)
}

// lockRollout loads a rollout FOR UPDATE with its channel name and enforces
// the revision rule when expectedRevision is nonzero. Must run in a
// transaction.
func (s *Service) lockRollout(ctx context.Context, orgID, rolloutID, expectedRevision int64) (sqlc.FirmwareRollout, string, error) {
	q := s.store.GetQueries(ctx)
	row, err := q.GetFirmwareRolloutForUpdate(ctx, sqlc.GetFirmwareRolloutForUpdateParams{RolloutID: rolloutID, OrgID: orgID})
	if err != nil {
		return sqlc.FirmwareRollout{}, "", fleeterror.NewNotFoundErrorf("rollout not found: %d", rolloutID)
	}
	if expectedRevision != 0 && row.Revision != expectedRevision {
		return sqlc.FirmwareRollout{}, "", reason(fleeterror.NewFailedPreconditionErrorf,
			ErrorInfo{Reason: ReasonStaleRevision, CurrentRevision: row.Revision},
			"rollout %d is at revision %d, not %d", rolloutID, row.Revision, expectedRevision)
	}
	channel, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: row.ChannelID, OrgID: orgID})
	if err != nil {
		return sqlc.FirmwareRollout{}, "", fleeterror.NewNotFoundErrorf("release channel not found: %d", row.ChannelID)
	}
	return row, channel.Name, nil
}

// extra merges the mutation's note into event metadata.
func (m Mutation) extra(extra map[string]any) map[string]any {
	if m.Note == "" {
		return extra
	}
	out := map[string]any{"note": m.Note}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// advance moves a rollout from the given stage to its next batch or, after
// the last batch, to the rest stage, attributing it to m when an actor drove
// it. row is updated in place.
func (s *Service) advance(ctx context.Context, row *sqlc.FirmwareRollout, from string, m *Mutation) error {
	stage, batch := StageRest, row.CurrentBatch
	if next := row.CurrentBatch + 1; next < row.BatchCount {
		stage, batch = StageBatch, next
	}
	params := sqlc.AdvanceFirmwareRolloutStageParams{RolloutID: row.ID, FromStage: from, Stage: stage, CurrentBatch: batch}
	if m != nil {
		params.ActorType, params.ActorID, params.ActorName = m.actorParams()
	}
	n, err := s.store.GetQueries(ctx).AdvanceFirmwareRolloutStage(ctx, params)
	if err != nil {
		return fleeterror.NewInternalErrorf("advance rollout: %v", err)
	}
	if n == 0 {
		return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonNotAtGate}, "rollout %d is no longer in the %s stage", row.ID, from)
	}
	row.Stage, row.CurrentBatch, row.StageChangedAt = stage, batch, s.now()
	return nil
}

// CancelRollout cancels the remaining work of an active rollout. Miners
// already updated keep the new firmware; miners not yet updated are halted
// so reconciliation does not simply restart the change. Returns the
// canceled rollout and its channel.
func (s *Service) CancelRollout(ctx context.Context, orgID, rolloutID int64, m Mutation) (*Rollout, *Channel, error) {
	var channel *Channel
	view, err := s.mutateActive(ctx, orgID, rolloutID, m, func(ctx context.Context, row *sqlc.FirmwareRollout, channelName string) error {
		q := s.store.GetQueries(ctx)
		n, err := q.CancelFirmwareRollout(ctx, sqlc.CancelFirmwareRolloutParams{
			RolloutID: rolloutID, ActorType: m.Actor.Type, ActorID: m.Actor.ID, ActorName: m.Actor.Name,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("cancel rollout: %v", err)
		}
		if n == 0 {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonNotActive}, "rollout %d is not active", rolloutID)
		}
		row.Status, row.CancelReason = StatusCanceled, CancelReasonCanceledRemaining
		row.FinishedAt = sql.NullTime{Time: s.now(), Valid: true}

		targets, err := s.listTargets(ctx, *row)
		if err != nil {
			return err
		}
		var remaining []int64
		for _, t := range targets {
			if !t.excluded() && !t.halted() && !t.verified(*row) {
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
		s.logRolloutEvent(ctx, *row, channelName, EventRolloutCanceled, false, m.extra(map[string]any{"remaining": len(remaining)}))
		channel, err = s.GetChannel(ctx, orgID, row.ChannelID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return view, channel, nil
}

// RetryFailedDevices re-queues the suppressed members of a rollout's pair. An
// active rollout retries its own FAILED and SKIPPED targets in place and
// appends targets suppressed by an earlier rollout of the generation. A
// finished rollout must be current under the assignment-generation rule and
// its pair must have no active rollout; then one all-at-once rollout starts
// for every member the enforcement rule suppresses in the generation,
// whichever rollout produced that phase, inheriting the lineage. Returns the
// rollout now carrying the miners, or the referenced rollout unchanged when
// nothing is suppressed.
func (s *Service) RetryFailedDevices(ctx context.Context, orgID, rolloutID int64, m Mutation) (*Rollout, error) {
	var view *Rollout
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		q := s.store.GetQueries(ctx)
		row, channelName, err := s.lockRollout(ctx, orgID, rolloutID, m.ExpectedRevision)
		if err != nil {
			return err
		}
		pair := PairKey{Manufacturer: row.Manufacturer, Model: row.Model}

		if row.Status == StatusActive {
			suppressed, err := q.ListReleaseChannelSuppressedMembers(ctx, sqlc.ListReleaseChannelSuppressedMembersParams{
				OrgID: orgID, ChannelID: row.ChannelID, Manufacturer: pair.Manufacturer, Model: pair.Model, AssignmentGeneration: row.AssignmentGeneration,
			})
			if err != nil {
				return fleeterror.NewInternalErrorf("list suppressed members: %v", err)
			}
			ids := make([]int64, len(suppressed))
			for i, d := range suppressed {
				ids[i] = d.DeviceID
			}
			requeued, err := q.RequeueFirmwareRolloutDevices(ctx, sqlc.RequeueFirmwareRolloutDevicesParams{RolloutID: rolloutID, DeviceIds: ids})
			if err != nil {
				return fleeterror.NewInternalErrorf("requeue devices: %v", err)
			}
			if len(requeued) > 0 {
				if err := q.RecordFirmwareRolloutAction(ctx, sqlc.RecordFirmwareRolloutActionParams{
					RolloutID: rolloutID, ActorType: m.Actor.Type, ActorID: m.Actor.ID, ActorName: m.Actor.Name,
				}); err != nil {
					return fleeterror.NewInternalErrorf("record retry: %v", err)
				}
				s.logRolloutEvent(ctx, row, channelName, EventRolloutRetried, false, m.extra(map[string]any{"retried": len(requeued)}))
			}
			view, err = s.refreshView(ctx, orgID, rolloutID)
			return err
		}

		assignment, err := q.GetReleaseChannelFirmware(ctx, sqlc.GetReleaseChannelFirmwareParams{
			ChannelID: row.ChannelID, Manufacturer: pair.Manufacturer, Model: pair.Model,
		})
		if err != nil || assignment.AssignmentGeneration != row.AssignmentGeneration || assignment.FirmwareChecksum != row.FirmwareChecksum {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonStaleGeneration},
				"rollout %d is not the current assignment of %s %s in %s", rolloutID, pair.Manufacturer, pair.Model, channelName)
		}
		if _, err := q.GetActiveFirmwareRolloutForPair(ctx, sqlc.GetActiveFirmwareRolloutForPairParams{
			ChannelID: row.ChannelID, Manufacturer: pair.Manufacturer, Model: pair.Model,
		}); err == nil {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonRolloutActive},
				"%s %s in %s already has an active rollout", pair.Manufacturer, pair.Model, channelName)
		}
		suppressed, err := q.ListReleaseChannelSuppressedMembers(ctx, sqlc.ListReleaseChannelSuppressedMembersParams{
			OrgID: orgID, ChannelID: row.ChannelID, Manufacturer: pair.Manufacturer, Model: pair.Model, AssignmentGeneration: row.AssignmentGeneration,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("list suppressed members: %v", err)
		}
		if len(suppressed) == 0 {
			fileID, _ := s.files.FindFirmwareFileIDByChecksum(row.FirmwareChecksum)
			view, err = s.rolloutView(ctx, row, channelName, fileID)
			return err
		}
		members := make([]sqlc.ListReleaseChannelMismatchedMembersRow, len(suppressed))
		for i, d := range suppressed {
			members[i] = sqlc.ListReleaseChannelMismatchedMembersRow{DeviceID: d.DeviceID, DeviceIdentifier: d.DeviceIdentifier}
		}
		started, err := s.startRollout(ctx, rolloutSpec{
			OrgID: orgID, ChannelID: row.ChannelID, ChannelName: channelName, Pair: pair,
			FirmwareChecksum: row.FirmwareChecksum, FirmwareVersion: row.FirmwareVersion,
			PreviousFirmwareChecksum: row.PreviousFirmwareChecksum, PreviousFirmwareVersion: row.PreviousFirmwareVersion,
			AssignmentGeneration: row.AssignmentGeneration, Actor: m.Actor, Behavior: allAtOnce, Members: members,
		})
		if err != nil {
			return err
		}
		s.logRolloutEvent(ctx, row, channelName, EventRolloutRetried, false, m.extra(map[string]any{"retried": len(suppressed)}))
		s.logRolloutEvent(ctx, *started, channelName, EventRolloutStarted, false, map[string]any{"retry_of": rolloutID})
		view, err = s.refreshView(ctx, orgID, started.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

// GetRollout returns one rollout of the org with live per-device progress.
func (s *Service) GetRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	row, err := s.store.GetQueries(ctx).GetFirmwareRolloutWithChannel(ctx, sqlc.GetFirmwareRolloutWithChannelParams{RolloutID: rolloutID, OrgID: orgID})
	if err != nil {
		return nil, fleeterror.NewNotFoundErrorf("rollout not found: %d", rolloutID)
	}
	fileID, _ := s.files.FindFirmwareFileIDByChecksum(row.FirmwareRollout.FirmwareChecksum)
	return s.rolloutView(ctx, row.FirmwareRollout, row.ChannelName, fileID)
}

// ListRolloutDevices returns one page of a rollout's miners in snapshot
// order with live progress. The returned cursor is empty on the last page.
func (s *Service) ListRolloutDevices(ctx context.Context, orgID, rolloutID int64, pageSize int32, cursor string) ([]RolloutDevice, string, error) {
	row, err := s.store.GetQueries(ctx).GetFirmwareRolloutWithChannel(ctx, sqlc.GetFirmwareRolloutWithChannelParams{RolloutID: rolloutID, OrgID: orgID})
	if err != nil {
		return nil, "", fleeterror.NewNotFoundErrorf("rollout not found: %d", rolloutID)
	}
	offset := 0
	if cursor != "" {
		parts, err := decodeCursor(cursor, 1)
		if err != nil {
			return nil, "", err
		}
		offset, err = strconv.Atoi(parts[0])
		if err != nil || offset < 0 {
			return nil, "", fleeterror.NewInvalidArgumentError("invalid cursor")
		}
	}
	// The snapshot is small enough to derive every phase in one pass; the
	// page is cut afterwards so phases stay consistent within a response.
	targets, err := s.listTargets(ctx, row.FirmwareRollout)
	if err != nil {
		return nil, "", err
	}
	devices := make([]RolloutDevice, 0, len(targets))
	for _, t := range targets {
		devices = append(devices, t.view(row.FirmwareRollout))
	}
	if offset >= len(devices) {
		return []RolloutDevice{}, "", nil
	}
	limit := int(clampPageSize(pageSize))
	end := min(offset+limit, len(devices))
	next := ""
	if end < len(devices) {
		next = encodeCursor(strconv.Itoa(end))
	}
	return devices[offset:end], next, nil
}

// RolloutFilter selects and pages the rollouts ListRollouts returns.
type RolloutFilter struct {
	// ChannelID limits results to one channel; 0 means every channel.
	ChannelID int64
	// Status limits results to one status; "" means every status.
	Status string
	// PageSize is the maximum number of rollouts to return; 0 means
	// DefaultPageSize. Larger than MaxPageSize is clamped.
	PageSize int32
	// Cursor is the opaque cursor returned by the previous page; "" starts
	// from the newest rollout.
	Cursor string
	// UpdatedAfter, when set, filters by the recorded change timestamp.
	// It cannot be combined with PollCursor and is not a lossless poll cursor.
	UpdatedAfter *time.Time
	// PollCursor resumes a completed polling cycle. Keep it unchanged while
	// paging, then use the returned poll cursor for the next cycle.
	PollCursor string
}

// ListRollouts returns rollouts for an org, newest first, with live
// per-device progress. The page cursor is empty when no more rollouts match.
// The poll cursor is captured before the first page and carried through every
// subsequent page, so changes committed while paging remain eligible in the
// next cycle. Rows may repeat between cycles; callers replace them by ID.
func (s *Service) ListRollouts(ctx context.Context, orgID int64, filter RolloutFilter) ([]Rollout, string, string, error) {
	if filter.UpdatedAfter != nil && filter.PollCursor != "" {
		return nil, "", "", fleeterror.NewInvalidArgumentError("updated_after cannot be combined with poll_cursor")
	}
	params := sqlc.ListFirmwareRolloutsParams{OrgID: orgID}
	if filter.ChannelID != 0 {
		params.ChannelID = sql.NullInt64{Int64: filter.ChannelID, Valid: true}
	}
	if filter.Status != "" {
		params.Status = sql.NullString{String: filter.Status, Valid: true}
	}
	if filter.UpdatedAfter != nil {
		params.UpdatedAfter = sql.NullTime{Time: *filter.UpdatedAfter, Valid: true}
	}
	if filter.PollCursor != "" {
		parts, err := decodeCursor(filter.PollCursor, 1)
		if err != nil {
			return nil, "", "", err
		}
		xmin, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || xmin <= 0 {
			return nil, "", "", fleeterror.NewInvalidArgumentError("invalid poll cursor")
		}
		params.AfterRevisionTxid = sql.NullInt64{Int64: xmin, Valid: true}
	}
	var pollXmin int64
	if filter.Cursor != "" {
		parts, err := decodeCursor(filter.Cursor, 3)
		if err != nil {
			return nil, "", "", err
		}
		nanos, err1 := strconv.ParseInt(parts[0], 10, 64)
		id, err2 := strconv.ParseInt(parts[1], 10, 64)
		xmin, err3 := strconv.ParseInt(parts[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || id <= 0 || xmin <= 0 {
			return nil, "", "", fleeterror.NewInvalidArgumentError("invalid cursor")
		}
		params.BeforeCreatedAt = sql.NullTime{Time: time.Unix(0, nanos), Valid: true}
		params.BeforeID = sql.NullInt64{Int64: id, Valid: true}
		pollXmin = xmin
	} else {
		// This separate statement precedes the list snapshot. Every writer
		// still invisible to that snapshot has a transaction ID at or above
		// this boundary, even if it commits after a newer writer.
		var err error
		pollXmin, err = s.store.GetQueries(ctx).GetFirmwareRolloutPollWatermark(ctx)
		if err != nil {
			return nil, "", "", fleeterror.NewInternalErrorf("get rollout poll watermark: %v", err)
		}
	}
	limit := clampPageSize(filter.PageSize)
	// Fetch one extra row to learn whether another page exists.
	params.PageLimit = limit + 1

	rows, err := s.store.GetQueries(ctx).ListFirmwareRollouts(ctx, params)
	if err != nil {
		return nil, "", "", fleeterror.NewInternalErrorf("list rollouts: %v", err)
	}
	pollXminText := strconv.FormatInt(pollXmin, 10)
	next := ""
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1].FirmwareRollout
		// The immutable sort key keeps paging stable while new rollouts
		// start. The poll boundary stays fixed until every page is read.
		next = encodeCursor(strconv.FormatInt(last.CreatedAt.UnixNano(), 10), strconv.FormatInt(last.ID, 10), pollXminText)
	}
	rollouts := make([]Rollout, 0, len(rows))
	// Finding a file verifies its full payload. Share the result, including
	// absence, only within this response so later polls see file changes.
	fileIDs := make(map[string]string)
	for _, row := range rows {
		checksum := row.FirmwareRollout.FirmwareChecksum
		fileID, checked := fileIDs[checksum]
		if !checked {
			fileID, _ = s.files.FindFirmwareFileIDByChecksum(checksum)
			fileIDs[checksum] = fileID
		}
		view, err := s.rolloutView(ctx, row.FirmwareRollout, row.ChannelName, fileID)
		if err != nil {
			return nil, "", "", err
		}
		rollouts = append(rollouts, *view)
	}
	return rollouts, next, encodeCursor(pollXminText), nil
}

// --- Targets ---

// target is one miner of a rollout with derived health.
type target struct {
	sqlc.ListFirmwareRolloutDevicesRow
	// foreignCommand: a FirmwareUpdate for another checksum, or a legacy
	// file outside the artifact, is pending or processing on the miner's queue. Under the
	// mismatch rule the miner is not on target until it drains: the rollout's
	// own update is queued behind it, and what the miner reports meanwhile
	// says nothing about where it will end up.
	foreignCommand bool
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

func (t target) skipped() bool { return t.halted() && t.HaltReason == HaltReasonSkipped }

// reportsTarget: the miner reports the rollout's version.
func (t target) reportsTarget(r sqlc.FirmwareRollout) bool {
	return t.FirmwareVersion == r.FirmwareVersion
}

// meetsConvergence checks the live evidence needed to enter DONE: target version with
// provenance equal to the rollout's artifact and no foreign firmware command
// outstanding, back online, and hashing if it was hashing before the update.
// A miner that was not hashing before (e.g. no pool configured) is not held
// to a standard the update cannot meet. Without a baseline, the contract
// requires current hashing.
func (t target) meetsConvergence(r sqlc.FirmwareRollout) bool {
	return t.reportsTarget(r) && t.LastDeployedFirmwareChecksum == r.FirmwareChecksum && !t.foreignCommand &&
		t.online() && (t.hashing() || (t.BaselineAt.Valid && !t.baselineHashing()))
}

// verified is the persisted phase; later health samples do not rewrite it.
func (t target) verified(_ sqlc.FirmwareRollout) bool { return t.VerifiedAt.Valid }

// settled: nothing more will happen to this miner in the rollout.
func (t target) settled(r sqlc.FirmwareRollout) bool {
	return t.verified(r) || t.halted()
}

// phase derives the operator-facing phase. Finished rollouts keep their
// record: a miner failed in a completed-with-failures rollout stays failed
// even after its halt was released for a retry.
func (t target) phase(r sqlc.FirmwareRollout) string {
	switch {
	case t.excluded():
		return PhaseExcluded
	case t.skipped():
		return PhaseSkipped
	case t.verified(r):
		return PhaseDone
	case t.HaltReason == HaltReasonFailed && (t.halted() || r.Status != StatusActive):
		return PhaseFailed
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
		HasBaseline:        t.BaselineAt.Valid,
		BaselineHashing:    t.baselineHashing(),
		HashRateHs:         metricFrom(t.BaselineHashRateHs, t.HashRateHs),
		PowerW:             metricFrom(t.BaselinePowerW, t.PowerW),
		EfficiencyJh:       metricFrom(t.BaselineEfficiencyJh, t.EfficiencyJh),
		TempC:              metricFrom(t.BaselineTempC, t.TempC),
		OpenErrors:         t.OpenErrors,
		BaselineOpenErrors: t.BaselineOpenErrors.Int32,
		Attempts:           t.Attempts,
		LastError:          t.LastError,
		SkipNote:           t.SkipNote,

		LastDeployedFirmwareChecksum: t.LastDeployedFirmwareChecksum,
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
	rows, err := s.store.GetQueries(ctx).ListFirmwareRolloutDevices(ctx, r.ID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list rollout devices: %v", err)
	}
	own := map[string]bool{}
	for _, id := range s.files.FirmwareFileIDsByChecksum(r.FirmwareChecksum) {
		own[id] = true
	}
	targets := make([]target, 0, len(rows))
	for _, row := range rows {
		t := target{ListFirmwareRolloutDevicesRow: row}
		for _, checksum := range row.PendingFirmwareChecksums {
			if checksum != r.FirmwareChecksum {
				t.foreignCommand = true
			}
		}
		for _, id := range row.PendingLegacyFirmwareFileIds {
			if !own[id] {
				t.foreignCommand = true
			}
		}
		targets = append(targets, t)
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

func (s *Service) rolloutView(ctx context.Context, r sqlc.FirmwareRollout, channelName, firmwareFileID string) (*Rollout, error) {
	targets, err := s.listTargets(ctx, r)
	if err != nil {
		return nil, err
	}
	view := &Rollout{
		ID:                       r.ID,
		ChannelID:                r.ChannelID,
		ChannelName:              channelName,
		Manufacturer:             r.Manufacturer,
		Model:                    r.Model,
		FirmwareChecksum:         r.FirmwareChecksum,
		FirmwareVersion:          r.FirmwareVersion,
		FirmwareFileID:           firmwareFileID,
		PreviousFirmwareChecksum: r.PreviousFirmwareChecksum,
		PreviousFirmwareVersion:  r.PreviousFirmwareVersion,
		AssignmentGeneration:     r.AssignmentGeneration,
		Status:                   r.Status,
		Stage:                    r.Stage,
		CancelReason:             r.CancelReason,
		Behavior:                 behaviorFromRollout(r),
		BatchCount:               r.BatchCount,
		CurrentBatch:             r.CurrentBatch,
		StageChangedAt:           r.StageChangedAt,
		CreatedAt:                r.CreatedAt,
		Revision:                 r.Revision,
		UpdatedAt:                r.UpdatedAt,
		StartedBy:                Actor{Type: r.StartedByType, ID: r.StartedByID, Name: r.StartedByName},
		LastActionBy:             Actor{Type: r.LastActionByType, ID: r.LastActionByID, Name: r.LastActionByName},
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
	view.DeviceCounts = countPhases(view.Devices)
	if r.Stage != StageRest {
		var batch []RolloutDevice
		for _, d := range view.Devices {
			if d.Batch == r.CurrentBatch+1 {
				batch = append(batch, d)
			}
		}
		view.CurrentBatchCounts = countPhases(batch)
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
		if r.BehaviorSnapshot.AutoContinue && ev != nil && ev.HoldReason == holdStabilizing {
			return StateStabilizingTelemetry
		}
		if r.BehaviorSnapshot.Method == MethodPilotThenContinue && r.CurrentBatch == 0 {
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
		"manufacturer":     r.Manufacturer,
		"model":            r.Model,
		"firmware_version": r.FirmwareVersion,
		"method":           r.BehaviorSnapshot.Method,
		"stage":            r.Stage,
	}
	for k, v := range extra {
		metadata[k] = v
	}
	event := activitymodels.Event{
		Category:       activitymodels.CategoryDeviceCommand,
		Type:           eventType,
		Description:    fmt.Sprintf("%s: %s %s → %s", eventDescriptions[eventType], channelName, strings.TrimSpace(r.Manufacturer+" "+r.Model), r.FirmwareVersion),
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
