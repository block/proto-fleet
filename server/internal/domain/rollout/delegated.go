package rollout

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// DeviceSelection chooses named queued targets or the next Count in snapshot
// order. Exactly one selection must be supplied.
type DeviceSelection struct {
	DeviceIdentifiers []string
	Count             int32
}

func validateIdentifiers(identifiers []string) error {
	if len(identifiers) == 0 || len(identifiers) > 1000 {
		return fleeterror.NewInvalidArgumentError("select between 1 and 1000 devices")
	}
	seen := make(map[string]bool, len(identifiers))
	for _, id := range identifiers {
		if id == "" || !utf8.ValidString(id) || utf8.RuneCountInString(id) > 255 || strings.ContainsRune(id, 0) || seen[id] {
			return fleeterror.NewInvalidArgumentError("device identifiers must be nonempty, unique, and at most 255 characters")
		}
		seen[id] = true
	}
	return nil
}

func validateMutation(m Mutation) error {
	if m.ExpectedRevision < 0 || !utf8.ValidString(m.Note) || utf8.RuneCountInString(m.Note) > 1024 || strings.ContainsRune(m.Note, 0) {
		return fleeterror.NewInvalidArgumentError("invalid revision or action note")
	}
	return nil
}

func selectQueued(r sqlc.FirmwareRollout, targets []target, selection DeviceSelection) ([]target, error) {
	named := map[string]bool{}
	for _, id := range selection.DeviceIdentifiers {
		named[id] = true
	}
	selected := make([]target, 0)
	for _, t := range targets {
		if selection.Count > 0 {
			if t.phase(r) == PhaseQueued && t.IsChannelMember && len(selected) < int(selection.Count) {
				selected = append(selected, t)
			}
		} else if named[t.DeviceIdentifier] && t.phase(r) == PhaseQueued && t.IsChannelMember {
			selected = append(selected, t)
			delete(named, t.DeviceIdentifier)
		}
	}
	if len(named) > 0 {
		var rejected []string
		for _, id := range selection.DeviceIdentifiers {
			if named[id] {
				rejected = append(rejected, id)
			}
		}
		return nil, reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonDeviceNotQueued, DeviceIdentifiers: rejected}, "selected devices are not queued in rollout %d", r.ID)
	}
	return selected, nil
}

func (s *Service) currentAssignment(ctx context.Context, r sqlc.FirmwareRollout) error {
	assignment, err := s.store.GetQueries(ctx).GetReleaseChannelFirmware(ctx, sqlc.GetReleaseChannelFirmwareParams{
		ChannelID: r.ChannelID, Manufacturer: r.Manufacturer, Model: r.Model,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewInternalErrorf("load firmware assignment: %w", err)
	}
	if err != nil || assignment.AssignmentGeneration != r.AssignmentGeneration || assignment.FirmwareChecksum != r.FirmwareChecksum || assignment.FirmwareVersion != r.FirmwareVersion {
		return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonStaleGeneration}, "rollout %d is not current", r.ID)
	}
	return nil
}

