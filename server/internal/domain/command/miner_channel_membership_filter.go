package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// MinerChannelMembershipFilterName tags Skipped entries for leased miner channel devices.
const MinerChannelMembershipFilterName = "miner_channel_membership"

const minerChannelMembershipSkipReason = "device is leased by another miner channel owner"

// MinerChannelMembershipQuerier is the store surface needed by the lease filter.
type MinerChannelMembershipQuerier interface {
	ListActiveOwnedMinerChannelMemberships(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.MinerChannelDeviceOwnership, error)
}

// MinerChannelMembershipFilter blocks external commands against devices leased by
// another user. MinerChannel enforcement self-traffic bypasses the gate.
type MinerChannelMembershipFilter struct {
	querier MinerChannelMembershipQuerier
}

func NewMinerChannelMembershipFilter(querier MinerChannelMembershipQuerier) *MinerChannelMembershipFilter {
	return &MinerChannelMembershipFilter{querier: querier}
}

func (f *MinerChannelMembershipFilter) Name() string {
	return MinerChannelMembershipFilterName
}

func (f *MinerChannelMembershipFilter) Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	if in.Actor == session.ActorMinerChannel || len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	rows, err := f.querier.ListActiveOwnedMinerChannelMemberships(ctx, in.OrganizationID, in.DeviceIdentifiers)
	if err != nil {
		return CommandFilterOutput{}, fmt.Errorf("failed to list active owned miner channel memberships: %w", err)
	}
	if len(rows) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	locked := make(map[string]models.MinerChannelDeviceOwnership, len(rows))
	for _, row := range rows {
		locked[row.DeviceIdentifier] = row
	}

	var kept []string
	var skipped []SkippedDevice
	for _, id := range in.DeviceIdentifiers {
		row, ok := locked[id]
		if !ok || row.OwnerUserID == nil || *row.OwnerUserID == in.UserID || commandFilterAdminRole(in.Role) {
			kept = append(kept, id)
			continue
		}
		skipped = append(skipped, SkippedDevice{
			DeviceIdentifier: id,
			FilterName:       f.Name(),
			Reason:           minerChannelMembershipSkipReason,
		})
	}
	return CommandFilterOutput{Kept: kept, Skipped: skipped}, nil
}

func commandFilterAdminRole(role string) bool {
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "SUPER_ADMIN", "ADMIN":
		return true
	default:
		return false
	}
}
