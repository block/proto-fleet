package sqlstores_test

import (
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notes/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func requireNotFound(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.True(t, errors.As(err, &fe), "expected FleetError, got %T", err)
	require.Equal(t, connect.CodeNotFound, fe.GRPCCode)
}

// TestNoteStore_FeedPagination walks the keyset cursor across three
// pages that include rows sharing one created_at value, proving the id
// tiebreak yields no duplicates and no skips. CURRENT_TIMESTAMP would
// give every insert a distinct time, so the test pins created_at by
// raw UPDATE after insert.
func TestNoteStore_FeedPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	alice := testContext.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLNoteStore(testContext.ServiceProvider.DB)

	older := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	pin := func(noteID int64, at time.Time) {
		_, err := testContext.ServiceProvider.DB.ExecContext(t.Context(),
			`UPDATE note SET created_at = $1 WHERE id = $2`, at, noteID)
		require.NoError(t, err)
	}

	var ids []int64
	for range 5 {
		note, err := store.CreateNote(t.Context(), alice.OrganizationID, alice.DatabaseID, "note")
		require.NoError(t, err)
		ids = append(ids, note.ID)
	}
	// ids[0..1] share the older timestamp; ids[2..4] share the newer
	// one, so both page boundaries cross an id tiebreak.
	pin(ids[0], older)
	pin(ids[1], older)
	pin(ids[2], newer)
	pin(ids[3], newer)
	pin(ids[4], newer)

	wantOrder := []int64{ids[4], ids[3], ids[2], ids[1], ids[0]}

	var walked []int64
	params := models.ListNotesParams{OrgID: alice.OrganizationID, PageSize: 2}
	for range 4 {
		rows, err := store.ListNotes(t.Context(), params)
		require.NoError(t, err)
		for _, row := range rows {
			walked = append(walked, row.ID)
			require.Equal(t, "alice@example.com", row.AuthorUsername,
				"the user join supplies the display username")
		}
		if len(rows) < 2 {
			break
		}
		last := rows[len(rows)-1]
		cursorTime := last.CreatedAt
		cursorID := last.ID
		params.CursorTime = &cursorTime
		params.CursorID = &cursorID
	}

	require.Equal(t, wantOrder, walked,
		"cursor walk must visit every live note exactly once, newest first, id-descending within equal timestamps")
}

func TestNoteStore_CrossOrgIsolationAndOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	alice := testContext.DatabaseService.CreateSuperAdminUser()
	bob := testContext.DatabaseService.CreateSuperAdminUser2()
	store := sqlstores.NewSQLNoteStore(testContext.ServiceProvider.DB)

	bobNote, err := store.CreateNote(t.Context(), bob.OrganizationID, bob.DatabaseID, "org B secret")
	require.NoError(t, err)

	// Org A sees an empty feed and cannot reach org B's note by id
	// through any verb.
	rows, err := store.ListNotes(t.Context(), models.ListNotesParams{OrgID: alice.OrganizationID, PageSize: 10})
	require.NoError(t, err)
	require.Empty(t, rows, "org A's feed must not contain org B's notes")

	_, err = store.GetNote(t.Context(), alice.OrganizationID, bobNote.ID)
	requireNotFound(t, err)
	_, err = store.UpdateNoteContent(t.Context(), alice.OrganizationID, bobNote.ID, alice.DatabaseID, "hijack")
	requireNotFound(t, err)
	requireNotFound(t, store.SoftDeleteNote(t.Context(), alice.OrganizationID, bobNote.ID))

	// Ownership predicate: a different user id in the author's own org
	// cannot update the row.
	aliceNote, err := store.CreateNote(t.Context(), alice.OrganizationID, alice.DatabaseID, "mine")
	require.NoError(t, err)
	_, err = store.UpdateNoteContent(t.Context(), alice.OrganizationID, aliceNote.ID, alice.DatabaseID+9999, "forged")
	requireNotFound(t, err)

	// The legitimate author updates fine and the trigger bumps
	// updated_at past created_at (the wire-level "edited" signal).
	updated, err := store.UpdateNoteContent(t.Context(), alice.OrganizationID, aliceNote.ID, alice.DatabaseID, "mine, edited")
	require.NoError(t, err)
	require.Equal(t, "mine, edited", updated.Content)
	require.True(t, updated.UpdatedAt.After(updated.CreatedAt),
		"updated_at trigger must advance past created_at on edit")
}

func TestNoteStore_SoftDeleteExcludesFromReads(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	alice := testContext.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLNoteStore(testContext.ServiceProvider.DB)

	note, err := store.CreateNote(t.Context(), alice.OrganizationID, alice.DatabaseID, "ephemeral")
	require.NoError(t, err)

	require.NoError(t, store.SoftDeleteNote(t.Context(), alice.OrganizationID, note.ID))

	_, err = store.GetNote(t.Context(), alice.OrganizationID, note.ID)
	requireNotFound(t, err)

	rows, err := store.ListNotes(t.Context(), models.ListNotesParams{OrgID: alice.OrganizationID, PageSize: 10})
	require.NoError(t, err)
	require.Empty(t, rows)

	// Deleting an already-deleted note reports NotFound rather than
	// silently succeeding.
	requireNotFound(t, store.SoftDeleteNote(t.Context(), alice.OrganizationID, note.ID))
}
