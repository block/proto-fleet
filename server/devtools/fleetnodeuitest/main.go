package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"

	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	fleetnodeadminv1 "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	onboardingv1 "github.com/block/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

type config struct {
	apiURL        string
	nodeServerURL string
	stateDir      string
	nodeName      string
	adminUsername string
	adminPassword string
	allowInsecure bool
	timeout       time.Duration
}

type refreshFunc func(context.Context, *bootstrap.State) error

func main() {
	cfg := parseFlags()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)

	if err := ensureFleetNode(ctx, cfg); err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "fleetnode-ui-test enroll failed: %v\n", err)
		os.Exit(1)
	}
	cancel()
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.apiURL, "api-url", envOrDefault("FLEET_API_URL", "http://localhost:4000"), "Fleet API URL reachable by this helper")
	flag.StringVar(&cfg.nodeServerURL, "node-server-url", envOrDefault("FLEETNODE_SERVER_URL", ""), "Fleet API URL persisted into fleetnode state")
	flag.StringVar(&cfg.stateDir, "state-dir", envOrDefault("FLEETNODE_STATE_DIR", "/state"), "fleetnode state directory")
	flag.StringVar(&cfg.nodeName, "node-name", envOrDefault("FLEETNODE_NAME", "ui-test-fleetnode"), "fleet node name")
	flag.StringVar(&cfg.adminUsername, "admin-username", envOrDefault("FLEET_ADMIN_USERNAME", "admin"), "dev admin username")
	flag.StringVar(&cfg.adminPassword, "admin-password", envOrDefault("FLEET_ADMIN_PASSWORD", "Pass123!"), "dev admin password")
	flag.BoolVar(&cfg.allowInsecure, "allow-insecure-transport", envOrDefault("FLEETNODE_ALLOW_INSECURE_TRANSPORT", "true") == "true", "allow http:// fleetnode server URLs")
	flag.DurationVar(&cfg.timeout, "timeout", 60*time.Second, "overall enrollment timeout")
	flag.Parse()
	if cfg.nodeServerURL == "" {
		cfg.nodeServerURL = cfg.apiURL
	}
	return cfg
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func ensureFleetNode(ctx context.Context, cfg config) error {
	reused, err := ensureUsableState(ctx, cfg.stateDir, cfg.apiURL, cfg.nodeServerURL, cfg.allowInsecure, bootstrap.Refresh)
	if err != nil {
		return err
	}
	if reused {
		fmt.Printf("reusing enrolled fleet node state at %s\n", bootstrap.StatePath(cfg.stateDir))
		return nil
	}

	cookie, err := ensureSessionCookie(ctx, cfg)
	if err != nil {
		return err
	}
	adminClient := fleetnodeadminv1connect.NewFleetNodeAdminServiceClient(http.DefaultClient, cfg.apiURL)
	if err := revokeExistingFleetNodesByName(ctx, adminClient, cookie, cfg.nodeName); err != nil {
		return err
	}

	codeReq := connect.NewRequest(&fleetnodeadminv1.CreateEnrollmentCodeRequest{})
	codeReq.Header().Set("Cookie", cookie)
	codeResp, err := adminClient.CreateEnrollmentCode(ctx, codeReq)
	if err != nil {
		return fmt.Errorf("create enrollment code: %w", err)
	}

	result, err := bootstrap.Register(ctx, bootstrap.RegisterParams{
		ServerURL:              cfg.apiURL,
		Name:                   cfg.nodeName,
		Code:                   codeResp.Msg.GetCode(),
		AllowInsecureTransport: cfg.allowInsecure,
	})
	if err != nil {
		return fmt.Errorf("register fleet node: %w", err)
	}

	confirmReq := connect.NewRequest(&fleetnodeadminv1.ConfirmFleetNodeRequest{
		FleetNodeId:         result.State.FleetNodeID,
		PendingEnrollmentId: codeResp.Msg.GetPendingEnrollmentId(),
	})
	confirmReq.Header().Set("Cookie", cookie)
	confirmResp, err := adminClient.ConfirmFleetNode(ctx, confirmReq)
	if err != nil {
		return fmt.Errorf("confirm fleet node: %w", err)
	}

	if err := bootstrap.CompleteEnrollment(ctx, result.State, confirmResp.Msg.GetApiKey()); err != nil {
		return fmt.Errorf("complete fleet node enrollment: %w", err)
	}

	result.State.ServerURL = cfg.nodeServerURL
	result.State.AllowInsecureTransport = cfg.allowInsecure
	if err := bootstrap.SaveState(bootstrap.StatePath(cfg.stateDir), result.State); err != nil {
		return fmt.Errorf("save fleet node state: %w", err)
	}
	fmt.Printf("enrolled fleet_node_id=%d name=%q state=%s\n", result.State.FleetNodeID, cfg.nodeName, bootstrap.StatePath(cfg.stateDir))
	return nil
}

