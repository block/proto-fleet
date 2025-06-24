package testutil

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alecthomas/assert/v2"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/integrationtesting"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/auth"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/command"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/interceptors"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/onboarding"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/ping"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type InfrastructureProvider struct {
	serviceProvider  *ServiceProvider
	AuthClient       authv1connect.AuthServiceClient
	PairingClient    pairingv1connect.PairingServiceClient
	OnboardingClient onboardingv1connect.OnboardingServiceClient
	PingClient       pingv1connect.PingServiceClient
	CommandClient    minercommandv1connect.MinerCommandServiceClient
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

	commandHandler := command.NewHandler(serviceProvider.CommandService)
	mux.Handle(minercommandv1connect.NewMinerCommandServiceHandler(commandHandler, interceptorsOption))

	testServer := httptest.NewServer(mux)

	authClient := authv1connect.NewAuthServiceClient(http.DefaultClient, testServer.URL)
	pairingClient := pairingv1connect.NewPairingServiceClient(http.DefaultClient, testServer.URL)
	onboardingClient := onboardingv1connect.NewOnboardingServiceClient(http.DefaultClient, testServer.URL)
	pingClient := pingv1connect.NewPingServiceClient(http.DefaultClient, testServer.URL)
	commandClient := minercommandv1connect.NewMinerCommandServiceClient(http.DefaultClient, testServer.URL)

	provider := InfrastructureProvider{
		serviceProvider:  serviceProvider,
		AuthClient:       authClient,
		PairingClient:    pairingClient,
		OnboardingClient: onboardingClient,
		PingClient:       pingClient,
		CommandClient:    commandClient,
		ServerURL:        testServer.URL,
		testServer:       testServer,
	}

	t.Cleanup(func() {
		provider.testServer.Close()
		provider.serviceProvider.ExecutionServiceCancel()
	})

	return &provider
}

func InitializeDBServiceInfrastructure(t *testing.T) *TestContext {
	testConfig, err := GetTestConfig()
	assert.NoError(t, err, "error initializing test config")
	databaseService := NewDatabaseService(t, testConfig)
	serviceProvider := NewServiceProvider(t, databaseService.DB, testConfig)

	infrastructureProvider := NewInfrastructureProvider(t, serviceProvider, interceptors.UnauthenticatedProcedures)
	return &TestContext{DatabaseService: databaseService, ServiceProvider: serviceProvider, InfrastructureProvider: infrastructureProvider}
}

// SetupMockMinerServer creates a test HTTP server that simulates a miner API
func SetupMockMinerServer(t *testing.T, callCounter *integrationtesting.MockMinerCallCounter) *httptest.Server {
	if callCounter == nil {
		callCounter = integrationtesting.NewMockMinerCallCounter()
	}

	mockHandler := integrationtesting.NewMockMinerHandler(t, callCounter)

	mux := http.NewServeMux()
	path, handler := miner_command_apiconnect.NewMinerCommandApiHandler(mockHandler)
	mux.Handle(path, handler)

	handler2c := h2c.NewHandler(mux, &http2.Server{})

	server := httptest.NewUnstartedServer(handler2c)
	server.EnableHTTP2 = true
	server.Start()
	t.Logf("Mock miner (h2c) server started at %s", server.URL)
	t.Cleanup(func() {
		server.Close()
	})
	return server
}
