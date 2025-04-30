package auth_test

import (
	authDomain "github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/auth"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/onboarding"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	onboardingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"

	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db/dbtest"
)

func TestAuthServer_Authenticate(t *testing.T) {
	tokenSvc, _ := token.NewService(token.Config{
		SecretKey:        "000000000000000000000000000000000000",
		ExpirationPeriod: time.Hour * 24,
	})

	t.Run("should authenticate successfully on valid credentials", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authSvc := authDomain.NewService(conn, tokenSvc)

		// Setup test server
		mux := http.NewServeMux()
		onboardingServer := onboarding.NewHandler(authSvc)
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

		// Setup test server
		mux := http.NewServeMux()
		onboardingServer := onboarding.NewHandler(authSvc)
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
