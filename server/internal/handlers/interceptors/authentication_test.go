package interceptors_test

import (
	"testing"
	"time"

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

	t.Run("should pass auth check when session is valid", func(t *testing.T) {
		// Arrange
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		testUser := databaseService.CreateSuperAdminUser()

		// Create a session for the user
		session, err := serviceProvider.SessionService.Create(t.Context(), testUser.DatabaseID, testUser.OrganizationID, "test-agent", "127.0.0.1")
		assert.NoError(t, err)

		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		// Set session cookie
		cookie := serviceProvider.SessionService.CreateCookie(session.SessionID)
		req.Header().Set("Cookie", cookie.String())

		// Act
		resp, err := infrastructureProvider.PingClient.Ping(t.Context(), req)

		// Assert
		assert.NoError(t, err)
		assert.Equal(t, "Hello", resp.Msg.Text)
	})

	t.Run("should fail auth check when session cookie is missing", func(t *testing.T) {
		// Arrange
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		// No cookie set

		// Act
		_, err := infrastructureProvider.PingClient.Ping(t.Context(), req)

		// Assert
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "session cookie required")
	})

	t.Run("should fail auth check when session is invalid", func(t *testing.T) {
		// Arrange
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infrastructureProvider := testutil.NewInfrastructureProvider(t, serviceProvider, allowList)

		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		// Set invalid session cookie
		invalidCookie := serviceProvider.SessionService.CreateCookie("invalid-session-id-that-does-not-exist")
		req.Header().Set("Cookie", invalidCookie.String())

		// Act
		_, err := infrastructureProvider.PingClient.Ping(t.Context(), req)

		// Assert
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "invalid session")
	})

	// This test documents that orphaned sessions (sessions without valid users) cannot exist
	// due to database foreign key constraints. The FK constraint on session.user_id ensures
	// that if a user is deleted, their sessions are also cleaned up, preventing this attack vector.
	t.Run("orphaned sessions are prevented by database FK constraints", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)

		testUser := databaseService.CreateSuperAdminUser()

		// Create a valid session for the user
		sess, err := serviceProvider.SessionService.Create(t.Context(), testUser.DatabaseID, testUser.OrganizationID, "test-agent", "127.0.0.1")
		assert.NoError(t, err, "Session creation should succeed")

		// Delete the user's session, user_organization, and user from the database
		// Order matters due to foreign key constraints
		_, err = databaseService.DB.ExecContext(t.Context(), "DELETE FROM session WHERE user_id = ?", testUser.DatabaseID)
		assert.NoError(t, err, "Session deletion should succeed")
		_, err = databaseService.DB.ExecContext(t.Context(), "DELETE FROM user_organization WHERE user_id = ?", testUser.DatabaseID)
		assert.NoError(t, err, "User organization deletion should succeed")
		_, err = databaseService.DB.ExecContext(t.Context(), "DELETE FROM user WHERE id = ?", testUser.DatabaseID)
		assert.NoError(t, err, "User deletion should succeed")

		// Attempt to re-insert the session (now orphaned - user no longer exists)
		// This should fail due to FK constraint, which is the security behavior we're verifying
		now := time.Now()
		expires := now.Add(time.Hour)
		_, err = databaseService.DB.ExecContext(t.Context(),
			"INSERT INTO session (session_id, user_id, organization_id, user_agent, ip_address, created_at, last_activity, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			sess.SessionID, testUser.DatabaseID, testUser.OrganizationID, "test-agent", "127.0.0.1", now, now, expires)

		// FK constraint prevents orphaned sessions - this is expected and good security
		assert.Error(t, err, "FK constraint should prevent creating orphaned sessions")
	})
}
