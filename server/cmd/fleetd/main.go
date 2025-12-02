package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/capabilities"
	"github.com/btc-mining/proto-fleet/server/internal/domain/ipscanner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	minerModels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/plugins"
	sessionDomain "github.com/btc-mining/proto-fleet/server/internal/domain/session"

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
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	onboardingDomain "github.com/btc-mining/proto-fleet/server/internal/domain/onboarding"
	pairingDomain "github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
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

const (
	shutdownTimeout = 10 * time.Second
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

	// Initialize session store and service
	sessionStore := sqlstores.NewSQLSessionStore(conn)
	sessionSvc := sessionDomain.NewService(config.Session, sessionStore)

	// userStore implements both UserStore and UserManagementStore interfaces
	authSvc := authDomain.NewService(userStore, userStore, transactor, tokenSvc, sessionSvc, encryptSvc)

	// Start session cleanup goroutine
	sessionCleanupCtx, sessionCleanupCancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(sessionSvc.CleanupInterval())
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				deleted, err := sessionSvc.CleanupExpired(context.Background())
				if err != nil {
					slog.Error("failed to cleanup expired sessions", "error", err)
				} else if deleted > 0 {
					slog.Debug("cleaned up expired sessions", "count", deleted)
				}
			case <-sessionCleanupCtx.Done():
				return
			}
		}
	}()
	defer sessionCleanupCancel()

	if err := config.Plugins.Validate(); err != nil {
		return fmt.Errorf("invalid plugin configuration: %w", err)
	}

	pluginManager := plugins.NewManager(&config.Plugins)
	pluginService := plugins.NewService(pluginManager)

	// Load plugins early in the startup process with timeout
	pluginLoadCtx, pluginLoadCancel := context.WithTimeout(context.Background(),
		time.Duration(config.Plugins.MaxStartupTimeSeconds)*time.Second)
	defer pluginLoadCancel()

	if err := pluginManager.LoadPlugins(pluginLoadCtx); err != nil {
		slog.Error("Failed to load plugins", "error", err)
		if config.Plugins.FailOnUnhealthy {
			return fmt.Errorf("failed to load plugins: %w", err)
		}
		// Continue startup even if plugins fail to load
	}

	if err := pluginService.ValidatePluginHealth(pluginLoadCtx); err != nil {
		if config.Plugins.FailOnUnhealthy {
			return fmt.Errorf("plugin health check failed: %w", err)
		}
		slog.Warn("Plugin health check failed, continuing startup", "error", err)
	}

	// TODO(DASH-887): Remove hard dependency on proto plugin once:
	// 1. Plugin health checks can detect and report plugin loading failures
	// 2. The system can gracefully handle missing plugins (disable features vs. fatal error)
	// 3. UI can show which plugin-based features are unavailable
	// For now, we require the proto plugin to be available for fleet functionality
	if !pluginManager.HasPluginForMinerType(minerModels.TypeProto) {
		return fmt.Errorf("proto plugin is required but not loaded - ensure 'proto' plugin binary is in the plugins directory (check PLUGIN_DIR environment variable or default './plugins' directory)")
	}

	var discoverers []minerdiscovery.Discoverer

	pluginDiscoverers := pluginService.CreateDiscoverers()
	discoverers = append(discoverers, pluginDiscoverers...)

	discoveryService, err := minerdiscovery.NewService(discoverers...)
	if err != nil {
		return err
	}
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(conn)

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
	minerService := miner.NewMinerService(conn, userStore, encryptSvc, filesService, tokenSvc, pluginManager)

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

	// Ensure telemetry service cleanup on shutdown
	defer func() {
		slog.Info("Stopping telemetry service")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := telemetryService.Stop(shutdownCtx); err != nil {
			slog.Error("Failed to stop telemetry service", "error", err)
		}
	}()

	capabilitiesSvc, err := capabilities.NewService(config.Capabilities)
	if err != nil {
		return err
	}

	var pairers []pairingDomain.Pairer

	supportedTypes := pluginService.GetSupportedMinerTypes()
	for _, minerType := range supportedTypes {
		pluginPairer := plugins.NewPairer(pluginManager, minerType, transactor, discoveredDeviceStore, deviceStore, userStore, tokenSvc, encryptSvc)
		pairers = append(pairers, pluginPairer)
	}

	pairingSvc := pairingDomain.NewService(
		discoveredDeviceStore,
		deviceStore,
		transactor,
		tokenSvc,
		discoveryService,
		capabilitiesSvc,
		telemetryService,
		pairers...,
	)

	// Initialize IP scanner service
	ipScannerService := ipscanner.NewIPScannerService(
		config.IPScanner,
		deviceStore,
		discoveredDeviceStore,
		discoveryService,
		pairingSvc,
		slog.Default(),
	)

	if err := ipScannerService.Start(context.Background()); err != nil {
		slog.Error("failed to start IP scanner service", "error", err)
		return fmt.Errorf("failed to start IP scanner service: %w", err)
	}

	// Ensure IP scanner service cleanup on shutdown
	defer func() {
		slog.Info("Stopping IP scanner service")
		if err := ipScannerService.Stop(); err != nil {
			slog.Error("Failed to stop IP scanner service", "error", err)
		}
	}()

	fleetMgmtSvc := fleetmanagementDomain.NewService(deviceStore, discoveredDeviceStore, telemetryService, minerService)
	dbMessageQueue := queue.NewDatabaseMessageQueue(&config.Queue, conn)

	executionServiceCtx, executionServiceCancel := context.WithCancel(context.Background())
	defer executionServiceCancel()

	// Ensure plugin cleanup on shutdown
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(),
			time.Duration(config.Plugins.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()
		if err := pluginService.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown plugin service", "error", err)
		}
	}()

	executionService := commandDomain.NewExecutionService(executionServiceCtx, &config.Command, conn, dbMessageQueue, encryptSvc, tokenSvc, minerService, deviceStore, telemetryService)
	err = executionService.Start(executionServiceCtx)
	if err != nil {
		slog.Error("failed to start command execution service", "error", err)
	}

	statusService := commandDomain.NewStatusService(conn, dbMessageQueue)
	commandSvc := commandDomain.NewService(&config.Command, conn, executionService, dbMessageQueue, statusService, encryptSvc, filesService, deviceStore, telemetryService)
	onboardingSvc := onboardingDomain.NewService(deviceStore, poolStore, userStore)
	poolsSvc := poolsDomain.NewService(poolStore, transactor, config.Pools)

	middlewares := []server.Middleware{
		middleware.NewCORSMiddleware(config.HTTP.SuppressCors),
	}

	validateInterceptor := validate.NewInterceptor()

	li := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		interceptors.NewErrorStackTraceLoggingInterceptor(config.Log.Level),
		interceptors.NewRequestLoggingInterceptor(config.Log.Level),
		interceptors.NewAuthInterceptor(sessionSvc, userStore, interceptors.UnauthenticatedProcedures),
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
