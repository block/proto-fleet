package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

// backgroundLoop adapts a context-driven loop to the runtime job lifecycle.
type backgroundLoop struct {
	mu sync.Mutex

	run    func(context.Context)
	cancel context.CancelFunc
	done   chan struct{}
}

var _ runtimejobs.Lifecycle = (*backgroundLoop)(nil)

// stopOrderedLifecycle keeps a shared dependency active until the group reaches
// it in reverse stop order. This lets jobs registered after it finish draining
// without losing the dependency to the group's broadcast cancellation.
type stopOrderedLifecycle struct {
	lifecycle runtimejobs.Lifecycle
}

var _ runtimejobs.Lifecycle = stopOrderedLifecycle{}

func (l stopOrderedLifecycle) Start(ctx context.Context) error {
	return l.lifecycle.Start(context.WithoutCancel(ctx))
}

func (l stopOrderedLifecycle) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop ordered lifecycle: %w", err)
	}
	return l.lifecycle.Stop(ctx)
}

func newBackgroundLoop(run func(context.Context)) *backgroundLoop {
	return &backgroundLoop{run: run}
}

func (l *backgroundLoop) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start background loop: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.cancel != nil {
		select {
		case <-l.done:
			return fmt.Errorf("background loop activation ended before stop")
		default:
			return nil
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	l.cancel = cancel
	l.done = done
	go func() {
		defer close(done)
		l.run(runCtx)
	}()
	return nil
}

func (l *backgroundLoop) Stop(ctx context.Context) error {
	l.mu.Lock()
	if l.cancel == nil {
		l.mu.Unlock()
		return nil
	}
	cancel := l.cancel
	done := l.done
	l.mu.Unlock()

	cancel()
	select {
	case <-done:
		l.mu.Lock()
		if l.done == done {
			l.cancel = nil
			l.done = nil
		}
		l.mu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop background loop: %w", ctx.Err())
	}
}

type runtimeJobGroupStopper interface {
	Stop(ctx context.Context) error
}

// stopRuntimeJobGroup gives the group one graceful-shutdown budget, then one
// fresh bounded retry. Command execution receives a final independent budget
// after producer retries because its activation is detached from group
// cancellation to preserve shutdown ordering.
func stopRuntimeJobGroup(group runtimeJobGroupStopper, commandExecution runtimejobs.Lifecycle, timeout time.Duration) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	err := group.Stop(shutdownCtx)
	cancel()
	if err != nil {
		slog.Error("failed to stop runtime jobs", "error", err)
		drainCtx, drainCancel := context.WithTimeout(context.Background(), timeout)
		if err := group.Stop(drainCtx); err != nil {
			slog.Error("failed to drain runtime jobs", "error", err)
		}
		drainCancel()
	}

	commandCtx, commandCancel := context.WithTimeout(context.Background(), timeout)
	err = commandExecution.Stop(commandCtx)
	commandCancel()
	if err != nil {
		slog.Error("failed to stop command execution after runtime jobs", "error", err)
	}
}

type runtimeJobLifecycles struct {
	identityStateCleanup      runtimejobs.Lifecycle
	commandArtifactCleanup    runtimejobs.Lifecycle
	diagnosticsErrorCloser    runtimejobs.Lifecycle
	telemetry                 runtimejobs.Lifecycle
	ipScanner                 runtimejobs.Lifecycle
	commandExecution          runtimejobs.Lifecycle
	scheduleProcessor         runtimejobs.Lifecycle
	curtailmentReconciler     runtimejobs.Lifecycle
	curtailmentMQTTSubscriber runtimejobs.Lifecycle
	curtailmentAlertMetrics   runtimejobs.Lifecycle
	chunkedUploadCleanup      runtimejobs.Lifecycle
	systemMonitoring          runtimejobs.Lifecycle
}

func newRuntimeJobs(lifecycles runtimeJobLifecycles) ([]runtimejobs.Job, error) {
	jobs := make([]runtimejobs.Job, 0, 12)
	add := func(name string, lifecycle runtimejobs.Lifecycle) error {
		job, err := runtimejobs.NewJob(name, lifecycle)
		if err != nil {
			return fmt.Errorf("create runtime job %q: %w", name, err)
		}
		jobs = append(jobs, job)
		return nil
	}
	commandExecution := lifecycles.commandExecution
	if commandExecution != nil {
		commandExecution = stopOrderedLifecycle{lifecycle: commandExecution}
	}

	required := []struct {
		name      string
		lifecycle runtimejobs.Lifecycle
	}{
		{name: "identity-state-cleanup", lifecycle: lifecycles.identityStateCleanup},
		{name: "command-artifact-cleanup", lifecycle: lifecycles.commandArtifactCleanup},
		{name: "diagnostics-error-closer", lifecycle: lifecycles.diagnosticsErrorCloser},
		{name: "telemetry", lifecycle: lifecycles.telemetry},
		{name: "ip-scanner", lifecycle: lifecycles.ipScanner},
		{name: "command-execution", lifecycle: commandExecution},
		{name: "schedule-processor", lifecycle: lifecycles.scheduleProcessor},
		{name: "curtailment-reconciler", lifecycle: lifecycles.curtailmentReconciler},
		{name: "curtailment-mqtt-subscriber", lifecycle: lifecycles.curtailmentMQTTSubscriber},
	}
	for _, job := range required {
		if err := add(job.name, job.lifecycle); err != nil {
			return nil, err
		}
	}
	if lifecycles.curtailmentAlertMetrics != nil {
		if err := add("curtailment-alert-metrics", lifecycles.curtailmentAlertMetrics); err != nil {
			return nil, err
		}
	}
	if err := add("chunked-upload-cleanup", lifecycles.chunkedUploadCleanup); err != nil {
		return nil, err
	}
	if lifecycles.systemMonitoring != nil {
		if err := add("system-monitoring", lifecycles.systemMonitoring); err != nil {
			return nil, err
		}
	}

	return jobs, nil
}
