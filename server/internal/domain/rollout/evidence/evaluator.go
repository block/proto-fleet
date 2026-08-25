package evidence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

type Evaluator struct {
	cfg        Config
	store      Store
	controller RolloutController
	now        func() time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

var _ runtimejobs.Lifecycle = (*Evaluator)(nil)

type RolloutController interface {
	Continue(ctx context.Context, req rollout.AdmitRequest) (*rollout.Rollout, error)
}

func NewEvaluator(
	cfg Config,
	store Store,
	controller RolloutController,
) *Evaluator {
	return &Evaluator{
		cfg:        cfg.withDefaults(),
		store:      store,
		controller: controller,
		now:        time.Now,
	}
}

func (e *Evaluator) Start(ctx context.Context) error {
	if e.store == nil {
		return errors.New("rollout evidence evaluator: store is required")
	}
	if e.cfg.TickInterval < time.Second {
		return fmt.Errorf(
			"rollout evidence evaluator: tick_interval must be at least 1s, got %s",
			e.cfg.TickInterval,
		)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rollout evidence evaluator: start: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		select {
		case <-e.done:
			return errors.New("rollout evidence evaluator: previous activation is stopping")
		default:
			return nil
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	e.cancel = cancel
	e.done = done
	go e.loop(runCtx, done)
	return nil
}

func (e *Evaluator) Stop(ctx context.Context) error {
	e.mu.Lock()
	if e.cancel == nil {
		e.mu.Unlock()
		return nil
	}
	cancel := e.cancel
	done := e.done
	e.mu.Unlock()

	cancel()
	select {
	case <-done:
		e.mu.Lock()
		if e.done == done {
			e.cancel = nil
			e.done = nil
		}
		e.mu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("rollout evidence evaluator: stop: %w", ctx.Err())
	}
}

func (e *Evaluator) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	reportProgress := runtimejobs.TrackProgress(ctx, e.cfg.TickInterval)
	e.safeRunOnce(ctx)
	reportProgress()

	ticker := time.NewTicker(e.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.safeRunOnce(ctx)
			reportProgress()
		}
	}
}

func (e *Evaluator) safeRunOnce(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("rollout evidence evaluator recovered panic", "panic", recovered)
		}
	}()
	e.RunOnce(ctx)
}

func (e *Evaluator) RunOnce(ctx context.Context) {
	candidates, err := e.store.ListCandidates(ctx, e.cfg.BatchSize, e.cfg.WorkBudget)
	if err != nil {
		slog.Error("rollout evidence evaluator failed to list candidates", "error", err)
		return
	}

	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}
		if err := e.evaluateCandidate(ctx, candidate); err != nil {
			slog.Error(
				"rollout evidence evaluation failed",
				"rollout_id", candidate.RolloutID,
				"batch_id", candidate.BatchID,
				"error", err,
			)
		}
	}
}

func (e *Evaluator) evaluateCandidate(ctx context.Context, candidate Candidate) error {
	now := e.now().UTC()
	if !now.After(candidate.CompletedAt) {
		return nil
	}
	if candidate.AutoControlStatus != nil {
		switch *candidate.AutoControlStatus {
		case rollout.ControlStatusSucceeded:
			// Evidence continues refreshing, but this batch can never advance again.
		case rollout.ControlStatusFailed:
			if candidate.Status != rollout.EvidenceStatusAutomationError {
				return e.persistAutomationError(
					ctx,
					candidate,
					now,
					automaticContinueFailedMessage,
				)
			}
		case rollout.ControlStatusStarted:
			if candidate.RolloutState == rollout.StateRunning &&
				candidate.AutoControlExpectedRevision != nil &&
				candidate.AutoControlResultingRevision != nil &&
				candidate.RolloutRevision == *candidate.AutoControlResultingRevision {
				return e.continueCandidate(
					ctx,
					candidate,
					*candidate.AutoControlExpectedRevision,
					now,
				)
			}
		}
	}
	windowEnd := now
	postWindowEnd := candidate.CompletedAt.Add(postWindow)
	if windowEnd.After(postWindowEnd) {
		windowEnd = postWindowEnd
	}

	snapshot, err := e.store.Refresh(ctx, candidate, windowEnd)
	if err != nil {
		return fmt.Errorf("refresh post evidence: %w", err)
	}
	summary := buildSummary(candidate, snapshot, now)
	updated, err := e.store.UpdateSummary(ctx, summary)
	if err != nil {
		return fmt.Errorf("update batch evidence summary: %w", err)
	}
	if !updated {
		return nil
	}
	if summary.Status == rollout.EvidenceStatusHealthy &&
		candidate.Status != rollout.EvidenceStatusAutomationError &&
		candidate.AutoControlStatus == nil &&
		candidate.RolloutState == rollout.StateReview &&
		candidate.IsCurrentReviewBatch &&
		candidate.HasPendingBatch {
		return e.continueCandidate(ctx, candidate, candidate.RolloutRevision, now)
	}
	return nil
}

