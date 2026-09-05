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
	"strings"
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
	"github.com/block/proto-fleet/server/generated/grpc/instance/v1/instancev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/inventory/v1/inventoryv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/maintenance/v1/maintenancev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/schedule/v1/schedulev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/serverlog/v1/serverlogv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/sitemap/v1/sitemapv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/sites/v1/sitesv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/telemetry/v1/telemetryv1connect"
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
	inventoryDomain "github.com/block/proto-fleet/server/internal/domain/inventory"
	maintenanceDomain "github.com/block/proto-fleet/server/internal/domain/maintenance"
	onboardingDomain "github.com/block/proto-fleet/server/internal/domain/onboarding"
	pairingDomain "github.com/block/proto-fleet/server/internal/domain/pairing"
	poolsDomain "github.com/block/proto-fleet/server/internal/domain/pools"
	scheduleDomain "github.com/block/proto-fleet/server/internal/domain/schedule"
	sitemapDomain "github.com/block/proto-fleet/server/internal/domain/sitemap"
	sitesDomain "github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/scheduler"
	tokenDomain "github.com/block/proto-fleet/server/internal/domain/token"
	updatesDomain "github.com/block/proto-fleet/server/internal/domain/updates"
	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/ha/deployment"
	"github.com/block/proto-fleet/server/internal/ha/readiness"
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
	inventoryHandler "github.com/block/proto-fleet/server/internal/handlers/inventory"
	maintenanceHandler "github.com/block/proto-fleet/server/internal/handlers/maintenance"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	minerProxyHandler "github.com/block/proto-fleet/server/internal/handlers/minerproxy"
	"github.com/block/proto-fleet/server/internal/handlers/networkinfo"
	"github.com/block/proto-fleet/server/internal/handlers/onboarding"
	"github.com/block/proto-fleet/server/internal/handlers/pairing"
	"github.com/block/proto-fleet/server/internal/handlers/pools"
	scheduleHandler "github.com/block/proto-fleet/server/internal/handlers/schedule"
	serverlogHandler "github.com/block/proto-fleet/server/internal/handlers/serverlog"
	sitemapHandler "github.com/block/proto-fleet/server/internal/handlers/sitemap"
	sitesHandler "github.com/block/proto-fleet/server/internal/handlers/sites"
	telemetryHandler "github.com/block/proto-fleet/server/internal/handlers/telemetry"
	updatesHandler "github.com/block/proto-fleet/server/internal/handlers/updates"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/mqttclient"
	"github.com/block/proto-fleet/server/internal/infrastructure/server"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const shutdownTimeout = 10 * time.Second

// version is overwritten at release build time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	cli := &fleetdCLI{}
	parser := kong.Must(
		cli,
		kong.Name("fleetd"),
		kong.Configuration(fleetdYAMLLoader, "/etc/fleetd/config.yaml"),
		kong.BindTo(context.Background(), (*context.Context)(nil)),
	)
	kctx, err := parser.Parse(normalizeFleetdArgs(os.Args[1:]))
	parser.FatalIfErrorf(err)

	// Kong applies flag defaults tree-wide, so for non-server commands
	// cli.Server.Log carries the compiled defaults (info level, 1000 buffer).
	logging.InitLogger(cli.Server.Log)

	if err := kctx.Run(); err != nil {
		slog.Error(fmt.Sprintf("%+v", err))
		os.Exit(1)
	}
}

// normalizeFleetdArgs preserves the pre-command CLI contract. Kong's default
// subcommand handles an empty invocation, but flags must be routed explicitly.
func normalizeFleetdArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	for _, path := range fleetdCommandPaths {
		if args[0] == strings.Fields(path)[0] {
			return args
		}
	}
	return append([]string{"server"}, args...)
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
	infrastructurev1connect.InfrastructureServiceName,
	sitemapv1connect.SiteMapServiceName,
	curtailmentv1connect.CurtailmentServiceName,
	device_setv1connect.DeviceSetServiceName,
	instancev1connect.InstanceUpdateServiceName,
	maintenancev1connect.MaintenanceServiceName,
	inventoryv1connect.InventoryServiceName,
}

