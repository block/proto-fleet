package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notes/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.NoteStore = &SQLNoteStore{}

type SQLNoteStore struct {
	SQLConnectionManager
}

func NewSQLNoteStore(conn *sql.DB) *SQLNoteStore {
	return &SQLNoteStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLNoteStore) CreateNote(ctx context.Context, orgID, userID int64, content string) (*models.Note, error) {
	row, err := s.GetQueries(ctx).CreateNote(ctx, sqlc.CreateNoteParams{
		OrgID:   orgID,
		UserID:  userID,
		Content: content,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create note: %v", err)
	}
	out := noteFromRow(row)
	return &out, nil
}

func (s *SQLNoteStore) GetNote(ctx context.Context, orgID, id int64) (*models.Note, error) {
	row, err := s.GetQueries(ctx).GetNote(ctx, sqlc.GetNoteParams{ID: id, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("note %d not found", id)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get note: %v", err)
	}
	out := noteFromRow(row)
	return &out, nil
}

func (s *SQLNoteStore) ListNotes(ctx context.Context, params models.ListNotesParams) ([]models.Note, error) {
	arg := sqlc.ListNotesParams{
		OrgID:    params.OrgID,
		PageSize: params.PageSize,
	}
	if params.CursorTime != nil {
		arg.CursorTime = sql.NullTime{Time: *params.CursorTime, Valid: true}
	}
	if params.CursorID != nil {
		arg.CursorID = sql.NullInt64{Int64: *params.CursorID, Valid: true}
	}
	rows, err := s.GetQueries(ctx).ListNotes(ctx, arg)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list notes: %v", err)
	}
	out := make([]models.Note, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.Note{
			ID:             row.ID,
			OrgID:          row.OrgID,
			UserID:         row.UserID,
			AuthorUsername: row.AuthorUsername,
			Content:        row.Content,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		})
	}
	return out, nil
}

func (s *SQLNoteStore) UpdateNoteContent(ctx context.Context, orgID, id, authorUserID int64, content string) (*models.Note, error) {
	row, err := s.GetQueries(ctx).UpdateNoteContent(ctx, sqlc.UpdateNoteContentParams{
		Content: content,
		ID:      id,
		OrgID:   orgID,
		UserID:  authorUserID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("note %d not found", id)
		}
		return nil, fleeterror.NewInternalErrorf("failed to update note: %v", err)
	}
	out := noteFromRow(row)
	return &out, nil
}

func (s *SQLNoteStore) SoftDeleteNote(ctx context.Context, orgID, id int64) error {
	affected, err := s.GetQueries(ctx).SoftDeleteNote(ctx, sqlc.SoftDeleteNoteParams{ID: id, OrgID: orgID})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to delete note: %v", err)
	}
	if affected == 0 {
		return fleeterror.NewNotFoundErrorf("note %d not found", id)
	}
	return nil
}

// noteFromRow maps the bare note row (no username join) used by the
// create/get/update paths.
func noteFromRow(row sqlc.Note) models.Note {
	return models.Note{
		ID:        row.ID,
		OrgID:     row.OrgID,
		UserID:    row.UserID,
		Content:   row.Content,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
