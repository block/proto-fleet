package containers

import (
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPlantProjectionKeepsIndependentFreshness(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		age   time.Duration
		stale bool
	}{{0, false}, {3 * time.Minute, true}, {-time.Minute, true}} {
		data, err := json.Marshal(sdk.ContainerPlant{ObservedAt: now.Add(-tc.age), DryCoolerInletTemperatureC: new(0.0)})
		require.NoError(t, err)
		c := &pb.Container{}
		require.NoError(t, projectPlant(c, data, now))
		require.Equal(t, tc.stale, c.Plant.Stale)
		require.Equal(t, 0.0, *c.Plant.DryCoolerInletTemperatureC)
	}
	c := &pb.Container{}
	require.NoError(t, projectPlant(c, []byte("null"), now))
	require.Nil(t, c.Plant)
}
