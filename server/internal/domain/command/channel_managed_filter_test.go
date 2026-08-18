package command

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

type fakeChannelManagedQuerier struct {
	managed       []string
	err           error
	calls         int
	lastOrgID     int64
	lastDeviceIDs []string
}

func (f *fakeChannelManagedQuerier) ListChannelManagedDeviceIdentifiers(
	_ context.Context,
	orgID int64,
	deviceIdentifiers []string,
) ([]string, error) {
	f.calls++
	f.lastOrgID = orgID
	f.lastDeviceIDs = append([]string(nil), deviceIdentifiers...)
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.managed...), nil
}

func TestChannelManagedFilterExternalFirmwareFailsClosed(t *testing.T) {
	q := &fakeChannelManagedQuerier{managed: []string{"miner-2"}}
	filter := NewChannelManagedFilter(q)

	out, err := filter.Apply(t.Context(), CommandFilterInput{
		CommandType:       commandtype.FirmwareUpdate,
		OrganizationID:    7,
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	require.Len(t, out.Skipped, 1)
	assert.Equal(t, "miner-2", out.Skipped[0].DeviceIdentifier)
	assert.Equal(t, ChannelManagedFilterName, out.Skipped[0].FilterName)
	assert.Equal(t, int64(7), q.lastOrgID)
	assert.Equal(t, []string{"miner-1", "miner-2"}, q.lastDeviceIDs)
}

func TestChannelManagedFilterEnforcementActorBypassesOnlyFirmwareGate(t *testing.T) {
	q := &fakeChannelManagedQuerier{managed: []string{"miner-1"}}
	filter := NewChannelManagedFilter(q)

	out, err := filter.Apply(t.Context(), CommandFilterInput{
		CommandType:       commandtype.FirmwareUpdate,
		OrganizationID:    7,
		Actor:             session.ActorChannelEnforcement,
		DeviceIdentifiers: []string{"miner-1"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	assert.Empty(t, out.Skipped)
	assert.Zero(t, q.calls)
}

func TestChannelEnforcementActorStillYieldsToCurtailment(t *testing.T) {
	curtailment := &fakeCurtailmentActiveQuerier{active: []string{"miner-1"}}
	channelManaged := &fakeChannelManagedQuerier{managed: []string{"miner-1"}}

	kept, skipped, err := applyFilters(t.Context(), []CommandFilter{
		NewCurtailmentActiveFilter(curtailment),
		NewChannelManagedFilter(channelManaged),
	}, CommandFilterInput{
		CommandType:       commandtype.FirmwareUpdate,
		OrganizationID:    7,
		Actor:             session.ActorChannelEnforcement,
		DeviceIdentifiers: []string{"miner-1"},
	})

	require.NoError(t, err)
	assert.Empty(t, kept)
	require.Len(t, skipped, 1)
	assert.Equal(t, CurtailmentActiveFilterName, skipped[0].FilterName)
	assert.Zero(t, channelManaged.calls)
}

func TestChannelManagedFilterAllowsNonFirmwareCommands(t *testing.T) {
	for _, commandType := range []commandtype.Type{commandtype.Reboot, commandtype.DownloadLogs} {
		t.Run(commandType.String(), func(t *testing.T) {
			q := &fakeChannelManagedQuerier{managed: []string{"miner-1"}}
			filter := NewChannelManagedFilter(q)

			out, err := filter.Apply(t.Context(), CommandFilterInput{
				CommandType:       commandType,
				OrganizationID:    7,
				DeviceIdentifiers: []string{"miner-1"},
			})

			require.NoError(t, err)
			assert.Equal(t, []string{"miner-1"}, out.Kept)
			assert.Empty(t, out.Skipped)
			assert.Zero(t, q.calls)
		})
	}
}

func TestChannelManagedFilterStoreFailureFailsClosed(t *testing.T) {
	filter := NewChannelManagedFilter(&fakeChannelManagedQuerier{err: errors.New("db down")})

	out, err := filter.Apply(t.Context(), CommandFilterInput{
		CommandType:       commandtype.FirmwareUpdate,
		OrganizationID:    7,
		DeviceIdentifiers: []string{"miner-1"},
	})

	require.Error(t, err)
	assert.Empty(t, out.Kept)
	assert.Empty(t, out.Skipped)
}
