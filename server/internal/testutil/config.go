package testutil

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

type Config struct {
	ServiceMasterKey    string // the service master key used for ENCRYPT / DECRYPT purposes
	AuthTokenSecretKey  string
	PairingSecretKey    string
	antminerCredentials *AntminerCredentials
	encryptService      *encrypt.Service
}

type AntminerCredentials struct {
	Username string
	Password string
}

func (c *Config) GetAntminerPasswordEnc(t *testing.T) string {
	pass, err := c.encryptService.Encrypt([]byte(c.antminerCredentials.Password))
	if err != nil {
		t.Fatalf("failed to encrypt antminer upassword")
	}

	return pass
}

func (c *Config) GetAntminerUsernameEnc(t *testing.T) string {
	username, err := c.encryptService.Encrypt([]byte(c.antminerCredentials.Username))
	if err != nil {
		t.Fatalf("failed to encrypt antminer username")
	}

	return username
}

func GetTestConfig() (*Config, error) {
	serviceMasterKey := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	encryptConfig := &encrypt.Config{ServiceMasterKey: serviceMasterKey}

	encryptService, err := encrypt.NewService(encryptConfig)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create encrypt service for validation: %v", err)
	}
	return &Config{
		ServiceMasterKey:   serviceMasterKey,
		AuthTokenSecretKey: "00000000000000000000000000000000000000000000",
		PairingSecretKey:   "00000000000000000000000000000000000000000000",
		antminerCredentials: &AntminerCredentials{
			Username: "antminer-commander",
			Password: "Ants-for-the-win",
		},
		encryptService: encryptService,
	}, nil
}
