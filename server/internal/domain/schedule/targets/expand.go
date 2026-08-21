package targets

import (
	"context"
	"fmt"
	"strconv"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// UnspecifiedTargetHandler lets callers preserve their local behavior for
// SCHEDULE_TARGET_TYPE_UNSPECIFIED without duplicating the target expansion
// logic. Pass nil to ignore unspecified targets silently.
type UnspecifiedTargetHandler func(targetID string)

// DeviceResolver is the slice of the device store Expand needs to turn a site
// or building target into device identifiers. Satisfied by interfaces.DeviceStore.
type DeviceResolver interface {
	GetDeviceIdentifiersByOrgWithFilter(ctx context.Context, orgID int64, filter *stores.MinerFilter) ([]string, error)
}

// scheduleTargetPairingStatuses is the paired-like set site/building expansion
// filters to. GetDeviceIdentifiersByOrgWithFilter defaults to PAIRED-only when
// PairingStatuses is empty, which would silently drop auth-needed / default-
// password miners; pass the set explicitly to match how the rest of the fleet
// (and the building/collection device-stats paths) count membership.
var scheduleTargetPairingStatuses = []fm.PairingStatus{
	fm.PairingStatus_PAIRING_STATUS_PAIRED,
	fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
	fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
}

// Expand converts schedule targets into deduplicated device identifiers. Rack
// and group targets resolve through the collection store; site and building
// targets resolve through the device store at call time (dynamic — they
// reflect whatever paired miners are at the site/building now, not a
// create-time snapshot). Output order follows target order, with duplicate
// identifiers omitted.
func Expand(
	ctx context.Context,
	collectionStore stores.CollectionStore,
	deviceResolver DeviceResolver,
	scheduleTargets []*pb.ScheduleTarget,
	orgID int64,
	onUnspecified UnspecifiedTargetHandler,
) ([]string, error) {
	seen := make(map[string]struct{})
	var identifiers []string

	addAll := func(devices []string) {
		for _, device := range devices {
			if _, dup := seen[device]; !dup {
				seen[device] = struct{}{}
				identifiers = append(identifiers, device)
			}
		}
	}

	for _, target := range scheduleTargets {
		switch target.TargetType {
		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER:
			addAll([]string{target.TargetId})

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK:
			rackID, err := strconv.ParseInt(target.TargetId, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid rack target_id %q: %w", target.TargetId, err)
			}
			rackDevices, err := collectionStore.GetDeviceIdentifiersByDeviceSetID(ctx, rackID, orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve rack %d devices: %w", rackID, err)
			}
			addAll(rackDevices)

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP:
			groupID, err := strconv.ParseInt(target.TargetId, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid group target_id %q: %w", target.TargetId, err)
			}
			groupDevices, err := collectionStore.GetDeviceIdentifiersByDeviceSetID(ctx, groupID, orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve group %d devices: %w", groupID, err)
			}
			addAll(groupDevices)

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE:
			siteID, err := strconv.ParseInt(target.TargetId, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid site target_id %q: %w", target.TargetId, err)
			}
			siteDevices, err := deviceResolver.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, &stores.MinerFilter{
				SiteIDs:         []int64{siteID},
				PairingStatuses: scheduleTargetPairingStatuses,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to resolve site %d devices: %w", siteID, err)
			}
			addAll(siteDevices)

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_BUILDING:
			buildingID, err := strconv.ParseInt(target.TargetId, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid building target_id %q: %w", target.TargetId, err)
			}
			buildingDevices, err := deviceResolver.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, &stores.MinerFilter{
				BuildingIDs:     []int64{buildingID},
				PairingStatuses: scheduleTargetPairingStatuses,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to resolve building %d devices: %w", buildingID, err)
			}
			addAll(buildingDevices)

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED:
			if onUnspecified != nil {
				onUnspecified(target.TargetId)
			}
		}
	}

	return identifiers, nil
}
