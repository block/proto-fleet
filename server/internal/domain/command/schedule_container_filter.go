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

const ScheduleContainerFilterName = "schedule_container"

// ScheduleContainerFilter excludes containers from generic scheduled commands.
// Class metadata is authoritative; transient capability-discovery failures must
// not turn conventional miner commands (especially window restores) into skips.
type ScheduleContainerFilter struct {
	conn *sql.DB
}

func NewScheduleContainerFilter(conn *sql.DB) *ScheduleContainerFilter {
	return &ScheduleContainerFilter{conn: conn}
}

func (f *ScheduleContainerFilter) Name() string { return ScheduleContainerFilterName }

func (f *ScheduleContainerFilter) Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	if in.Actor != session.ActorScheduler || len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	if in.CommandType != commandtype.StopMining && in.CommandType != commandtype.Reboot && in.CommandType != commandtype.SetPowerTarget {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	containers, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) ([]string, error) {
		return q.GetContainerIdentifiersForCommands(ctx, sqlc.GetContainerIdentifiersForCommandsParams{
			OrgID: in.OrganizationID, DeviceIdentifiers: in.DeviceIdentifiers,
		})
	})
	if err != nil {
		return CommandFilterOutput{}, err
	}
	unsupported := make(map[string]bool, len(containers))
	for _, identifier := range containers {
		unsupported[identifier] = true
	}
	var out CommandFilterOutput
	for _, identifier := range in.DeviceIdentifiers {
		if !unsupported[identifier] {
			out.Kept = append(out.Kept, identifier)
			continue
		}
		out.Skipped = append(out.Skipped, SkippedDevice{
			DeviceIdentifier: identifier, FilterName: f.Name(),
			Reason: fmt.Sprintf("containers do not support generic %s commands", in.CommandType.String()),
		})
	}
	return out, nil
}
