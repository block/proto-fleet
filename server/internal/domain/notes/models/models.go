// Package models holds the domain shapes for the shared team notepad:
// one org-wide feed of notes every member can read and post to.
package models

import "time"

const (
	// DefaultPageSize is applied when a list request carries no page
	// size (e.g. an internal caller passing the zero value).
	DefaultPageSize = 25

	// MaxPageSize caps a single ListNotes page. Mirrors the activity
	// log's wire-level lte:100 validation.
	MaxPageSize = 100

	// MaxContentRunes caps note content after trimming. The proto
	// annotation enforces the same number of codepoints pre-trim; the
	// domain recheck is authoritative for the stored value.
	MaxContentRunes = 4096
)

// ClampPageSize normalizes a requested page size to [1, MaxPageSize];
// the zero value selects DefaultPageSize. The handler (computing the
// has-more boundary) and the service (defending internal callers)
// apply the same rule so they cannot disagree about what counts as a
// full page.
func ClampPageSize(size int32) int32 {
	if size <= 0 {
		return DefaultPageSize
	}
	if size > MaxPageSize {
		return MaxPageSize
	}
	return size
}

// Note is one entry in the org's shared notepad. AuthorUsername is a
// read-time projection from the "user" table (or stamped from the
// session on create/update); it is display attribution only — the
// author-only edit/delete rule keys on UserID.
type Note struct {
	ID             int64
	OrgID          int64
	UserID         int64
	AuthorUsername string
	Content        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ListNotesParams is the domain-level input for one feed page. A nil
// cursor pair means "first page"; both cursor fields are set together
// from the previous page's last row.
type ListNotesParams struct {
	OrgID      int64
	PageSize   int32
	CursorTime *time.Time
	CursorID   *int64
}
