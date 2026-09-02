// Package rollout implements firmware rollout lanes: containers of miners
// grouped by model, with one optional firmware assignment per model. Once a
// model has an assignment, the enforcement loop updates every lane member of
// that model that is not running the assigned version. Each enforcement run
// for one (lane, model) pair is tracked as a rollout.
//
// Rollouts run with a method: immediate (everything at once), pilot (one
// batch, then a review gate, then the rest) or fixed batches (a review gate
// after every batch). A gate can be released manually or, when the rollout
// opted in, automatically once post-update evidence meets its thresholds.
// A miner counts as updated only when it reports the target version, is back
// online, and is at least as healthy (hashing) as before the update.
package rollout

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"connectrpc.com/authn"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const (
	// StatusActive marks a rollout that is still enforcing its firmware.
	StatusActive = "active"
	// StatusCompleted marks a rollout whose targets are all verified.
	StatusCompleted = "completed"
	// StatusCanceled marks a rollout that ended before completion.
	StatusCanceled = "canceled"

	// CancelReasonSuperseded: a newer assignment (or rollback) replaced it.
	CancelReasonSuperseded = "superseded"
	// CancelReasonAborted: an operator aborted it.
	CancelReasonAborted = "aborted"
	// CancelReasonCleared: the model's assignment was cleared.
	CancelReasonCleared = "cleared"

	// DeviceStatePending means no update command was sent to the miner yet.
	DeviceStatePending = "pending"
	// DeviceStateUpdating means a command was sent but the miner has not
	// reported the target version yet.
	DeviceStateUpdating = "updating"
	// DeviceStateVerifying means the miner reports the target version but is
	// not yet back online / hashing.
	DeviceStateVerifying = "verifying"
	// DeviceStateUpdated means the miner is on the target version, online,
	// and at least as healthy as before the update.
	DeviceStateUpdated = "updated"

	// MethodImmediate updates every mismatched target at once.
	MethodImmediate = "immediate"
	// MethodPilot updates one pilot batch, gates, then updates the rest.
	MethodPilot = "pilot"
	// MethodBatches updates fixed-size batches with a gate after each.
	MethodBatches = "batches"

	// StageBatch: only the current batch is being updated.
	StageBatch = "batch"
	// StageAwaitingReview: the current batch is done; enforcement holds
	// until the rollout is continued (manually or by auto-advance).
	StageAwaitingReview = "awaiting_review"
	// StageRest: all remaining targets are being updated (the only stage of
	// immediate rollouts).
	StageRest = "rest"

	// Activity event types.
	EventRolloutStarted     = "rollout_started"
	EventRolloutReviewReady = "rollout_review_ready"
	EventRolloutContinued   = "rollout_continued"
	EventRolloutPaused      = "rollout_paused"
	EventRolloutResumed     = "rollout_resumed"
	EventRolloutAborted     = "rollout_aborted"
	EventRolloutCompleted   = "rollout_completed"

	// deviceStatusActive is the device_status value of a hashing miner.
	deviceStatusActive = "ACTIVE"

	rolloutActorName = "rollout-enforcement"

	// resendInterval is how long the enforcement loop waits before
	// re-issuing an update command to a miner that is still mismatched
	// (e.g. it was offline or the install failed).
	resendInterval = 10 * time.Minute
)

// CommandDispatcher is the slice of the command service the rollout domain uses.
type CommandDispatcher interface {
	FirmwareUpdate(ctx context.Context, deviceSelector *commandpb.DeviceSelector, firmwareFileID string) (*command.CommandResult, error)
}

// FirmwareFiles is the slice of the firmware files service the rollout domain uses.
type FirmwareFiles interface {
	GetFirmwareMetadata(fileID string) (files.FirmwareMetadata, error)
}

// ActivityLogger records rollout lifecycle events in the activity log.
type ActivityLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
}

// Service implements rollout-lane management and firmware enforcement.
type Service struct {
	store    *sqlstores.SQLRolloutLaneStore
	commands CommandDispatcher
	files    FirmwareFiles
	activity ActivityLogger
	now      func() time.Time
}

// NewService builds the rollout service. activityLog may be nil.
func NewService(store *sqlstores.SQLRolloutLaneStore, commands CommandDispatcher, firmwareFiles FirmwareFiles, activityLog ActivityLogger) *Service {
	return &Service{store: store, commands: commands, files: firmwareFiles, activity: activityLog, now: time.Now}
}

// LaneMiner is a lane member with its currently reported model and firmware.
type LaneMiner struct {
	DeviceID         int64
	DeviceIdentifier string
	Model            string
	FirmwareVersion  string
}

// ModelGroup is the per-model view inside a lane.
type ModelGroup struct {
	Model           string
	FirmwareFileID  string
	FirmwareVersion string
	Miners          []LaneMiner
	ActiveRolloutID int64
}

// Lane is the operator-facing view of a rollout lane.
type Lane struct {
	ID          int64
	Name        string
	CreatedAt   time.Time
	ModelGroups []ModelGroup
}

// RolloutDevice is the live progress and health of one miner within a
// rollout, alongside the baseline captured when the rollout started.
type RolloutDevice struct {
	DeviceID         int64
	DeviceIdentifier string
	FirmwareVersion  string
	State            string
	// 1-based batch number; 0 when not part of a snapshotted batch.
	Batch               int32
	Status              string
	Online              bool
	Hashing             bool
	HasBaseline         bool
	BaselineHashing     bool
	HashRateHs          float64
	HasHashRate         bool
	BaselineHashRateHs  float64
	HasBaselineHashRate bool
	OpenErrors          int32
	BaselineOpenErrors  int32
}

