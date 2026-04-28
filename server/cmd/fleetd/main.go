package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // #nosec G108 -- pprof endpoint intentionally exposed for debugging
	"os"
	"time"
	_ "time/tzdata"

	"github.com/block/proto-fleet/server/internal/domain/ipscanner"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	sessionDomain "github.com/block/proto-fleet/server/internal/domain/session"

	"github.com/block/proto-fleet/server/internal/infrastructure/files"

	"github.com/block/proto-fleet/server/internal/handlers/health"

	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"connectrpc.com/validate"
	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	fleet_telemetry "github.com/block/proto-fleet/server/internal/infrastructure/fleet-telemetry"
	"github.com/block/proto-fleet/server/internal/infrastructure/logging"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/block/proto-fleet/server/generated/grpc/activity/v1/activityv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/collection/v1/collectionv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/device_set/v1/device_setv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/errors/v1/errorsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1/foremanimportv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/schedule/v1/schedulev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/telemetry/v1/telemetryv1connect"
	activityDomain "github.com/block/proto-fleet/server/internal/domain/activity"
	apikeyDomain "github.com/block/proto-fleet/server/internal/domain/apikey"
	authDomain "github.com/block/proto-fleet/server/internal/domain/auth"
	collectionDomain "github.com/block/proto-fleet/server/internal/domain/collection"
	commandDomain "github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/deviceresolver"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics"
	fleetmanagementDomain "github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	foremanImportDomain "github.com/block/proto-fleet/server/internal/domain/foremanimport"
	onboardingDomain "github.com/block/proto-fleet/server/internal/domain/onboarding"
	pairingDomain "github.com/block/proto-fleet/server/internal/domain/pairing"
	poolsDomain "github.com/block/proto-fleet/server/internal/domain/pools"
	scheduleDomain "github.com/block/proto-fleet/server/internal/domain/schedule"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/scheduler"
	tokenDomain "github.com/block/proto-fleet/server/internal/domain/token"
	activityHandler "github.com/block/proto-fleet/server/internal/handlers/activity"
	apikeyHandler "github.com/block/proto-fleet/server/internal/handlers/apikey"
	"github.com/block/proto-fleet/server/internal/handlers/auth"
	collectionHandler "github.com/block/proto-fleet/server/internal/handlers/collection"
	"github.com/block/proto-fleet/server/internal/handlers/command"
	devicesetHandler "github.com/block/proto-fleet/server/internal/handlers/deviceset"
	errorqueryHandler "github.com/block/proto-fleet/server/internal/handlers/errorquery"
	firmwareHandler "github.com/block/proto-fleet/server/internal/handlers/firmware"
	"github.com/block/proto-fleet/server/internal/handlers/fleetmanagement"
	foremanImportHandler "github.com/block/proto-fleet/server/internal/handlers/foremanimport"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/handlers/networkinfo"
	"github.com/block/proto-fleet/server/internal/handlers/onboarding"
	"github.com/block/proto-fleet/server/internal/handlers/pairing"
	"github.com/block/proto-fleet/server/internal/handlers/pools"
	scheduleHandler "github.com/block/proto-fleet/server/internal/handlers/schedule"
	telemetryHandler "github.com/block/proto-fleet/server/internal/handlers/telemetry"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/server"
)

const (
	shutdownTimeout = 10 * time.Second
)

// version is overwritten at release build time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	config := &Config{}

	_ = kong.Parse(
		config,
		kong.Name("fleetd"),
		kong.Configuration(kongyaml.Loader, "/etc/fleetd/config.yaml"),
	)

	logging.InitLogger(config.Log)

	slog.Info("fleetd starting", "version", version)

	if err := start(config); err != nil {
		slog.Error(fmt.Sprintf("%+v", err))
		os.Exit(1)
	}
}

var reflectEnabledServices = []string{
	pairingv1connect.PairingServiceName,
	telemetryv1connect.TelemetryServiceName,
}

