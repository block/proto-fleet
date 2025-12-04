package telemetry

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
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
	info, err := session.GetInfo(ctx)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("failed to get session info: %w", err))
	}
	query, err := toStreamQuery(req.Msg)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	updateTelemetryChan, err := h.telemetryService.StreamTelemetryUpdates(ctx, query)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	updateStatusChan, err := h.telemetryService.StreamDeviceStatusUpdates(ctx, query)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}

	updateCountsChan, err := h.telemetryService.StreamMinerStateCounts(ctx, info.OrganizationID, *query.HeartbeatInterval)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start miner state counts stream: %w", err))
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		case update, ok := <-updateTelemetryChan:
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
		case update, ok := <-updateStatusChan:
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
		case update, ok := <-updateCountsChan:
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

func (h *Handler) GetCombinedMetrics(
	ctx context.Context,
	req *connect.Request[telemetryv1.GetCombinedMetricsRequest],
) (*connect.Response[telemetryv1.GetCombinedMetricsResponse], error) {
	query, err := toCombinedMetricsQuery(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	combinedMetrics, err := h.telemetryService.GetCombinedMetrics(ctx, query)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response, err := fromCombinedMetrics(combinedMetrics)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(response), nil
}

func (h *Handler) StreamCombinedMetricUpdates(
	ctx context.Context,
	req *connect.Request[telemetryv1.StreamCombinedMetricUpdatesRequest],
	stream *connect.ServerStream[telemetryv1.StreamCombinedMetricUpdatesResponse],
) error {
	query, err := toStreamCombinedMetricsQuery(req.Msg)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	updateChan, err := h.telemetryService.StreamCombinedMetrics(ctx, query)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start combined metrics stream: %w", err))
	}

	for {
		select {
		case <-ctx.Done():
			return connect.NewError(connect.CodeAborted, fmt.Errorf("context cancelled: %w", ctx.Err()))
		case combinedMetrics, ok := <-updateChan:
			if !ok {
				return nil
			}

			response, err := h.convertCombinedMetricsToStreamResponse(combinedMetrics, query.UpdateInterval)
			if err != nil {
				return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert metrics to stream response: %w", err))
			}

			if err := stream.Send(response); err != nil {
				return fmt.Errorf("failed to send stream response: %w", err)
			}
		}
	}
}

func (h *Handler) convertCombinedMetricsToStreamResponse(combinedMetrics models.CombinedMetric, updateInterval time.Duration) (*telemetryv1.StreamCombinedMetricUpdatesResponse, error) {
	metrics := make([]*telemetryv1.Metric, len(combinedMetrics.Metrics))
	for i, metric := range combinedMetrics.Metrics {
		measurementType, err := measurementTypeToProto(metric.MeasurementType)
		if err != nil {
			return nil, fmt.Errorf("failed to convert measurement type: %w", err)
		}

		aggregatedValues := make([]*telemetryv1.AggregatedValue, len(metric.AggregatedValues))
		for j, aggValue := range metric.AggregatedValues {
			aggregationType, err := aggregationTypeToProto(aggValue.Type)
			if err != nil {
				return nil, fmt.Errorf("failed to convert aggregation type: %w", err)
			}

			aggregatedValues[j] = &telemetryv1.AggregatedValue{
				AggregationType: aggregationType,
				Value:           aggValue.Value,
			}
		}

		metrics[i] = &telemetryv1.Metric{
			MeasurementType:  measurementType,
			OpenTime:         timestamppb.New(metric.OpenTime),
			AggregatedValues: aggregatedValues,
		}
	}

	// Convert temperature status counts if present
	var temperatureStatusCounts []*telemetryv1.TemperatureStatusCount
	for _, statusCount := range combinedMetrics.TemperatureStatusCounts {
		temperatureStatusCounts = append(temperatureStatusCounts, &telemetryv1.TemperatureStatusCount{
			Timestamp:     timestamppb.New(statusCount.Timestamp),
			ColdCount:     statusCount.ColdCount,
			OkCount:       statusCount.OkCount,
			HotCount:      statusCount.HotCount,
			CriticalCount: statusCount.CriticalCount,
		})
	}

	// Convert uptime status counts if present
	var uptimeStatusCounts []*telemetryv1.UptimeStatusCount
	for _, statusCount := range combinedMetrics.UptimeStatusCounts {
		uptimeStatusCounts = append(uptimeStatusCounts, &telemetryv1.UptimeStatusCount{
			Timestamp:       timestamppb.New(statusCount.Timestamp),
			HashingCount:    statusCount.HashingCount,
			NotHashingCount: statusCount.NotHashingCount,
		})
	}

	nextUpdateTime := time.Now().Add(updateInterval)

	return &telemetryv1.StreamCombinedMetricUpdatesResponse{
		Metrics:                 metrics,
		NextUpdateTime:          timestamppb.New(nextUpdateTime),
		TemperatureStatusCounts: temperatureStatusCounts,
		UptimeStatusCounts:      uptimeStatusCounts,
	}, nil
}
