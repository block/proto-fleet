package serverlog

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/serverlog/v1"
	"github.com/block/proto-fleet/server/generated/grpc/serverlog/v1/serverlogv1connect"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/logging"
)

const DefaultLimit = 500

type Handler struct {
	buffer *logging.Buffer
}

var _ serverlogv1connect.ServerLogServiceHandler = &Handler{}

func NewHandler(buffer *logging.Buffer) *Handler {
	return &Handler{buffer: buffer}
}

func (h *Handler) ListServerLogs(
	ctx context.Context,
	req *connect.Request[pb.ListServerLogsRequest],
) (*connect.Response[pb.ListServerLogsResponse], error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	if err := requireAdmin(info); err != nil {
		return nil, err
	}

	if h.buffer == nil {
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
		Entries:    entries,
		LatestId:   snap.LatestID,
		BufferSize: int32(snap.Size),
		Truncated:  snap.Truncated,
	}), nil
}

func requireAdmin(info *session.Info) error {
	if info.Role != domainAuth.SuperAdminRoleName {
		return fleeterror.NewForbiddenError("only super admins can view server logs")
	}
	return nil
}

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
		return slog.LevelDebug - 100
	}
}
