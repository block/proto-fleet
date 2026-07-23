// Curtailment confirmation fast path (issue #661).
//
// A reconciler-owned pulse that promotes `dispatched` targets to
// confirmed/resolved from fresh telemetry samples between full ticks. The
// pulse is confirmation-only: it never dispatches commands, never burns retry
// budget, never ages dispatch timeouts, and never transitions event state —
// all corrective and event-level work stays on the full 30s tick. Its only
// writes are the same guarded promotions the tick performs
// (dispatched → confirmed for curtail work, dispatched → resolved for restore
// work), made single-winner by the expected-state and expected-batch-UUID
// guards on UpdateTargetState.
//
// Lifecycle: the pulse goroutine parks with zero periodic work while no
// eligible rows exist. Wakes arrive when a tick observes durable dispatched
// work (deferred from each phase handler, which also covers startup and
// crash recovery via the initial wake in Start). While active it re-runs
// every confirmationPulseInterval, backing off exponentially on pass
// failures, and parks again once the eligibility read returns no rows.
//
// Freshness (R3): a sample only confirms a target when its fleetd-owned
// flight start is strictly later than the target's durable phase dispatch
// timestamp. Device-reported timestamps are never compared against fleetd
// clocks.
package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	telemetryModels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

const (
	// confirmationPulseInterval is the between-pass cadence while eligible
	// work exists. Internal constant, not config (KTD8).
	confirmationPulseInterval = 3 * time.Second
	// confirmationBackoffMax caps the exponential backoff applied when a
	// pass fails (eligibility read error or panic).
	confirmationBackoffMax = 30 * time.Second
	// confirmationPassTimeout bounds the sampling half of one pass: the
	// eligibility read plus batch sampling. Guarded writes run under a
	// separate confirmationWriteTimeout budget (derived after sampling
	// returns) so a pass whose sampling exhausts this budget still promotes
	// the samples that already succeeded instead of discarding them.
	confirmationPassTimeout = 30 * time.Second
	// confirmationWriteTimeout bounds the guarded-write half of one pass. It
	// is derived fresh from the pulse's work context once sampling returns,
	// so Stop still cancels it. Modest by design: the writes are single-row
	// guarded UPDATEs already scoped by the eligibility read.
	confirmationWriteTimeout = 10 * time.Second
)

// ConfirmationSampler is the narrow read-only telemetry seam the pulse
// consumes. *telemetry.TelemetryService satisfies it.
type ConfirmationSampler interface {
	SampleDeviceMetrics(ctx context.Context, requests []telemetry.SampleRequest) []telemetry.SampleResult
}

// WithConfirmationSampler injects the telemetry sampler backing the
// confirmation fast path. Required when Config.ConfirmationFastPathEnabled;
// may be nil when disabled.
func WithConfirmationSampler(sampler ConfirmationSampler) Option {
	return func(r *Reconciler) { r.sampler = sampler }
}

// wakeConfirmation nudges the pulse out of its parked state. Non-blocking:
// the buffered channel coalesces bursts of wakes into one pass.
func (r *Reconciler) wakeConfirmation() {
	select {
	case r.confirmationWake <- struct{}{}:
	default:
	}
}

// wakeIfDispatchedWork wakes the pulse when any target holds durable
// dispatched work. Deferred at the end of each phase handler so both
// fresh dispatches (rows just written) and recovery cases (rows found
// dispatched after a restart) start a confirmation pass.
func (r *Reconciler) wakeIfDispatchedWork(targets []*models.Target) {
	for _, t := range targets {
		if t != nil && t.State == models.TargetStateDispatched {
			r.wakeConfirmation()
			return
		}
	}
}

// confirmationLoop is the pulse goroutine: parked on the wake channel,
// active on a pulse cadence while eligible work remains.
func (r *Reconciler) confirmationLoop(stopCtx, workCtx context.Context) {
	defer r.wg.Done()
	for {
		// Parked: zero periodic work until something dispatches.
		select {
		case <-stopCtx.Done():
			return
		case <-r.confirmationWake:
		}

		backoff := r.confirmationPulse
		for {
			parked, failed := r.safeConfirmationPass(workCtx)
			if parked {
				break
			}
			if failed {
				backoff = min(backoff*2, confirmationBackoffMax)
			} else {
				backoff = r.confirmationPulse
			}
			select {
			case <-stopCtx.Done():
				return
			case <-r.confirmationWake:
				// Fresh dispatch while active: run the next pass now.
			case <-time.After(backoff):
			}
		}
	}
}

