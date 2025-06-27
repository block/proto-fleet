package testutil

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/command"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/proto"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"

	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	"github.com/btc-mining/proto-fleet/server/internal/domain/onboarding"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"

	pairingAntminer "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/antminer"
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
}

func NewServiceProvider(t *testing.T, db *sql.DB, config *Config) *ServiceProvider {
	tokenConfig := token.Config{ClientToken: token.AuthTokenConfig{SecretKey: config.AuthTokenSecretKey, ExpirationPeriod: time.Minute * 5}, MinerTokenExpirationPeriod: time.Minute * 5}
	tokenService, err := token.NewService(tokenConfig)
	assert.NoError(t, err)

	encryptConfig := encrypt.Config{ServiceMasterKey: config.ServiceMasterKey}
	encryptService, err := encrypt.NewService(&encryptConfig)
	assert.NoError(t, err)

	authService := auth.NewService(db, tokenService, encryptService)

	pairingConfig := pairing.Config{SecretKey: config.PairingSecretKey}

	protoDiscoverer := proto.NewDiscoverer()
	minerDiscoveryService, err := minerdiscovery.NewService(protoDiscoverer)
	assert.NoError(t, err)

	deviceStore := minerdiscovery.NewInMemoryDiscoveredDeviceStore()

	protoPairer := pairingProto.NewService(db, pairingConfig)
	antminerPairer := pairingAntminer.NewService(db, encryptService)

	pairingService := pairing.NewService(deviceStore, db, tokenService, minerDiscoveryService, protoPairer, antminerPairer)

	commandConfig := &command.Config{MaxWorkers: 50, MasterPollingInterval: time.Second, WorkerExecutionTimeout: 30 * time.Second, BatchStatusUpdatePollingInterval: time.Second}

	dbMessageQueueConfig := queue.Config{DequeLimit: 500, MaxFailureRetries: 5}
	dbMessageQueue := queue.NewDatabaseMessageQueue(&dbMessageQueueConfig, db)

	executionServiceCtx, executionServiceCancel := context.WithCancel(t.Context())

	executionService := command.NewExecutionService(executionServiceCtx, commandConfig, db, dbMessageQueue, encryptService, tokenService)
	statusService := command.NewStatusService(db, dbMessageQueue)
	commandService := command.NewService(commandConfig, db, executionService, dbMessageQueue, statusService)

	onboardingService := onboarding.NewService(db)

	return &ServiceProvider{
		DB:                     db,
		TokenService:           tokenService,
		AuthService:            authService,
		PairingService:         pairingService,
		OnboardingService:      onboardingService,
		CommandService:         commandService,
		ExecutionServiceCancel: executionServiceCancel,
		EncryptService:         encryptService,
	}
}
