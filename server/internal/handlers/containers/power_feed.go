package containers

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// authorizePowerFeedRead requires the reads needed to see the module, at the
// container's current site.
func authorizePowerFeedRead(ctx context.Context, site *int64) error {
	for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: site}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) GetModulePowerFeed(ctx context.Context, req *connect.Request[pb.GetModulePowerFeedRequest]) (*connect.Response[pb.GetModulePowerFeedResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("module commands unavailable")
	}
	result, err := h.commands.ReadModulePowerFeed(ctx, req.Msg.ModuleId, authorizePowerFeedRead)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(modulePowerFeedResponse(result)), nil
}

func modulePowerFeedResponse(r command.ModulePowerFeed) *pb.GetModulePowerFeedResponse {
	i := r.Inspection
	out := &pb.GetModulePowerFeedResponse{
		ModuleId: r.ModuleID, DeviceIdentifier: r.DeviceIdentifier, ContainerName: r.ContainerName, Tank: r.Tank, Module: r.Module,
		ObservedAt: timestamppb.New(i.ObservedAt), Mapping: powerFeedMapping(i.Mapping), FeedUnavailableReason: i.FeedUnavailableReason,
		ControllerWritePrimary: i.ControllerWritePrimary, RecoveryCycleKnown: i.RecoveryCycleKnown,
	}
	if f := i.Feed; f != nil {
		state := pb.PowerFeedState_POWER_FEED_STATE_UNSPECIFIED
		switch f.State {
		case sdk.PowerFeedStateEnabled:
			state = pb.PowerFeedState_POWER_FEED_STATE_ENABLED
		case sdk.PowerFeedStateDisabled:
			state = pb.PowerFeedState_POWER_FEED_STATE_DISABLED
		}
		out.Feed = &pb.PowerFeedReading{
			Id: f.ID, State: state, Active: f.Active, Faulted: f.Faulted, Overridden: f.Overridden, Phase: f.Phase,
			VoltageV: f.VoltageV, CurrentA: f.CurrentA, PowerW: f.PowerW,
			VoltageAbV: f.VoltageABV, VoltageBcV: f.VoltageBCV, VoltageCaV: f.VoltageCAV,
			CurrentAA: f.CurrentAA, CurrentBA: f.CurrentBA, CurrentCA: f.CurrentCA,
		}
	}
	if c := i.RecoveryCycle; c != nil {
		out.RecoveryCycle = &pb.PowerRecoveryCycle{Id: c.ID, Scope: c.Scope, Phase: c.Phase, OutletIds: c.OutletIDs, RestoreAfter: optionalTimestamp(c.RestoreAfter), CreatedAt: optionalTimestamp(c.CreatedAt), LastError: c.LastError}
	}
	return out
}

func powerFeedMapping(m sdk.PowerFeedMapping) *pb.PowerFeedMapping {
	confidence := pb.PowerFeedMappingConfidence_POWER_FEED_MAPPING_CONFIDENCE_UNAVAILABLE
	switch m.Confidence {
	case sdk.PowerFeedMappingWiringVerified:
		confidence = pb.PowerFeedMappingConfidence_POWER_FEED_MAPPING_CONFIDENCE_WIRING_VERIFIED
	case sdk.PowerFeedMappingConfigured:
		confidence = pb.PowerFeedMappingConfidence_POWER_FEED_MAPPING_CONFIDENCE_CONFIGURED
	case sdk.PowerFeedMappingAmbiguous:
		confidence = pb.PowerFeedMappingConfidence_POWER_FEED_MAPPING_CONFIDENCE_AMBIGUOUS
	}
	out := &pb.PowerFeedMapping{Confidence: confidence, FeedId: m.FeedID, Reasons: m.Reasons}
	for _, ref := range m.SharedWith {
		out.SharedWith = append(out.SharedWith, &pb.PowerFeedModuleRef{Tank: ref.Tank, Module: ref.Module})
	}
	for _, b := range m.Boards {
		out.Boards = append(out.Boards, &pb.PowerFeedBoardEvidence{Position: b.Position, Occupied: b.Occupied, SerialNumber: b.Serial, SlotFeedId: b.SlotFeedID, ObservedFeedId: b.ObservedFeedID, ExpectedFeedId: b.ExpectedFeedID, WiringStatus: b.WiringStatus, WiringReasons: b.WiringReasons})
	}
	return out
}
