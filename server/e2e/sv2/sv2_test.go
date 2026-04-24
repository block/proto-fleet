//go:build e2e

// Package sv2 holds end-to-end tests for the Stratum V2 pool-assignment
// flow. Lives in its own subpackage so it compiles cleanly regardless of
// the state of server/e2e/plugin_integration_test.go, which currently
// references generated-proto symbols that were renamed on a separate
// cleanup track — keeping SV2 e2e isolated means the SV2 test still
// runs in CI while that file is being fixed.
//
// Invocation: launched inside the same `just rebuild-all` window the
// rest of the server/e2e suite uses. Prerequisites checked in
// TestMain — a missing fleet-api (port 4000 closed) skips the suite
// rather than failing, so local runs without the stack don't look
// broken.
package sv2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	onboardingv1 "github.com/block/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	pairingv1 "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	poolsv1 "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
)

const (
	fleetAPIURL    = "http://localhost:4000"
	protoSimIP     = "127.0.0.1"
	protoSimPort   = "8080"
	testUsername   = "e2e-sv2-admin"
	testPassword   = "e2e-sv2-password"
	requestTimeout = 15 * time.Second
)

// TestMain skips the suite when the Fleet API isn't reachable so
// developers running `go test ./...` without a live stack don't see
// red output that's unrelated to their change. CI drives this under
// `just rebuild-all` where the stack is always running.
func TestMain(m *testing.M) {
	if !fleetAPIReachable() {
		fmt.Println("fleet-api at", fleetAPIURL, "not reachable — skipping SV2 e2e suite")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fleetAPIReachable() bool {
	conn, err := net.DialTimeout("tcp", "localhost:4000", 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// TestSV2PoolAssignmentFlow exercises the full HTTP RPC path for SV2
// pool assignment: bootstrap auth, discover/pair a proto-sim device,
// create an SV1 pool (passthrough), create an SV2 pool
// (SLOT_WARNING_SV2_NOT_SUPPORTED because proxy is off by default),
// and confirm UpdateMiningPools rejects the SV2 assignment with
// FAILED_PRECONDITION.
func TestSV2PoolAssignmentFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx := context.Background()
	client := newHTTPClient(t)

	onboarding := onboardingv1connect.NewOnboardingServiceClient(client, fleetAPIURL)
	_, _ = onboarding.CreateAdminLogin(ctx, connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
		Username: testUsername,
		Password: testPassword,
	})) // idempotent — re-runs against the same stack return a "already exists" error we intentionally swallow

	// Authenticate and let the cookie jar pick up the session. Every
	// subsequent call on the same http.Client replays the cookie.
	authClient := authv1connect.NewAuthServiceClient(client, fleetAPIURL)
	_, err := authClient.Authenticate(ctx, connect.NewRequest(&authv1.AuthenticateRequest{
		Username: testUsername,
		Password: testPassword,
	}))
	require.NoError(t, err, "authentication must succeed before pool RPCs are exercised")

	pairing := pairingv1connect.NewPairingServiceClient(client, fleetAPIURL)
	devices := discoverProtoSim(t, ctx, pairing)
	require.Len(t, devices, 1, "should discover exactly one proto-sim")
	deviceID := devices[0].GetDeviceIdentifier()
	require.NotEmpty(t, deviceID)
	require.NoError(t, pairProtoSim(ctx, pairing, deviceID))

	pools := poolsv1connect.NewPoolsServiceClient(client, fleetAPIURL)
	cmd := minercommandv1connect.NewMinerCommandServiceClient(client, fleetAPIURL)

	t.Run("SV1PoolPreviewsAsPassthrough", func(t *testing.T) {
		id := createPool(t, ctx, pools, &poolsv1.PoolConfig{
			PoolName: "e2e-sv2-sv1-pool",
			Url:      "stratum+tcp://pool.e2e.example.com:3333",
			Username: "sv1-worker",
			Password: wrapperspb.String("x"),
		})
		t.Cleanup(func() { _ = deletePool(ctx, pools, id) })

		previews := previewSinglePoolAssignment(t, ctx, cmd, deviceID, id)
		require.Len(t, previews, 1)
		d := previews[0]
		require.Equal(t, deviceID, d.GetDeviceIdentifier())
		assert.Equal(t, minercommandv1.DeviceWarning_DEVICE_WARNING_UNSPECIFIED, d.GetDeviceWarning())
		require.Len(t, d.GetSlots(), 1)
		assert.Equal(t, minercommandv1.RewriteReason_REWRITE_REASON_PASSTHROUGH, d.GetSlots()[0].GetRewriteReason())
		assert.Equal(t, minercommandv1.SlotWarning_SLOT_WARNING_UNSPECIFIED, d.GetSlots()[0].GetWarning())
	})

	t.Run("SV2PoolPreviewSurfacesSlotWarning", func(t *testing.T) {
		id := createPool(t, ctx, pools, &poolsv1.PoolConfig{
			PoolName: "e2e-sv2-sv2-pool",
			Url:      "stratum2+tcp://pool.e2e.example.com:34254",
			Username: "sv2-worker",
			Password: wrapperspb.String("x"),
		})
		t.Cleanup(func() { _ = deletePool(ctx, pools, id) })

		previews := previewSinglePoolAssignment(t, ctx, cmd, deviceID, id)
		require.Len(t, previews, 1)
		d := previews[0]
		require.Len(t, d.GetSlots(), 1)
		assert.Equal(t, minercommandv1.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED, d.GetSlots()[0].GetWarning(),
			"SV2 pool assigned to SV1-only device without proxy must surface SLOT_WARNING_SV2_NOT_SUPPORTED")
		assert.Equal(t, minercommandv1.RewriteReason_REWRITE_REASON_UNSPECIFIED, d.GetSlots()[0].GetRewriteReason(),
			"rejected slots carry UNSPECIFIED reason — there's no URL the commit path would push")
	})

	t.Run("SV2CommitRejectsSynchronously", func(t *testing.T) {
		id := createPool(t, ctx, pools, &poolsv1.PoolConfig{
			PoolName: "e2e-sv2-commit-pool",
			Url:      "stratum2+tcp://pool.e2e.example.com:34254",
			Username: "sv2-worker",
			Password: wrapperspb.String("x"),
		})
		t.Cleanup(func() { _ = deletePool(ctx, pools, id) })

		req := connect.NewRequest(&minercommandv1.UpdateMiningPoolsRequest{
			DeviceSelector: deviceSelector(deviceID),
			DefaultPool: &minercommandv1.PoolSlotConfig{
				PoolSource: &minercommandv1.PoolSlotConfig_PoolId{PoolId: id},
			},
			UserUsername: testUsername,
			UserPassword: testPassword,
		})
		_, err := cmd.UpdateMiningPools(ctx, req)
		require.Error(t, err, "commit must reject synchronously, not enqueue a doomed batch")

		var connectErr *connect.Error
		require.True(t, errors.As(err, &connectErr), "expected connect.Error; got %T: %v", err, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code(),
			"preflight mismatch maps to FAILED_PRECONDITION per the plan")
	})
}

func newHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &http.Client{Timeout: requestTimeout, Jar: jar}
}

