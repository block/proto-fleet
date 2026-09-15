package maintenance_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	handler "github.com/block/proto-fleet/server/internal/handlers/maintenance"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestMaintenanceAPIEnforcesReadOnlyAndOrganizationIsolation(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	suffix := time.Now().UnixNano()
	insertOrg := func(name string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO organization (org_id, name) VALUES ($1, $2) RETURNING id
		`, fmt.Sprintf("mh-%s-%d", name, suffix), "Maintenance handler "+name).Scan(&id))
		return id
	}
	ownerOrgID := insertOrg("owner")
	otherOrgID := insertOrg("other")

	var ticketID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO repair_ticket (org_id, ticket_number, category, component)
		VALUES ($1, 'TK-0001', 2, 'Transformer')
		RETURNING id
	`, ownerOrgID).Scan(&ticketID))

	store := sqlstores.NewSQLMaintenanceStore(db)
	service := domain.NewService(store, store, nil, nil, nil)
	h := handler.NewHandler(service)

	readOnlyCtx := maintenanceContext(t, ownerOrgID, authz.PermMaintenanceRead)
	_, err := h.UpdateRepairTicket(readOnlyCtx, connect.NewRequest(&pb.UpdateRepairTicketRequest{Id: ticketID}))
	require.True(t, fleeterror.IsForbiddenError(err), "read-only callers must not mutate: %v", err)

	otherOrgCtx := maintenanceContext(t, otherOrgID, authz.PermMaintenanceRead)
	_, err = h.GetRepairTicket(otherOrgCtx, connect.NewRequest(&pb.GetRepairTicketRequest{Id: ticketID}))
	require.True(t, fleeterror.IsNotFoundError(err), "cross-org ticket IDs must be hidden: %v", err)
}

func TestMaintenanceAPIExcludesTicketsAtNarrowedSites(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	suffix := time.Now().UnixNano()
	var orgID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name) VALUES ($1, $2) RETURNING id
	`, fmt.Sprintf("mh-scope-%d", suffix), "Maintenance scope").Scan(&orgID))
	insertSite := func(name string) int64 {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO site (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id
		`, orgID, name, fmt.Sprintf("%s-%d", name, suffix)).Scan(&id))
		return id
	}
	allowedSiteID := insertSite("allowed")
	deniedSiteID := insertSite("denied")
	for number, siteID := range []*int64{&allowedSiteID, &deniedSiteID, nil} {
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO repair_ticket (org_id, ticket_number, category, component, site_id)
			VALUES ($1, $2, 2, 'Transformer', $3) RETURNING id
		`, orgID, fmt.Sprintf("TK-%04d", number+1), siteID).Scan(new(int64)))
	}

	store := sqlstores.NewSQLMaintenanceStore(db)
	h := handler.NewHandler(domain.NewService(store, store, nil, nil, nil))
	scopedCtx := maintenanceContextWithAssignments(t, orgID,
		authz.Assignment{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermMaintenanceRead}},
		authz.Assignment{AssignmentID: 2, ScopeType: authz.ScopeSite, SiteID: &deniedSiteID},
	)

	response, err := h.ListRepairTickets(scopedCtx, connect.NewRequest(&pb.ListRepairTicketsRequest{PageSize: 10}))
	require.NoError(t, err)
	require.Len(t, response.Msg.GetTickets(), 2)
	require.Equal(t, int32(2), response.Msg.GetTotalCount())
	for _, ticket := range response.Msg.GetTickets() {
		require.NotEqual(t, deniedSiteID, ticket.GetTicket().GetSiteId())
	}

	var deniedTicketID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT id FROM repair_ticket WHERE org_id = $1 AND site_id = $2
	`, orgID, deniedSiteID).Scan(&deniedTicketID))
	_, err = h.GetRepairTicket(scopedCtx, connect.NewRequest(&pb.GetRepairTicketRequest{Id: deniedTicketID}))
	require.True(t, fleeterror.IsNotFoundError(err), "denied site tickets must be masked: %v", err)

	for number, siteID := range []*int64{&allowedSiteID, &deniedSiteID, nil} {
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO repair_ticket (org_id, ticket_number, category, component, site_id, status, completed_at)
			VALUES ($1, $2, 2, 'Transformer', $3, 5, NOW()) RETURNING id
		`, orgID, fmt.Sprintf("TK-%04d", number+100), siteID).Scan(new(int64)))
	}
	siteOnlyCtx := maintenanceContextWithAssignments(t, orgID,
		authz.Assignment{AssignmentID: 3, ScopeType: authz.ScopeSite, SiteID: &allowedSiteID, Permissions: []string{authz.PermMaintenanceRead}},
	)

	listResponse, err := h.ListRepairTickets(siteOnlyCtx, connect.NewRequest(&pb.ListRepairTicketsRequest{PageSize: 10}))
	require.NoError(t, err)
	require.Len(t, listResponse.Msg.GetTickets(), 2)
	for _, ticket := range listResponse.Msg.GetTickets() {
		require.Equal(t, allowedSiteID, ticket.GetTicket().GetSiteId())
	}
	statsResponse, err := h.GetTicketStats(siteOnlyCtx, connect.NewRequest(&pb.GetTicketStatsRequest{}))
	require.NoError(t, err)
	require.Equal(t, int32(1), statsResponse.Msg.GetOpenCount())
	historyResponse, err := h.ListCompletedTickets(siteOnlyCtx, connect.NewRequest(&pb.ListCompletedTicketsRequest{PageSize: 10}))
	require.NoError(t, err)
	require.Len(t, historyResponse.Msg.GetTickets(), 1)
	require.Equal(t, allowedSiteID, historyResponse.Msg.GetTickets()[0].GetTicket().GetSiteId())
}

func maintenanceContextWithAssignments(t *testing.T, orgID int64, assignments ...authz.Assignment) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod: session.AuthMethodSession, SessionID: fmt.Sprintf("maintenance-handler-%d", orgID),
		UserID: orgID, OrganizationID: orgID, ExternalUserID: fmt.Sprintf("maintenance-handler-user-%d", orgID),
		Username: fmt.Sprintf("maintenance-handler-user-%d", orgID),
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions(assignments))
}

func maintenanceContext(t *testing.T, orgID int64, permissions ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		SessionID:      fmt.Sprintf("maintenance-handler-%d", orgID),
		UserID:         orgID,
		OrganizationID: orgID,
		ExternalUserID: fmt.Sprintf("maintenance-handler-user-%d", orgID),
		Username:       fmt.Sprintf("maintenance-handler-user-%d", orgID),
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: orgID,
		ScopeType:    authz.ScopeOrg,
		Permissions:  permissions,
	}}))
}
