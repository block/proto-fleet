package testutil

import (
	"database/sql"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
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
}

func NewServiceProvider(t *testing.T, db *sql.DB) *ServiceProvider {
	tokenConfig := token.Config{ClientToken: token.AuthTokenConfig{SecretKey: "00000000000000000000000000000000000000000000", ExpirationPeriod: time.Minute * 5}, MinerTokenExpirationPeriod: time.Minute * 5}
	tokenService, err := token.NewService(tokenConfig)
	assert.NoError(t, err)

	encryptConfig := encrypt.Config{ServiceMasterKey: "EV0g7BoFQnqshvzep9knZsUUmvsqSBsjDAJus7ri0B8="}
	encryptService, err := encrypt.NewService(&encryptConfig)
	assert.NoError(t, err)

	authService := auth.NewService(db, tokenService, encryptService)

	minerClientService := client.NewService()
	pairingConfig := pairing.Config{SecretKey: "00000000000000000000000000000000000000000000"}
	pairingService := pairing.NewService(db, minerClientService, pairingConfig, tokenService)

	onboardingService := onboarding.NewService(db)

	return &ServiceProvider{
		DB:                db,
		TokenService:      tokenService,
		AuthService:       authService,
		PairingService:    pairingService,
		OnboardingService: onboardingService,
	}
}
