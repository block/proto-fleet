package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// reconcileActiveFans reports true only when the requested hardware command
// and its durable state write both succeed. Dynamic dispatch uses that result
// to fail closed while airflow is known to need reopening.
func (r *Reconciler) reconcileActiveFans(ctx context.Context, ev *models.Event, targets []*models.Target) bool {
	if r.fans == nil || r.fanStore == nil || ev == nil || len(ev.FacilityFanDeviceIDs) == 0 {
		return false
	}
	confirmed, allConfirmed := activeFanTargetConfirmation(targets)
	now := r.now()
	desiredPower := driver.PowerOff
	if !allConfirmed || confirmed == 0 {
		if ev.FanOffSentAt == nil {
			return false
		}
		desiredPower = driver.PowerOn
	} else if (ev.FanOffSentAt == nil || ev.FanAirflowReopenedAt != nil) &&
		!activeFanOffDelayElapsed(ev, targets, now) {
		// Initial curtailment, dynamic admission, and drift all start their
		// cooling delay only after the latest miner confirmation. A later
		// airflow-reopen command remains the lower bound when applicable.
		return false
	}
	if desiredPower == driver.PowerOff && ev.FanOffSentAt == nil {
		// Persist the first OFF intent before hardware I/O. If the command
		// succeeds but its follow-up state write is lost, later admission and
		// operator recovery must conservatively assume the fans may be off.
		intent := interfaces.UpdateCurtailmentFanStateParams{
			ExpectedEventState: models.EventStateActive,
			FanOffSentAt:       &now,
			LastError:          ev.FanLastError,
		}
		_, err := r.fanStore.CommandFanState(ctx, ev.ID, intent, func(context.Context) *string {
			return ev.FanLastError
		})
		if err != nil {
			if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				slog.Error("curtailment reconciler: facility fan OFF intent update failed", "event_id", ev.ID, "error", err)
			}
			return false
		}
		ev.FanOffSentAt = &now
	}

	params := interfaces.UpdateCurtailmentFanStateParams{
		ExpectedEventState: models.EventStateActive,
	}
	switch desiredPower {
	case driver.PowerOn:
		if ev.FanAirflowReopenedAt == nil {
			params.FanAirflowReopenedAt = &now
		}
		params.FanAirflowReopenedAtOnSuccess = &now
	case driver.PowerOff:
		if ev.FanAirflowReopenedAt != nil {
			params.ClearFanAirflowReopenedAt = true
		}
	default:
		return false
	}
	lastError, err := r.commandAndPersistFanState(ctx, ev, params, desiredPower)
	if err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: facility fan state update failed", "event_id", ev.ID, "power", desiredPower, "error", err)
		}
		return false
	}
	if lastError == nil && params.FanAirflowReopenedAtOnSuccess != nil {
		ev.FanAirflowReopenedAt = params.FanAirflowReopenedAtOnSuccess
	} else if params.FanAirflowReopenedAt != nil {
		ev.FanAirflowReopenedAt = params.FanAirflowReopenedAt
	}
	if params.ClearFanAirflowReopenedAt {
		ev.FanAirflowReopenedAt = nil
	}
	ev.FanLastError = lastError
	return lastError == nil
}

func activeFanTargetsNeedAirflow(ev *models.Event, targets []*models.Target) bool {
	if ev == nil || ev.FanOffSentAt == nil {
		return false
	}
	confirmed, allConfirmed := activeFanTargetConfirmation(targets)
	return !allConfirmed || confirmed == 0
}

func hasFreshCurtailedFanEvidence(c *models.Candidate, target *models.Target, driftThresholdFactor float64) bool {
	if c == nil || target == nil || c.LatestMetricsAt == nil {
		return false
	}
	return isCurtailed(c.LatestPowerW, target.BaselinePowerW, c.LatestHashRateHS, driftThresholdFactor, true)
}

func hasFreshTelemetry(c *models.Candidate) bool {
	if c == nil || c.LatestMetricsAt == nil {
		return false
	}
	if c.LatestPowerW != nil && curtailment.IsFinite(*c.LatestPowerW) {
		return true
	}
	return c.LatestHashRateHS != nil && curtailment.IsFinite(*c.LatestHashRateHS)
}

func activeFanTargetConfirmation(targets []*models.Target) (confirmed int, allConfirmed bool) {
	allConfirmed = true
	for _, target := range targets {
		if target.DesiredState == models.DesiredStateActive {
			if target.State != models.TargetStateResolved && target.State != models.TargetStateReleased {
				allConfirmed = false
			}
			continue
		}
		if target.DesiredState != "" && target.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		// Only a confirmed curtailment supplies positive evidence that a
		// potentially powered miner is no longer hashing. Unavailable and
		// other terminal bookkeeping states may represent a miner that never
		// received a curtail command, so they must keep airflow open.
		if target.State != models.TargetStateConfirmed {
			allConfirmed = false
			continue
		}
		confirmed++
	}
	return confirmed, allConfirmed
}

func activeFanOffDelayElapsed(ev *models.Event, targets []*models.Target, now time.Time) bool {
	if ev.FanOffDelaySec <= 0 {
		return true
	}
	var latestConfirmation *time.Time
	for _, target := range targets {
		if target.DesiredState != "" && target.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		if target.State != models.TargetStateConfirmed || target.ConfirmedAt == nil {
			return false
		}
		if ev.FanAirflowReopenedAt != nil && target.ConfirmedAt.Before(*ev.FanAirflowReopenedAt) {
			return false
		}
		if latestConfirmation == nil || target.ConfirmedAt.After(*latestConfirmation) {
			latestConfirmation = target.ConfirmedAt
		}
	}
	if latestConfirmation == nil {
		return false
	}
	delayStartedAt := *latestConfirmation
	if ev.FanAirflowReopenedAt != nil && ev.FanAirflowReopenedAt.After(delayStartedAt) {
		delayStartedAt = *ev.FanAirflowReopenedAt
	}
	return !now.Before(delayStartedAt.Add(time.Duration(ev.FanOffDelaySec) * time.Second))
}

