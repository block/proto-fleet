package rollout

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControlReconcilerUsesBoundedStaleCandidatePass(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 25, 2, 0, 0, 0, time.UTC)
	candidate := StartedControlCandidate{
		ID: uuid.New(), RolloutID: uuid.New(), OrgID: 42,
		Operation: ControlOperationAdmit, UpdatedAt: now.Add(-time.Minute),
	}
	store := &fakeControlReconciliationStore{candidates: []StartedControlCandidate{candidate}}
	reconciler := NewControlReconciler(
		ControlReconcilerConfig{BatchSize: 7, StaleAfter: 45 * time.Second},
		store,
	)
	reconciler.now = func() time.Time { return now }

	reconciler.RunOnce(t.Context())

	require.Equal(t, int32(7), store.limit)
	assert.Equal(t, now.Add(-45*time.Second), store.staleBefore)
	assert.Equal(t, []StartedControlCandidate{candidate}, store.reconciled)
}

func TestControlReconcilerRestartPassIsIdempotent(t *testing.T) {
	t.Parallel()

	candidate := StartedControlCandidate{
		ID: uuid.New(), RolloutID: uuid.New(), OrgID: 42,
		Operation: ControlOperationRevert,
	}
	store := &fakeControlReconciliationStore{
		candidates: []StartedControlCandidate{candidate},
		outcomes: []ControlReconciliationOutcome{
			ControlReconciliationCommitted,
			ControlReconciliationSettled,
		},
	}
	reconciler := NewControlReconciler(ControlReconcilerConfig{}, store)

	reconciler.RunOnce(t.Context())
	reconciler.RunOnce(t.Context())

	assert.Equal(t, []StartedControlCandidate{candidate, candidate}, store.reconciled)
}

func TestControlReconcilerRuntimeStartRunsImmediateRestartPass(t *testing.T) {
	t.Parallel()

	candidate := StartedControlCandidate{
		ID: uuid.New(), RolloutID: uuid.New(), OrgID: 42,
		Operation: ControlOperationAdmit,
	}
	store := &fakeControlReconciliationStore{
		candidates: []StartedControlCandidate{candidate},
	}
	reconciler := NewControlReconciler(
		ControlReconcilerConfig{TickInterval: time.Hour},
		store,
	)

	require.NoError(t, reconciler.Start(t.Context()))
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return len(store.reconciled) == 1
	}, time.Second, time.Millisecond)
	require.NoError(t, reconciler.Stop(t.Context()))
}

type fakeControlReconciliationStore struct {
	mu          sync.Mutex
	candidates  []StartedControlCandidate
	outcomes    []ControlReconciliationOutcome
	staleBefore time.Time
	limit       int32
	reconciled  []StartedControlCandidate
}

func (s *fakeControlReconciliationStore) ListStartedControlCandidates(
	_ context.Context,
	staleBefore time.Time,
	limit int32,
) ([]StartedControlCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staleBefore = staleBefore
	s.limit = limit
	return append([]StartedControlCandidate(nil), s.candidates...), nil
}

func (s *fakeControlReconciliationStore) ReconcileStartedControl(
	_ context.Context,
	candidate StartedControlCandidate,
) (ControlReconciliationOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconciled = append(s.reconciled, candidate)
	if len(s.outcomes) == 0 {
		return ControlReconciliationCommitted, nil
	}
	outcome := s.outcomes[0]
	s.outcomes = s.outcomes[1:]
	return outcome, nil
}
