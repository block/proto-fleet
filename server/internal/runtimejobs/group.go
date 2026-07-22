package runtimejobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Group owns at most one activation of an ordered set of jobs at a time.
//
// Lifecycle operations are serialized. A group can restart after a clean stop,
// but incomplete cleanup permanently prevents another activation.
type Group struct {
	stateMu         sync.Mutex
	operationPermit chan struct{}

	cleanupTimeout time.Duration
	jobs           []Job
	terminalErr    error
	pendingCleanup []Job
	stopAttempts   map[string]<-chan error
	activationDone <-chan struct{}
	cancel         context.CancelFunc
	stopGeneration uint64
}

// NewGroup validates jobs and creates a stopped group. cleanupTimeout is one
// wall-clock budget shared by every job during a stop or startup rollback.
func NewGroup(jobs []Job, cleanupTimeout time.Duration) (*Group, error) {
	if cleanupTimeout <= 0 {
		return nil, errors.New("runtime job cleanup timeout must be positive")
	}
	seen := make(map[string]struct{}, len(jobs))
	for i, job := range jobs {
		if err := job.validate(); err != nil {
			return nil, fmt.Errorf("runtime job %d: %w", i, err)
		}
		if _, ok := seen[job.Name()]; ok {
			return nil, fmt.Errorf("runtime job name %q appears more than once", job.Name())
		}
		seen[job.Name()] = struct{}{}
	}

	return &Group{
		cleanupTimeout:  cleanupTimeout,
		jobs:            slices.Clone(jobs),
		operationPermit: make(chan struct{}, 1),
		stopAttempts:    make(map[string]<-chan error),
	}, nil
}

// Err reports the terminal cleanup failure that prevents reactivation.
func (g *Group) Err() error {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.terminalErr
}

// Start starts every job in registration order.
func (g *Group) Start(ctx context.Context) error {
	// A Stop requested after this Start invocation must cancel this activation,
	// including the narrow interval between acquiring the operation permit and
	// publishing the activation cancel function.
	stopGeneration := g.currentStopGeneration()
	if err := g.acquireOperation(ctx); err != nil {
		return err
	}
	defer g.releaseOperation()

	if terminalErr := g.Err(); terminalErr != nil {
		return fmt.Errorf("runtime job group cannot restart after incomplete cleanup: %w", terminalErr)
	}
	activationDone, cancel := g.activation()
	if cancel != nil {
		select {
		case <-activationDone:
			return errors.New("runtime job group activation ended before stop")
		default:
		}
		return nil
	}

	activationCtx, cancel := context.WithCancel(ctx)
	g.setActivation(activationCtx.Done(), cancel, stopGeneration)
	started := 0
	for _, job := range g.jobs {
		if err := activationCtx.Err(); err != nil {
			return g.failStart(ctx, started, fmt.Errorf("start runtime job %q: %w", job.Name(), err))
		}
		if err := job.Start(activationCtx); err != nil {
			return g.failStart(ctx, started, fmt.Errorf("start runtime job %q: %w", job.Name(), err))
		}
		started++
	}
	if err := activationCtx.Err(); err != nil {
		return g.failStart(ctx, started, fmt.Errorf("start runtime job group: %w", err))
	}

	return nil
}

func (g *Group) failStart(ctx context.Context, started int, startErr error) error {
	g.cancelActivation()
	pendingCleanup, rollbackErr := g.stopJobs(ctx, g.jobs[:started])
	g.clearActivation()
	if rollbackErr == nil {
		return startErr
	}

	g.pendingCleanup = pendingCleanup
	terminalErr := errors.Join(startErr, fmt.Errorf("rollback runtime jobs: %w", rollbackErr))
	g.setTerminalErr(terminalErr)
	return terminalErr
}

