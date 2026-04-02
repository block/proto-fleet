package testutil

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

type Config struct {
	ServiceMasterKey    string // the service master key used for ENCRYPT / DECRYPT purposes
	MinerAuthPrivateKey string // a string encrypted by the ServiceMasterKey
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

func (c *Config) GetMinerAuthPrivateKey(t *testing.T) []byte {
	pemBytes, err := c.encryptService.Decrypt(c.MinerAuthPrivateKey)
	if err != nil {
		t.Fatalf("failed to decrypt miner auth private key")
	}

	return pemBytes
}

func GetTestConfig() (*Config, error) {
	serviceMasterKey := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	minerAuthPrivateKey := "z65ViaeDr/SF9jyoEJ/lp/Vsl8C4SrxehBbCCLez9OUA4ni3G8J1K/9db5tXyxx+xd3syUtei8Nw0Ml9QOVzGEvzsnVxp8B7G63VM8ls7i4rncYDrlRV4ietDPs="

	encryptConfig := &encrypt.Config{ServiceMasterKey: serviceMasterKey}

	encryptService, err := encrypt.NewService(encryptConfig)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create encrypt service for validation: %v", err)
	}

	_, err = encryptService.Decrypt(minerAuthPrivateKey)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("test config invalid, minerAuthPrivateKey cannot be decrypted: %v", err)
	}

	return &Config{
		ServiceMasterKey:    serviceMasterKey,
		MinerAuthPrivateKey: minerAuthPrivateKey,
		AuthTokenSecretKey:  "00000000000000000000000000000000000000000000",
		PairingSecretKey:    "00000000000000000000000000000000000000000000",
		antminerCredentials: &AntminerCredentials{
			Username: "antminer-commander",
			Password: "Ants-for-the-win",
		},
		encryptService: encryptService,
	}, nil
}
