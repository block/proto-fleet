package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	telemetryService *telemetry.TelemetryService
}

func NewHandler(telemetryService *telemetry.TelemetryService) *Handler {
	return &Handler{
		telemetryService: telemetryService,
	}
}

func (h *Handler) GetCombinedMetrics(
	ctx context.Context,
	req *connect.Request[telemetryv1.GetCombinedMetricsRequest],
) (*connect.Response[telemetryv1.GetCombinedMetricsResponse], error) {
	started := time.Now()
	info, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	query, err := toCombinedMetricsQuery(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	query.OrganizationID = info.OrganizationID

	slog.Info("GetCombinedMetrics request started", combinedMetricsHandlerLogArgs(query)...)

	combinedMetrics, err := h.telemetryService.GetCombinedMetrics(ctx, query)
	if err != nil {
		args := combinedMetricsHandlerLogArgs(query)
		args = append(args,
			"elapsed_ms", time.Since(started).Milliseconds(),
			"error", err,
		)
		slog.Warn("GetCombinedMetrics request failed", args...)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response, err := fromCombinedMetrics(combinedMetrics)
	if err != nil {
		args := combinedMetricsHandlerLogArgs(query)
		args = append(args,
			"elapsed_ms", time.Since(started).Milliseconds(),
			"metric_bucket_count", len(combinedMetrics.Metrics),
			"temperature_bucket_count", len(combinedMetrics.TemperatureStatusCounts),
			"uptime_bucket_count", len(combinedMetrics.UptimeStatusCounts),
			"error", err,
		)
		slog.Warn("GetCombinedMetrics response conversion failed", args...)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	args := combinedMetricsHandlerLogArgs(query)
	args = append(args,
		"elapsed_ms", time.Since(started).Milliseconds(),
		"metric_bucket_count", len(combinedMetrics.Metrics),
		"temperature_bucket_count", len(combinedMetrics.TemperatureStatusCounts),
		"uptime_bucket_count", len(combinedMetrics.UptimeStatusCounts),
		"has_live_state_counts", combinedMetrics.MinerStateCounts != nil,
	)
	slog.Info("GetCombinedMetrics request completed", args...)

	return connect.NewResponse(response), nil
}

func combinedMetricsHandlerLogArgs(query models.CombinedMetricsQuery) []any {
	args := []any{
		"component", "telemetry_handler",
		"org_id", query.OrganizationID,
		"device_selector_count", len(query.DeviceIDs),
		"all_devices", len(query.DeviceIDs) == 0,
		"measurement_count", len(query.MeasurementTypes),
		"aggregation_count", len(query.AggregationTypes),
		"site_count", len(query.SiteIDs),
		"include_unassigned", query.IncludeUnassigned,
		"page_size", query.PageSize,
	}
	if query.TimeRange.StartTime != nil {
		args = append(args, "start_time", query.TimeRange.StartTime.Format(time.RFC3339))
	}
	if query.TimeRange.EndTime != nil {
		args = append(args, "end_time", query.TimeRange.EndTime.Format(time.RFC3339))
	}
	if query.TimeRange.StartTime != nil && query.TimeRange.EndTime != nil {
		args = append(args, "range_ms", query.TimeRange.EndTime.Sub(*query.TimeRange.StartTime).Milliseconds())
	}
	if query.SlideInterval != nil {
		args = append(args, "slide_interval_ms", query.SlideInterval.Milliseconds())
	}
	return args
}

func (h *Handler) StreamCombinedMetricUpdates(
	ctx context.Context,
	req *connect.Request[telemetryv1.StreamCombinedMetricUpdatesRequest],
	stream *connect.ServerStream[telemetryv1.StreamCombinedMetricUpdatesResponse],
) error {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{})
	if err != nil {
		return err
	}

	query, err := toStreamCombinedMetricsQuery(req.Msg)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Pass organization ID to enable miner state counts in stream
	query.OrganizationID = info.OrganizationID

	updateChan, err := h.telemetryService.StreamCombinedMetrics(ctx, query)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start combined metrics stream: %w", err))
	}

	for {
		select {
		case <-ctx.Done():
			return fleeterror.NewCanceledError()
		case combinedMetrics, ok := <-updateChan:
			if !ok {
				return nil
			}

			response, err := h.convertCombinedMetricsToStreamResponse(combinedMetrics, query.UpdateInterval)
			if err != nil {
				return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert metrics to stream response: %w", err))
			}

			if err := stream.Send(response); err != nil {
				return fleeterror.NewInternalErrorf("failed to send stream response: %v", err)
			}
		}
	}
}

func (h *Handler) convertCombinedMetricsToStreamResponse(combinedMetrics models.CombinedMetric, updateInterval time.Duration) (*telemetryv1.StreamCombinedMetricUpdatesResponse, error) {
	metrics, err := convertMetricsToProto(combinedMetrics.Metrics)
	if err != nil {
		return nil, err
	}

	response := &telemetryv1.StreamCombinedMetricUpdatesResponse{
		Metrics:                 metrics,
		NextUpdateTime:          timestamppb.New(time.Now().Add(updateInterval)),
		TemperatureStatusCounts: convertTemperatureStatusCounts(combinedMetrics.TemperatureStatusCounts),
		UptimeStatusCounts:      convertUptimeStatusCounts(combinedMetrics.UptimeStatusCounts),
	}

	if combinedMetrics.MinerStateCounts != nil {
		response.MinerStateCounts = &telemetryv1.MinerStateCounts{
			HashingCount:  combinedMetrics.MinerStateCounts.Hashing,
			BrokenCount:   combinedMetrics.MinerStateCounts.Broken,
			OfflineCount:  combinedMetrics.MinerStateCounts.Offline,
			SleepingCount: combinedMetrics.MinerStateCounts.Sleeping,
		}
	}

	return response, nil
}
