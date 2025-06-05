package testutil

import (
	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/auth"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/interceptors"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/onboarding"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/ping"
	"net/http"
	"net/http/httptest"
	"testing"
)

type InfrastructureProvider struct {
	serviceProvider  *ServiceProvider
	AuthClient       authv1connect.AuthServiceClient
	PairingClient    pairingv1connect.PairingServiceClient
	OnboardingClient onboardingv1connect.OnboardingServiceClient
	PingClient       pingv1connect.PingServiceClient
	ServerURL        string
	testServer       *httptest.Server
}

type TestContext struct {
	DatabaseService        *DatabaseService
	ServiceProvider        *ServiceProvider
	InfrastructureProvider *InfrastructureProvider
}

func NewInfrastructureProvider(t *testing.T, serviceProvider *ServiceProvider, authInterceptorAllowList []string) *InfrastructureProvider {
	interceptorsOption := connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), interceptors.NewAuthInterceptor(serviceProvider.TokenService, authInterceptorAllowList))

	mux := http.NewServeMux()

	authHandler := auth.NewHandler(serviceProvider.AuthService)
	mux.Handle(authv1connect.NewAuthServiceHandler(authHandler, interceptorsOption))

	pairingHandler := pairing.NewHandler(serviceProvider.PairingService)
	mux.Handle(pairingv1connect.NewPairingServiceHandler(pairingHandler, interceptorsOption))

	onboardingHandler := onboarding.NewHandler(serviceProvider.AuthService, serviceProvider.OnboardingService)
	mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboardingHandler, interceptorsOption))

	pingHandler := ping.Handler{}
	mux.Handle(pingv1connect.NewPingServiceHandler(pingHandler, interceptorsOption))

	testServer := httptest.NewServer(mux)

	authClient := authv1connect.NewAuthServiceClient(http.DefaultClient, testServer.URL)
	pairingClient := pairingv1connect.NewPairingServiceClient(http.DefaultClient, testServer.URL)
	onboardingClient := onboardingv1connect.NewOnboardingServiceClient(http.DefaultClient, testServer.URL)
	pingClient := pingv1connect.NewPingServiceClient(http.DefaultClient, testServer.URL)

	provider := InfrastructureProvider{
		serviceProvider:  serviceProvider,
		AuthClient:       authClient,
		PairingClient:    pairingClient,
		OnboardingClient: onboardingClient,
		PingClient:       pingClient,
		ServerURL:        testServer.URL,
		testServer:       testServer,
	}

	t.Cleanup(func() {
		provider.testServer.Close()
	})

	return &provider
}

func InitializeDBServiceInfrastructure(t *testing.T) *TestContext {
	databaseService := NewDatabaseService(t)
	serviceProvider := NewServiceProvider(t, databaseService.DB)

	infrastructureProvider := NewInfrastructureProvider(t, serviceProvider, interceptors.UnauthenticatedProcedures)
	return &TestContext{DatabaseService: databaseService, ServiceProvider: serviceProvider, InfrastructureProvider: infrastructureProvider}
}
