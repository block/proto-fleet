package command

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// ReleaseChannelFirmwareFilter keeps direct firmware commands from fighting a
// channel's assignment or bypassing its pilot, batching and offline budget.
// Other miner actions and miners without an assignment are unaffected.
type ReleaseChannelFirmwareFilter struct {
	conn *sql.DB
}

const releaseChannelFirmwareFilterName = "release_channel_firmware"

func NewReleaseChannelFirmwareFilter(conn *sql.DB) *ReleaseChannelFirmwareFilter {
	return &ReleaseChannelFirmwareFilter{conn: conn}
}

func (*ReleaseChannelFirmwareFilter) Name() string { return releaseChannelFirmwareFilterName }

func (f *ReleaseChannelFirmwareFilter) Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	if in.CommandType != commandtype.FirmwareUpdate || in.Actor == session.ActorRolloutEnforcement || len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	lookup := func(q sqlc.Querier) ([]sqlc.ListManagedFirmwareUpdateDevicesRow, error) {
		return q.ListManagedFirmwareUpdateDevices(ctx, sqlc.ListManagedFirmwareUpdateDevicesParams{
			OrgID: in.OrganizationID, DeviceIdentifiers: in.DeviceIdentifiers,
		})
	}
	var managed []sqlc.ListManagedFirmwareUpdateDevicesRow
	var err error
	if q := db.GetTxQueries(ctx); q != nil {
		managed, err = lookup(q)
	} else {
		managed, err = db.WithTransaction(ctx, f.conn, lookup)
	}
	if err != nil {
		return CommandFilterOutput{}, fmt.Errorf("check release channel firmware assignments: %w", err)
	}
	channels := make(map[string]string, len(managed))
	for _, device := range managed {
		channels[device.DeviceIdentifier] = device.ChannelName
	}
	out := CommandFilterOutput{}
	for _, identifier := range in.DeviceIdentifiers {
		if channelName, assigned := channels[identifier]; assigned {
			out.Skipped = append(out.Skipped, SkippedDevice{
				DeviceIdentifier: identifier, FilterName: f.Name(),
				Reason: fmt.Sprintf("firmware is managed by release channel %q; change its firmware in Settings > Firmware > Release channels", channelName),
			})
		} else {
			out.Kept = append(out.Kept, identifier)
		}
	}
	return out, nil
}
