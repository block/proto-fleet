package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
	"github.com/stretchr/testify/require"
)

type noopLifecycle struct{}

func (noopLifecycle) Start(context.Context) error { return nil }
func (noopLifecycle) Stop(context.Context) error  { return nil }

type scriptedRuntimeJobGroupStopper struct {
	stops    []func(context.Context) error
	contexts []context.Context
}

func (s *scriptedRuntimeJobGroupStopper) Stop(ctx context.Context) error {
	s.contexts = append(s.contexts, ctx)
	stop := s.stops[0]
	s.stops = s.stops[1:]
	return stop(ctx)
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
	group := &scriptedRuntimeJobGroupStopper{stops: []func(context.Context) error{
		func(context.Context) error { return nil },
	}}

	stopRuntimeJobGroup(group, time.Second)

	require.Len(t, group.contexts, 1)
	_, hasDeadline := group.contexts[0].Deadline()
	require.True(t, hasDeadline)
}

func TestStopRuntimeJobGroupRetriesFailureWithFreshDeadline(t *testing.T) {
	group := &scriptedRuntimeJobGroupStopper{stops: []func(context.Context) error{
		func(context.Context) error { return errors.New("first stop failed") },
		func(context.Context) error { return nil },
	}}

	stopRuntimeJobGroup(group, time.Second)

	require.Len(t, group.contexts, 2)
	firstDeadline, firstHasDeadline := group.contexts[0].Deadline()
	secondDeadline, secondHasDeadline := group.contexts[1].Deadline()
	require.True(t, firstHasDeadline)
	require.True(t, secondHasDeadline)
	require.True(t, secondDeadline.After(firstDeadline))
}

func TestStopRuntimeJobGroupBoundsDrainRetry(t *testing.T) {
	group := &scriptedRuntimeJobGroupStopper{stops: []func(context.Context) error{
		func(context.Context) error { return errors.New("first stop failed") },
		func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}}

	started := time.Now()
	stopRuntimeJobGroup(group, 5*time.Millisecond)

	require.Len(t, group.contexts, 2)
	require.ErrorIs(t, group.contexts[1].Err(), context.DeadlineExceeded)
	require.Less(t, time.Since(started), time.Second)
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