func (e *Evaluator) continueCandidate(
	ctx context.Context,
	candidate Candidate,
	expectedRevision int64,
	now time.Time,
) error {
	if e.controller == nil {
		return nil
	}
	_, err := e.controller.Continue(ctx, rollout.AdmitRequest{
		OrgID:            candidate.OrgID,
		RolloutID:        candidate.RolloutID,
		ExpectedRevision: expectedRevision,
		IdempotencyKey:   autoContinueIdempotencyKey(candidate.BatchID),
		Reason:           autoContinueReason,
		ActorUserID:      candidate.RolloutCreatedByUserID,
		ActorType:        rollout.ActorTypeSystem,
	})
	if err == nil || errors.Is(err, rollout.ErrRevisionConflict) {
		return nil
	}
	slog.Error(
		"automatic rollout continue returned an ambiguous error; awaiting control reconciliation",
		"rollout_id", candidate.RolloutID,
		"batch_id", candidate.BatchID,
		"error", err,
	)
	return nil
}

func (e *Evaluator) persistAutomationError(
	ctx context.Context,
	candidate Candidate,
	now time.Time,
	message string,
) error {
	if err := e.store.MarkAutomationError(ctx, Summary{
		RolloutID:    candidate.RolloutID,
		BatchID:      candidate.BatchID,
		OrgID:        candidate.OrgID,
		Status:       rollout.EvidenceStatusAutomationError,
		EvaluatedAt:  now,
		ErrorMessage: &message,
	}); err != nil {
		return fmt.Errorf("persist automatic rollout continue error: %w", err)
	}
	return nil
}

const (
	autoContinueReason             = "Hashrate evidence policy healthy duration satisfied"
	automaticContinueFailedMessage = "Automatic rollout continue failed; use manual controls to proceed."
)

func autoContinueIdempotencyKey(batchID int64) string {
	return fmt.Sprintf("rollout-evidence-auto-continue-batch-%d", batchID)
}

