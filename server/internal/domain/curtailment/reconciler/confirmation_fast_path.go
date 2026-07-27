// Curtailment confirmation fast path (issue #661).
//
// A reconciler-owned pulse that promotes `dispatched` targets to
// confirmed/resolved from fresh telemetry samples between full ticks. The
// pulse is confirmation-only: it never dispatches commands, never burns retry
// budget, never ages dispatch timeouts, and never transitions event state —
// all corrective and event-level work stays on the full 30s tick. Positive
// promotions are grouped by event; the bulk write revalidates event phase,
// target state/direction/batch, and live device ownership before committing.
//
// Lifecycle: the pulse goroutine parks with zero periodic work while no
// eligible rows exist. Wakes arrive when a tick observes durable dispatched
// work (deferred from each phase handler, which also covers startup and
// crash recovery via the initial wake in Start). While active it re-runs
// every confirmationPulseInterval, backing off exponentially on pass
// failures, and parks again once the eligibility read returns no rows.
//
// Freshness (R3): a sample only confirms a target when its fleetd-owned
// flight start is strictly later than the local eligibility-read boundary.
// The read can only return a durably dispatched row, so this proves physical
// post-dispatch ordering without comparing wall clocks from different fleetd
// processes. Device-reported timestamps are never used for ordering.
package reconciler

