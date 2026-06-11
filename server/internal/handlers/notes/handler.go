// Package notes is the Connect-RPC surface for NoteService, the
// org-wide shared team notepad. Translation between proto and domain
// types lives in translate.go; this file is the wiring + auth gates.
//
// All gates use the *Anywhere middleware variants: the notepad is an
// org-shared collaborative resource with no site dimension, so a role
// assignment at any scope (org or site) makes the caller a member of
// the team the feed serves.
package notes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/notes/v1"
	"github.com/block/proto-fleet/server/generated/grpc/notes/v1/notesv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notes"
	"github.com/block/proto-fleet/server/internal/domain/notes/models"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the NoteService Connect-RPC surface.
type Handler struct {
	service *notes.Service
}

var _ notesv1connect.NoteServiceHandler = &Handler{}

// NewHandler returns a NoteService handler bound to the supplied
// domain service.
func NewHandler(service *notes.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListNotes(ctx context.Context, req *connect.Request[pb.ListNotesRequest]) (*connect.Response[pb.ListNotesResponse], error) {
	info, err := middleware.RequirePermissionAnywhere(ctx, authz.PermNoteRead)
	if err != nil {
		return nil, err
	}

	// Clamp here, not just in the service: the has-more boundary below
	// must compare against the page size the query actually used, or a
	// defaulted request would never emit a continuation token.
	pageSize := models.ClampPageSize(req.Msg.GetPageSize())
	params := models.ListNotesParams{
		OrgID:    info.OrganizationID,
		PageSize: pageSize,
	}
	if token := req.Msg.GetPageToken(); token != "" {
		createdAt, id, err := decodeCursor(token)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
		params.CursorTime = &createdAt
		params.CursorID = &id
	}

	rows, err := h.service.ListNotes(ctx, params)
	if err != nil {
		return nil, err
	}

	var nextPageToken string
	if len(rows) == int(pageSize) {
		last := rows[len(rows)-1]
		nextPageToken, err = encodeCursor(last.CreatedAt, last.ID)
		if err != nil {
			return nil, fleeterror.NewInternalError(err.Error())
		}
	}

	return connect.NewResponse(toListNotesResponse(rows, nextPageToken)), nil
}

func (h *Handler) CreateNote(ctx context.Context, req *connect.Request[pb.CreateNoteRequest]) (*connect.Response[pb.CreateNoteResponse], error) {
	info, err := middleware.RequirePermissionAnywhere(ctx, authz.PermNoteCreate)
	if err != nil {
		return nil, err
	}

	note, err := h.service.CreateNote(ctx, info.OrganizationID, info.UserID, info.Username, req.Msg.GetContent())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateNoteResponse{Note: toProtoNote(note)}), nil
}

func (h *Handler) UpdateNote(ctx context.Context, req *connect.Request[pb.UpdateNoteRequest]) (*connect.Response[pb.UpdateNoteResponse], error) {
	info, err := middleware.RequirePermissionAnywhere(ctx, authz.PermNoteCreate)
	if err != nil {
		return nil, err
	}

	note, err := h.service.UpdateNote(ctx, info.OrganizationID, req.Msg.GetId(), info.UserID, info.Username, req.Msg.GetContent())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateNoteResponse{Note: toProtoNote(note)}), nil
}

func (h *Handler) DeleteNote(ctx context.Context, req *connect.Request[pb.DeleteNoteRequest]) (*connect.Response[pb.DeleteNoteResponse], error) {
	// note:create lets an author delete their own note; note:manage
	// alone also passes so a moderator-only role works. The domain
	// layer enforces the author-or-moderator rule.
	info, err := middleware.RequireAnyPermissionAnywhere(ctx, []string{authz.PermNoteCreate, authz.PermNoteManage})
	if err != nil {
		return nil, err
	}
	canModerate := middleware.CallerHasPermissionAnywhere(ctx, authz.PermNoteManage)

	if err := h.service.DeleteNote(ctx, info.OrganizationID, req.Msg.GetId(), info.UserID, canModerate); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteNoteResponse{}), nil
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
