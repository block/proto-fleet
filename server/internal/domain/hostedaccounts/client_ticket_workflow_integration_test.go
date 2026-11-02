package hostedaccounts_test

import (
	"database/sql"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	mpb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func ticketServiceForTest(conn *sql.DB) *maintenance.Service {
	store := sqlstores.NewSQLMaintenanceStore(conn)
	return maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(conn), sqlstores.NewSQLTransactor(conn), nil)
}
func createClientIssue(t *testing.T, f fixture, actor int64, accountID, description string) *pb.AccountTicketDetail {
	t.Helper()
	result, err := f.svc.CreateAccountTicket(t.Context(), actor, f.org, &pb.CreateAccountTicketRequest{AccountId: accountID, Description: description, IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	return result
}
func TestClientTicketDescriptionOnlyAndTeammateLifecycle(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, f.host, hostedaccounts.RoleViewer))
	created := createClientIssue(t, f, f.user, a.AccountID, "All assigned equipment is missing")
	require.Equal(t, mpb.TicketCategory_TICKET_CATEGORY_ACCOUNT, created.Ticket.Category)
	require.Equal(t, "Unspecified", created.Ticket.Component)
	require.Empty(t, created.Ticket.Diagnosis)
	require.True(t, created.CanClose)
	require.Equal(t, a.AccountID, created.Ticket.CreatedByAccountId)
	require.Empty(t, created.Comments)
	listed, err := f.svc.ListAccountTickets(t.Context(), f.user, f.org, &pb.ListAccountTicketsRequest{AccountId: a.AccountID})
	require.NoError(t, err)
	require.Len(t, listed.Tickets, 1)
	require.Equal(t, created.Ticket.Id, listed.Tickets[0].Id)
	host, err := ticketServiceForTest(f.db).GetRepairTicket(t.Context(), f.org, created.Ticket.Id)
	require.NoError(t, err)
	require.Equal(t, created.Ticket.TicketNumber, host.Ticket.TicketNumber)
	_, err = f.svc.ChangeAccountTicketStatus(t.Context(), f.host, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id, Close: true, ExpectedUpdatedAt: created.Ticket.UpdatedAt})
	require.True(t, fleeterror.IsInvalidArgumentError(err), "closing requires a visible note: %v", err)
	closed, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.host, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id, Close: true, ResolutionNote: "Equipment has now appeared", ExpectedUpdatedAt: created.Ticket.UpdatedAt})
	require.NoError(t, err)
	require.Equal(t, mpb.TicketStatus_TICKET_STATUS_COMPLETED, closed.Ticket.Status)
	require.Equal(t, "Equipment has now appeared", closed.Ticket.ResolutionNote)
	reopened, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: closed.Ticket.Id, ExpectedUpdatedAt: closed.Ticket.UpdatedAt})
	require.NoError(t, err)
	require.Equal(t, created.Ticket.Id, reopened.Ticket.Id)
	require.Equal(t, mpb.TicketStatus_TICKET_STATUS_OPEN, reopened.Ticket.Status)
	require.Equal(t, created.Ticket.CreatedAt, reopened.Ticket.CreatedAt)
	require.Nil(t, reopened.Ticket.CompletedAt)
}

func TestClientTicketAccessAppliesToEveryTeammateRole(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Team-wide ticketing", f.user, hostedaccounts.AllCapabilities)
	created := createClientIssue(t, f, f.user, account.AccountID, "An issue shared with the team")
	for _, role := range []hostedaccounts.Role{hostedaccounts.RoleViewer, hostedaccounts.RolePoolOperator, hostedaccounts.RoleAdministrator} {
		t.Run(string(role), func(t *testing.T) {
			require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, account.AccountID, f.host, role))
			metadata, err := f.svc.GetAccount(t.Context(), f.host, f.org, account.AccountID)
			require.NoError(t, err)
			require.True(t, metadata.TicketAccess, "team membership grants ticketing without another permission")
			listed, err := f.svc.ListAccounts(t.Context(), f.host, f.org)
			require.NoError(t, err)
			found := false
			for _, item := range listed {
				if item.AccountID == account.AccountID {
					found = true
					require.True(t, item.TicketAccess)
				}
			}
			require.True(t, found)
			view, err := f.svc.GetAccountTicket(t.Context(), f.host, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id})
			require.NoError(t, err)
			require.True(t, view.CanClose, "any teammate can close an account-created ticket")
			_, err = f.svc.CommentAccountTicket(t.Context(), f.host, f.org, &pb.CommentAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, Text: "Following this issue", IdempotencyKey: uuid.NewString()})
			require.NoError(t, err)
		})
	}
}

