package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

func TestHeartbeatTransportTLS(t *testing.T) {
	// Isolate process-wide CA roots; use the production client with certificate
	// verification rather than changing host trust or injecting a transport.
	if os.Getenv("FLEET_HEARTBEAT_TLS_TEST") != "1" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHeartbeatTransportTLS$", "-test.v", "-test.timeout=20s")
		cmd.Env = append(os.Environ(), "FLEET_HEARTBEAT_TLS_TEST=1", "GODEBUG="+os.Getenv("GODEBUG")+",x509usefallbackroots=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		t.Logf("%s", output)
		return
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey: "fleet_known_key", identityPub: pub, challenge: []byte("local-transport-challenge"),
		sessionToken: "session-2", expectedSessionToken: "session-2", sessionExpiresAt: time.Now().Add(24 * time.Hour),
	}
	type attempt struct {
		auth    string
		timeout int64
		msg     *pb.UploadHeartbeatRequest
	}
	var mu sync.Mutex
	var attempts []attempt
	fake.heartbeatResponder = func(call int, req *connect.Request[pb.UploadHeartbeatRequest]) error {
		timeout, err := strconv.ParseInt(req.Header().Get("Connect-Timeout-Ms"), 10, 64)
		assert.NoError(t, err, "real Connect transport must carry the heartbeat deadline")
		mu.Lock()
		attempts = append(attempts, attempt{req.Header().Get("Authorization"), timeout, proto.CloneOf(req.Msg)})
		mu.Unlock()
		if call == 1 || call == 3 {
			return connect.NewError(connect.CodeUnavailable, errors.New("temporary failure"))
		}
		return nil
	}
	_, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, 2, req.ProtoMajor)
		assert.NotNil(t, req.TLS)
		assert.Equal(t, "h2", req.TLS.NegotiatedProtocol)
		handler.ServeHTTP(w, req)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	x509.SetFallbackRoots(pool)
	dir := t.TempDir()
	state := &bootstrap.State{
		ServerURL: server.URL, FleetNodeID: 42, APIKey: "fleet_known_key",
		IdentityPublicKeyHex: hex.EncodeToString(pub), IdentityPrivateKeyHex: hex.EncodeToString(priv),
		SessionToken: "session-1", SessionExpiresAt: time.Now().Add(24 * time.Hour),
	}
	cmd := &RunCmd{now: time.Now}
	setRunState(cmd, state, bootstrap.StatePath(dir))
	client, err := bootstrap.NewAuthenticatedGatewayClient(server.URL, func() string { return cmd.stateSnapshot().SessionToken })
	require.NoError(t, err)

	t.Run("retry then refresh preserves authenticated metadata", func(t *testing.T) {
		require.NoError(t, cmd.tick(t.Context(), client, discardLogger(t)))
		mu.Lock()
		got := append([]attempt(nil), attempts...)
		mu.Unlock()
		require.Len(t, got, 4)
		assert.Equal(t, []string{"Bearer session-1", "Bearer session-1", "Bearer session-2", "Bearer session-2"}, []string{got[0].auth, got[1].auth, got[2].auth, got[3].auth})
		for _, pair := range [][2]int{{0, 1}, {2, 3}} {
			assert.True(t, proto.Equal(got[pair[0]].msg, got[pair[1]].msg), "retry must preserve the full metadata message")
			assert.LessOrEqual(t, got[pair[0]].timeout, int64(30_000))
			assert.Less(t, got[pair[1]].timeout, got[pair[0]].timeout, "retry must consume the original budget")
		}
		assert.False(t, got[2].msg.GetSentAt().AsTime().Before(got[0].msg.GetSentAt().AsTime()))
		assert.Equal(t, 1, fake.completeAuthCount())
		persisted, exists, err := bootstrap.LoadState(bootstrap.StatePath(dir))
		require.NoError(t, err)
		require.True(t, exists)
		assert.Equal(t, "session-2", persisted.SessionToken)
		assert.Equal(t, state.APIKey, persisted.APIKey)
		assert.Equal(t, state.IdentityPrivateKeyHex, persisted.IdentityPrivateKeyHex)
	})

	t.Run("persistent Unavailable is capped at three wire requests", func(t *testing.T) {
		fake.heartbeatMu.Lock()
		fake.heartbeatResponder = func(_ int, req *connect.Request[pb.UploadHeartbeatRequest]) error {
			mu.Lock()
			attempts = append(attempts, attempt{auth: req.Header().Get("Authorization"), msg: proto.CloneOf(req.Msg)})
			mu.Unlock()
			return connect.NewError(connect.CodeUnavailable, errors.New("still unavailable"))
		}
		fake.heartbeatMu.Unlock()
		mu.Lock()
		attempts = nil
		mu.Unlock()
		err := cmd.sendHeartbeat(t.Context(), client, discardLogger(t))
		require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
		mu.Lock()
		defer mu.Unlock()
		require.Len(t, attempts, 3)
		for _, got := range attempts {
			assert.Equal(t, "Bearer session-2", got.auth)
			assert.True(t, proto.Equal(attempts[0].msg, got.msg))
		}
	})

	t.Run("cancellation interrupts wire retry backoff", func(t *testing.T) {
		mu.Lock()
		attempts = nil
		mu.Unlock()
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		err := cmd.sendHeartbeat(ctx, client, discardLogger(t))
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err), "retain the last transport cause")
		mu.Lock()
		defer mu.Unlock()
		require.Len(t, attempts, 1)
	})
}
