package runtimejobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

const (
	groupCleanupTimeout   = 10 * time.Second
	progressCheckInterval = 5 * time.Second
)

type jobRuntimeStatus struct {
	state        State
	lastProgress time.Time
	staleAfter   time.Duration
	staleLogged  bool
}

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
	state          State
	jobStatuses    []jobRuntimeStatus
	generation     uint64

	logger          *slog.Logger
	monitorInterval time.Duration
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

	jobStatuses := make([]jobRuntimeStatus, len(jobs))
	for i := range jobStatuses {
		jobStatuses[i].state = StateStopped
	}
	return &Group{
		jobs:            slices.Clone(jobs),
		state:           StateStopped,
		jobStatuses:     jobStatuses,
		logger:          slog.Default(),
		monitorInterval: progressCheckInterval,
	}, nil
}

// Err reports the terminal cleanup failure that prevents reactivation.
func (g *Group) Err() error {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.terminalErr
}

// Status returns a concurrency-safe snapshot in job registration order.
func (g *Group) Status() GroupStatus {
	now := time.Now()
	g.stateMu.Lock()
	defer g.stateMu.Unlock()

	status := GroupStatus{
		State:         g.state,
		TerminalError: g.terminalErr,
		Jobs:          make([]JobStatus, len(g.jobs)),
	}
	for i, job := range g.jobs {
		runtimeStatus := g.jobStatuses[i]
		status.Jobs[i] = JobStatus{
			Name:         job.Name(),
			State:        runtimeStatus.state,
			LastProgress: runtimeStatus.lastProgress,
			StaleAfter:   runtimeStatus.staleAfter,
			Stale:        isStale(runtimeStatus, now),
		}
	}
	return status
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
	generation := g.beginActivation(activationCtx.Done(), cancel)
	started := 0
	for i, job := range g.jobs {
		g.setJobState(i, StateStarting)
		if err := activationCtx.Err(); err != nil {
			g.logStartFailure(job, 0, err)
			g.setJobState(i, StateFailed)
			return g.failStart(ctx, started, fmt.Errorf("start runtime job %q: %w", job.Name(), err))
		}
		jobCtx := context.WithValue(activationCtx, progressContextKey{}, progressContext{
			group:      g,
			jobIndex:   i,
			generation: generation,
		})
		startedAt := time.Now()
		if err := job.Start(jobCtx); err != nil {
			g.logStartFailure(job, time.Since(startedAt), err)
			g.setJobState(i, StateFailed)
			return g.failStart(ctx, started, fmt.Errorf("start runtime job %q: %w", job.Name(), err))
		}
		g.logger.Info("runtime job started",
			"job", job.Name(),
			"duration", time.Since(startedAt),
		)
		g.setJobState(i, StateRunning)
		started++
	}
	if err := activationCtx.Err(); err != nil {
		return g.failStart(ctx, started, fmt.Errorf("start runtime job group: %w", err))
	}

	g.setGroupState(StateRunning)
	go g.monitorProgress(activationCtx, generation)
	return nil
}

func (g *Group) failStart(ctx context.Context, started int, startErr error) error {
	g.endActivation()
	rollbackCtx := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		rollbackCtx, cancel = context.WithDeadline(rollbackCtx, deadline)
		defer cancel()
	}
	rollbackErr := g.stopJobs(rollbackCtx, g.jobs[:started])
	if rollbackErr == nil {
		g.setGroupState(StateStopped)
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

	g.endActivation()
	g.setGroupState(StateStopping)
	err := g.stopJobs(ctx, g.jobs)
	if err != nil {
		g.setTerminalErr(err)
		return err
	}
	g.setGroupState(StateStopped)
	return nil
}

// Abort immediately cancels work that must not survive ownership loss.
// Stop must follow to finish cleanup and make the group restartable.
func (g *Group) Abort() {
	for _, job := range g.jobs {
		if aborter, ok := job.(Aborter); ok {
			aborter.Abort()
		}
	}
}

func (g *Group) setTerminalErr(err error) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.terminalErr = err
	g.state = StateFailed
}

func (g *Group) activation() (<-chan struct{}, context.CancelFunc) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.activationDone, g.cancel
}

func (g *Group) beginActivation(done <-chan struct{}, cancel context.CancelFunc) uint64 {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.generation++
	g.activationDone = done
	g.cancel = cancel
	g.state = StateStarting
	for i := range g.jobStatuses {
		g.jobStatuses[i] = jobRuntimeStatus{state: StateStopped}
	}
	return g.generation
}

