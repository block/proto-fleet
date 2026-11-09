package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
)

type poolObservationDevice struct {
	sdk.Device
	stored                []sdk.StoredMiningPool
	runtime               []sdk.ConfiguredPool
	storedErr, runtimeErr error
	clears                int
}

func (d *poolObservationDevice) GetStoredMiningPools(context.Context) ([]sdk.StoredMiningPool, error) {
	return d.stored, d.storedErr
}
func (d *poolObservationDevice) GetMiningPools(context.Context) ([]sdk.ConfiguredPool, error) {
	return d.runtime, d.runtimeErr
}
func (d *poolObservationDevice) UpdateMiningPools(_ context.Context, pools []sdk.MiningPoolConfig) error {
	if len(pools) != 0 {
		return errors.New("expected explicit clear")
	}
	d.clears++
	d.stored, d.runtime = nil, nil
	return nil
}

// Route the real cloud adapter's serialized commands through the real node
// command handler, with only the transport and physical device substituted.
type poolObservationSender struct {
	t    *testing.T
	node *RunCmd
}

func (s poolObservationSender) SendCommand(ctx context.Context, _ int64, version pb.CommandProtocolVersion, command *pb.ControlCommand) (*pb.ControlAck, error) {
	var envelope pb.AgentCommand
	require.NoError(s.t, proto.Unmarshal(command.GetPayload(), &envelope))
	if envelope.GetMinerCommand().GetVerifyPoolsCleared() != nil {
		require.Equal(s.t, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V9, version)
	}
	ack := &captureAcker{}
	s.node.handleMinerCommand(ctx, nil, ack, command.GetCommandId(), envelope.GetMinerCommand(), discardLogger(s.t))
	result := ack.only(s.t)
	require.NotContains(s.t, string(result.GetPayload()), "private-pool-secret")
	require.NotContains(s.t, result.GetErrorMessage(), "private-pool-secret")
	return result, nil
}

func TestRemotePoolClearingObservesCompleteStoredAndRuntimeSettings(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDevice := mocks.NewMockDevice(ctrl)
	device := &poolObservationDevice{Device: mockDevice}
	mockDevice.EXPECT().Close(gomock.Any()).Return(nil).AnyTimes()
	mockDevice.EXPECT().DescribeDevice(gomock.Any()).Return(sdk.DeviceInfo{}, sdk.Capabilities{sdk.CapabilityPoolClear: true}, nil).AnyTimes()
	driver := mocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).AnyTimes()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	node := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}, passwordUpdatePrivateKey: key.Bytes()}
	remote, err := remotenode.New(remotenode.Config{
		Sender: poolObservationSender{t: t, node: node}, FleetNodeID: 1, OrgID: 1,
		DeviceIdentifier: "dev-1", DriverName: "virtual", IPAddress: "10.0.0.5", Port: "4028", URLScheme: "http",
		EncryptionPublicKey: func(context.Context) ([]byte, error) { return key.PublicKey().Bytes(), nil },
	})
	require.NoError(t, err)
	for _, tc := range []struct {
		name                  string
		stored                []sdk.StoredMiningPool
		runtime               []sdk.ConfiguredPool
		storedErr, runtimeErr error
		cleared, wantError    bool
	}{
		{name: "empty", cleared: true},
		{name: "empty slots", stored: []sdk.StoredMiningPool{{Password: proto.String("")}, {Priority: 4, Password: proto.String("")}}, cleared: true},
		{name: "hidden extra stored slot", stored: []sdk.StoredMiningPool{{Priority: 4, Password: proto.String("private-pool-secret")}}},
		{name: "leftover username", stored: []sdk.StoredMiningPool{{Username: "worker", Password: proto.String("")}}},
		{name: "unknown password", stored: []sdk.StoredMiningPool{{}}, wantError: true},
		{name: "runtime override beyond normal slots", runtime: []sdk.ConfiguredPool{{Priority: 4, URL: "stratum+tcp://old.example:3333"}}},
		{name: "stored unavailable", storedErr: errors.New("private-pool-secret"), wantError: true},
		{name: "runtime unavailable", runtimeErr: errors.New("private-pool-secret"), wantError: true},
		{name: "unsupported firmware", storedErr: sdk.NewErrUnsupportedCapability("private-pool-secret"), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device.stored, device.runtime = tc.stored, tc.runtime
			device.storedErr, device.runtimeErr = tc.storedErr, tc.runtimeErr
			cleared, err := remote.VerifyPoolsCleared(t.Context())
			if tc.wantError {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-pool-secret")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.cleared, cleared)
		})
	}
	device.storedErr, device.runtimeErr = nil, nil
	device.stored = []sdk.StoredMiningPool{{Password: proto.String("private-pool-secret")}}
	cleared, err := remote.VerifyPoolsCleared(t.Context())
	require.NoError(t, err)
	require.False(t, cleared)
	require.NoError(t, remote.UpdateMiningPools(t.Context(), dto.UpdateMiningPoolsPayload{ClearAll: true}))
	cleared, err = remote.VerifyPoolsCleared(t.Context())
	require.NoError(t, err)
	require.True(t, cleared)
	require.Equal(t, 1, device.clears)
}