func buildSummary(candidate Candidate, snapshot Snapshot, now time.Time) Summary {
	summary := Summary{
		RolloutID:                          candidate.RolloutID,
		BatchID:                            candidate.BatchID,
		OrgID:                              candidate.OrgID,
		Status:                             rollout.EvidenceStatusCollecting,
		TotalCount:                         int64(len(snapshot.Members)),
		LatestPolicyBucketHashrateHS:       candidate.LatestPolicyBucketHashrateHS,
		LatestPolicyBucketDeltaBasisPoints: candidate.LatestPolicyBucketDeltaBasisPoints,
		HealthySince:                       candidate.HealthySince,
		LastPolicyBucketBoundary:           candidate.LastPolicyBucketBoundary,
		ExpectedEvaluatedAt:                candidate.EvaluatedAt,
		ExpectedLastPolicyBucketBoundary:   candidate.LastPolicyBucketBoundary,
		EvaluatedAt:                        now,
		ErrorMessage:                       candidate.ErrorMessage,
	}

	baselineByMember := make(map[int64]float64, len(snapshot.Members))
	baselineUnavailable := len(snapshot.Members) == 0
	postMissing := len(snapshot.Members) == 0
	stale := false
	if snapshot.Aggregate != nil {
		aggregate := snapshot.Aggregate
		summary.TotalCount = aggregate.TotalCount
		summary.PairedCount = aggregate.PairedCount
		baselineUnavailable = aggregate.TotalCount == 0 || !aggregate.BaselineAvailable
		postMissing = aggregate.TotalCount == 0 || !aggregate.PostAvailable
		stale = postMissing && now.Sub(candidate.CompletedAt) > staleAfter
		if !postMissing && !isFreshObservation(aggregate.OldestPostObservedAt, now) {
			stale = true
		}
		summary.CumulativeBaselineHashrateHS = aggregate.BaselineAverage
		summary.CumulativeCurrentHashrateHS = aggregate.PostAverage
		if aggregate.BaselineAverage != nil && aggregate.PostAverage != nil {
			summary.CumulativeDeltaBasisPoints = deltaBasisPoints(
				*aggregate.BaselineAverage,
				*aggregate.PostAverage,
			)
		}
	} else {
		var baselineSum, currentSum float64
		for _, member := range snapshot.Members {
			if member.BaselineHashrateHS == nil ||
				!isFiniteNonNegative(*member.BaselineHashrateHS) ||
				*member.BaselineHashrateHS == 0 {
				baselineUnavailable = true
				continue
			}
			baselineByMember[member.MemberID] = *member.BaselineHashrateHS
			if member.PostHashrateHS == nil || !isFiniteNonNegative(*member.PostHashrateHS) {
				postMissing = true
				if now.Sub(candidate.CompletedAt) > staleAfter {
					stale = true
				}
				continue
			}
			if !isFreshObservation(member.PostObservedAt, now) {
				stale = true
			}
			summary.PairedCount++
			baselineSum += *member.BaselineHashrateHS
			currentSum += *member.PostHashrateHS
		}
		if summary.PairedCount > 0 {
			baselineAverage := baselineSum / float64(summary.PairedCount)
			currentAverage := currentSum / float64(summary.PairedCount)
			summary.CumulativeBaselineHashrateHS = &baselineAverage
			summary.CumulativeCurrentHashrateHS = &currentAverage
			summary.CumulativeDeltaBasisPoints = deltaBasisPoints(baselineAverage, currentAverage)
		}
	}

	postWindowEnd := candidate.CompletedAt.Add(postWindow)
	windowClosed := !now.Before(postWindowEnd)
	if windowClosed {
		summary.PostWindowFinalized = true
		summary.PostWindowFinalizedAt = &postWindowEnd
	}
	autoControlSucceeded :=
		candidate.AutoControlStatus != nil && *candidate.AutoControlStatus == rollout.ControlStatusSucceeded

	switch {
	case candidate.Status == rollout.EvidenceStatusCancelled:
		summary.Status = rollout.EvidenceStatusCancelled
	case candidate.Status == rollout.EvidenceStatusAutomationError:
		summary.Status = rollout.EvidenceStatusAutomationError
	case autoControlSucceeded:
		summary.Status = rollout.EvidenceStatusHealthy
	case baselineUnavailable:
		summary.Status = rollout.EvidenceStatusUnavailable
	case postMissing && windowClosed:
		summary.Status = rollout.EvidenceStatusUnavailable
	case postMissing && stale:
		summary.Status = rollout.EvidenceStatusStale
		summary.HealthySince = nil
	case postMissing:
		summary.Status = rollout.EvidenceStatusCollecting
	case candidate.PolicyEnabled:
		applyPolicyBuckets(&summary, candidate, snapshot, now, baselineByMember)
		if stale {
			summary.Status = rollout.EvidenceStatusStale
			summary.HealthySince = nil
		}
	case stale:
		summary.Status = rollout.EvidenceStatusStale
		summary.HealthySince = nil
	case candidate.Status == rollout.EvidenceStatusHealthy ||
		candidate.Status == rollout.EvidenceStatusHeld:
		summary.Status = candidate.Status
	default:
		summary.Status = rollout.EvidenceStatusObserving
	}

	if windowClosed {
		if candidate.PolicyEnabled &&
			len(snapshot.PolicyBuckets) == 0 &&
			(candidate.Status == rollout.EvidenceStatusHealthy ||
				candidate.Status == rollout.EvidenceStatusHeld) {
			summary.Status = candidate.Status
		}
		switch summary.Status {
		case rollout.EvidenceStatusAutomationError,
			rollout.EvidenceStatusUnavailable,
			rollout.EvidenceStatusHealthy,
			rollout.EvidenceStatusHeld,
			rollout.EvidenceStatusCancelled:
		case rollout.EvidenceStatusPending,
			rollout.EvidenceStatusCollecting,
			rollout.EvidenceStatusObserving,
			rollout.EvidenceStatusStale,
			rollout.EvidenceStatusFinalized:
			summary.Status = rollout.EvidenceStatusFinalized
			summary.HealthySince = nil
		}
	}

	return summary
}

