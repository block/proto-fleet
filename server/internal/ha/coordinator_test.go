package ha

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"testing/synctest"
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

func TestCoordinatorRenewsAfterClosingDCSProof(t *testing.T) {
	// Arrange
	closingProofDone := false
	store := &fakeLeaseStore{}
	coordinator := newCoordinatorWithHolder(
		&closingProofObserver{
			observation: coordinatorObservation("cluster-a", 41, time.Second),
			completed:   &closingProofDone,
		},
		store,
		coordinatorTestConfig(),
		uuid.New(),
	)

	// Act
	err := coordinator.step(t.Context())

	// Assert
	require.NoError(t, err)
	require.True(t, closingProofDone)
	require.Equal(
		t,
		[]string{"acquire", "renew"},
		store.callSequence(),
	)
	require.Equal(t, StateActive, coordinator.Snapshot().State)
}

func TestCoordinatorMarksPassiveCurrentOnlyAfterClosingDCSProof(t *testing.T) {
	// Arrange
	closingProofDone := false
	observed := coordinatorObservation("cluster-a", 41, 40*time.Millisecond)
	coordinator := newCoordinatorWithHolder(
		&closingProofObserver{
			observation: observed,
			completed:   &closingProofDone,
		},
		&fakeLeaseStore{acquireErr: ErrLeaseUnavailable},
		coordinatorTestConfig(),
		uuid.New(),
	)

	// Act
	err := coordinator.step(t.Context())

	// Assert
	require.ErrorIs(t, err, ErrLeaseUnavailable)
	require.True(t, closingProofDone)
	snapshot := coordinator.Snapshot()
	require.True(t, snapshot.ObservationAvailable)
	require.Equal(t, observed.DCSProofDeadline, snapshot.FreshUntil)
}

