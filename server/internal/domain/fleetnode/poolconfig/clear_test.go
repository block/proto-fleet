package poolconfig

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestClearPoolsRequiresExplicitIntentAndNoPoolValues(t *testing.T) {
	require.Error(t, validateConfig("miner", &gatewaypb.MiningPoolWriteConfiguration{}))
	require.NoError(t, validateConfig("miner", &gatewaypb.MiningPoolWriteConfiguration{ClearAll: true}))
	mixed := testConfig()
	mixed.ClearAll = true
	require.Error(t, validateConfig("miner", mixed))
}