// Evidence summarizes post-update health of the miners under review versus
// their own baselines, and whether it clears the auto-advance thresholds.
type Evidence struct {
	DevicesTotal                  int32
	Verified                      int32
	Online                        int32
	Hashing                       int32
	BaselineHashing               int32
	HashrateChangePercent         float64
	HasHashrateEvidence           bool
	BaselineHashRateHs            float64
	CurrentHashRateHs             float64
	NewErrors                     int32
	ReadyToAdvance                bool
	HoldReason                    string
	StabilizationRemainingSeconds int32
}

// Rollout is one firmware change for one model within one lane.
type Rollout struct {
	ID                      int64
	LaneID                  int64
	LaneName                string
	Model                   string
	FirmwareFileID          string
	FirmwareVersion         string
	Status                  string
	Method                  string
	Stage                   string
	BatchSize               int32
	BatchCount              int32
	CurrentBatch            int32
	PausedAt                *time.Time
	AutoAdvance             bool
	MaxHashrateDropPercent  float64
	StabilizationSeconds    int32
	PreviousFirmwareFileID  string
	PreviousFirmwareVersion string
	CancelReason            string
	StageChangedAt          time.Time
	CreatedAt               time.Time
	FinishedAt              *time.Time
	Devices                 []RolloutDevice
	// Evidence for the miners under review; nil unless the rollout is active.
	Evidence *Evidence
}

// Assignment is the desired firmware for one model within a lane. An empty
// FirmwareFileID clears the model's assignment.
type Assignment struct {
	Model          string
	FirmwareFileID string
}

// RolloutOptions selects how rollouts started by an apply call run. The
// zero value means immediate.
type RolloutOptions struct {
	Method                 string
	BatchSize              int32
	AutoAdvance            bool
	MaxHashrateDropPercent float64
	StabilizationSeconds   int32
}

func (o *RolloutOptions) validate() error {
	if o.Method == "" {
		o.Method = MethodImmediate
	}
	switch o.Method {
	case MethodImmediate:
		o.BatchSize, o.AutoAdvance, o.MaxHashrateDropPercent, o.StabilizationSeconds = 0, false, 0, 0
	case MethodPilot, MethodBatches:
		if o.BatchSize < 1 {
			return fleeterror.NewInvalidArgumentError("staged rollouts need a batch size of at least 1")
		}
	default:
		return fleeterror.NewInvalidArgumentErrorf("unknown rollout method %q", o.Method)
	}
	if o.MaxHashrateDropPercent < 0 || o.MaxHashrateDropPercent > 100 {
		return fleeterror.NewInvalidArgumentError("max hashrate drop must be between 0 and 100 percent")
	}
	if o.StabilizationSeconds < 0 {
		return fleeterror.NewInvalidArgumentError("stabilization must not be negative")
	}
	return nil
}

// AbortResult describes what an abort did to the lane.
type AbortResult struct {
	Rollout *Rollout
	Lane    *Lane
	// Rollouts started to bring already-updated miners back (at most one).
	Started []Rollout
	// True when the previous assignment was restored, false when cleared.
	RestoredPrevious bool
}

// CreateLane creates an empty rollout lane.
func (s *Service) CreateLane(ctx context.Context, orgID int64, name string) (*Lane, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fleeterror.NewInvalidArgumentError("lane name is required")
	}
	row, err := s.store.Queries(ctx).CreateRolloutLane(ctx, sqlc.CreateRolloutLaneParams{OrgID: orgID, Name: name})
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return nil, fleeterror.NewInvalidArgumentErrorf("a lane named %q already exists", name)
		}
		return nil, fleeterror.NewInternalErrorf("create lane: %v", err)
	}
	return &Lane{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt}, nil
}

// DeleteLane removes a lane with its memberships, assignments, and rollouts.
func (s *Service) DeleteLane(ctx context.Context, orgID, laneID int64) error {
	if err := s.store.Queries(ctx).DeleteRolloutLane(ctx, sqlc.DeleteRolloutLaneParams{LaneID: laneID, OrgID: orgID}); err != nil {
		return fleeterror.NewInternalErrorf("delete lane: %v", err)
	}
	return nil
}

