// Package runtimejobs provides lifecycle management for Fleet background jobs.
package runtimejobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Lifecycle is implemented by independently activatable background work.
//
// Start must return only after startup has succeeded or failed. A failed Start
// must leave the lifecycle stopped and safe to start again. Stop must honor its
// context, fully drain the activation before returning nil, and allow a later
// Start.
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Job is a validated, named Lifecycle managed by a Group.
type Job struct {
	name      string
	lifecycle Lifecycle
}

var _ Lifecycle = Job{}

// NewJob validates and names a lifecycle for runtime orchestration.
func NewJob(name string, lifecycle Lifecycle) (Job, error) {
	job := Job{name: name, lifecycle: lifecycle}
	if err := job.validate(); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Name identifies the job within its group.
func (j Job) Name() string {
	return j.name
}

// Start delegates activation to the job's lifecycle.
func (j Job) Start(ctx context.Context) error {
	return j.lifecycle.Start(ctx)
}

// Stop delegates cleanup to the job's lifecycle.
func (j Job) Stop(ctx context.Context) error {
	return j.lifecycle.Stop(ctx)
}

func (j Job) validate() error {
	if j.name == "" {
		return errors.New("name must not be empty")
	}
	if j.lifecycle == nil {
		return errors.New("lifecycle must not be nil")
	}
	return nil
}

// Group owns at most one activation of an ordered set of jobs at a time.
//
// Lifecycle operations are serialized. A group can restart after a clean stop,
// but incomplete cleanup permanently prevents another activation.
type Group struct {
	mu sync.Mutex

	cleanupTimeout time.Duration
	jobs           []Job
	terminalErr    error
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
		cleanupTimeout: cleanupTimeout,
		jobs:           slices.Clone(jobs),
	}, nil
}

// Start starts every job in registration order.
func (g *Group) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.terminalErr != nil {
		return fmt.Errorf("runtime job group cannot restart after incomplete cleanup: %w", g.terminalErr)
	}
	if g.cancel != nil {
		return nil
	}

	activationCtx, cancel := context.WithCancel(ctx)
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
	rollbackErr := g.stopJobs(ctx, g.jobs[:started])
	g.cancel = nil
	if rollbackErr == nil {
		return startErr
	}

	g.terminalErr = errors.Join(startErr, fmt.Errorf("rollback runtime jobs: %w", rollbackErr))
	return g.terminalErr
}

// Stop broadcasts cancellation, then stops jobs in reverse registration order.
func (g *Group) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.terminalErr != nil {
		return g.terminalErr
	}
	if g.cancel == nil {
		return nil
	}

	g.cancel()
	g.cancel = nil
	if err := g.stopJobs(ctx, g.jobs); err != nil {
		g.terminalErr = err
		return err
	}
	return nil
}

func (g *Group) stopJobs(parent context.Context, jobs []Job) error {
	stopCtx, cancel := context.WithTimeout(parent, g.cleanupTimeout)
	defer cancel()

	var stopErrors []error
	for _, job := range slices.Backward(jobs) {
		result := make(chan error, 1)
		stopGoroutineStarted := make(chan struct{})
		go func() {
			close(stopGoroutineStarted)
			result <- job.Stop(stopCtx)
		}()
		<-stopGoroutineStarted

		var err error
		select {
		case err = <-result:
		case <-stopCtx.Done():
			err = stopCtx.Err()
		}
		if err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("stop runtime job %q: %w", job.Name(), err))
		}
	}
	return errors.Join(stopErrors...)
}
