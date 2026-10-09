package main

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
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
