package deviceset

import (
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	dspb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// toAssignDevicesToRackParams translates the proto request into the
// domain-layer input shape. Mirrors the proto→domain helpers under
// server/internal/handlers/sites/translate.go so handler bodies stay
// focused on auth + wiring.
//
// Only the device_list selector variant is accepted. all_devices is
// rejected with InvalidArgument because moving every paired device
// into a single rack is never the intended operation; filter-based
// selectors are reserved for a future expansion.
func toAssignDevicesToRackParams(req *dspb.AssignDevicesToRackRequest, orgID int64) (collection.AssignDevicesToRackParams, error) {
	var targetRackID *int64
	if req.TargetRackId != nil {
		v := req.GetTargetRackId()
		targetRackID = &v
	}
	identifiers, err := identifiersFromAssignSelector(req.GetDeviceSelector())
	if err != nil {
		return collection.AssignDevicesToRackParams{}, err
	}
	return collection.AssignDevicesToRackParams{
		OrgID:             orgID,
		TargetRackID:      targetRackID,
		DeviceIdentifiers: identifiers,
	}, nil
}

// identifiersFromAssignSelector enforces the device_list-only contract
// for AssignDevicesToRack. The all_devices variant and the unset
// oneof are both rejected with InvalidArgument so callers learn
// up-front instead of getting a 0-row response or a panic downstream.
func identifiersFromAssignSelector(sel *commonpb.DeviceSelector) ([]string, error) {
	if sel == nil {
		return nil, fleeterror.NewInvalidArgumentError("device_selector is required")
	}
	switch v := sel.GetSelectionType().(type) {
	case *commonpb.DeviceSelector_DeviceList:
		if v.DeviceList == nil {
			return nil, fleeterror.NewInvalidArgumentError("device_selector.device_list is required")
		}
		return v.DeviceList.GetDeviceIdentifiers(), nil
	case *commonpb.DeviceSelector_AllDevices:
		return nil, fleeterror.NewInvalidArgumentError("device_selector.all_devices is not supported for AssignDevicesToRack; pass an explicit device_list")
	default:
		return nil, fleeterror.NewInvalidArgumentError("device_selector must set device_list")
	}
}