func TestCoordinatorWaitForActiveUnblocksOnActivationAndOwnershipLoss(t *testing.T) {
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Second)},
		&fakeLeaseStore{},
		coordinatorTestConfig(),
		uuid.New(),
	)
	type activeResult struct {
		ctx context.Context //nolint:containedctx // Carries the lifetime returned by the goroutine.
		err error
	}
	waitResult := make(chan activeResult, 1)
	go func() {
		activeCtx, _, err := coordinator.WaitForActive(t.Context())
		waitResult <- activeResult{ctx: activeCtx, err: err}
	}()

	require.NoError(t, coordinator.step(t.Context()))
	result := <-waitResult
	require.NoError(t, result.err)
	coordinator.deactivate(ErrOwnershipLost)

	require.Eventually(t, func() bool { return result.ctx.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
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
	require.Equal(t, holder, coordinator.HolderID())
}

func TestCoordinatorRejectsTimelineMismatchFromNewWriter(t *testing.T) {
	store := &fakeLeaseStore{}
	coordinator := newCoordinatorWithHolder(
		&sequenceObserver{results: []observerResult{
			{observation: coordinatorObservation("cluster-a", 41, time.Second)},
			{
				observation: coordinatorObservation("cluster-a", 42, time.Second),
				err:         fmt.Errorf("promotion observation: %w", ErrTimelineMismatch),
			},
		}},
		store,
		coordinatorTestConfig(),
		uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)

	require.ErrorIs(t, coordinator.step(t.Context()), ErrWriterChanged)
	require.ErrorIs(t, context.Cause(activeCtx), ErrWriterChanged)
	_, _, active = coordinator.ActiveLifetime()
	require.False(t, active)
	require.Equal(t, []string{"acquire", "renew"}, store.callSequence())
}

func TestCoordinatorGivesPeersAnAcquisitionWindowAfterPassiveProofFailure(t *testing.T) {
	// Arrange
	holder := uuid.New()
	proofErr := errors.New("closing DCS proof failed")
	store := &fakeLeaseStore{}
	config := coordinatorTestConfig()
	coordinator := newCoordinatorWithHolder(
		actionThenErrorObserver{
			observation: coordinatorObservation("cluster-a", 41, time.Second),
			err:         proofErr,
		},
		store,
		config,
		holder,
	)

	// Act
	require.ErrorIs(t, coordinator.step(t.Context()), proofErr)
	require.NoError(t, coordinator.step(t.Context()))

	// Assert
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
	require.NotEqual(t, holder, coordinator.HolderID())
	require.Equal(t, 1, store.acquireCount())
	coordinator.mu.RLock()
	acquireAfter := coordinator.acquireAfter
	coordinator.mu.RUnlock()
	require.True(t, acquireAfter.After(time.Now().Add(config.RetryInterval)))

	coordinator.mu.Lock()
	coordinator.acquireAfter = time.Now().Add(-time.Millisecond)
	coordinator.mu.Unlock()

	require.ErrorIs(t, coordinator.step(t.Context()), proofErr)
	require.Equal(t, 2, store.acquireCount())
}

func TestCoordinatorCancelsLifetimeOnRenewalLoss(t *testing.T) {
	store := &fakeLeaseStore{}
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Second)},
		store,
		coordinatorTestConfig(),
		uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)
	store.setRenewError(ErrOwnershipLost)

	require.ErrorIs(t, coordinator.step(t.Context()), ErrOwnershipLost)
	require.Error(t, activeCtx.Err())
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorRunStopsAfterActiveOwnershipLoss(t *testing.T) {
	// Arrange
	store := &fakeLeaseStore{}
	config := coordinatorTestConfig()
	config.RenewInterval = time.Millisecond
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Second)},
		store,
		config,
		uuid.New(),
	)
	runResult := make(chan error, 1)
	go func() { runResult <- coordinator.Run(t.Context()) }()
	activeCtx, _, err := coordinator.WaitForActive(t.Context())
	require.NoError(t, err)

	// Act
	store.setRenewError(ErrOwnershipLost)

	// Assert
	require.ErrorIs(t, <-runResult, ErrOwnershipLost)
	require.ErrorIs(t, context.Cause(activeCtx), ErrOwnershipLost)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorRenewalCannotReacquireAfterWatchdogDemotion(t *testing.T) {
	// Arrange
	store := &fakeLeaseStore{}
	coordinator := newCoordinatorWithHolder(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Second)},
		store,
		coordinatorTestConfig(),
		uuid.New(),
	)
	require.NoError(t, coordinator.step(t.Context()))
	activeCtx, _, active := coordinator.ActiveLifetime()
	require.True(t, active)
	coordinator.deactivate(ErrOwnershipExpired)

	// Act
	err := coordinator.renewActive(t.Context(), activeCtx)

	// Assert
	require.ErrorIs(t, err, ErrOwnershipLost)
	require.Equal(t, 1, store.acquireCount())
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

func TestCoordinatorCannotRenewLeaseThatExpiredBeforeAcquireReturned(t *testing.T) {
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

	require.ErrorIs(t, coordinator.step(t.Context()), ErrOwnershipLost)
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

func TestCoordinatorRejectsLateRenewalBeforeWatchdogCallback(t *testing.T) {
	for _, phase := range []string{"store response", "closing proof"} {
		for _, delay := range []time.Duration{6 * time.Second, 7 * time.Second, 8 * time.Second} {
			t.Run(fmt.Sprintf("%s/%s", phase, delay), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					config := coordinatorTestConfig()
					config.LeaseDuration = 10 * time.Second
					config.RenewInterval = 3 * time.Second
					store := &delayedRenewalStore{}
					observed := coordinatorObservation("cluster-a", 41, time.Minute)
					coordinator := newCoordinatorWithHolder(staticObserver{observation: observed}, store, config, uuid.New())
					require.NoError(t, coordinator.step(t.Context()))
					activeCtx, _, active := coordinator.ActiveLifetime()
					require.True(t, active)
					gate := newGate()
					gate.activate(activeCtx)
					defer coordinator.deactivate(ErrPlannedStop)
					defer gate.deactivate()

					// Delay delivery of the old watchdog callback independently of
					// renewal completion, as can happen when goroutines resume late.
					coordinator.mu.Lock()
					stopped := coordinator.leaseTimer.Stop()
					coordinator.mu.Unlock()
					require.True(t, stopped)
					time.Sleep(config.RenewInterval)
					if phase == "store response" {
						store.delay = delay
					} else {
						coordinator.observer = delayedClosingProofObserver{observation: observed, delay: delay}
					}

					// The write succeeds before expiry. The completion is timely,
					// exactly at the old deadline, or late; the new TTL stays live.
					err := coordinator.renewActive(t.Context(), activeCtx)

					require.Equal(t, []string{"acquire", "renew", "renew"}, store.callSequence())
					if delay < config.LeaseDuration-config.RenewInterval {
						require.NoError(t, err)
						time.Sleep(2 * time.Second)
						require.NoError(t, activeCtx.Err())
						require.True(t, gate.Active())
						time.Sleep(2 * time.Second)
						synctest.Wait()
						require.ErrorIs(t, context.Cause(activeCtx), ErrOwnershipExpired)
						return
					}
					require.ErrorIs(t, err, ErrOwnershipExpired)
					require.ErrorContains(t, err, "HA renewal completed after prior local deadline")
					require.ErrorIs(t, context.Cause(activeCtx), ErrOwnershipExpired)
					require.Equal(t, StatePassive, coordinator.Snapshot().State)
					require.False(t, gate.Active())
					_, _, err = gate.Admit(t.Context())
					require.ErrorIs(t, err, ErrNotActive)
				})
			})
		}
	}
}

