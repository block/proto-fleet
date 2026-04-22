package telemetry

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
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
	query, err := toCombinedMetricsQuery(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Scope to caller's org; store returns nil counts when OrgID is unset.
	if info, err := session.GetInfo(ctx); err == nil {
		query.OrganizationID = info.OrganizationID
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
	info, err := session.GetInfo(ctx)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("failed to get session info: %w", err))
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
