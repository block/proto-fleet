package testutil

import (
	"database/sql"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	"github.com/btc-mining/proto-fleet/server/internal/domain/command"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/proto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/onboarding"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
)

type ServiceProvider struct {
	DB                *sql.DB
	TokenService      *token.Service
	AuthService       *auth.Service
	PairingService    *pairing.Service
	OnboardingService *onboarding.Service
	CommandService    *command.Service
}

func NewServiceProvider(t *testing.T, db *sql.DB) *ServiceProvider {
	secretKey := "0000000000000000000000000000000000000000000"
	tokenConfig := token.Config{ClientToken: token.AuthTokenConfig{SecretKey: secretKey, ExpirationPeriod: time.Minute * 5}, MinerTokenExpirationPeriod: time.Minute * 5}
	tokenService, err := token.NewService(tokenConfig)
	assert.NoError(t, err)

	serviceMasterKey := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	encryptConfig := encrypt.Config{ServiceMasterKey: serviceMasterKey}
	encryptService, err := encrypt.NewService(&encryptConfig)
	assert.NoError(t, err)

	authService := auth.NewService(db, tokenService, encryptService)

	minerClientService := client.NewService()
	pairingConfig := pairing.Config{SecretKey: secretKey}
	protoDiscoverer := proto.NewDiscoverer(minerClientService)
	discoveryService, _ := minerdiscovery.NewService(protoDiscoverer)

	pairingService := pairing.NewService(db, pairingConfig, tokenService, discoveryService)

	onboardingService := onboarding.NewService(db)

	commandService := command.NewService(db, minerClientService, tokenService, encryptService)

	return &ServiceProvider{
		DB:                db,
		TokenService:      tokenService,
		AuthService:       authService,
		PairingService:    pairingService,
		OnboardingService: onboardingService,
		CommandService:    commandService,
	}
}
