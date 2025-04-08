package grpc_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"
	"github.com/google/uuid"

	onboardingv1 "github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1/onboardingv1connect"

	"github.com/btc-mining/miner-firmware/fleet/internal/application"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/api/grpc"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db/dbtest"
)

func TestOnboardingServer_CreateAdminLogin(t *testing.T) {
	tokenSvc, _ := domain.NewTokenService(domain.AuthConfig{
		SecretKey:        "000000000000000000000000000000000000",
		ExpirationPeriod: time.Hour * 24,
	})
	authSvc := domain.NewAuthService(tokenSvc)

	t.Run("should create an admin user", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authUseCases := application.NewAuthUseCases(conn, authSvc)

		// Setup test server
		mux := http.NewServeMux()
		server := grpc.NewOnboardingServer(authUseCases)
		path, handler := onboardingv1connect.NewOnboardingServiceHandler(server)
		mux.Handle(path, handler)
		testServer := httptest.NewServer(mux)
		defer testServer.Close()

		// Create client
		client := onboardingv1connect.NewOnboardingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "alice@example.com",
			Password: "fizzbuzz",
		})

		resp, err := client.CreateAdminLogin(t.Context(), req)
		assert.NoError(t, err)

		// Verify response
		assert.NotEqual(t, "", resp.Msg.UserId, "expected userId in response, got nil")
		assert.NoError(t, uuid.Validate(resp.Msg.UserId), "expected userId to be a valid uuid")
	})

	t.Run("should fail on create an admin user when username not set", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authUseCases := application.NewAuthUseCases(conn, authSvc)

		// Setup test server
		mux := http.NewServeMux()
		server := grpc.NewOnboardingServer(authUseCases)
		path, handler := onboardingv1connect.NewOnboardingServiceHandler(server)
		mux.Handle(path, handler)
		testServer := httptest.NewServer(mux)
		defer testServer.Close()

		// Create client
		client := onboardingv1connect.NewOnboardingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "alice@example.com",
			Password: "",
		})

		_, err := client.CreateAdminLogin(t.Context(), req)
		assert.Error(t, err)

	})

	t.Run("should fail on create an admin user when password not set", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authUseCases := application.NewAuthUseCases(conn, authSvc)

		// Setup test server
		mux := http.NewServeMux()
		server := grpc.NewOnboardingServer(authUseCases)
		path, handler := onboardingv1connect.NewOnboardingServiceHandler(server)
		mux.Handle(path, handler)
		testServer := httptest.NewServer(mux)
		defer testServer.Close()

		// Create client
		client := onboardingv1connect.NewOnboardingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "",
			Password: "fizzbuzz",
		})

		_, err := client.CreateAdminLogin(t.Context(), req)
		assert.Error(t, err)
	})
}
