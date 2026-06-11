package admin

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// maxDiscoveredPageSize bounds one ListFleetNodeDiscoveredDevices response (and
// is the default when the request omits a limit). Matches the proto limit cap.
const maxDiscoveredPageSize = 1024

func (h *Handler) ListFleetNodeDiscoveredDevices(ctx context.Context, req *connect.Request[pb.ListFleetNodeDiscoveredDevicesRequest]) (*connect.Response[pb.ListFleetNodeDiscoveredDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	var nodeFilter *int64
	if id := req.Msg.GetFleetNodeId(); id > 0 {
		nodeFilter = &id
	}
	var cursor *int64
	if c := req.Msg.GetCursor(); c > 0 {
		cursor = &c
	}
	// Default + clamp so an omitted/zero limit can't return the whole org at once.
	pageSize := int64(req.Msg.GetLimit())
	if pageSize <= 0 || pageSize > maxDiscoveredPageSize {
		pageSize = maxDiscoveredPageSize
	}
	devices, nextCursor, err := h.pairing.ListDiscoveredDevicesForFleetNode(ctx, info.OrganizationID, nodeFilter, cursor, &pageSize)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListFleetNodeDiscoveredDevicesResponse{Devices: make([]*pb.FleetNodeDiscoveredDevice, 0, len(devices))}
	for _, d := range devices {
		dev := &pb.FleetNodeDiscoveredDevice{
			FleetNodeId:      d.FleetNodeID,
			DeviceIdentifier: d.DeviceIdentifier,
			IpAddress:        d.IPAddress,
			Port:             d.Port,
			UrlScheme:        d.URLScheme,
			DriverName:       d.DriverName,
			Model:            d.Model,
			Manufacturer:     d.Manufacturer,
			FirmwareVersion:  d.FirmwareVersion,
			PairingStatus:    d.PairingStatus,
		}
		if !d.LastSeen.IsZero() {
			dev.LastSeen = timestamppb.New(d.LastSeen)
		}
		resp.Devices = append(resp.Devices, dev)
	}
	if nextCursor != nil {
		resp.NextCursor = *nextCursor
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) PairDiscoveredDevicesOnFleetNode(ctx context.Context, req *connect.Request[pb.PairDiscoveredDevicesOnFleetNodeRequest], stream *connect.ServerStream[pb.PairDiscoveredDevicesOnFleetNodeResponse]) error {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerPair, authz.ResourceContext{})
	if err != nil {
		return err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{}); err != nil {
		return err
	}
	fleetNodeID := req.Msg.GetFleetNodeId()
	if fleetNodeID <= 0 {
		return fleeterror.NewInvalidArgumentError("fleet_node_id is required")
	}

	node, err := h.enrollment.GetFleetNodeByID(ctx, fleetNodeID, info.OrganizationID)
	if err != nil {
		return err
	}
	if node.EnrollmentStatus != enrollment.FleetNodeStatusConfirmed {
		return fleeterror.NewFailedPreconditionError("fleet node is not CONFIRMED")
	}

	targets, err := h.pairing.ResolvePairTargets(ctx, fleetNodeID, info.OrganizationID, req.Msg.GetDeviceIdentifiers(), req.Msg.GetPairAllUnpaired(), req.Msg.GetCredentials())
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fleeterror.NewInvalidArgumentError("no pairable devices for the requested selection")
	}

	credentials := req.Msg.GetCredentials()
	assignedBy := info.UserID

	// The gateway persists results authoritatively; this callback only forwards them
	// for live display, so a send failure (operator gone) must not abort the command.
	return h.pairing.PairOnNode(ctx, fleetNodeID, targets, credentials, info.OrganizationID, &assignedBy,
		func(results []*gatewaypb.FleetNodePairResult) error {
			// Forward only while the operator is connected; once gone, skip the send so
			// it can't block the command (which keeps persisting server-side).
			if ctx.Err() == nil {
				out := &pb.PairDiscoveredDevicesOnFleetNodeResponse{Results: make([]*pb.DevicePairingResult, 0, len(results))}
				for _, r := range results {
					res := &pb.DevicePairingResult{
						DeviceIdentifier: r.GetDeviceIdentifier(),
						PairingStatus:    pairOutcomeStatus(r.GetOutcome()),
					}
					if res.PairingStatus != fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED {
						res.Error = r.GetErrorMessage()
					}
					out.Results = append(out.Results, res)
				}
				if sendErr := stream.Send(out); sendErr != nil {
					slog.Warn("operator pair stream send failed; pairing continues server-side",
						"fleet_node_id", fleetNodeID, "err", sendErr)
				}
			}
			return nil
		})
}

// pairOutcomeStatus maps a node pair outcome to the operator-facing enum, matching
// what PersistFleetNodePairResult records.
func pairOutcomeStatus(outcome gatewaypb.PairOutcome) fleetmanagementv1.PairingStatus {
	switch outcome {
	case gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED:
		return fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED
	case gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED, gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED:
		return fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED
	case gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR, gatewaypb.PairOutcome_PAIR_OUTCOME_UNSPECIFIED:
		return fleetmanagementv1.PairingStatus_PAIRING_STATUS_FAILED
	default:
		return fleetmanagementv1.PairingStatus_PAIRING_STATUS_FAILED
	}
}
