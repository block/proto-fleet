package main

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type moduleMiningDevice struct {
	sdk.Device
	received sdk.ModuleMiningRequest
}

func (d *moduleMiningDevice) SetModuleMining(_ context.Context, r sdk.ModuleMiningRequest) error {
	d.received = r
	return nil
}
func TestRunModuleMiningActionPreservesIdentityAndExactScope(t *testing.T) {
	d := &moduleMiningDevice{}
	action := &pb.SetModuleMiningAction{Mining: new(false), Modules: []*pb.ModuleMiningTarget{{Tank: 1, Module: 3, ReportedId: "a"}, {Tank: 2, Module: 3, ReportedId: "b"}}}
	_, err := runMinerAction(t.Context(), nil, "module-command", "", nil, d, &pb.MinerCommand{Action: &pb.MinerCommand_SetModuleMining{SetModuleMining: action}})
	require.NoError(t, err)
	require.Equal(t, []sdk.ModuleMiningTarget{{Tank: 1, Module: 3, ReportedID: "a"}, {Tank: 2, Module: 3, ReportedID: "b"}}, d.received.Modules)
	require.False(t, *d.received.Mining)
}

func TestModuleMiningCommandBuildsControllerAndPreservesExactScope(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)
	base := mocks.NewMockDevice(ctrl)
	base.EXPECT().Close(gomock.Any()).Return(nil)
	device := &moduleMiningDevice{Device: base}
	driver.EXPECT().NewDevice(gomock.Any(), gomock.Not("dev-1"), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ string, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
		require.Equal(t, sdk.DeviceClassContainer, info.DeviceClass)
		return sdk.NewDeviceResult{Device: device}, nil
	})
	run := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}}
	acker := &captureAcker{}
	action := &pb.SetModuleMiningAction{Mining: new(false), Modules: []*pb.ModuleMiningTarget{{Tank: 1, Module: 3, ReportedId: "a"}, {Tank: 2, Module: 3, ReportedId: "b"}}}
	run.handleMinerCommand(t.Context(), nil, acker, "modules", withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_SetModuleMining{SetModuleMining: action}}), discardLogger(t))
	require.True(t, acker.only(t).Succeeded)
	require.Equal(t, []sdk.ModuleMiningTarget{{Tank: 1, Module: 3, ReportedID: "a"}, {Tank: 2, Module: 3, ReportedID: "b"}}, device.received.Modules)
	require.False(t, *device.received.Mining)
}
