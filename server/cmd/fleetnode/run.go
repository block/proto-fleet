package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
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

	now           func() time.Time                                                `kong:"-"`
	clientFactory func(serverURL string, tokenSource func() string) gatewayClient `kong:"-"`
	signals       []os.Signal                                                     `kong:"-"`
	parentCtx     context.Context                                                 `kong:"-"` //nolint:containedctx // test seam for daemon shutdown without OS signals
}

type gatewayClient interface {
	UploadHeartbeat(ctx context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error)
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

	logger := slog.New(slog.NewTextHandler(stderr, nil))

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

	client := r.clientFactory(st.ServerURL, func() string { return st.SessionToken })

	logger.Info("daemon started",
		"fleet_node_id", st.FleetNodeID,
		"server_url", st.ServerURL,
		"heartbeat_interval", r.HeartbeatInterval.String(),
		"session_expires_at", st.SessionExpiresAt.Format(time.RFC3339),
	)

	ticker := time.NewTicker(r.HeartbeatInterval)
	defer ticker.Stop()

	if err := r.tick(ctx, client, st, path, logger); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			logger.Info("daemon shutting down", "fleet_node_id", st.FleetNodeID)
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
	if err := fleetnodebootstrap.Refresh(ctx, st); err != nil {
		return err
	}
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
