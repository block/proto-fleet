package rollout

import (
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

func TestFirmwareVersionStatesReachClients(t *testing.T) {
	for state, want := range map[string]pb.FirmwareVersionState{
		rollout.VersionStateKnown:   pb.FirmwareVersionState_FIRMWARE_VERSION_STATE_KNOWN,
		rollout.VersionStateMixed:   pb.FirmwareVersionState_FIRMWARE_VERSION_STATE_MIXED,
		rollout.VersionStateUnknown: pb.FirmwareVersionState_FIRMWARE_VERSION_STATE_UNKNOWN,
	} {
		require.Equal(t, want, minerToProto(&rollout.ChannelMiner{FirmwareVersionState: state}).GetFirmwareVersionState(), state)
		require.Equal(t, want, deviceToProto(&rollout.RolloutDevice{FirmwareVersionState: state}).GetFirmwareVersionState(), state)
	}
}
