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
	terminalErrMu   sync.Mutex
	operationPermit chan struct{}

	cleanupTimeout time.Duration
	jobs           []Job
	terminalErr    error
	pendingCleanup []Job
	activationDone <-chan struct{}
	cancel         context.CancelFunc
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
	}, nil
}

// Err reports the terminal cleanup failure that prevents reactivation.
func (g *Group) Err() error {
	g.terminalErrMu.Lock()
	defer g.terminalErrMu.Unlock()
	return g.terminalErr
}

// Start starts every job in registration order.
func (g *Group) Start(ctx context.Context) error {
	if err := g.acquireOperation(ctx); err != nil {
		return err
	}
	defer g.releaseOperation()

	if terminalErr := g.Err(); terminalErr != nil {
		return fmt.Errorf("runtime job group cannot restart after incomplete cleanup: %w", terminalErr)
	}
	if g.cancel != nil {
		select {
		case <-g.activationDone:
			return errors.New("runtime job group activation ended before stop")
		default:
		}
		return nil
	}

	activationCtx, cancel := context.WithCancel(ctx)
	g.activationDone = activationCtx.Done()
	g.cancel = cancel
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

	return nil
}

func (g *Group) failStart(ctx context.Context, started int, startErr error) error {
	g.cancel()
	pendingCleanup, rollbackErr := g.stopJobs(ctx, g.jobs[:started])
	g.activationDone = nil
	g.cancel = nil
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
	if err := g.acquireOperation(ctx); err != nil {
		return err
	}
	defer g.releaseOperation()

	if len(g.pendingCleanup) > 0 {
		var err error
		g.pendingCleanup, err = g.stopJobs(ctx, g.pendingCleanup)
		return err
	}
	if g.cancel == nil {
		return nil
	}

	g.cancel()
	g.activationDone = nil
	g.cancel = nil
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
	g.terminalErrMu.Lock()
	defer g.terminalErrMu.Unlock()
	g.terminalErr = err
}

func (g *Group) stopJobs(parent context.Context, jobs []Job) ([]Job, error) {
	stopCtx, cancel := context.WithTimeout(parent, g.cleanupTimeout)
	defer cancel()

	var pendingCleanup []Job
	var stopErrors []error
	for _, job := range slices.Backward(jobs) {
		result := make(chan error, 1)
		stopGoroutineStarted := make(chan struct{})
		go func() {
			close(stopGoroutineStarted)
			result <- job.Stop(stopCtx)
		}()
		// Schedule every stop attempt before honoring an already-expired group
		// deadline. Once the caller's budget is gone, entering Stop itself is
		// necessarily best effort; waiting for it could make shutdown unbounded.
		<-stopGoroutineStarted

		err := waitForStopResult(stopCtx, result)
		if err != nil {
			pendingCleanup = append(pendingCleanup, job)
			stopErrors = append(stopErrors, fmt.Errorf("stop runtime job %q: %w", job.Name(), err))
		}
	}
	slices.Reverse(pendingCleanup)
	return pendingCleanup, errors.Join(stopErrors...)
}

func waitForStopResult(stopCtx context.Context, result <-chan error) error {
	select {
	case err := <-result:
		return err
	case <-stopCtx.Done():
		select {
		case err := <-result:
			return err
		default:
			return fmt.Errorf("wait for runtime job stop: %w", stopCtx.Err())
		}
	}
}