func start(config *Config) error {
	shutdownTracer, err := fleet_telemetry.Setup(context.Background(), version, config.FleetTelemetry)
	if err != nil {
		return fmt.Errorf("setup fleet telemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := shutdownTracer(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown tracer", "error", err)
		}
	}()

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
	collectionStore := sqlstores.NewSQLCollectionStore(conn)
	activityStore := sqlstores.NewSQLActivityStore(conn)

	activitySvc := activityDomain.NewService(activityStore)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(conn)
	apiKeySvc := apikeyDomain.NewService(apiKeyStore, activitySvc)

	tokenSvc, err := tokenDomain.NewService(config.Auth)
	if err != nil {
		return err
	}

	// Initialize session store and service
	sessionStore := sqlstores.NewSQLSessionStore(conn)
	sessionSvc := sessionDomain.NewService(config.Session, sessionStore)

	// userStore implements both UserStore and UserManagementStore interfaces
	authSvc := authDomain.NewService(userStore, userStore, transactor, tokenSvc, sessionSvc, encryptSvc, activitySvc)

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

	// TODO: Remove hard dependency on proto plugin once:
	// 1. Plugin health checks can detect and report plugin loading failures
	// 2. The system can gracefully handle missing plugins (disable features vs. fatal error)
	// 3. UI can show which plugin-based features are unavailable
	// For now, we require the proto plugin to be available for fleet functionality
	if !pluginManager.HasPluginForDriverName(models.DriverNameProto) {
		return fmt.Errorf("proto plugin is required but not loaded - ensure 'proto' plugin binary is in the plugins directory (check PLUGIN_DIR environment variable or default './plugins' directory)")
	}

	discoverer := pluginService.CreateDiscoverer()
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(conn)

	timescaledbService, err := timescaledb.NewTelemetryStore(conn, config.TimescaleDB)
	if err != nil {
		return err
	}
	scheduler := scheduler.NewScheduler(
		config.Scheduler,
	)

	filesService, err := files.NewService(config.Files)
	if err != nil {
		return err
	}
	minerService := miner.NewMinerService(conn, userStore, encryptSvc, filesService, tokenSvc, pluginManager)

	// Create diagnostics service for error polling and auto-closing stale errors
	diagnosticsCtx, diagnosticsCancel := context.WithCancel(context.Background())
	defer diagnosticsCancel()
	errorStore := sqlstores.NewSQLErrorStore(conn, transactor)
	diagnosticsService := diagnostics.NewService(diagnosticsCtx, config.Diagnostics, errorStore, transactor)

	telemetryService := telemetry.NewTelemetryService(
		config.Telemetry,
		timescaledbService,
		minerService,
		scheduler,
		deviceStore,
		diagnosticsService,
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

	pluginPairer := plugins.NewPairer(pluginManager, transactor, discoveredDeviceStore, deviceStore, userStore, tokenSvc, encryptSvc)

	pairingSvc := pairingDomain.NewService(
		discoveredDeviceStore,
		deviceStore,
		transactor,
		tokenSvc,
		discoverer,
		pluginService,
		telemetryService,
		pluginPairer,
	)
	pairingSvc.WithMinerInvalidator(minerService.InvalidateMiner)

	// Initialize IP scanner service
	ipScannerService := ipscanner.NewIPScannerService(
		config.IPScanner,
		deviceStore,
		discoveredDeviceStore,
		discoverer,
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

	executionService := commandDomain.NewExecutionService(executionServiceCtx, &config.Command, conn, dbMessageQueue, encryptSvc, tokenSvc, minerService, deviceStore, telemetryService, filesService)
	err = executionService.Start(executionServiceCtx)
	if err != nil {
		slog.Error("failed to start command execution service", "error", err)
	}

	statusService := commandDomain.NewStatusService(conn, dbMessageQueue)
	commandSvc := commandDomain.NewService(&config.Command, conn, executionService, dbMessageQueue, statusService, encryptSvc, filesService, deviceStore, userStore, authSvc, telemetryService, pluginService, activitySvc)
	fleetMgmtSvc := fleetmanagementDomain.NewService(deviceStore, discoveredDeviceStore, telemetryService, minerService, pluginService, poolStore, errorStore, collectionStore, commandSvc, activitySvc)
	defer fleetMgmtSvc.WaitForPendingClearAuthKeys(shutdownTimeout)
	onboardingSvc := onboardingDomain.NewService(deviceStore, poolStore, userStore)
	poolsSvc := poolsDomain.NewService(poolStore, transactor, config.Pools, activitySvc)
	scheduleStore := sqlstores.NewSQLScheduleStore(conn)
	scheduleSvc := scheduleDomain.NewService(scheduleStore, scheduleStore, scheduleStore, transactor, activitySvc)

	// Register the schedule-conflict preflight filter on commandSvc so every
	// caller (manual API, schedule processor, future curtailment reconciler)
	// sees the same priority/manual-fallback semantics. Pre-pre-work this
	// only ran inline inside the schedule processor, leaving manual
	// SetPowerTarget calls free to race a running power-target schedule.
	commandSvc.RegisterFilter(commandDomain.NewScheduleConflictFilter(scheduleStore, scheduleStore, collectionStore))

	scheduleProcessor := scheduleDomain.NewProcessor(scheduleStore, scheduleStore, collectionStore, commandSvc, activitySvc)
	if err := scheduleProcessor.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start schedule processor: %w", err)
	}
	defer func() {
		if err := scheduleProcessor.Stop(); err != nil {
			slog.Error("failed to stop schedule processor", "error", err)
		}
	}()

	deviceResolver := deviceresolver.New(deviceStore)
	collectionSvc := collectionDomain.NewService(collectionStore, deviceStore, transactor, deviceResolver.Resolve, telemetryService, activitySvc)
	foremanImportSvc := foremanImportDomain.NewService(poolsSvc, collectionSvc, deviceStore)

	middlewares := []server.Middleware{
		middleware.NewCORSMiddleware(config.HTTP.SuppressCors),
		middleware.TelemetryMiddleware{},
	}

	validateInterceptor := validate.NewInterceptor()

	li := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		interceptors.NewErrorStackTraceLoggingInterceptor(config.Log.Level),
		interceptors.NewRequestLoggingInterceptor(config.Log.Level, interceptors.RedactedRequestProcedures, interceptors.RedactedResponseProcedures),
		interceptors.NewAuthInterceptor(sessionSvc, userStore, userStore, apiKeySvc, interceptors.UnauthenticatedProcedures, interceptors.SessionOnlyProcedures),
		validateInterceptor,
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", health.NewHandler())
	mux.Handle("/api/v1/firmware/upload", firmwareHandler.NewUploadHandler(filesService, sessionSvc, userStore, filesService.MaxFirmwareFileSize()))
	mux.Handle("/api/v1/firmware/check", firmwareHandler.NewCheckHandler(filesService, sessionSvc, userStore))
	mux.Handle("GET /api/v1/firmware/config", firmwareHandler.NewConfigHandler(filesService, sessionSvc, userStore, config.Files))

	chunkedMgr := firmwareHandler.NewChunkedUploadManager()
	mux.Handle("POST /api/v1/firmware/upload/chunked", firmwareHandler.NewInitiateHandler(chunkedMgr, filesService, sessionSvc, userStore))
	mux.Handle("PUT /api/v1/firmware/upload/chunked/{uploadId}", firmwareHandler.NewChunkHandler(chunkedMgr, sessionSvc, userStore))
	mux.Handle("POST /api/v1/firmware/upload/chunked/{uploadId}/complete", firmwareHandler.NewCompleteHandler(chunkedMgr, filesService, sessionSvc, userStore))
	mux.Handle("GET /api/v1/firmware/files", firmwareHandler.NewListFilesHandler(filesService, sessionSvc, userStore))
	mux.Handle("DELETE /api/v1/firmware/files/{fileId}", firmwareHandler.NewDeleteFileHandler(filesService, sessionSvc, userStore))
	mux.Handle("DELETE /api/v1/firmware/files", firmwareHandler.NewDeleteAllFilesHandler(filesService, sessionSvc, userStore))

	chunkedCleanupCtx, chunkedCleanupCancel := context.WithCancel(context.Background())
	go chunkedMgr.StartCleanup(chunkedCleanupCtx, config.Files.ChunkedUploadSessionTTL)
	defer chunkedCleanupCancel()

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
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(scheduleHandler.NewHandler(scheduleSvc), li))
	mux.Handle(collectionv1connect.NewDeviceCollectionServiceHandler(collectionHandler.NewHandler(collectionSvc), li))
	mux.Handle(device_setv1connect.NewDeviceSetServiceHandler(devicesetHandler.NewHandler(collectionSvc), li))
	mux.Handle(telemetryv1connect.NewTelemetryServiceHandler(telemetryHandler.NewHandler(telemetryService), li))
	mux.Handle(errorsv1connect.NewErrorQueryServiceHandler(errorqueryHandler.NewHandler(diagnosticsService), li))
	mux.Handle(foremanimportv1connect.NewForemanImportServiceHandler(foremanImportHandler.NewHandler(foremanImportSvc), li))
	mux.Handle(activityv1connect.NewActivityServiceHandler(activityHandler.NewHandler(activitySvc), li))
	mux.Handle(apikeyv1connect.NewApiKeyServiceHandler(apikeyHandler.NewHandler(apiKeySvc), li))

	if config.HTTP.PprofAddr != "" {
		ln, err := net.Listen("tcp", config.HTTP.PprofAddr)
		if err != nil {
			return fmt.Errorf("pprof debug server: %w", err)
		}
		pprofServer := &http.Server{
			Handler:           http.DefaultServeMux,
			ReadHeaderTimeout: config.HTTP.ReadHeaderTimeout,
		}
		go func() {
			slog.Info("Starting pprof debug server", "addr", config.HTTP.PprofAddr)
			if err := pprofServer.Serve(ln); err != nil && err != http.ErrServerClosed {
				slog.Error("pprof debug server failed", "error", err)
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := pprofServer.Shutdown(shutdownCtx); err != nil {
				slog.Error("Failed to shutdown pprof debug server", "error", err)
			}
		}()
	}

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
