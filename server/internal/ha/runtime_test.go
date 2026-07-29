package ha

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
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
	demotions   chan error
	resumed     chan struct{}
}

func newRuntimeTestOwner() *runtimeTestOwner {
	return &runtimeTestOwner{
		activations: make(chan runtimeTestActivation, 4),
		demotions:   make(chan error, 4),
		resumed:     make(chan struct{}, 4),
	}
}

func (o *runtimeTestOwner) Run(ctx context.Context) error {
	<-ctx.Done()
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

func (o *runtimeTestOwner) RequestDemotion(err error) {
	o.demotions <- err
}

func (o *runtimeTestOwner) ResumeAcquisition() {
	o.resumed <- struct{}{}
}

type runtimeTestGroup struct {
	mu sync.Mutex

	started     int
	stopped     int
	startErr    error
	stopErr     error
	terminalErr error
	status      runtimejobs.GroupStatus
	aborted     int
	abortFirst  bool
	startedCh   chan context.Context
	stoppedCh   chan struct{}
}

func newRuntimeTestGroup() *runtimeTestGroup {
	return &runtimeTestGroup{
		status: runtimejobs.GroupStatus{
			State: runtimejobs.StateRunning,
			Jobs: []runtimejobs.JobStatus{{
				Name:  "command-execution",
				State: runtimejobs.StateRunning,
			}},
		},
		startedCh: make(chan context.Context, 4),
		stoppedCh: make(chan struct{}, 4),
	}
}

func (g *runtimeTestGroup) Start(ctx context.Context) error {
	g.mu.Lock()
	g.started++
	g.aborted = 0
	err := g.startErr
	g.mu.Unlock()
	g.startedCh <- ctx
	return err
}

func (g *runtimeTestGroup) Stop(context.Context) error {
	g.mu.Lock()
	g.stopped++
	g.abortFirst = g.aborted > 0
	err := g.stopErr
	g.mu.Unlock()
	g.stoppedCh <- struct{}{}
	return err
}

func (g *runtimeTestGroup) Abort() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.aborted++
}

func (g *runtimeTestGroup) wasAbortedBeforeStop() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.abortFirst
}

func (g *runtimeTestGroup) Status() runtimejobs.GroupStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.status
}

func (g *runtimeTestGroup) Err() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.terminalErr
}

func (g *runtimeTestGroup) setStale(stale bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status.Jobs[0].Stale = stale
}

func TestNewRuntimeRequiresExplicitCriticalHealth(t *testing.T) {
	group, err := runtimejobs.NewGroup(nil)
	require.NoError(t, err)

	_, err = NewRuntime(&Coordinator{}, group)
	require.ErrorContains(t, err, "at least one critical health check")

	_, err = NewRuntime(&Coordinator{}, group, nil)
	require.ErrorContains(t, err, "must not be nil")

	_, err = NewStandaloneRuntime(group)
	require.ErrorContains(t, err, "at least one critical health check")

	_, err = NewStandaloneRuntime(group, nil)
	require.ErrorContains(t, err, "must not be nil")
}

func TestRuntimeStartsOnlyForOwnedLifetimeAndDrainsOnDemotion(t *testing.T) {
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	runtime := newRuntime(owner, group, nil, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	require.Never(t, runtime.Active, 20*time.Millisecond, time.Millisecond)
	select {
	case <-group.startedCh:
		t.Fatal("passive runtime started jobs")
	default:
	}

	activeCtx, cancelActive := context.WithCancel(t.Context())
	owner.activations <- runtimeTestActivation{
		ctx:   activeCtx,
		token: Token{WriterGeneration: 7, LeaseEpoch: 11},
	}
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)

	requestCtx, release, err := runtime.Admit(t.Context())
	require.NoError(t, err)

	group.setStale(true)
	require.Never(t, func() bool { return requestCtx.Err() != nil }, 20*time.Millisecond, time.Millisecond)

	cancelActive()
	require.Eventually(t, func() bool { return requestCtx.Err() != nil }, eventuallyTimeout, eventuallyInterval)
	requireReceive(t, group.stoppedCh)
	require.Never(t, channelClosed(owner.resumed), 20*time.Millisecond, time.Millisecond)
	release()
	requireReceive(t, owner.resumed)
	require.False(t, runtime.Active())

	cancelRun()
	require.NoError(t, <-runResult)
}

func TestRuntimeAdmissionDrainTimeoutIsTerminal(t *testing.T) {
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	config := runtimeTestConfig()
	config.CleanupTimeout = 20 * time.Millisecond
	runtime := newRuntime(owner, group, nil, config)
	runCtx, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	activeCtx, cancelActive := context.WithCancel(t.Context())
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	_, release, err := runtime.Admit(t.Context())
	require.NoError(t, err)
	defer release()

	cancelActive()
	requireReceive(t, group.stoppedCh)
	require.ErrorContains(t, <-runResult, "drain active Fleet requests")
	require.Never(t, channelClosed(owner.resumed), 20*time.Millisecond, time.Millisecond)
}

