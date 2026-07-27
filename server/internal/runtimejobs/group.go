package runtimejobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

const groupCleanupTimeout = 10 * time.Second

// Group owns at most one activation of an ordered set of jobs at a time.
//
// Lifecycle operations are serialized. A group can restart after a clean stop,
// but incomplete cleanup permanently prevents another activation.
type Group struct {
	stateMu     sync.Mutex
	operationMu sync.Mutex

	jobs           []Job
	terminalErr    error
	activationDone <-chan struct{}
	cancel         context.CancelFunc
}

// NewGroup validates group configuration and creates a stopped group.
// The group shares one ten-second cleanup budget across every job during a stop
// or startup rollback.
func NewGroup(jobs []Job) (*Group, error) {
	seen := make(map[string]struct{}, len(jobs))
	for i, job := range jobs {
		if job == nil {
			return nil, fmt.Errorf("runtime job %d must not be nil", i)
		}
		if _, ok := seen[job.Name()]; ok {
			return nil, fmt.Errorf("runtime job name %q appears more than once", job.Name())
		}
		seen[job.Name()] = struct{}{}
	}

	return &Group{jobs: slices.Clone(jobs)}, nil
}

// Err reports the terminal cleanup failure that prevents reactivation.
func (g *Group) Err() error {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.terminalErr
}

// Start starts every job in registration order.
func (g *Group) Start(ctx context.Context) error {
	g.operationMu.Lock()
	defer g.operationMu.Unlock()

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
	g.setActivation(activationCtx.Done(), cancel)
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
	rollbackCtx := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		rollbackCtx, cancel = context.WithDeadline(rollbackCtx, deadline)
		defer cancel()
	}
	rollbackErr := g.stopJobs(rollbackCtx, g.jobs[:started])
	g.clearActivation()
	if rollbackErr == nil {
		return startErr
	}

	terminalErr := errors.Join(startErr, fmt.Errorf("rollback runtime jobs: %w", rollbackErr))
	g.setTerminalErr(terminalErr)
	return terminalErr
}

// Stop broadcasts cancellation, then stops jobs in reverse registration order.
// A cleanup failure is terminal and is not retried.
func (g *Group) Stop(ctx context.Context) error {
	g.operationMu.Lock()
	defer g.operationMu.Unlock()

	if terminalErr := g.Err(); terminalErr != nil {
		return terminalErr
	}
	_, cancel := g.activation()
	if cancel == nil {
		return nil
	}

	g.cancelActivation()
	g.clearActivation()
	err := g.stopJobs(ctx, g.jobs)
	if err != nil {
		g.setTerminalErr(err)
		return err
	}
	return nil
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

func (g *Group) setActivation(done <-chan struct{}, cancel context.CancelFunc) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.activationDone = done
	g.cancel = cancel
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

func (g *Group) stopJobs(parent context.Context, jobs []Job) error {
	stopCtx, cancel := context.WithTimeout(parent, groupCleanupTimeout)
	defer cancel()

	var stopErrors []error
	for _, job := range slices.Backward(jobs) {
		err := g.stopJob(stopCtx, job)
		if err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("stop runtime job %q: %w", job.Name(), err))
		}
	}
	return errors.Join(stopErrors...)
}

func (g *Group) stopJob(stopCtx context.Context, job Job) error {
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

	return waitForStopResult(stopCtx, result)
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
