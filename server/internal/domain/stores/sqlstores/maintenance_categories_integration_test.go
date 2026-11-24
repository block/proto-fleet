package sqlstores_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceOtherTicketRequiresSiteDescriptionAndExplicitAudience(t *testing.T) {
	conn := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, conn, "other-ticket")
	site := insertMaintenanceTestSite(t, conn, org, "General site")
	store := sqlstores.NewSQLMaintenanceStore(conn)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(conn), sqlstores.NewSQLTransactor(conn), nil)
	params := models.CreateParams{OrgID: org, Category: models.TicketCategoryOther, SiteID: &site, Description: "Site access question", IdempotencyKey: uuid.NewString()}
	internal, created, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, models.TicketCategoryOther, internal.Category)
	require.Equal(t, "Unknown", internal.Component)
	require.Equal(t, params.Description, internal.Description)
	require.Equal(t, &site, internal.SiteID)
	require.False(t, internal.SharedWithClients)
	require.Empty(t, internal.Targets)
	require.Empty(t, internal.Audiences)
	for _, scenario := range []struct {
		name   string
		change func(*models.CreateParams)
	}{
		{"site", func(p *models.CreateParams) { p.SiteID = nil }},
		{"description", func(p *models.CreateParams) { p.Description = " " }},
		{"equipment", func(p *models.CreateParams) {
			p.Targets = []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: 1}}
		}},
		{"miner", func(p *models.CreateParams) { identifier := "miner"; p.MinerIdentifier = &identifier }},
		{"shared audience", func(p *models.CreateParams) { p.ShareWithClients = true }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			invalid := params
			invalid.IdempotencyKey = uuid.NewString()
			scenario.change(&invalid)
			_, _, err := service.CreateRepairTicket(t.Context(), invalid)
			require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
		})
	}
	owner, _, _ := insertMaintenanceSnapshotAccounts(t, conn, org)
	var account string
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT account_id FROM hosted_account WHERE id=$1", owner).Scan(&account))
	params.IdempotencyKey = uuid.NewString()
	params.ShareWithClients = true
	params.ClientAccountIDs = []string{account}
	shared, _, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	require.True(t, shared.SharedWithClients)
	require.Len(t, shared.Audiences, 1)
	require.Equal(t, account, shared.Audiences[0].AccountID)
	require.Empty(t, shared.Targets)
	component := "Site access"
	updated, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: org, ID: internal.ID, ExpectedUpdatedAt: internal.UpdatedAt, Component: &component})
	require.NoError(t, err)
	require.Equal(t, component, updated.Component)
}

func TestMaintenanceAffectedBoardsStayWithinOpeningParent(t *testing.T) {
	conn := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, conn, "one-parent")
	site := insertMaintenanceTestSite(t, conn, org, "Parent site")
	_, module, board := insertMaintenancePhysicalTargets(t, conn, org, site, "opening-parent")
	_, other, otherBoard := insertMaintenancePhysicalTargets(t, conn, org, site, "other-parent")
	store := sqlstores.NewSQLMaintenanceStore(conn)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(conn), sqlstores.NewSQLTransactor(conn), nil)
	ticket, _, err := service.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Description: "Module fault", IdempotencyKey: uuid.NewString(), Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}})
	require.NoError(t, err)
	_, err = service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: ticket.UpdatedAt, AffectedBoards: []models.TicketAssetSelection{{Kind: models.TicketCategoryHashboard, ID: otherBoard, ParentModuleID: &other}}})
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	retained, err := service.GetRepairTicket(t.Context(), org, ticket.ID)
	require.NoError(t, err)
	require.Len(t, retained.Ticket.Targets, 1)
	updated, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: ticket.UpdatedAt, AffectedBoards: []models.TicketAssetSelection{{Kind: models.TicketCategoryHashboard, ID: board, ParentModuleID: &module}}})
	require.NoError(t, err)
	require.Len(t, updated.Targets, 2)
}
