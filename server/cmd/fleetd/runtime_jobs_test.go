package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type noopLifecycle struct{}

func (noopLifecycle) Start(context.Context) error { return nil }
func (noopLifecycle) Stop(context.Context) error  { return nil }

type funcLifecycle struct {
	start func(context.Context) error
	stop  func(context.Context) error
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

type scriptedRuntimeJobGroupStopper struct {
	stop     func(context.Context) error
	contexts []context.Context
}

type scriptedFleetRuntime struct {
	run        func(context.Context) error
	beginDrain func(time.Time)
}

func (r scriptedFleetRuntime) BeginPlannedDrain(deadline time.Time) {
	if r.beginDrain != nil {
		r.beginDrain(deadline)
	}
}

func (r scriptedFleetRuntime) Run(ctx context.Context) error {
	return r.run(ctx)
}

func (s *scriptedRuntimeJobGroupStopper) Stop(ctx context.Context) error {
	s.contexts = append(s.contexts, ctx)
	return s.stop(ctx)
}

func TestNewRuntimeJobs(t *testing.T) {
	miningStarted := make(chan struct{}, 1)
	miningStopped := make(chan struct{}, 1)
	miningLoop := newBackgroundLoop(func(ctx context.Context) {
		miningStarted <- struct{}{}
		<-ctx.Done()
		miningStopped <- struct{}{}
	})
	all := runtimeJobLifecycles{
		plugins:                   noopLifecycle{},
		pendingUnpairs:            noopLifecycle{},
		identityStateCleanup:      noopLifecycle{},
		commandArtifactCleanup:    noopLifecycle{},
		diagnosticsErrorCloser:    noopLifecycle{},
		telemetry:                 noopLifecycle{},
		ipScanner:                 noopLifecycle{},
		commandExecution:          noopLifecycle{},
		miningGroupReconciliation: miningLoop,
		poolConfigurationUpdates:  noopLifecycle{},
		scheduleProcessor:         noopLifecycle{},
		rolloutEnforcement:        noopLifecycle{},
		curtailmentReconciler:     noopLifecycle{},
		curtailmentMQTTSubscriber: noopLifecycle{},
		curtailmentAutomation:     noopLifecycle{},
		curtailmentRigConfig:      noopLifecycle{},
		curtailmentAlertMetrics:   noopLifecycle{},
		chunkedUploadCleanup:      noopLifecycle{},
		systemMonitoring:          noopLifecycle{},
		haReadiness:               noopLifecycle{},
		releaseChecker:            noopLifecycle{},
	}

	jobs, err := newRuntimeJobs(all)
	require.NoError(t, err)
	require.Equal(t, []string{
		"plugins",
		"identity-state-cleanup",
		"command-artifact-cleanup",
		"diagnostics-error-closer",
		"telemetry",
		"ip-scanner",
		"command-execution",
		"mining-group-reconciliation",
		"pool-configuration-updates",
		"schedule-processor",
		"rollout-enforcement",
		"curtailment-reconciler",
		"curtailment-automation",
		"curtailment-mqtt-subscriber",
		"curtailment-rig-config",
		"curtailment-alert-metrics",
		"chunked-upload-cleanup",
		"system-monitoring",
		"ha-readiness",
		"release-checker",
		"pending-unpairs",
	}, jobNames(jobs))
	select {
	case <-miningStarted:
		t.Fatal("mining group reconciliation started before runtime activation")
	default:
	}
	group, err := runtimejobs.NewGroup(jobs)
	require.NoError(t, err)
	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	require.NoError(t, group.Start(activeCtx))
	requireReceive(t, miningStarted)
	cancelActive()
	requireReceive(t, miningStopped)
	require.NoError(t, group.Stop(t.Context()))

	all.curtailmentAlertMetrics = nil
	all.systemMonitoring = nil
	all.haReadiness = nil
	all.releaseChecker = nil
	jobs, err = newRuntimeJobs(all)
	require.NoError(t, err)
	require.Equal(t, []string{
		"plugins",
		"identity-state-cleanup",
		"command-artifact-cleanup",
		"diagnostics-error-closer",
		"telemetry",
		"ip-scanner",
		"command-execution",
		"mining-group-reconciliation",
		"pool-configuration-updates",
		"schedule-processor",
		"rollout-enforcement",
		"curtailment-reconciler",
		"curtailment-automation",
		"curtailment-mqtt-subscriber",
		"curtailment-rig-config",
		"chunked-upload-cleanup",
		"pending-unpairs",
	}, jobNames(jobs))
}

func TestNewRuntimeJobsRejectsMissingRequiredLifecycle(t *testing.T) {
	_, err := newRuntimeJobs(runtimeJobLifecycles{})
	require.ErrorContains(t, err, `create runtime job "plugins"`)
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
		plugins:                   noopLifecycle{},
		pendingUnpairs:            noopLifecycle{},
		identityStateCleanup:      noopLifecycle{},
		commandArtifactCleanup:    noopLifecycle{},
		diagnosticsErrorCloser:    noopLifecycle{},
		telemetry:                 noopLifecycle{},
		ipScanner:                 noopLifecycle{},
		commandExecution:          commandExecution,
		miningGroupReconciliation: producer,
		poolConfigurationUpdates:  noopLifecycle{},
		scheduleProcessor:         producer,
		rolloutEnforcement:        noopLifecycle{},
		curtailmentReconciler:     noopLifecycle{},
		curtailmentMQTTSubscriber: noopLifecycle{},
		curtailmentAutomation:     noopLifecycle{},
		curtailmentRigConfig:      noopLifecycle{},
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

func TestStopRuntimeJobGroupAfterRunOnlySkipsHAAbort(t *testing.T) {
	t.Run("HA abort", func(t *testing.T) {
		// Arrange
		group := &scriptedRuntimeJobGroupStopper{
			stop: func(context.Context) error { return nil },
		}

		// Act
		stopRuntimeJobGroupAfterRun(
			errors.Join(errors.New("runtime failed"), ha.ErrRuntimeAborted),
			group,
			noopLifecycle{},
			time.Second,
		)

		// Assert
		require.Empty(t, group.contexts)
	})

	t.Run("standalone failure", func(t *testing.T) {
		// Arrange
		group := &scriptedRuntimeJobGroupStopper{
			stop: func(context.Context) error { return nil },
		}

		// Act
		stopRuntimeJobGroupAfterRun(errors.New("runtime failed"), group, noopLifecycle{}, time.Second)

		// Assert
		require.Len(t, group.contexts, 1)
	})
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
		result <- serveFleetRuntime(server, listener, runtime, time.Second, nil)
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
		nil,
	)

	require.ErrorIs(t, err, runtimeErr)
}

func TestServeFleetRuntimeDrainsCancelledRequestsBeforeStoppingRuntime(t *testing.T) {
	for _, protocol := range []string{"http1", "h2c"} {
		t.Run(protocol, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			started := make(chan struct{})
			cancelled := make(chan struct{})
			allowCleanup := make(chan struct{})
			cleaned := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(allowCleanup) }) }
			t.Cleanup(release)
			drain := &httpRequestDrain{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
				close(cancelled)
				// Model detached completion bookkeeping after transport cancellation.
				<-allowCleanup
				close(cleaned)
				w.WriteHeader(http.StatusNoContent)
			})}
			var handler http.Handler = drain
			var transport http.RoundTripper = &http.Transport{}
			if protocol == "h2c" {
				handler = h2c.NewHandler(drain, &http2.Server{})
				transport = &http2.Transport{
					AllowHTTP: true,
					DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, network, addr)
					},
				}
			}
			client := &http.Client{Transport: transport, Timeout: time.Second}
			t.Cleanup(client.CloseIdleConnections)
			server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
			t.Cleanup(func() { _ = server.Close() })
			runtimeStarted := make(chan struct{})
			runtimeStopped := make(chan struct{})
			cleanupBeforeStop := make(chan bool, 1)
			runtime := scriptedFleetRuntime{run: func(ctx context.Context) error {
				close(runtimeStarted)
				<-ctx.Done()
				select {
				case <-cleaned:
					cleanupBeforeStop <- true
				default:
					cleanupBeforeStop <- false
				}
				if !errors.Is(context.Cause(ctx), ha.ErrPlannedStop) {
					return fmt.Errorf("clean drain stop cause: %w", context.Cause(ctx))
				}
				close(runtimeStopped)
				return nil
			}}
			result := make(chan error, 1)
			go func() { result <- serveFleetRuntimeWithContext(ctx, server, listener, runtime, time.Second, drain) }()
			requireReceive(t, runtimeStarted)
			requestDone := make(chan struct{})
			go func() {
				defer close(requestDone)
				response, err := client.Get("http://" + listener.Addr().String()) //nolint:noctx // Test client has a deadline.
				if err == nil {
					_ = response.Body.Close()
				}
			}()
			requireReceive(t, started)
			cancel()
			requireReceive(t, cancelled)
			select {
			case <-runtimeStopped:
				t.Fatal("runtime stopped before request bookkeeping finished")
			default:
			}
			// Admission is closed even on a still-open h2c connection.
			response := httptest.NewRecorder()
			drain.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			release()
			require.NoError(t, <-result)
			require.True(t, <-cleanupBeforeStop)
			requireReceive(t, requestDone)
		})
	}
}

