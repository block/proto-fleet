package sqlstores_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestPairingMetadataSaveKeepsCurrentEndpointGroup(t *testing.T) {
	_, store, org := ct1HAStore(t)
	before := saveCT1HA(t, store, "container", ct1HADiscovery(org, "a", "10.0.0.11"))
	current := saveCT1HA(t, store, "scan-b", ct1HADiscovery(org, "b", "10.0.0.12"))
	before.FirmwareVersion = "pair-v2"
	saved, err := store.Save(t.Context(), before.GetDeviceOrgIdentifier(), before)
	require.NoError(t, err)
	require.Equal(t, "pair-v2", saved.FirmwareVersion)
	require.Len(t, saved.Endpoints, 2)
	require.True(t, proto.Equal(current.Endpoints[0], saved.Endpoints[0]))
	require.True(t, proto.Equal(current.Endpoints[1], saved.Endpoints[1]))
	before.DriverName = "unrelated-driver"
	_, err = store.Save(t.Context(), before.GetDeviceOrgIdentifier(), before)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}
