package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/coreos/go-systemd/v22/daemon"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	sessionRefreshLeeway     = 1 * time.Hour
)

// Var so the bounded-shutdown path can be exercised without a 10-second test.
var controlWorkerShutdownTimeout = 10 * time.Second

type RunCmd struct {
	HeartbeatInterval    time.Duration `name:"heartbeat-interval" default:"30s" help:"interval between UploadHeartbeat calls (maximum 30s)"`
	LocalDiscoverySubnet string        `name:"local-discovery-subnet" env:"FLEETNODE_LOCAL_DISCOVERY_SUBNET" help:"CIDR to scan for automatic local-subnet discovery instead of detecting the host subnet"`

	now                      func() time.Time                                                         `kong:"-"`
	clientFactory            func(serverURL string, tokenSource func() string) (gatewayClient, error) `kong:"-"`
	signals                  []os.Signal                                                              `kong:"-"`
	parentCtx                context.Context                                                          `kong:"-"` //nolint:containedctx // test seam for daemon shutdown without OS signals
	discoverer               discoverer                                                               `kong:"-"`
	driverGetter             driverGetter                                                             `kong:"-"`
	minerSecrets             secretProvider                                                           `kong:"-"`
	passwordUpdatePrivateKey []byte                                                                   `kong:"-"`
	pairer                   pairer                                                                   `kong:"-"`
	telemetry                telemetryFetcher                                                         `kong:"-"`
	nmapPath                 string                                                                   `kong:"-"`
	resolver                 ipResolver                                                               `kong:"-"`
	localSubnets             func() ([]string, error)                                                 `kong:"-"` // test seam for local-subnet detection
	firmwareTempRoot         string                                                                   `kong:"-"`
	notifyReady              func() error                                                             `kong:"-"`

	stateMu sync.Mutex `kong:"-"` // guards session state and the active control-session cancel func.
	pairMu  sync.Mutex `kong:"-"` // serializes pair commands; held until every pair worker exits (see handlePairCommand).

	controlSessionCancel context.CancelCauseFunc `kong:"-"`

	controlConcurrencyOnce sync.Once      `kong:"-"`
	controlCommandSlots    chan struct{}  `kong:"-"`
	controlDiscoverySlot   chan struct{}  `kong:"-"`
	controlWorkers         sync.WaitGroup `kong:"-"`
}

type gatewayClient interface {
	UploadHeartbeat(ctx context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error)
	ReportDiscoveredDevices(ctx context.Context, req *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error)
	ReportPairedDevices(ctx context.Context, req *connect.Request[pb.ReportPairedDevicesRequest]) (*connect.Response[pb.ReportPairedDevicesResponse], error)
	UploadCommandArtifact(ctx context.Context) *connect.ClientStreamForClient[pb.UploadCommandArtifactRequest, pb.UploadCommandArtifactResponse]
	DownloadCommandArtifact(ctx context.Context, req *connect.Request[pb.DownloadCommandArtifactRequest]) (*connect.ServerStreamForClient[pb.DownloadCommandArtifactResponse], error)
	ControlStream(ctx context.Context) *connect.BidiStreamForClient[pb.ControlStreamRequest, pb.ControlStreamResponse]
}

func (r *RunCmd) Run(c *Context) error {
	return r.run(c, os.Stdout)
}

