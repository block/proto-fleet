package main

import (
	"connectrpc.com/grpcreflect"
	"fmt"
	authDomain "github.com/btc-mining/miner-firmware/fleet/internal/domain/auth"
	pairingDomain "github.com/btc-mining/miner-firmware/fleet/internal/domain/pairing"
	tokenDomain "github.com/btc-mining/miner-firmware/fleet/internal/domain/token"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/auth"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/health"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/interceptors"
	"github.com/btc-mining/miner-firmware/fleet/internal/handlers/middleware"
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

	// init middle ware
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

	var handler http.Handler = mux
	if authMiddleware != nil {
		handler = authMiddleware.Wrap(handler)
	}

	handler = h2c.NewHandler(handler, &http2.Server{})
	_ = http.Server{
		Addr:              config.HTTP.Address,
		Handler:           handler,
		ReadHeaderTimeout: config.HTTP.ReadHeaderTimeout,
	}

	return nil
}
