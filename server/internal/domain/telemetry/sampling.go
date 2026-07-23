// Read-only device-metrics sampling.
//
// SampleDeviceMetrics returns a current metrics sample for each requested
// device without any server-side persistence: no device_metrics/status writer
// enqueue, no firmware or pairing reconciliation, no scheduler mutation, and
// no diagnostics polling. It exists for callers (the curtailment confirmation
// fast path) that need fresh, ordered evidence of device state between
// scheduled polls.
//
// The sampler shares three things with scheduled collection:
//
//  1. Per-device flights. The inFlight map now stores *inFlightEntry values
//     carrying a fleetd-owned flight start time, a metrics-ready signal with
//     the shared sample, and a claim-complete signal. A sampler joins a
//     full-telemetry flight that started after the caller's freshness bound,
//     and waits for status-only or RefreshDevice claims to complete before
//     starting its own read.
//  2. Completed samples. The most recent successful sample per device is
//     retained in memory (with its flight start) and reused when it satisfies
//     the caller's bound and is younger than sampleReuseWindow. The window
//     keeps repeated pulses from replaying one stale negative sample forever;
//     retention is in-memory because persisted device_metrics rows carry no
//     fleetd-owned flight start and so can never prove post-dispatch ordering.
//  3. The fetch budget. Direct reads are executed by the same worker pool
//     that serves scheduled collection (workers select across tasks,
//     statusTasks, and sampleTasks), so combined scheduled plus confirmation
//     fetches can never exceed Config.ConcurrencyLimit. Fairness between the
//     classes is the select's pseudo-random choice; additionally the number
//     of concurrent sampler waiters is capped at half the pool (minimum one)
//     so confirmation work can never occupy every worker. At
//     ConcurrencyLimit=1 the reservation degrades to temporal sharing: each
//     hold is bounded by MetricTimeout.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// sampleReuseWindow bounds how long a completed sample may satisfy new
// sampling requests. It matches the confirmation pulse interval so a parked
// caller re-requesting every pulse eventually triggers a fresh device read
// instead of replaying the same sample.
const sampleReuseWindow = 3 * time.Second

// errNoSampleFromFlight is published to joiners of a flight that ended
// without ever producing a metrics sample (for example a claim released on
// shutdown before its fetch ran).
var errNoSampleFromFlight = errors.New("telemetry flight ended without a metrics sample")

// inFlightEntry is the value stored in TelemetryService.inFlight while a
// device is claimed. flightStart is fleetd-owned and set at claim time, so it
// is always at or before the moment the device fetch actually starts; a
// flight whose start is after a dispatch timestamp therefore observed the
// device strictly after that dispatch.
type inFlightEntry struct {
	kind inFlightKind
	// flightStart is the fleetd-owned claim time. Device-reported timestamps
	// are observation metadata only and must never be compared against
	// fleetd clocks.
	flightStart time.Time

	metricsOnce sync.Once
	// metricsReady closes once metrics (or metricsErr) is set. For
	// full-telemetry and sample flights this happens right after the device
	// fetch returns, before any persistence side effects run.
	metricsReady chan struct{}
	metrics      modelsV2.DeviceMetrics
	metricsErr   error

	claimOnce sync.Once
	// claimDone closes when the claim is released and the device becomes
	// claimable again.
	claimDone chan struct{}
}

func newInFlightEntry(kind inFlightKind) *inFlightEntry {
	return &inFlightEntry{
		kind:         kind,
		flightStart:  time.Now(),
		metricsReady: make(chan struct{}),
		claimDone:    make(chan struct{}),
	}
}

// joinable reports whether a sampler may consume this flight's metrics
// directly. Status-only flights fetch no metrics; RefreshDevice flights are
// deliberately opaque so their existing wait/re-poll/flush contract stays
// untouched (their completed sample is still reusable via retention).
func (e *inFlightEntry) joinable() bool {
	return e.kind == inFlightKindFullTelemetry || e.kind == inFlightKindSample
}

func (e *inFlightEntry) publishMetrics(metrics modelsV2.DeviceMetrics, err error) {
	e.metricsOnce.Do(func() {
		e.metrics = metrics
		e.metricsErr = err
		close(e.metricsReady)
	})
}

// releaseInFlight publishes a terminal "no sample" error to any joiners that
// are still waiting, signals claim completion, and frees the device for the
// next claimant. Safe to call exactly once per held claim; the claim holder
// is the only releaser by invariant.
func (s *TelemetryService) releaseInFlight(deviceID models.DeviceIdentifier, entry *inFlightEntry) {
	entry.publishMetrics(modelsV2.DeviceMetrics{}, errNoSampleFromFlight)
	entry.claimOnce.Do(func() { close(entry.claimDone) })
	s.inFlight.Delete(deviceID)
}

// releaseInFlightByID releases the current claim when the caller does not
// hold the entry reference.
func (s *TelemetryService) releaseInFlightByID(deviceID models.DeviceIdentifier) {
	if v, ok := s.inFlight.Load(deviceID); ok {
		if entry, ok := v.(*inFlightEntry); ok {
			s.releaseInFlight(deviceID, entry)
			return
		}
	}
	s.inFlight.Delete(deviceID)
}

