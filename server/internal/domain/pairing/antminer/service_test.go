package antminer_test

import (
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing/antminer"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/golang/mock/gomock"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAntminerCredentialValidation(t *testing.T) {
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	webClient := mocks.NewMockWebAPIClient(ctrl)

	service := antminer.NewService(nil, nil, encryptService, webClient)
	ctx := t.Context()

	device := &minerdiscovery.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-antminer-001",
			MacAddress:       "00:11:22:33:44:55",
		},
		Type: models.TypeAntminer.String(),
	}

	t.Run("fails with nil credentials", func(t *testing.T) {
		err := service.PairDevice(ctx, device, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credentials are required for Antminer pairing")
	})

	t.Run("fails with empty username", func(t *testing.T) {
		password := "password123"
		credentials := &pb.Credentials{
			Username: "",
			Password: &password,
		}

		err := service.PairDevice(ctx, device, credentials)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credentials are required for Antminer pairing")
	})

	t.Run("fails with empty password", func(t *testing.T) {
		emptyPassword := ""
		credentials := &pb.Credentials{
			Username: "admin",
			Password: &emptyPassword,
		}

		err := service.PairDevice(ctx, device, credentials)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credentials are required for Antminer pairing")
	})

	t.Run("fails with whitespace-only credentials", func(t *testing.T) {
		whitespacePassword := "\t\n"
		credentials := &pb.Credentials{
			Username: "   ",
			Password: &whitespacePassword,
		}

		err := service.PairDevice(ctx, device, credentials)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credentials are required for Antminer pairing")
	})
}
