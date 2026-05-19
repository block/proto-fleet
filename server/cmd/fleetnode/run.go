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
	"syscall"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/fleetnodebootstrap"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	sessionRefreshLeeway     = 1 * time.Hour
)

type RunCmd struct {
	HeartbeatInterval time.Duration `name:"heartbeat-interval" default:"30s" help:"interval between UploadHeartbeat calls"`
	PluginsDir        string        `name:"plugins-dir" help:"absolute path to the discovery plugins directory; defaults to <dir-of-binary>/plugins. If the resolved directory is missing the control loop stays off (heartbeat only)."`
	NmapPath          string        `name:"nmap-path" help:"absolute path to the nmap binary; defaults to <dir-of-binary>/nmap when present, otherwise to 'nmap' on PATH at scan time"`

	now           func() time.Time                                                `kong:"-"`
	clientFactory func(serverURL string, tokenSource func() string) gatewayClient `kong:"-"`
	signals       []os.Signal                                                     `kong:"-"`
	parentCtx     context.Context                                                 `kong:"-"` //nolint:containedctx // test seam for daemon shutdown without OS signals
	discoverer    discoverer                                                      `kong:"-"`
	nmapPath      string                                                          `kong:"-"`

	// Guards st.SessionToken: refreshAndSave writes; tokenSource reads.
	stateMu sync.Mutex `kong:"-"`
}

type gatewayClient interface {
	UploadHeartbeat(ctx context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error)
	ReportDiscoveredDevices(ctx context.Context, req *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error)
	ControlStream(ctx context.Context) *connect.BidiStreamForClient[pb.ControlStreamRequest, pb.ControlStreamResponse]
}

func (r *RunCmd) Run(c *Context) error {
	return r.run(c, os.Stderr)
}

