package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // #nosec G108 -- pprof endpoint intentionally exposed for debugging
	"os"
	"time"
	_ "time/tzdata"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/ipscanner"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	sessionDomain "github.com/block/proto-fleet/server/internal/domain/session"

	"github.com/block/proto-fleet/server/internal/infrastructure/files"

	"github.com/block/proto-fleet/server/internal/handlers/health"

	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/infrastructure/sysmon"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"connectrpc.com/validate"
	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	fleet_telemetry "github.com/block/proto-fleet/server/internal/infrastructure/fleet-telemetry"
	"github.com/block/proto-fleet/server/internal/infrastructure/logging"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/block/proto-fleet/server/generated/grpc/activity/v1/activityv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/alerts/v1/alertsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/authz/v1/authzv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/buildings/v1/buildingsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/collection/v1/collectionv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/device_set/v1/device_setv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/errors/v1/errorsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1/foremanimportv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1/infrastructurev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/schedule/v1/schedulev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/serverlog/v1/serverlogv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/sites/v1/sitesv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/telemetry/v1/telemetryv1connect"
	"github.com/block/proto-fleet/server/generated/sqlc"
	activityDomain "github.com/block/proto-fleet/server/internal/domain/activity"
	alertsDomain "github.com/block/proto-fleet/server/internal/domain/alerts"
	apikeyDomain "github.com/block/proto-fleet/server/internal/domain/apikey"
	authDomain "github.com/block/proto-fleet/server/internal/domain/auth"
	buildingsDomain "github.com/block/proto-fleet/server/internal/domain/buildings"
	collectionDomain "github.com/block/proto-fleet/server/internal/domain/collection"
	commandDomain "github.com/block/proto-fleet/server/internal/domain/command"
	curtailmentDomain "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	curtailmentReconciler "github.com/block/proto-fleet/server/internal/domain/curtailment/reconciler"
	"github.com/block/proto-fleet/server/internal/domain/deviceresolver"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics"
	fleetmanagementDomain "github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	fleetnodeauth "github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	fleetnodediscovery "github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/fleetoptions"
	foremanImportDomain "github.com/block/proto-fleet/server/internal/domain/foremanimport"
	infrastructureDomain "github.com/block/proto-fleet/server/internal/domain/infrastructure"
	onboardingDomain "github.com/block/proto-fleet/server/internal/domain/onboarding"
	pairingDomain "github.com/block/proto-fleet/server/internal/domain/pairing"
	poolsDomain "github.com/block/proto-fleet/server/internal/domain/pools"
	scheduleDomain "github.com/block/proto-fleet/server/internal/domain/schedule"
	sitesDomain "github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/scheduler"
	tokenDomain "github.com/block/proto-fleet/server/internal/domain/token"
	activityHandler "github.com/block/proto-fleet/server/internal/handlers/activity"
	"github.com/block/proto-fleet/server/internal/handlers/alertmanagerwebhook"
	alertsHandler "github.com/block/proto-fleet/server/internal/handlers/alerts"
	apikeyHandler "github.com/block/proto-fleet/server/internal/handlers/apikey"
	"github.com/block/proto-fleet/server/internal/handlers/auth"
	authzHandler "github.com/block/proto-fleet/server/internal/handlers/authz"
	buildingsHandler "github.com/block/proto-fleet/server/internal/handlers/buildings"
	collectionHandler "github.com/block/proto-fleet/server/internal/handlers/collection"
	"github.com/block/proto-fleet/server/internal/handlers/command"
	curtailmentHandler "github.com/block/proto-fleet/server/internal/handlers/curtailment"
	devicesetHandler "github.com/block/proto-fleet/server/internal/handlers/deviceset"
	errorqueryHandler "github.com/block/proto-fleet/server/internal/handlers/errorquery"
	firmwareHandler "github.com/block/proto-fleet/server/internal/handlers/firmware"
	"github.com/block/proto-fleet/server/internal/handlers/fleetmanagement"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/admin"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	foremanImportHandler "github.com/block/proto-fleet/server/internal/handlers/foremanimport"
	infrastructureHandler "github.com/block/proto-fleet/server/internal/handlers/infrastructure"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	minerProxyHandler "github.com/block/proto-fleet/server/internal/handlers/minerproxy"
	"github.com/block/proto-fleet/server/internal/handlers/networkinfo"
	"github.com/block/proto-fleet/server/internal/handlers/onboarding"
	"github.com/block/proto-fleet/server/internal/handlers/pairing"
	"github.com/block/proto-fleet/server/internal/handlers/pools"
	scheduleHandler "github.com/block/proto-fleet/server/internal/handlers/schedule"
	serverlogHandler "github.com/block/proto-fleet/server/internal/handlers/serverlog"
	sitesHandler "github.com/block/proto-fleet/server/internal/handlers/sites"
	telemetryHandler "github.com/block/proto-fleet/server/internal/handlers/telemetry"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/mqttclient"
	"github.com/block/proto-fleet/server/internal/infrastructure/server"
)

