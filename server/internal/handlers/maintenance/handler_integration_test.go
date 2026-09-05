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