func applyPolicyBuckets(
	summary *Summary,
	candidate Candidate,
	snapshot Snapshot,
	now time.Time,
	baselineByMember map[int64]float64,
) {
	buckets := append([]PolicyBucket(nil), snapshot.PolicyBuckets...)
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Boundary.Before(buckets[j].Boundary)
	})

	healthySince := candidate.HealthySince
	previousBoundary := candidate.LastPolicyBucketBoundary
	status := rollout.EvidenceStatusObserving
	if candidate.Status == rollout.EvidenceStatusHealthy ||
		candidate.Status == rollout.EvidenceStatusHeld {
		status = candidate.Status
	}
	if candidate.Status == rollout.EvidenceStatusStale {
		status = rollout.EvidenceStatusStale
	}

	evaluatorWasStale := candidate.EvaluatedAt != nil &&
		now.Sub(*candidate.EvaluatedAt) > staleAfter
	if evaluatorWasStale {
		status = rollout.EvidenceStatusStale
		for index := len(buckets) - 1; index >= 0; index-- {
			boundary := buckets[index].Boundary
			if boundary.After(now) ||
				(candidate.LastPolicyBucketBoundary != nil &&
					!boundary.After(*candidate.LastPolicyBucketBoundary)) {
				continue
			}
			summary.LastPolicyBucketBoundary = &boundary
			break
		}
		summary.HealthySince = nil
		summary.Status = status
		return
	}

	candidateBucketStale := candidate.LastPolicyBucketBoundary != nil &&
		now.Sub(*candidate.LastPolicyBucketBoundary) > staleAfter
	if candidate.LastPolicyBucketBoundary == nil {
		healthySince = nil
		status = rollout.EvidenceStatusObserving
		if now.Sub(candidate.CompletedAt) > staleAfter {
			candidateBucketStale = true
		}
	}
	if candidateBucketStale {
		healthySince = nil
		status = rollout.EvidenceStatusStale
	}

	for index := range buckets {
		bucket := &buckets[index]
		if candidate.LastPolicyBucketBoundary == nil ||
			bucket.Boundary.After(*candidate.LastPolicyBucketBoundary) {
			continue
		}
		delta, currentAverage, ok := policyBucketDelta(
			*bucket,
			summary.TotalCount,
			baselineByMember,
			now,
		)
		if !ok {
			healthySince = nil
			status = rollout.EvidenceStatusStale
			continue
		}
		if bucket.Boundary.Equal(*candidate.LastPolicyBucketBoundary) {
			summary.LatestPolicyBucketHashrateHS = &currentAverage
			summary.LatestPolicyBucketDeltaBasisPoints = &delta
		}
		if delta < -candidate.MaxDropBasisPoints {
			healthySince = nil
			status = rollout.EvidenceStatusHeld
			continue
		}
		if status == rollout.EvidenceStatusHeld && healthySince == nil {
			start := bucket.Boundary.Add(-PolicyBucketDuration)
			healthySince = &start
			status = rollout.EvidenceStatusObserving
		}
	}

	for index := range buckets {
		bucket := &buckets[index]
		if candidate.LastPolicyBucketBoundary != nil &&
			!bucket.Boundary.After(*candidate.LastPolicyBucketBoundary) {
			continue
		}
		if now.Sub(bucket.Boundary) > staleAfter {
			healthySince = nil
			previousBoundary = &bucket.Boundary
			summary.LastPolicyBucketBoundary = &bucket.Boundary
			status = rollout.EvidenceStatusStale
			continue
		}
		summary.LastPolicyBucketBoundary = &bucket.Boundary
		previousBoundaryBeforeBucket := previousBoundary
		previousBoundary = &bucket.Boundary
		delta, currentAverage, ok := policyBucketDelta(
			*bucket,
			summary.TotalCount,
			baselineByMember,
			now,
		)
		if !ok {
			healthySince = nil
			status = rollout.EvidenceStatusStale
			continue
		}
		if previousBoundaryBeforeBucket != nil &&
			bucket.Boundary.Sub(*previousBoundaryBeforeBucket) != PolicyBucketDuration {
			healthySince = nil
		}

		summary.LatestPolicyBucketHashrateHS = &currentAverage
		summary.LatestPolicyBucketDeltaBasisPoints = &delta

		if delta < -candidate.MaxDropBasisPoints {
			healthySince = nil
			status = rollout.EvidenceStatusHeld
			continue
		}
		if healthySince == nil {
			start := bucket.Boundary.Add(-PolicyBucketDuration)
			healthySince = &start
		}
		status = rollout.EvidenceStatusObserving
		if bucket.Boundary.Sub(*healthySince) >=
			time.Duration(candidate.HealthyDurationSeconds)*time.Second {
			status = rollout.EvidenceStatusHealthy
		}
	}

	newestBoundary := candidate.LastPolicyBucketBoundary
	if len(buckets) > 0 &&
		(newestBoundary == nil || buckets[len(buckets)-1].Boundary.After(*newestBoundary)) {
		newestBoundary = &buckets[len(buckets)-1].Boundary
	}
	if newestBoundary == nil {
		if now.Sub(candidate.CompletedAt) > staleAfter {
			healthySince = nil
			status = rollout.EvidenceStatusStale
		}
	} else if now.Sub(*newestBoundary) > staleAfter {
		healthySince = nil
		status = rollout.EvidenceStatusStale
	}

	summary.HealthySince = healthySince
	summary.Status = status
}

