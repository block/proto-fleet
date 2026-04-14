package testutil

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/infrastructure/files"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"go.uber.org/mock/gomock"

	"github.com/alecthomas/assert/v2"
	"github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/onboarding"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	"github.com/block/proto-fleet/server/internal/domain/token"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"

	pairingMocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
)

const (
	testClientTokenExpirationPeriod = 5 * time.Minute
	testMinerTokenExpirationPeriod  = 5 * time.Minute
	testMaxWorkers                  = 50
	testWorkerExecutionTimeout      = 30 * time.Second
	testMasterPollingInterval       = time.Second
	testBatchStatusUpdateInterval   = time.Second
	testDequeueLimit                = 500
	testMaxFailureRetries           = 5
	testSessionDuration             = 5 * time.Minute
	testSessionIDBytes              = 32
	testSessionCookieName           = "fleet_session"
	testSessionCleanupInterval      = time.Hour
)

type ServiceProvider struct {
	DB                     *sql.DB
	TokenService           *token.Service
	SessionService         *session.Service
	AuthService            *auth.Service
	ApiKeyService          *apikey.Service
	PairingService         *pairing.Service
	OnboardingService      *onboarding.Service
	CommandService         *command.Service
	ExecutionServiceCancel context.CancelFunc
	EncryptService         *encrypt.Service
	FleetManagementService *fleetmanagement.Service
	DeviceStore            *sqlstores.SQLDeviceStore
	UserStore              *sqlstores.SQLUserStore
	FilesService           *files.Service
	MinerService           *miner.Service
	PluginService          *plugins.Service
}

func NewServiceProvider(t *testing.T, db *sql.DB, config *Config) *ServiceProvider {
	tokenConfig := token.Config{
		ClientToken: token.AuthTokenConfig{
			SecretKey:        config.AuthTokenSecretKey,
			ExpirationPeriod: testClientTokenExpirationPeriod,
		},
		MinerTokenExpirationPeriod: testMinerTokenExpirationPeriod,
	}
	tokenService, err := token.NewService(tokenConfig)
	assert.NoError(t, err)

	encryptConfig := encrypt.Config{ServiceMasterKey: config.ServiceMasterKey}
	encryptService, err := encrypt.NewService(&encryptConfig)
	assert.NoError(t, err)

	transactor := sqlstores.NewSQLTransactor(db)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	poolStore := sqlstores.NewSQLPoolStore(db, encryptService)
	sessionStore := sqlstores.NewSQLSessionStore(db)

	sessionConfig := session.Config{
		Duration:        testSessionDuration,
		IDBytes:         testSessionIDBytes,
		CookieName:      testSessionCookieName,
		CookieSecure:    false,
		CookieSameSite:  "Strict",
		CleanupInterval: testSessionCleanupInterval,
	}
	sessionService := session.NewService(sessionConfig, sessionStore)

	// userStore implements both UserStore and UserManagementStore interfaces
	activityStore := sqlstores.NewSQLActivityStore(db)
	activitySvc := activity.NewService(activityStore)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(db)
	apiKeySvc := apikey.NewService(apiKeyStore, activitySvc)

	authService := auth.NewService(userStore, userStore, transactor, tokenService, sessionService, encryptService, activitySvc)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	listenerMock := pairingMocks.NewMockListener(ctrl)
	listenerMock.EXPECT().AddDevices(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

	// Use mock proto discoverer for testing instead of legacy implementation.
	// Note: This mock won't actually discover devices - tests requiring discovery
	// should set up EXPECT() calls with appropriate return values.
	// TODO: Replace with plugin-based test infrastructure when available.
	discoverer := NewMockProtoDiscoverer(ctrl)

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(db)

	filesService, err := files.NewService(files.Config{})
	assert.NoError(t, err)

	// Use mock proto pairer instead of legacy implementation
	protoPairer := NewMockProtoPairer(ctrl)

	// Create plugin manager and service for capabilities
	pluginConfig := &plugins.Config{
		Enabled: false, // Plugins disabled in tests by default
	}
	pluginManager := plugins.NewManager(pluginConfig)
	pluginService := plugins.NewService(pluginManager)

	minerService := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService, pluginManager)

	pairingService := pairing.NewService(discoveredDeviceStore, deviceStore, transactor, tokenService, discoverer, pluginService, listenerMock, protoPairer)

	commandConfig := &command.Config{
		MaxWorkers:                       testMaxWorkers,
		MasterPollingInterval:            testMasterPollingInterval,
		WorkerExecutionTimeout:           testWorkerExecutionTimeout,
		BatchStatusUpdatePollingInterval: testBatchStatusUpdateInterval,
	}

	dbMessageQueueConfig := queue.Config{
		DequeLimit:        testDequeueLimit,
		MaxFailureRetries: testMaxFailureRetries,
	}
	dbMessageQueue := queue.NewDatabaseMessageQueue(&dbMessageQueueConfig, db)

	executionServiceCtx, executionServiceCancel := context.WithCancel(t.Context())

	executionService := command.NewExecutionService(executionServiceCtx, commandConfig, db, dbMessageQueue, encryptService, tokenService, minerService, deviceStore, nil, filesService)
	err = executionService.Start(executionServiceCtx)
	assert.NoError(t, err)

	statusService := command.NewStatusService(db, dbMessageQueue)
	commandService := command.NewService(commandConfig, db, executionService, dbMessageQueue, statusService, encryptService, filesService, deviceStore, userStore, authService, nil, pluginService, activitySvc)

	onboardingService := onboarding.NewService(deviceStore, poolStore, userStore)

	errorStore := sqlstores.NewSQLErrorStore(db, transactor)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	fleetManagementService := fleetmanagement.NewService(deviceStore, discoveredDeviceStore, fleetmanagement.NewMockTelemetryCollector(), minerService, pluginService, poolStore, errorStore, collectionStore, commandService, activitySvc)

	return &ServiceProvider{
		DB:                     db,
		TokenService:           tokenService,
		SessionService:         sessionService,
		AuthService:            authService,
		ApiKeyService:          apiKeySvc,
		PairingService:         pairingService,
		OnboardingService:      onboardingService,
		CommandService:         commandService,
		ExecutionServiceCancel: executionServiceCancel,
		EncryptService:         encryptService,
		FleetManagementService: fleetManagementService,
		DeviceStore:            deviceStore,
		UserStore:              userStore,
		FilesService:           filesService,
		MinerService:           minerService,
		PluginService:          pluginService,
	}
}