func (g *Group) endActivation() {
	g.stateMu.Lock()
	cancel := g.cancel
	g.activationDone = nil
	g.cancel = nil
	g.generation++
	g.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (g *Group) stopJobs(parent context.Context, jobs []Job) error {
	stopCtx, cancel := context.WithTimeout(parent, groupCleanupTimeout)
	defer cancel()

	var stopErrors []error
	for i, job := range slices.Backward(jobs) {
		g.setJobState(i, StateStopping)
		startedAt := time.Now()
		err := g.stopJob(stopCtx, job)
		if err != nil {
			g.logger.Error("runtime job stop failed",
				"job", job.Name(),
				"duration", time.Since(startedAt),
				"error", err,
			)
			g.setJobState(i, StateFailed)
			stopErrors = append(stopErrors, fmt.Errorf("stop runtime job %q: %w", job.Name(), err))
			continue
		}
		g.logger.Info("runtime job stopped",
			"job", job.Name(),
			"duration", time.Since(startedAt),
		)
		g.setJobState(i, StateStopped)
	}
	return errors.Join(stopErrors...)
}

func (g *Group) setGroupState(state State) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.state = state
}

func (g *Group) setJobState(index int, state State) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.jobStatuses[index].state = state
	if state != StateRunning && state != StateStarting {
		g.jobStatuses[index].staleLogged = false
	}
}

func (g *Group) logStartFailure(job Job, duration time.Duration, err error) {
	g.logger.Error("runtime job start failed",
		"job", job.Name(),
		"duration", duration,
		"error", err,
	)
}

func (g *Group) trackProgress(index int, generation uint64, staleAfter time.Duration) func() {
	g.stateMu.Lock()
	if generation != g.generation ||
		(g.jobStatuses[index].state != StateStarting && g.jobStatuses[index].state != StateRunning) {
		g.stateMu.Unlock()
		return func() {}
	}
	g.jobStatuses[index].lastProgress = time.Now()
	g.jobStatuses[index].staleAfter = staleAfter
	g.jobStatuses[index].staleLogged = false
	g.stateMu.Unlock()

	return func() {
		g.stateMu.Lock()
		defer g.stateMu.Unlock()
		if generation != g.generation ||
			(g.jobStatuses[index].state != StateStarting && g.jobStatuses[index].state != StateRunning) {
			return
		}
		g.jobStatuses[index].lastProgress = time.Now()
	}
}

func (g *Group) monitorProgress(ctx context.Context, generation uint64) {
	ticker := time.NewTicker(g.monitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			g.markActivationStopping(generation)
			return
		case now := <-ticker.C:
			g.logProgressTransitions(generation, now)
		}
	}
}

func (g *Group) markActivationStopping(generation uint64) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	if generation != g.generation || g.state != StateRunning {
		return
	}
	g.state = StateStopping
	for i := range g.jobStatuses {
		if g.jobStatuses[i].state == StateRunning {
			g.jobStatuses[i].state = StateStopping
			g.jobStatuses[i].staleLogged = false
		}
	}
}

func (g *Group) logProgressTransitions(generation uint64, now time.Time) {
	type transition struct {
		stale        bool
		name         string
		lastProgress time.Time
		staleAfter   time.Duration
	}
	var transitions []transition

	g.stateMu.Lock()
	if generation != g.generation {
		g.stateMu.Unlock()
		return
	}
	for i, job := range g.jobs {
		status := &g.jobStatuses[i]
		if status.state != StateRunning || status.staleAfter <= 0 {
			continue
		}
		stale := isStale(*status, now)
		switch {
		case stale && !status.staleLogged:
			status.staleLogged = true
			transitions = append(transitions, transition{
				stale:        true,
				name:         job.Name(),
				lastProgress: status.lastProgress,
				staleAfter:   status.staleAfter,
			})
		case !stale && status.staleLogged:
			status.staleLogged = false
			transitions = append(transitions, transition{
				name:         job.Name(),
				lastProgress: status.lastProgress,
				staleAfter:   status.staleAfter,
			})
		}
	}
	g.stateMu.Unlock()

	for _, transition := range transitions {
		if transition.stale {
			g.logger.Warn("runtime job stale",
				"job", transition.name,
				"last_progress", transition.lastProgress,
				"stale_after", transition.staleAfter,
			)
			continue
		}
		g.logger.Info("runtime job recovered",
			"job", transition.name,
			"last_progress", transition.lastProgress,
			"stale_after", transition.staleAfter,
		)
	}
}

func isStale(status jobRuntimeStatus, now time.Time) bool {
	return status.state == StateRunning &&
		status.staleAfter > 0 &&
		!status.lastProgress.IsZero() &&
		!now.Before(status.lastProgress.Add(status.staleAfter))
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