// safeConfirmationPass keeps a panicking pass from killing the pulse
// goroutine; a panic counts as a failed pass for backoff purposes.
func (r *Reconciler) safeConfirmationPass(ctx context.Context) (parked, failed bool) {
	defer func() {
		if rec := recover(); rec != nil {
			r.metrics.IncConfirmationPassFailure()
			slog.Error("curtailment confirmation fast path: recovered panic in pass", "panic", rec)
			parked, failed = false, true
		}
	}()
	return r.confirmationPass(ctx)
}

// confirmationPass runs one confirmation wave: read eligible work, sample
// each unique device once, and apply guarded promotions for targets whose
// post-dispatch sample proves the desired state. Returns parked=true when no
// eligible work exists.
func (r *Reconciler) confirmationPass(ctx context.Context) (parked, failed bool) {
	passCtx, cancel := context.WithTimeout(ctx, r.confirmationPassTimeout)
	defer cancel()

	items, err := r.store.ListEligibleConfirmationTargets(passCtx)
	if err != nil {
		if ctx.Err() == nil {
			r.metrics.IncConfirmationPassFailure()
			slog.Error("curtailment confirmation fast path: eligibility read failed", "error", err)
		}
		return false, true
	}
	if len(items) == 0 {
		return true, false
	}

	// One request per item; the sampler deduplicates device IDs keeping the
	// strictest (latest) dispatch bound, so a device targeted by multiple
	// events is still read once.
	requests := make([]telemetry.SampleRequest, 0, len(items))
	for _, item := range items {
		requests = append(requests, telemetry.SampleRequest{
			DeviceID:     telemetryModels.DeviceIdentifier(item.DeviceIdentifier),
			SampledAfter: item.DispatchedAt,
		})
	}
	results := r.sampler.SampleDeviceMetrics(passCtx, requests)
	samplesByDevice := make(map[string]telemetry.SampleResult, len(results))
	for _, res := range results {
		samplesByDevice[string(res.DeviceID)] = res
	}

	// passCtx bounded only the eligibility read and sampling above. Promote
	// every already-successful sample under a fresh write budget derived
	// from the pulse's work context, so a pass whose sampling exhausted
	// passCtx still lands its early successes instead of discarding them all.
	// Deriving from ctx (not passCtx) keeps Stop cancellation working. A
	// timed-out sampling still reports failed=true so the unsampled remainder
	// backs off.
	sampledTimedOut := passCtx.Err() != nil
	writeCtx, cancelWrite := context.WithTimeout(ctx, confirmationWriteTimeout)
	defer cancelWrite()

	for _, item := range items {
		if writeCtx.Err() != nil {
			return false, true
		}
		sample, ok := samplesByDevice[item.DeviceIdentifier]
		if !ok || sample.Err != nil {
			// Per-device sampling failure: preserved siblings still confirm;
			// this row waits for the next pulse or the full tick.
			continue
		}
		r.confirmFromSample(writeCtx, item, sample)
	}
	return false, sampledTimedOut
}

