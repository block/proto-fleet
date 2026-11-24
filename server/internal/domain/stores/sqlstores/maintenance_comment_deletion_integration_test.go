package sqlstores_test

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceCommentDeletionAdvancesTicketVersionAtomically(t *testing.T) {
	conn := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, conn, "comment-version")
	otherOrg := insertMaintenanceTestOrg(t, conn, "comment-other")
	author := insertMaintenanceTestUser(t, conn, org, "comment-author")
	other := insertMaintenanceTestUser(t, conn, org, "comment-other")
	store := sqlstores.NewSQLMaintenanceStore(conn)
	ticket, err := store.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: org, Category: models.TicketCategoryInfrastructure, Component: "Power", IdempotencyKey: "comment-version", CreateRequestHash: maintenanceCreateRequestHash}, "TK-0001")
	require.NoError(t, err)
	comment, err := store.CreateTicketComment(t.Context(), org, ticket.ID, author, "Visible host reply", "version-comment", maintenanceCreateRequestHash, models.CommentScope{Internal: false})
	require.NoError(t, err)
	for _, caller := range []struct{ org, user int64 }{{org, other}, {otherOrg, author}} {
		rows, err := store.SoftDeleteTicketComment(t.Context(), caller.org, caller.user, comment.ID)
		require.NoError(t, err)
		require.Zero(t, rows)
		unchanged, err := store.GetRepairTicket(t.Context(), org, ticket.ID)
		require.NoError(t, err)
		require.Equal(t, ticket.UpdatedAt, unchanged.UpdatedAt)
	}
	rollback := errors.New("abort comment deletion")
	err = sqlstores.NewSQLTransactor(conn).RunInTx(t.Context(), func(ctx context.Context) error {
		rows, err := store.SoftDeleteTicketComment(ctx, org, author, comment.ID)
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
		updated, err := store.GetRepairTicket(ctx, org, ticket.ID)
		require.NoError(t, err)
		require.True(t, updated.UpdatedAt.After(ticket.UpdatedAt))
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	unchanged, err := store.GetRepairTicket(t.Context(), org, ticket.ID)
	require.NoError(t, err)
	require.Equal(t, ticket.UpdatedAt, unchanged.UpdatedAt)
	comments, err := store.ListTicketComments(t.Context(), org, ticket.ID)
	require.NoError(t, err)
	require.Len(t, comments, 1, "rollback must restore the comment together with the ticket version")
	rows, err := store.SoftDeleteTicketComment(t.Context(), org, author, comment.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	updated, err := store.GetRepairTicket(t.Context(), org, ticket.ID)
	require.NoError(t, err)
	require.True(t, updated.UpdatedAt.After(ticket.UpdatedAt))
	comments, err = store.ListTicketComments(t.Context(), org, ticket.ID)
	require.NoError(t, err)
	require.Empty(t, comments)
	rows, err = store.SoftDeleteTicketComment(t.Context(), org, author, comment.ID)
	require.NoError(t, err)
	require.Zero(t, rows, "repeated deletion must preserve affected-row semantics")
	unchanged, err = store.GetRepairTicket(t.Context(), org, ticket.ID)
	require.NoError(t, err)
	require.Equal(t, updated.UpdatedAt, unchanged.UpdatedAt, "a deletion no-op must not invalidate comment history")
}
