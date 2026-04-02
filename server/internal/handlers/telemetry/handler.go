package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// activeStream tracks an active streaming goroutine with its cancel function and unique ID.
type activeStream struct {
	cancel func()
	id     uint64
}

// telemetryStreamIDCounter generates unique IDs for active streams.
var telemetryStreamIDCounter uint64

type Handler struct {
	telemetryService *telemetry.TelemetryService

	// Stream deduplication: ensures only one active stream per session per endpoint.
	// When a new stream request arrives for a session that already has an active stream,
	// the previous stream is cancelled to prevent connection exhaustion from rapid scrolling.
	activeCombinedStreams   map[string]*activeStream
	activeCombinedStreamsMu sync.Mutex
}

func NewHandler(telemetryService *telemetry.TelemetryService) *Handler {
	return &Handler{
		telemetryService:      telemetryService,
		activeCombinedStreams: make(map[string]*activeStream),
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

	// Build deduplication key from session ID and connection ID
	// This allows multiple tabs (different connection IDs) to have independent streams
	dedupeKey := info.SessionID
	if req.Msg.ConnectionId != "" {
		dedupeKey = info.SessionID + ":" + req.Msg.ConnectionId
	}

	// Cancel any existing stream for this session+connection to prevent connection exhaustion
	h.activeCombinedStreamsMu.Lock()
	if existing, exists := h.activeCombinedStreams[dedupeKey]; exists {
		existing.cancel()
		slog.Debug("cancelled existing combined metrics stream", "dedupeKey", dedupeKey)
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	streamID := atomic.AddUint64(&telemetryStreamIDCounter, 1)
	h.activeCombinedStreams[dedupeKey] = &activeStream{
		cancel: cancelStream,
		id:     streamID,
	}
	h.activeCombinedStreamsMu.Unlock()

	// Clean up on exit
	defer func() {
		cancelStream()
		h.activeCombinedStreamsMu.Lock()
		if existing, exists := h.activeCombinedStreams[dedupeKey]; exists && existing.id == streamID {
			delete(h.activeCombinedStreams, dedupeKey)
			slog.Debug("cleaned up combined metrics stream", "dedupeKey", dedupeKey, "streamID", streamID)
		}
		h.activeCombinedStreamsMu.Unlock()
	}()

	query, err := toStreamCombinedMetricsQuery(req.Msg)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Pass organization ID to enable miner state counts in stream
	query.OrganizationID = info.OrganizationID

	updateChan, err := h.telemetryService.StreamCombinedMetrics(streamCtx, query)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start combined metrics stream: %w", err))
	}

	for {
		select {
		case <-streamCtx.Done():
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
