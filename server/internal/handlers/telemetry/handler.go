package telemetry

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	telemetryv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
)

type Handler struct {
	telemetryService *telemetry.TelemetryService
}

func NewHandler(telemetryService *telemetry.TelemetryService) *Handler {
	return &Handler{
		telemetryService: telemetryService,
	}
}

func (h *Handler) GetSnapshot(
	ctx context.Context,
	req *connect.Request[telemetryv1.GetSnapshotRequest],
) (*connect.Response[telemetryv1.GetSnapshotResponse], error) {
	query, err := toLatestTelemetryQuery(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	telemetryData, err := h.telemetryService.GetLatestTelemetry(ctx, query)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response, err := fromTelemetryData(telemetryData)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&telemetryv1.GetSnapshotResponse{
		Telemetry: response,
	}), nil
}

func (h *Handler) GetTimeSeries(
	ctx context.Context,
	req *connect.Request[telemetryv1.GetTimeSeriesRequest],
) (*connect.Response[telemetryv1.GetTimeSeriesResponse], error) {
	query, err := toTimeSeriesTelemetryQuery(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	telemetryData, err := h.telemetryService.GetTimeSeriesTelemetry(ctx, query)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response, err := fromTelemetryData(telemetryData)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&telemetryv1.GetTimeSeriesResponse{
		Telemetry: response,
	}), nil
}

func (h *Handler) GetMetadata(
	ctx context.Context,
	req *connect.Request[telemetryv1.GetMetadataRequest],
) (*connect.Response[telemetryv1.GetMetadataResponse], error) {
	query, err := toMetadataQuery(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	metadata, err := h.telemetryService.GetTelemetryMetadata(ctx, query)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response, err := fromDeviceMetadata(metadata)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&telemetryv1.GetMetadataResponse{
		Devices: response,
	}), nil
}

func (h *Handler) StreamUpdates(
	ctx context.Context,
	req *connect.Request[telemetryv1.StreamUpdatesRequest],
	stream *connect.ServerStream[telemetryv1.StreamUpdatesResponse],
) error {
	query, err := toStreamQuery(req.Msg)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	updateChan, err := h.telemetryService.StreamTelemetryUpdates(ctx, query)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		case update, ok := <-updateChan:
			if !ok {
				return nil
			}

			response, err := fromTelemetryUpdate(update)
			if err != nil {
				return connect.NewError(connect.CodeInternal, err)
			}

			if err := stream.Send(response); err != nil {
				return fmt.Errorf("failed to send stream response: %w", err)
			}
		}
	}
}

func (h *Handler) GetAggregatedSnapshot(
	ctx context.Context,
	req *connect.Request[telemetryv1.GetAggregatedSnapshotRequest],
) (*connect.Response[telemetryv1.GetAggregatedSnapshotResponse], error) {
	query, err := toAggregationQuery(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	aggregatedData, err := h.telemetryService.GetAggregatedTelemetry(ctx, query)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response, err := fromAggregatedTelemetry(aggregatedData)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&telemetryv1.GetAggregatedSnapshotResponse{
		AggregatedData: response,
	}), nil
}
