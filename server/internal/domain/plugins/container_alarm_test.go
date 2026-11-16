package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPluginMinerContainerAlarmActionSendsOnceAndKeepsEvidence(t *testing.T) {
	for _, tc := range []struct {
		name          string
		deviceErr     error
		notDispatched bool
		uncertain     bool
		unimplemented bool
	}{
		{name: "acknowledged"},
		{name: "refused", deviceErr: sdk.WriteRefused(errors.New("controller refused"), sdk.WriteStageDispatch, sdk.WriteReasonRejected), notDispatched: true},
		{name: "unsupported", deviceErr: sdk.UnsupportedWrite(sdk.CapabilityContainerAlarm), notDispatched: true, unimplemented: true},
		{name: "unknown outcome", deviceErr: sdk.WithWriteOutcome(errors.New("deadline"), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed), uncertain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner, device := createGatedTestPluginMiner()
			device.onWrite = func(context.Context, commandtype.Type) error { return tc.deviceErr }
			err := miner.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute})
			require.Equal(t, 1, device.writes, "a plant alarm command must never be replayed")
			if tc.deviceErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, tc.notDispatched, devicewrite.IsNotDispatched(err))
			require.Equal(t, tc.uncertain, devicewrite.IsUncertainDelivery(err))
			require.Equal(t, tc.unimplemented, fleeterror.IsUnimplementedError(err))
		})
	}
}

func TestPluginMinerContainerAlarmActionRejectsInvalidActionAndMissingSupport(t *testing.T) {
	miner, device := createGatedTestPluginMiner()
	err := miner.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: "silence"})
	require.True(t, devicewrite.IsNotDispatched(err))
	require.Zero(t, device.writes)

	unsupported, _ := createTestPluginMiner()
	err = unsupported.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute})
	require.True(t, fleeterror.IsUnimplementedError(err))

	// A gRPC plugin always satisfies the interface; without the advertised
	// capability nothing is sent and the operator is told so.
	notAdvertised, device := createGatedTestPluginMiner()
	delete(notAdvertised.caps, sdk.CapabilityContainerAlarm)
	err = notAdvertised.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute})
	require.True(t, fleeterror.IsUnimplementedError(err))
	require.True(t, devicewrite.IsNotDispatched(err))
	require.Zero(t, device.writes)
}
