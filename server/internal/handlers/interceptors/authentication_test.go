package interceptors_test

import (
	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"testing"

	pingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
)

func TestAuthInterceptor(t *testing.T) {

	allowList := []string{
		pingv1connect.PingServiceEchoProcedure,
	}

	t.Run("should respect allow list", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		// Make request
		req := connect.NewRequest(&pingv1.EchoRequest{
			Text: "Hello",
		})

		resp, err := infrastructureProvider.PingClient.Echo(t.Context(), req)
		assert.NoError(t, err)

		// Verify response
		assert.Equal(t, "Hello", resp.Msg.Text)
	})

	t.Run("should fail auth when procedure not in allow list", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, []string{})

		// Make request
		req := connect.NewRequest(&pingv1.EchoRequest{
			Text: "Hello",
		})

		_, err := infrastructureProvider.PingClient.Echo(t.Context(), req)
		assert.Error(t, err)
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("should pass auth check when token is valid", func(t *testing.T) {
		// Setup test server
		databaseService := testutil.NewDatabaseService(t)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		// Make request
		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		jwt, _, err := serviceProvider.TokenService.GenerateClientAuthJWT(123, 1)
		assert.NoError(t, err)

		req.Header().Set(
			"Authorization",
			"Bearer "+jwt,
		)

		resp, err := infrastructureProvider.PingClient.Ping(t.Context(), req)
		assert.NoError(t, err)

		// Verify response
		assert.Equal(t, "Hello", resp.Msg.Text)
	})

	t.Run("should fail auth check when token is invalid", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		// Make request
		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		req.Header().Set(
			"Authorization",
			"Bearer hvhjvghjvjvgvcghjvjvgj",
		)

		_, err := infrastructureProvider.PingClient.Ping(t.Context(), req)

		// Verify response
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})
}
