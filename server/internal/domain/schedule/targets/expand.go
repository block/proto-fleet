package targets

import (
	"context"
	"fmt"
	"strconv"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// UnspecifiedTargetHandler lets callers preserve their local behavior for
// SCHEDULE_TARGET_TYPE_UNSPECIFIED without duplicating the target expansion
// logic. Pass nil to ignore unspecified targets silently.
type UnspecifiedTargetHandler func(targetID string)

// Expand converts schedule targets into deduplicated device identifiers. Rack
// and group targets are expanded through the collection store. Output order
// follows target order, with duplicate identifiers omitted.
func Expand(
	ctx context.Context,
	collectionStore stores.CollectionStore,
	scheduleTargets []*pb.ScheduleTarget,
	orgID int64,
	onUnspecified UnspecifiedTargetHandler,
) ([]string, error) {
	seen := make(map[string]struct{})
	var identifiers []string

	for _, target := range scheduleTargets {
		switch target.TargetType {
		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER:
			if _, dup := seen[target.TargetId]; !dup {
				seen[target.TargetId] = struct{}{}
				identifiers = append(identifiers, target.TargetId)
			}

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK:
			rackID, err := strconv.ParseInt(target.TargetId, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid rack target_id %q: %w", target.TargetId, err)
			}
			rackDevices, err := collectionStore.GetDeviceIdentifiersByDeviceSetID(ctx, rackID, orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve rack %d devices: %w", rackID, err)
			}
			for _, device := range rackDevices {
				if _, dup := seen[device]; !dup {
					seen[device] = struct{}{}
					identifiers = append(identifiers, device)
				}
			}

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP:
			groupID, err := strconv.ParseInt(target.TargetId, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid group target_id %q: %w", target.TargetId, err)
			}
			groupDevices, err := collectionStore.GetDeviceIdentifiersByDeviceSetID(ctx, groupID, orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve group %d devices: %w", groupID, err)
			}
			for _, device := range groupDevices {
				if _, dup := seen[device]; !dup {
					seen[device] = struct{}{}
					identifiers = append(identifiers, device)
				}
			}

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED:
			if onUnspecified != nil {
				onUnspecified(target.TargetId)
			}
		}
	}

	return identifiers, nil
}