// UpdateMembers adds and removes lane members. Added miners are moved out of
// whichever lane they were in before (single-lane membership is enforced by
// the primary key on rollout_lane_member.device_id).
func (s *Service) UpdateMembers(ctx context.Context, orgID, laneID int64, add, remove []string) (*Lane, error) {
	q := s.store.Queries(ctx)
	if _, err := q.GetRolloutLane(ctx, sqlc.GetRolloutLaneParams{LaneID: laneID, OrgID: orgID}); err != nil {
		return nil, fleeterror.NewNotFoundErrorf("lane not found: %d", laneID)
	}
	if len(add) > 0 {
		if err := q.AddRolloutLaneMembers(ctx, sqlc.AddRolloutLaneMembersParams{
			LaneID: laneID, OrgID: orgID, DeviceIdentifiers: add,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("add lane members: %v", err)
		}
	}
	if len(remove) > 0 {
		if err := q.RemoveRolloutLaneMembers(ctx, sqlc.RemoveRolloutLaneMembersParams{
			LaneID: laneID, OrgID: orgID, DeviceIdentifiers: remove,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("remove lane members: %v", err)
		}
	}
	return s.getLane(ctx, orgID, laneID)
}

// ApplyFirmware replaces per-model assignments of a lane and starts a
// rollout (with the given options) for every changed model that has
// mismatched members. Unchanged assignments are left alone; their active
// rollout, if any, keeps running.
func (s *Service) ApplyFirmware(ctx context.Context, orgID, userID, laneID int64, assignments []Assignment, opts RolloutOptions) ([]Rollout, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	q := s.store.Queries(ctx)
	lane, err := q.GetRolloutLane(ctx, sqlc.GetRolloutLaneParams{LaneID: laneID, OrgID: orgID})
	if err != nil {
		return nil, fleeterror.NewNotFoundErrorf("lane not found: %d", laneID)
	}
	existing, err := q.ListRolloutLaneFirmware(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list lane firmware: %v", err)
	}
	current := map[string]sqlc.RolloutLaneFirmware{}
	for _, f := range existing {
		if f.LaneID == laneID {
			current[f.Model] = f
		}
	}

	var started []Rollout
	for _, a := range assignments {
		if a.Model == "" {
			return nil, fleeterror.NewInvalidArgumentError("assignment model is required")
		}
		prev, hadPrev := current[a.Model]

		if a.FirmwareFileID == "" {
			if !hadPrev {
				continue
			}
			if err := q.DeleteRolloutLaneFirmware(ctx, sqlc.DeleteRolloutLaneFirmwareParams{LaneID: laneID, Model: a.Model}); err != nil {
				return nil, fleeterror.NewInternalErrorf("clear assignment: %v", err)
			}
			if err := q.CancelActiveFirmwareRollout(ctx, sqlc.CancelActiveFirmwareRolloutParams{
				LaneID: laneID, Model: a.Model, CancelReason: CancelReasonCleared,
			}); err != nil {
				return nil, fleeterror.NewInternalErrorf("cancel rollout: %v", err)
			}
			continue
		}

		if hadPrev && prev.FirmwareFileID == a.FirmwareFileID {
			continue
		}
		meta, err := s.files.GetFirmwareMetadata(a.FirmwareFileID)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentErrorf("invalid firmware file %q: %v", a.FirmwareFileID, err)
		}
		if !strings.EqualFold(meta.TargetModel, a.Model) {
			return nil, fleeterror.NewInvalidArgumentErrorf("firmware file %q targets model %q, not %q", a.FirmwareFileID, meta.TargetModel, a.Model)
		}
		if err := q.UpsertRolloutLaneFirmware(ctx, sqlc.UpsertRolloutLaneFirmwareParams{
			LaneID:         laneID,
			Model:          a.Model,
			FirmwareFileID: a.FirmwareFileID,
			// The metadata version is what miners report after installing.
			FirmwareVersion: meta.FirmwareVersion,
			AssignedBy:      userID,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("assign firmware: %v", err)
		}
		if err := q.CancelActiveFirmwareRollout(ctx, sqlc.CancelActiveFirmwareRolloutParams{
			LaneID: laneID, Model: a.Model, CancelReason: CancelReasonSuperseded,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("cancel superseded rollout: %v", err)
		}

		// Start the rollout here rather than leaving it to the enforcement
		// loop, so the operator's method applies (the loop only ever starts
		// immediate drift-correction rollouts).
		spec := rolloutSpec{
			OrgID: orgID, LaneID: laneID, LaneName: lane.Name, Model: a.Model,
			FirmwareFileID: a.FirmwareFileID, FirmwareVersion: meta.FirmwareVersion,
			CreatedBy: userID, Options: opts,
		}
		if hadPrev {
			spec.PreviousFirmwareFileID = prev.FirmwareFileID
			spec.PreviousFirmwareVersion = prev.FirmwareVersion
		}
		r, err := s.startRollout(ctx, spec)
		if err != nil {
			return nil, err
		}
		if r == nil {
			continue // every member already reports the version
		}
		s.logRolloutEvent(ctx, *r, lane.Name, EventRolloutStarted, false, nil)
		view, err := s.rolloutView(ctx, *r, lane.Name)
		if err != nil {
			return nil, err
		}
		started = append(started, *view)
	}
	return started, nil
}

// rolloutSpec is everything needed to create a rollout for one assignment.
type rolloutSpec struct {
	OrgID, LaneID                                   int64
	LaneName, Model                                 string
	FirmwareFileID, FirmwareVersion                 string
	PreviousFirmwareFileID, PreviousFirmwareVersion string
	CreatedBy                                       int64
	Options                                         RolloutOptions
}

// startRollout creates a rollout for one assignment, snapshots every
// mismatched member with its baseline health and, for staged methods, its
// batch (the first Options.BatchSize mismatched miners by identifier form
// batch 0, and so on). Returns nil when no member is mismatched.
func (s *Service) startRollout(ctx context.Context, spec rolloutSpec) (*sqlc.FirmwareRollout, error) {
	q := s.store.Queries(ctx)
	targets, err := q.ListFirmwareRolloutTargets(ctx, sqlc.ListFirmwareRolloutTargetsParams{
		RolloutID: 0, LaneID: spec.LaneID, Model: spec.Model,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list rollout targets: %v", err)
	}
	var mismatched []string
	for _, t := range targets {
		if t.FirmwareVersion != spec.FirmwareVersion {
			mismatched = append(mismatched, t.DeviceIdentifier)
		}
	}
	if len(mismatched) == 0 {
		return nil, nil
	}

	opts := spec.Options
	batchSize := int(opts.BatchSize)
	if batchSize > len(mismatched) {
		batchSize = len(mismatched)
	}
	var batches [][]string
	switch opts.Method {
	case MethodPilot:
		batches = [][]string{mismatched[:batchSize]}
	case MethodBatches:
		for start := 0; start < len(mismatched); start += batchSize {
			end := min(start+batchSize, len(mismatched))
			batches = append(batches, mismatched[start:end])
		}
	}
	stage := StageRest
	if len(batches) > 0 {
		stage = StageBatch
	}

	params := sqlc.CreateFirmwareRolloutParams{
		OrgID:                   spec.OrgID,
		LaneID:                  spec.LaneID,
		Model:                   spec.Model,
		FirmwareFileID:          spec.FirmwareFileID,
		FirmwareVersion:         spec.FirmwareVersion,
		CreatedBy:               spec.CreatedBy,
		Method:                  opts.Method,
		Stage:                   stage,
		BatchSize:               int32(batchSize),    // #nosec G115 -- capped at Options.BatchSize, an int32
		BatchCount:              int32(len(batches)), // #nosec G115 -- bounded by the member count
		AutoAdvance:             opts.AutoAdvance,
		StabilizationSeconds:    opts.StabilizationSeconds,
		PreviousFirmwareFileID:  spec.PreviousFirmwareFileID,
		PreviousFirmwareVersion: spec.PreviousFirmwareVersion,
	}
	if opts.MaxHashrateDropPercent > 0 {
		params.MaxHashrateDropPercent = sql.NullFloat64{Float64: opts.MaxHashrateDropPercent, Valid: true}
	}
	if opts.Method == MethodImmediate {
		params.BatchSize = 0
	}
	r, err := q.CreateFirmwareRollout(ctx, params)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("create rollout: %v", err)
	}

	// Baselines for every mismatched miner; batched ones also get their
	// batch index. Late joiners have no baseline and are judged on
	// version + online only.
	batched := map[string]bool{}
	for i, batch := range batches {
		if err := q.SnapshotFirmwareRolloutDevices(ctx, sqlc.SnapshotFirmwareRolloutDevicesParams{
			RolloutID:         r.ID,
			BatchIndex:        sql.NullInt32{Int32: int32(i), Valid: true}, // #nosec G115 -- batch count is bounded by the member count
			DeviceIdentifiers: batch,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("snapshot batch %d: %v", i, err)
		}
		for _, id := range batch {
			batched[id] = true
		}
	}
	var rest []string
	for _, id := range mismatched {
		if !batched[id] {
			rest = append(rest, id)
		}
	}
	if len(rest) > 0 {
		if err := q.SnapshotFirmwareRolloutDevices(ctx, sqlc.SnapshotFirmwareRolloutDevicesParams{
			RolloutID: r.ID, DeviceIdentifiers: rest,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("snapshot rollout devices: %v", err)
		}
	}
	return &r, nil
}

// ContinueRollout releases the review gate of a staged rollout: the next
// batch starts, or the rest stage when the last batch was under review.
func (s *Service) ContinueRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	row, laneName, err := s.activeRollout(ctx, orgID, rolloutID)
	if err != nil {
		return nil, err
	}
	if row.Stage != StageAwaitingReview {
		return nil, fleeterror.NewInvalidArgumentErrorf("rollout %d is not awaiting review", rolloutID)
	}
	if err := s.advance(ctx, &row); err != nil {
		return nil, err
	}
	s.logRolloutEvent(ctx, row, laneName, EventRolloutContinued, false, nil)
	return s.rolloutView(ctx, row, laneName)
}

// advance moves a rollout at the review gate to its next batch or, after
// the last batch, to the rest stage. row is updated in place.
func (s *Service) advance(ctx context.Context, row *sqlc.FirmwareRollout) error {
	stage, batch := StageRest, row.CurrentBatch
	if next := row.CurrentBatch + 1; next < row.BatchCount {
		stage, batch = StageBatch, next
	}
	n, err := s.store.Queries(ctx).AdvanceFirmwareRolloutStage(ctx, sqlc.AdvanceFirmwareRolloutStageParams{
		RolloutID: row.ID, FromStage: StageAwaitingReview, Stage: stage, CurrentBatch: batch,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("continue rollout: %v", err)
	}
	if n == 0 {
		return fleeterror.NewInvalidArgumentErrorf("rollout %d is not awaiting review", row.ID)
	}
	row.Stage, row.CurrentBatch, row.StageChangedAt = stage, batch, s.now()
	return nil
}

// PauseRollout holds an active rollout: no new commands, no transitions.
func (s *Service) PauseRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	row, laneName, err := s.activeRollout(ctx, orgID, rolloutID)
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
	s.logRolloutEvent(ctx, row, laneName, EventRolloutPaused, false, nil)
	return s.rolloutView(ctx, row, laneName)
}

// ResumeRollout lets a paused rollout continue where it left off.
func (s *Service) ResumeRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	row, laneName, err := s.activeRollout(ctx, orgID, rolloutID)
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
	s.logRolloutEvent(ctx, row, laneName, EventRolloutResumed, false, nil)
	return s.rolloutView(ctx, row, laneName)
}

// AbortRollout cancels an active rollout and makes sure enforcement does not
// simply restart it: the model's previous assignment is restored (starting an
// immediate rollout back to it for miners already updated) or, when there was
// no previous assignment, the assignment is cleared.
func (s *Service) AbortRollout(ctx context.Context, orgID, userID, rolloutID int64) (*AbortResult, error) {
	row, laneName, err := s.activeRollout(ctx, orgID, rolloutID)
	if err != nil {
		return nil, err
	}
	q := s.store.Queries(ctx)
	n, err := q.AbortFirmwareRollout(ctx, rolloutID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("abort rollout: %v", err)
	}
	if n == 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf("rollout %d is not active", rolloutID)
	}
	row.Status, row.CancelReason = StatusCanceled, CancelReasonAborted
	row.FinishedAt = sql.NullTime{Time: s.now(), Valid: true}

	result := &AbortResult{}
	canRestore := row.PreviousFirmwareFileID != "" && row.PreviousFirmwareFileID != row.FirmwareFileID
	if canRestore {
		started, err := s.ApplyFirmware(ctx, orgID, userID, row.LaneID, []Assignment{
			{Model: row.Model, FirmwareFileID: row.PreviousFirmwareFileID},
		}, RolloutOptions{})
		if err != nil {
			// The previous file may be gone; fall back to clearing so the
			// aborted change is not re-enforced.
			slog.Warn("abort: could not restore previous firmware, clearing assignment",
				"rollout_id", rolloutID, "previous_file", row.PreviousFirmwareFileID, "error", err)
			canRestore = false
		} else {
			result.Started = started
			result.RestoredPrevious = true
		}
	}
	if !canRestore {
		if err := q.DeleteRolloutLaneFirmware(ctx, sqlc.DeleteRolloutLaneFirmwareParams{LaneID: row.LaneID, Model: row.Model}); err != nil {
			return nil, fleeterror.NewInternalErrorf("clear assignment: %v", err)
		}
	}
	s.logRolloutEvent(ctx, row, laneName, EventRolloutAborted, false, map[string]any{
		"restored_previous":         result.RestoredPrevious,
		"previous_firmware_version": row.PreviousFirmwareVersion,
	})

	view, err := s.rolloutView(ctx, row, laneName)
	if err != nil {
		return nil, err
	}
	result.Rollout = view
	lane, err := s.getLane(ctx, orgID, row.LaneID)
	if err != nil {
		return nil, err
	}
	result.Lane = lane
	return result, nil
}

// activeRollout loads an active rollout of the org together with its lane
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
	lane, err := q.GetRolloutLane(ctx, sqlc.GetRolloutLaneParams{LaneID: row.LaneID, OrgID: orgID})
	if err != nil {
		return sqlc.FirmwareRollout{}, "", fleeterror.NewNotFoundErrorf("lane not found: %d", row.LaneID)
	}
	return row, lane.Name, nil
}

// RollbackFirmware re-applies the firmware of a past rollout to its lane's
// model group, rolling the group's miners back to that version. It delegates
// to ApplyFirmware so a rollback follows the exact same path as a manual
// assign: the rollout's file is re-validated, any active rollout for the
// (lane, model) pair is canceled, and a new rollout enforces the restored
// version. Returns the rollout's lane id and the started rollouts.
func (s *Service) RollbackFirmware(ctx context.Context, orgID, userID, rolloutID int64) (int64, []Rollout, error) {
	row, err := s.store.Queries(ctx).GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{
		RolloutID: rolloutID, OrgID: orgID,
	})
	if err != nil {
		return 0, nil, fleeterror.NewNotFoundErrorf("rollout not found: %d", rolloutID)
	}
	started, err := s.ApplyFirmware(ctx, orgID, userID, row.LaneID, []Assignment{
		{Model: row.Model, FirmwareFileID: row.FirmwareFileID},
	}, RolloutOptions{})
	if err != nil {
		return 0, nil, err
	}
	return row.LaneID, started, nil
}