func (r *RunCmd) run(c *Context, stderr io.Writer) error {
	if r.HeartbeatInterval <= 0 {
		r.HeartbeatInterval = defaultHeartbeatInterval
	}
	if r.now == nil {
		r.now = func() time.Time { return time.Now().UTC() }
	}
	if r.clientFactory == nil {
		r.clientFactory = func(url string, src func() string) gatewayClient {
			return fleetnodebootstrap.NewAuthenticatedGatewayClient(url, src)
		}
	}
	if len(r.signals) == 0 {
		r.signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}
	if r.parentCtx == nil {
		r.parentCtx = context.Background()
	}

	// Resolve --plugins-dir and --nmap-path before touching disk state so
	// misconfiguration fails fast. The plugin manager execs anything in the
	// resolved plugins dir, so any non-owner write capability there is
	// RCE-equivalent.
	exeDir := executableDir()
	var resolvedPluginsDir string
	if r.discoverer == nil {
		resolved, resolveErr := resolvePluginsDir(r.PluginsDir, exeDir)
		if resolveErr != nil {
			return resolveErr
		}
		resolvedPluginsDir = resolved
	}
	resolvedNmapPath, err := resolveNmapPath(r.NmapPath, exeDir)
	if err != nil {
		return err
	}
	r.nmapPath = resolvedNmapPath

	path := fleetnodebootstrap.StatePath(c.StateDir)
	st, exists, err := fleetnodebootstrap.LoadState(path)
	if err != nil {
		return err
	}
	if !exists || st.FleetNodeID == 0 {
		return fmt.Errorf("no state at %s; run `fleetnode enroll` first", path)
	}
	if st.APIKey == "" {
		return fmt.Errorf("state at %s has no api_key; complete enrollment via `fleetnode refresh` before running the daemon", path)
	}

	if resolvedPluginsDir != "" {
		disc, cleanup, bootstrapErr := newPluginDiscoverer(resolvedPluginsDir)
		if bootstrapErr != nil {
			return fmt.Errorf("bootstrap discovery plugins: %w", bootstrapErr)
		}
		defer cleanup()
		r.discoverer = disc
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	if resolvedPluginsDir != "" {
		source := "binary-adjacent"
		if r.PluginsDir != "" {
			source = "flag"
		}
		logger.Info("plugins dir resolved", "plugins_dir", resolvedPluginsDir, "source", source)
	} else if r.PluginsDir == "" {
		logger.Info("no plugins dir found adjacent to binary; control loop disabled (heartbeat only)")
	}

	return fleetnodebootstrap.WithStateLock(c.StateDir, func() error {
		return r.runLocked(c, logger)
	})
}

func (r *RunCmd) runLocked(c *Context, logger *slog.Logger) error {
	path := fleetnodebootstrap.StatePath(c.StateDir)
	st, exists, err := fleetnodebootstrap.LoadState(path)
	if err != nil {
		return err
	}
	if !exists || st.FleetNodeID == 0 || st.APIKey == "" {
		return fmt.Errorf("state at %s became invalid between checks; re-run after `fleetnode enroll`", path)
	}
	// Validate on every entry, not just on the refresh path, so a tampered
	// state cannot redirect bearer heartbeats to a plaintext non-loopback
	// URL when the existing session_token is still fresh.
	if err := fleetnodebootstrap.ValidateServerURL(st.ServerURL, st.AllowInsecureTransport); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(r.parentCtx, r.signals...)
	defer stop()

	if r.sessionNeedsRefresh(st) {
		if err := r.refreshAndSave(ctx, st, path, logger); err != nil {
			if errors.Is(err, fleetnodebootstrap.ErrBeginAuthRejected) {
				return fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Verify the api_key matches the one minted in the UI and retry; local credentials are preserved", fleetnodebootstrap.ErrBeginAuthRejected)
			}
			return fmt.Errorf("initial session refresh: %w", err)
		}
	}

	tokenSource := func() string {
		r.stateMu.Lock()
		defer r.stateMu.Unlock()
		return st.SessionToken
	}
	client := r.clientFactory(st.ServerURL, tokenSource)

	controlEnabled := r.discoverer != nil

	logger.Info("daemon started",
		"fleet_node_id", st.FleetNodeID,
		"server_url", st.ServerURL,
		"heartbeat_interval", r.HeartbeatInterval.String(),
		"control_loop_enabled", controlEnabled,
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

	if controlEnabled {
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
		if err != nil {
			return err
		}
	}
	logger.Info("daemon shutting down", "fleet_node_id", st.FleetNodeID)
	return nil
}

func (r *RunCmd) runHeartbeatLoop(ctx context.Context, client gatewayClient, st *fleetnodebootstrap.State, path string, logger *slog.Logger) error {
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

func (r *RunCmd) sessionNeedsRefresh(st *fleetnodebootstrap.State) bool {
	if st.SessionToken == "" {
		return true
	}
	if st.SessionExpiresAt.IsZero() {
		return true
	}
	return st.SessionExpiresAt.Sub(r.now()) < sessionRefreshLeeway
}

func (r *RunCmd) refreshAndSave(ctx context.Context, st *fleetnodebootstrap.State, path string, logger *slog.Logger) error {
	logger.Info("refreshing session", "fleet_node_id", st.FleetNodeID, "session_expires_at", st.SessionExpiresAt.Format(time.RFC3339))
	// Handshake against a shallow copy so the 2-RPC network call doesn't hold
	// stateMu and stall the control loop's token reads.
	next := *st
	if err := fleetnodebootstrap.Refresh(ctx, &next); err != nil {
		return err
	}
	r.stateMu.Lock()
	st.SessionToken = next.SessionToken
	st.SessionExpiresAt = next.SessionExpiresAt
	r.stateMu.Unlock()
	if err := fleetnodebootstrap.SaveState(path, st); err != nil {
		return fmt.Errorf("save state after refresh: %w", err)
	}
	logger.Info("session refreshed", "fleet_node_id", st.FleetNodeID, "session_expires_at", st.SessionExpiresAt.Format(time.RFC3339))
	return nil
}

// tick runs one heartbeat cycle. A non-nil return signals a permanent
// condition (server-side credential revoked or fleet_node deleted) that
// the operator must resolve by re-enrolling; the daemon exits instead of
// looping forever. Transient errors are logged and tick returns nil so
// the next tick can retry.
func (r *RunCmd) tick(ctx context.Context, client gatewayClient, st *fleetnodebootstrap.State, path string, logger *slog.Logger) error {
	if r.sessionNeedsRefresh(st) {
		if err := r.refreshAndSave(ctx, st, path, logger); err != nil {
			if errors.Is(err, fleetnodebootstrap.ErrBeginAuthRejected) {
				return fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Exiting; re-enroll once the operator-side cause is resolved", fleetnodebootstrap.ErrBeginAuthRejected)
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
		if errors.Is(refreshErr, fleetnodebootstrap.ErrBeginAuthRejected) {
			return fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Exiting; re-enroll once the operator-side cause is resolved", fleetnodebootstrap.ErrBeginAuthRejected)
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

func (r *RunCmd) sendHeartbeat(ctx context.Context, client gatewayClient) error {
	_, err := client.UploadHeartbeat(ctx, connect.NewRequest(&pb.UploadHeartbeatRequest{
		SentAt: timestamppb.New(r.now()),
	}))
	return err
}

var _ gatewayClient = fleetnodegatewayv1connect.FleetNodeGatewayServiceClient(nil)