func (r *Reconciler) reopenEventFans(ctx context.Context, ev *models.Event) bool {
	if r.fans == nil || r.fanStore == nil || ev == nil || len(ev.FacilityFanDeviceIDs) == 0 || ev.FanOffSentAt == nil {
		return false
	}
	now := r.now()
	params := interfaces.UpdateCurtailmentFanStateParams{
		ExpectedEventState: ev.State,
	}
	if ev.FanAirflowReopenedAt == nil {
		params.FanAirflowReopenedAt = &now
	}
	params.FanAirflowReopenedAtOnSuccess = &now
	lastError, err := r.commandAndPersistFanState(ctx, ev, params, driver.PowerOn)
	if err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: facility fan airflow reopen failed", "event_id", ev.ID, "error", err)
		}
		return false
	}
	if lastError == nil {
		ev.FanAirflowReopenedAt = &now
	} else if params.FanAirflowReopenedAt != nil {
		ev.FanAirflowReopenedAt = params.FanAirflowReopenedAt
	}
	ev.FanLastError = lastError
	return lastError == nil
}

func (r *Reconciler) reconcileRestoringFans(ctx context.Context, ev *models.Event) {
	if r.fans == nil || r.fanStore == nil || ev == nil || len(ev.FacilityFanDeviceIDs) == 0 {
		return
	}
	now := r.now()
	params := interfaces.UpdateCurtailmentFanStateParams{
		ExpectedEventState: models.EventStateRestoring,
	}
	if ev.FanOnSentAt == nil {
		params.FanOnSentAt = &now
		// Airflow markers from the active phase do not prove that this
		// restoration's ON command succeeded. Clear the old marker when the
		// first attempt fails; a successful command replaces it atomically.
		params.ClearFanAirflowReopenedAt = true
	}
	if restoringFanAirflowStartedAt(ev) == nil {
		params.FanAirflowReopenedAtOnSuccess = &now
	}
	lastError, err := r.commandAndPersistFanState(ctx, ev, params, driver.PowerOn)
	if err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: facility fan ON state update failed", "event_id", ev.ID, "error", err)
		}
		return
	}
	if ev.FanOnSentAt == nil {
		ev.FanOnSentAt = &now
	}
	if lastError == nil && params.FanAirflowReopenedAtOnSuccess != nil {
		ev.FanAirflowReopenedAt = params.FanAirflowReopenedAtOnSuccess
	} else if params.ClearFanAirflowReopenedAt {
		ev.FanAirflowReopenedAt = nil
	}
	ev.FanLastError = lastError
	if r.fanAlert != nil {
		alertStartedAt := restoreFanFailureAlertStartedAt(ev)
		switch {
		case lastError == nil:
			r.fanAlert.EmitCurtailmentFanRestoreFailure(ctx, ev.OrgID, ev.EventUUID.String(), false)
		case alertStartedAt != nil && !now.Before(alertStartedAt.Add(time.Duration(ev.FanRestoreDelaySec)*time.Second)):
			// Emit before terminal evaluation so even a targetless or already
			// resolved event leaves an operator-visible signal when its fan
			// restore has remained broken through the fail-open delay.
			r.fanAlert.EmitCurtailmentFanRestoreFailure(ctx, ev.OrgID, ev.EventUUID.String(), true)
		}
	}
}

// restoreFanFailureAlertStartedAt returns the timestamp used to decide when a
// fan restore failure becomes operator-visible. Prefer confirmed restored
// airflow so alerting follows the same cooling gate that protects miner
// restores after a failed first ON attempt.
func restoreFanFailureAlertStartedAt(ev *models.Event) *time.Time {
	if airflowStartedAt := restoringFanAirflowStartedAt(ev); airflowStartedAt != nil {
		return airflowStartedAt
	}
	if ev == nil {
		return nil
	}
	if ev.FanOnSentAt != nil {
		return ev.FanOnSentAt
	}
	return ev.FanAirflowReopenedAt
}

// restoringFanAirflowStartedAt returns the first successful ON command in the
// current restoring phase. FanOnSentAt records the first attempt (and therefore
// bounds fail-open behavior), while an older airflow marker may belong to the
// active phase and must not satisfy the restore cooling gate.
func restoringFanAirflowStartedAt(ev *models.Event) *time.Time {
	if ev == nil || ev.FanOnSentAt == nil || ev.FanAirflowReopenedAt == nil ||
		ev.FanAirflowReopenedAt.Before(*ev.FanOnSentAt) {
		return nil
	}
	return ev.FanAirflowReopenedAt
}

func (r *Reconciler) commandAndPersistFanState(
	ctx context.Context,
	event *models.Event,
	params interfaces.UpdateCurtailmentFanStateParams,
	power driver.PowerMode,
) (*string, error) {
	return r.fanStore.CommandFanState(ctx, event.ID, params, func(commandCtx context.Context) *string {
		fanCtx, cancel := fanCommandContext(commandCtx)
		defer cancel()
		return r.fans.SetState(fanCtx, event, power)
	})
}

func fanCommandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.WithTimeout(ctx, 0)
	}
	// This deadline applies only to hardware I/O. The enclosing store call keeps
	// the event context so the remaining half can persist timeout evidence before
	// telemetry confirmation, state transitions, and miner dispatch continue.
	return context.WithTimeout(ctx, remaining/2)
}
