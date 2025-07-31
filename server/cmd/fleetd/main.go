package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"

	"github.com/btc-mining/proto-fleet/server/internal/handlers/health"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/influxdb"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"connectrpc.com/validate"
	"github.com/alecthomas/kong"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/logging"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1/telemetryv1connect"
	authDomain "github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	commandDomain "github.com/btc-mining/proto-fleet/server/internal/domain/command"
	fleetmanagementDomain "github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
	antminerRPC "github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	antminerWeb "github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	antminerDiscoverer "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/antminer"
	protoDiscoverer "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/proto"
	onboardingDomain "github.com/btc-mining/proto-fleet/server/internal/domain/onboarding"
	pairingDomain "github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	pairingAntminer "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/antminer"
	pairingProto "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/proto"
	poolsDomain "github.com/btc-mining/proto-fleet/server/internal/domain/pools"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/scheduler"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/auth"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/command"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/fleetmanagement"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/interceptors"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/middleware"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/networkinfo"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/onboarding"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/pools"
	telemetryHandler "github.com/btc-mining/proto-fleet/server/internal/handlers/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/server"
)

func main() {
	config := &Config{}

	data, err := os.ReadFile("/var/lib/fleet/start/.env")
	if err != nil && !os.IsNotExist(err) {
		slog.Error("failed to read .env file", "error", err)
		os.Exit(1)
	}
	os.Setenv("INFLUXDB3_AUTH_TOKEN", strings.TrimPrefix(strings.TrimSpace(string(data)), "INFLUXDB3_AUTH_TOKEN="))

	_ = kong.Parse(config, kong.Name("fleetd"))

	logging.InitLogger(config.Log)

	err = start(config)
	if err != nil {
		slog.Error(fmt.Sprintf("%+v", err))
		os.Exit(1)
	}
}

var reflectEnabledServices = []string{
	pairingv1connect.PairingServiceName,
	telemetryv1connect.TelemetryServiceName,
}

func start(config *Config) error {

	conn, err := db.ConnectAndMigrate(&config.DB)
	if err != nil {
		return err
	}

	transactor := sqlstores.NewSQLTransactor(conn)

	encryptSvc, err := encrypt.NewService(&config.Encrypt)
	if err != nil {
		return err
	}

	userStore := sqlstores.NewSQLUserStore(conn)
	poolStore := sqlstores.NewSQLPoolStore(conn, encryptSvc)
	deviceStore := sqlstores.NewSQLDeviceStore(conn)

	tokenSvc, err := tokenDomain.NewService(config.Auth)
	if err != nil {
		return err
	}
	authSvc := authDomain.NewService(userStore, transactor, tokenSvc, encryptSvc)
	protoDiscoverer := protoDiscoverer.NewDiscoverer()
	antminerDiscoverer := antminerDiscoverer.NewDiscoverer(antminerRPC.NewService())
	discoveryService, err := minerdiscovery.NewService(protoDiscoverer, antminerDiscoverer)
	if err != nil {
		return err
	}
	discoveredDeviceStore := minerdiscovery.NewInMemoryDiscoveredDeviceStore()

	influxdbService, err := influxdb.NewTelemetryStore(config.InfluxDB)
	if err != nil {
		return err
	}
	scheduler := scheduler.NewScheduler(
		config.Scheduler,
	)
	filesService, err := files.NewService()
	if err != nil {
		return err
	}
	minerService := miner.NewMinerService(conn, userStore, encryptSvc, filesService, tokenSvc)

	telemetryService := telemetry.NewTelemetryService(
		config.Telemetry,
		influxdbService,
		minerService,
		scheduler,
		deviceStore,
	)

	if err := telemetryService.Start(context.Background()); err != nil {
		slog.Error("failed to start telemetry service", "error", err)
		return fmt.Errorf("failed to start telemetry service: %w", err)
	}

	protoPairer := pairingProto.NewService(transactor, deviceStore, userStore, config.Pairing, minerService, tokenSvc, encryptSvc)
	antminerPairer := pairingAntminer.NewService(transactor, deviceStore, encryptSvc, antminerWeb.NewService())

	pairingSvc := pairingDomain.NewService(
		discoveredDeviceStore,
		deviceStore,
		transactor,
		tokenSvc,
		discoveryService,
		telemetryService,
		protoPairer,
		antminerPairer,
	)
	fleetMgmtSvc := fleetmanagementDomain.NewService(deviceStore, fleetmanagementDomain.NewMockTelemetryCollector(), minerService)
	dbMessageQueue := queue.NewDatabaseMessageQueue(&config.Queue, conn)

	executionServiceCtx, executionServiceCancel := context.WithCancel(context.Background())
	defer executionServiceCancel()

	executionService := commandDomain.NewExecutionService(executionServiceCtx, &config.Command, conn, dbMessageQueue, encryptSvc, tokenSvc, minerService)
	err = executionService.Start(executionServiceCtx)
	if err != nil {
		slog.Error("failed to start command execution service", "error", err)
	}

	statusService := commandDomain.NewStatusService(conn, dbMessageQueue)
	commandSvc := commandDomain.NewService(&config.Command, conn, executionService, dbMessageQueue, statusService, encryptSvc, filesService)
	onboardingSvc := onboardingDomain.NewService(deviceStore, poolStore)
	poolsSvc := poolsDomain.NewService(poolStore, transactor, config.Pools)

	middlewares := []server.Middleware{
		middleware.NewCORSMiddleware(config.HTTP.SuppressCors),
	}

	validateInterceptor, err := validate.NewInterceptor()
	if err != nil {
		slog.Error("failed to create validate interceptor", "error", err)
		return fmt.Errorf("failed to create validate interceptor: %w", err)
	}

	li := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		interceptors.NewErrorStackTraceLoggingInterceptor(config.Log.Level),
		interceptors.NewRequestLoggingInterceptor(config.Log.Level),
		interceptors.NewAuthInterceptor(tokenSvc, interceptors.UnauthenticatedProcedures),
		validateInterceptor,
	)

	mux := http.NewServeMux()

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
	mux.Handle(telemetryv1connect.NewTelemetryServiceHandler(telemetryHandler.NewHandler(telemetryService), li))

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
