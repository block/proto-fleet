package auth_test

import (
	"net/http"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	onboardingv1 "github.com/block/proto-fleet/server/generated/grpc/onboarding/v1"
)

func TestAuthServer_Authenticate(t *testing.T) {

	t.Run("should authenticate successfully on valid credentials", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)

		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "alice@example.com",
			Password: "fizzbuzz",
		})

		_, err := testContext.InfrastructureProvider.OnboardingClient.CreateAdminLogin(t.Context(), req)
		assert.NoError(t, err)

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: "alice@example.com",
			Password: "fizzbuzz",
		})

		authResp, err := testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		// Verify response - check user info and session expiry
		assert.True(t, authResp.Msg.UserInfo != nil, "expected user_info in response")
		assert.NotEqual(t, "", authResp.Msg.UserInfo.UserId, "expected user_id in response")
		assert.True(t, authResp.Msg.SessionExpiry > 0, "expected session_expiry to be set")

		// Verify Set-Cookie header is present
		setCookie := authResp.Header().Get("Set-Cookie")
		assert.NotEqual(t, "", setCookie, "expected Set-Cookie header in response")
	})

	t.Run("should fail on invalid credentials", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)

		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "alice@example.com",
			Password: "fizzbuzz",
		})

		_, err := testContext.InfrastructureProvider.OnboardingClient.CreateAdminLogin(t.Context(), req)
		assert.NoError(t, err)

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: "alice@example.com",
			Password: "buzzbuzz",
		})

		_, err = testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authReq)
		assert.Error(t, err)

	})
}

func TestAuthServer_UpdatePassword(t *testing.T) {

	t.Run("should update password to new password", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)

		adminUsername := "alice@example.com"
		adminPassword := "fizzbuzz"
		setupAuthClientFor(t, testContext.InfrastructureProvider, adminUsername, adminPassword)

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: adminUsername,
			Password: adminPassword,
		})

		authResp, err := testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		// Extract session cookie from response
		sessionCookie := extractSessionCookie(authResp.Header())
		assert.True(t, sessionCookie != nil, "expected session cookie in response")

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: "fizzbuzz",
			NewPassword:     "buzzbuzz",
		})
		// Use session cookie instead of Bearer token
		updatePWReq.Header().Set("Cookie", sessionCookie.String())
		_, err = testContext.InfrastructureProvider.AuthClient.UpdatePassword(t.Context(), updatePWReq)
		assert.NoError(t, err)

		_, err = testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authReq)
		assert.Error(t, err)

		authReq = connect.NewRequest(&authv1.AuthenticateRequest{
			Username: "alice@example.com",
			Password: "buzzbuzz",
		})
		authResp, err = testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		// Verify response - check user info and session expiry
		assert.True(t, authResp.Msg.UserInfo != nil, "expected user_info in response")
		assert.NotEqual(t, "", authResp.Msg.UserInfo.UserId, "expected user_id in response")
		assert.True(t, authResp.Msg.SessionExpiry > 0, "expected session_expiry to be set")
	})

	t.Run("should fail to update password when new password is same as current", func(t *testing.T) {
		adminUsername := "alice@example.com"
		adminPassword := "fizzbuzz"
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		setupAuthClientFor(t, testContext.InfrastructureProvider, adminUsername, adminPassword)

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: adminUsername,
			Password: adminPassword,
		})

		authResp, err := testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		// Extract session cookie from response
		sessionCookie := extractSessionCookie(authResp.Header())
		assert.True(t, sessionCookie != nil, "expected session cookie in response")

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: adminPassword,
			NewPassword:     adminPassword,
		})
		updatePWReq.Header().Set("Cookie", sessionCookie.String())
		_, err = testContext.InfrastructureProvider.AuthClient.UpdatePassword(t.Context(), updatePWReq)
		assert.Error(t, err)
	})

	t.Run("should fail to update password when current password does not match", func(t *testing.T) {
		adminUsername := "alice@example.com"
		adminPassword := "fizzbuzz"
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		setupAuthClientFor(t, testContext.InfrastructureProvider, adminUsername, adminPassword)

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: adminUsername,
			Password: adminPassword,
		})

		authResp, err := testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		// Extract session cookie from response
		sessionCookie := extractSessionCookie(authResp.Header())
		assert.True(t, sessionCookie != nil, "expected session cookie in response")

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: "catchmeifyoucan",
			NewPassword:     "buzzbuzz",
		})
		updatePWReq.Header().Set("Cookie", sessionCookie.String())
		_, err = testContext.InfrastructureProvider.AuthClient.UpdatePassword(t.Context(), updatePWReq)
		assert.Error(t, err)
	})
}

func TestAuthServer_SessionOnlyEndpoints(t *testing.T) {
	t.Run("should reject API key auth for user audit and list users", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		testUser := testContext.DatabaseService.CreateSuperAdminUser()

		fullKey, _, err := testContext.ServiceProvider.ApiKeyService.Create(
			t.Context(),
			testUser.DatabaseID,
			testUser.OrganizationID,
			"ext-id",
			testUser.Username,
			"test-key",
			nil,
		)
		assert.NoError(t, err)

		auditReq := connect.NewRequest(&authv1.GetUserAuditInfoRequest{})
		auditReq.Header().Set("Authorization", "Bearer "+fullKey)

		_, err = testContext.InfrastructureProvider.AuthClient.GetUserAuditInfo(t.Context(), auditReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

		listReq := connect.NewRequest(&authv1.ListUsersRequest{})
		listReq.Header().Set("Authorization", "Bearer "+fullKey)

		_, err = testContext.InfrastructureProvider.AuthClient.ListUsers(t.Context(), listReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("should reject ambiguous auth before session-only enforcement", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		testUser := testContext.DatabaseService.CreateSuperAdminUser()

		fullKey, _, err := testContext.ServiceProvider.ApiKeyService.Create(
			t.Context(),
			testUser.DatabaseID,
			testUser.OrganizationID,
			"ext-id",
			testUser.Username,
			"test-key",
			nil,
		)
		assert.NoError(t, err)

		sess, err := testContext.ServiceProvider.SessionService.Create(
			t.Context(),
			testUser.DatabaseID,
			testUser.OrganizationID,
			"test-agent",
			"127.0.0.1",
		)
		assert.NoError(t, err)

		auditReq := connect.NewRequest(&authv1.GetUserAuditInfoRequest{})
		auditReq.Header().Set("Authorization", "Bearer "+fullKey)
		auditReq.Header().Set("Cookie", testContext.ServiceProvider.SessionService.CreateCookie(sess.SessionID).String())

		_, err = testContext.InfrastructureProvider.AuthClient.GetUserAuditInfo(t.Context(), auditReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "ambiguous")
	})
}

func setupAuthClientFor(
	t *testing.T,
	infrastructureProvider *testutil.InfrastructureProvider,
	adminUsername,
	password string,
) {

	// Make request
	req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
		Username: adminUsername,
		Password: password,
	})

	_, err := infrastructureProvider.OnboardingClient.CreateAdminLogin(t.Context(), req)
	assert.NoError(t, err)
}

// extractSessionCookie parses the Set-Cookie header and returns the session cookie
func extractSessionCookie(header http.Header) *http.Cookie {
	setCookie := header.Get("Set-Cookie")
	if setCookie == "" {
		return nil
	}

	cookie, err := http.ParseSetCookie(setCookie)
	if err != nil {
		return nil
	}

	if cookie.Name != "fleet_session" {
		return nil
	}

	return cookie
}