func TestCoordinatorDoesNotAcceptOrReplayUncertainRenewal(t *testing.T) {
	for _, outcome := range []string{"parent canceled", "response lost"} {
		t.Run(outcome, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := &delayedRenewalStore{}
				coordinator := newCoordinatorWithHolder(
					staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Minute)},
					store, coordinatorTestConfig(), uuid.New(),
				)
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(ErrPlannedStop)
				defer coordinator.deactivate(ErrPlannedStop)
				require.NoError(t, coordinator.step(ctx))
				activeCtx, _, active := coordinator.ActiveLifetime()
				require.True(t, active)
				wantErr := io.ErrUnexpectedEOF
				if outcome == "parent canceled" {
					store.afterRenew = func() { cancel(ErrPlannedStop) }
					wantErr = ErrPlannedStop
				} else {
					store.responseErr = wantErr
				}

				err := coordinator.renewActive(ctx, activeCtx)

				require.ErrorIs(t, err, wantErr)
				require.Error(t, activeCtx.Err())
				require.Equal(t, StatePassive, coordinator.Snapshot().State)
				require.Equal(t, []string{"acquire", "renew", "renew"}, store.callSequence())
			})
		})
	}
}

type delayedRenewalStore struct {
	fakeLeaseStore
	delay       time.Duration
	afterRenew  func()
	responseErr error
}

func (s *delayedRenewalStore) Renew(ctx context.Context, observed WriterObservation, active Ownership, duration time.Duration) (Ownership, error) {
	renewed, err := s.fakeLeaseStore.Renew(ctx, observed, active, duration)
	if s.afterRenew != nil {
		s.afterRenew()
	}
	// A committed response can arrive after cancellation; do not simulate a
	// rollback or a second mutation when its delivery is delayed.
	time.Sleep(s.delay)
	if s.responseErr != nil {
		return Ownership{}, s.responseErr
	}
	return renewed, err
}

type delayedClosingProofObserver struct {
	observation WriterObservation
	delay       time.Duration
}

func (o delayedClosingProofObserver) ObserveAndRun(ctx context.Context, action func(context.Context, WriterObservation) error) (WriterObservation, error) {
	if err := action(ctx, o.observation); err != nil {
		return WriterObservation{}, err
	}
	time.Sleep(o.delay)
	return o.observation, nil
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

func (c *Coordinator) step(ctx context.Context) error {
	activeCtx, _, active := c.ActiveLifetime()
	if active {
		return c.renewActive(ctx, activeCtx)
	}
	_, err := c.tryAcquire(ctx)
	return err
}

type staticObserver struct {
	observation WriterObservation
	err         error
}

type closingProofObserver struct {
	observation WriterObservation
	completed   *bool
}

func (o *closingProofObserver) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	if err := action(ctx, o.observation); err != nil {
		return WriterObservation{}, err
	}
	*o.completed = true
	return o.observation, nil
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
	calls   chan struct{}
}