// confirmFromSample applies one guarded promotion when the sample proves the
// item's desired state. Negative or insufficient evidence is a no-op: retry
// budget, dispatch-timeout aging, and unpaired-device handling belong to the
// full tick (KTD2).
func (r *Reconciler) confirmFromSample(ctx context.Context, item models.ConfirmationTarget, sample telemetry.SampleResult) {
	// R3: only evidence observed strictly after this item's own dispatch
	// counts. The sampler already enforced the deduplicated bound; re-check
	// per item so a device shared across events cannot leak evidence.
	if !sample.FlightStart.After(item.DispatchedAt) {
		return
	}
	if item.ForceIncludeAllPairedMiners && !curtailment.IsAllPairedPolicyPairingStatus(item.PairingStatus) {
		return
	}

	powerW, hashRateHS := sampleMeasurements(sample.Metrics)
	now := r.now()
	var params interfaces.UpdateCurtailmentTargetStateParams
	switch item.DesiredState {
	case models.DesiredStateCurtailed:
		if !isCurtailed(powerW, item.BaselinePowerW, hashRateHS, r.cfg.DriftThresholdFactor, true) {
			return
		}
		params = confirmedCurtailTargetParams(now, powerW)
	case models.DesiredStateActive:
		if !isRestored(powerW, item.BaselinePowerW, hashRateHS, r.cfg.DriftThresholdFactor) {
			return
		}
		params = resolvedRestoreTargetParams(now, powerW)
	default:
		return
	}

	// Full guard set: expected event state and desired state (as the full
	// tick uses) plus the fast-path guards — current target state and the
	// exact phase batch UUID from the eligibility read — so a concurrent
	// tick promotion, stop/restore flip, or timeout redispatch (new batch
	// UUID) race-loses instead of double-writing.
	expectedEventState := item.EventState
	expectedDesired := item.DesiredState
	expectedTargetState := models.TargetStateDispatched
	expectedBatch := item.BatchUUID
	params.ExpectedEventState = &expectedEventState
	params.ExpectedDesiredState = &expectedDesired
	params.ExpectedState = &expectedTargetState
	params.ExpectedDispatchBatchUUID = &expectedBatch

	// Reuse the full tick's write path so race/failure classification,
	// race-loss Warn logging, and the IncEventStateRaceLoss /
	// IncTargetWriteFailure metrics live in one place (writeTargetState,
	// reconciler.go) instead of being reimplemented here. writeTargetState
	// only reads ev.ID/State/EventUUID, and it defaults
	// ExpectedEventState/ExpectedDesiredState only when nil — both are
	// already set above — so every guard passes through untouched.
	ev := &models.Event{ID: item.EventID, EventUUID: item.EventUUID, State: item.EventState}
	if err := r.writeTargetState(ctx, ev, item.DeviceIdentifier, params); err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment confirmation fast path: confirm write failed",
				"event_id", item.EventID, "device", item.DeviceIdentifier, "error", err)
		}
		return
	}
	slog.Info("curtailment confirmation fast path: target confirmed",
		"event_id", item.EventID, "event_uuid", item.EventUUID,
		"device", item.DeviceIdentifier, "desired_state", item.DesiredState,
		"sample_source", sample.Source, "flight_start", sample.FlightStart)
}

// sampleMeasurements extracts the power/hash pointers the isCurtailed /
// isRestored predicates consume from a live metrics sample.
func sampleMeasurements(m modelsV2.DeviceMetrics) (powerW, hashRateHS *float64) {
	if m.PowerW != nil {
		v := m.PowerW.Value
		powerW = &v
	}
	if m.HashrateHS != nil {
		v := m.HashrateHS.Value
		hashRateHS = &v
	}
	return powerW, hashRateHS
}

// confirmedCurtailTargetParams is the shared Dispatched → Confirmed
// promotion used by both the full tick (confirmOneDispatched) and the fast
// path. Confirmation resets retry budget.
func confirmedCurtailTargetParams(now time.Time, observedPowerW *float64) interfaces.UpdateCurtailmentTargetStateParams {
	zero := int32(0)
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:       models.TargetStateConfirmed,
		ConfirmedAt: &now,
		ObservedAt:  &now,
		RetryCount:  &zero,
	}
	if observedPowerW != nil && isFinite(*observedPowerW) {
		power := *observedPowerW
		params.ObservedPowerW = &power
	}
	return params
}

// resolvedRestoreTargetParams is the shared Dispatched → Resolved promotion
// used by both the full tick (confirmOneRestore) and the fast path.
func resolvedRestoreTargetParams(now time.Time, observedPowerW *float64) interfaces.UpdateCurtailmentTargetStateParams {
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:       models.TargetStateResolved,
		ConfirmedAt: &now,
		ObservedAt:  &now,
	}
	if observedPowerW != nil && isFinite(*observedPowerW) {
		power := *observedPowerW
		params.ObservedPowerW = &power
	}
	return params
}
