package command

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

const ChannelManagedFilterName = "channel_managed"

const channelManagedSkipReason = "device firmware is managed by a software channel"

type ChannelManagedQuerier interface {
	ListChannelManagedDeviceIdentifiers(
		ctx context.Context,
		orgID int64,
		deviceIdentifiers []string,
	) ([]string, error)
}

type ChannelManagedFilter struct {
	querier ChannelManagedQuerier
}

func NewChannelManagedFilter(querier ChannelManagedQuerier) *ChannelManagedFilter {
	return &ChannelManagedFilter{querier: querier}
}

func (f *ChannelManagedFilter) Name() string {
	return ChannelManagedFilterName
}

func (f *ChannelManagedFilter) Apply(
	ctx context.Context,
	in CommandFilterInput,
) (CommandFilterOutput, error) {
	if in.CommandType != commandtype.FirmwareUpdate || len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	if in.Actor == session.ActorChannelEnforcement {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	managed, err := f.querier.ListChannelManagedDeviceIdentifiers(
		ctx,
		in.OrganizationID,
		in.DeviceIdentifiers,
	)
	if err != nil {
		return CommandFilterOutput{}, fmt.Errorf("list channel-managed devices: %w", err)
	}
	if len(managed) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	managedSet := make(map[string]struct{}, len(managed))
	for _, identifier := range managed {
		managedSet[identifier] = struct{}{}
	}
	kept := make([]string, 0, len(in.DeviceIdentifiers)-len(managed))
	skipped := make([]SkippedDevice, 0, len(managed))
	for _, identifier := range in.DeviceIdentifiers {
		if _, ok := managedSet[identifier]; !ok {
			kept = append(kept, identifier)
			continue
		}
		skipped = append(skipped, SkippedDevice{
			DeviceIdentifier: identifier,
			FilterName:       f.Name(),
			Reason:           channelManagedSkipReason,
		})
	}
	return CommandFilterOutput{Kept: kept, Skipped: skipped}, nil
}