func TestServeFleetRuntimeWaitsForHandlerAfterShutdownTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	allowCleanup := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(allowCleanup) }) }
	t.Cleanup(release)
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			close(cancelled)
			<-allowCleanup
		}),
	}
	runtimeStopped := make(chan struct{})
	runtime := scriptedFleetRuntime{run: func(ctx context.Context) error {
		<-ctx.Done()
		if errors.Is(context.Cause(ctx), ha.ErrPlannedStop) {
			return errors.New("HTTP timeout marked as planned stop")
		}
		close(runtimeStopped)
		return nil
	}}
	result := make(chan error, 1)
	go func() {
		result <- serveFleetRuntimeWithContext(ctx, server, listener, runtime, 5*time.Millisecond, nil)
	}()
	client := &http.Client{Timeout: time.Second}
	t.Cleanup(client.CloseIdleConnections)
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := client.Get("http://" + listener.Addr().String()) //nolint:noctx // Test client has a deadline.
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	requireReceive(t, started)
	cancel()
	requireReceive(t, cancelled)
	// Socket closure establishes that the graceful shutdown budget expired.
	requireReceive(t, requestDone)
	select {
	case <-runtimeStopped:
		t.Fatal("shutdown timeout stopped runtime before handler completion")
	default:
	}
	release()
	require.ErrorIs(t, <-result, context.DeadlineExceeded)
	requireReceive(t, runtimeStopped)
}