// AdvanceRollout admits the complete selection before invoking the same
// dispatch implementation as autonomous rollouts. Its transaction cannot be
// retried because command dispatch may have effects outside the database.
func (s *Service) AdvanceRollout(ctx context.Context, orgID, rolloutID int64, selection DeviceSelection, m Mutation) (*Rollout, []string, error) {
	if err := validateMutation(m); err != nil {
		return nil, nil, err
	}
	if (selection.Count > 0) == (len(selection.DeviceIdentifiers) > 0) || selection.Count < 0 || selection.Count > 1000 {
		return nil, nil, fleeterror.NewInvalidArgumentError("select devices or a count between 1 and 1000")
	}
	if len(selection.DeviceIdentifiers) > 0 {
		if err := validateIdentifiers(selection.DeviceIdentifiers); err != nil {
			return nil, nil, err
		}
	}
	var view *Rollout
	var sent []string
	var release func()
	defer func() {
		if release != nil {
			release()
		}
	}()
	err := s.tx.RunInTxNoRetry(ctx, func(ctx context.Context) error {
		if err := s.lockEventStream(ctx, orgID); err != nil {
			return err
		}
		q := s.store.GetQueries(ctx)
		observed, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rolloutID, OrgID: orgID})
		if err != nil {
			return rolloutLookupError(rolloutID, err)
		}
		channel, err := q.GetReleaseChannelForUpdate(ctx, sqlc.GetReleaseChannelForUpdateParams{ChannelID: observed.ChannelID, OrgID: orgID})
		if err != nil {
			return channelLookupError(observed.ChannelID, err)
		}
		r, _, err := s.lockRollout(ctx, orgID, rolloutID, m.ExpectedRevision)
		if err != nil {
			return err
		}
		if err := requireDelegated(r); err != nil {
			return err
		}
		if r.PausedAt.Valid {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonPaused}, "rollout %d is paused", rolloutID)
		}
		if err := s.currentAssignment(ctx, r); err != nil {
			return err
		}
		targets, err := s.listTargets(ctx, r)
		if err != nil {
			return err
		}
		selected, err := selectQueued(r, targets, selection)
		if err != nil {
			return err
		}
		// Pending commands and incompatible hardware cannot be dispatched.
		// Reject before any target is changed or any new command is queued.
		for _, t := range selected {
			if !t.InScope.Valid || !t.InScope.Bool {
				return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonDeviceNotDispatchable, DeviceIdentifiers: []string{t.DeviceIdentifier}}, "selected device %s no longer matches the assigned firmware", t.DeviceIdentifier)
			}
			if len(t.PendingFirmwareChecksums) > 0 || len(t.PendingLegacyFirmwareFileIds) > 0 {
				return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonDeviceNotDispatchable, DeviceIdentifiers: []string{t.DeviceIdentifier}}, "selected device %s has a pending firmware command", t.DeviceIdentifier)
			}
		}
		budget, err := s.refreshOfflineBudget(ctx, r.ChannelID, channel.MaxConcurrentOffline)
		if err != nil {
			return err
		}
		if room := budget.room(); room >= 0 && len(selected) > room {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonOfflineBudgetFull}, "offline capacity cannot admit all %d selected devices", len(selected))
		}
		if len(selected) > 0 {
			release, err = s.files.PinFirmwareArtifact(r.FirmwareChecksum)
			if err != nil {
				return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonArtifactMissing}, "firmware artifact is unavailable: %v", err)
			}
			if _, available := s.files.FindFirmwareFileIDByChecksum(r.FirmwareChecksum); !available {
				return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonArtifactMissing}, "firmware artifact is unavailable")
			}
			_, sent, err = s.dispatchLockedBatch(ctx, r, selected, budget)
			if err != nil {
				return err
			}
			if len(sent) != len(selected) {
				actual := make(map[string]bool, len(sent))
				for _, id := range sent {
					actual[id] = true
				}
				var rejected []string
				for _, target := range selected {
					if !actual[target.DeviceIdentifier] {
						rejected = append(rejected, target.DeviceIdentifier)
					}
				}
				// The production dispatcher queues commands and defers publish
				// until this transaction commits. Roll back its complete batch
				// when a preflight filter rejected any selected target.
				return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonDeviceNotDispatchable, DeviceIdentifiers: rejected}, "selected targets failed command preflight; no updates were queued")
			}
		}
		if err := q.RecordFirmwareRolloutAction(ctx, sqlc.RecordFirmwareRolloutActionParams{RolloutID: r.ID, ActorType: m.Actor.Type, ActorID: m.Actor.ID, ActorName: m.Actor.Name}); err != nil {
			return fleeterror.NewInternalErrorf("record controller action: %w", err)
		}
		waiting := sql.NullTime{}
		if len(sent) == 0 && !hasUpdatesInFlight(r, targets) {
			waiting = sql.NullTime{Time: s.now(), Valid: true}
		}
		if err := s.setControllerWait(ctx, r.ID, waiting); err != nil {
			return err
		}
		if err := s.logRolloutEvent(ctx, r, channel.Name, EventRolloutAdvanced, false, m.extra(map[string]any{"device_identifiers": sent})); err != nil {
			return err
		}
		view, err = s.refreshView(ctx, orgID, rolloutID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return view, sent, nil
}

func requireDelegated(r sqlc.FirmwareRollout) error {
	if r.Status != StatusActive {
		return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonNotActive}, "rollout %d is not active", r.ID)
	}
	if r.BehaviorSnapshot.Method != MethodDelegated {
		return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonNotDelegated}, "rollout %d is not delegated", r.ID)
	}
	return nil
}

func hasUpdatesInFlight(r sqlc.FirmwareRollout, targets []target) bool {
	for _, t := range targets {
		if p := t.phase(r); p == PhaseInProgress || p == PhaseRetrying {
			return true
		}
	}
	return false
}