import (
	"context"
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
	// so Stop still cancels it. Modest by design: writes are guarded bulk
	// updates already scoped by the eligibility read.
	confirmationWriteTimeout  = 10 * time.Second
	confirmationLogSampleSize = 5
	confirmationBulkChunkSize = 500
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

	// This local boundary is captured only after the durable eligibility read
	// returned. Requiring local telemetry flights to start after it avoids
	// cross-process wall-clock comparisons and guarantees post-dispatch
	// evidence even during startup recovery.
	sampledAfter := r.now()

	// One request per item; the sampler deduplicates device IDs keeping the
	// organization boundary. A device targeted by multiple rows in one org is
	// still read once.
	requests := make([]telemetry.SampleRequest, 0, len(items))
	for _, item := range items {
		requests = append(requests, telemetry.SampleRequest{
			DeviceID:     telemetryModels.DeviceIdentifier(item.DeviceIdentifier),
			OrgID:        item.OrgID,
			SampledAfter: sampledAfter,
		})
	}
	results := r.sampler.SampleDeviceMetrics(passCtx, requests)
	type sampleKey struct {
		orgID  int64
		device string
	}
	samplesByDevice := make(map[sampleKey]telemetry.SampleResult, len(results))
	sampleFailures := 0
	reusedSamples := 0
	joinedSamples := 0
	directSamples := 0
	for _, res := range results {
		samplesByDevice[sampleKey{orgID: res.OrgID, device: string(res.DeviceID)}] = res
		if res.Err != nil {
			sampleFailures++
			continue
		}
		switch res.Source {
		case telemetry.SampleSourceReused:
			reusedSamples++
		case telemetry.SampleSourceJoined:
			joinedSamples++
		case telemetry.SampleSourceDirect:
			directSamples++
		}
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

	type eventUpdates struct {
		eventState models.EventState
		updates    []interfaces.ConfirmationUpdate
	}
	updatesByEvent := make(map[int64]*eventUpdates)
	eventOrder := make([]int64, 0)
	positiveCount := 0
	for _, item := range items {
		sample, ok := samplesByDevice[sampleKey{orgID: item.OrgID, device: item.DeviceIdentifier}]
		if !ok || sample.Err != nil {
			// Per-device sampling failure: preserved siblings still confirm;
			// this row waits for the next pulse or the full tick.
			continue
		}
		update, ok := r.confirmationUpdateFromSample(item, sample, sampledAfter)
		if !ok {
			continue
		}
		group := updatesByEvent[item.EventID]
		if group == nil {
			group = &eventUpdates{eventState: item.EventState}
			updatesByEvent[item.EventID] = group
			eventOrder = append(eventOrder, item.EventID)
		}
		group.updates = append(group.updates, update)
		positiveCount++
	}

	appliedCount := 0
	raceLossCount := 0
	writeFailureCount := 0
	var firstWriteError error
	sampleDeviceIDs := make([]string, 0, confirmationLogSampleSize)
	for _, eventID := range eventOrder {
		group := updatesByEvent[eventID]
		for start := 0; start < len(group.updates); start += confirmationBulkChunkSize {
			if writeCtx.Err() != nil {
				return false, true
			}
			end := min(start+confirmationBulkChunkSize, len(group.updates))
			chunk := group.updates[start:end]
			result, err := r.confirmationStore.BulkConfirmTargets(writeCtx, eventID, group.eventState, chunk)
			if err != nil {
				writeFailureCount += len(chunk)
				r.metrics.IncTargetWriteFailure()
				if firstWriteError == nil {
					firstWriteError = err
				}
				continue
			}
			appliedCount += result.AppliedCount
			if result.AppliedCount < len(chunk) {
				raceLossCount += len(chunk) - result.AppliedCount
				r.metrics.IncEventStateRaceLoss()
			}
			for _, deviceID := range result.SampleDeviceIdentifiers {
				if len(sampleDeviceIDs) >= confirmationLogSampleSize {
					break
				}
				sampleDeviceIDs = append(sampleDeviceIDs, deviceID)
			}
		}
	}
	logAttrs := []any{
		"eligible_count", len(items),
		"sample_failure_count", sampleFailures,
		"reused_sample_count", reusedSamples,
		"joined_sample_count", joinedSamples,
		"direct_sample_count", directSamples,
		"positive_count", positiveCount,
		"confirmed_count", appliedCount,
		"race_loss_count", raceLossCount,
		"write_failure_count", writeFailureCount,
		"sample_device_ids", sampleDeviceIDs,
	}
	switch {
	case writeFailureCount > 0:
		slog.Error("curtailment confirmation fast path: pass completed",
			append(logAttrs, "first_write_error", firstWriteError)...)
	case appliedCount > 1 || raceLossCount > 0 || sampleFailures > 0:
		slog.Info("curtailment confirmation fast path: pass completed", logAttrs...)
	case appliedCount == 1:
		slog.Debug("curtailment confirmation fast path: pass completed", logAttrs...)
	}
	return false, sampledTimedOut
}

// confirmationUpdateFromSample builds one guarded promotion payload when the
// sample proves the item's desired state. Negative or insufficient evidence
// is a no-op: retry budget and dispatch-timeout aging belong to the full tick.
func (r *Reconciler) confirmationUpdateFromSample(
	item models.ConfirmationTarget,
	sample telemetry.SampleResult,
	sampledAfter time.Time,
) (interfaces.ConfirmationUpdate, bool) {
	// Re-check the sampler contract before trusting the result. OrgID prevents
	// a same-identifier sample from another tenant being associated with this
	// row; the bulk write repeats live ownership and pairing checks atomically.
	if sample.OrgID != item.OrgID || !sample.FlightStart.After(sampledAfter) {
		return interfaces.ConfirmationUpdate{}, false
	}

	powerW, hashRateHS := sampleMeasurements(sample.Metrics)
	now := r.now()
	update := interfaces.ConfirmationUpdate{
		DeviceIdentifier: item.DeviceIdentifier,
		BatchUUID:        item.BatchUUID,
		ObservedAt:       now,
		ConfirmedAt:      now,
	}
	if powerW != nil && isFinite(*powerW) {
		power := *powerW
		update.ObservedPowerW = &power
	}
	switch item.DesiredState {
	case models.DesiredStateCurtailed:
		if item.ForceIncludeAllPairedMiners && !curtailment.IsAllPairedPolicyPairingStatus(item.PairingStatus) {
			return interfaces.ConfirmationUpdate{}, false
		}
		if !isCurtailed(powerW, item.BaselinePowerW, hashRateHS, r.cfg.DriftThresholdFactor, true) {
			return interfaces.ConfirmationUpdate{}, false
		}
		update.Phase = models.TargetPhaseCurtail
	case models.DesiredStateActive:
		if !isRestored(powerW, item.BaselinePowerW, hashRateHS, r.cfg.DriftThresholdFactor) {
			return interfaces.ConfirmationUpdate{}, false
		}
		update.Phase = models.TargetPhaseRestore
	default:
		return interfaces.ConfirmationUpdate{}, false
	}
	return update, true
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