// publishFlightSample shares the outcome of a full collection fetch with any
// sampler joined to the device's current flight, and retains successful
// samples for short-window reuse. Called from GetTelemetryFromDevice
// immediately after the device fetch, before persistence side effects.
func (s *TelemetryService) publishFlightSample(deviceID models.DeviceIdentifier, result *deviceResult, fetchErr error) {
	v, ok := s.inFlight.Load(deviceID)
	if !ok {
		return
	}
	entry, ok := v.(*inFlightEntry)
	if !ok {
		return
	}
	sampleErr := fetchErr
	if sampleErr == nil && result != nil {
		sampleErr = result.metricsErr
	}
	if sampleErr != nil || result == nil {
		if sampleErr == nil {
			sampleErr = errNoSampleFromFlight
		}
		entry.publishMetrics(modelsV2.DeviceMetrics{}, sampleErr)
		return
	}
	entry.publishMetrics(result.metrics, nil)
	s.retainSample(deviceID, result.metrics, entry.flightStart)
}

// retainedSample is the most recent successful metrics sample for a device,
// kept for sampleReuseWindow so bursts of sampling requests (and scheduled
// polls that just completed) do not trigger redundant device reads.
type retainedSample struct {
	metrics     modelsV2.DeviceMetrics
	flightStart time.Time
	completedAt time.Time
}

func (s *TelemetryService) retainSample(deviceID models.DeviceIdentifier, metrics modelsV2.DeviceMetrics, flightStart time.Time) {
	s.retainedSamples.Store(deviceID, &retainedSample{
		metrics:     metrics,
		flightStart: flightStart,
		completedAt: time.Now(),
	})
}

// SampleSource records how a sample was obtained.
type SampleSource string

const (
	// SampleSourceReused means a recently completed sample satisfied the bound.
	SampleSourceReused SampleSource = "reused"
	// SampleSourceJoined means the sampler consumed an in-flight full poll.
	SampleSourceJoined SampleSource = "joined"
	// SampleSourceDirect means the sampler performed its own device read.
	SampleSourceDirect SampleSource = "direct"
)

// SampleRequest asks for one device's current metrics observed strictly after
// SampledAfter (fleetd clock). A zero SampledAfter accepts any fresh sample.
type SampleRequest struct {
	DeviceID models.DeviceIdentifier
	// SampledAfter is the exclusive lower bound on the fleetd-owned flight
	// start time. Flights started at or before this instant cannot satisfy
	// the request.
	SampledAfter time.Time
}

// SampleResult is the per-device outcome of SampleDeviceMetrics. Err is
// per-device so failed devices never invalidate successful siblings.
type SampleResult struct {
	DeviceID models.DeviceIdentifier
	Metrics  modelsV2.DeviceMetrics
	// FlightStart is the fleetd-owned start time of the flight that produced
	// Metrics. Always after the request's SampledAfter bound when Err is nil.
	FlightStart time.Time
	Source      SampleSource
	Err         error
}

// sampleTask carries a claimed direct read to the shared worker pool.
type sampleTask struct {
	deviceID models.DeviceIdentifier
	entry    *inFlightEntry
}

// SampleDeviceMetrics returns one result per unique requested device.
// Duplicate device IDs are deduplicated keeping the latest SampledAfter
// bound. Each device is served by reuse, join, or one direct read, in that
// order of preference; every per-device operation is bounded by
// MetricTimeout. The call itself performs no persistence.
func (s *TelemetryService) SampleDeviceMetrics(ctx context.Context, requests []SampleRequest) []SampleResult {
	if len(requests) == 0 {
		return nil
	}

	// Deduplicate, keeping first-seen order and the strictest bound.
	order := make([]models.DeviceIdentifier, 0, len(requests))
	bounds := make(map[models.DeviceIdentifier]time.Time, len(requests))
	for _, req := range requests {
		bound, seen := bounds[req.DeviceID]
		if !seen {
			order = append(order, req.DeviceID)
			bounds[req.DeviceID] = req.SampledAfter
			continue
		}
		if req.SampledAfter.After(bound) {
			bounds[req.DeviceID] = req.SampledAfter
		}
	}

	results := make([]SampleResult, len(order))
	waiterLimit := s.config.ConcurrencyLimit / 2
	if waiterLimit < 1 {
		waiterLimit = 1
	}
	if waiterLimit > len(order) {
		waiterLimit = len(order)
	}

	var wg sync.WaitGroup
	indexes := make(chan int)
	for range waiterLimit {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indexes {
				deviceID := order[i]
				results[i] = s.sampleOne(ctx, deviceID, bounds[deviceID])
			}
		}()
	}
	for i := range order {
		indexes <- i
	}
	close(indexes)
	wg.Wait()
	return results
}

