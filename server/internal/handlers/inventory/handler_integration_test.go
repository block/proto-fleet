package inventory_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	inventoryv1 "github.com/block/proto-fleet/server/generated/grpc/inventory/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/inventory"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	handler "github.com/block/proto-fleet/server/internal/handlers/inventory"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestInventoryAPIExcludesPartsAtNarrowedSites(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	suffix := time.Now().UnixNano()
	var orgID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name) VALUES ($1, $2) RETURNING id
	`, fmt.Sprintf("ih-%d", suffix), "Inventory handler").Scan(&orgID))
	insertSite := func(name string) int64 {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO site (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id
		`, orgID, name, fmt.Sprintf("%s-%d", name, suffix)).Scan(&id))
		return id
	}
	allowedSiteID := insertSite("allowed")
	deniedSiteID := insertSite("denied")
	partIDs := make([]int64, 0, 3)
	for index, siteID := range []*int64{&allowedSiteID, &deniedSiteID, nil} {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO inventory_part (org_id, name, type, site_id)
			VALUES ($1, $2, 'fan', $3) RETURNING id
		`, orgID, fmt.Sprintf("Part %d", index), siteID).Scan(&id))
		partIDs = append(partIDs, id)
	}

	store := sqlstores.NewSQLInventoryStore(db)
	h := handler.NewHandler(inventory.NewService(store, nil, nil))
	scopedCtx := inventoryContext(t, orgID,
		authz.Assignment{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermMaintenanceRead}},
		authz.Assignment{AssignmentID: 2, ScopeType: authz.ScopeSite, SiteID: &deniedSiteID},
	)

	response, err := h.ListInventoryParts(scopedCtx, connect.NewRequest(&inventoryv1.ListInventoryPartsRequest{PageSize: 10}))
	require.NoError(t, err)
	require.Len(t, response.Msg.GetParts(), 2)
	require.Equal(t, int32(2), response.Msg.GetTotalCount())
	for _, part := range response.Msg.GetParts() {
		require.NotEqual(t, deniedSiteID, part.GetSiteId())
	}

	_, err = h.GetInventoryPart(scopedCtx, connect.NewRequest(&inventoryv1.GetInventoryPartRequest{Id: partIDs[1]}))
	require.True(t, fleeterror.IsNotFoundError(err), "denied site parts must be masked: %v", err)

	_, err = h.GetInventoryInsights(scopedCtx, connect.NewRequest(&inventoryv1.GetInventoryInsightsRequest{}))
	require.True(t, fleeterror.IsForbiddenError(err), "org totals must not leak a denied site's inventory: %v", err)

	siteOnlyCtx := inventoryContext(t, orgID,
		authz.Assignment{AssignmentID: 3, ScopeType: authz.ScopeSite, SiteID: &allowedSiteID, Permissions: []string{authz.PermMaintenanceRead}},
	)
	siteOnlyResponse, err := h.ListInventoryParts(siteOnlyCtx, connect.NewRequest(&inventoryv1.ListInventoryPartsRequest{PageSize: 10}))
	require.NoError(t, err)
	require.Len(t, siteOnlyResponse.Msg.GetParts(), 1)
	require.Equal(t, allowedSiteID, siteOnlyResponse.Msg.GetParts()[0].GetSiteId())
}

func inventoryContext(t *testing.T, orgID int64, assignments ...authz.Assignment) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod: session.AuthMethodSession, SessionID: fmt.Sprintf("inventory-handler-%d", orgID),
		UserID: orgID, OrganizationID: orgID, ExternalUserID: fmt.Sprintf("inventory-handler-user-%d", orgID),
		Username: fmt.Sprintf("inventory-handler-user-%d", orgID),
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions(assignments))
}
