package pairing

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/stretchr/testify/require"
)

func TestPairingRetainsDeviceClassWhenIdentityIsUnavailable(t *testing.T) {
	device := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{
		DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
	}}
	applyReportedIdentity(device, &gatewaypb.FleetNodePairResult{Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED})
	require.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, device.DeviceClass)
	applyReportedIdentity(device, &gatewaypb.FleetNodePairResult{
		Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG,
	})
	require.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_RIG, device.DeviceClass)
}