func (r *RunCmd) run(c *Context, logOutput io.Writer) error {
	if err := r.validateHeartbeatInterval(); err != nil {
		return err
	}
	if r.now == nil {
		r.now = func() time.Time { return time.Now().UTC() }
	}
	if r.clientFactory == nil {
		r.clientFactory = func(url string, src func() string) (gatewayClient, error) {
			return bootstrap.NewAuthenticatedGatewayClient(url, src)
		}
	}
	if len(r.signals) == 0 {
		r.signals = defaultSignals()
	}
	if r.parentCtx == nil {
		r.parentCtx = context.Background()
	}
	if r.notifyReady == nil {
		r.notifyReady = func() error {
			_, err := daemon.SdNotify(false, daemon.SdNotifyReady)
			if err != nil {
				return fmt.Errorf("send systemd readiness notification: %w", err)
			}
			return nil
		}
	}

	// Wire signals before plugin work so a SIGTERM during the up-to-60s
	// plugin load aborts cleanly instead of orphaning subprocesses.
	ctx, stop := signal.NotifyContext(r.parentCtx, r.signals...)
	defer stop()

	// Resolve binary-adjacent plugins/nmap before touching disk state so
	// misconfiguration fails fast.
	exeDir := executableDir()
	var resolvedPluginsDir string
	if r.discoverer == nil {
		resolved, resolveErr := resolvePluginsDir(exeDir)
		if resolveErr != nil {
			return operatorActionRequired(resolveErr)
		}
		resolvedPluginsDir = resolved
	}
	path := bootstrap.StatePath(c.StateDir)
	st, exists, err := bootstrap.LoadState(path)
	if err != nil {
		return operatorActionRequired(err)
	}
	if !exists || st.FleetNodeID == 0 {
		return operatorActionRequired(fmt.Errorf("no state at %s; run `fleetnode enroll` first", path))
	}
	if st.APIKey == "" {
		return operatorActionRequired(fmt.Errorf("state at %s has no api_key; complete enrollment via `fleetnode refresh` before running the daemon", path))
	}

	logger := slog.New(slog.NewTextHandler(logOutput, nil))
	r.nmapPath = resolveNmapPath(exeDir, logger)
	switch {
	case resolvedPluginsDir != "":
		logger.Info("plugins dir resolved", "plugins_dir", resolvedPluginsDir)
	case r.discoverer != nil:
		logger.Info("using injected discoverer; plugins dir resolution skipped")
	default:
		logger.Info("no plugins dir found adjacent to binary; control loop disabled (heartbeat only)")
	}

	// runLocked reloads state under the lock and bootstraps plugins from it, so a
	// concurrent enroll/refresh that rewrites state.yaml while we wait for the lock
	// can't leave the discoverer scoped to a stale fleet_node_id.
	logger.Info("acquiring state lock", "state_dir", c.StateDir)
	var runErr error
	if err := bootstrap.WithStateLock(c.StateDir, func() error {
		runErr = r.runLocked(ctx, c, resolvedPluginsDir, logger)
		// Keep runLocked's exit classification separate from lock acquisition errors.
		return nil
	}); err != nil {
		return operatorActionRequired(err)
	}
	return runErr
}

func (r *RunCmd) validateHeartbeatInterval() error {
	if r.HeartbeatInterval == 0 {
		r.HeartbeatInterval = defaultHeartbeatInterval
	}
	if r.HeartbeatInterval < 0 {
		return fmt.Errorf("heartbeat interval must be positive")
	}
	if r.HeartbeatInterval > defaultHeartbeatInterval {
		return fmt.Errorf("heartbeat interval must be no greater than %s", defaultHeartbeatInterval)
	}
	return nil
}

