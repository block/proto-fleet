package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"connectrpc.com/connect"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1/onboardingv1connect"

	"github.com/btc-mining/miner-firmware/fleet/internal/application"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/api"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/api/grpc"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/api/grpc/middleware"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db"
	"github.com/btc-mining/miner-firmware/fleet/internal/logging"

	"github.com/alecthomas/kong"
)

type Config struct {
	DB      db.DBConfig           `embed:"" prefix:"db" envprefix:"DB_"`
	Logging logging.LoggingConfig `embed:"" prefix:"logging" envprefix:"LOGGING_"`
	HTTP    api.HTTPConfig        `embed:"" prefix:"http" envprefix:"HTTP_"`
	Auth    domain.AuthConfig     `embed:"" prefix:"auth" envprefix:"AUTH_"`
}

func main() {
	config := &Config{}

	_ = kong.Parse(config, kong.Name("fleetd"))

	logging.InitLogger(config.Logging)

	err := start(config)
	if err != nil {
		slog.Error(fmt.Sprintf("%+v", err))
		os.Exit(1)
	}
}

var unauthenticatedProcedures = []string{
	authv1connect.AuthServiceAuthenticateProcedure,
	onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
}

func start(config *Config) error {

	conn, err := db.ConnectAndMigrate(&config.DB)
	if err != nil {
		return err
	}
	// initialize domain services
	tokenSvc, err := domain.NewTokenService(config.Auth)
	if err != nil {
		return err
	}
	authSvc := domain.NewAuthService(tokenSvc)

	authMiddleware := middleware.NewAuthMiddleware(tokenSvc, unauthenticatedProcedures)

	// initialize use cases
	authUseCases := application.NewAuthUseCases(conn, authSvc)

	interceptors := connect.WithInterceptors(
		grpc.ErrorLoggingInterceptor(),
	)

	requestHandlers := []api.HandlerWithPath{
		grpcHandler(authv1connect.NewAuthServiceHandler(grpc.NewAuthServer(authUseCases), interceptors)),
		grpcHandler(onboardingv1connect.NewOnboardingServiceHandler(grpc.NewOnboardingServer(authUseCases), interceptors)),
	}

	return api.RunServer(&config.HTTP, requestHandlers, authMiddleware)
}

func grpcHandler(path string, handler http.Handler) api.HandlerWithPath {
	return api.HandlerWithPath{Path: path, Handler: handler}
}