func revokeExistingFleetNodesByName(ctx context.Context, adminClient fleetnodeadminv1connect.FleetNodeAdminServiceClient, cookie, nodeName string) error {
	listReq := connect.NewRequest(&fleetnodeadminv1.ListFleetNodesRequest{})
	listReq.Header().Set("Cookie", cookie)
	listResp, err := adminClient.ListFleetNodes(ctx, listReq)
	if err != nil {
		return fmt.Errorf("list fleet nodes before enrollment: %w", err)
	}

	for _, node := range listResp.Msg.GetFleetNodes() {
		if node.GetName() != nodeName || node.GetEnrollmentStatus() == fleetnodeadminv1.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_REVOKED {
			continue
		}
		revokeMsg := &fleetnodeadminv1.RevokeFleetNodeRequest{
			FleetNodeId: node.GetFleetNodeId(),
		}
		if node.GetEnrollmentStatus() == fleetnodeadminv1.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_AWAITING_CONFIRMATION {
			if pendingEnrollmentID := node.GetPendingEnrollmentId(); pendingEnrollmentID > 0 {
				revokeMsg.PendingEnrollmentId = &pendingEnrollmentID
			}
		}
		revokeReq := connect.NewRequest(revokeMsg)
		revokeReq.Header().Set("Cookie", cookie)
		if _, err := adminClient.RevokeFleetNode(ctx, revokeReq); err != nil {
			return fmt.Errorf("revoke existing fleet node %d (%q): %w", node.GetFleetNodeId(), nodeName, err)
		}
		fmt.Printf("revoked existing fleet_node_id=%d name=%q before re-enrollment\n", node.GetFleetNodeId(), nodeName)
	}
	return nil
}

func ensureUsableState(ctx context.Context, stateDir, apiURL, nodeServerURL string, allowInsecure bool, refresh refreshFunc) (bool, error) {
	statePath := bootstrap.StatePath(stateDir)
	st, exists, err := bootstrap.LoadState(statePath)
	if err != nil {
		return false, err
	}
	if !exists || st.FleetNodeID == 0 || st.APIKey == "" {
		return false, nil
	}

	candidate := *st
	candidate.ServerURL = apiURL
	candidate.AllowInsecureTransport = allowInsecure
	stateReusable := refresh(ctx, &candidate) == nil
	if !stateReusable {
		return false, nil
	}

	candidate.ServerURL = nodeServerURL
	candidate.AllowInsecureTransport = allowInsecure
	if err := bootstrap.SaveState(statePath, &candidate); err != nil {
		return false, fmt.Errorf("save refreshed fleet node state: %w", err)
	}
	return true, nil
}

func ensureSessionCookie(ctx context.Context, cfg config) (string, error) {
	onboardingClient := onboardingv1connect.NewOnboardingServiceClient(http.DefaultClient, cfg.apiURL)
	initResp, err := onboardingClient.GetFleetInitStatus(ctx, connect.NewRequest(&onboardingv1.GetFleetInitStatusRequest{}))
	if err != nil {
		return "", fmt.Errorf("get fleet init status: %w", err)
	}
	if initResp.Msg.GetStatus() == nil || !initResp.Msg.GetStatus().GetAdminCreated() {
		_, err := onboardingClient.CreateAdminLogin(ctx, connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: cfg.adminUsername,
			Password: cfg.adminPassword,
		}))
		if err != nil {
			return "", fmt.Errorf("create admin login: %w", err)
		}
	}

	authClient := authv1connect.NewAuthServiceClient(http.DefaultClient, cfg.apiURL)
	authResp, err := authClient.Authenticate(ctx, connect.NewRequest(&authv1.AuthenticateRequest{
		Username: cfg.adminUsername,
		Password: cfg.adminPassword,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			return "", fmt.Errorf("authenticate admin %q: %w. If this dev database already has an admin, rerun with FLEET_ADMIN_USERNAME and FLEET_ADMIN_PASSWORD set to that user's credentials, or reset the dev database before using the default admin credentials", cfg.adminUsername, err)
		}
		return "", fmt.Errorf("authenticate admin: %w", err)
	}
	rawCookie := authResp.Header().Get("Set-Cookie")
	if rawCookie == "" {
		return "", fmt.Errorf("authenticate admin: missing Set-Cookie header")
	}
	cookie, err := http.ParseSetCookie(rawCookie)
	if err != nil {
		return "", fmt.Errorf("parse session cookie: %w", err)
	}
	return fmt.Sprintf("%s=%s", cookie.Name, cookie.Value), nil
}
