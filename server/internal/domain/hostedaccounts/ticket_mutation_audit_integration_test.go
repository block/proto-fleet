package hostedaccounts_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	activitypb "github.com/block/proto-fleet/server/generated/grpc/activity/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAccountTicketMutationsAuditOnceWithoutConversationContents(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Ticket audit account", f.user, hostedaccounts.AllCapabilities)
	b := f.client(t, "Private sibling", f.host, hostedaccounts.AllCapabilities)
	request := &pb.CreateAccountTicketRequest{AccountId: a.AccountID, Description: "Private opening description", IdempotencyKey: uuid.NewString()}
	created, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	request.IdempotencyKey = " " + request.IdempotencyKey + " "
	retry, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, created.Ticket.Id, retry.Ticket.Id)
	service := ticketServiceForTest(f.db)
	host, err := service.GetRepairTicket(t.Context(), f.org, created.Ticket.Id)
	require.NoError(t, err)
	operational := "Host operational diagnosis that must survive closure"
	updated, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: f.org, ID: created.Ticket.Id, ExpectedUpdatedAt: host.Ticket.UpdatedAt, Notes: &operational})
	require.NoError(t, err)
	commentRequest := &pb.CommentAccountTicketRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id, Text: "Private client comment", IdempotencyKey: uuid.NewString()}
	_, err = f.svc.CommentAccountTicket(t.Context(), f.user, f.org, commentRequest)
	require.NoError(t, err)
	commentRequest.IdempotencyKey = " " + commentRequest.IdempotencyKey + " "
	_, err = f.svc.CommentAccountTicket(t.Context(), f.user, f.org, commentRequest)
	require.NoError(t, err)
	closeRequest := &pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id, Close: true, ResolutionNote: "Private closure explanation", ExpectedUpdatedAt: created.Ticket.UpdatedAt}
	// Fetch the current token after the host's independent operational update.
	current, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	closeRequest.ExpectedUpdatedAt = current.Ticket.UpdatedAt
	closed, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, closeRequest)
	require.NoError(t, err)
	_, err = f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, closeRequest)
	require.Error(t, err, "stale closure retry cannot write or audit again")
	host, err = service.GetRepairTicket(t.Context(), f.org, created.Ticket.Id)
	require.NoError(t, err)
	require.Equal(t, &operational, host.Ticket.Notes)
	require.Equal(t, closeRequest.ResolutionNote, host.Ticket.ResolutionNote)
	require.Greater(t, host.Ticket.UpdatedAt.UnixNano(), updated.UpdatedAt.UnixNano())
	reopenRequest := &pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id, ExpectedUpdatedAt: closed.Ticket.UpdatedAt}
	_, err = f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, reopenRequest)
	require.NoError(t, err)
	_, err = f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, reopenRequest)
	require.Error(t, err)
	host, err = service.GetRepairTicket(t.Context(), f.org, created.Ticket.Id)
	require.NoError(t, err)
	require.Equal(t, &operational, host.Ticket.Notes)
	require.Empty(t, host.Ticket.ResolutionNote)
	var closingAccountID *int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT resolution_note_account_id FROM repair_ticket WHERE id=$1`, created.Ticket.Id).Scan(&closingAccountID))
	require.Nil(t, closingAccountID)

	var actorUUID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.user).Scan(&actorUUID))
	events := []string{"create_account_ticket", "comment_account_ticket", "close_account_ticket", "reopen_account_ticket"}
	rows, err := f.db.QueryContext(t.Context(), `SELECT event_type,event_category,user_id,actor_type,metadata::text,description FROM activity_log WHERE organization_id=$1 AND metadata->>'account_id'=$2 AND event_type=ANY($3) ORDER BY id`, f.org, a.AccountID, pq.Array(events))
	require.NoError(t, err)
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var event, category, actor, actorType, metadata, description string
		require.NoError(t, rows.Scan(&event, &category, &actor, &actorType, &metadata, &description))
		require.Equal(t, "fleet_management", category)
		require.Equal(t, "user", actorType)
		require.Equal(t, actorUUID, actor)
		var fields map[string]any
		require.NoError(t, json.Unmarshal([]byte(metadata), &fields))
		require.Equal(t, a.AccountID, fields["account_id"])
		require.EqualValues(t, created.Ticket.Id, fields["ticket_id"])
		for _, secret := range []string{request.Description, operational, commentRequest.Text, closeRequest.ResolutionNote, b.AccountID} {
			require.NotContains(t, metadata+description, secret)
		}
		got = append(got, event)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, events, got)
	require.NoError(t, rows.Close())
	page, err := f.svc.Activities(t.Context(), f.user, f.org, &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventTypes: events}})
	require.NoError(t, err)
	require.Len(t, page.Activities, 4)
	other, err := f.svc.Activities(t.Context(), f.host, f.org, &pb.ListAccountActivitiesRequest{AccountId: b.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventTypes: events}})
	require.NoError(t, err)
	require.Empty(t, other.Activities)
}

func TestAccountTicketAuditFailureRollsBackMutation(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Atomic ticket audit", f.user, hostedaccounts.AllCapabilities)
	created := createClientIssue(t, f, f.user, a.AccountID, "Existing ticket")
	failAudit := func(event string, action func() error) {
		t.Helper()
		// A scoped constraint fails only this test account's selected audit event.
		name := "ck_ticket_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, err := f.db.ExecContext(t.Context(), fmt.Sprintf(`ALTER TABLE activity_log ADD CONSTRAINT %s CHECK (NOT (organization_id=%d AND event_type='%s' AND metadata->>'account_id'='%s')) NOT VALID`, name, f.org, event, a.AccountID))
		require.NoError(t, err)
		defer func() {
			_, err := f.db.ExecContext(t.Context(), "ALTER TABLE activity_log DROP CONSTRAINT "+name)
			require.NoError(t, err)
		}()
		require.Error(t, action(), "audit failure must fail the mutation")
	}
	createKey := uuid.NewString()
	failAudit("create_account_ticket", func() error {
		_, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: a.AccountID, Description: "Must roll back", IdempotencyKey: createKey})
		return err
	})
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM repair_ticket WHERE org_id=$1 AND idempotency_key=$2`, f.org, createKey).Scan(&count))
	require.Zero(t, count)
	commentKey := uuid.NewString()
	failAudit("comment_account_ticket", func() error {
		_, err := f.svc.CommentAccountTicket(t.Context(), f.user, f.org, &pb.CommentAccountTicketRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id, Text: "Must roll back", IdempotencyKey: commentKey})
		return err
	})
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM repair_ticket_comment WHERE org_id=$1 AND idempotency_key=$2`, f.org, commentKey).Scan(&count))
	require.Zero(t, count)
	closeRequest := &pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id, Close: true, ResolutionNote: "Resolved", ExpectedUpdatedAt: created.Ticket.UpdatedAt}
	failAudit("close_account_ticket", func() error {
		_, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, closeRequest)
		return err
	})
	closed, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, closeRequest)
	require.NoError(t, err, "failed audit must preserve the open ticket and its version")
	reopenRequest := &pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id, ExpectedUpdatedAt: closed.Ticket.UpdatedAt}
	failAudit("reopen_account_ticket", func() error {
		_, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, reopenRequest)
		return err
	})
	_, err = f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, reopenRequest)
	require.NoError(t, err, "failed audit must preserve the closed ticket and its version")
}
