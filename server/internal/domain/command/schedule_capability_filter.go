package command

import (
	"context"
	"database/sql"
	"fmt"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

const ScheduleCapabilityFilterName = "schedule_capability"

var scheduledCommandTypes = map[commandtype.Type]pb.CommandType{
	commandtype.StopMining:     pb.CommandType_COMMAND_TYPE_STOP_MINING,
	commandtype.Reboot:         pb.CommandType_COMMAND_TYPE_REBOOT,
	commandtype.SetPowerTarget: pb.CommandType_COMMAND_TYPE_SET_POWER_TARGET,
}

// ScheduleCapabilityFilter excludes unsupported schedule targets before enqueue.
// Site membership includes containers, whose tank controls do not imply support
// for generic miner commands.
type ScheduleCapabilityFilter struct {
	checker *CapabilityChecker
}

func NewScheduleCapabilityFilter(conn *sql.DB, provider CapabilitiesProvider) *ScheduleCapabilityFilter {
	return &ScheduleCapabilityFilter{checker: NewCapabilityChecker(conn, provider)}
}

func (f *ScheduleCapabilityFilter) Name() string { return ScheduleCapabilityFilterName }

func (f *ScheduleCapabilityFilter) Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	if in.Actor != session.ActorScheduler || len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	commandType, scheduled := scheduledCommandTypes[in.CommandType]
	if !scheduled {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	if f.checker.capabilitiesProvider == nil {
		return CommandFilterOutput{}, fmt.Errorf("schedule capability provider is not configured")
	}
	devices, err := f.checker.getDeviceInfoByDeviceIdentifiers(ctx, in.DeviceIdentifiers, in.OrganizationID)
	if err != nil {
		return CommandFilterOutput{}, err
	}
	required := GetRequiredCapabilities(commandType)
	supported := make(map[string]bool, len(devices))
	for _, device := range devices {
		supported[device.DeviceIdentifier] = f.checker.deviceSupportsCommand(ctx, device, required)
	}
	var out CommandFilterOutput
	for _, identifier := range in.DeviceIdentifiers {
		canExecute, available := supported[identifier]
		if canExecute {
			out.Kept = append(out.Kept, identifier)
			continue
		}
		reason := fmt.Sprintf("device does not advertise support for %s", in.CommandType.String())
		if !available {
			reason = "device is unavailable for scheduled commands"
		}
		out.Skipped = append(out.Skipped, SkippedDevice{
			DeviceIdentifier: identifier, FilterName: f.Name(), Reason: reason,
		})
	}
	return out, nil
}
