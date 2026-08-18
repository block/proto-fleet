package ha

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	eventuallyTimeout  = time.Second
	eventuallyInterval = time.Millisecond
)

type runtimeTestActivation struct {
	ctx   context.Context //nolint:containedctx // Models an ownership lifetime delivered to the fake.
	token Token
}

type runtimeTestOwner struct {
	activations chan runtimeTestActivation
	stopped     chan struct{}
}

func newRuntimeTestOwner() *runtimeTestOwner {
	return &runtimeTestOwner{
		activations: make(chan runtimeTestActivation, 1),
		stopped:     make(chan struct{}),
	}
}

func (o *runtimeTestOwner) Run(ctx context.Context) error {
	<-ctx.Done()
	close(o.stopped)
	return fmt.Errorf("runtime test owner stopped: %w", ctx.Err())
}

func (o *runtimeTestOwner) WaitForActive(ctx context.Context) (context.Context, Token, error) {
	select {
	case activation := <-o.activations:
		return activation.ctx, activation.token, nil
	case <-ctx.Done():
		return nil, Token{}, fmt.Errorf("wait for test activation: %w", ctx.Err())
	}
}

func (o *runtimeTestOwner) Snapshot() Snapshot { return Snapshot{} }

type missedActivationOwner struct{}

func (missedActivationOwner) Run(context.Context) error {
	return ErrOwnershipLost
}

func (missedActivationOwner) WaitForActive(ctx context.Context) (context.Context, Token, error) {
	<-ctx.Done()
	return nil, Token{}, fmt.Errorf("wait for missed activation: %w", ctx.Err())
}

func (missedActivationOwner) Snapshot() Snapshot { return Snapshot{} }

type runtimeTestGroup struct {
	mu sync.Mutex

	started   int
	stopped   int
	aborted   int
	startErr  error
	stopErr   error
	abortErr  error
	abortWait <-chan struct{}
	startedCh chan context.Context
	stoppedCh chan struct{}
	abortedCh chan context.Context
}

func newRuntimeTestGroup() *runtimeTestGroup {
	return &runtimeTestGroup{
		startedCh: make(chan context.Context, 1),
		stoppedCh: make(chan struct{}, 1),
		abortedCh: make(chan context.Context, 1),
	}
}

func (g *runtimeTestGroup) Abort(ctx context.Context) error {
	g.mu.Lock()
	g.aborted++
	err := g.abortErr
	g.mu.Unlock()
	g.abortedCh <- ctx
	if g.abortWait != nil {
		select {
		case <-g.abortWait:
		case <-ctx.Done():
		}
	}
	return err
}

func (g *runtimeTestGroup) Start(ctx context.Context) error {
	g.mu.Lock()
	g.started++
	err := g.startErr
	g.mu.Unlock()
	g.startedCh <- ctx
	return err
}

func (g *runtimeTestGroup) Stop(context.Context) error {
	g.mu.Lock()
	g.stopped++
	err := g.stopErr
	g.mu.Unlock()
	g.stoppedCh <- struct{}{}
	return err
}

func TestNewRuntimeRequiresExplicitCriticalHealth(t *testing.T) {
	group, err := runtimejobs.NewGroup(nil)
	require.NoError(t, err)

	_, err = NewRuntime(&Coordinator{}, group, nil)
	require.ErrorContains(t, err, "requires a critical health check")

	_, err = NewStandaloneRuntime(group, nil)
	require.ErrorContains(t, err, "requires a critical health check")
}

func TestHARuntimeStartsOnlyAfterOwnership(t *testing.T) {
	// Arrange
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	runtime := newRuntime(owner, group, alwaysHealthy, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() { runResult <- runtime.Run(runCtx) }()

	// Act
	require.Never(t, runtime.Active, 20*time.Millisecond, time.Millisecond)
	activeCtx, cancelActive := context.WithCancel(t.Context())
	owner.activations <- runtimeTestActivation{ctx: activeCtx}

	// Assert
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)

	cancelRun()
	cancelActive()
	requireReceive(t, group.stoppedCh)
	require.NoError(t, <-runResult)
}

func TestHARuntimeLogsDegradedWhenRuntimeGroupFailsToStart(t *testing.T) {
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	group.startErr = errors.New("start failed")
	runtime := newRuntime(owner, group, alwaysHealthy, runtimeTestConfig())
	logger, logs := newHATestLogger()
	runtime.logger = logger
	runResult := make(chan error, 1)
	go func() { runResult <- runtime.Run(t.Context()) }()
	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	owner.activations <- runtimeTestActivation{ctx: activeCtx}

	requireReceiveContext(t, group.startedCh)
	runErr := <-runResult
	require.ErrorContains(t, runErr, "start failed")
	requireReceive(t, owner.stopped)
	record := requireHAEvent(t, logs, haEventStateDegraded)
	require.Equal(t, "critical_runtime_unhealthy", logAttr(record, "reason"))
}

