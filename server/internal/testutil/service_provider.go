package testutil

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"

	"github.com/btc-mining/proto-fleet/server/internal/domain/command"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/proto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"
	"github.com/golang/mock/gomock"

	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	"github.com/btc-mining/proto-fleet/server/internal/domain/onboarding"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"

	antminerWeb "github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	pairingAntminer "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/antminer"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing/mocks"
	pairingProto "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/proto"
)

type ServiceProvider struct {
	DB                     *sql.DB
	TokenService           *token.Service
	AuthService            *auth.Service
	PairingService         *pairing.Service
	OnboardingService      *onboarding.Service
	CommandService         *command.Service
	ExecutionServiceCancel context.CancelFunc
	EncryptService         *encrypt.Service
	FleetManagementService *fleetmanagement.Service
	DeviceStore            *sqlstores.SQLDeviceStore
	FilesService           *files.Service
}

func NewServiceProvider(t *testing.T, db *sql.DB, config *Config) *ServiceProvider {
	tokenConfig := token.Config{ClientToken: token.AuthTokenConfig{SecretKey: config.AuthTokenSecretKey, ExpirationPeriod: time.Minute * 5}, MinerTokenExpirationPeriod: time.Minute * 5}
	tokenService, err := token.NewService(tokenConfig)
	assert.NoError(t, err)

	encryptConfig := encrypt.Config{ServiceMasterKey: config.ServiceMasterKey}
	encryptService, err := encrypt.NewService(&encryptConfig)
	assert.NoError(t, err)

	// Initialize stores
	transactor := sqlstores.NewSQLTransactor(db)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	poolStore := sqlstores.NewSQLPoolStore(db, encryptService)

	authService := auth.NewService(userStore, transactor, tokenService, encryptService)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	listenerMock := mocks.NewMockListener(ctrl)
	listenerMock.EXPECT().AddDevices(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

	pairingConfig := pairing.Config{SecretKey: config.PairingSecretKey}

	protoDiscoverer := proto.NewDiscoverer()
	minerDiscoveryService, err := minerdiscovery.NewService(protoDiscoverer)
	assert.NoError(t, err)

	discoveredDeviceStore := minerdiscovery.NewInMemoryDiscoveredDeviceStore()

	protoPairer := pairingProto.NewService(transactor, deviceStore, pairingConfig)
	antminerPairer := pairingAntminer.NewService(transactor, deviceStore, encryptService, antminerWeb.NewService())

	pairingService := pairing.NewService(discoveredDeviceStore, deviceStore, transactor, tokenService, minerDiscoveryService, listenerMock, protoPairer, antminerPairer)

	commandConfig := &command.Config{MaxWorkers: 50, MasterPollingInterval: time.Second, WorkerExecutionTimeout: 30 * time.Second, BatchStatusUpdatePollingInterval: time.Second}

	dbMessageQueueConfig := queue.Config{DequeLimit: 500, MaxFailureRetries: 5}
	dbMessageQueue := queue.NewDatabaseMessageQueue(&dbMessageQueueConfig, db)

	executionServiceCtx, executionServiceCancel := context.WithCancel(t.Context())

	filesService, err := files.NewService()
	assert.NoError(t, err)

	minerService := miner.NewMinerService(db, encryptService, filesService)
	executionService := command.NewExecutionService(executionServiceCtx, commandConfig, db, dbMessageQueue, encryptService, tokenService, minerService)
	err = executionService.Start(executionServiceCtx)
	assert.NoError(t, err)

	statusService := command.NewStatusService(db, dbMessageQueue)
	commandService := command.NewService(commandConfig, db, executionService, dbMessageQueue, statusService, encryptService, filesService)

	onboardingService := onboarding.NewService(deviceStore, poolStore)

	fleetManagementService := fleetmanagement.NewService(deviceStore, fleetmanagement.NewMockTelemetryCollector(), minerService)

	return &ServiceProvider{
		DB:                     db,
		TokenService:           tokenService,
		AuthService:            authService,
		PairingService:         pairingService,
		OnboardingService:      onboardingService,
		CommandService:         commandService,
		ExecutionServiceCancel: executionServiceCancel,
		EncryptService:         encryptService,
		FleetManagementService: fleetManagementService,
		DeviceStore:            deviceStore,
		FilesService:           filesService,
	}
}
