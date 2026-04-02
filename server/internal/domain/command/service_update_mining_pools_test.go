package command

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateUpdateMiningPoolsPayload_RawPoolsPreserveExistingSuffixes(t *testing.T) {
	service := &Service{}

	payload, err := service.createUpdateMiningPoolsPayload(
		context.Background(),
		&pb.PoolSlotConfig{
			PoolSource: &pb.PoolSlotConfig_RawPool{
				RawPool: &pb.RawPoolInfo{
					Url:      "stratum+tcp://pool1.example.com:3333",
					Username: "wallet.existing-worker",
				},
			},
		},
		&pb.PoolSlotConfig{
			PoolSource: &pb.PoolSlotConfig_RawPool{
				RawPool: &pb.RawPoolInfo{
					Url:      "stratum+tcp://pool2.example.com:3333",
					Username: "wallet.backup-worker",
				},
			},
		},
		nil,
	)
	require.NoError(t, err)

	assert.False(t, payload.DefaultPool.AppendMinerName)
	assert.Equal(t, "wallet.existing-worker", payload.DefaultPool.Username)
	require.NotNil(t, payload.Backup1Pool)
	assert.False(t, payload.Backup1Pool.AppendMinerName)
	assert.Equal(t, "wallet.backup-worker", payload.Backup1Pool.Username)
}

func TestCreateUpdateMiningPoolsPayload_RawPoolsAppendMinerNameWhenUsernameHasNoSuffix(t *testing.T) {
	service := &Service{}

	payload, err := service.createUpdateMiningPoolsPayload(
		context.Background(),
		&pb.PoolSlotConfig{
			PoolSource: &pb.PoolSlotConfig_RawPool{
				RawPool: &pb.RawPoolInfo{
					Url:      "stratum+tcp://pool1.example.com:3333",
					Username: "wallet",
				},
			},
		},
		&pb.PoolSlotConfig{
			PoolSource: &pb.PoolSlotConfig_RawPool{
				RawPool: &pb.RawPoolInfo{
					Url:      "stratum+tcp://pool2.example.com:3333",
					Username: "wallet-backup",
				},
			},
		},
		nil,
	)
	require.NoError(t, err)

	assert.True(t, payload.DefaultPool.AppendMinerName)
	assert.Equal(t, "wallet", payload.DefaultPool.Username)
	require.NotNil(t, payload.Backup1Pool)
	assert.True(t, payload.Backup1Pool.AppendMinerName)
	assert.Equal(t, "wallet-backup", payload.Backup1Pool.Username)
}