func discoverProtoSim(t *testing.T, ctx context.Context, client pairingv1connect.PairingServiceClient) []*pairingv1.Device {
	t.Helper()
	req := connect.NewRequest(&pairingv1.DiscoverRequest{
		Mode: &pairingv1.DiscoverRequest_IpList{
			IpList: &pairingv1.IPListModeRequest{
				IpAddresses: []string{protoSimIP},
				Ports:       []string{protoSimPort},
			},
		},
	})
	stream, err := client.Discover(ctx, req)
	require.NoError(t, err)
	var devices []*pairingv1.Device
	for stream.Receive() {
		devices = append(devices, stream.Msg().GetDevices()...)
	}
	require.NoError(t, stream.Err())
	return devices
}

func pairProtoSim(ctx context.Context, client pairingv1connect.PairingServiceClient, deviceID string) error {
	_, err := client.Pair(ctx, connect.NewRequest(&pairingv1.PairRequest{
		DeviceSelector: deviceSelector(deviceID),
	}))
	// Re-pair on an already-paired device returns a non-fatal error — we
	// treat that as success so the test is idempotent across rebuilds.
	if err != nil {
		var ce *connect.Error
		if errors.As(err, &ce) && ce.Code() == connect.CodeAlreadyExists {
			return nil
		}
		return err
	}
	return nil
}

func createPool(t *testing.T, ctx context.Context, client poolsv1connect.PoolsServiceClient, config *poolsv1.PoolConfig) int64 {
	t.Helper()
	resp, err := client.CreatePool(ctx, connect.NewRequest(&poolsv1.CreatePoolRequest{PoolConfig: config}))
	require.NoError(t, err, "CreatePool should succeed")
	require.NotNil(t, resp.Msg.GetPool())
	return resp.Msg.GetPool().GetPoolId()
}

func deletePool(ctx context.Context, client poolsv1connect.PoolsServiceClient, id int64) error {
	_, err := client.DeletePool(ctx, connect.NewRequest(&poolsv1.DeletePoolRequest{PoolId: id}))
	return err
}

func previewSinglePoolAssignment(
	t *testing.T,
	ctx context.Context,
	client minercommandv1connect.MinerCommandServiceClient,
	deviceID string,
	poolID int64,
) []*minercommandv1.DevicePoolPreview {
	t.Helper()
	resp, err := client.PreviewMiningPoolAssignment(ctx, connect.NewRequest(&minercommandv1.PreviewMiningPoolAssignmentRequest{
		DeviceSelector: deviceSelector(deviceID),
		DefaultPool: &minercommandv1.PoolSlotConfig{
			PoolSource: &minercommandv1.PoolSlotConfig_PoolId{PoolId: poolID},
		},
	}))
	require.NoError(t, err)
	return resp.Msg.GetPreviews()
}

func deviceSelector(deviceID string) *minercommandv1.DeviceSelector {
	return &minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonv1.DeviceIdentifierList{
				DeviceIdentifiers: []string{deviceID},
			},
		},
	}
}
