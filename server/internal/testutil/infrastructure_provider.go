package testutil

import (
	"crypto/tls"
	"crypto/x509"
	"net"
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
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
	proto_client "github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
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
	Config                 *Config
}

func NewInfrastructureProvider(t *testing.T, serviceProvider *ServiceProvider, authInterceptorAllowList []string) *InfrastructureProvider {
	interceptorsOption := connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), interceptors.NewAuthInterceptor(serviceProvider.TokenService, serviceProvider.UserStore, authInterceptorAllowList))

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
	return &TestContext{DatabaseService: databaseService, ServiceProvider: serviceProvider, InfrastructureProvider: infrastructureProvider, Config: testConfig}
}

// SetupMockMinerServer creates a test HTTP server that simulates a miner API
func SetupMockMinerServer(t *testing.T, callCounter *integrationtesting.MockMinerCallCounter, useTLS bool) *httptest.Server {

	// Reset clients and set environment variable to skip TLS verification for the duration of the test
	proto_client.ResetClients()
	t.Setenv("SKIP_TLS_VERIFY", "true")

	if callCounter == nil {
		callCounter = integrationtesting.NewMockMinerCallCounter()
	}

	mockHandler := integrationtesting.NewMockMinerHandler(t, callCounter)

	mux := http.NewServeMux()
	path, handler := miner_command_apiconnect.NewMinerCommandApiHandler(mockHandler)
	authPath, authHandler := miner_system_apiconnect.NewMinerPairingApiHandler(mockHandler)
	systemPath, systemHandler := miner_system_apiconnect.NewMinerSystemApiHandler(mockHandler)
	mux.Handle(path, handler)
	mux.Handle(authPath, authHandler)
	mux.Handle(systemPath, systemHandler)

	var server *httptest.Server

	if useTLS {
		// For HTTPS, use the standard handler without h2c wrapping
		server = httptest.NewUnstartedServer(mux)
	} else {
		// For HTTP, use h2c handler for HTTP/2 over cleartext
		h2cHandler := h2c.NewHandler(mux, &http2.Server{})
		server = httptest.NewUnstartedServer(h2cHandler)
	}

	server.EnableHTTP2 = true

	// close the default listener
	server.Listener.Close()
	listener, err := net.Listen("tcp", "localhost:2121")
	if err != nil {
		t.Fatalf("Failed to listen on port 2121: %v", err)
	}
	server.Listener = listener

	if useTLS {
		server.StartTLS()
		trustTestCACert(t, server)
	} else {
		server.Start()
	}

	t.Logf("Mock miner server started at %s (TLS: %v)", server.URL, useTLS)
	t.Cleanup(func() {
		server.Close()
	})
	return server
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