func policyBucketDelta(
	bucket PolicyBucket,
	totalCount int64,
	baselineByMember map[int64]float64,
	now time.Time,
) (int32, float64, bool) {
	if bucket.MemberCount > 0 {
		if bucket.MemberCount != totalCount ||
			!isFiniteNonNegative(bucket.BaselineAverage) ||
			bucket.BaselineAverage == 0 ||
			!isFiniteNonNegative(bucket.CurrentAverage) ||
			!isFreshTime(bucket.OldestObservedAt, now) {
			return 0, 0, false
		}
		delta := deltaBasisPoints(bucket.BaselineAverage, bucket.CurrentAverage)
		if delta == nil {
			return 0, 0, false
		}
		return *delta, bucket.CurrentAverage, true
	}
	if len(bucket.Members) != int(totalCount) || totalCount == 0 {
		return 0, 0, false
	}
	seen := make(map[int64]struct{}, len(bucket.Members))
	var baselineSum, currentSum float64
	for _, member := range bucket.Members {
		baseline, ok := baselineByMember[member.MemberID]
		if !ok || !isFiniteNonNegative(baseline) || baseline == 0 {
			return 0, 0, false
		}
		if _, duplicate := seen[member.MemberID]; duplicate {
			return 0, 0, false
		}
		if !isFiniteNonNegative(member.AvgHashrateHS) ||
			!isFreshTime(member.ObservedAt, now) {
			return 0, 0, false
		}
		seen[member.MemberID] = struct{}{}
		baselineSum += baseline
		currentSum += member.AvgHashrateHS
	}
	baselineAverage := baselineSum / float64(totalCount)
	currentAverage := currentSum / float64(totalCount)
	delta := deltaBasisPoints(baselineAverage, currentAverage)
	if delta == nil {
		return 0, 0, false
	}
	return *delta, currentAverage, true
}

func deltaBasisPoints(baseline, current float64) *int32 {
	if !isFiniteNonNegative(baseline) || baseline == 0 ||
		!isFiniteNonNegative(current) {
		return nil
	}
	value := math.Round(((current - baseline) / baseline) * 10_000)
	value = min(max(value, math.MinInt32), math.MaxInt32)
	result := int32(value)
	return &result
}

func isFiniteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func isFreshObservation(observedAt *time.Time, now time.Time) bool {
	return observedAt != nil && isFreshTime(*observedAt, now)
}

func isFreshTime(observedAt, now time.Time) bool {
	return !observedAt.IsZero() &&
		!observedAt.After(now) &&
		now.Sub(observedAt) <= staleAfter
}
