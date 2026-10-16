package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/notes/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=note.go -destination=mocks/mock_note_store.go -package=mocks NoteStore

// NoteStore is the persistence boundary for the shared team notepad.
// All methods are org-scoped; cross-org reads are not supported.
type NoteStore interface {
	// CreateNote inserts a new note row and returns it. The returned
	// Note has an empty AuthorUsername — the caller stamps it from the
	// session.
	CreateNote(ctx context.Context, orgID, userID int64, content string) (*models.Note, error)

	// GetNote returns the live note or NotFound. AuthorUsername is not
	// populated.
	GetNote(ctx context.Context, orgID, id int64) (*models.Note, error)

	// ListNotes returns one feed page, newest first, with the author's
	// username joined in.
	ListNotes(ctx context.Context, params models.ListNotesParams) ([]models.Note, error)

	// UpdateNoteContent updates the live note's content iff it belongs
	// to authorUserID — the ownership predicate is in the SQL so it
	// cannot race the caller's read. Returns NotFound when no row
	// matches (missing, deleted, or different author). AuthorUsername
	// is not populated on the returned Note.
	UpdateNoteContent(ctx context.Context, orgID, id, authorUserID int64, content string) (*models.Note, error)

	// SoftDeleteNote sets deleted_at on the live note. Returns NotFound
	// when the note is missing or already deleted. Authorization
	// (author-or-moderator) is the domain layer's responsibility.
	SoftDeleteNote(ctx context.Context, orgID, id int64) error
}
