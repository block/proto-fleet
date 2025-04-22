package main

import (
	"connectrpc.com/grpcreflect"
	"fmt"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/networkinfo/v1/networkinfov1connect"
	authDomain "github.com/btc-mining/miner-firmware/fleet/internal/domain/auth"
	fleetmanagementDomain "github.com/btc-mining/miner-firmware/fleet/internal/domain/fleetmanagement"
	pairingDomain "github.com/btc-mining/miner-firmware/fleet/internal/domain/pairing"
	tokenDomain "github.com/btc-mining/miner-firmware/fleet/internal/domain/token"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/auth"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/fleetmanagement"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/health"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/interceptors"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/middleware"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/networkinfo"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/onboarding"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/pairing"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/static"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/logging"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"log/slog"
	"net/http"
	"os"

	"connectrpc.com/connect"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/pairing/v1/pairingv1connect"

	"github.com/alecthomas/kong"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db"
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

	// TODO remove the following before beta
	pairingv1connect.PairingServiceDiscoverProcedure,
	networkinfov1connect.NetworkInfoServiceGetNetworkInfoProcedure,
	fleetmanagementv1connect.FleetManagementServiceSetDefaultPoolProcedure,
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
	authSvc := authDomain.NewService(conn, tokenSvc)
	pairingSvc := pairingDomain.NewService()
	fleetMgmtSvc := fleetmanagementDomain.NewService(conn)

	// init middleware
	authMiddleware := middleware.NewAuthMiddleware(tokenSvc, unauthenticatedProcedures)

	// init interceptors
	li := connect.WithInterceptors(
		interceptors.ErrorLoggingInterceptor(),
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
	mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboarding.NewHandler(authSvc), li))
	mux.Handle(pairingv1connect.NewPairingServiceHandler(pairing.NewHandler(pairingSvc), li))
	mux.Handle(networkinfov1connect.NewNetworkInfoServiceHandler(networkinfo.NewHandler(pairingSvc), li))
	mux.Handle(fleetmanagementv1connect.NewFleetManagementServiceHandler(fleetmanagement.NewHandler(fleetMgmtSvc), li))

	var handler http.Handler = mux
	if authMiddleware != nil {
		handler = authMiddleware.Wrap(handler)
	}

	handler = h2c.NewHandler(handler, &http2.Server{})
	server := http.Server{
		Addr:              config.HTTP.Address,
		Handler:           handler,
		ReadHeaderTimeout: config.HTTP.ReadHeaderTimeout,
	}

	err = server.ListenAndServe()
	if err != nil {
		return fmt.Errorf("server shutting down: %+v", err)
	}
	return nil
}
