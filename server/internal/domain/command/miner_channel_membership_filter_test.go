package command

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

type fakeMinerChannelMembershipQuerier struct {
	rows []models.MinerChannelDeviceOwnership
	err  error
}

func (f fakeMinerChannelMembershipQuerier) ListActiveOwnedMinerChannelMemberships(context.Context, int64, []string) ([]models.MinerChannelDeviceOwnership, error) {
	return f.rows, f.err
}

func TestMinerChannelMembershipFilter_BypassesMinerChannelActor(t *testing.T) {
	ownerID := int64(7)
	filter := NewMinerChannelMembershipFilter(fakeMinerChannelMembershipQuerier{
		rows: []models.MinerChannelDeviceOwnership{{DeviceIdentifier: "miner-1", OwnerUserID: &ownerID}},
	})

	out, err := filter.Apply(context.Background(), CommandFilterInput{
		Actor:             session.ActorMinerChannel,
		DeviceIdentifiers: []string{"miner-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	assert.Empty(t, out.Skipped)
}

func TestMinerChannelMembershipFilter_BlocksNonOwnerAndKeepsOwner(t *testing.T) {
	ownerID := int64(7)
	filter := NewMinerChannelMembershipFilter(fakeMinerChannelMembershipQuerier{
		rows: []models.MinerChannelDeviceOwnership{{DeviceIdentifier: "miner-1", OwnerUserID: &ownerID}},
	})

	out, err := filter.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.Reboot,
		OrganizationID:    3,
		UserID:            8,
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"miner-2"}, out.Kept)
	require.Len(t, out.Skipped, 1)
	assert.Equal(t, "miner-1", out.Skipped[0].DeviceIdentifier)
	assert.Equal(t, MinerChannelMembershipFilterName, out.Skipped[0].FilterName)
	assert.Equal(t, minerChannelMembershipSkipReason, out.Skipped[0].Reason)

	out, err = filter.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.Reboot,
		OrganizationID:    3,
		UserID:            ownerID,
		DeviceIdentifiers: []string{"miner-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	assert.Empty(t, out.Skipped)
}

func TestMinerChannelMembershipFilter_AllowsAdminRole(t *testing.T) {
	ownerID := int64(7)
	filter := NewMinerChannelMembershipFilter(fakeMinerChannelMembershipQuerier{
		rows: []models.MinerChannelDeviceOwnership{{DeviceIdentifier: "miner-1", OwnerUserID: &ownerID}},
	})

	out, err := filter.Apply(context.Background(), CommandFilterInput{
		Role:              "SUPER_ADMIN",
		DeviceIdentifiers: []string{"miner-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	assert.Empty(t, out.Skipped)
}

func TestMinerChannelMembershipFilter_StoreErrorBubblesUp(t *testing.T) {
	filter := NewMinerChannelMembershipFilter(fakeMinerChannelMembershipQuerier{err: errors.New("boom")})

	_, err := filter.Apply(context.Background(), CommandFilterInput{DeviceIdentifiers: []string{"miner-1"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
