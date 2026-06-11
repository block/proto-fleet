// Package notes implements the shared team notepad: one org-wide,
// append-style feed of notes every member of the org can read and
// post to. Authors edit and delete their own notes; moderation
// (deleting another author's note) is a separate capability the
// handler resolves and passes in.
package notes

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notes/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event types recorded on the org activity log for notepad mutations.
const (
	eventNoteCreated = "note.created"
	eventNoteUpdated = "note.updated"
	eventNoteDeleted = "note.deleted"
)

// Service owns the notepad's business rules: content normalization,
// the author-only edit rule, and the author-or-moderator delete rule.
// Tenancy (org scoping) is enforced by the store's SQL predicates.
type Service struct {
	store       interfaces.NoteStore
	activitySvc *activity.Service
}

// NewService wires the notepad domain service. activitySvc may be nil
// (Log is nil-safe), which test harnesses use to skip audit writes.
func NewService(store interfaces.NoteStore, activitySvc *activity.Service) *Service {
	return &Service{store: store, activitySvc: activitySvc}
}

// ListNotes returns one feed page, newest first. PageSize is clamped
// to [1, MaxPageSize]; the zero value selects DefaultPageSize so
// internal callers can pass a zero-valued params struct safely.
func (s *Service) ListNotes(ctx context.Context, params models.ListNotesParams) ([]models.Note, error) {
	if params.PageSize <= 0 {
		params.PageSize = models.DefaultPageSize
	}
	if params.PageSize > models.MaxPageSize {
		params.PageSize = models.MaxPageSize
	}
	return s.store.ListNotes(ctx, params)
}

// CreateNote appends a note authored by the caller and returns it
// with the author's username stamped for immediate display.
func (s *Service) CreateNote(ctx context.Context, orgID, authorUserID int64, authorUsername, content string) (*models.Note, error) {
	content, err := normalizeContent(content)
	if err != nil {
		return nil, err
	}

	note, err := s.store.CreateNote(ctx, orgID, authorUserID, content)
	if err != nil {
		return nil, err
	}
	note.AuthorUsername = authorUsername

	s.logEvent(ctx, orgID, eventNoteCreated,
		fmt.Sprintf("Added a team note (id=%d)", note.ID),
		map[string]any{
			"note_id":        note.ID,
			"content_length": utf8.RuneCountInString(content),
		})
	return note, nil
}

// UpdateNote replaces the content of the caller's own note. Editing
// another author's note is Forbidden regardless of role — moderation
// covers deletion only, so a note's content always reflects its
// author's words.
func (s *Service) UpdateNote(ctx context.Context, orgID, noteID, callerUserID int64, callerUsername, content string) (*models.Note, error) {
	content, err := normalizeContent(content)
	if err != nil {
		return nil, err
	}

	existing, err := s.store.GetNote(ctx, orgID, noteID)
	if err != nil {
		return nil, err
	}
	if existing.UserID != callerUserID {
		return nil, fleeterror.NewForbiddenError("only the author can edit a note")
	}

	// The store repeats the ownership predicate in the UPDATE's WHERE,
	// so a concurrent delete/re-author cannot slip between the read
	// above and this write.
	note, err := s.store.UpdateNoteContent(ctx, orgID, noteID, callerUserID, content)
	if err != nil {
		return nil, err
	}
	note.AuthorUsername = callerUsername

	s.logEvent(ctx, orgID, eventNoteUpdated,
		fmt.Sprintf("Edited a team note (id=%d)", note.ID),
		map[string]any{
			"note_id":        note.ID,
			"content_length": utf8.RuneCountInString(content),
		})
	return note, nil
}

// DeleteNote soft-deletes a note. The caller must be the note's
// author, or canModerateAnyNote must be true (the handler resolves it
// from the note:manage permission).
func (s *Service) DeleteNote(ctx context.Context, orgID, noteID, callerUserID int64, canModerateAnyNote bool) error {
	existing, err := s.store.GetNote(ctx, orgID, noteID)
	if err != nil {
		return err
	}
	moderated := existing.UserID != callerUserID
	if moderated && !canModerateAnyNote {
		return fleeterror.NewForbiddenError("only the author or a notepad moderator can delete a note")
	}

	if err := s.store.SoftDeleteNote(ctx, orgID, noteID); err != nil {
		return err
	}

	s.logEvent(ctx, orgID, eventNoteDeleted,
		fmt.Sprintf("Deleted a team note (id=%d)", noteID),
		map[string]any{
			"note_id":        noteID,
			"author_user_id": existing.UserID,
			"moderated":      moderated,
		})
	return nil
}

// normalizeContent trims surrounding whitespace and validates the
// result. The proto annotation bounds the pre-trim length on the
// wire; this check is authoritative for the stored value.
func normalizeContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fleeterror.NewInvalidArgumentError("note content cannot be empty")
	}
	if utf8.RuneCountInString(content) > models.MaxContentRunes {
		return "", fleeterror.NewInvalidArgumentErrorf("note content exceeds %d characters", models.MaxContentRunes)
	}
	return content, nil
}

// logEvent records an org-scoped notepad event on the activity log.
// Note content never enters the audit row — only its length — so the
// activity feed can't become a side channel around a future
// note deletion.
func (s *Service) logEvent(ctx context.Context, orgID int64, eventType, description string, metadata map[string]any) {
	event := activitymodels.Event{
		Category:       activitymodels.CategoryNote,
		Type:           eventType,
		OrganizationID: &orgID,
		Description:    description,
		Metadata:       metadata,
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)
}
