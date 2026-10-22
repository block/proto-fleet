package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/block/proto-fleet/server/internal/ha"
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

func (l stopOrderedLifecycle) Abort() {
	if aborter, ok := l.lifecycle.(runtimejobs.Aborter); ok {
		aborter.Abort()
	}
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

// stopOnlyLifecycle includes already-created resources and request-spawned work
// in the runtime's ordered cleanup proof before voluntary HA lease release.
// A timeout keeps one participant from starving later ones of the shared group
// budget; detached participants keep their own budget even after earlier
// participants have used the group's.
type stopOnlyLifecycle struct {
	stop     func(context.Context) error
	timeout  time.Duration
	detached bool
}

func (l stopOnlyLifecycle) Start(context.Context) error { return nil }

func (l stopOnlyLifecycle) Stop(ctx context.Context) error {
	if l.detached {
		ctx = context.WithoutCancel(ctx)
	}
	if l.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.timeout)
		defer cancel()
	}
	return l.stop(ctx)
}

type runtimeJobGroupStopper interface {
	Stop(ctx context.Context) error
}

type fleetRuntimeRunner interface {
	Run(ctx context.Context) error
	BeginPlannedDrain(deadline time.Time)
}

// httpRequestDrain rejects new requests once shutdown begins and tracks handler
// completion, including HTTP/2 handlers on connections hijacked by h2c.
type httpRequestDrain struct {
	handler http.Handler
	mu      sync.Mutex
	stopped bool
	active  sync.WaitGroup
}

func (d *httpRequestDrain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		http.Error(w, "Fleet is shutting down", http.StatusServiceUnavailable)
		return
	}
	d.active.Add(1)
	d.mu.Unlock()
	defer d.active.Done()
	d.handler.ServeHTTP(w, r)
}

func (d *httpRequestDrain) stop() {
	d.mu.Lock()
	d.stopped = true
	d.mu.Unlock()
}

// httpConnectionListener retains ownership of accepted sockets after h2c
// hijacks them, when net/http no longer closes them during shutdown.
type httpConnectionListener struct {
	net.Listener
	mu      sync.Mutex
	conns   map[*httpConnection]struct{}
	stopped bool
}

func (l *httpConnectionListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("accept HTTP connection: %w", err)
	}
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	tracked := &httpConnection{Conn: conn, listener: l}
	l.conns[tracked] = struct{}{}
	l.mu.Unlock()
	return tracked, nil
}

func (l *httpConnectionListener) closeConnections() {
	l.mu.Lock()
	l.stopped = true
	conns := make([]*httpConnection, 0, len(l.conns))
	for conn := range l.conns {
		conns = append(conns, conn)
	}
	l.mu.Unlock()
	// Close outside the mutex: each connection removes itself from the registry.
	for _, conn := range conns {
		_ = conn.Close()
	}
}

type httpConnection struct {
	net.Conn
	listener *httpConnectionListener
}

func (c *httpConnection) Close() error {
	err := c.Conn.Close()
	c.listener.mu.Lock()
	delete(c.listener.conns, c)
	c.listener.mu.Unlock()
	if err != nil {
		return fmt.Errorf("close HTTP connection: %w", err)
	}
	return nil
}

// serveFleetRuntime starts serving always-on health before runtime activation.
// Process shutdown cancels requests before stopping their runtime dependencies.
func serveFleetRuntime(
	server *http.Server,
	listener net.Listener,
	runtime fleetRuntimeRunner,
	timeout time.Duration,
	drain *httpRequestDrain,
) error {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	return serveFleetRuntimeWithContext(ctx, server, listener, runtime, timeout, drain)
}

func serveFleetRuntimeWithContext(
	ctx context.Context,
	server *http.Server,
	listener net.Listener,
	runtime fleetRuntimeRunner,
	timeout time.Duration,
	drain *httpRequestDrain,
) error {
	runtimeCtx, cancelRuntime := context.WithCancelCause(context.Background())
	defer cancelRuntime(context.Canceled)
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server.BaseContext = func(net.Listener) context.Context { return requestCtx }
	if drain == nil {
		drain = &httpRequestDrain{handler: server.Handler}
		server.Handler = drain
	}

	connections := &httpConnectionListener{Listener: listener, conns: make(map[*httpConnection]struct{})}
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- server.Serve(connections)
	}()
	runtimeResult := make(chan error, 1)
	go func() {
		runtimeResult <- runtime.Run(runtimeCtx)
	}()

	var serverErr, runtimeErr error
	var serverStopped, runtimeStopped bool
	plannedDrain := false
	select {
	case <-ctx.Done():
		plannedDrain = true
	case runtimeErr = <-runtimeResult:
		runtimeStopped = true
		if runtimeErr == nil {
			runtimeErr = errors.New("Fleet runtime stopped unexpectedly")
		}
	case serverErr = <-serverResult:
		serverStopped = true
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	if plannedDrain {
		deadline, _ := shutdownCtx.Deadline()
		runtime.BeginPlannedDrain(deadline)
	}
	// Closing admission before cancellation prevents keep-alive and h2c requests
	// from starting work after the drain has begun.
	drain.stop()
	cancelRequests()
	shutdownErr := server.Shutdown(shutdownCtx)
	shutdownCancel()
	if shutdownErr != nil {
		// Closing sockets cancels transports, but does not prove handlers ended.
		_ = server.Close()
	}
	// Shutdown and Close exclude hijacked h2c connections. Interrupt their I/O
	// before waiting for handlers, while retaining dependencies through cleanup.
	connections.closeConnections()
	// A handler that ignores cancellation must keep its dependencies and binding
	// locks until it actually returns; a timeout cannot establish completion.
	drain.active.Wait()
	if !serverStopped {
		serverErr = <-serverResult
	}
	// Only signal-driven shutdown with a completed HTTP drain may voluntarily
	// release HA ownership. Serve failures and uncertain cleanup retain the TTL.
	stopCause := context.Canceled
	if ctx.Err() != nil && shutdownErr == nil && !runtimeStopped &&
		(serverErr == nil || errors.Is(serverErr, http.ErrServerClosed)) {
		stopCause = ha.ErrPlannedStop
	}
	cancelRuntime(stopCause)
	if !runtimeStopped {
		runtimeErr = <-runtimeResult
	}

	var result error
	if runtimeErr != nil {
		result = runtimeErr
	}
	if shutdownErr != nil {
		result = errors.Join(result, fmt.Errorf("shutdown HTTP server: %w", shutdownErr))
	}
	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		result = errors.Join(result, fmt.Errorf("serve HTTP: %w", serverErr))
	}
	return result
}

