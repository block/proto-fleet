package grpc_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	onboardingv1 "github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1/onboardingv1connect"

	authv1 "github.com/btc-mining/miner-firmware/fleet/generated/grpc/auth/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/auth/v1/authv1connect"

	"github.com/btc-mining/miner-firmware/fleet/internal/application"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/api/grpc"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db/dbtest"
)

func TestAuthServer_Authenticate(t *testing.T) {
	tokenSvc, _ := domain.NewTokenService(domain.AuthConfig{
		SecretKey:        "000000000000000000000000000000000000",
		ExpirationPeriod: time.Hour * 24,
	})
	authSvc := domain.NewAuthService(tokenSvc)

	t.Run("should authenticate successfully on valid credentials", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authUseCases := application.NewAuthUseCases(conn, authSvc)

		// Setup test server
		mux := http.NewServeMux()
		onboardingServer := grpc.NewOnboardingServer(authUseCases)
		mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboardingServer))

		authServer := grpc.NewAuthServer(authUseCases)
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
	})

	t.Run("should fail on invalid credentials", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authUseCases := application.NewAuthUseCases(conn, authSvc)

		// Setup test server
		mux := http.NewServeMux()
		onboardingServer := grpc.NewOnboardingServer(authUseCases)
		mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboardingServer))

		authServer := grpc.NewAuthServer(authUseCases)
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