func (s *sequenceObserver) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	s.mu.Lock()
	result := s.results[0]
	s.results = s.results[1:]
	s.mu.Unlock()
	if s.calls != nil {
		s.calls <- struct{}{}
	}
	if result.err != nil {
		return result.observation, result.err
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
	calls        []string
	acquireErr   error
	renewErr     error
	acquireDelay time.Duration
	acquires     int
	releaseErr   error
}

func (f *fakeLeaseStore) Acquire(
	_ context.Context,
	observed WriterObservation,
	holder uuid.UUID,
	duration time.Duration,
) (Ownership, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquires++
	f.calls = append(f.calls, "acquire")
	if f.acquireErr != nil {
		return Ownership{}, f.acquireErr
	}
	now := time.Now()
	if f.acquireDelay > 0 {
		time.Sleep(f.acquireDelay)
	}
	active := Ownership{
		DCSClusterID: observed.DCSClusterID,
		Token: Token{
			WriterGeneration: observed.WriterGeneration,
			LeaseEpoch:       1,
		},
		HolderID:     holder,
		DatabaseTime: now,
		ExpiresAt:    now.Add(duration),
	}
	return active, nil
}

func (f *fakeLeaseStore) acquireCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquires
}

func (f *fakeLeaseStore) Renew(
	_ context.Context,
	_ WriterObservation,
	active Ownership,
	duration time.Duration,
) (Ownership, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "renew")
	if f.renewErr != nil {
		return Ownership{}, f.renewErr
	}
	now := time.Now()
	if !active.ExpiresAt.After(now) {
		return Ownership{}, ErrOwnershipLost
	}
	active.DatabaseTime = now
	active.ExpiresAt = now.Add(duration)
	return active, nil
}

func (f *fakeLeaseStore) setRenewError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewErr = err
}

func (f *fakeLeaseStore) callSequence() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeLeaseStore) Release(_ context.Context, _ Ownership) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "release")
	return f.releaseErr
}

func TestCoordinatorRetainsPlannedStopCauseOnActiveLifetime(t *testing.T) {
	coordinator, err := NewCoordinator(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Minute)},
		&fakeLeaseStore{}, coordinatorTestConfig())
	require.NoError(t, err)
	runCtx, cancelRun := context.WithCancelCause(t.Context())
	result := make(chan error, 1)
	go func() { result <- coordinator.Run(runCtx) }()
	activeCtx, _, err := coordinator.WaitForActive(t.Context())
	require.NoError(t, err)
	cancelRun(ErrPlannedStop)
	require.ErrorIs(t, <-result, context.Canceled)
	require.ErrorIs(t, context.Cause(activeCtx), ErrPlannedStop)
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
}

func TestCoordinatorReleaseRequiresCurrentHolder(t *testing.T) {
	// Arrange
	store := &fakeLeaseStore{}
	coordinator, err := NewCoordinator(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Minute)},
		store, coordinatorTestConfig())
	require.NoError(t, err)

	// Act
	err = coordinator.Release(t.Context(), Ownership{HolderID: uuid.New()})

	// Assert
	require.ErrorIs(t, err, ErrOwnershipLost)
	require.NotContains(t, store.callSequence(), "release")
}

func TestCoordinatorReleaseWrapsStoreError(t *testing.T) {
	// Arrange
	storeErr := errors.New("database unavailable")
	store := &fakeLeaseStore{releaseErr: storeErr}
	coordinator, err := NewCoordinator(
		staticObserver{observation: coordinatorObservation("cluster-a", 41, time.Minute)},
		store, coordinatorTestConfig())
	require.NoError(t, err)

	// Act
	err = coordinator.Release(t.Context(), Ownership{HolderID: coordinator.HolderID()})

	// Assert
	require.ErrorIs(t, err, storeErr)
	require.ErrorContains(t, err, "release Fleet ownership term")
	require.Equal(t, []string{"release"}, store.callSequence())
}
