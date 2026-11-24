package testutil

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alecthomas/assert/v2"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
	apikeyHandlerPkg "github.com/block/proto-fleet/server/internal/handlers/apikey"
	"github.com/block/proto-fleet/server/internal/handlers/auth"
	"github.com/block/proto-fleet/server/internal/handlers/command"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/onboarding"
	"github.com/block/proto-fleet/server/internal/handlers/pairing"
	"github.com/block/proto-fleet/server/internal/handlers/ping"
)

type InfrastructureProvider struct {
	AuthClient       authv1connect.AuthServiceClient
	ApiKeyClient     apikeyv1connect.ApiKeyServiceClient
	OnboardingClient onboardingv1connect.OnboardingServiceClient
	PingClient       pingv1connect.PingServiceClient
	ServerURL        string
	testServer       *httptest.Server
}

type TestContext struct {
	DatabaseService        *DatabaseService
	ServiceProvider        *ServiceProvider
	InfrastructureProvider *InfrastructureProvider
	Config                 *Config
}

func NewInfrastructureProvider(t *testing.T, serviceProvider *ServiceProvider, authInterceptorAllowList []string) *InfrastructureProvider {
	interceptorsOption := connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), interceptors.NewAuthInterceptor(serviceProvider.SessionService, serviceProvider.UserStore, serviceProvider.UserStore, serviceProvider.ApiKeyService, serviceProvider.PermissionResolver, nil, authInterceptorAllowList, interceptors.SessionOnlyProcedures, nil))

	mux := http.NewServeMux()

	authHandler := auth.NewHandler(serviceProvider.AuthService)
	mux.Handle(authv1connect.NewAuthServiceHandler(authHandler, interceptorsOption))

	// nil fleet node services: no fleet node discovery/pairing fan-out in this test harness.
	pairingHandler := pairing.NewHandler(serviceProvider.PairingService, nil, nil)
	mux.Handle(pairingv1connect.NewPairingServiceHandler(pairingHandler, interceptorsOption))

	onboardingHandler := onboarding.NewHandler(serviceProvider.AuthService, serviceProvider.OnboardingService)
	mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboardingHandler, interceptorsOption))

	pingHandler := ping.Handler{}
	mux.Handle(pingv1connect.NewPingServiceHandler(pingHandler, interceptorsOption))

	commandHandler := command.NewHandler(serviceProvider.CommandService)
	mux.Handle(minercommandv1connect.NewMinerCommandServiceHandler(commandHandler, interceptorsOption))

	apiKeyHandler := apikeyHandlerPkg.NewHandler(serviceProvider.ApiKeyService)
	mux.Handle(apikeyv1connect.NewApiKeyServiceHandler(apiKeyHandler, interceptorsOption))

	testServer := httptest.NewServer(mux)

	authClient := authv1connect.NewAuthServiceClient(http.DefaultClient, testServer.URL)
	apiKeyClient := apikeyv1connect.NewApiKeyServiceClient(http.DefaultClient, testServer.URL)
	onboardingClient := onboardingv1connect.NewOnboardingServiceClient(http.DefaultClient, testServer.URL)
	pingClient := pingv1connect.NewPingServiceClient(http.DefaultClient, testServer.URL)

	provider := InfrastructureProvider{
		AuthClient:       authClient,
		ApiKeyClient:     apiKeyClient,
		OnboardingClient: onboardingClient,
		PingClient:       pingClient,
		ServerURL:        testServer.URL,
		testServer:       testServer,
	}

	t.Cleanup(provider.testServer.Close)

	return &provider
}

func InitializeDBServiceInfrastructure(t *testing.T) *TestContext {
	testConfig, err := GetTestConfig()
	assert.NoError(t, err, "error initializing test config")
	databaseService := NewDatabaseService(t, testConfig)
	serviceProvider := NewServiceProvider(t, databaseService.DB, testConfig)

	infrastructureProvider := NewInfrastructureProvider(t, serviceProvider, interceptors.UnauthenticatedProcedures)
	return &TestContext{DatabaseService: databaseService, ServiceProvider: serviceProvider, InfrastructureProvider: infrastructureProvider, Config: testConfig}
}