// ListLanes returns all lanes of an org with members grouped by model.
func (s *Service) ListLanes(ctx context.Context, orgID int64) ([]Lane, error) {
	q := s.store.Queries(ctx)
	rows, err := q.ListRolloutLanes(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list lanes: %v", err)
	}
	lanes := make([]Lane, 0, len(rows))
	for _, row := range rows {
		lane, err := s.buildLane(ctx, orgID, row)
		if err != nil {
			return nil, err
		}
		lanes = append(lanes, *lane)
	}
	return lanes, nil
}

func (s *Service) getLane(ctx context.Context, orgID, laneID int64) (*Lane, error) {
	row, err := s.store.Queries(ctx).GetRolloutLane(ctx, sqlc.GetRolloutLaneParams{LaneID: laneID, OrgID: orgID})
	if err != nil {
		return nil, fleeterror.NewNotFoundErrorf("lane not found: %d", laneID)
	}
	return s.buildLane(ctx, orgID, row)
}

func (s *Service) buildLane(ctx context.Context, orgID int64, row sqlc.RolloutLane) (*Lane, error) {
	q := s.store.Queries(ctx)
	members, err := q.ListRolloutLaneMembers(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list lane members: %v", err)
	}
	firmware, err := q.ListRolloutLaneFirmware(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list lane firmware: %v", err)
	}
	active, err := q.ListActiveFirmwareRollouts(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list active rollouts: %v", err)
	}

	groups := map[string]*ModelGroup{}
	order := []string{}
	group := func(model string) *ModelGroup {
		if g, ok := groups[model]; ok {
			return g
		}
		g := &ModelGroup{Model: model}
		groups[model] = g
		order = append(order, model)
		return g
	}
	for _, m := range members {
		if m.LaneID != row.ID {
			continue
		}
		g := group(m.Model)
		g.Miners = append(g.Miners, LaneMiner{
			DeviceID:         m.DeviceID,
			DeviceIdentifier: m.DeviceIdentifier,
			Model:            m.Model,
			FirmwareVersion:  m.FirmwareVersion,
		})
	}
	for _, f := range firmware {
		if f.LaneID != row.ID {
			continue
		}
		g := group(f.Model)
		g.FirmwareFileID = f.FirmwareFileID
		g.FirmwareVersion = f.FirmwareVersion
	}
	for _, r := range active {
		if r.FirmwareRollout.LaneID != row.ID {
			continue
		}
		group(r.FirmwareRollout.Model).ActiveRolloutID = r.FirmwareRollout.ID
	}

	lane := &Lane{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt}
	for _, model := range order {
		lane.ModelGroups = append(lane.ModelGroups, *groups[model])
	}
	return lane, nil
}