func (r *RunCmd) runLocked(ctx context.Context, c *Context, resolvedPluginsDir string, logger *slog.Logger) error {
	path := bootstrap.StatePath(c.StateDir)
	st, exists, err := bootstrap.LoadState(path)
	if err != nil {
		return operatorActionRequired(err)
	}
	if !exists || st.FleetNodeID == 0 || st.APIKey == "" {
		return operatorActionRequired(fmt.Errorf("state at %s became invalid between checks; re-run after `fleetnode enroll`", path))
	}
	// Re-validate on every entry so a tampered state.yaml can't redirect
	// bearer heartbeats to a plaintext non-loopback URL while the cached
	// session_token is still fresh.
	if err := bootstrap.ValidateServerURL(st.ServerURL, st.AllowInsecureTransport); err != nil {
		return operatorActionRequired(err)
	}
	r.firmwareTempRoot = filepath.Join(c.StateDir, firmwareArtifactTempDirName)
	if err := prepareFirmwareArtifactTempRoot(r.firmwareTempRoot); err != nil {
		return operatorActionRequired(fmt.Errorf("prepare firmware temp dir: %w", err))
	}

	// Reap + spawn inside the lock, from the state loaded under it, so the
	// synthesized auto: identifiers hash with the fleet_node_id the gateway
	// attributes reports to, and a contending agent's reaper can't kill our
	// children mid-startup.
	if resolvedPluginsDir != "" {
		reapOrphanedPlugins(ctx, resolvedPluginsDir, logger)
		credentials, credentialErr := ensureCredentialCodec(path, st)
		if credentialErr != nil {
			return operatorActionRequired(fmt.Errorf("prepare credential key: %w", credentialErr))
		}
		disc, prr, tf, cleanup, bootstrapErr := newPluginComponents(ctx, resolvedPluginsDir, st.FleetNodeID, credentials)
		if bootstrapErr != nil {
			return fmt.Errorf("bootstrap plugins: %w", bootstrapErr)
		}
		defer cleanup()
		r.discoverer = disc
		// Same plugin manager powers discovery, command execution, and pairing; don't
		// load plugins twice.
		if r.driverGetter == nil {
			r.driverGetter = disc.svc.GetManager()
		}
		if r.minerSecrets == nil {
			r.minerSecrets = credentials
		}
		if r.passwordUpdatePrivateKey == nil {
			privateKey, err := decodePasswordUpdatePrivateKey(st)
			if err != nil {
				return operatorActionRequired(err)
			}
			r.passwordUpdatePrivateKey = privateKey
		}
		r.pairer = prr
		r.telemetry = tf
	}

	tokenSource := func() string {
		r.stateMu.Lock()
		defer r.stateMu.Unlock()
		return st.SessionToken
	}
	client, err := r.clientFactory(st.ServerURL, tokenSource)
	if err != nil {
		return err
	}

	logger.Info("daemon started",
		"fleet_node_id", st.FleetNodeID,
		"server_url", st.ServerURL,
		"heartbeat_interval", r.HeartbeatInterval.String(),
		"control_loop_enabled", r.discoverer != nil,
		"session_expires_at", st.SessionExpiresAt.Format(time.RFC3339),
	)

	if err := r.tick(ctx, client, st, path, logger); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := r.notifyReady(); err != nil {
		return fmt.Errorf("notify systemd ready: %w", err)
	}

	loopCtx, cancelLoops := context.WithCancel(ctx)
	defer cancelLoops()

	errCh := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := r.runHeartbeatLoop(loopCtx, client, st, path, logger); err != nil {
			errCh <- err
			cancelLoops()
		}
	}()

	if r.discoverer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.runControlLoop(loopCtx, client, st, logger); err != nil {
				errCh <- err
				cancelLoops()
			}
		}()
	}

	wg.Wait()
	close(errCh)
	var loopErr error
	for err := range errCh {
		if loopErr == nil {
			loopErr = err
		}
	}
	r.waitForControlWorkers(logger)
	if loopErr != nil {
		return loopErr
	}
	logger.Info("daemon shutting down", "fleet_node_id", st.FleetNodeID)
	return nil
}

func (r *RunCmd) initControlConcurrency() {
	r.controlConcurrencyOnce.Do(func() {
		r.controlCommandSlots = make(chan struct{}, commandPoolSize)
		r.controlDiscoverySlot = make(chan struct{}, 1)
	})
}

func (r *RunCmd) waitForControlWorkers(logger *slog.Logger) {
	r.initControlConcurrency()
	done := make(chan struct{})
	go func() {
		r.controlWorkers.Wait()
		close(done)
	}()
	timer := time.NewTimer(controlWorkerShutdownTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		logger.Warn("control command handlers still running; continuing daemon shutdown",
			"timeout", controlWorkerShutdownTimeout.String(),
			"workers", len(r.controlCommandSlots)+len(r.controlDiscoverySlot))
	}
}

func (r *RunCmd) runHeartbeatLoop(ctx context.Context, client gatewayClient, st *bootstrap.State, path string, logger *slog.Logger) error {
	ticker := time.NewTicker(r.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.tick(ctx, client, st, path, logger); err != nil {
				return err
			}
		}
	}
}

func (r *RunCmd) sessionNeedsRefresh(st *bootstrap.State) bool {
	if st.SessionToken == "" {
		return true
	}
	if st.SessionExpiresAt.IsZero() {
		return true
	}
	return st.SessionExpiresAt.Sub(r.now()) < sessionRefreshLeeway
}

func (r *RunCmd) refreshAndSave(ctx context.Context, st *bootstrap.State, path string, logger *slog.Logger) error {
	logger.Info("refreshing session", "fleet_node_id", st.FleetNodeID, "session_expires_at", st.SessionExpiresAt.Format(time.RFC3339))
	// Handshake on a shallow copy so the 2-RPC call doesn't hold stateMu and
	// stall the control loop's token reads.
	next := *st
	if err := bootstrap.Refresh(ctx, &next); err != nil {
		if errors.Is(err, bootstrap.ErrBeginAuthRejected) || connect.CodeOf(err) != connect.CodeUnauthenticated {
			return err
		}
		logger.Warn("session refresh completion rejected; retrying handshake once", "fleet_node_id", st.FleetNodeID, "err", err)
		if err := bootstrap.Refresh(ctx, &next); err != nil {
			return err
		}
	}
	r.stateMu.Lock()
	st.SessionToken = next.SessionToken
	st.SessionExpiresAt = next.SessionExpiresAt
	// Snapshot under the lock so SaveState's yaml.Marshal doesn't race the
	// tokenSource goroutine that the control loop will add later.
	snapshot := *st
	cancelControlSession := r.controlSessionCancel
	r.stateMu.Unlock()
	if cancelControlSession != nil {
		cancelControlSession(errControlSessionRotated)
	}
	if err := bootstrap.SaveState(path, &snapshot); err != nil {
		return fmt.Errorf("save state after refresh: %w", err)
	}
	logger.Info("session refreshed", "fleet_node_id", st.FleetNodeID, "session_expires_at", st.SessionExpiresAt.Format(time.RFC3339))
	return nil
}