func (s *Service) skipTargets(ctx context.Context, r sqlc.FirmwareRollout, targets []target, m Mutation) error {
	ids := make([]int64, 0, len(targets))
	for _, t := range targets {
		ids = append(ids, t.DeviceID)
	}
	if len(ids) > 0 {
		if err := s.store.GetQueries(ctx).SkipFirmwareRolloutDevices(ctx, sqlc.SkipFirmwareRolloutDevicesParams{RolloutID: r.ID, DeviceIds: ids, Note: m.Note}); err != nil {
			return fleeterror.NewInternalErrorf("skip rollout devices: %w", err)
		}
	}
	return nil
}

// SkipRolloutDevices is available for every active rollout, including a paused
// rollout. It preserves the assignment-generation suppression used by retries.
func (s *Service) SkipRolloutDevices(ctx context.Context, orgID, rolloutID int64, identifiers []string, m Mutation) (*Rollout, error) {
	if err := validateMutation(m); err != nil {
		return nil, err
	}
	if err := validateIdentifiers(identifiers); err != nil {
		return nil, err
	}
	return s.mutateActive(ctx, orgID, rolloutID, m, func(ctx context.Context, r *sqlc.FirmwareRollout, channelName string) error {
		targets, err := s.listTargets(ctx, *r)
		if err != nil {
			return err
		}
		selected, err := selectQueued(*r, targets, DeviceSelection{DeviceIdentifiers: identifiers})
		if err != nil {
			return err
		}
		if err := s.skipTargets(ctx, *r, selected, m); err != nil {
			return err
		}
		if err := s.store.GetQueries(ctx).RecordFirmwareRolloutAction(ctx, sqlc.RecordFirmwareRolloutActionParams{RolloutID: r.ID, ActorType: m.Actor.Type, ActorID: m.Actor.ID, ActorName: m.Actor.Name}); err != nil {
			return fleeterror.NewInternalErrorf("record skip action: %w", err)
		}
		if r.BehaviorSnapshot.Method == MethodDelegated && !hasUpdatesInFlight(*r, targets) {
			if err := s.setControllerWait(ctx, r.ID, sql.NullTime{Time: s.now(), Valid: true}); err != nil {
				return err
			}
		}
		return s.logRolloutEvent(ctx, *r, channelName, EventRolloutDevicesSkipped, false, m.extra(map[string]any{"device_identifiers": identifiers}))
	})
}

// CompleteRollout settles remaining queued targets without sending firmware.
func (s *Service) CompleteRollout(ctx context.Context, orgID, rolloutID int64, m Mutation) (*Rollout, error) {
	if err := validateMutation(m); err != nil {
		return nil, err
	}
	return s.mutateActive(ctx, orgID, rolloutID, m, func(ctx context.Context, r *sqlc.FirmwareRollout, channelName string) error {
		if err := requireDelegated(*r); err != nil {
			return err
		}
		if err := s.currentAssignment(ctx, *r); err != nil {
			return err
		}
		targets, err := s.listTargets(ctx, *r)
		if err != nil {
			return err
		}
		if hasUpdatesInFlight(*r, targets) {
			return reason(fleeterror.NewFailedPreconditionErrorf, ErrorInfo{Reason: ReasonUpdatesInFlight}, "rollout %d still has updates in flight", r.ID)
		}
		var queued []target
		for _, t := range targets {
			if t.phase(*r) == PhaseQueued {
				queued = append(queued, t)
			}
		}
		if err := s.skipTargets(ctx, *r, queued, m); err != nil {
			return err
		}
		if len(queued) > 0 {
			var names []string
			for _, target := range queued {
				names = append(names, target.DeviceIdentifier)
			}
			if err := s.logRolloutEvent(ctx, *r, channelName, EventRolloutDevicesSkipped, false, m.extra(map[string]any{"device_identifiers": names})); err != nil {
				return err
			}
		}
		return s.completeDelegated(ctx, *r, channelName, targets, m)
	})
}

func (s *Service) completeDelegated(ctx context.Context, r sqlc.FirmwareRollout, channelName string, targets []target, m Mutation) error {
	status, eventType := StatusCompleted, EventRolloutCompleted
	if anyFailed(activeTargets(targets)) {
		status, eventType = StatusCompletedWithFailures, EventRolloutCompletedWithFailures
	}
	if err := s.store.GetQueries(ctx).CompleteDelegatedFirmwareRollout(ctx, sqlc.CompleteDelegatedFirmwareRolloutParams{
		RolloutID: r.ID, Status: status, ActorType: m.Actor.Type, ActorID: m.Actor.ID, ActorName: m.Actor.Name,
	}); err != nil {
		return fleeterror.NewInternalErrorf("complete delegated rollout: %w", err)
	}
	r.Status = status
	return s.logRolloutEvent(ctx, r, channelName, eventType, m.Actor.Type == ActorTypeSystem, m.extra(nil))
}