func TestClientTicketBoardScopeAndPhysicalHistory(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Original", f.user, hostedaccounts.DefaultCapabilities)
	b := f.client(t, "Other", f.host, hostedaccounts.DefaultCapabilities)
	module, _ := f.miningModule(t)
	another, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	_, err := f.db.ExecContext(t.Context(), "UPDATE container_module SET model='Module X' WHERE id=$1", module)
	require.NoError(t, err)
	var board, installation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT hashboard_id,id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL", module).Scan(&board, &installation))
	request := &pb.CreateAccountTicketRequest{AccountId: a.AccountID, Description: "Board reports a hardware fault", IdempotencyKey: uuid.NewString(), Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_HASHBOARD, Id: board, ParentModuleId: &another}}}
	_, err = f.svc.CreateAccountTicket(t.Context(), f.user, f.org, request)
	require.Error(t, err, "forged parent must fail server-side")
	request.Targets[0].ParentModuleId = &module
	request.AccountId = b.AccountID
	_, err = f.svc.CreateAccountTicket(t.Context(), f.host, f.org, request)
	require.Error(t, err, "another account's physical board must be rejected")
	request.AccountId = a.AccountID
	created, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, created.Ticket.Targets, 1)
	require.Equal(t, board, created.Ticket.Targets[0].Id)
	require.Equal(t, "Module X", created.Ticket.Targets[0].Model, "container controller model must never stand in for module model")
	require.Equal(t, installation, created.Ticket.Targets[0].GetInstallationId())
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(6, module, 1)))
	_, err = f.db.ExecContext(t.Context(), "UPDATE hashboard_installation SET removed_at=clock_timestamp() WHERE id=$1", installation)
	require.NoError(t, err)
	var replacement int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id", f.org).Scan(&replacement))
	_, err = f.db.ExecContext(t.Context(), "INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,1,clock_timestamp(),clock_timestamp())", replacement, module)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_observation(hashboard_id,device_id,last_seen_at) SELECT $1,device_id,now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, replacement)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET hashboard_id=$1 WHERE module_id=$2 AND position=1`, replacement, module)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, b.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 2)))
	retained, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: a.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	require.Equal(t, board, retained.Ticket.Targets[0].Id)
	require.Equal(t, installation, retained.Ticket.Targets[0].GetInstallationId())
	_, err = f.svc.GetAccountTicket(t.Context(), f.host, f.org, &pb.GetAccountTicketRequest{AccountId: b.AccountID, TicketId: created.Ticket.Id})
	require.True(t, fleeterror.IsNotFoundError(err), "current installation/ownership must not expose prior tickets: %v", err)
	retry, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, request)
	require.NoError(t, err, "retrying an original submission uses its stored audience")
	require.Equal(t, created.Ticket.Id, retry.Ticket.Id)
}

func TestClientTicketPrivateConversationsAndLiveAccess(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Account A", f.user, hostedaccounts.DefaultCapabilities)
	b := f.client(t, "Account B", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, f.host, hostedaccounts.RoleViewer))
	service := ticketServiceForTest(f.db)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Site','ticket-site') RETURNING id", f.org).Scan(&site))
	hostTicket, _, err := service.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: f.org, IdempotencyKey: uuid.NewString(), Category: models.TicketCategoryInfrastructure, SiteID: &site, Component: "Power", ShareWithClients: true, ClientAccountIDs: []string{a.AccountID, b.AccountID}})
	require.NoError(t, err)
	_, _, err = service.CreateComment(t.Context(), f.org, hostTicket.ID, f.host, "", "Host internal diagnosis", uuid.NewString(), models.CommentScope{Internal: true})
	require.NoError(t, err)
	_, err = f.svc.CommentAccountTicket(t.Context(), f.user, f.org, &pb.CommentAccountTicketRequest{AccountId: a.AccountID, TicketId: hostTicket.ID, Text: "Account A private details", IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	_, _, err = service.CreateComment(t.Context(), f.org, hostTicket.ID, f.host, "", "Private host reply", uuid.NewString(), models.CommentScope{AccountID: a.AccountID})
	require.NoError(t, err)
	_, _, err = service.CreateComment(t.Context(), f.org, hostTicket.ID, f.host, "", "Power restored for all clients", uuid.NewString(), models.CommentScope{Internal: false})
	require.NoError(t, err)
	viewA, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: a.AccountID, TicketId: hostTicket.ID})
	require.NoError(t, err)
	viewB, err := f.svc.GetAccountTicket(t.Context(), f.host, f.org, &pb.GetAccountTicketRequest{AccountId: b.AccountID, TicketId: hostTicket.ID})
	require.NoError(t, err)
	require.Len(t, viewA.Comments, 3)
	require.Len(t, viewB.Comments, 1)
	require.Equal(t, "Power restored for all clients", viewB.Comments[0].Text)
	_, err = f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: hostTicket.ID, Close: true, ResolutionNote: "Confirmed", ExpectedUpdatedAt: viewA.Ticket.UpdatedAt})
	require.True(t, fleeterror.IsForbiddenError(err), "clients cannot close host-created tickets: %v", err)
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.user, f.org, a.AccountID, f.host))
	_, err = f.svc.GetAccountTicket(t.Context(), f.host, f.org, &pb.GetAccountTicketRequest{AccountId: a.AccountID, TicketId: hostTicket.ID})
	require.True(t, fleeterror.IsForbiddenError(err))
	disabled := false
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.DefaultCapabilities, false, &disabled))
	_, err = f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: a.AccountID, TicketId: hostTicket.ID})
	require.True(t, fleeterror.IsForbiddenError(err))
	_, err = service.GetRepairTicket(t.Context(), f.org, hostTicket.ID)
	require.NoError(t, err, "disabling client access never removes host records")
}