func TestServeFleetRuntimeLateH2CDrainIsNotPlannedStop(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	allowCleanup := make(chan struct{})
	release := sync.OnceFunc(func() { close(allowCleanup) })
	t.Cleanup(release)
	drain := &httpRequestDrain{handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		<-allowCleanup
	})}
	server := &http.Server{Handler: h2c.NewHandler(drain, &http2.Server{}), ReadHeaderTimeout: time.Second}
	t.Cleanup(func() { _ = server.Close() })
	deadline := make(chan time.Time, 1)
	stopCause := make(chan error, 1)
	// No health ticker runs here: the HTTP drain must detect its own deadline.
	runtime := scriptedFleetRuntime{
		beginDrain: func(at time.Time) { deadline <- at },
		run: func(ctx context.Context) error {
			<-ctx.Done()
			stopCause <- context.Cause(ctx)
			return nil
		},
	}
	result := make(chan error, 1)
	go func() {
		result <- serveFleetRuntimeWithContext(ctx, server, listener, runtime, 20*time.Millisecond, drain)
	}()
	client := &http.Client{Timeout: time.Second, Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
	t.Cleanup(client.CloseIdleConnections)
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := client.Get("http://" + listener.Addr().String()) //nolint:noctx // Test client has a deadline.
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	requireReceive(t, started)
	cancel()
	requireReceive(t, cancelled)
	// h2c sockets close before the handler finishes, even though Shutdown succeeds.
	requireReceive(t, requestDone)
	drainDeadline := <-deadline
	timer := time.NewTimer(time.Until(drainDeadline))
	defer timer.Stop()
	<-timer.C
	select {
	case <-stopCause:
		t.Fatal("runtime stopped before the handler finished")
	default:
	}
	release()
	// A nil result proves this exercised Shutdown's success path for hijacked h2c.
	require.NoError(t, <-result)
	require.ErrorIs(t, <-stopCause, context.Canceled)
}

