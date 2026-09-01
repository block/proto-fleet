// Package rollout implements firmware rollout lanes: containers of miners
// grouped by model, with one optional firmware assignment per model. Once a
// model has an assignment, the enforcement loop updates every lane member of
// that model that is not running the assigned version. Each enforcement run
// for one (lane, model) pair is tracked as a rollout.
package rollout

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/authn"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const (
	// StatusActive marks a rollout that is still enforcing its firmware.
	StatusActive = "active"
	// StatusCompleted marks a rollout whose targets all reported the version.
	StatusCompleted = "completed"
	// StatusCanceled marks a rollout superseded or cleared before completion.
	StatusCanceled = "canceled"

	// DeviceStatePending means no update command was sent to the miner yet.
	DeviceStatePending = "pending"
	// DeviceStateUpdating means a command was sent but the miner has not
	// reported the target version yet.
	DeviceStateUpdating = "updating"
	// DeviceStateUpdated means the miner reports the target version.
	DeviceStateUpdated = "updated"

	// MethodImmediate updates every mismatched target at once.
	MethodImmediate = "immediate"
	// MethodPilot updates a pilot cohort first and gates the rest behind an
	// operator review.
	MethodPilot = "pilot"

	// StagePilot: only the pilot cohort is being updated.
	StagePilot = "pilot"
	// StageAwaitingReview: the pilot cohort is done; enforcement is paused
	// until an operator continues the rollout.
	StageAwaitingReview = "awaiting_review"
	// StageRest: all targets are being updated (the only stage of
	// immediate rollouts).
	StageRest = "rest"

	// CohortPilot marks a device snapshotted into the pilot cohort.
	CohortPilot = "pilot"

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

// Service implements rollout-lane management and firmware enforcement.
type Service struct {
	store    *sqlstores.SQLRolloutLaneStore
	commands CommandDispatcher
	files    FirmwareFiles
}

