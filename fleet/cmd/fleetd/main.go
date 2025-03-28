package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"connectrpc.com/connect"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/authors/v1/authorsv1connect"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/greet/v1/greetv1connect"

	"github.com/btc-mining/miner-firmware/fleet/internal/application"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/api"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/api/grpc"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db"
	"github.com/btc-mining/miner-firmware/fleet/internal/logging"

	"github.com/alecthomas/kong"
)

type Config struct {
	DB      db.DBConfig           `embed:"" prefix:"db" envprefix:"DB_"`
	Logging logging.LoggingConfig `embed:"" prefix:"logging" envprefix:"LOGGING_"`
	HTTP    api.HTTPConfig        `embed:"" prefix:"http" envprefix:"HTTP_"`
}

func main() {
	config := parseConfig()

	logging.InitLogger(config.Logging)

	err := start(config)
	if err != nil {
		slog.Error(fmt.Sprintf("%+v", err))
		os.Exit(1)
	}
}

func start(config *Config) error {

	databaseConnection, err := db.NewDatabaseConnection(&config.DB)
	if err != nil {
		return err
	}

	requestHandlers := grpcHandlers(
		application.NewAuthorUseCases(databaseConnection),
	)

	return api.RunServer(&config.HTTP, requestHandlers)
}

func parseConfig() *Config {
	config := Config{}

	_ = kong.Parse(&config, kong.Name("fleetd"))

	return &config
}

func grpcHandlers(
	authorUseCases *application.AuthorUseCases,
) []api.HandlerWithPath {
	interceptors := connect.WithInterceptors(
		grpc.ErrorLoggingInterceptor(),
	)

	return []api.HandlerWithPath{
		grpcHandler(greetv1connect.NewGreetServiceHandler(&grpc.GreetServer{}, interceptors)),
		grpcHandler(authorsv1connect.NewAuthorsServiceHandler(grpc.NewAuthorsServer(authorUseCases), interceptors)),
	}
}

func grpcHandler(path string, handler http.Handler) api.HandlerWithPath {
	return api.HandlerWithPath{Path: path, Handler: handler}
}