// enforceDelegated observes convergence, settles failed attempts, and watches
// the wait clock. It never sends or retries a command or enrolls another miner.
func (s *Service) enforceDelegated(ctx context.Context, observed sqlc.FirmwareRollout) error {
	return s.tx.RunInTx(ctx, func(ctx context.Context) error {
		if err := s.lockEventStream(ctx, observed.OrgID); err != nil {
			return err
		}
		r, channelName, err := s.lockRollout(ctx, observed.OrgID, observed.ID, 0)
		if err != nil {
			return err
		}
		if r.Status != StatusActive || r.PausedAt.Valid {
			return nil
		}
		targets, err := s.listTargets(ctx, r)
		if err != nil {
			return err
		}
		completions, err := s.store.GetQueries(ctx).ListFirmwareRolloutDispatchCompletions(ctx, r.ID)
		if err != nil {
			return fleeterror.NewInternalErrorf("load delegated command completions: %w", err)
		}
		finishedAt := make(map[int64]time.Time, len(completions))
		for _, c := range completions {
			finishedAt[c.DeviceID] = c.FinishedAt
		}
		var failedIDs []int64
		var failedNames []string
		for _, t := range activeTargets(targets) {
			if t.settled(r) || t.Attempts == 0 || !t.LastSentAt.Valid || len(t.PendingFirmwareChecksums) > 0 || len(t.PendingLegacyFirmwareFileIds) > 0 {
				continue
			}
			// The worker may install for longer than the verification interval
			// and reboots the miner as its command finishes. Measure from then,
			// so a slow but successful update is not failed while rebooting.
			attemptEnded := t.LastSentAt.Time
			if finished, ok := finishedAt[t.DeviceID]; ok && finished.After(attemptEnded) {
				attemptEnded = finished
			}
			if s.now().Sub(attemptEnded) < resendInterval {
				continue
			}
			failedIDs = append(failedIDs, t.DeviceID)
			failedNames = append(failedNames, t.DeviceIdentifier)
		}
		if len(failedIDs) > 0 {
			if err := s.store.GetQueries(ctx).HaltFirmwareRolloutDevices(ctx, sqlc.HaltFirmwareRolloutDevicesParams{
				RolloutID: r.ID, DeviceIds: failedIDs, HaltReason: HaltReasonFailed,
				LastError: fmt.Sprintf("Did not verify %s after the controller's update attempt; retry to queue another attempt.", r.FirmwareVersion),
			}); err != nil {
				return fleeterror.NewInternalErrorf("settle delegated attempts: %w", err)
			}
			if err := s.logRolloutEvent(ctx, r, channelName, EventRolloutDeviceFailed, true, map[string]any{"device_identifiers": failedNames}); err != nil {
				return err
			}
			targets, err = s.listTargets(ctx, r)
			if err != nil {
				return err
			}
		}
		if allSettled(activeTargets(targets), r) {
			return s.completeDelegated(ctx, r, channelName, targets, Mutation{Actor: SystemActor})
		}
		if hasUpdatesInFlight(r, targets) {
			return s.setControllerWait(ctx, r.ID, sql.NullTime{})
		}
		if !r.ControllerWaitingSince.Valid {
			return s.setControllerWait(ctx, r.ID, sql.NullTime{Time: s.now(), Valid: true})
		}
		timeout := time.Duration(r.BehaviorSnapshot.ControllerTimeoutSeconds) * time.Second
		if timeout <= 0 || s.now().Sub(r.ControllerWaitingSince.Time) < timeout {
			return nil
		}
		if _, err := s.store.GetQueries(ctx).PauseFirmwareRollout(ctx, sqlc.PauseFirmwareRolloutParams{
			RolloutID: r.ID, ActorType: ActorTypeSystem, ActorID: 0, ActorName: "",
		}); err != nil {
			return fleeterror.NewInternalErrorf("pause timed-out rollout: %w", err)
		}
		return s.logRolloutEvent(ctx, r, channelName, EventRolloutControllerTimedOut, true, nil)
	})
}