func TestHARuntimeReturnsWhenOwnershipEndsBeforeActivationIsObserved(t *testing.T) {
	// Arrange
	runtime := newRuntime(missedActivationOwner{}, newRuntimeTestGroup(), alwaysHealthy, runtimeTestConfig())

	// Act
	err := runtime.Run(t.Context())

	// Assert
	require.ErrorIs(t, err, ErrOwnershipLost)
	require.False(t, runtime.Active())
}

func TestHARuntimeAbortsOnOwnershipLoss(t *testing.T) {
	// Arrange
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	runtime := newRuntime(owner, group, alwaysHealthy, runtimeTestConfig())
	runResult := make(chan error, 1)
	go func() { runResult <- runtime.Run(t.Context()) }()
	activeCtx, cancelActive := context.WithCancelCause(t.Context())
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	jobCtx := requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	requestCtx, release, err := runtime.Admit(t.Context())
	require.NoError(t, err)
	defer release()

	// Act
	cancelActive(ErrOwnershipLost)

	// Assert
	runErr := <-runResult
	require.ErrorIs(t, runErr, ErrRuntimeAborted)
	require.ErrorIs(t, runErr, ErrOwnershipLost)
	abortCtx := requireReceiveContext(t, group.abortedCh)
	_, hasDeadline := abortCtx.Deadline()
	require.True(t, hasDeadline)
	require.Eventually(t, func() bool { return requestCtx.Err() != nil }, eventuallyTimeout, eventuallyInterval)
	require.False(t, runtime.Active())
	require.Error(t, jobCtx.Err())
}

func TestHARuntimeSurvivesTransientTimelineMismatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		config := coordinatorTestConfig()
		config.LeaseDuration = 10 * time.Second
		config.RenewInterval = 3 * time.Second
		observer := &sequenceObserver{
			results: []observerResult{
				{observation: coordinatorObservation("cluster-a", 41, 20*time.Second)},
				{
					observation: coordinatorObservation("cluster-a", 41, 20*time.Second),
					err:         fmt.Errorf("promotion observation: %w", ErrTimelineMismatch),
				},
				{observation: coordinatorObservation("cluster-a", 41, 20*time.Second)},
			},
			calls: make(chan struct{}, 3),
		}
		store := &fakeLeaseStore{}
		coordinator := newCoordinatorWithHolder(observer, store, config, uuid.New())
		group := newRuntimeTestGroup()
		runtimeConfig := runtimeTestConfig()
		runtimeConfig.HealthCheckInterval = time.Second
		runtime := newRuntime(coordinator, group, alwaysHealthy, runtimeConfig)
		runCtx, cancelRun := context.WithCancel(context.Background())
		runResult := make(chan error, 1)
		go func() { runResult <- runtime.Run(runCtx) }()

		requireReceive(t, observer.calls)
		requireReceiveContext(t, group.startedCh)
		synctest.Wait()
		require.True(t, runtime.Active())
		validSnapshot := coordinator.Snapshot()
		validTimer := coordinator.leaseTimer

		time.Sleep(config.RenewInterval)
		synctest.Wait()
		requireReceive(t, observer.calls)
		require.True(t, runtime.Active())
		require.False(t, coordinator.Snapshot().ObservationAvailable)
		require.Equal(t, validSnapshot.FreshUntil, coordinator.Snapshot().FreshUntil)
		require.Same(t, validTimer, coordinator.leaseTimer)
		require.Equal(t, []string{"acquire", "renew"}, store.callSequence())

		time.Sleep(config.RenewInterval)
		synctest.Wait()
		requireReceive(t, observer.calls)
		require.True(t, runtime.Active())
		require.True(t, coordinator.Snapshot().ObservationAvailable)
		require.Equal(t, []string{"acquire", "renew", "renew"}, store.callSequence())

		cancelRun()
		synctest.Wait()
		requireReceive(t, group.stoppedCh)
		require.NoError(t, <-runResult)
		select {
		case <-group.abortedCh:
			t.Fatal("transient timeline mismatch aborted the Fleet runtime")
		default:
		}
	})
}

