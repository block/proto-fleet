package testutil

import (
	"crypto/tls"
	"crypto/x509"
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
	"github.com/block/proto-fleet/server/generated/grpc/telemetry/v1/telemetryv1connect"
	apikeyHandlerPkg "github.com/block/proto-fleet/server/internal/handlers/apikey"
	"github.com/block/proto-fleet/server/internal/handlers/auth"
	"github.com/block/proto-fleet/server/internal/handlers/command"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/onboarding"
	"github.com/block/proto-fleet/server/internal/handlers/pairing"
	"github.com/block/proto-fleet/server/internal/handlers/ping"
)

type InfrastructureProvider struct {
	serviceProvider  *ServiceProvider
	AuthClient       authv1connect.AuthServiceClient
	ApiKeyClient     apikeyv1connect.ApiKeyServiceClient
	PairingClient    pairingv1connect.PairingServiceClient
	OnboardingClient onboardingv1connect.OnboardingServiceClient
	PingClient       pingv1connect.PingServiceClient
	CommandClient    minercommandv1connect.MinerCommandServiceClient
	TelemetryClient  telemetryv1connect.TelemetryServiceClient
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
	interceptorsOption := connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), interceptors.NewAuthInterceptor(serviceProvider.SessionService, serviceProvider.UserStore, serviceProvider.UserStore, serviceProvider.ApiKeyService, authInterceptorAllowList, interceptors.SessionOnlyProcedures))

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

	apiKeyHandler := apikeyHandlerPkg.NewHandler(serviceProvider.ApiKeyService)
	mux.Handle(apikeyv1connect.NewApiKeyServiceHandler(apiKeyHandler, interceptorsOption))

	testServer := httptest.NewServer(mux)

	authClient := authv1connect.NewAuthServiceClient(http.DefaultClient, testServer.URL)
	apiKeyClient := apikeyv1connect.NewApiKeyServiceClient(http.DefaultClient, testServer.URL)
	pairingClient := pairingv1connect.NewPairingServiceClient(http.DefaultClient, testServer.URL)
	onboardingClient := onboardingv1connect.NewOnboardingServiceClient(http.DefaultClient, testServer.URL)
	pingClient := pingv1connect.NewPingServiceClient(http.DefaultClient, testServer.URL)
	commandClient := minercommandv1connect.NewMinerCommandServiceClient(http.DefaultClient, testServer.URL)

	provider := InfrastructureProvider{
		serviceProvider:  serviceProvider,
		AuthClient:       authClient,
		ApiKeyClient:     apiKeyClient,
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
	return &TestContext{DatabaseService: databaseService, ServiceProvider: serviceProvider, InfrastructureProvider: infrastructureProvider, Config: testConfig}
}

// SetupMockMinerServer is deprecated and should not be used.
// TODO: SetupMockMinerServer should be reimplemented using plugin-based test infrastructure
// Deprecated: This function was removed with legacy proto implementation. Use plugin-based testing instead.
func SetupMockMinerServer(t *testing.T, _ interface{}, _ bool, _ ...int) *httptest.Server {
	t.Skip("SetupMockMinerServer removed with legacy proto implementation - needs rewrite with plugin infrastructure")
	return nil
}

func trustTestCACert(t *testing.T, server *httptest.Server) {
	certDER := server.TLS.Certificates[0].Certificate[0]
	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parsing test server cert: %v", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	pool.AddCert(leaf)

	originalTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("expected http.DefaultTransport to be *http.Transport, got %T", http.DefaultTransport)
	}

	testTransport := originalTransport.Clone()
	testTransport.TLSClientConfig = &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}

	http.DefaultClient.Transport = testTransport

	// Save the original transport to restore after the test
	t.Cleanup(func() {
		http.DefaultClient.Transport = originalTransport
	})
}
