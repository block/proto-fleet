package ha

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewCoordinatorUsesRandomProcessIncarnation(t *testing.T) {
	first, err := NewCoordinator(staticObserver{}, &fakeLeaseStore{}, coordinatorTestConfig())
	require.NoError(t, err)
	second, err := NewCoordinator(staticObserver{}, &fakeLeaseStore{}, coordinatorTestConfig())
	require.NoError(t, err)

	require.NotEqual(t, uuid.Nil, first.HolderID())
	require.NotEqual(t, first.HolderID(), second.HolderID())
}

func TestTokenCompareOrdersWriterGenerationBeforeLeaseEpoch(t *testing.T) {
	require.Equal(t, -1, (Token{WriterGeneration: 41, LeaseEpoch: 99}).Compare(
		Token{WriterGeneration: 42, LeaseEpoch: 1},
	))
	require.Equal(t, -1, (Token{WriterGeneration: 42, LeaseEpoch: 1}).Compare(
		Token{WriterGeneration: 42, LeaseEpoch: 2},
	))
	require.Equal(t, 0, (Token{WriterGeneration: 42, LeaseEpoch: 2}).Compare(
		Token{WriterGeneration: 42, LeaseEpoch: 2},
	))
	require.Equal(t, 1, (Token{WriterGeneration: 43, LeaseEpoch: 1}).Compare(
		Token{WriterGeneration: 42, LeaseEpoch: 100},
	))
}

func TestCoordinatorActivatesAndExposesLifetime(t *testing.T) {
	holder := uuid.New()
	store := &fakeLeaseStore{}
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: observation("cluster-a", 41)},
		store,
		coordinatorTestConfig(),
		holder,
	)

	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, token, active := coordinator.ActiveLifetime()
	require.True(t, active)
	require.NoError(t, activeCtx.Err())
	require.Equal(t, Token{WriterGeneration: 41, LeaseEpoch: 1}, token)
	require.Equal(t, StateActive, coordinator.Snapshot().State)
}

func TestCoordinatorCancelsLifetimeOnObservationLoss(t *testing.T) {
	observer := &sequenceObserver{
		results: []observerResult{
			{observation: observation("cluster-a", 41)},
			{err: errors.New("DCS unavailable")},
		},
	}
	coordinator := newCoordinatorWithHolder(
		observer, &fakeLeaseStore{}, coordinatorTestConfig(), uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.Error(t, coordinator.step(t.Context()))
	require.Eventually(t, func() bool { return activeCtx.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorCancelsLifetimeOnRenewalLoss(t *testing.T) {
	store := &fakeLeaseStore{renewErr: ErrOwnershipLost}
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: observation("cluster-a", 41)},
		store,
		coordinatorTestConfig(),
		uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.ErrorIs(t, coordinator.step(t.Context()), ErrOwnershipLost)
	require.Eventually(t, func() bool { return activeCtx.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorCancelsLifetimeOnWriterGenerationChange(t *testing.T) {
	observer := &sequenceObserver{
		results: []observerResult{
			{observation: observation("cluster-a", 41)},
			{observation: observation("cluster-a", 42)},
		},
	}
	coordinator := newCoordinatorWithHolder(
		observer, &fakeLeaseStore{}, coordinatorTestConfig(), uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.ErrorIs(t, coordinator.step(t.Context()), ErrWriterChanged)
	require.Eventually(t, func() bool { return activeCtx.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func coordinatorTestConfig() CoordinatorConfig {
	return CoordinatorConfig{
		LeaseDuration: time.Second,
		RenewInterval: 100 * time.Millisecond,
		RetryInterval: 10 * time.Millisecond,
	}
}

type staticObserver struct {
	observation WriterObservation
	err         error
}

func (s staticObserver) Observe(context.Context) (WriterObservation, error) {
	return s.observation, s.err
}

type observerResult struct {
	observation WriterObservation
	err         error
}

type sequenceObserver struct {
	mu      sync.Mutex
	results []observerResult
}

func (s *sequenceObserver) Observe(context.Context) (WriterObservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.results[0]
	s.results = s.results[1:]
	return result.observation, result.err
}

type fakeLeaseStore struct {
	mu         sync.Mutex
	ownership  Ownership
	acquireErr error
	renewErr   error
}

func (f *fakeLeaseStore) Acquire(
	_ context.Context,
	observed WriterObservation,
	holder uuid.UUID,
	duration time.Duration,
) (Ownership, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.acquireErr != nil {
		return Ownership{}, f.acquireErr
	}
	now := time.Now()
	f.ownership = Ownership{
		DCSClusterID: observed.DCSClusterID,
		Token: Token{
			WriterGeneration: observed.WriterGeneration,
			LeaseEpoch:       1,
		},
		HolderID:     holder,
		DatabaseTime: now,
		ExpiresAt:    now.Add(duration),
	}
	return f.ownership, nil
}

func (f *fakeLeaseStore) Renew(
	_ context.Context,
	ownership Ownership,
	duration time.Duration,
) (Ownership, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.renewErr != nil {
		return Ownership{}, f.renewErr
	}
	now := time.Now()
	ownership.DatabaseTime = now
	ownership.ExpiresAt = now.Add(duration)
	f.ownership = ownership
	return ownership, nil
}