func TestHARuntimeRetriesTimelineMismatchBeforeActivation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		config := coordinatorTestConfig()
		observer := &sequenceObserver{
			results: []observerResult{
				{err: fmt.Errorf("promotion observation: %w", ErrTimelineMismatch)},
				{observation: coordinatorObservation("cluster-a", 42, 20*time.Second)},
			},
			calls: make(chan struct{}, 2),
		}
		store := &fakeLeaseStore{}
		coordinator := newCoordinatorWithHolder(observer, store, config, uuid.New())
		group := newRuntimeTestGroup()
		runtime := newRuntime(coordinator, group, alwaysHealthy, runtimeTestConfig())
		runCtx, cancelRun := context.WithCancel(context.Background())
		runResult := make(chan error, 1)
		go func() { runResult <- runtime.Run(runCtx) }()

		requireReceive(t, observer.calls)
		synctest.Wait()
		require.False(t, runtime.Active())
		require.Empty(t, store.callSequence())
		select {
		case err := <-runResult:
			t.Fatalf("Fleet runtime exited after transient timeline mismatch: %v", err)
		default:
		}

		time.Sleep(config.RetryInterval)
		synctest.Wait()
		requireReceive(t, observer.calls)
		requireReceiveContext(t, group.startedCh)
		require.True(t, runtime.Active())
		require.Equal(t, []string{"acquire", "renew"}, store.callSequence())

		cancelRun()
		synctest.Wait()
		requireReceive(t, group.stoppedCh)
		require.NoError(t, <-runResult)
		select {
		case <-group.abortedCh:
			t.Fatal("pre-activation timeline mismatch aborted the Fleet runtime")
		default:
		}
	})
}

func TestHARuntimePersistentTimelineMismatchExpiresExistingProof(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		config := coordinatorTestConfig()
		config.LeaseDuration = 10 * time.Second
		config.RenewInterval = 3 * time.Second
		observed := coordinatorObservation("cluster-a", 41, 20*time.Second)
		mismatch := observerResult{
			observation: observed,
			err:         fmt.Errorf("promotion observation: %w", ErrTimelineMismatch),
		}
		observer := &sequenceObserver{results: []observerResult{
			{observation: observed},
			mismatch,
			mismatch,
			mismatch,
		}}
		store := &fakeLeaseStore{}
		coordinator := newCoordinatorWithHolder(observer, store, config, uuid.New())
		group := newRuntimeTestGroup()
		runtimeConfig := runtimeTestConfig()
		runtimeConfig.HealthCheckInterval = time.Second
		runtime := newRuntime(coordinator, group, alwaysHealthy, runtimeConfig)
		runResult := make(chan error, 1)
		go func() { runResult <- runtime.Run(context.Background()) }()

		requireReceiveContext(t, group.startedCh)
		synctest.Wait()
		require.True(t, runtime.Active())

		runErr := <-runResult
		require.ErrorIs(t, runErr, ErrRuntimeAborted)
		require.ErrorIs(t, runErr, ErrOwnershipExpired)
		requireReceiveContext(t, group.abortedCh)
		require.False(t, runtime.Active())
		require.Equal(t, []string{"acquire", "renew"}, store.callSequence())
	})
}

func TestHARuntimeExitsWhenCriticalHealthFails(t *testing.T) {
	// Arrange
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	var healthy atomic.Bool
	healthy.Store(true)
	runtime := newRuntime(owner, group, healthy.Load, runtimeTestConfig())
	logger, logs := newHATestLogger()
	runtime.logger = logger
	runResult := make(chan error, 1)
	go func() { runResult <- runtime.Run(t.Context()) }()
	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)

	// Act
	healthy.Store(false)

	// Assert
	runErr := <-runResult
	require.ErrorIs(t, runErr, ErrRuntimeAborted)
	require.ErrorIs(t, runErr, errCriticalRuntimeUnhealthy)
	requireReceiveContext(t, group.abortedCh)
	require.False(t, runtime.Active())
	record := requireHAEvent(t, logs, haEventStateDegraded)
	require.Equal(t, "critical_runtime_unhealthy", logAttr(record, "reason"))
}

func TestEndpointMonitorAllowsDelayedStartupThenFailsClosed(t *testing.T) {
	// Arrange
	healthy := false
	startedAt := time.Unix(100, 0)
	monitor := newEndpointMonitor(func() bool { return healthy }, startedAt, endpointStartupTimeout)

	// Act and assert
	require.NoError(t, monitor.check(startedAt.Add(endpointHeartbeatTimeout)))
	healthy = true
	require.NoError(t, monitor.check(startedAt.Add(endpointStartupTimeout-time.Second)))
	healthy = false
	require.NoError(t, monitor.check(startedAt.Add(endpointStartupTimeout)))
	healthy = true
	require.NoError(t, monitor.check(startedAt.Add(endpointStartupTimeout+time.Second)))
	healthy = false
	require.NoError(t, monitor.check(startedAt.Add(endpointStartupTimeout+2*time.Second)))
	require.NoError(t, monitor.check(startedAt.Add(endpointStartupTimeout+3*time.Second)))
	require.ErrorIs(t, monitor.check(startedAt.Add(endpointStartupTimeout+4*time.Second)), errEndpointUnavailable)

	neverReady := newEndpointMonitor(func() bool { return false }, startedAt, endpointStartupTimeout)
	require.ErrorIs(t, neverReady.check(startedAt.Add(endpointStartupTimeout)), errEndpointUnavailable)
}

