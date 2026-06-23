package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	sessionRefreshLeeway     = 1 * time.Hour
)

type RunCmd struct {
	HeartbeatInterval    time.Duration `name:"heartbeat-interval" default:"30s" help:"interval between UploadHeartbeat calls"`
	LocalDiscoverySubnet string        `name:"local-discovery-subnet" env:"FLEETNODE_LOCAL_DISCOVERY_SUBNET" help:"CIDR to scan for automatic local-subnet discovery instead of detecting the host subnet"`

	now           func() time.Time                                                         `kong:"-"`
	clientFactory func(serverURL string, tokenSource func() string) (gatewayClient, error) `kong:"-"`
	signals       []os.Signal                                                              `kong:"-"`
	parentCtx     context.Context                                                          `kong:"-"` //nolint:containedctx // test seam for daemon shutdown without OS signals
	discoverer    discoverer                                                               `kong:"-"`
	driverGetter  driverGetter                                                             `kong:"-"`
	minerSecrets  secretProvider                                                           `kong:"-"`
	pairer        pairer                                                                   `kong:"-"`
	nmapPath      string                                                                   `kong:"-"`
	resolver      ipResolver                                                               `kong:"-"`
	localSubnets  func() ([]string, error)                                                 `kong:"-"` // test seam for local-subnet detection

	stateMu sync.Mutex `kong:"-"` // guards st.SessionToken across refreshAndSave + tokenSource.
	pairMu  sync.Mutex `kong:"-"` // serializes pair commands; held until every pair worker exits (see handlePairCommand).
}

type gatewayClient interface {
	UploadHeartbeat(ctx context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error)
	ReportDiscoveredDevices(ctx context.Context, req *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error)
	ReportPairedDevices(ctx context.Context, req *connect.Request[pb.ReportPairedDevicesRequest]) (*connect.Response[pb.ReportPairedDevicesResponse], error)
	ControlStream(ctx context.Context) *connect.BidiStreamForClient[pb.ControlStreamRequest, pb.ControlStreamResponse]
}

func (r *RunCmd) Run(c *Context) error {
	return r.run(c, os.Stdout)
}

func (r *RunCmd) run(c *Context, logOutput io.Writer) error {
	if r.HeartbeatInterval <= 0 {
		r.HeartbeatInterval = defaultHeartbeatInterval
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
			return resolveErr
		}
		resolvedPluginsDir = resolved
	}
	path := bootstrap.StatePath(c.StateDir)
	st, exists, err := bootstrap.LoadState(path)
	if err != nil {
		return err
	}
	if !exists || st.FleetNodeID == 0 {
		return fmt.Errorf("no state at %s; run `fleetnode enroll` first", path)
	}
	if st.APIKey == "" {
		return fmt.Errorf("state at %s has no api_key; complete enrollment via `fleetnode refresh` before running the daemon", path)
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
	return bootstrap.WithStateLock(c.StateDir, func() error {
		return r.runLocked(ctx, c, resolvedPluginsDir, logger)
	})
}

func (r *RunCmd) runLocked(ctx context.Context, c *Context, resolvedPluginsDir string, logger *slog.Logger) error {
	path := bootstrap.StatePath(c.StateDir)
	st, exists, err := bootstrap.LoadState(path)
	if err != nil {
		return err
	}
	if !exists || st.FleetNodeID == 0 || st.APIKey == "" {
		return fmt.Errorf("state at %s became invalid between checks; re-run after `fleetnode enroll`", path)
	}
	// Re-validate on every entry so a tampered state.yaml can't redirect
	// bearer heartbeats to a plaintext non-loopback URL while the cached
	// session_token is still fresh.
	if err := bootstrap.ValidateServerURL(st.ServerURL, st.AllowInsecureTransport); err != nil {
		return err
	}

	// Reap + spawn inside the lock, from the state loaded under it, so the
	// synthesized auto: identifiers hash with the fleet_node_id the gateway
	// attributes reports to, and a contending agent's reaper can't kill our
	// children mid-startup.
	if resolvedPluginsDir != "" {
		reapOrphanedPlugins(ctx, resolvedPluginsDir, logger)
		credentials, credentialErr := ensureCredentialCodec(path, st)
		if credentialErr != nil {
			return fmt.Errorf("prepare credential key: %w", credentialErr)
		}
		disc, prr, cleanup, bootstrapErr := newPluginComponents(ctx, resolvedPluginsDir, st.FleetNodeID, credentials)
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
		r.pairer = prr
	}

	if r.sessionNeedsRefresh(st) {
		if err := r.refreshAndSave(ctx, st, path, logger); err != nil {
			if errors.Is(err, bootstrap.ErrBeginAuthRejected) {
				return fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Verify the api_key matches the one minted in the UI and retry; local credentials are preserved", bootstrap.ErrBeginAuthRejected)
			}
			return fmt.Errorf("initial session refresh: %w", err)
		}
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
	for err := range errCh {
		return err
	}
	logger.Info("daemon shutting down", "fleet_node_id", st.FleetNodeID)
	return nil
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
		return err
	}
	r.stateMu.Lock()
	st.SessionToken = next.SessionToken
	st.SessionExpiresAt = next.SessionExpiresAt
	// Snapshot under the lock so SaveState's yaml.Marshal doesn't race the
	// tokenSource goroutine that the control loop will add later.
	snapshot := *st
	r.stateMu.Unlock()
	if err := bootstrap.SaveState(path, &snapshot); err != nil {
		return fmt.Errorf("save state after refresh: %w", err)
	}
	logger.Info("session refreshed", "fleet_node_id", st.FleetNodeID, "session_expires_at", st.SessionExpiresAt.Format(time.RFC3339))
	return nil
}

// tick runs one heartbeat cycle. A non-nil return is a permanent condition
// (revoked credential / deleted fleet_node) that requires re-enrollment;
// transient errors return nil so the next tick retries.
func (r *RunCmd) tick(ctx context.Context, client gatewayClient, st *bootstrap.State, path string, logger *slog.Logger) error {
	if r.sessionNeedsRefresh(st) {
		if err := r.refreshAndSave(ctx, st, path, logger); err != nil {
			if errors.Is(err, bootstrap.ErrBeginAuthRejected) {
				return fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Exiting; re-enroll once the operator-side cause is resolved", bootstrap.ErrBeginAuthRejected)
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
		return fmt.Errorf("fleet_node not found server-side (revoked or deleted); exiting, re-enroll on this host: %w", err)
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		logger.Error("heartbeat failed", "fleet_node_id", st.FleetNodeID, "err", err)
		return nil
	}

	logger.Warn("heartbeat rejected as Unauthenticated; refreshing session and retrying", "fleet_node_id", st.FleetNodeID, "err", err)
	if refreshErr := r.refreshAndSave(ctx, st, path, logger); refreshErr != nil {
		if errors.Is(refreshErr, bootstrap.ErrBeginAuthRejected) {
			return fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Exiting; re-enroll once the operator-side cause is resolved", bootstrap.ErrBeginAuthRejected)
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
		return fmt.Errorf("fleet_node not found server-side (revoked or deleted); exiting, re-enroll on this host: %w", retryErr)
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