func NewService(store *sqlstores.SQLRolloutLaneStore, commands CommandDispatcher, firmwareFiles FirmwareFiles) *Service {
	return &Service{store: store, commands: commands, files: firmwareFiles}
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

// RolloutDevice is the live progress of one miner within a rollout.
type RolloutDevice struct {
	DeviceID         int64
	DeviceIdentifier string
	FirmwareVersion  string
	State            string
	InPilotCohort    bool
}

// Rollout is one firmware change for one model within one lane.
type Rollout struct {
	ID              int64
	LaneID          int64
	LaneName        string
	Model           string
	FirmwareFileID  string
	FirmwareVersion string
	Status          string
	Method          string
	Stage           string
	PilotCount      int32
	CreatedAt       time.Time
	FinishedAt      *time.Time
	Devices         []RolloutDevice
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
	Method     string
	PilotCount int32
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

// ApplyFirmware replaces per-model assignments of a lane and starts rollouts
// for changed models that have mismatched members. Unchanged assignments are
// left alone (their active rollout, if any, keeps running). With the pilot
// method, each started rollout snapshots a pilot cohort and gates the rest
// of the miners behind an operator review.
func (s *Service) ApplyFirmware(ctx context.Context, orgID, userID, laneID int64, assignments []Assignment, opts RolloutOptions) ([]Rollout, error) {
	if opts.Method == "" {
		opts.Method = MethodImmediate
	}
	if opts.Method != MethodImmediate && opts.Method != MethodPilot {
		return nil, fleeterror.NewInvalidArgumentErrorf("unknown rollout method %q", opts.Method)
	}
	if opts.Method == MethodPilot && opts.PilotCount < 1 {
		return nil, fleeterror.NewInvalidArgumentError("pilot rollouts need a pilot count of at least 1")
	}

	q := s.store.Queries(ctx)
	if _, err := q.GetRolloutLane(ctx, sqlc.GetRolloutLaneParams{LaneID: laneID, OrgID: orgID}); err != nil {
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

	// Models whose assignment changed to a new firmware in this call; only
	// these get pilot rollouts when the pilot method is requested.
	var changed []sqlc.UpsertRolloutLaneFirmwareParams
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
			if err := q.CancelActiveFirmwareRollout(ctx, sqlc.CancelActiveFirmwareRolloutParams{LaneID: laneID, Model: a.Model}); err != nil {
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
		params := sqlc.UpsertRolloutLaneFirmwareParams{
			LaneID:         laneID,
			Model:          a.Model,
			FirmwareFileID: a.FirmwareFileID,
			// The metadata version is what miners report after installing.
			FirmwareVersion: meta.FirmwareVersion,
			AssignedBy:      userID,
		}
		if err := q.UpsertRolloutLaneFirmware(ctx, params); err != nil {
			return nil, fleeterror.NewInternalErrorf("assign firmware: %v", err)
		}
		if err := q.CancelActiveFirmwareRollout(ctx, sqlc.CancelActiveFirmwareRolloutParams{LaneID: laneID, Model: a.Model}); err != nil {
			return nil, fleeterror.NewInternalErrorf("cancel superseded rollout: %v", err)
		}
		changed = append(changed, params)
	}

	var started []Rollout
	if opts.Method == MethodPilot {
		// Create the changed models' rollouts here, before the enforcement
		// loop can start immediate ones for the now-mismatched assignments.
		for _, c := range changed {
			r, err := s.startPilotRollout(ctx, orgID, userID, c, opts.PilotCount)
			if err != nil {
				return nil, err
			}
			if r == nil {
				continue // no mismatched members, nothing to roll out
			}
			view, err := s.rolloutView(ctx, *r, "")
			if err != nil {
				return nil, err
			}
			started = append(started, *view)
		}
		return started, nil
	}

	created, err := s.startNeededRollouts(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range created {
		if r.LaneID != laneID {
			continue
		}
		view, err := s.rolloutView(ctx, r, "")
		if err != nil {
			return nil, err
		}
		started = append(started, *view)
	}
	return started, nil
}

// startPilotRollout creates a pilot rollout for one changed assignment and
// snapshots its pilot cohort: the first pilotCount mismatched members by
// identifier. Returns nil when no member is mismatched (nothing to enforce;
// the drift loop covers later joiners).
func (s *Service) startPilotRollout(ctx context.Context, orgID, userID int64, a sqlc.UpsertRolloutLaneFirmwareParams, pilotCount int32) (*sqlc.FirmwareRollout, error) {
	q := s.store.Queries(ctx)
	targets, err := q.ListFirmwareRolloutTargets(ctx, sqlc.ListFirmwareRolloutTargetsParams{
		RolloutID: 0, LaneID: a.LaneID, Model: a.Model,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list pilot targets: %v", err)
	}
	var mismatched []string
	for _, t := range targets {
		if t.FirmwareVersion != a.FirmwareVersion {
			mismatched = append(mismatched, t.DeviceIdentifier)
		}
	}
	if len(mismatched) == 0 {
		return nil, nil
	}
	cohort := mismatched
	if int(pilotCount) < len(cohort) {
		cohort = cohort[:pilotCount]
	}
	r, err := q.CreateFirmwareRollout(ctx, sqlc.CreateFirmwareRolloutParams{
		OrgID:           orgID,
		LaneID:          a.LaneID,
		Model:           a.Model,
		FirmwareFileID:  a.FirmwareFileID,
		FirmwareVersion: a.FirmwareVersion,
		CreatedBy:       userID,
		Method:          MethodPilot,
		Stage:           StagePilot,
		PilotCount:      int32(len(cohort)), // #nosec G115 -- capped at pilotCount, an int32
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("create pilot rollout: %v", err)
	}
	if err := q.InsertFirmwareRolloutCohort(ctx, sqlc.InsertFirmwareRolloutCohortParams{
		RolloutID: r.ID, Cohort: CohortPilot, DeviceIdentifiers: cohort,
	}); err != nil {
		return nil, fleeterror.NewInternalErrorf("snapshot pilot cohort: %v", err)
	}
	return &r, nil
}

// ContinueRollout advances a pilot rollout that is awaiting review to its
// rest stage; the next enforcement pass updates the remaining miners.
func (s *Service) ContinueRollout(ctx context.Context, orgID, rolloutID int64) (*Rollout, error) {
	q := s.store.Queries(ctx)
	row, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rolloutID, OrgID: orgID})
	if err != nil {
		return nil, fleeterror.NewNotFoundErrorf("rollout not found: %d", rolloutID)
	}
	if row.Status != StatusActive || row.Method != MethodPilot || row.Stage != StageAwaitingReview {
		return nil, fleeterror.NewInvalidArgumentErrorf("rollout %d is not awaiting review", rolloutID)
	}
	n, err := q.SetFirmwareRolloutStage(ctx, sqlc.SetFirmwareRolloutStageParams{
		RolloutID: rolloutID, FromStage: StageAwaitingReview, Stage: StageRest,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("continue rollout: %v", err)
	}
	if n == 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf("rollout %d is not awaiting review", rolloutID)
	}
	row.Stage = StageRest
	return s.rolloutView(ctx, row, "")
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
		if r.LaneID != row.ID {
			continue
		}
		group(r.Model).ActiveRolloutID = r.ID
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
		view, err := s.rolloutView(ctx, sqlc.FirmwareRollout{
			ID: row.ID, OrgID: row.OrgID, LaneID: row.LaneID, Model: row.Model,
			FirmwareFileID: row.FirmwareFileID, FirmwareVersion: row.FirmwareVersion,
			Status: row.Status, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
			FinishedAt: row.FinishedAt,
			Method:     row.Method, Stage: row.Stage, PilotCount: row.PilotCount,
		}, row.LaneName)
		if err != nil {
			return nil, err
		}
		rollouts = append(rollouts, *view)
	}
	return rollouts, nil
}

func (s *Service) rolloutView(ctx context.Context, r sqlc.FirmwareRollout, laneName string) (*Rollout, error) {
	targets, err := s.store.Queries(ctx).ListFirmwareRolloutTargets(ctx, sqlc.ListFirmwareRolloutTargetsParams{
		RolloutID: r.ID, LaneID: r.LaneID, Model: r.Model,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list rollout targets: %v", err)
	}
	view := &Rollout{
		ID:              r.ID,
		LaneID:          r.LaneID,
		LaneName:        laneName,
		Model:           r.Model,
		FirmwareFileID:  r.FirmwareFileID,
		FirmwareVersion: r.FirmwareVersion,
		Status:          r.Status,
		Method:          r.Method,
		Stage:           r.Stage,
		PilotCount:      r.PilotCount,
		CreatedAt:       r.CreatedAt,
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time
		view.FinishedAt = &t
	}
	for _, t := range targets {
		state := DeviceStatePending
		switch {
		// A rollout completes only once every target matched, so report it
		// that way: comparing against live versions would misreport history
		// after a later rollout moves the same miners elsewhere.
		case r.Status == StatusCompleted:
			state = DeviceStateUpdated
		case t.FirmwareVersion == r.FirmwareVersion:
			state = DeviceStateUpdated
		case t.UpdateSentAt.Valid:
			state = DeviceStateUpdating
		}
		view.Devices = append(view.Devices, RolloutDevice{
			DeviceID:         t.DeviceID,
			DeviceIdentifier: t.DeviceIdentifier,
			FirmwareVersion:  t.FirmwareVersion,
			State:            state,
			InPilotCohort:    t.Cohort == CohortPilot,
		})
	}
	return view, nil
}

// EnforceTick runs one enforcement pass: it starts rollouts for assignments
// with mismatched members and drives every active rollout forward.
func (s *Service) EnforceTick(ctx context.Context) {
	if _, err := s.startNeededRollouts(ctx); err != nil {
		slog.Error("rollout enforcement: start rollouts", "error", err)
	}
	active, err := s.store.Queries(ctx).ListActiveFirmwareRollouts(ctx)
	if err != nil {
		slog.Error("rollout enforcement: list active rollouts", "error", err)
		return
	}
	for _, row := range active {
		r := sqlc.FirmwareRollout{
			ID: row.ID, OrgID: row.OrgID, LaneID: row.LaneID, Model: row.Model,
			FirmwareFileID: row.FirmwareFileID, FirmwareVersion: row.FirmwareVersion,
			Status: row.Status, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
			Method: row.Method, Stage: row.Stage, PilotCount: row.PilotCount,
		}
		if err := s.enforceRollout(ctx, r); err != nil {
			slog.Error("rollout enforcement", "rollout_id", r.ID, "error", err)
		}
	}
}

// startNeededRollouts creates a rollout for every assignment that has at
// least one mismatched member and no active rollout.
func (s *Service) startNeededRollouts(ctx context.Context) ([]sqlc.FirmwareRollout, error) {
	q := s.store.Queries(ctx)
	needed, err := q.ListRolloutLaneFirmwareNeedingRollout(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("find assignments needing rollout: %v", err)
	}
	var created []sqlc.FirmwareRollout
	for _, n := range needed {
		// Drift-correction rollouts (late joiners, miners off the assigned
		// version) always run immediately: no operator is present to review
		// a gate.
		r, err := q.CreateFirmwareRollout(ctx, sqlc.CreateFirmwareRolloutParams{
			OrgID:           n.OrgID,
			LaneID:          n.LaneID,
			Model:           n.Model,
			FirmwareFileID:  n.FirmwareFileID,
			FirmwareVersion: n.FirmwareVersion,
			CreatedBy:       n.AssignedBy,
			Method:          MethodImmediate,
			Stage:           StageRest,
			PilotCount:      0,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("create rollout: %v", err)
		}
		created = append(created, r)
	}
	return created, nil
}

// enforceRollout drives one active rollout forward according to its stage:
// the pilot stage updates only the snapshotted cohort and then parks the
// rollout at the review gate; awaiting_review does nothing; the rest stage
// (and every immediate rollout) updates all mismatched targets and completes
// the rollout once every target matches.
func (s *Service) enforceRollout(ctx context.Context, r sqlc.FirmwareRollout) error {
	if r.Stage == StageAwaitingReview {
		return nil
	}
	q := s.store.Queries(ctx)
	targets, err := q.ListFirmwareRolloutTargets(ctx, sqlc.ListFirmwareRolloutTargetsParams{
		RolloutID: r.ID, LaneID: r.LaneID, Model: r.Model,
	})
	if err != nil {
		return err
	}
	if r.Stage == StagePilot {
		// Only the cohort counts in the pilot stage. Cohort members that
		// left the lane drop out of the targets, so the gate is reached
		// once every remaining cohort member reports the version.
		var cohort []sqlc.ListFirmwareRolloutTargetsRow
		for _, t := range targets {
			if t.Cohort == CohortPilot {
				cohort = append(cohort, t)
			}
		}
		targets = cohort
	}

	var toSend []string
	mismatched := false
	cutoff := time.Now().Add(-resendInterval)
	for _, t := range targets {
		if t.FirmwareVersion == r.FirmwareVersion {
			continue
		}
		mismatched = true
		if !t.UpdateSentAt.Valid || t.UpdateSentAt.Time.Before(cutoff) {
			toSend = append(toSend, t.DeviceIdentifier)
		}
	}
	if !mismatched {
		if r.Stage == StagePilot {
			// Pilot done: park at the review gate instead of completing.
			_, err := q.SetFirmwareRolloutStage(ctx, sqlc.SetFirmwareRolloutStageParams{
				RolloutID: r.ID, FromStage: StagePilot, Stage: StageAwaitingReview,
			})
			return err
		}
		return q.CompleteFirmwareRollout(ctx, r.ID)
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
	return q.MarkFirmwareRolloutDevicesSent(ctx, sqlc.MarkFirmwareRolloutDevicesSentParams{
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