// ListRollouts returns rollouts for an org (optionally one lane) with live
// per-device progress computed against current lane membership.
func (s *Service) ListRollouts(ctx context.Context, orgID int64, laneID int64) ([]Rollout, error) {
	q := s.store.Queries(ctx)
	var laneFilter sql.NullInt64
	if laneID != 0 {
		laneFilter = sql.NullInt64{Int64: laneID, Valid: true}
	}
	rows, err := q.ListFirmwareRollouts(ctx, sqlc.ListFirmwareRolloutsParams{OrgID: orgID, LaneID: laneFilter})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list rollouts: %v", err)
	}
	rollouts := make([]Rollout, 0, len(rows))
	for _, row := range rows {
		view, err := s.rolloutView(ctx, row.FirmwareRollout, row.LaneName)
		if err != nil {
			return nil, err
		}
		rollouts = append(rollouts, *view)
	}
	return rollouts, nil
}

// target is one lane member of a rollout's model with derived health.
type target struct {
	sqlc.ListFirmwareRolloutTargetsRow
}

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

// verified: on the target version, back online, and hashing if it was
// hashing before the update. A miner that was not hashing before (e.g. no
// pool configured) is not held to a standard the update cannot meet.
func (t target) verified(version string) bool {
	return t.FirmwareVersion == version && t.online() && (t.hashing() || !t.baselineHashing())
}