func TestServeFleetRuntimeSignalShutdown(t *testing.T) {
	if os.Getenv("FLEETD_SHUTDOWN_SIGNAL_TEST") == "1" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		server := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
		runtime := scriptedFleetRuntime{run: func(ctx context.Context) error {
			_, _ = fmt.Fprintln(os.Stdout, "ready for shutdown signal")
			<-ctx.Done()
			if !errors.Is(context.Cause(ctx), ha.ErrPlannedStop) {
				return fmt.Errorf("signal stop cause: %w", context.Cause(ctx))
			}
			return nil
		}}
		require.NoError(t, serveFleetRuntime(server, listener, runtime, time.Second, nil))
		return
	}

	for _, shutdownSignal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(shutdownSignal.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			process := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServeFleetRuntimeSignalShutdown$") //nolint:gosec // Executes this test binary with fixed arguments.
			process.Env = append(os.Environ(), "FLEETD_SHUTDOWN_SIGNAL_TEST=1")
			stdout, err := process.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, process.Start())
			t.Cleanup(func() { _ = process.Process.Kill() })
			ready, err := bufio.NewReader(stdout).ReadString('\n')
			require.NoError(t, err)
			require.Equal(t, "ready for shutdown signal\n", ready)
			require.NoError(t, process.Process.Signal(shutdownSignal))
			require.NoError(t, process.Wait(), "signal must trigger a clean runtime shutdown")
		})
	}
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

// shutdownLeaseStore checks the boundary between the HTTP drain, runtime jobs,
// and ownership coordinator; SQL ownership matching is covered by HA DB tests.
type shutdownLeaseStore struct {
	requestsDrained <-chan struct{}
	jobsStopped     <-chan struct{}
	released        chan ha.Ownership
}

func (s *shutdownLeaseStore) Acquire(_ context.Context, observed ha.WriterObservation, holder uuid.UUID, duration time.Duration) (ha.Ownership, error) {
	now := time.Now()
	return ha.Ownership{DCSClusterID: observed.DCSClusterID, Token: ha.Token{WriterGeneration: observed.WriterGeneration, LeaseEpoch: 7}, HolderID: holder, DatabaseTime: now, ExpiresAt: now.Add(duration)}, nil
}

func (s *shutdownLeaseStore) Renew(_ context.Context, _ ha.WriterObservation, active ha.Ownership, duration time.Duration) (ha.Ownership, error) {
	active.DatabaseTime = time.Now()
	active.ExpiresAt = active.DatabaseTime.Add(duration)
	return active, nil
}

func (s *shutdownLeaseStore) Release(_ context.Context, active ha.Ownership) error {
	for _, drained := range []<-chan struct{}{s.requestsDrained, s.jobsStopped} {
		select {
		case <-drained:
		default:
			return errors.New("lease released before request/job completion")
		}
	}
	s.released <- active
	return nil
}

type shutdownWriterObserver struct{}

func (shutdownWriterObserver) ObserveAndRun(ctx context.Context, action func(context.Context, ha.WriterObservation) error) (ha.WriterObservation, error) {
	observed := ha.WriterObservation{DCSClusterID: "cluster-a", WriterGeneration: 41, DCSProofDeadline: time.Now().Add(time.Minute)}
	return observed, action(ctx, observed)
}