// stopRuntimeJobGroupAfterRun avoids repeating cleanup already completed by a fatal HA abort.
func stopRuntimeJobGroupAfterRun(
	runErr error,
	group runtimeJobGroupStopper,
	commandExecution runtimejobs.Lifecycle,
	timeout time.Duration,
) {
	if errors.Is(runErr, ha.ErrRuntimeAborted) {
		return
	}
	stopRuntimeJobGroup(group, commandExecution, timeout)
}

// stopRuntimeJobGroup gives the group one graceful-shutdown budget. Command
// execution receives a final independent budget because its activation is
// detached from group cancellation to preserve shutdown ordering.
func stopRuntimeJobGroup(group runtimeJobGroupStopper, commandExecution runtimejobs.Lifecycle, timeout time.Duration) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	err := group.Stop(shutdownCtx)
	cancel()
	if err != nil {
		slog.Error("failed to stop runtime jobs", "error", err)
	}

	commandCtx, commandCancel := context.WithTimeout(context.Background(), timeout)
	err = commandExecution.Stop(commandCtx)
	commandCancel()
	if err != nil {
		slog.Error("failed to stop command execution after runtime jobs", "error", err)
	}
}

type runtimeJobLifecycles struct {
	plugins                   runtimejobs.Lifecycle
	pendingUnpairs            runtimejobs.Lifecycle
	identityStateCleanup      runtimejobs.Lifecycle
	commandArtifactCleanup    runtimejobs.Lifecycle
	diagnosticsErrorCloser    runtimejobs.Lifecycle
	telemetry                 runtimejobs.Lifecycle
	ipScanner                 runtimejobs.Lifecycle
	commandExecution          runtimejobs.Lifecycle
	scheduleProcessor         runtimejobs.Lifecycle
	rolloutEnforcement        runtimejobs.Lifecycle
	curtailmentReconciler     runtimejobs.Lifecycle
	curtailmentMQTTSubscriber runtimejobs.Lifecycle
	curtailmentRigConfig      runtimejobs.Lifecycle
	curtailmentAlertMetrics   runtimejobs.Lifecycle
	chunkedUploadCleanup      runtimejobs.Lifecycle
	systemMonitoring          runtimejobs.Lifecycle
	haReadiness               runtimejobs.Lifecycle
	releaseChecker            runtimejobs.Lifecycle
}

func newRuntimeJobs(lifecycles runtimeJobLifecycles) ([]runtimejobs.Job, error) {
	var jobs []runtimejobs.Job
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
		{name: "plugins", lifecycle: lifecycles.plugins},
		{name: "identity-state-cleanup", lifecycle: lifecycles.identityStateCleanup},
		{name: "command-artifact-cleanup", lifecycle: lifecycles.commandArtifactCleanup},
		{name: "diagnostics-error-closer", lifecycle: lifecycles.diagnosticsErrorCloser},
		{name: "telemetry", lifecycle: lifecycles.telemetry},
		{name: "ip-scanner", lifecycle: lifecycles.ipScanner},
		{name: "command-execution", lifecycle: commandExecution},
		{name: "schedule-processor", lifecycle: lifecycles.scheduleProcessor},
		{name: "rollout-enforcement", lifecycle: lifecycles.rolloutEnforcement},
		{name: "curtailment-reconciler", lifecycle: lifecycles.curtailmentReconciler},
		{name: "curtailment-mqtt-subscriber", lifecycle: lifecycles.curtailmentMQTTSubscriber},
		{name: "curtailment-rig-config", lifecycle: lifecycles.curtailmentRigConfig},
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
	if lifecycles.haReadiness != nil {
		if err := add("ha-readiness", lifecycles.haReadiness); err != nil {
			return nil, err
		}
	}
	if lifecycles.releaseChecker != nil {
		if err := add("release-checker", lifecycles.releaseChecker); err != nil {
			return nil, err
		}
	}

	// Stop background unpairs before their telemetry/plugin dependencies. All
	// HTTP handlers have drained before a planned stop reaches this participant.
	if err := add("pending-unpairs", lifecycles.pendingUnpairs); err != nil {
		return nil, err
	}
	return jobs, nil
}
