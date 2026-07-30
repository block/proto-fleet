package ha

import (
	"context"
	"errors"
	"fmt"
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
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Second)},
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
	holder := uuid.New()
	observer := &sequenceObserver{
		results: []observerResult{
			{observation: coordinatorObservation("cluster-a", 41, time.Second)},
			{err: errors.New("DCS unavailable")},
		},
	}
	coordinator := newCoordinatorWithHolder(
		observer, &fakeLeaseStore{}, coordinatorTestConfig(), holder,
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.Error(t, coordinator.step(t.Context()))
	require.Error(t, activeCtx.Err())
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
	require.NotEqual(t, holder, coordinator.HolderID())
}

func TestCoordinatorKeepsHolderAfterPassiveAcquisitionProofFailure(t *testing.T) {
	holder := uuid.New()
	proofErr := errors.New("closing DCS proof failed")
	coordinator := newCoordinatorWithHolder(
		actionThenErrorObserver{
			observation: coordinatorObservation("cluster-a", 41, time.Second),
			err:         proofErr,
		},
		&fakeLeaseStore{},
		coordinatorTestConfig(),
		holder,
	)

	require.ErrorIs(t, coordinator.step(t.Context()), proofErr)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
	require.Equal(t, holder, coordinator.HolderID())
}

func TestCoordinatorCancelsLifetimeOnRenewalLoss(t *testing.T) {
	store := &fakeLeaseStore{renewErr: ErrOwnershipLost}
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Second)},
		store,
		coordinatorTestConfig(),
		uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.ErrorIs(t, coordinator.step(t.Context()), ErrOwnershipLost)
	require.Error(t, activeCtx.Err())
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorCancelsLifetimeOnWriterGenerationChange(t *testing.T) {
	observer := &sequenceObserver{
		results: []observerResult{
			{observation: coordinatorObservation("cluster-a", 41, time.Second)},
			{observation: coordinatorObservation("cluster-a", 42, time.Second)},
		},
	}
	coordinator := newCoordinatorWithHolder(
		observer, &fakeLeaseStore{}, coordinatorTestConfig(), uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.ErrorIs(t, coordinator.step(t.Context()), ErrWriterChanged)
	require.Error(t, activeCtx.Err())
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorCancelsLifetimeWhenLeaseExpiresWithoutRenewal(t *testing.T) {
	config := coordinatorTestConfig()
	config.LeaseDuration = 40 * time.Millisecond
	config.RenewInterval = 10 * time.Millisecond
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Second)},
		&fakeLeaseStore{},
		config,
		uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.Eventually(t, func() bool { return activeCtx.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorRunRetriesWhenPassiveObservationBlocks(t *testing.T) {
	config := coordinatorTestConfig()
	config.LeaseDuration = 30 * time.Millisecond
	config.RenewInterval = 10 * time.Millisecond
	config.RetryInterval = 5 * time.Millisecond
	observer := &blockingObserver{calls: make(chan struct{}, 2)}
	coordinator := newCoordinatorWithHolder(
		observer,
		&fakeLeaseStore{},
		config,
		uuid.New(),
	)
	ctx, cancel := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() {
		runResult <- coordinator.Run(ctx)
	}()

	requireCall(t, observer.calls)
	requireCall(t, observer.calls)
	cancel()

	require.ErrorIs(t, <-runResult, context.Canceled)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorActiveRenewalStopsAtWatchdogDeadline(t *testing.T) {
	config := coordinatorTestConfig()
	config.LeaseDuration = time.Second
	config.RenewInterval = 10 * time.Millisecond
	observer := &activateThenBlockObserver{
		observation: coordinatorObservation("cluster-a", 41, 40*time.Millisecond),
	}
	coordinator := newCoordinatorWithHolder(
		observer,
		&fakeLeaseStore{},
		config,
		uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.ErrorIs(t, coordinator.step(t.Context()), context.Canceled)
	require.Error(t, activeCtx.Err())
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorRejectsLeaseThatExpiredBeforeAcquireReturned(t *testing.T) {
	config := coordinatorTestConfig()
	config.LeaseDuration = 10 * time.Millisecond
	config.RenewInterval = 5 * time.Millisecond
	store := &fakeLeaseStore{acquireDelay: 20 * time.Millisecond}
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Second)},
		store,
		config,
		uuid.New(),
	)

	require.ErrorIs(t, coordinator.step(t.Context()), ErrOwnershipExpired)
	_, _, active := coordinator.ActiveLifetime()
	require.False(t, active)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorCapsLifetimeAtDCSProofDeadline(t *testing.T) {
	config := coordinatorTestConfig()
	config.LeaseDuration = time.Second
	config.RenewInterval = 100 * time.Millisecond
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, 40*time.Millisecond)},
		&fakeLeaseStore{},
		config,
		uuid.New(),
	)

	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)
	require.Eventually(t, func() bool { return activeCtx.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorSuccessfulRenewalExtendsWatchdog(t *testing.T) {
	config := coordinatorTestConfig()
	config.LeaseDuration = 200 * time.Millisecond
	config.RenewInterval = 50 * time.Millisecond
	observer := &sequenceObserver{
		results: []observerResult{
			{observation: coordinatorObservation("cluster-a", 41, time.Second)},
			{observation: coordinatorObservation("cluster-a", 41, time.Second)},
		},
	}
	coordinator := newCoordinatorWithHolder(
		observer,
		&fakeLeaseStore{},
		config,
		uuid.New(),
	)

	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	time.Sleep(120 * time.Millisecond)
	require.NoError(t, coordinator.step(t.Context()))
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, activeCtx.Err())
	require.Equal(t, StateActive, coordinator.Snapshot().State)

	require.Eventually(t, func() bool { return activeCtx.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func requireCall(t *testing.T, calls <-chan struct{}) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for observer call")
	}
}

func coordinatorObservation(
	clusterID string,
	generation int64,
	proofTTL time.Duration,
) WriterObservation {
	observed := observation(clusterID, generation)
	observed.DCSProofDeadline = time.Now().Add(proofTTL)
	return observed
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

func (s staticObserver) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	if s.err != nil {
		return WriterObservation{}, s.err
	}
	if err := action(ctx, s.observation); err != nil {
		return WriterObservation{}, err
	}
	return s.observation, nil
}

type actionThenErrorObserver struct {
	observation WriterObservation
	err         error
}

func (o actionThenErrorObserver) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	if err := action(ctx, o.observation); err != nil {
		return WriterObservation{}, err
	}
	return WriterObservation{}, o.err
}

type observerResult struct {
	observation WriterObservation
	err         error
}

type sequenceObserver struct {
	mu      sync.Mutex
	results []observerResult
}

func (s *sequenceObserver) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	s.mu.Lock()
	result := s.results[0]
	s.results = s.results[1:]
	s.mu.Unlock()
	if result.err != nil {
		return WriterObservation{}, result.err
	}
	if err := action(ctx, result.observation); err != nil {
		return WriterObservation{}, err
	}
	return result.observation, nil
}

type blockingObserver struct {
	calls chan struct{}
}

func (b *blockingObserver) ObserveAndRun(
	ctx context.Context,
	_ func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	b.calls <- struct{}{}
	<-ctx.Done()
	return WriterObservation{}, fmt.Errorf("observation canceled: %w", ctx.Err())
}

type activateThenBlockObserver struct {
	mu          sync.Mutex
	observation WriterObservation
	activated   bool
}

func (b *activateThenBlockObserver) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	b.mu.Lock()
	activated := b.activated
	b.activated = true
	b.mu.Unlock()
	if activated {
		<-ctx.Done()
		return WriterObservation{}, fmt.Errorf("renewal observation canceled: %w", ctx.Err())
	}
	if err := action(ctx, b.observation); err != nil {
		return WriterObservation{}, err
	}
	return b.observation, nil
}

type fakeLeaseStore struct {
	mu           sync.Mutex
	ownership    Ownership
	acquireErr   error
	renewErr     error
	acquireDelay time.Duration
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
	if f.acquireDelay > 0 {
		time.Sleep(f.acquireDelay)
	}
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
	_ WriterObservation,
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
