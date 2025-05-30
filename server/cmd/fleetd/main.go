package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/minerclient"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"github.com/alecthomas/kong"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	authDomain "github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	commandDomain "github.com/btc-mining/proto-fleet/server/internal/domain/command"
	fleetmanagementDomain "github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
	onboardingDomain "github.com/btc-mining/proto-fleet/server/internal/domain/onboarding"
	pairingDomain "github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	poolsDomain "github.com/btc-mining/proto-fleet/server/internal/domain/pools"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/auth"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/command"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/fleetmanagement"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/health"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/interceptors"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/middleware"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/networkinfo"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/onboarding"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/pools"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/static"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/logging"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/server"
)

func main() {
	config := &Config{}

	_ = kong.Parse(config, kong.Name("fleetd"))

	logging.InitLogger(config.Log)

	err := start(config)
	if err != nil {
		slog.Error(fmt.Sprintf("%+v", err))
		os.Exit(1)
	}
}

var unauthenticatedProcedures = []string{
	"/health",
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	authv1connect.AuthServiceAuthenticateProcedure,
	onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
	networkinfov1connect.NetworkInfoServiceGetNetworkInfoProcedure,
}

var reflectEnabledServices = []string{
	pairingv1connect.PairingServiceName,
}

func start(config *Config) error {

	conn, err := db.ConnectAndMigrate(&config.DB)
	if err != nil {
		return err
	}

	// initialize domain services
	tokenSvc, err := tokenDomain.NewService(config.Auth)
	if err != nil {
		return err
	}
	minerClient := minerclient.NewService()
	authSvc := authDomain.NewService(conn, tokenSvc)
	pairingSvc := pairingDomain.NewService(conn, minerClient, config.Pairing)
	fleetMgmtSvc := fleetmanagementDomain.NewService(conn, fleetmanagementDomain.NewMockTelemetryCollector())
	commandSvc := commandDomain.NewService(conn, minerClient)
	onboardingSvc := onboardingDomain.NewService(conn)
	poolsSvc := poolsDomain.NewService(conn, config.Pools)

	// init middleware
	middlewares := []server.Middleware{
		middleware.NewCORSMiddleware(config.HTTP.SuppressCors),
	}

	// init interceptors
	li := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		interceptors.NewErrorStackTraceLoggingInterceptor(config.Log.Level),
		interceptors.NewRequestLoggingInterceptor(config.Log.Level),
		interceptors.NewAuthInterceptor(tokenSvc, unauthenticatedProcedures),
	)

	// setup rpc handlers
	mux := http.NewServeMux()

	mux.Handle("/", static.NewHandler(config.HTTP.StaticAssetPath))
	mux.HandleFunc("/health", health.NewHandler())

	if len(reflectEnabledServices) != 0 {
		slog.Debug("enabling reflection", "services", reflectEnabledServices)
		reflector := grpcreflect.NewStaticReflector(reflectEnabledServices...)
		mux.Handle(grpcreflect.NewHandlerV1(reflector))
		mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))
	}

	mux.Handle(authv1connect.NewAuthServiceHandler(auth.NewHandler(authSvc), li))
	mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboarding.NewHandler(authSvc, onboardingSvc), li))
	mux.Handle(pairingv1connect.NewPairingServiceHandler(pairing.NewHandler(pairingSvc), li))
	mux.Handle(networkinfov1connect.NewNetworkInfoServiceHandler(networkinfo.NewHandler(pairingSvc), li))
	mux.Handle(fleetmanagementv1connect.NewFleetManagementServiceHandler(fleetmanagement.NewHandler(fleetMgmtSvc), li))
	mux.Handle(minercommandv1connect.NewMinerCommandServiceHandler(command.NewHandler(commandSvc), li))
	mux.Handle(poolsv1connect.NewPoolsServiceHandler(pools.NewHandler(poolsSvc), li))

	var handler http.Handler = mux
	for _, m := range middlewares {
		handler = m.Wrap(handler)
	}

	handler = h2c.NewHandler(handler, &http2.Server{})
	httpServer := http.Server{
		Addr:              config.HTTP.Address,
		Handler:           handler,
		ReadHeaderTimeout: config.HTTP.ReadHeaderTimeout,
	}

	err = httpServer.ListenAndServe()
	if err != nil {
		return fmt.Errorf("server shutting down: %+v", err)
	}
	return nil
}
