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
	"syscall"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
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
		rolloutEnforcement:        noopLifecycle{},
		curtailmentReconciler:     noopLifecycle{},
		curtailmentMQTTSubscriber: noopLifecycle{},
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
		"identity-state-cleanup",
		"command-artifact-cleanup",
		"diagnostics-error-closer",
		"telemetry",
		"ip-scanner",
		"command-execution",
		"schedule-processor",
		"rollout-enforcement",
		"curtailment-reconciler",
		"curtailment-mqtt-subscriber",
		"curtailment-rig-config",
		"curtailment-alert-metrics",
		"chunked-upload-cleanup",
		"system-monitoring",
		"ha-readiness",
		"release-checker",
	}, jobNames(jobs))

	all.curtailmentAlertMetrics = nil
	all.systemMonitoring = nil
	all.haReadiness = nil
	all.releaseChecker = nil
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
		"rollout-enforcement",
		"curtailment-reconciler",
		"curtailment-mqtt-subscriber",
		"curtailment-rig-config",
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
		rolloutEnforcement:        noopLifecycle{},
		curtailmentReconciler:     noopLifecycle{},
		curtailmentMQTTSubscriber: noopLifecycle{},
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

func TestServeFleetRuntimeSignalShutdown(t *testing.T) {
	if os.Getenv("FLEETD_SHUTDOWN_SIGNAL_TEST") == "1" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		server := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
		runtime := scriptedFleetRuntime{run: func(ctx context.Context) error {
			_, _ = fmt.Fprintln(os.Stdout, "ready for shutdown signal")
			<-ctx.Done()
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