func TestEndpointLossStopsLeaseRenewalBeforeAbortCleanup(t *testing.T) {
	// Arrange
	owner := newRuntimeTestOwner()
	abortWait := make(chan struct{})
	group := newRuntimeTestGroup()
	group.abortWait = abortWait
	var endpointHealthy atomic.Bool
	endpointChecked := make(chan struct{}, 1)
	endpointHealthy.Store(true)
	config := runtimeTestConfig()
	config.EndpointHealthy = func() bool {
		healthy := endpointHealthy.Load()
		if healthy {
			select {
			case endpointChecked <- struct{}{}:
			default:
			}
		}
		return healthy
	}
	runtime := newRuntime(owner, group, alwaysHealthy, config)
	logger, logs := newHATestLogger()
	runtime.logger = logger
	runResult := make(chan error, 1)
	go func() { runResult <- runtime.Run(t.Context()) }()
	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	requireReceive(t, endpointChecked)

	// Act
	endpointHealthy.Store(false)
	requireReceiveContext(t, group.abortedCh)

	// Assert
	requireReceive(t, owner.stopped)
	close(abortWait)
	runErr := <-runResult
	require.ErrorIs(t, runErr, ErrRuntimeAborted)
	require.ErrorIs(t, runErr, errEndpointUnavailable)
	require.False(t, runtime.Active())
	record := requireHAEvent(t, logs, haEventStateDegraded)
	require.Equal(t, string(ReasonEndpointUnhealthy), logAttr(record, "reason"))
}

func TestHARuntimeJoinsAbortCleanupError(t *testing.T) {
	// Arrange
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	cleanupErr := errors.New("cleanup failed")
	group.abortErr = cleanupErr
	runtime := newRuntime(owner, group, alwaysHealthy, runtimeTestConfig())
	runResult := make(chan error, 1)
	go func() { runResult <- runtime.Run(t.Context()) }()
	activeCtx, cancelActive := context.WithCancelCause(t.Context())
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)

	// Act
	cancelActive(ErrOwnershipLost)

	// Assert
	runErr := <-runResult
	require.ErrorIs(t, runErr, ErrRuntimeAborted)
	require.ErrorIs(t, runErr, ErrOwnershipLost)
	require.ErrorIs(t, runErr, cleanupErr)
}

func TestStandaloneRuntimePreservesGracefulLifecycle(t *testing.T) {
	// Arrange
	group := newRuntimeTestGroup()
	runtime := newRuntime(nil, group, alwaysHealthy, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() { runResult <- runtime.Run(runCtx) }()

	// Act
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	cancelRun()

	// Assert
	requireReceive(t, group.stoppedCh)
	require.NoError(t, <-runResult)
}

func TestStandaloneRuntimeStopsWhenCriticalHealthFails(t *testing.T) {
	// Arrange
	group := newRuntimeTestGroup()
	var healthy atomic.Bool
	healthy.Store(true)
	runtime := newRuntime(nil, group, healthy.Load, runtimeTestConfig())
	runResult := make(chan error, 1)
	go func() { runResult <- runtime.Run(t.Context()) }()
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	requestCtx, release, err := runtime.Admit(t.Context())
	require.NoError(t, err)
	defer release()

	// Act
	healthy.Store(false)

	// Assert
	requireReceive(t, group.stoppedCh)
	require.Eventually(t, func() bool { return requestCtx.Err() != nil }, eventuallyTimeout, eventuallyInterval)
	require.False(t, runtime.Active())
	require.ErrorIs(t, <-runResult, errCriticalRuntimeUnhealthy)
}

func runtimeTestConfig() RuntimeConfig {
	return RuntimeConfig{
		HealthCheckInterval: time.Millisecond,
		CleanupTimeout:      time.Second,
	}
}

func alwaysHealthy() bool {
	return true
}

func requireReceive(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(eventuallyTimeout):
		t.Fatal("timed out waiting for signal")
	}
}

func requireReceiveContext(t *testing.T, ch <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-ch:
		return ctx
	case <-time.After(eventuallyTimeout):
		t.Fatal("timed out waiting for context")
		return nil
	}
}
