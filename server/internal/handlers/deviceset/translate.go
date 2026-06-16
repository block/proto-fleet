package deviceset

import (
	dspb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	"github.com/block/proto-fleet/server/internal/domain/collection"
)

// toAssignDevicesToRackParams translates the proto request into the
// domain-layer input shape. Mirrors the proto→domain helpers under
// server/internal/handlers/sites/translate.go so handler bodies stay
// focused on auth + wiring.
func toAssignDevicesToRackParams(req *dspb.AssignDevicesToRackRequest, orgID int64) collection.AssignDevicesToRackParams {
	var targetRackID *int64
	if req.TargetRackId != nil {
		v := req.GetTargetRackId()
		targetRackID = &v
	}
	return collection.AssignDevicesToRackParams{
		OrgID:             orgID,
		TargetRackID:      targetRackID,
		DeviceIdentifiers: req.GetDeviceIdentifiers(),
	}
}
