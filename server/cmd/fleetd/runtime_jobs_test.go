package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
	"github.com/stretchr/testify/require"
)

type noopLifecycle struct{}

func (noopLifecycle) Start(context.Context) error { return nil }
func (noopLifecycle) Stop(context.Context) error  { return nil }

type funcLifecycle struct {
	start func(context.Context) error
	stop  func(context.Context) error
	abort func()
}

func (l funcLifecycle) Start(ctx context.Context) error {
	if l.start == nil {
		return nil
	}
	return l.start(ctx)
}

func (l funcLifecycle) Stop(ctx context.Context) error {
	if l.stop == nil {
		return nil
	}
	return l.stop(ctx)
}

func (l funcLifecycle) Abort() {
	if l.abort != nil {
		l.abort()
	}
}

type scriptedRuntimeJobGroupStopper struct {
	stop     func(context.Context) error
	contexts []context.Context
}

type scriptedFleetRuntime struct {
	run func(context.Context) error
}

func (r scriptedFleetRuntime) Run(ctx context.Context) error {
	return r.run(ctx)
}

func (s *scriptedRuntimeJobGroupStopper) Stop(ctx context.Context) error {
	s.contexts = append(s.contexts, ctx)
	return s.stop(ctx)
}

func TestNewRuntimeJobs(t *testing.T) {
	all := runtimeJobLifecycles{
		identityStateCleanup:      noopLifecycle{},
		commandArtifactCleanup:    noopLifecycle{},
		diagnosticsErrorCloser:    noopLifecycle{},
		telemetry:                 noopLifecycle{},
		ipScanner:                 noopLifecycle{},
		commandExecution:          noopLifecycle{},
		scheduleProcessor:         noopLifecycle{},
		curtailmentReconciler:     noopLifecycle{},
		curtailmentMQTTSubscriber: noopLifecycle{},
		curtailmentAlertMetrics:   noopLifecycle{},
		chunkedUploadCleanup:      noopLifecycle{},
		systemMonitoring:          noopLifecycle{},
	}

	jobs, err := newRuntimeJobs(all)
	require.NoError(t, err)
	require.Equal(t, []string{
		"identity-state-cleanup",
		"command-artifact-cleanup",
		"diagnostics-error-closer",
		"telemetry",
		"ip-scanner",
		"command-execution",
		"schedule-processor",
		"curtailment-reconciler",
		"curtailment-mqtt-subscriber",
		"curtailment-alert-metrics",
		"chunked-upload-cleanup",
		"system-monitoring",
	}, jobNames(jobs))

	all.curtailmentAlertMetrics = nil
	all.systemMonitoring = nil
	jobs, err = newRuntimeJobs(all)
	require.NoError(t, err)
	require.Equal(t, []string{
		"identity-state-cleanup",
		"command-artifact-cleanup",
		"diagnostics-error-closer",
		"telemetry",
		"ip-scanner",
		"command-execution",
		"schedule-processor",
		"curtailment-reconciler",
		"curtailment-mqtt-subscriber",
		"chunked-upload-cleanup",
	}, jobNames(jobs))
}

func TestNewRuntimeJobsRejectsMissingRequiredLifecycle(t *testing.T) {
	_, err := newRuntimeJobs(runtimeJobLifecycles{})
	require.ErrorContains(t, err, `create runtime job "identity-state-cleanup"`)
}

func TestRuntimeJobGroupKeepsCommandExecutionAliveWhileProducersDrain(t *testing.T) {
	var commandDone <-chan struct{}
	commandStopped := make(chan struct{})
	commandExecution := funcLifecycle{
		start: func(ctx context.Context) error {
			commandDone = ctx.Done()
			return nil
		},
		stop: func(context.Context) error {
			close(commandStopped)
			return nil
		},
	}
	producer := funcLifecycle{stop: func(context.Context) error {
		select {
		case <-commandDone:
			return errors.New("command execution canceled before producer drained")
		default:
		}
		select {
		case <-commandStopped:
			return errors.New("command execution stopped before producer drained")
		default:
			return nil
		}
	}}

	jobs, err := newRuntimeJobs(runtimeJobLifecycles{
		identityStateCleanup:      noopLifecycle{},
		commandArtifactCleanup:    noopLifecycle{},
		diagnosticsErrorCloser:    noopLifecycle{},
		telemetry:                 noopLifecycle{},
		ipScanner:                 noopLifecycle{},
		commandExecution:          commandExecution,
		scheduleProcessor:         producer,
		curtailmentReconciler:     noopLifecycle{},
		curtailmentMQTTSubscriber: noopLifecycle{},
		chunkedUploadCleanup:      noopLifecycle{},
	})
	require.NoError(t, err)
	group, err := runtimejobs.NewGroup(jobs)
	require.NoError(t, err)
	require.NoError(t, group.Start(t.Context()))
	require.NoError(t, group.Stop(t.Context()))
	select {
	case <-commandStopped:
	default:
		t.Fatal("command execution was not stopped")
	}
}