func (t target) state(version string) string {
	switch {
	case t.verified(version):
		return DeviceStateUpdated
	case t.FirmwareVersion == version:
		return DeviceStateVerifying
	case t.UpdateSentAt.Valid:
		return DeviceStateUpdating
	}
	return DeviceStatePending
}

func (t target) view(r sqlc.FirmwareRollout) RolloutDevice {
	state := t.state(r.FirmwareVersion)
	// A rollout completes only once every target verified, so report it
	// that way: comparing against live versions would misreport history
	// after a later rollout moves the same miners elsewhere.
	if r.Status == StatusCompleted {
		state = DeviceStateUpdated
	}
	d := RolloutDevice{
		DeviceID:            t.DeviceID,
		DeviceIdentifier:    t.DeviceIdentifier,
		FirmwareVersion:     t.FirmwareVersion,
		State:               state,
		Status:              t.Status,
		Online:              t.online(),
		Hashing:             t.hashing(),
		HasBaseline:         t.BaselineStatus.Valid,
		BaselineHashing:     t.baselineHashing(),
		HasHashRate:         t.HashRateHs.Valid,
		HashRateHs:          t.HashRateHs.Float64,
		HasBaselineHashRate: t.BaselineHashRateHs.Valid,
		BaselineHashRateHs:  t.BaselineHashRateHs.Float64,
		OpenErrors:          t.OpenErrors,
		BaselineOpenErrors:  t.BaselineOpenErrors.Int32,
	}
	if t.BatchIndex.Valid {
		d.Batch = t.BatchIndex.Int32 + 1
	}
	return d
}

func (s *Service) listTargets(ctx context.Context, r sqlc.FirmwareRollout) ([]target, error) {
	rows, err := s.store.Queries(ctx).ListFirmwareRolloutTargets(ctx, sqlc.ListFirmwareRolloutTargetsParams{
		RolloutID: r.ID, LaneID: r.LaneID, Model: r.Model,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list rollout targets: %v", err)
	}
	targets := make([]target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, target{row})
	}
	return targets, nil
}

