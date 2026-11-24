package gateway_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestBootstrapRateLimitsAllow100NodesBehindOneIP(t *testing.T) {
	// Arrange
	harness := newControlHarness(t)
	apiKeys := apikey.NewService(sqlstores.NewSQLApiKeyStore(harness.db), nil)
	store := sqlstores.NewSQLFleetNodeEnrollmentStore(harness.db)
	enroll := enrollment.NewService(store, apiKeys, sqlstores.NewSQLTransactor(harness.db), nil)
	authSvc := auth.NewService(sqlstores.NewSQLFleetNodeAuthStore(harness.db), store, apiKeys)
	proxy, err := middleware.NewClientIPMiddleware("127.0.0.1/32,::1/128")
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(
		gateway.NewHandler(enroll, authSvc, nil, harness.registry, nil), gateway.BootstrapRateLimitOption(),
		connect.WithInterceptors(interceptors.NewErrorMappingInterceptor())))
	// Every request crosses one trusted proxy with the same public client IP.
	srv := testutil.NewH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Header.Set("X-Forwarded-For", "192.0.2.1")
		proxy.Wrap(mux).ServeHTTP(w, req)
	}))
	// H2C uses an HTTP/2 transport, matching the Node bootstrap client.
	client, err := bootstrap.NewGatewayClient(srv.URL)
	require.NoError(t, err)

	type node struct {
		index     int
		code      string
		pendingID int64
		pub       ed25519.PublicKey
		priv      ed25519.PrivateKey
	}
	nodes := make([]node, 100)
	for i := range nodes {
		pub, priv, keyErr := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, keyErr)
		code, pendingID, _, codeErr := enroll.CreateCodeWithEnrollmentID(t.Context(), 1, 1, time.Hour)
		require.NoError(t, codeErr)
		nodes[i] = node{index: i, code: code, pendingID: pendingID, pub: pub, priv: priv}
	}
	registerAndAuthenticate := func(n node) error {
		registered, regErr := client.Register(t.Context(), connect.NewRequest(&pb.RegisterRequest{
			EnrollmentToken: n.code, Name: fmt.Sprintf("burst-node-%d", n.index), IdentityPubkey: n.pub,
			EncryptionPubkey: []byte("01234567890123456789012345678901"),
		}))
		if regErr != nil {
			return fmt.Errorf("register: %w", regErr)
		}
		key, _, confirmErr := enroll.ConfirmExpected(t.Context(), registered.Msg.GetFleetNodeId(), 1, n.pendingID, 1)
		if confirmErr != nil {
			return fmt.Errorf("confirm: %w", confirmErr)
		}
		begun, beginErr := client.BeginAuthHandshake(t.Context(), connect.NewRequest(&pb.BeginAuthHandshakeRequest{ApiKey: key, IdentityPubkey: n.pub}))
		if beginErr != nil {
			return fmt.Errorf("begin handshake: %w", beginErr)
		}
		completed, completeErr := client.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{
			Challenge: begun.Msg.GetChallenge(), Signature: ed25519.Sign(n.priv, begun.Msg.GetChallenge()),
		}))
		if completeErr != nil {
			return fmt.Errorf("complete handshake: %w", completeErr)
		}
		subject, resolveErr := authSvc.ResolveSession(t.Context(), completed.Msg.GetSessionToken())
		if resolveErr != nil {
			return fmt.Errorf("resolve session: %w", resolveErr)
		}
		if subject.FleetNodeID != registered.Msg.GetFleetNodeId() {
			return fmt.Errorf("session resolved to another node: %d", subject.FleetNodeID)
		}
		return nil
	}

	// Act
	var wg sync.WaitGroup
	results := make(chan error, len(nodes))
	for _, n := range nodes {
		wg.Go(func() { results <- registerAndAuthenticate(n) })
	}
	wg.Wait()
	close(results)

	// Assert
	for result := range results {
		require.NoError(t, result)
	}
}