func TestRuntimeJobGroupAbortsCommandExecution(t *testing.T) {
	commandAborted := false
	jobs, err := newRuntimeJobs(runtimeJobLifecycles{
		identityStateCleanup:      noopLifecycle{},
		commandArtifactCleanup:    noopLifecycle{},
		diagnosticsErrorCloser:    noopLifecycle{},
		telemetry:                 noopLifecycle{},
		ipScanner:                 noopLifecycle{},
		commandExecution:          funcLifecycle{abort: func() { commandAborted = true }},
		scheduleProcessor:         noopLifecycle{},
		curtailmentReconciler:     noopLifecycle{},
		curtailmentMQTTSubscriber: noopLifecycle{},
		chunkedUploadCleanup:      noopLifecycle{},
	})
	require.NoError(t, err)
	group, err := runtimejobs.NewGroup(jobs)
	require.NoError(t, err)

	group.Abort()

	require.True(t, commandAborted)
}

func TestBackgroundLoopCanRestartAfterDraining(t *testing.T) {
	started := make(chan struct{}, 2)
	loop := newBackgroundLoop(func(ctx context.Context) {
		started <- struct{}{}
		<-ctx.Done()
	})

	for range 2 {
		require.NoError(t, loop.Start(context.Background()))
		requireReceive(t, started)
		require.NoError(t, loop.Stop(context.Background()))
	}
}

func TestBackgroundLoopActivationCancellationRequiresStopBeforeRestart(t *testing.T) {
	started := make(chan struct{}, 2)
	loop := newBackgroundLoop(func(ctx context.Context) {
		started <- struct{}{}
		<-ctx.Done()
	})

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	require.NoError(t, loop.Start(firstCtx))
	requireReceive(t, started)
	cancelFirst()
	require.Eventually(t, func() bool {
		return loop.Start(context.Background()) != nil
	}, time.Second, time.Millisecond)
	require.NoError(t, loop.Stop(context.Background()))
	require.NoError(t, loop.Start(context.Background()))
	requireReceive(t, started)
	require.NoError(t, loop.Stop(context.Background()))
}

func TestBackgroundLoopStopCanBeRetriedAfterTimeout(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	loop := newBackgroundLoop(func(context.Context) {
		started <- struct{}{}
		<-release
	})

	require.NoError(t, loop.Start(context.Background()))
	requireReceive(t, started)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	require.ErrorIs(t, loop.Stop(stopCtx), context.DeadlineExceeded)

	close(release)
	require.NoError(t, loop.Stop(context.Background()))
}

func TestStopRuntimeJobGroupDoesNotRetrySuccessfulStop(t *testing.T) {
	group := &scriptedRuntimeJobGroupStopper{
		stop: func(context.Context) error { return nil },
	}

	stopRuntimeJobGroup(group, noopLifecycle{}, time.Second)

	require.Len(t, group.contexts, 1)
	_, hasDeadline := group.contexts[0].Deadline()
	require.True(t, hasDeadline)
}

func TestStopRuntimeJobGroupStopsCommandAfterGroupFailure(t *testing.T) {
	group := &scriptedRuntimeJobGroupStopper{
		stop: func(context.Context) error { return errors.New("stop failed") },
	}
	commandStopped := false

	stopRuntimeJobGroup(group, funcLifecycle{stop: func(context.Context) error {
		commandStopped = true
		return nil
	}}, time.Second)

	require.Len(t, group.contexts, 1)
	require.True(t, commandStopped)
}

func TestStopRuntimeJobGroupBoundsGroupAndCommandStops(t *testing.T) {
	group := &scriptedRuntimeJobGroupStopper{
		stop: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	commandStopped := make(chan bool, 1)
	commandExecution := funcLifecycle{stop: func(ctx context.Context) error {
		_, hasDeadline := ctx.Deadline()
		commandStopped <- hasDeadline && ctx.Err() == nil
		return nil
	}}

	started := time.Now()
	stopRuntimeJobGroup(group, commandExecution, 5*time.Millisecond)

	require.Len(t, group.contexts, 1)
	require.ErrorIs(t, group.contexts[0].Err(), context.DeadlineExceeded)
	require.True(t, <-commandStopped)
	require.Less(t, time.Since(started), time.Second)
}

func TestServeFleetRuntimeServesHealthBeforeRuntimeActivation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}),
		ReadHeaderTimeout: time.Second,
	}
	runtimeStarted := make(chan struct{})
	runtime := scriptedFleetRuntime{run: func(ctx context.Context) error {
		close(runtimeStarted)
		<-ctx.Done()
		return nil
	}}
	result := make(chan error, 1)
	go func() {
		result <- serveFleetRuntime(server, listener, runtime, time.Second)
	}()
	requireReceive(t, runtimeStarted)

	response, err := http.Get("http://" + listener.Addr().String()) //nolint:noctx // Test-only local request.
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))

	require.NoError(t, server.Close())
	require.NoError(t, <-result)
}

func TestServeFleetRuntimeStopsHTTPWhenRuntimeFails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{
		Handler:           http.NotFoundHandler(),
		ReadHeaderTimeout: time.Second,
	}
	runtimeErr := errors.New("activation failed")

	err = serveFleetRuntime(
		server,
		listener,
		scriptedFleetRuntime{run: func(context.Context) error { return runtimeErr }},
		time.Second,
	)

	require.ErrorIs(t, err, runtimeErr)
}

func jobNames(jobs []runtimejobs.Job) []string {
	names := make([]string, 0, len(jobs))
	for _, job := range jobs {
		names = append(names, job.Name())
	}
	return names
}

func requireReceive(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for background loop")
	}
}
