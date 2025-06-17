package auth_test

import (
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/testutil"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	onboardingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
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

		// Verify response
		assert.NotEqual(t, "", authResp.Msg.Token, "expected userId in response, got nil")
		claims, err := testContext.ServiceProvider.TokenService.VerifyClientAuthJWT(authResp.Msg.Token)
		assert.NoError(t, err)
		assert.Equal(t, claims.ExpiresAt.Unix(), authResp.Msg.TokenExpiry, "expected token expiry to equal expires at")
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

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: "fizzbuzz",
			NewPassword:     "buzzbuzz",
		})
		updatePWReq.Header().Set("Authorization", "Bearer "+authResp.Msg.Token)
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

		// Verify response
		assert.NotEqual(t, "", authResp.Msg.Token, "expected userId in response, got nil")
		claims, err := testContext.ServiceProvider.TokenService.VerifyClientAuthJWT(authResp.Msg.Token)
		assert.NoError(t, err)
		assert.Equal(t, claims.ExpiresAt.Unix(), authResp.Msg.TokenExpiry, "expected token expiry to equal expires at")
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

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: adminPassword,
			NewPassword:     adminPassword,
		})
		updatePWReq.Header().Set("Authorization", "Bearer "+authResp.Msg.Token)
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

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: "catchmeifyoucan",
			NewPassword:     "buzzbuzz",
		})
		updatePWReq.Header().Set("Authorization", "Bearer "+authResp.Msg.Token)
		_, err = testContext.InfrastructureProvider.AuthClient.UpdatePassword(t.Context(), updatePWReq)
		assert.Error(t, err)
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