// reviewScope is the set of targets whose evidence governs the rollout right
// now: the current batch while batching or at the gate, everything in the
// rest stage.
func reviewScope(r sqlc.FirmwareRollout, targets []target) []target {
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

// evaluate summarizes the scope's post-update health against baseline and
// decides whether the rollout may auto-advance. Missing or degraded evidence
// never advances a rollout on its own.
func (s *Service) evaluate(r sqlc.FirmwareRollout, scope []target) Evidence {
	ev := Evidence{DevicesTotal: int32(len(scope))} // #nosec G115 -- bounded by the member count
	missingSamples := 0
	for _, t := range scope {
		if t.verified(r.FirmwareVersion) {
			ev.Verified++
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
		if t.BaselineHashRateHs.Valid {
			if t.HashRateHs.Valid {
				ev.HasHashrateEvidence = true
				ev.BaselineHashRateHs += t.BaselineHashRateHs.Float64
				ev.CurrentHashRateHs += t.HashRateHs.Float64
			} else {
				missingSamples++
			}
		}
		if t.BaselineOpenErrors.Valid && t.OpenErrors > t.BaselineOpenErrors.Int32 {
			ev.NewErrors += t.OpenErrors - t.BaselineOpenErrors.Int32
		}
	}
	if ev.HasHashrateEvidence && ev.BaselineHashRateHs > 0 {
		ev.HashrateChangePercent = (ev.CurrentHashRateHs - ev.BaselineHashRateHs) / ev.BaselineHashRateHs * 100
	}

	switch {
	case r.Status != StatusActive:
		return ev
	case r.PausedAt.Valid:
		ev.HoldReason = "Paused"
	case r.Stage == StageBatch:
		ev.HoldReason = "Batch in progress"
	case r.Stage == StageRest:
		ev.HoldReason = ""
	case !r.AutoAdvance:
		ev.HoldReason = "Manual review"
	case ev.Verified < ev.DevicesTotal:
		ev.HoldReason = fmt.Sprintf("%d of %d miners not yet verified", ev.DevicesTotal-ev.Verified, ev.DevicesTotal)
	case missingSamples > 0:
		ev.HoldReason = fmt.Sprintf("No recent hashrate sample for %d miners", missingSamples)
	case ev.NewErrors > 0:
		ev.HoldReason = fmt.Sprintf("%d new errors since the update", ev.NewErrors)
	case r.MaxHashrateDropPercent.Valid && ev.HasHashrateEvidence && ev.HashrateChangePercent < -r.MaxHashrateDropPercent.Float64:
		ev.HoldReason = fmt.Sprintf("Hashrate down %.1f%% (limit %.0f%%)", -ev.HashrateChangePercent, r.MaxHashrateDropPercent.Float64)
	default:
		remaining := time.Duration(r.StabilizationSeconds)*time.Second - s.now().Sub(r.StageChangedAt)
		if remaining > 0 {
			ev.StabilizationRemainingSeconds = int32(math.Ceil(remaining.Seconds())) // #nosec G115 -- bounded by StabilizationSeconds, an int32
			ev.HoldReason = "Stabilizing"
		} else {
			ev.ReadyToAdvance = true
		}
	}
	return ev
}

func (s *Service) rolloutView(ctx context.Context, r sqlc.FirmwareRollout, laneName string) (*Rollout, error) {
	targets, err := s.listTargets(ctx, r)
	if err != nil {
		return nil, err
	}
	view := &Rollout{
		ID:                      r.ID,
		LaneID:                  r.LaneID,
		LaneName:                laneName,
		Model:                   r.Model,
		FirmwareFileID:          r.FirmwareFileID,
		FirmwareVersion:         r.FirmwareVersion,
		Status:                  r.Status,
		Method:                  r.Method,
		Stage:                   r.Stage,
		BatchSize:               r.BatchSize,
		BatchCount:              r.BatchCount,
		CurrentBatch:            r.CurrentBatch,
		AutoAdvance:             r.AutoAdvance,
		MaxHashrateDropPercent:  r.MaxHashrateDropPercent.Float64,
		StabilizationSeconds:    r.StabilizationSeconds,
		PreviousFirmwareFileID:  r.PreviousFirmwareFileID,
		PreviousFirmwareVersion: r.PreviousFirmwareVersion,
		CancelReason:            r.CancelReason,
		StageChangedAt:          r.StageChangedAt,
		CreatedAt:               r.CreatedAt,
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
	if r.Status == StatusActive {
		ev := s.evaluate(r, reviewScope(r, targets))
		view.Evidence = &ev
	}
	return view, nil
}

// EnforceTick runs one enforcement pass: it starts rollouts for assignments
// with mismatched members and drives every active rollout forward.
func (s *Service) EnforceTick(ctx context.Context) {
	if err := s.startNeededRollouts(ctx); err != nil {
		slog.Error("rollout enforcement: start rollouts", "error", err)
	}
	active, err := s.store.Queries(ctx).ListActiveFirmwareRollouts(ctx)
	if err != nil {
		slog.Error("rollout enforcement: list active rollouts", "error", err)
		return
	}
	for _, row := range active {
		if err := s.enforceRollout(ctx, row.FirmwareRollout, row.LaneName); err != nil {
			slog.Error("rollout enforcement", "rollout_id", row.FirmwareRollout.ID, "error", err)
		}
	}
}

// startNeededRollouts creates an immediate rollout for every assignment that
// has at least one mismatched member and no active rollout: late joiners and
// miners that drifted off the assigned version. No operator is present to
// review a gate, so these never stage.
func (s *Service) startNeededRollouts(ctx context.Context) error {
	q := s.store.Queries(ctx)
	needed, err := q.ListRolloutLaneFirmwareNeedingRollout(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("find assignments needing rollout: %v", err)
	}
	for _, n := range needed {
		if _, err := s.startRollout(ctx, rolloutSpec{
			OrgID: n.OrgID, LaneID: n.LaneID, Model: n.Model,
			FirmwareFileID: n.FirmwareFileID, FirmwareVersion: n.FirmwareVersion,
			// The assignment did not change, so "previous" is the assignment
			// itself; abort then clears rather than restores.
			PreviousFirmwareFileID: n.FirmwareFileID, PreviousFirmwareVersion: n.FirmwareVersion,
			CreatedBy: n.AssignedBy, Options: RolloutOptions{Method: MethodImmediate},
		}); err != nil {
			return err
		}
	}
	return nil
}

// enforceRollout drives one active rollout forward according to its stage.
// Paused rollouts are left alone. The batch stage updates only the current
// batch and parks the rollout at the review gate once the batch is verified;
// the gate holds until continued (manually, or by auto-advance once the
// evidence clears the thresholds); the rest stage (and every immediate
// rollout) updates all mismatched targets and completes the rollout once
// every target is verified.
func (s *Service) enforceRollout(ctx context.Context, r sqlc.FirmwareRollout, laneName string) error {
	if r.PausedAt.Valid {
		return nil
	}
	targets, err := s.listTargets(ctx, r)
	if err != nil {
		return err
	}
	scope := reviewScope(r, targets)

	switch r.Stage {
	case StageAwaitingReview:
		if !r.AutoAdvance {
			return nil
		}
		ev := s.evaluate(r, scope)
		if !ev.ReadyToAdvance {
			return nil
		}
		if err := s.advance(ctx, &r); err != nil {
			return err
		}
		s.logRolloutEvent(ctx, r, laneName, EventRolloutContinued, true, map[string]any{
			"auto_advanced":           true,
			"hashrate_change_percent": ev.HashrateChangePercent,
		})
		return nil

	case StageBatch:
		if allVerified(scope, r.FirmwareVersion) {
			n, err := s.store.Queries(ctx).AdvanceFirmwareRolloutStage(ctx, sqlc.AdvanceFirmwareRolloutStageParams{
				RolloutID: r.ID, FromStage: StageBatch, Stage: StageAwaitingReview, CurrentBatch: r.CurrentBatch,
			})
			if err != nil {
				return err
			}
			if n > 0 {
				r.Stage = StageAwaitingReview
				s.logRolloutEvent(ctx, r, laneName, EventRolloutReviewReady, true, map[string]any{
					"batch": r.CurrentBatch + 1, "batch_count": r.BatchCount,
				})
			}
			return nil
		}
		return s.dispatchUpdates(ctx, r, scope)

	default:
		if allVerified(scope, r.FirmwareVersion) {
			n, err := s.store.Queries(ctx).CompleteFirmwareRollout(ctx, r.ID)
			if err != nil {
				return err
			}
			if n > 0 {
				r.Status = StatusCompleted
				s.logRolloutEvent(ctx, r, laneName, EventRolloutCompleted, true, nil)
			}
			return nil
		}
		return s.dispatchUpdates(ctx, r, scope)
	}
}

func allVerified(scope []target, version string) bool {
	for _, t := range scope {
		if !t.verified(version) {
			return false
		}
	}
	return true
}

// dispatchUpdates sends the firmware update to every mismatched target in
// scope that was not attempted within resendInterval.
func (s *Service) dispatchUpdates(ctx context.Context, r sqlc.FirmwareRollout, scope []target) error {
	var toSend []string
	cutoff := s.now().Add(-resendInterval)
	for _, t := range scope {
		if t.FirmwareVersion == r.FirmwareVersion {
			continue
		}
		if !t.UpdateSentAt.Valid || t.UpdateSentAt.Time.Before(cutoff) {
			toSend = append(toSend, t.DeviceIdentifier)
		}
	}
	if len(toSend) == 0 {
		return nil
	}
	selector := &commandpb.DeviceSelector{
		SelectionType: &commandpb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: toSend},
		},
	}
	result, err := s.commands.FirmwareUpdate(s.enforcementContext(ctx, r), selector, r.FirmwareFileID)
	if err != nil {
		return err
	}
	slog.Info("rollout enforcement dispatched firmware updates",
		"rollout_id", r.ID, "lane_id", r.LaneID, "model", r.Model, "stage", r.Stage,
		"dispatched", result.DispatchedCount, "skipped", len(result.Skipped))
	// Mark every attempted device (dispatched or preflight-skipped) so the
	// next attempt waits for resendInterval instead of retrying each tick.
	return s.store.Queries(ctx).MarkFirmwareRolloutDevicesSent(ctx, sqlc.MarkFirmwareRolloutDevicesSentParams{
		RolloutID: r.ID, DeviceIdentifiers: toSend,
	})
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

// logRolloutEvent records a rollout lifecycle event. system marks events
// raised by the enforcement loop rather than an operator; operator events
// take their actor from the request session.
func (s *Service) logRolloutEvent(ctx context.Context, r sqlc.FirmwareRollout, laneName, eventType string, system bool, extra map[string]any) {
	if s.activity == nil {
		return
	}
	orgID := r.OrgID
	metadata := map[string]any{
		"rollout_id":       r.ID,
		"lane_id":          r.LaneID,
		"lane_name":        laneName,
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
		Description:    fmt.Sprintf("%s: %s %s → %s", eventDescriptions[eventType], laneName, r.Model, r.FirmwareVersion),
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
	EventRolloutStarted:     "Started firmware rollout",
	EventRolloutReviewReady: "Firmware rollout ready for review",
	EventRolloutContinued:   "Continued firmware rollout",
	EventRolloutPaused:      "Paused firmware rollout",
	EventRolloutResumed:     "Resumed firmware rollout",
	EventRolloutAborted:     "Aborted firmware rollout",
	EventRolloutCompleted:   "Completed firmware rollout",
}