// sampleOne obtains a single device sample under a MetricTimeout-bounded
// child context covering flight wait, admission, and fetch.
func (s *TelemetryService) sampleOne(ctx context.Context, deviceID models.DeviceIdentifier, sampledAfter time.Time) SampleResult {
	opCtx, cancel := context.WithTimeout(ctx, s.sampleOperationTimeout())
	defer cancel()

	for {
		// 1. Reuse a recently completed qualifying sample.
		if v, ok := s.retainedSamples.Load(deviceID); ok {
			if retained, ok := v.(*retainedSample); ok &&
				retained.flightStart.After(sampledAfter) &&
				time.Since(retained.completedAt) <= sampleReuseWindow {
				return SampleResult{
					DeviceID:    deviceID,
					Metrics:     retained.metrics,
					FlightStart: retained.flightStart,
					Source:      SampleSourceReused,
				}
			}
		}

		// 2. Join or wait out the device's current flight.
		if v, ok := s.inFlight.Load(deviceID); ok {
			entry, isEntry := v.(*inFlightEntry)
			if !isEntry {
				// Foreign claim value (should not happen in production).
				// Treat it as an opaque claim and poll for release.
				select {
				case <-opCtx.Done():
					return s.sampleTimeoutResult(deviceID, opCtx)
				case <-time.After(10 * time.Millisecond):
					continue
				}
			}
			if entry.joinable() && entry.flightStart.After(sampledAfter) {
				select {
				case <-entry.metricsReady:
					if entry.metricsErr != nil {
						return SampleResult{DeviceID: deviceID, Source: SampleSourceJoined, Err: entry.metricsErr}
					}
					return SampleResult{
						DeviceID:    deviceID,
						Metrics:     entry.metrics,
						FlightStart: entry.flightStart,
						Source:      SampleSourceJoined,
					}
				case <-opCtx.Done():
					return s.sampleTimeoutResult(deviceID, opCtx)
				}
			}
			// Pre-bound flight or status-only/refresh claim: wait for the
			// claim to complete, then re-evaluate (retention may now hold a
			// qualifying sample, or the device is claimable for a direct read).
			select {
			case <-entry.claimDone:
				continue
			case <-opCtx.Done():
				return s.sampleTimeoutResult(deviceID, opCtx)
			}
		}

		// 3. Claim the device and perform one direct read via the shared pool.
		entry := newInFlightEntry(inFlightKindSample)
		if _, loaded := s.inFlight.LoadOrStore(deviceID, entry); loaded {
			continue // lost the claim race; re-evaluate the new flight
		}
		// Never enqueue on a context that is already done: select would pick
		// between the two ready cases at random and could strand the claim
		// behind a task nobody is waiting for.
		if opCtx.Err() != nil {
			s.releaseInFlight(deviceID, entry)
			return s.sampleTimeoutResult(deviceID, opCtx)
		}
		select {
		case s.sampleTasks <- sampleTask{deviceID: deviceID, entry: entry}:
		case <-opCtx.Done():
			s.releaseInFlight(deviceID, entry)
			return s.sampleTimeoutResult(deviceID, opCtx)
		}
		select {
		case <-entry.metricsReady:
			if entry.metricsErr != nil {
				return SampleResult{DeviceID: deviceID, Source: SampleSourceDirect, Err: entry.metricsErr}
			}
			return SampleResult{
				DeviceID:    deviceID,
				Metrics:     entry.metrics,
				FlightStart: entry.flightStart,
				Source:      SampleSourceDirect,
			}
		case <-opCtx.Done():
			// The worker still owns the claim and will release it when the
			// fetch finishes (bounded by its own MetricTimeout).
			return s.sampleTimeoutResult(deviceID, opCtx)
		}
	}
}

func (s *TelemetryService) sampleTimeoutResult(deviceID models.DeviceIdentifier, opCtx context.Context) SampleResult {
	return SampleResult{
		DeviceID: deviceID,
		Source:   SampleSourceDirect,
		Err:      fmt.Errorf("sampling device %s: %w", deviceID, opCtx.Err()),
	}
}

func (s *TelemetryService) sampleOperationTimeout() time.Duration {
	if s.config.MetricTimeout > 0 {
		return s.config.MetricTimeout
	}
	return 5 * time.Second
}

// processSample executes a claimed direct read on a shared pool worker. It
// publishes the outcome to the claim entry and retains successful samples,
// with no persistence, scheduler, firmware, pairing, or diagnostics side
// effects (R5).
func (s *TelemetryService) processSample(ctx context.Context, task sampleTask) {
	fetchCtx, cancel := context.WithTimeout(ctx, s.sampleOperationTimeout())
	defer cancel()

	result, err := s.fetchTelemetryFromMiner(fetchCtx, models.Device{ID: task.deviceID})
	sampleErr := err
	if sampleErr == nil && result != nil {
		sampleErr = result.metricsErr
	}
	if sampleErr != nil || result == nil {
		if sampleErr == nil {
			sampleErr = errNoSampleFromFlight
		}
		task.entry.publishMetrics(modelsV2.DeviceMetrics{}, sampleErr)
		return
	}
	task.entry.publishMetrics(result.metrics, nil)
	s.retainSample(task.deviceID, result.metrics, task.entry.flightStart)
}
