package activity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/activity/v1"
	"github.com/block/proto-fleet/server/generated/grpc/activity/v1/activityv1connect"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

var _ activityv1connect.ActivityServiceHandler = &Handler{}

type Handler struct {
	activitySvc *activity.Service
}

func NewHandler(activitySvc *activity.Service) *Handler {
	return &Handler{activitySvc: activitySvc}
}

func (h *Handler) ListActivities(
	ctx context.Context,
	req *connect.Request[pb.ListActivitiesRequest],
) (*connect.Response[pb.ListActivitiesResponse], error) {
	info, err := getSessionInfo(ctx)
	if err != nil {
		return nil, err
	}

	filter, err := toFilter(info.OrganizationID, req.Msg)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError(err.Error())
	}

	entries, err := h.activitySvc.List(ctx, filter)
	if err != nil {
		return nil, serviceError(ctx, err)
	}

	var totalCount int32
	if req.Msg.GetPageToken() == "" {
		count, err := h.activitySvc.Count(ctx, filter)
		if err != nil {
			return nil, serviceError(ctx, err)
		}
		totalCount = int32(count) // #nosec G115 -- total_count is display-only; truncation at 2B is acceptable
	}

	activities := make([]*pb.ActivityEntry, len(entries))
	for i, e := range entries {
		activities[i] = entryToProto(e)
	}

	var nextPageToken string
	if len(entries) == filter.PageSize {
		last := entries[len(entries)-1]
		nextPageToken, err = encodeCursor(last.CreatedAt, last.ID)
		if err != nil {
			return nil, fleeterror.NewInternalError(err.Error())
		}
	}

	return connect.NewResponse(&pb.ListActivitiesResponse{
		Activities:    activities,
		NextPageToken: nextPageToken,
		TotalCount:    totalCount,
	}), nil
}

func (h *Handler) ExportActivities(
	ctx context.Context,
	req *connect.Request[pb.ExportActivitiesRequest],
	stream *connect.ServerStream[pb.ExportActivitiesResponse],
) error {
	info, err := getSessionInfo(ctx)
	if err != nil {
		return err
	}

	filter := filterFromProto(info.OrganizationID, req.Msg.GetFilter())
	filter.PageSize = models.MaxPageSize

	var headerBuf strings.Builder
	hw := csv.NewWriter(&headerBuf)
	_ = hw.Write([]string{"Timestamp", "Type", "Category", "Description", "Result", "Scope", "User"})
	hw.Flush()
	if err := stream.Send(&pb.ExportActivitiesResponse{Chunk: []byte(headerBuf.String())}); err != nil {
		if ctx.Err() != nil {
			return contextError(ctx)
		}
		return fleeterror.NewInternalErrorf("sending CSV header: %v", err)
	}

	for {
		if ctx.Err() != nil {
			return contextError(ctx)
		}
		entries, err := h.activitySvc.List(ctx, filter)
		if err != nil {
			return serviceError(ctx, err)
		}
		if len(entries) == 0 {
			break
		}

		var buf bytes.Buffer
		for _, e := range entries {
			buf.WriteString(formatCSVRow(e))
		}
		if err := stream.Send(&pb.ExportActivitiesResponse{Chunk: buf.Bytes()}); err != nil {
			if ctx.Err() != nil {
				return contextError(ctx)
			}
			return fleeterror.NewInternalErrorf("sending CSV chunk: %v", err)
		}

		if len(entries) < filter.PageSize {
			break
		}
		last := entries[len(entries)-1]
		filter.CursorTime = &last.CreatedAt
		filter.CursorID = &last.ID
	}

	return nil
}

func (h *Handler) ListActivityFilterOptions(
	ctx context.Context,
	_ *connect.Request[pb.ListActivityFilterOptionsRequest],
) (*connect.Response[pb.ListActivityFilterOptionsResponse], error) {
	info, err := getSessionInfo(ctx)
	if err != nil {
		return nil, err
	}

	opts, err := h.activitySvc.GetFilterOptions(ctx, info.OrganizationID)
	if err != nil {
		return nil, serviceError(ctx, err)
	}

	eventTypes := make([]*pb.EventTypeOption, len(opts.EventTypes))
	for i, et := range opts.EventTypes {
		eventTypes[i] = &pb.EventTypeOption{
			EventType:     et.EventType,
			EventCategory: et.EventCategory,
		}
	}

	users := make([]*pb.UserOption, len(opts.Users))
	for i, u := range opts.Users {
		users[i] = &pb.UserOption{
			UserId:   u.UserID,
			Username: u.Username,
		}
	}

	return connect.NewResponse(&pb.ListActivityFilterOptionsResponse{
		EventTypes: eventTypes,
		ScopeTypes: opts.ScopeTypes,
		Users:      users,
	}), nil
}

// --- error helpers ---

func serviceError(ctx context.Context, err error) fleeterror.FleetError {
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	return fleeterror.NewInternalError(err.Error())
}

