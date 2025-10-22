package interceptors_test

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"

	pingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
)

func TestAuthInterceptor(t *testing.T) {
	testConfig, err := testutil.GetTestConfig()
	assert.NoError(t, err, "error initializing test config")

	allowList := []string{
		pingv1connect.PingServiceEchoProcedure,
	}

	t.Run("should respect allow list", func(t *testing.T) {
		// Arrange
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		req := connect.NewRequest(&pingv1.EchoRequest{
			Text: "Hello",
		})

		// Act
		resp, err := infrastructureProvider.PingClient.Echo(t.Context(), req)

		// Assert
		assert.NoError(t, err)
		assert.Equal(t, "Hello", resp.Msg.Text)
	})

	t.Run("should fail auth when procedure not in allow list", func(t *testing.T) {
		// Arrange
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, []string{})

		req := connect.NewRequest(&pingv1.EchoRequest{
			Text: "Hello",
		})

		// Act
		_, err := infrastructureProvider.PingClient.Echo(t.Context(), req)

		// Assert
		assert.Error(t, err)
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("should pass auth check when token is valid", func(t *testing.T) {
		// Arrange
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		testUser := databaseService.CreateSuperAdminUser()

		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		jwt, _, err := serviceProvider.TokenService.GenerateClientAuthJWT(testUser.DatabaseID, testUser.OrganizationID)
		assert.NoError(t, err)

		req.Header().Set(
			"Authorization",
			"Bearer "+jwt,
		)

		// Act
		resp, err := infrastructureProvider.PingClient.Ping(t.Context(), req)

		// Assert
		assert.NoError(t, err)
		assert.Equal(t, "Hello", resp.Msg.Text)
	})

	t.Run("should fail auth check when token is invalid", func(t *testing.T) {
		// Arrange
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		req.Header().Set(
			"Authorization",
			"Bearer hvhjvghjvjvgvcghjvjvgj",
		)

		// Act
		_, err := infrastructureProvider.PingClient.Ping(t.Context(), req)

		// Assert
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("should fail auth check when user does not exist", func(t *testing.T) {
		// Arrange
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		nonExistentUserID := int64(999999)
		nonExistentOrgID := int64(999999)

		jwt, _, err := serviceProvider.TokenService.GenerateClientAuthJWT(nonExistentUserID, nonExistentOrgID)
		assert.NoError(t, err, "JWT generation should succeed even for non-existent user")

		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		req.Header().Set(
			"Authorization",
			"Bearer "+jwt,
		)

		// Act
		_, err = infrastructureProvider.PingClient.Ping(t.Context(), req)

		// Assert
		assert.Error(t, err, "Authentication should fail for non-existent user")
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "User with id 999999 not found")
	})
}
