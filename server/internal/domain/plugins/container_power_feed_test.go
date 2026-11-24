package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type powerFeedSDKDevice struct {
	*mockSDKDevice
	calls []sdk.ContainerPowerFeedRequest
}

func (d *powerFeedSDKDevice) ContainerPowerFeed(_ context.Context, r sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	d.calls = append(d.calls, r)
	if r.ReadOnly() {
		return sdk.ContainerPowerFeedResult{Inspection: &sdk.ModulePowerFeedInspection{ObservedAt: time.Now(), Mapping: sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingUnavailable}}}, nil
	}
	return sdk.ContainerPowerFeedResult{Applied: &sdk.PowerFeedReading{ID: r.FeedID, State: sdk.PowerFeedStateDisabled, Overridden: true}}, nil
}

func TestPluginMinerPowerFeedWritesNeedControlCapabilityAndAdmission(t *testing.T) {
	module := sdk.ContainerPowerFeedModule{Tank: 1, Module: 2, ReportedID: "module-a"}
	write := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedTurnOff, Module: module, FeedID: "pdu-1-outlet-01"}
	inspect := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedInspect, Module: module}
	_, base := createTestPluginMiner()
	device := &powerFeedSDKDevice{mockSDKDevice: base}
	miner := createTestPluginMinerWithDevice(device)
	kinds := []commandtype.Type{}
	miner.writeGate = func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		kinds = append(kinds, kind)
		return call(ctx)
	}

	miner.caps = sdk.Capabilities{sdk.CapabilityContainerPowerFeed: true}
	_, err := miner.ContainerPowerFeed(t.Context(), write)
	require.True(t, devicewrite.IsNotDispatched(err), "inspection alone does not allow control: %v", err)
	require.Empty(t, device.calls)

	_, err = miner.ContainerPowerFeed(t.Context(), inspect)
	require.NoError(t, err)
	require.Empty(t, kinds, "inspection takes no write admission")

	miner.caps[sdk.CapabilityContainerPowerFeedControl] = true
	got, err := miner.ContainerPowerFeed(t.Context(), write)
	require.NoError(t, err)
	require.Equal(t, sdk.PowerFeedStateDisabled, got.Applied.State)
	require.Equal(t, []commandtype.Type{commandtype.ControlPowerFeed}, kinds)
	require.Equal(t, []sdk.ContainerPowerFeedRequest{inspect, write}, device.calls)
}
