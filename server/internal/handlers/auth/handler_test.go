package auth_test

import (
	"database/sql"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/middleware"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/server"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authDomain "github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	onboardingDomain "github.com/btc-mining/proto-fleet/server/internal/domain/onboarding"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/auth"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/onboarding"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	onboardingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"

	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db/dbtest"
)

var tokenSvc, _ = token.NewService(token.Config{
	SecretKey:        "000000000000000000000000000000000000",
	ExpirationPeriod: time.Hour * 24,
})

func TestAuthServer_Authenticate(t *testing.T) {

	t.Run("should authenticate successfully on valid credentials", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authSvc := authDomain.NewService(conn, tokenSvc)
		onboardingSvc := onboardingDomain.NewService(conn)

		// Setup test server
		mux := http.NewServeMux()
		onboardingServer := onboarding.NewHandler(authSvc, onboardingSvc)
		mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboardingServer))

		authServer := auth.NewHandler(authSvc)
		mux.Handle(authv1connect.NewAuthServiceHandler(authServer))

		testServer := httptest.NewServer(mux)
		defer testServer.Close()

		// Create clients
		onboardingClient := onboardingv1connect.NewOnboardingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		authClient := authv1connect.NewAuthServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "alice@example.com",
			Password: "fizzbuzz",
		})

		_, err := onboardingClient.CreateAdminLogin(t.Context(), req)
		assert.NoError(t, err)

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: "alice@example.com",
			Password: "fizzbuzz",
		})

		authResp, err := authClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		// Verify response
		assert.NotEqual(t, "", authResp.Msg.Token, "expected userId in response, got nil")
		claims, err := tokenSvc.VerifyJWT(authResp.Msg.Token)
		assert.NoError(t, err)
		assert.Equal(t, claims.ExpiresAt.Unix(), authResp.Msg.TokenExpiry, "expected token expiry to equal expires at")
	})

	t.Run("should fail on invalid credentials", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authSvc := authDomain.NewService(conn, tokenSvc)
		onboardingSvc := onboardingDomain.NewService(conn)

		// Setup test server
		mux := http.NewServeMux()
		onboardingServer := onboarding.NewHandler(authSvc, onboardingSvc)
		mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboardingServer))

		authServer := auth.NewHandler(authSvc)
		mux.Handle(authv1connect.NewAuthServiceHandler(authServer))

		testServer := httptest.NewServer(mux)
		defer testServer.Close()

		// Create clients
		onboardingClient := onboardingv1connect.NewOnboardingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		authClient := authv1connect.NewAuthServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "alice@example.com",
			Password: "fizzbuzz",
		})

		_, err := onboardingClient.CreateAdminLogin(t.Context(), req)
		assert.NoError(t, err)

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: "alice@example.com",
			Password: "buzzbuzz",
		})

		_, err = authClient.Authenticate(t.Context(), authReq)
		assert.Error(t, err)

	})
}

func TestAuthServer_UpdatePassword(t *testing.T) {

	t.Run("should update password to new password", func(t *testing.T) {
		adminUsername := "alice@example.com"
		adminPassword := "fizzbuzz"
		conn := dbtest.GetTestDB(t)
		testServer, authClient := setupAuthClientFor(conn, t, adminUsername, adminPassword)
		defer testServer.Close()

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: adminUsername,
			Password: adminPassword,
		})

		authResp, err := authClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: "fizzbuzz",
			NewPassword:     "buzzbuzz",
		})
		updatePWReq.Header().Set("Authorization", "Bearer "+authResp.Msg.Token)
		_, err = authClient.UpdatePassword(t.Context(), updatePWReq)
		assert.NoError(t, err)

		_, err = authClient.Authenticate(t.Context(), authReq)
		assert.Error(t, err)

		authReq = connect.NewRequest(&authv1.AuthenticateRequest{
			Username: "alice@example.com",
			Password: "buzzbuzz",
		})
		authResp, err = authClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		// Verify response
		assert.NotEqual(t, "", authResp.Msg.Token, "expected userId in response, got nil")
		claims, err := tokenSvc.VerifyJWT(authResp.Msg.Token)
		assert.NoError(t, err)
		assert.Equal(t, claims.ExpiresAt.Unix(), authResp.Msg.TokenExpiry, "expected token expiry to equal expires at")
	})

	t.Run("should fail to update password when new password is same as current", func(t *testing.T) {
		adminUsername := "alice@example.com"
		adminPassword := "fizzbuzz"
		conn := dbtest.GetTestDB(t)
		testServer, authClient := setupAuthClientFor(conn, t, adminUsername, adminPassword)
		defer testServer.Close()

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: adminUsername,
			Password: adminPassword,
		})

		authResp, err := authClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: adminPassword,
			NewPassword:     adminPassword,
		})
		updatePWReq.Header().Set("Authorization", "Bearer "+authResp.Msg.Token)
		_, err = authClient.UpdatePassword(t.Context(), updatePWReq)
		assert.Error(t, err)
	})

	t.Run("should fail to update password when current password does not match", func(t *testing.T) {
		adminUsername := "alice@example.com"
		adminPassword := "fizzbuzz"
		conn := dbtest.GetTestDB(t)
		testServer, authClient := setupAuthClientFor(conn, t, adminUsername, adminPassword)
		defer testServer.Close()

		authReq := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: adminUsername,
			Password: adminPassword,
		})

		authResp, err := authClient.Authenticate(t.Context(), authReq)
		assert.NoError(t, err)

		updatePWReq := connect.NewRequest(&authv1.UpdatePasswordRequest{
			CurrentPassword: "catchmeifyoucan",
			NewPassword:     "buzzbuzz",
		})
		updatePWReq.Header().Set("Authorization", "Bearer "+authResp.Msg.Token)
		_, err = authClient.UpdatePassword(t.Context(), updatePWReq)
		assert.Error(t, err)
	})
}

func setupAuthClientFor(conn *sql.DB, t *testing.T, adminUsername, password string) (*httptest.Server, authv1connect.AuthServiceClient) {

	// Setup dependencies
	authSvc := authDomain.NewService(conn, tokenSvc)
	onboardingSvc := onboardingDomain.NewService(conn)

	// Setup test server
	mux := http.NewServeMux()
	onboardingServer := onboarding.NewHandler(authSvc, onboardingSvc)
	mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboardingServer))

	authServer := auth.NewHandler(authSvc)
	mux.Handle(authv1connect.NewAuthServiceHandler(authServer))

	// TODO refactor server command or introduce helper to make test server setup easier
	middlewares := []server.Middleware{
		middleware.NewAuthMiddleware(tokenSvc, []string{
			authv1connect.AuthServiceAuthenticateProcedure,
			onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
		}),
	}

	var handler http.Handler = mux
	for _, m := range middlewares {
		handler = m.Wrap(handler)
	}

	testServer := httptest.NewServer(handler)

	// Create clients
	onboardingClient := onboardingv1connect.NewOnboardingServiceClient(
		http.DefaultClient,
		testServer.URL,
	)

	authClient := authv1connect.NewAuthServiceClient(
		http.DefaultClient,
		testServer.URL,
	)

	// Make request
	req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
		Username: adminUsername,
		Password: password,
	})

	_, err := onboardingClient.CreateAdminLogin(t.Context(), req)
	assert.NoError(t, err)

	return testServer, authClient
}