const shutdownTimeout = 10 * time.Second

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

// reflectEnabledServices lists the gRPC services exposed via the
// reflection endpoint. Policy: every authenticated service is
// reflected so SDK + tooling can discover them. The auth interceptor
// chain is the security boundary; reflection itself just exposes
// service shape (no business data), so the inclusion list is "all
// services" rather than a curated subset.
var reflectEnabledServices = []string{
	pairingv1connect.PairingServiceName,
	telemetryv1connect.TelemetryServiceName,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceName,
	sitesv1connect.SiteServiceName,
	buildingsv1connect.BuildingServiceName,
	curtailmentv1connect.CurtailmentServiceName,
	device_setv1connect.DeviceSetServiceName,
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

	metricsProvider, err := metrics.Setup(context.Background(), version, config.Metrics, conn)
	if err != nil {
		return fmt.Errorf("setup metrics provider: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := metricsProvider.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown metrics provider", "error", err)
		}
	}()

	// Fail fast rather than warn: this state is only reachable through
	// contradictory hand-edited env (run-fleet.sh already couples the flags),
	// and continuing would leave provisioned heartbeat rules firing forever
	// with no collector and no webhook receiver to deliver them.
	if config.SystemMonitoring.Enabled && !metricsProvider.Enabled() {
		return errors.New("FLEET_SYSTEM_MONITORING_ENABLED requires FLEET_ALERTS_ENABLED (the metrics writer feeds the system-monitoring rules)")
	}

	// Cap the reconcile at 60s. The advisory lock inside Reconcile makes
	// concurrent boots serialize, so a non-winner during a rolling
	// deploy waits for the winner to commit; without a deadline a stuck
	// reconcile would block boot forever. 60s is generous for the work
	// (catalog upsert + 3 role rows per active org) and short enough
	// that a stuck instance crashes loud rather than hanging silently.
	reconcileCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := authz.Reconcile(reconcileCtx, conn); err != nil {
		return fmt.Errorf("reconcile built-in roles: %w", err)
	}

	permissionResolver := authz.NewPermissionResolver(conn)

	transactor := sqlstores.NewSQLTransactor(conn)

	encryptSvc, err := encrypt.NewService(&config.Encrypt)
	if err != nil {
		return err
	}

	userStore := sqlstores.NewSQLUserStore(conn)
	poolStore := sqlstores.NewSQLPoolStore(conn, encryptSvc)
	deviceStore := sqlstores.NewSQLDeviceStore(conn)
	collectionStore := sqlstores.NewSQLCollectionStore(conn, config.TimescaleDB.MaxAge)
	activityStore := sqlstores.NewSQLActivityStore(conn)
	notificationHistoryStore := sqlstores.NewSQLNotificationHistoryStore(conn)

	activitySvc := activityDomain.NewService(activityStore)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(conn)
	apiKeySvc := apikeyDomain.NewService(apiKeyStore, activitySvc)

	fleetNodeEnrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(conn)
	fleetNodeEnrollmentSvc := enrollment.NewService(fleetNodeEnrollmentStore, apiKeySvc, transactor, activitySvc)
	fleetNodePairingStore := sqlstores.NewSQLFleetNodePairingStore(conn)
	fleetNodePairingSvc := fleetnodepairing.NewService(fleetNodePairingStore, fleetNodeEnrollmentStore, transactor)
	fleetNodeControlRegistry := control.NewRegistry()
	fleetNodeDiscoverySvc := fleetnodediscovery.NewService(fleetNodeControlRegistry, fleetNodeEnrollmentSvc)
	fleetNodeAuthStore := sqlstores.NewSQLFleetNodeAuthStore(conn)
	fleetNodeAuthSvc := fleetnodeauth.NewService(fleetNodeAuthStore, fleetNodeEnrollmentStore, apiKeySvc)

	tokenSvc, err := tokenDomain.NewService(config.Auth)
	if err != nil {
		return err
	}

	// Initialize session store and service
	sessionStore := sqlstores.NewSQLSessionStore(conn)
	sessionSvc := sessionDomain.NewService(config.Session, sessionStore)

	// userStore implements both UserStore and UserManagementStore interfaces
	authSvc := authDomain.NewService(userStore, userStore, transactor, tokenSvc, sessionSvc, encryptSvc, activitySvc, permissionResolver)

	// Start session cleanup goroutine
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(sessionSvc.CleanupInterval())
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if deleted, err := sessionSvc.CleanupExpired(cleanupCtx); err != nil {
					slog.Error("failed to cleanup expired sessions", "error", err)
				} else if deleted > 0 {
					slog.Debug("cleaned up expired sessions", "count", deleted)
				}
				if swept, err := fleetNodeEnrollmentSvc.SweepExpired(cleanupCtx); err != nil {
					slog.Error("failed to sweep expired fleet node enrollments", "error", err)
				} else if swept > 0 {
					slog.Debug("swept expired fleet node enrollments", "count", swept)
				}
				if challenges, sessions, err := fleetNodeAuthSvc.SweepExpired(cleanupCtx); err != nil {
					slog.Error("failed to sweep expired fleet node auth state", "error", err)
				} else if challenges > 0 || sessions > 0 {
					slog.Debug("swept expired fleet node auth state", "challenges", challenges, "sessions", sessions)
				}
			case <-cleanupCtx.Done():
				return
			}
		}
	}()
	defer cleanupCancel()

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

	fleetNodePairingSvc.WithProvisioning(deviceStore, discoveredDeviceStore, fleetNodeControlRegistry)

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
	commandArtifactCleanupCtx, commandArtifactCleanupCancel := context.WithCancel(context.Background())
	runCommandArtifactSweep := func() {
		deleted, sweepErr := filesService.SweepExpiredCommandArtifacts(time.Now().UTC(), filesService.CommandArtifactRetentionTTL())
		if sweepErr != nil {
			slog.Error("failed to sweep expired command artifacts", "error", sweepErr)
			return
		}
		if deleted > 0 {
			slog.Debug("swept expired command artifacts", "count", deleted)
		}
	}
	go func() {
		ticker := time.NewTicker(filesService.CommandArtifactCleanupInterval())
		defer ticker.Stop()
		runCommandArtifactSweep()

		for {
			select {
			case <-ticker.C:
				runCommandArtifactSweep()
			case <-commandArtifactCleanupCtx.Done():
				return
			}
		}
	}()
	defer commandArtifactCleanupCancel()
	minerService := miner.NewMinerService(conn, userStore, encryptSvc, filesService, pluginManager).
		WithCommandSender(fleetNodeControlRegistry)

	// Create diagnostics service for error polling and auto-closing stale errors
	diagnosticsCtx, diagnosticsCancel := context.WithCancel(context.Background())
	defer diagnosticsCancel()
	errorStore := sqlstores.NewSQLErrorStore(conn, transactor)
	diagnosticsService := diagnostics.NewService(diagnosticsCtx, config.Diagnostics, errorStore, transactor).
		WithDeviceScopeResolver(deviceStore)

	// Shared per-org cache for ListMinerStateSnapshots option arrays
	// (models, firmware versions). The TTL is the primary freshness
	// mechanism; pairing and delete flows invalidate obvious add/remove
	// membership changes.
	fleetOptionsCache := fleetoptions.NewCache(fleetoptions.DefaultTTL, 1024)

	telemetryService := telemetry.NewTelemetryService(
		config.Telemetry,
		timescaledbService,
		minerService,
		scheduler,
		deviceStore,
		diagnosticsService,
	)
	telemetryService.WithMetricsEmitter(metricsProvider)
	fleetNodePairingSvc.WithTelemetryScheduler(telemetryService)
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

	pluginPairer := plugins.NewPairer(pluginManager, transactor, discoveredDeviceStore, deviceStore, encryptSvc)

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
	pairingSvc.WithOptionsCache(fleetOptionsCache)
	fleetNodePairingSvc.WithMinerInvalidator(minerService.InvalidateMinerByID)
	fleetNodeEnrollmentSvc.WithMinerInvalidator(minerService.InvalidateMinerByID)

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
	executionService.WithMetricsEmitter(metricsProvider)
	err = executionService.Start(executionServiceCtx)
	if err != nil {
		slog.Error("failed to start command execution service", "error", err)
	}

	statusService := commandDomain.NewStatusService(conn, dbMessageQueue)
	commandSvc := commandDomain.NewService(&config.Command, conn, executionService, dbMessageQueue, statusService, encryptSvc, filesService, deviceStore, userStore, authSvc, telemetryService, pluginService, activitySvc)
	commandSvc.SetPluginCapabilitiesProvider(pluginService)
	// buildingStore is constructed below alongside siteStore; both are
	// needed for the parseFilter cross-org check on building_ids and
	// zone_keys. Hoist the construction so fleetMgmtSvc can depend on it.
	siteStore := sqlstores.NewSQLSiteStore(conn)
	buildingStore := sqlstores.NewSQLBuildingStore(conn)
	fleetMgmtSvc := fleetmanagementDomain.NewService(deviceStore, discoveredDeviceStore, telemetryService, minerService, pluginService, poolStore, errorStore, collectionStore, buildingStore, commandSvc, activitySvc)
	fleetMgmtSvc.WithOptionsCache(fleetOptionsCache)
	defer fleetMgmtSvc.WaitForPendingUnpairs(shutdownTimeout)
	onboardingSvc := onboardingDomain.NewService(deviceStore, poolStore, userStore)
	poolsSvc := poolsDomain.NewService(poolStore, transactor, config.Pools, activitySvc)
	scheduleStore := sqlstores.NewSQLScheduleStore(conn)
	scheduleSvc := scheduleDomain.NewService(scheduleStore, scheduleStore, scheduleStore, transactor, activitySvc)

	curtailmentStore := sqlstores.NewSQLCurtailmentStore(conn)
	// Curtailment operational metrics route through this single recorder.
	// Swap NoOpMetrics for the platform observability implementation once
	// the pipeline shape lands (OTel Meter, Prometheus, or DogStatsD).
	var curtailmentMetrics curtailmentDomain.Metrics = curtailmentDomain.NoOpMetrics{}
	curtailmentSvc := curtailmentDomain.NewService(curtailmentStore,
		curtailmentDomain.WithServiceMetrics(curtailmentMetrics),
		curtailmentDomain.WithAuditLogger(activitySvc),
	)
	curtailmentResponseProfileSvc := curtailmentDomain.NewResponseProfileService(curtailmentStore)

	sitesSvc := sitesDomain.NewService(siteStore, buildingStore, collectionStore, deviceStore, telemetryService, transactor, activitySvc)
	buildingsSvc := buildingsDomain.NewService(buildingStore, siteStore, collectionStore, deviceStore, telemetryService, transactor, activitySvc)
	infrastructureStore := sqlstores.NewSQLInfrastructureDeviceStore(conn)
	infrastructureSvc := infrastructureDomain.NewService(infrastructureStore, siteStore, infrastructureDomain.NewDefaultDriverRegistry(), transactor, activitySvc)

	// Register the schedule-conflict preflight filter on commandSvc so every
	// caller (manual API, schedule processor, future curtailment reconciler)
	// sees the same priority/manual-fallback semantics. Pre-pre-work this
	// only ran inline inside the schedule processor, leaving manual
	// SetPowerTarget calls free to race a running power-target schedule.
	commandSvc.RegisterFilter(commandDomain.NewScheduleConflictFilter(scheduleStore))
	// CurtailmentActiveFilter blocks non-curtailment commands on locked
	// devices; reconciler self-traffic bypasses via ActorCurtailment.
	commandSvc.RegisterFilter(commandDomain.NewCurtailmentActiveFilter(curtailmentStore))

	scheduleProcessor := scheduleDomain.NewProcessor(scheduleStore, scheduleStore, collectionStore, deviceStore, commandSvc, activitySvc)
	if err := scheduleProcessor.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start schedule processor: %w", err)
	}
	defer func() {
		if err := scheduleProcessor.Stop(); err != nil {
			slog.Error("failed to stop schedule processor", "error", err)
		}
	}()

	curtailmentRec := curtailmentReconciler.New(config.Curtailment, curtailmentStore, commandSvc, curtailmentReconciler.WithMetrics(curtailmentMetrics))
	if err := curtailmentRec.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start curtailment reconciler: %w", err)
	}
	defer func() {
		if err := curtailmentRec.Stop(); err != nil {
			slog.Error("failed to stop curtailment reconciler", "error", err)
		}
	}()

	mqttQueries, err := sqlc.Prepare(context.Background(), db.NewRetryDB(conn))
	if err != nil {
		return fmt.Errorf("failed to prepare curtailment mqtt sql queries: %w", err)
	}
	defer func() {
		if err := mqttQueries.Close(); err != nil {
			slog.Error("failed to close curtailment mqtt prepared queries", "error", err)
		}
	}()

	mqttSettingsStore := mqttingest.NewSQLCSettingsStore(mqttQueries)
	curtailmentAutomationSvc, err := curtailmentDomain.NewAutomationService(curtailmentDomain.AutomationServiceConfig{
		Store:       curtailmentStore,
		Profiles:    curtailmentResponseProfileSvc,
		SourceStore: mqttSettingsStore,
		Curtailment: curtailmentSvc,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize curtailment automation service: %w", err)
	}

	mqttSubscriber, err := mqttingest.NewSubscriber(mqttingest.Config{
		Store:          mqttingest.NewSQLCStore(mqttQueries),
		NewClient:      func() mqttingest.MQTTClient { return mqttclient.New() },
		Decryptor:      encryptSvc,
		Logger:         slog.Default(),
		SignalExecutor: curtailmentAutomationSvc,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize curtailment mqtt subscriber: %w", err)
	}
	if err := mqttSubscriber.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start curtailment mqtt subscriber: %w", err)
	}
	defer mqttSubscriber.Stop()
	mqttConnectionTester, err := mqttingest.NewMQTTConnectionTester(mqttingest.ConnectionTesterConfig{
		NewClient: func() mqttingest.MQTTClient { return mqttclient.New() },
	})
	if err != nil {
		return fmt.Errorf("failed to initialize curtailment mqtt connection tester: %w", err)
	}
	mqttSettingsSvc, err := mqttingest.NewSettingsService(mqttingest.SettingsServiceConfig{
		Store:            mqttSettingsStore,
		Cipher:           encryptSvc,
		Runtime:          mqttSubscriber,
		ConnectionTester: mqttConnectionTester,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize curtailment mqtt settings service: %w", err)
	}

	deviceResolver := deviceresolver.New(deviceStore)
	collectionSvc := collectionDomain.NewService(collectionStore, deviceStore, siteStore, buildingStore, transactor, deviceResolver.Resolve, telemetryService, activitySvc)
	foremanImportSvc := foremanImportDomain.NewService(poolsSvc, collectionSvc, deviceStore)

	grafanaClient := alertsDomain.NewGrafana(config.Metrics.Grafana)
	// fleet-api owns org channel storage + delivery; Grafana keeps only rule evaluation,
	// silences (rule pause / maintenance windows), and the internal history webhook.
	alertChannelStore := sqlstores.NewSQLAlertChannelStore(conn)
	alertsDeliverer := alertsDomain.NewDeliverer(alertChannelStore, encryptSvc, alertChannelStore, config.Metrics.AlertDestinations, config.PublicURL)
	alertsSvc := alertsDomain.NewService(grafanaClient, alertChannelStore, encryptSvc, alertsDeliverer, config.Metrics.AlertDestinations)

	middlewares := []server.Middleware{
		middleware.NewCORSMiddleware(config.HTTP.SuppressCors),
		middleware.TelemetryMiddleware{},
	}

	validateInterceptor := validate.NewInterceptor()

	li := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		interceptors.NewErrorStackTraceLoggingInterceptor(config.Log.Level),
		interceptors.NewRequestLoggingInterceptor(config.Log.Level, interceptors.RedactedRequestProcedures, interceptors.RedactedResponseProcedures),
		interceptors.NewFleetNodeAuthInterceptor(fleetNodeAuthSvc, interceptors.FleetNodeAuthenticatedProcedures),
		interceptors.NewAuthInterceptor(sessionSvc, userStore, userStore, apiKeySvc, permissionResolver, interceptors.UnauthenticatedProcedures, interceptors.SessionOnlyProcedures, interceptors.FleetNodeAuthenticatedProcedures),
		validateInterceptor,
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", health.NewHandler())
	mux.HandleFunc("/health/ready", health.NewReadyHandler(conn))
	if config.Metrics.Enabled {
		if config.Metrics.WebhookToken == "" {
			slog.Warn("FLEET_ALERTS_WEBHOOK_TOKEN is not set; alertmanager webhook will reject every delivery")
		}
		orgQueries := sqlc.New(db.NewRetryDB(conn))
		mux.Handle("POST "+alertmanagerwebhook.Path, alertmanagerwebhook.NewHandler(notificationHistoryStore, config.Metrics.WebhookToken, orgQueries, alertsDeliverer))
	}
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
	mux.Handle("/miners/{deviceIdentifier}/api/v1/{rest...}", minerProxyHandler.NewHandler(conn, sessionSvc, userStore, permissionResolver, encryptSvc))

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
	mux.Handle(pairingv1connect.NewPairingServiceHandler(pairing.NewHandler(pairingSvc, fleetNodeDiscoverySvc, fleetNodePairingSvc), li))
	mux.Handle(networkinfov1connect.NewNetworkInfoServiceHandler(networkinfo.NewHandler(pairingSvc), li))
	mux.Handle(fleetmanagementv1connect.NewFleetManagementServiceHandler(fleetmanagement.NewHandler(fleetMgmtSvc), li))
	mux.Handle(minercommandv1connect.NewMinerCommandServiceHandler(command.NewHandler(commandSvc), li))
	mux.Handle(poolsv1connect.NewPoolsServiceHandler(pools.NewHandler(poolsSvc), li))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(scheduleHandler.NewHandler(scheduleSvc), li))
	mux.Handle(curtailmentv1connect.NewCurtailmentServiceHandler(curtailmentHandler.NewHandlerWithAutomation(curtailmentSvc, curtailmentResponseProfileSvc, curtailmentAutomationSvc, mqttSettingsSvc), li))
	mux.Handle(sitesv1connect.NewSiteServiceHandler(sitesHandler.NewHandler(sitesSvc), li))
	mux.Handle(buildingsv1connect.NewBuildingServiceHandler(buildingsHandler.NewHandler(buildingsSvc), li))
	mux.Handle(infrastructurev1connect.NewInfrastructureServiceHandler(infrastructureHandler.NewHandler(infrastructureSvc), li))
	mux.Handle(fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(
		gateway.NewHandler(fleetNodeEnrollmentSvc, fleetNodeAuthSvc, fleetNodePairingSvc, fleetNodeControlRegistry, filesService),
		li,
		gateway.CommandArtifactUploadReadLimitOption(),
	))
	mux.Handle(fleetnodeadminv1connect.NewFleetNodeAdminServiceHandler(admin.NewHandler(fleetNodeEnrollmentSvc, fleetNodePairingSvc, fleetNodeDiscoverySvc), li))
	mux.Handle(collectionv1connect.NewDeviceCollectionServiceHandler(collectionHandler.NewHandler(collectionSvc), li))
	mux.Handle(device_setv1connect.NewDeviceSetServiceHandler(devicesetHandler.NewHandler(collectionSvc), li))
	mux.Handle(telemetryv1connect.NewTelemetryServiceHandler(telemetryHandler.NewHandler(telemetryService), li))
	mux.Handle(errorsv1connect.NewErrorQueryServiceHandler(errorqueryHandler.NewHandler(diagnosticsService), li))
	mux.Handle(foremanimportv1connect.NewForemanImportServiceHandler(foremanImportHandler.NewHandler(foremanImportSvc), li))
	mux.Handle(activityv1connect.NewActivityServiceHandler(activityHandler.NewHandler(activitySvc), li))
	mux.Handle(apikeyv1connect.NewApiKeyServiceHandler(apikeyHandler.NewHandler(apiKeySvc), li))
	mux.Handle(authzv1connect.NewAuthzServiceHandler(authzHandler.NewHandler(authz.NewService(conn, activitySvc)), li))
	mux.Handle(serverlogv1connect.NewServerLogServiceHandler(serverlogHandler.NewHandler(logging.DefaultBuffer()), li))

	alertHandler := alertsHandler.NewHandler(alertsSvc, notificationHistoryStore)
	mux.Handle(alertsv1connect.NewChannelServiceHandler(alertHandler, li))
	mux.Handle(alertsv1connect.NewRuleServiceHandler(alertHandler, li))
	mux.Handle(alertsv1connect.NewMaintenanceWindowServiceHandler(alertHandler, li))
	mux.Handle(alertsv1connect.NewHistoryServiceHandler(alertHandler, li))
	// Runtime capability probe so the prebuilt client can surface the Alerts
	// nav only when the sidecar this feature proxies is actually enabled.
	mux.HandleFunc("GET /api/v1/alerts/enabled", alertsHandler.NewEnabledHandler(config.Metrics.Enabled))

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

	handler = h2c.NewHandler(handler, newHTTP2Server(config.HTTP))
	httpServer := http.Server{
		Addr:              config.HTTP.Address,
		Handler:           handler,
		ReadHeaderTimeout: config.HTTP.ReadHeaderTimeout,
	}
	listener, err := net.Listen("tcp", config.HTTP.Address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.HTTP.Address, err)
	}

	// Started only once the listener is accepting: the first heartbeat is what
	// clears the Fleet Heartbeat Stale alert, so a crash-looping boot must not
	// keep refreshing it and mask a down fleet-api.
	if config.SystemMonitoring.Enabled {
		sysmonCtx, sysmonCancel := context.WithCancel(context.Background())
		defer sysmonCancel()
		go sysmon.New(config.SystemMonitoring, metricsProvider).Run(sysmonCtx)
	}

	err = httpServer.Serve(listener)
	if err != nil {
		return fmt.Errorf("server shutting down: %+v", err)
	}
	return nil
}

func newHTTP2Server(config HTTPConfig) *http2.Server {
	return &http2.Server{
		WriteByteTimeout: config.WriteByteTimeout,
	}
}