func start(config *Config) (result error) {
	if err := config.HA.Validate(); err != nil {
		return fmt.Errorf("invalid HA configuration: %w", err)
	}
	if err := validateHAHTTPAddress(*config); err != nil {
		return err
	}
	if config.HA.Enabled {
		if err := config.DB.ValidateHA(); err != nil {
			return fmt.Errorf("invalid HA database configuration: %w", err)
		}
	}
	// Construct one configured registry before starting services. The CRUD
	// service uses it now; the Phase 5 reconciler will share this same instance.
	infrastructureDriverRegistry, err := infrastructureDomain.NewConfiguredDriverRegistry(config.Infrastructure)
	if err != nil {
		return fmt.Errorf("configure infrastructure drivers: %w", err)
	}

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
	inventoryStore := sqlstores.NewSQLInventoryStore(conn)
	maintenanceStore := sqlstores.NewSQLMaintenanceStore(conn)
	inventorySvc := inventoryDomain.NewService(inventoryStore, transactor, activitySvc)
	maintenanceSvc := maintenanceDomain.NewService(maintenanceStore, maintenanceStore, inventoryStore, transactor, activitySvc)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(conn)
	apiKeySvc := apikeyDomain.NewService(apiKeyStore, activitySvc)

	fleetNodeEnrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(conn)
	fleetNodeEnrollmentSvc := enrollment.NewService(fleetNodeEnrollmentStore, apiKeySvc, transactor, activitySvc)
	fleetNodePairingStore := sqlstores.NewSQLFleetNodePairingStore(conn)
	fleetNodePairingSvc := fleetnodepairing.NewService(fleetNodePairingStore, fleetNodeEnrollmentStore, transactor)
	fleetNodeControlRegistry := control.NewRegistry()
	fleetNodeEnrollmentSvc.WithControlStreamInvalidator(fleetNodeControlRegistry.RevokeSession)
	fleetNodeDiscoverySvc := fleetnodediscovery.NewService(fleetNodeControlRegistry, fleetNodeEnrollmentSvc)
	fleetNodeAuthStore := sqlstores.NewSQLFleetNodeAuthStore(conn)
	fleetNodeAuthSvc := fleetnodeauth.NewService(fleetNodeAuthStore, fleetNodeEnrollmentStore, apiKeySvc)

	tokenSvc, err := tokenDomain.NewService(config.Auth)
	if err != nil {
		return err
	}

	// Initialize session store and service
	sessionStore := sqlstores.NewSQLSessionStore(conn)
	sessionSvc := sessionDomain.NewServiceWithValidationFailureClassifier(
		config.Session,
		sessionStore,
		db.IsFailoverPostgresError,
	)

	// userStore implements both UserStore and UserManagementStore interfaces
	authSvc := authDomain.NewService(userStore, userStore, transactor, tokenSvc, sessionSvc, encryptSvc, activitySvc, permissionResolver)

	identityStateCleanup := newBackgroundLoop(func(ctx context.Context) {
		cleanupInterval := sessionSvc.CleanupInterval()
		reportProgress := runtimejobs.TrackProgress(ctx, cleanupInterval)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if deleted, err := sessionSvc.CleanupExpired(ctx); err != nil {
					slog.Error("failed to cleanup expired sessions", "error", err)
				} else if deleted > 0 {
					slog.Debug("cleaned up expired sessions", "count", deleted)
				}
				if swept, err := fleetNodeEnrollmentSvc.SweepExpired(ctx); err != nil {
					slog.Error("failed to sweep expired fleet node enrollments", "error", err)
				} else if swept > 0 {
					slog.Debug("swept expired fleet node enrollments", "count", swept)
				}
				if challenges, sessions, err := fleetNodeAuthSvc.SweepExpired(ctx); err != nil {
					slog.Error("failed to sweep expired fleet node auth state", "error", err)
				} else if challenges > 0 || sessions > 0 {
					slog.Debug("swept expired fleet node auth state", "challenges", challenges, "sessions", sessions)
				}
				reportProgress()
			case <-ctx.Done():
				return
			}
		}
	})

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
	commandArtifactCleanup := newBackgroundLoop(func(ctx context.Context) {
		cleanupInterval := filesService.CommandArtifactCleanupInterval()
		reportProgress := runtimejobs.TrackProgress(ctx, cleanupInterval)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		runCommandArtifactSweep()
		reportProgress()

		for {
			select {
			case <-ticker.C:
				runCommandArtifactSweep()
				reportProgress()
			case <-ctx.Done():
				return
			}
		}
	})
	minerService := miner.NewMinerService(conn, userStore, encryptSvc, filesService, pluginManager).
		WithCommandSender(fleetNodeControlRegistry)

	// Create diagnostics service for error polling and auto-closing stale errors
	errorStore := sqlstores.NewSQLErrorStore(conn, transactor)
	diagnosticsService := diagnostics.NewService(config.Diagnostics, errorStore, transactor).
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

	dbMessageQueue := queue.NewDatabaseMessageQueue(&config.Queue, conn)

	// Ensure plugin cleanup on shutdown
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(),
			time.Duration(config.Plugins.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()
		if err := pluginService.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown plugin service", "error", err)
		}
	}()

	executionService := commandDomain.NewExecutionService(&config.Command, conn, dbMessageQueue, encryptSvc, tokenSvc, minerService, deviceStore, telemetryService, filesService)
	executionService.WithMetricsEmitter(metricsProvider)

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
	// Filtered "select all" command dispatch resolves its MinerListFilter through
	// the fleetmanagement resolver; wire it now that fleetMgmtSvc exists (it
	// depends on commandSvc, so this can't be passed to NewService).
	commandSvc.SetDeviceIdentifierResolver(fleetMgmtSvc)
	defer fleetMgmtSvc.WaitForPendingUnpairs(shutdownTimeout)
	onboardingSvc := onboardingDomain.NewService(deviceStore, poolStore, userStore)
	poolsSvc := poolsDomain.NewService(poolStore, transactor, config.Pools, activitySvc)
	scheduleStore := sqlstores.NewSQLScheduleStore(conn)
	scheduleSvc := scheduleDomain.NewService(scheduleStore, scheduleStore, scheduleStore, transactor, activitySvc)

	curtailmentStore := sqlstores.NewSQLCurtailmentStore(conn)
	infrastructureStore := sqlstores.NewSQLInfrastructureDeviceStore(conn)
	facilityFanController := curtailmentDomain.NewFacilityFanController(
		infrastructureStore,
		siteStore,
		infrastructureDriverRegistry,
		activitySvc,
	)
	// Curtailment operational metrics route through this single recorder.
	// Swap NoOpMetrics for the platform observability implementation once
	// the pipeline shape lands (OTel Meter, Prometheus, or DogStatsD).
	var curtailmentMetrics curtailmentDomain.Metrics = curtailmentDomain.NoOpMetrics{}
	curtailmentSvc := curtailmentDomain.NewService(curtailmentStore,
		curtailmentDomain.WithServiceMetrics(curtailmentMetrics),
		curtailmentDomain.WithAuditLogger(activitySvc),
		curtailmentDomain.WithFacilityFanController(facilityFanController),
	)
	curtailmentResponseProfileSvc := curtailmentDomain.NewResponseProfileService(curtailmentStore)

	sitesSvc := sitesDomain.NewService(siteStore, buildingStore, collectionStore, deviceStore, telemetryService, transactor, activitySvc)
	buildingsSvc := buildingsDomain.NewService(buildingStore, siteStore, collectionStore, deviceStore, telemetryService, transactor, activitySvc)
	infrastructureSvc := infrastructureDomain.NewService(infrastructureStore, siteStore, infrastructureDriverRegistry, transactor, activitySvc)
	sitemapSvc := sitemapDomain.NewService(siteStore, buildingStore, collectionStore, deviceStore, fleetMgmtSvc, transactor, activitySvc)

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

	curtailmentRec := curtailmentReconciler.New(
		config.Curtailment,
		curtailmentStore,
		commandSvc,
		curtailmentReconciler.WithMetrics(curtailmentMetrics),
		curtailmentReconciler.WithFacilityFanController(facilityFanController),
		curtailmentReconciler.WithFacilityFanAlertEmitter(metricsProvider),
		curtailmentReconciler.WithDispatchPermissionResolver(permissionResolver),
		// The confirmation fast path samples device metrics through the
		// telemetry service's read-only seam (shared worker pool, no
		// persistence side effects).
		curtailmentReconciler.WithConfirmationSampler(telemetryService),
	)

	mqttQueries, err := db.NewPreparedQuerier(context.Background(), conn)
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
		RigConfigApplier: commandSvc,
		RigConfigStore:   mqttingest.NewSQLCRigConfigReconciliationStore(mqttQueries),
	})
	if err != nil {
		return fmt.Errorf("failed to initialize curtailment mqtt settings service: %w", err)
	}
	pairingSvc.WithRigConfigReapplier(mqttSettingsSvc.ReapplyRigConfigBestEffort)
	fleetNodePairingSvc.WithRigConfigReapplier(mqttSettingsSvc.ReapplyRigConfigBestEffort)

	// Feeds the MQTT curtailment default alert rules; skipped when the
	// metrics pipeline is off so its periodic queries aren't wasted work.
	var curtailmentAlertMetrics runtimejobs.Lifecycle
	if metricsProvider.Enabled() {
		alertMetricsLoop, err := curtailmentDomain.NewAlertMetricsLoop(curtailmentDomain.AlertMetricsConfig{
			Sources:           mqttingest.NewSQLCStore(mqttQueries),
			Runtime:           mqttSubscriber,
			ActiveCurtailment: curtailmentStore,
			Emitter:           metricsProvider,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize curtailment alert metrics loop: %w", err)
		}
		curtailmentAlertMetrics = alertMetricsLoop
	}

	deviceResolver := deviceresolver.New(deviceStore)
	collectionSvc := collectionDomain.NewService(collectionStore, deviceStore, siteStore, buildingStore, transactor, deviceResolver.Resolve, telemetryService, activitySvc)
	foremanImportSvc := foremanImportDomain.NewService(poolsSvc, collectionSvc, deviceStore)

	grafanaClient := alertsDomain.NewGrafana(config.Metrics.Grafana)
	// fleet-api owns org channel storage + delivery; Grafana keeps only rule evaluation,
	// silences (rule pause / maintenance windows), and the internal history webhook.
	alertChannelStore := sqlstores.NewSQLAlertChannelStore(conn)
	alertRouteStore := sqlstores.NewSQLAlertRouteStore(conn)
	alertRuleConfigStore := sqlstores.NewSQLAlertRuleConfigStore(conn)
	alertMaintenanceWindowStore := sqlstores.NewSQLAlertMaintenanceWindowStore(conn)
	alertsDeliverer := alertsDomain.NewDeliverer(alertChannelStore, alertRouteStore, alertMaintenanceWindowStore, encryptSvc, alertChannelStore, config.Metrics.AlertDestinations, config.PublicURL)
	alertScopeLookup := alertScopeStores{sites: siteStore, buildings: buildingStore, sets: collectionStore}
	alertsSvc := alertsDomain.NewService(grafanaClient, alertChannelStore, alertRouteStore, alertRuleConfigStore, alertMaintenanceWindowStore, encryptSvc, alertsDeliverer, alertScopeLookup, config.Metrics.AlertDestinations)

	// Both updates URLs end up inside a copy-paste upgrade command, so an
	// http:// base must fail startup (explicit Validate, like Plugins above —
	// kong only auto-validates flag leaves, not embedded config structs).
	if err := config.Updates.Validate(); err != nil {
		return fmt.Errorf("invalid updates configuration: %w", err)
	}
	// The checker is constructed even when disabled: the updates service still
	// answers version/status calls, reading the zero snapshot as "no offer".
	releaseChecker := updatesDomain.NewChecker(config.Updates, version)
	updatesSvc := updatesDomain.NewService(config.Updates, version, releaseChecker,
		db.NewFailoverResettingQuerier(db.NewRetryDB(conn)), activitySvc)

	// The public listener is bound before this group starts. This channel keeps
	// the first system heartbeat from clearing its stale alert before then.
	listenerBound := make(chan struct{})
	var systemMonitoring runtimejobs.Lifecycle
	if config.SystemMonitoring.Enabled {
		collector := sysmon.New(config.SystemMonitoring, metricsProvider)
		systemMonitoring = newBackgroundLoop(func(ctx context.Context) {
			select {
			case <-listenerBound:
				collector.Run(ctx)
			case <-ctx.Done():
			}
		})
	}
	var haReadiness runtimejobs.Lifecycle
	if config.HA.Enabled && metricsProvider.Enabled() {
		collector := readiness.New(func(ctx context.Context) (bool, error) {
			report, err := deployment.StatusWithDatabase(ctx, "/etc/proto-fleet/ha/node.env", conn)
			if err != nil {
				return false, err
			}
			return report.Control != nil && report.Control.FailoverReady, nil
		}, metricsProvider)
		haReadiness = newBackgroundLoop(func(ctx context.Context) {
			select {
			case <-listenerBound:
				collector.Run(ctx)
			case <-ctx.Done():
			}
		})
	}

	chunkedMgr := firmwareHandler.NewChunkedUploadManager()
	chunkedUploadCleanup := newBackgroundLoop(func(ctx context.Context) {
		chunkedMgr.StartCleanup(ctx, config.Files.ChunkedUploadSessionTTL)
	})
	// nil-when-disabled mirrors systemMonitoring: newRuntimeJobs skips
	// optional jobs entirely instead of starting a lifecycle that no-ops.
	var releaseCheckerJob runtimejobs.Lifecycle
	if config.Updates.Enabled {
		releaseCheckerJob = releaseChecker
	}
	jobs, err := newRuntimeJobs(runtimeJobLifecycles{
		identityStateCleanup:      identityStateCleanup,
		commandArtifactCleanup:    commandArtifactCleanup,
		diagnosticsErrorCloser:    diagnosticsService,
		telemetry:                 telemetryService,
		ipScanner:                 ipScannerService,
		commandExecution:          executionService,
		scheduleProcessor:         scheduleProcessor,
		curtailmentReconciler:     curtailmentRec,
		curtailmentMQTTSubscriber: mqttSubscriber,
		curtailmentRigConfig:      mqttSettingsSvc,
		curtailmentAlertMetrics:   curtailmentAlertMetrics,
		chunkedUploadCleanup:      chunkedUploadCleanup,
		systemMonitoring:          systemMonitoring,
		haReadiness:               haReadiness,
		releaseChecker:            releaseCheckerJob,
	})
	if err != nil {
		return err
	}
	runtimeJobGroup, err := runtimejobs.NewGroup(jobs)
	if err != nil {
		return fmt.Errorf("create runtime job group: %w", err)
	}
	fleetRuntime, closeHA, err := ha.NewConfiguredRuntime(
		config.HA,
		conn,
		runtimeJobGroup,
		executionService.IsRunning,
	)
	if err != nil {
		return fmt.Errorf("create Fleet runtime: %w", err)
	}
	defer func() {
		if err := closeHA(); err != nil {
			slog.Error("Failed to close HA services", "error", err)
		}
	}()
	defer func() {
		stopRuntimeJobGroupAfterRun(result, runtimeJobGroup, executionService, shutdownTimeout)
	}()

	middlewares := []server.Middleware{
		middleware.NewCORSMiddleware(config.HTTP.SuppressCors),
		middleware.TelemetryMiddleware{TrustIncomingTraces: config.FleetTelemetry.TrustIncomingTraces},
	}
	activeHTTP := middleware.NewActiveMiddleware(fleetRuntime)

	validateInterceptor := validate.NewInterceptor()

	li := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		interceptors.NewActiveInterceptor(fleetRuntime),
		interceptors.NewErrorStackTraceLoggingInterceptor(config.Log.Level),
		interceptors.NewRequestLoggingInterceptor(config.Log.Level, interceptors.RedactedRequestProcedures, interceptors.RedactedResponseProcedures),
		interceptors.NewFleetNodeAuthInterceptor(fleetNodeAuthSvc, interceptors.FleetNodeAuthenticatedProcedures),
		interceptors.NewAuthInterceptor(sessionSvc, userStore, userStore, apiKeySvc, permissionResolver, interceptors.UnauthenticatedProcedures, interceptors.SessionOnlyProcedures, interceptors.FleetNodeAuthenticatedProcedures),
		validateInterceptor,
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", health.NewHandler(version))
	mux.HandleFunc("/health/ready", health.NewReadyHandler(conn, fleetRuntime))
	mux.HandleFunc("/health/active", health.NewActiveHandler(version, fleetRuntime))
	if config.HA.Enabled {
		mux.HandleFunc("/health/ha", health.NewHAHandler(version, fleetRuntime))
		mux.HandleFunc("/health/passive", health.NewPassiveHandler(fleetRuntime))
	}
	if config.Metrics.Enabled {
		if config.Metrics.WebhookToken == "" {
			slog.Warn("FLEET_ALERTS_WEBHOOK_TOKEN is not set; alertmanager webhook will reject every delivery")
		}
		orgQueries := db.NewFailoverResettingQuerier(db.NewRetryDB(conn))
		mux.Handle("POST "+alertmanagerwebhook.Path, activeHTTP.Wrap(alertmanagerwebhook.NewHandler(notificationHistoryStore, config.Metrics.WebhookToken, orgQueries, alertsDeliverer)))
	}
	mux.Handle("/api/v1/firmware/upload", activeHTTP.Wrap(firmwareHandler.NewUploadHandler(filesService, sessionSvc, userStore, activitySvc, permissionResolver)))
	mux.Handle("/api/v1/firmware/check", activeHTTP.Wrap(firmwareHandler.NewCheckHandler(filesService, sessionSvc, userStore)))
	mux.Handle("GET /api/v1/firmware/config", activeHTTP.Wrap(firmwareHandler.NewConfigHandler(filesService, sessionSvc, userStore, config.Files)))
	mux.Handle("POST /api/v1/firmware/upload/chunked", activeHTTP.Wrap(firmwareHandler.NewInitiateHandler(chunkedMgr, filesService, sessionSvc, userStore, permissionResolver)))
	mux.Handle("PUT /api/v1/firmware/upload/chunked/{uploadId}", activeHTTP.Wrap(firmwareHandler.NewChunkHandler(chunkedMgr, sessionSvc, userStore, permissionResolver)))
	mux.Handle("POST /api/v1/firmware/upload/chunked/{uploadId}/complete", activeHTTP.Wrap(firmwareHandler.NewCompleteHandler(chunkedMgr, filesService, sessionSvc, userStore, activitySvc, permissionResolver)))
	mux.Handle("GET /api/v1/firmware/files", activeHTTP.Wrap(firmwareHandler.NewListFilesHandler(filesService, sessionSvc, userStore)))
	mux.Handle("PATCH /api/v1/firmware/files/{fileId}", activeHTTP.Wrap(firmwareHandler.NewUpdateMetadataHandler(filesService, sessionSvc, userStore, activitySvc, permissionResolver)))
	mux.Handle("DELETE /api/v1/firmware/files/{fileId}", activeHTTP.Wrap(firmwareHandler.NewDeleteFileHandler(filesService, sessionSvc, userStore, permissionResolver)))
	mux.Handle("DELETE /api/v1/firmware/files", activeHTTP.Wrap(firmwareHandler.NewDeleteAllFilesHandler(filesService, sessionSvc, userStore, permissionResolver)))
	mux.Handle("/miners/{deviceIdentifier}/api/v1/{rest...}", activeHTTP.Wrap(minerProxyHandler.NewHandler(conn, sessionSvc, userStore, permissionResolver, encryptSvc)))

	if len(reflectEnabledServices) != 0 {
		slog.Debug("enabling reflection", "services", reflectEnabledServices)
		reflector := grpcreflect.NewStaticReflector(reflectEnabledServices...)
		path, handler := grpcreflect.NewHandlerV1(reflector)
		mux.Handle(path, activeHTTP.Wrap(handler))
		path, handler = grpcreflect.NewHandlerV1Alpha(reflector)
		mux.Handle(path, activeHTTP.Wrap(handler))
	}

	mux.Handle(authv1connect.NewAuthServiceHandler(auth.NewHandler(authSvc), li))
	mux.Handle(onboardingv1connect.NewOnboardingServiceHandler(onboarding.NewHandler(authSvc, onboardingSvc), li))
	mux.Handle(pairingv1connect.NewPairingServiceHandler(pairing.NewHandler(pairingSvc, fleetNodeDiscoverySvc, fleetNodePairingSvc), li))
	mux.Handle(networkinfov1connect.NewNetworkInfoServiceHandler(networkinfo.NewHandler(pairingSvc), li))
	mux.Handle(fleetmanagementv1connect.NewFleetManagementServiceHandler(fleetmanagement.NewHandler(fleetMgmtSvc), li))
	mux.Handle(minercommandv1connect.NewMinerCommandServiceHandler(command.NewHandler(commandSvc), li))
	mux.Handle(poolsv1connect.NewPoolsServiceHandler(pools.NewHandler(poolsSvc), li))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(scheduleHandler.NewHandler(scheduleSvc), li))
	mux.Handle(curtailmentv1connect.NewCurtailmentServiceHandler(
		curtailmentHandler.NewHandlerWithAutomation(curtailmentSvc, curtailmentResponseProfileSvc, curtailmentAutomationSvc, mqttSettingsSvc),
		li,
		curtailmentHandler.RequestReadLimitOption(),
	))
	mux.Handle(sitesv1connect.NewSiteServiceHandler(sitesHandler.NewHandler(sitesSvc), li))
	mux.Handle(buildingsv1connect.NewBuildingServiceHandler(buildingsHandler.NewHandler(buildingsSvc), li))
	mux.Handle(infrastructurev1connect.NewInfrastructureServiceHandler(infrastructureHandler.NewHandler(infrastructureSvc), li))
	mux.Handle(sitemapv1connect.NewSiteMapServiceHandler(
		sitemapHandler.NewHandler(sitemapSvc),
		li,
		connect.WithReadMaxBytes(sitemapDomain.MaxImportBytes+1024),
	))
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
	mux.Handle(maintenancev1connect.NewMaintenanceServiceHandler(maintenanceHandler.NewHandler(maintenanceSvc), li))
	mux.Handle(inventoryv1connect.NewInventoryServiceHandler(inventoryHandler.NewHandler(inventorySvc), li))
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
	mux.Handle("GET /api/v1/alerts/enabled", activeHTTP.Wrap(alertsHandler.NewEnabledHandler(config.Metrics.Enabled)))

	mux.Handle(instancev1connect.NewInstanceUpdateServiceHandler(updatesHandler.NewHandler(updatesSvc), li))

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
	close(listenerBound)
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Error("failed to close HTTP listener", "error", err)
		}
	}()

	return serveFleetRuntime(&httpServer, listener, fleetRuntime, shutdownTimeout)
}

func newHTTP2Server(config HTTPConfig) *http2.Server {
	return &http2.Server{
		WriteByteTimeout: config.WriteByteTimeout,
	}
}

// alertScopeStores adapts the site/building/device-set stores to the alerts
// domain's ScopeLookup for rule-scope ownership validation.
type alertScopeStores struct {
	sites     *sqlstores.SQLSiteStore
	buildings *sqlstores.SQLBuildingStore
	sets      *sqlstores.SQLCollectionStore
}

func (a alertScopeStores) SitesByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error) {
	return a.sites.SitesByIDs(ctx, orgID, ids)
}

func (a alertScopeStores) BuildingsByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error) {
	return a.buildings.BuildingsByIDs(ctx, orgID, ids)
}

func (a alertScopeStores) DeviceSetsByIDs(ctx context.Context, orgID int64, setType string, ids []int64) ([]int64, error) {
	return a.sets.DeviceSetsByIDs(ctx, orgID, setType, ids)
}