func TestRuntimeDemotesWhenCriticalHealthFailsAfterAdmission(t *testing.T) {
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	var healthy atomic.Bool
	healthy.Store(true)
	runtime := newRuntime(owner, group, []HealthCheck{healthy.Load}, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	requestCtx, release, err := runtime.Admit(t.Context())
	require.NoError(t, err)

	healthy.Store(false)
	_ = requireReceiveError(t, owner.demotions)
	requireReceive(t, group.stoppedCh)
	require.Eventually(t, func() bool { return requestCtx.Err() != nil }, eventuallyTimeout, eventuallyInterval)
	require.Never(t, channelClosed(owner.resumed), 20*time.Millisecond, time.Millisecond)
	release()
	requireReceive(t, owner.resumed)
	require.False(t, runtime.Active())
	require.True(t, group.wasAbortedBeforeStop())

	cancelRun()
	require.NoError(t, <-runResult)
}

func TestRuntimeDemotesWhenCriticalHealthFailsDuringStartup(t *testing.T) {
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	runtime := newRuntime(owner, group, []HealthCheck{func() bool { return false }}, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	_ = requireReceiveError(t, owner.demotions)
	requireReceive(t, group.stoppedCh)
	requireReceive(t, owner.resumed)
	require.False(t, runtime.Active())

	cancelRun()
	require.NoError(t, <-runResult)
}

func TestRuntimeStopsOwnershipAfterTerminalCleanupFailure(t *testing.T) {
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	group.stopErr = errors.New("cleanup failed")
	group.terminalErr = group.stopErr
	runtime := newRuntime(owner, group, nil, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	activeCtx, cancelActive := context.WithCancel(t.Context())
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	cancelActive()

	err := <-runResult
	require.ErrorContains(t, err, "cleanup failed")
	require.False(t, runtime.Active())
}

func TestRuntimeStartFailureDemotesWithoutAdmission(t *testing.T) {
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	group.startErr = errors.New("start failed")
	runtime := newRuntime(owner, group, nil, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	require.ErrorContains(t, requireReceiveError(t, owner.demotions), "start failed")
	requireReceive(t, owner.resumed)
	require.False(t, runtime.Active())

	cancelRun()
	require.NoError(t, <-runResult)
}

func TestRuntimeTerminalStartFailureStopsCoordinator(t *testing.T) {
	owner := newRuntimeTestOwner()
	group := newRuntimeTestGroup()
	group.startErr = errors.New("start failed")
	group.terminalErr = errors.New("rollback failed")
	runtime := newRuntime(owner, group, nil, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	owner.activations <- runtimeTestActivation{ctx: activeCtx}
	requireReceiveContext(t, group.startedCh)
	err := <-runResult
	require.ErrorContains(t, err, "rollback failed")
	select {
	case <-owner.resumed:
		t.Fatal("terminal cleanup failure resumed acquisition")
	default:
	}
}

func TestStandaloneRuntimePreservesSingleHostLifecycle(t *testing.T) {
	group := newRuntimeTestGroup()
	runtime := newStandaloneRuntime(group, []HealthCheck{func() bool { return true }}, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	cancelRun()
	requireReceive(t, group.stoppedCh)
	require.NoError(t, <-runResult)
}

func TestStandaloneRuntimeIgnoresObservationalJobStaleness(t *testing.T) {
	group := newRuntimeTestGroup()
	runtime := newStandaloneRuntime(group, []HealthCheck{func() bool { return true }}, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	requestCtx, release, err := runtime.Admit(t.Context())
	require.NoError(t, err)
	defer release()

	group.setStale(true)
	require.Never(t, func() bool { return requestCtx.Err() != nil }, 20*time.Millisecond, time.Millisecond)
	require.True(t, runtime.Active())

	cancelRun()
	requireReceive(t, group.stoppedCh)
	require.NoError(t, <-runResult)
	require.False(t, runtime.Active())
}

func TestStandaloneRuntimeStopsWhenCriticalHealthFails(t *testing.T) {
	group := newRuntimeTestGroup()
	var healthy atomic.Bool
	healthy.Store(true)
	runtime := newStandaloneRuntime(group, []HealthCheck{healthy.Load}, runtimeTestConfig())
	runCtx, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	runResult := make(chan error, 1)
	go func() {
		runResult <- runtime.Run(runCtx)
	}()

	requireReceiveContext(t, group.startedCh)
	require.Eventually(t, runtime.Active, eventuallyTimeout, eventuallyInterval)
	requestCtx, release, err := runtime.Admit(t.Context())
	require.NoError(t, err)
	defer release()

	healthy.Store(false)
	requireReceive(t, group.stoppedCh)
	require.Eventually(t, func() bool { return requestCtx.Err() != nil }, eventuallyTimeout, eventuallyInterval)
	require.False(t, runtime.Active())
	require.ErrorIs(t, <-runResult, errCriticalRuntimeUnhealthy)
}

func runtimeTestConfig() RuntimeConfig {
	return RuntimeConfig{
		ActivationTimeout:   10 * time.Millisecond,
		HealthCheckInterval: time.Millisecond,
		CleanupTimeout:      time.Second,
	}
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

func requireReceiveError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(eventuallyTimeout):
		t.Fatal("timed out waiting for error")
		return nil
	}
}