// Stop broadcasts cancellation, then stops jobs in reverse registration order.
func (g *Group) Stop(ctx context.Context) error {
	// Cancellation is not a lifecycle operation: broadcast it before waiting so
	// a Start blocked inside a job can return and release the operation slot.
	g.requestStop()
	if err := g.acquireOperation(ctx); err != nil {
		return err
	}
	defer g.releaseOperation()

	if len(g.pendingCleanup) > 0 {
		var err error
		g.pendingCleanup, err = g.stopJobs(ctx, g.pendingCleanup)
		return err
	}
	_, cancel := g.activation()
	if cancel == nil {
		return nil
	}

	g.cancelActivation()
	g.clearActivation()
	pendingCleanup, err := g.stopJobs(ctx, g.jobs)
	if err != nil {
		g.pendingCleanup = pendingCleanup
		g.setTerminalErr(err)
		return err
	}
	return nil
}

func (g *Group) acquireOperation(ctx context.Context) error {
	// Context bounds waiting behind another lifecycle operation. If the permit
	// is already free, Stop must still make its best-effort cleanup attempt.
	select {
	case g.operationPermit <- struct{}{}:
		return nil
	default:
	}

	select {
	case g.operationPermit <- struct{}{}:
		if err := ctx.Err(); err != nil {
			g.releaseOperation()
			return fmt.Errorf("acquire runtime job group operation: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("acquire runtime job group operation: %w", ctx.Err())
	}
}

func (g *Group) releaseOperation() {
	<-g.operationPermit
}

func (g *Group) setTerminalErr(err error) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.terminalErr = err
}

func (g *Group) activation() (<-chan struct{}, context.CancelFunc) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.activationDone, g.cancel
}

func (g *Group) currentStopGeneration() uint64 {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.stopGeneration
}

func (g *Group) setActivation(done <-chan struct{}, cancel context.CancelFunc, stopGeneration uint64) {
	g.stateMu.Lock()
	g.activationDone = done
	g.cancel = cancel
	stopRequested := g.stopGeneration != stopGeneration
	g.stateMu.Unlock()
	if stopRequested {
		cancel()
	}
}

func (g *Group) requestStop() {
	g.stateMu.Lock()
	g.stopGeneration++
	cancel := g.cancel
	g.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (g *Group) cancelActivation() {
	_, cancel := g.activation()
	if cancel != nil {
		cancel()
	}
}

func (g *Group) clearActivation() {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.activationDone = nil
	g.cancel = nil
}

func (g *Group) stopJobs(parent context.Context, jobs []Job) ([]Job, error) {
	stopCtx, cancel := context.WithTimeout(parent, g.cleanupTimeout)
	defer cancel()

	var pendingCleanup []Job
	var stopErrors []error
	for _, job := range slices.Backward(jobs) {
		err := g.stopJob(stopCtx, job)
		if err != nil {
			pendingCleanup = append(pendingCleanup, job)
			stopErrors = append(stopErrors, fmt.Errorf("stop runtime job %q: %w", job.Name(), err))
		}
	}
	slices.Reverse(pendingCleanup)
	return pendingCleanup, errors.Join(stopErrors...)
}

func (g *Group) stopJob(stopCtx context.Context, job Job) error {
	if result, ok := g.stopAttempts[job.Name()]; ok {
		err, completed := waitForStopResult(stopCtx, result)
		if !completed {
			return err
		}
		delete(g.stopAttempts, job.Name())
		if err == nil {
			return nil
		}
		// This invocation joined the previous attempt. Once that attempt has
		// returned, one fresh attempt is safe; never retry an attempt started by
		// this same invocation or an immediate error could loop forever.
	}

	result := make(chan error, 1)
	stopGoroutineStarted := make(chan struct{})
	g.stopAttempts[job.Name()] = result
	go func() {
		close(stopGoroutineStarted)
		result <- job.Stop(stopCtx)
	}()
	// Schedule every stop attempt before honoring an already-expired group
	// deadline. Once the caller's budget is gone, entering Stop itself is
	// necessarily best effort; waiting for it could make shutdown unbounded.
	<-stopGoroutineStarted

	err, completed := waitForStopResult(stopCtx, result)
	if completed {
		delete(g.stopAttempts, job.Name())
	}
	return err
}

func waitForStopResult(stopCtx context.Context, result <-chan error) (error, bool) {
	select {
	case err := <-result:
		return err, true
	case <-stopCtx.Done():
		select {
		case err := <-result:
			return err, true
		default:
			return fmt.Errorf("wait for runtime job stop: %w", stopCtx.Err()), false
		}
	}
}