func TestServeFleetRuntimeReleasesHALeaseOnlyAfterCleanDrain(t *testing.T) {
	for _, clean := range []bool{true, false} {
		t.Run(fmt.Sprintf("clean=%t", clean), func(t *testing.T) {
			requestStarted := make(chan struct{})
			requestCancelled := make(chan struct{})
			allowHandlerExit := make(chan struct{})
			exitHandler := sync.OnceFunc(func() { close(allowHandlerExit) })
			t.Cleanup(exitHandler)
			handlerDrained := make(chan struct{})
			jobsStopped := make(chan struct{})
			store := &shutdownLeaseStore{requestsDrained: handlerDrained, jobsStopped: jobsStopped, released: make(chan ha.Ownership, 1)}
			coordinator, err := ha.NewCoordinator(shutdownWriterObserver{}, store, ha.CoordinatorConfig{LeaseDuration: time.Minute, RenewInterval: 30 * time.Second, RetryInterval: time.Second})
			require.NoError(t, err)
			job, err := runtimejobs.NewJob("shutdown-test", funcLifecycle{stop: func(context.Context) error { close(jobsStopped); return nil }})
			require.NoError(t, err)
			group, err := runtimejobs.NewGroup([]runtimejobs.Job{job})
			require.NoError(t, err)
			runtime, err := ha.NewRuntime(coordinator, group, func() bool { return true })
			require.NoError(t, err)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				close(requestStarted)
				<-r.Context().Done()
				close(requestCancelled)
				<-allowHandlerExit
				close(handlerDrained)
			})}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			shutdownBudget := time.Second
			if !clean {
				shutdownBudget = 10 * time.Millisecond
			}
			go func() {
				result <- serveFleetRuntimeWithContext(ctx, server, listener, runtime, shutdownBudget, nil)
			}()
			require.Eventually(t, runtime.Active, time.Second, time.Millisecond)
			client := &http.Client{Timeout: time.Second}
			t.Cleanup(client.CloseIdleConnections)
			requestDone := make(chan struct{})
			go func() {
				defer close(requestDone)
				response, err := client.Get("http://" + listener.Addr().String()) //nolint:noctx // Client has a deadline.
				if err == nil {
					_ = response.Body.Close()
				}
			}()
			requireReceive(t, requestStarted)
			cancel()
			requireReceive(t, requestCancelled)
			if !clean {
				// Forced socket closure proves the HTTP shutdown budget was exceeded.
				requireReceive(t, requestDone)
				require.Eventually(t, func() bool { return coordinator.Snapshot().State == ha.StatePassive }, time.Second, time.Millisecond,
					"a stuck handler must not keep renewing the lease after the HTTP drain deadline")
				requireReceive(t, jobsStopped)
				require.False(t, runtime.Active())
				select {
				case <-handlerDrained:
					t.Fatal("handler must remain blocked while renewal stops")
				default:
				}
			}
			require.Empty(t, store.released)
			exitHandler()
			runErr := <-result
			if clean {
				require.NoError(t, runErr)
				require.Len(t, store.released, 1)
				require.Equal(t, ha.Ownership{
					DCSClusterID: "cluster-a",
					Token:        ha.Token{WriterGeneration: 41, LeaseEpoch: 7},
					HolderID:     coordinator.HolderID(),
				}, <-store.released)
			} else {
				require.ErrorIs(t, runErr, context.DeadlineExceeded)
				require.Empty(t, store.released)
			}
			requireReceive(t, requestDone)
		})
	}
}

func TestRuntimeJobsDrainUnpairsBeforeTheirPluginDependency(t *testing.T) {
	for _, failedParticipant := range []string{"", "unpairs", "plugins"} {
		t.Run(failedParticipant, func(t *testing.T) {
			var stopped []string
			stopErr := errors.New("cleanup incomplete")
			jobs, err := newRuntimeJobs(runtimeJobLifecycles{
				plugins: stopOnlyLifecycle{stop: func(context.Context) error {
					stopped = append(stopped, "plugins")
					if failedParticipant == "plugins" {
						return stopErr
					}
					return nil
				}},
				pendingUnpairs: stopOnlyLifecycle{stop: func(context.Context) error {
					stopped = append(stopped, "unpairs")
					if failedParticipant == "unpairs" {
						return stopErr
					}
					return nil
				}},
				identityStateCleanup:      noopLifecycle{},
				commandArtifactCleanup:    noopLifecycle{},
				diagnosticsErrorCloser:    noopLifecycle{},
				telemetry:                 funcLifecycle{stop: func(context.Context) error { stopped = append(stopped, "telemetry"); return nil }},
				ipScanner:                 noopLifecycle{},
				commandExecution:          noopLifecycle{},
				miningGroupReconciliation: noopLifecycle{},
				poolConfigurationUpdates:  noopLifecycle{},
				scheduleProcessor:         noopLifecycle{},
				rolloutEnforcement:        noopLifecycle{},
				curtailmentReconciler:     noopLifecycle{},
				curtailmentMQTTSubscriber: noopLifecycle{},
				curtailmentAutomation:     noopLifecycle{},
				curtailmentRigConfig:      noopLifecycle{},
				chunkedUploadCleanup:      noopLifecycle{},
			})
			require.NoError(t, err)
			group, err := runtimejobs.NewGroup(jobs)
			require.NoError(t, err)
			require.NoError(t, group.Start(t.Context()))
			err = group.Stop(t.Context())
			if failedParticipant == "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, stopErr)
			}
			require.Equal(t, []string{"unpairs", "telemetry", "plugins"}, stopped)
		})
	}
}

