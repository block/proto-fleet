package mininggroups

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type nodeReadinessSender struct {
	*control.Registry
	payload []byte
}

func (s nodeReadinessSender) SendCommand(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	return &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK, Payload: s.payload}, nil
}

func TestStateAndAdmissionShareLiveNodeDeactivationReadiness(t *testing.T) {
	for _, tc := range []struct {
		name           string
		version        gatewaypb.CommandProtocolVersion
		outsideDefault bool
	}{
		{name: "V14 ready except upgrade", version: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V14},
		{name: "V15 ready", version: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15},
		{name: "V14 return to default first", version: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V14, outsideDefault: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, f := deactivationFixture(t)
			registry := control.NewRegistry()
			stream, err := registry.RegisterAuthenticated(7, "node", tc.version)
			require.NoError(t, err)
			defer stream.Unregister()
			observed := sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 289, ObservedAt: time.Now().UTC(), Groups: []sdk.MiningGroup{{ID: "default", Revision: 1}}}
			if tc.outsideDefault {
				observed.Groups = append(observed.Groups, sdk.MiningGroup{ID: "custom", Revision: 1, MACs: []string{"02:00:00:00:00:01"}})
			}
			payload, err := json.Marshal(observed)
			require.NoError(t, err)
			remote, err := remotenode.New(remotenode.Config{Sender: nodeReadinessSender{Registry: registry, payload: payload}, FleetNodeID: 7, DeviceIdentifier: f.target.DeviceIdentifier, DriverName: "proto", URLScheme: "http", IPAddress: "127.0.0.1", Port: "80"})
			require.NoError(t, err)
			f.service.miners = callMinerGetter(func(context.Context, int64) (interfaces.Miner, error) { return remote, nil })
			state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			require.True(t, state.Observed.Enabled)
			require.True(t, state.Record.ObservedAt.Valid)
			request := uuid.NewString()
			_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, request)
			if tc.version == gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15 {
				require.Empty(t, state.DeactivationBlockedReason)
				require.NoError(t, err)
				require.Equal(t, "pending", f.state(t).Status)
				return
			}
			if tc.outsideDefault {
				require.Equal(t, "Move hashboards to the default group before disabling mining groups.", state.DeactivationBlockedReason)
				require.True(t, state.CanReturnToDefault)
			} else {
				require.Equal(t, "Upgrade the Fleet Node and reconnect it before disabling mining groups.", state.DeactivationBlockedReason)
				require.False(t, state.CanReturnToDefault)
			}
			require.ErrorContains(t, err, state.DeactivationBlockedReason)
			after := f.state(t)
			require.Equal(t, state.Record.Status, after.Status)
			require.Equal(t, state.Record.Revision, after.Revision)
			var requests int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM mining_group_request WHERE request_id=$1`, request).Scan(&requests))
			require.Zero(t, requests)
		})
	}
}