func isRetryableRefreshError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return false
	}
	code := connectErr.Code()
	return code == connect.CodeCanceled ||
		code == connect.CodeDeadlineExceeded ||
		code == connect.CodeResourceExhausted ||
		code == connect.CodeAborted ||
		code == connect.CodeInternal ||
		code == connect.CodeUnknown ||
		code == connect.CodeUnimplemented ||
		code == connect.CodeUnavailable
}

// tick runs one heartbeat cycle. A non-nil return is a permanent condition
// (revoked credential / deleted fleet_node) that requires re-enrollment;
// transient errors return nil so the next tick retries.
func (r *RunCmd) tick(ctx context.Context, client gatewayClient, st *bootstrap.State, path string, logger *slog.Logger) error {
	if r.sessionNeedsRefresh(st) {
		if err := r.refreshAndSave(ctx, st, path, logger); err != nil {
			if errors.Is(err, bootstrap.ErrBeginAuthRejected) {
				return operatorActionRequired(fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Exiting; local credentials are preserved, re-enroll once the operator-side cause is resolved", bootstrap.ErrBeginAuthRejected))
			}
			if !isRetryableRefreshError(err) {
				return operatorActionRequired(fmt.Errorf("session refresh requires operator action: %w", err))
			}
			logger.Error("session refresh failed; will retry on next tick", "fleet_node_id", st.FleetNodeID, "err", err)
			return nil
		}
	}

	err := r.sendHeartbeat(ctx, client)
	if err == nil {
		logger.Info("heartbeat sent", "fleet_node_id", st.FleetNodeID)
		return nil
	}
	if code := connect.CodeOf(err); code == connect.CodeNotFound {
		return operatorActionRequired(fmt.Errorf("fleet_node not found server-side (revoked or deleted); exiting, re-enroll on this host: %w", err))
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		logger.Error("heartbeat failed", "fleet_node_id", st.FleetNodeID, "err", err)
		return nil
	}

	logger.Warn("heartbeat rejected as Unauthenticated; refreshing session and retrying", "fleet_node_id", st.FleetNodeID, "err", err)
	if refreshErr := r.refreshAndSave(ctx, st, path, logger); refreshErr != nil {
		if errors.Is(refreshErr, bootstrap.ErrBeginAuthRejected) {
			return operatorActionRequired(fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Exiting; re-enroll once the operator-side cause is resolved", bootstrap.ErrBeginAuthRejected))
		}
		if !isRetryableRefreshError(refreshErr) {
			return operatorActionRequired(fmt.Errorf("session refresh requires operator action: %w", refreshErr))
		}
		logger.Error("post-Unauthenticated refresh failed; will retry on next tick", "fleet_node_id", st.FleetNodeID, "err", refreshErr)
		return nil
	}
	retryErr := r.sendHeartbeat(ctx, client)
	if retryErr == nil {
		logger.Info("heartbeat sent after refresh", "fleet_node_id", st.FleetNodeID)
		return nil
	}
	if code := connect.CodeOf(retryErr); code == connect.CodeNotFound {
		return operatorActionRequired(fmt.Errorf("fleet_node not found server-side (revoked or deleted); exiting, re-enroll on this host: %w", retryErr))
	}
	logger.Error("heartbeat retry after refresh failed", "fleet_node_id", st.FleetNodeID, "err", retryErr)
	return nil
}

const heartbeatTimeout = 30 * time.Second

func (r *RunCmd) sendHeartbeat(ctx context.Context, client gatewayClient) error {
	callCtx, cancel := context.WithTimeout(ctx, heartbeatTimeout)
	defer cancel()
	_, err := client.UploadHeartbeat(callCtx, connect.NewRequest(&pb.UploadHeartbeatRequest{
		SentAt: timestamppb.New(r.now()),
	}))
	return err
}

var _ gatewayClient = fleetnodegatewayv1connect.FleetNodeGatewayServiceClient(nil)