func contextError(ctx context.Context) fleeterror.FleetError {
	if ctx.Err() == context.DeadlineExceeded {
		return fleeterror.NewPlainError("deadline exceeded", connect.CodeDeadlineExceeded)
	}
	return fleeterror.NewCanceledError()
}

// --- session helper ---

func getSessionInfo(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	return info, nil
}

// --- filter mapping ---

func toFilter(orgID int64, req *pb.ListActivitiesRequest) (models.Filter, error) {
	f := filterFromProto(orgID, req.GetFilter())

	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = models.DefaultPageSize
	}
	if pageSize > models.MaxPageSize {
		pageSize = models.MaxPageSize
	}
	f.PageSize = pageSize

	if token := req.GetPageToken(); token != "" {
		cursorTime, cursorID, err := decodeCursor(token)
		if err != nil {
			return models.Filter{}, err
		}
		f.CursorTime = &cursorTime
		f.CursorID = &cursorID
	}

	return f, nil
}

func filterFromProto(orgID int64, pf *pb.ActivityFilter) models.Filter {
	f := models.Filter{
		OrganizationID: orgID,
	}
	if pf == nil {
		return f
	}
	f.EventCategories = pf.GetEventCategories()
	f.EventTypes = pf.GetEventTypes()
	f.UserIDs = pf.GetUserIds()
	f.ScopeTypes = pf.GetScopeTypes()
	f.SearchText = pf.GetSearchText()
	if pf.GetStartTime() != nil {
		t := pf.GetStartTime().AsTime()
		f.StartTime = &t
	}
	if pf.GetEndTime() != nil {
		t := pf.GetEndTime().AsTime()
		f.EndTime = &t
	}
	return f
}

// --- cursor encoding ---

type pageCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

func encodeCursor(createdAt time.Time, id int64) (string, error) {
	data, err := json.Marshal(pageCursor{CreatedAt: createdAt, ID: id})
	if err != nil {
		return "", fmt.Errorf("encoding page cursor: %w", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

func decodeCursor(token string) (time.Time, int64, error) {
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid page token encoding: %w", err)
	}
	var c pageCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid page token format: %w", err)
	}
	if c.CreatedAt.IsZero() {
		return time.Time{}, 0, fmt.Errorf("invalid page token: missing created_at")
	}
	if c.ID <= 0 {
		return time.Time{}, 0, fmt.Errorf("invalid page token: missing id")
	}
	return c.CreatedAt, c.ID, nil
}

// --- proto conversion ---

func entryToProto(e models.Entry) *pb.ActivityEntry {
	entry := &pb.ActivityEntry{
		EventId:       e.EventID,
		EventCategory: e.Category,
		EventType:     e.Type,
		Description:   e.Description,
		ActorType:     e.ActorType,
		CreatedAt:     timestamppb.New(e.CreatedAt),
		Result:        e.Result,
		ScopeType:     e.ScopeType,
		ScopeLabel:    e.ScopeLabel,
		UserId:        e.UserID,
		Username:      e.Username,
		ErrorMessage:  e.ErrorMessage,
	}

	if e.ScopeCount != nil {
		entry.ScopeCount = int32(*e.ScopeCount) // #nosec G115 -- scope_count is a small device count
	}

	if len(e.Metadata) > 0 {
		var raw map[string]any
		if json.Unmarshal(e.Metadata, &raw) == nil {
			if s, err := structpb.NewStruct(raw); err == nil {
				entry.Metadata = s
			}
		}
	}

	return entry
}

// --- CSV formatting ---

func formatCSVRow(e models.Entry) string {
	ts := formatActivityTimestamp(e.CreatedAt)
	scope := formatScope(e.ScopeType, e.ScopeLabel, e.ScopeCount)
	user := "\u2014"
	if e.Username != nil {
		user = *e.Username
	} else if e.UserID != nil {
		user = *e.UserID
	}
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{
		ts,
		sanitizeCSVField(e.Type),
		sanitizeCSVField(e.Category),
		sanitizeCSVField(e.Description),
		sanitizeCSVField(e.Result),
		sanitizeCSVField(scope),
		sanitizeCSVField(user),
	})
	w.Flush()
	return buf.String()
}

func formatActivityTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatScope(scopeType, scopeLabel *string, scopeCount *int) string {
	hasLabel := scopeLabel != nil && *scopeLabel != ""
	hasCount := scopeCount != nil && *scopeCount > 0

	if !hasLabel && !hasCount {
		return "\u2014"
	}

	unit := "miners"
	if hasCount && *scopeCount == 1 {
		unit = "miner"
	}

	if hasLabel && hasCount {
		return fmt.Sprintf("%s (%d %s)", *scopeLabel, *scopeCount, unit)
	}
	if hasLabel {
		return *scopeLabel
	}
	return fmt.Sprintf("%d %s", *scopeCount, unit)
}

func sanitizeCSVField(value string) string {
	if len(value) == 0 {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}
