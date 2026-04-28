// Package serverlog implements the ServerLogService Connect-RPC endpoint.
//
// The handler is intentionally thin: it pulls a snapshot from the
// process-global slog ring buffer (see internal/infrastructure/logging) and
// translates the records into the protobuf shape the UI expects. There is
// no persistence — restarts wipe the visible history.
package serverlog

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/serverlog/v1"
	"github.com/block/proto-fleet/server/generated/grpc/serverlog/v1/serverlogv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/logging"
)

// DefaultLimit is applied when the request asks for limit == 0.
//
// We cap the default well below the buffer capacity so the wire payload
// stays small for typical polls; clients tailing live logs only care about
// recent records and can request more if they need the full window.
const DefaultLimit = 500

// Handler implements the ServerLogService.
type Handler struct {
	buffer *logging.Buffer
}

var _ serverlogv1connect.ServerLogServiceHandler = &Handler{}

// NewHandler returns a Handler reading from the given buffer.
//
// In production the buffer is logging.DefaultBuffer(), wired up in main.go
// after logging.InitLogger has run. Tests can pass an isolated buffer.
func NewHandler(buffer *logging.Buffer) *Handler {
	return &Handler{buffer: buffer}
}

// ListServerLogs implements ServerLogService.ListServerLogs.
func (h *Handler) ListServerLogs(
	_ context.Context,
	req *connect.Request[pb.ListServerLogsRequest],
) (*connect.Response[pb.ListServerLogsResponse], error) {
	if h.buffer == nil {
		// We never want this RPC to silently return an empty list when
		// the buffer wasn't wired up — that masks a deployment bug.
		return nil, fleeterror.NewInternalError("server log buffer not configured")
	}

	limit := int(req.Msg.GetLimit())
	if limit <= 0 {
		limit = DefaultLimit
	}

	snap := h.buffer.Snapshot(logging.SnapshotOptions{
		SinceID:  req.Msg.GetSinceId(),
		MinLevel: protoLevelToSlog(req.Msg.GetMinLevel()),
		Search:   req.Msg.GetSearchText(),
		Limit:    limit,
	})

	entries := make([]*pb.LogEntry, len(snap.Records))
	for i, r := range snap.Records {
		entries[i] = recordToProto(r)
	}

	// #nosec G115 -- buffer Size and Capacity are bounded by config; safe int conversion.
	return connect.NewResponse(&pb.ListServerLogsResponse{
		Entries:        entries,
		LatestId:       snap.LatestID,
		BufferSize:     int32(snap.Size),
		BufferCapacity: int32(h.buffer.Capacity()),
		Truncated:      snap.Truncated,
	}), nil
}

// recordToProto converts a buffered record to the wire format.
func recordToProto(r logging.BufferedRecord) *pb.LogEntry {
	attrs := make([]*pb.LogAttr, len(r.Attrs))
	for i, kv := range r.Attrs {
		attrs[i] = &pb.LogAttr{Key: kv.Key, Value: kv.Value}
	}
	return &pb.LogEntry{
		Id:      r.ID,
		Time:    timestamppb.New(r.Time),
		Level:   slogLevelToProto(r.Level),
		Message: r.Message,
		Attrs:   attrs,
		Source:  r.Source,
	}
}

// slogLevelToProto maps slog's integer level scale onto our enum. slog
// uses arithmetic on slog.Level (so Info+1 is "Notice", etc.); we collapse
// those into the four standard buckets the UI surfaces.
func slogLevelToProto(l slog.Level) pb.LogLevel {
	switch {
	case l <= slog.LevelDebug:
		return pb.LogLevel_LOG_LEVEL_DEBUG
	case l < slog.LevelWarn:
		return pb.LogLevel_LOG_LEVEL_INFO
	case l < slog.LevelError:
		return pb.LogLevel_LOG_LEVEL_WARN
	default:
		return pb.LogLevel_LOG_LEVEL_ERROR
	}
}

// protoLevelToSlog maps the proto enum back onto slog's level scale, used
// as an inclusive minimum when filtering.
func protoLevelToSlog(l pb.LogLevel) slog.Level {
	switch l {
	case pb.LogLevel_LOG_LEVEL_DEBUG:
		return slog.LevelDebug
	case pb.LogLevel_LOG_LEVEL_INFO:
		return slog.LevelInfo
	case pb.LogLevel_LOG_LEVEL_WARN:
		return slog.LevelWarn
	case pb.LogLevel_LOG_LEVEL_ERROR:
		return slog.LevelError
	case pb.LogLevel_LOG_LEVEL_UNSPECIFIED:
		fallthrough
	default:
		// "Unspecified" semantically means "no minimum"; pick a value
		// below any real slog level so all records pass.
		return slog.LevelDebug - 100
	}
}
