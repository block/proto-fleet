package fleetmanagement

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	fleetMgmtSvc *fleetmanagement.Service
}

var _ fleetmanagementv1connect.FleetManagementServiceHandler = &Handler{}

func NewHandler(fleetMgmtSvc *fleetmanagement.Service) *Handler {
	return &Handler{
		fleetMgmtSvc: fleetMgmtSvc,
	}
}

func (h *Handler) ListMinerStateSnapshots(ctx context.Context, r *connect.Request[pb.ListMinerStateSnapshotsRequest]) (*connect.Response[pb.ListMinerStateSnapshotsResponse], error) {
	result, err := h.fleetMgmtSvc.ListMinerStateSnapshots(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) StreamMinerUpdates(ctx context.Context, r *connect.Request[pb.StreamMinerUpdatesRequest], stream *connect.ServerStream[pb.StreamMinerUpdatesResponse]) error {
	responseChan, err := h.fleetMgmtSvc.StreamMinerUpdates(ctx, r.Msg)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("stream context cancelled: %w", ctx.Err())
		case resp, ok := <-responseChan:
			if !ok {
				return nil
			}
			if err := stream.Send(resp); err != nil {
				return fleeterror.NewInternalErrorf("failed to send miner update: %v", err)
			}
		}
	}
}

func (h *Handler) StreamMinerListUpdates(ctx context.Context, r *connect.Request[pb.StreamMinerListUpdatesRequest], stream *connect.ServerStream[pb.StreamMinerListUpdatesResponse]) error {
	responseChan, err := h.fleetMgmtSvc.StreamMinerListUpdates(ctx, r.Msg)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("stream context cancelled: %w", ctx.Err())
		case resp, ok := <-responseChan:
			if !ok {
				return nil
			}
			if err := stream.Send(resp); err != nil {
				return fleeterror.NewInternalErrorf("failed to send miner list update: %v", err)
			}
		}
	}
}

func (h *Handler) GetMinerStateCounts(ctx context.Context, r *connect.Request[pb.GetMinerStateCountsRequest]) (*connect.Response[pb.GetMinerStateCountsResponse], error) {
	result, err := h.fleetMgmtSvc.GetMinerStateCounts(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetBatchMinerTelemetry(ctx context.Context, r *connect.Request[pb.GetBatchMinerTelemetryRequest]) (*connect.Response[pb.GetBatchMinerTelemetryResponse], error) {
	result, err := h.fleetMgmtSvc.GetBatchMinerTelemetry(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	// Convert raw storage units to display units (H/s → TH/s, W → kW, J/H → J/TH)
	for _, miner := range result.Miners {
		if miner == nil {
			continue
		}
		convertMeasurements(miner.Hashrate, models.MeasurementTypeHashrate)
		convertMeasurements(miner.PowerUsage, models.MeasurementTypePower)
		convertMeasurements(miner.Efficiency, models.MeasurementTypeEfficiency)
	}

	return connect.NewResponse(result), nil
}

// convertMeasurements converts measurement values from raw storage units to display units.
func convertMeasurements(measurements []*commonpb.Measurement, measurementType models.MeasurementType) {
	for _, m := range measurements {
		if m != nil {
			m.Value = models.ConvertToDisplayUnits(m.Value, measurementType)
		}
	}
}

func (h *Handler) GetMinerPoolAssignments(ctx context.Context, r *connect.Request[pb.GetMinerPoolAssignmentsRequest]) (*connect.Response[pb.GetMinerPoolAssignmentsResponse], error) {
	result, err := h.fleetMgmtSvc.GetMinerPoolAssignments(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerCoolingMode(ctx context.Context, r *connect.Request[pb.GetMinerCoolingModeRequest]) (*connect.Response[pb.GetMinerCoolingModeResponse], error) {
	result, err := h.fleetMgmtSvc.GetMinerCoolingMode(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) DeleteMiners(ctx context.Context, r *connect.Request[pb.DeleteMinersRequest]) (*connect.Response[pb.DeleteMinersResponse], error) {
	result, err := h.fleetMgmtSvc.DeleteMiners(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}