func TestServeFleetRuntimeServeFailureIsNotPlannedStop(t *testing.T) {
	// Arrange
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	runtimeStarted := make(chan struct{})
	stopCause := make(chan error, 1)
	var plannedDrain atomic.Bool
	runtime := scriptedFleetRuntime{
		run: func(ctx context.Context) error {
			close(runtimeStarted)
			<-ctx.Done()
			stopCause <- context.Cause(ctx)
			return nil
		},
		beginDrain: func(time.Time) { plannedDrain.Store(true) },
	}
	result := make(chan error, 1)
	go func() {
		result <- serveFleetRuntimeWithContext(t.Context(), server, listener, runtime, time.Second, nil)
	}()
	requireReceive(t, runtimeStarted)

	// Act
	require.NoError(t, listener.Close())

	// Assert
	require.ErrorContains(t, <-result, "serve HTTP")
	cause := <-stopCause
	require.ErrorIs(t, cause, context.Canceled)
	require.NotErrorIs(t, cause, ha.ErrPlannedStop)
	require.False(t, plannedDrain.Load(), "a serve failure is not a planned drain")
}

func TestServeFleetRuntimeBeginsPlannedDrainBeforeStoppingRuntime(t *testing.T) {
	// Arrange
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	runtimeStarted := make(chan struct{})
	var plannedDrain atomic.Bool
	runtime := scriptedFleetRuntime{
		run: func(ctx context.Context) error {
			close(runtimeStarted)
			<-ctx.Done()
			if !plannedDrain.Load() {
				return errors.New("runtime stopped before the planned drain began")
			}
			return nil
		},
		beginDrain: func(time.Time) { plannedDrain.Store(true) },
	}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- serveFleetRuntimeWithContext(ctx, server, listener, runtime, time.Second, nil) }()
	requireReceive(t, runtimeStarted)

	// Act
	cancel()

	// Assert
	require.NoError(t, <-result)
	require.True(t, plannedDrain.Load())
}

func TestStopOnlyLifecycleBudgets(t *testing.T) {
	expired, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name         string
		parent       context.Context //nolint:containedctx // Table input for the group's stop context.
		lifecycle    stopOnlyLifecycle
		wantCanceled bool
		wantDeadline bool
	}{
		{name: "timeout bounds the participant", parent: t.Context(), lifecycle: stopOnlyLifecycle{timeout: time.Minute}, wantDeadline: true},
		{name: "attached participant keeps group cancellation", parent: expired, lifecycle: stopOnlyLifecycle{timeout: time.Minute}, wantCanceled: true, wantDeadline: true},
		{name: "detached participant keeps its own budget", parent: expired, lifecycle: stopOnlyLifecycle{timeout: time.Minute, detached: true}, wantDeadline: true},
		{name: "no timeout uses the group context", parent: t.Context(), lifecycle: stopOnlyLifecycle{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			var deadline time.Time
			var hasDeadline bool
			lifecycle := test.lifecycle
			lifecycle.stop = func(ctx context.Context) error {
				deadline, hasDeadline = ctx.Deadline()
				return ctx.Err()
			}

			// Act
			err := lifecycle.Stop(test.parent)

			// Assert
			if test.wantCanceled {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantDeadline, hasDeadline)
			if test.wantDeadline {
				require.InDelta(t, time.Minute.Seconds(), time.Until(deadline).Seconds(), 5)
			}
		})
	}
}
