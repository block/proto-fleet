package sqlstores_test

import (
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceReplacementClearPersistsAndOmissionPreserves(t *testing.T) {
	for _, kind := range []models.TicketCategory{models.TicketCategoryModule, models.TicketCategoryHashboard} {
		t.Run(fmt.Sprintf("kind=%d", kind), func(t *testing.T) {
			db := testutil.GetTestDB(t)
			ctx := t.Context()
			org := insertMaintenanceTestOrg(t, db, "replacement-clear")
			site := insertMaintenanceTestSite(t, db, org, "Repair site")
			_, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "original")
			_, replacementModule, _ := insertMaintenancePhysicalTargets(t, db, org, site, "replacement")
			var replacementBoard int64
			require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,NOW(),NOW()) RETURNING id", org).Scan(&replacementBoard))
			_, err := db.ExecContext(ctx, "INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,2,NOW(),NOW())", replacementBoard, module)
			require.NoError(t, err)
			target := models.TicketAssetSelection{Kind: kind, ID: module}
			if kind == models.TicketCategoryHashboard {
				target.ID, target.ParentModuleID = board, &module
			}
			store := sqlstores.NewSQLMaintenanceStore(db)
			service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), sqlstores.NewSQLTransactor(db), nil)
			ticket, _, err := service.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: kind, Component: "Power", IdempotencyKey: "replacement-clear", Targets: []models.TicketAssetSelection{target}})
			require.NoError(t, err)
			require.Len(t, ticket.Targets, 1)
			opening := ticket.Targets[0]
			params := models.UpdateTargetParams{OrgID: org, TicketID: ticket.ID, AssetID: target.ID, Kind: kind, ExpectedUpdatedAt: ticket.UpdatedAt}
			if kind == models.TicketCategoryModule {
				params.ReplacementModuleID = &replacementModule
			} else {
				params.ReplacementHashboardID = &replacementBoard
			}
			_, err = service.UpdateTicketTarget(ctx, params)
			require.NoError(t, err)
			linked, err := store.GetRepairTicket(ctx, org, ticket.ID)
			require.NoError(t, err)
			if kind == models.TicketCategoryModule {
				require.Equal(t, &replacementModule, linked.Targets[0].ReplacementModuleID)
			} else {
				require.Equal(t, &replacementBoard, linked.Targets[0].ReplacementHashboardID)
			}
			// A partial evidence update must preserve a replacement omitted by its caller.
			diagnosis := "Connector failure"
			_, err = service.UpdateTicketTarget(ctx, models.UpdateTargetParams{OrgID: org, TicketID: ticket.ID, AssetID: target.ID, Kind: kind, ExpectedUpdatedAt: linked.UpdatedAt, Diagnosis: &diagnosis})
			require.NoError(t, err)
			preserved, err := store.GetRepairTicket(ctx, org, ticket.ID)
			require.NoError(t, err)
			require.Equal(t, linked.Targets[0].ReplacementModuleID, preserved.Targets[0].ReplacementModuleID)
			require.Equal(t, linked.Targets[0].ReplacementHashboardID, preserved.Targets[0].ReplacementHashboardID)
			// Clearing alone is a mutation, without rewriting opening or model evidence.
			_, err = service.UpdateTicketTarget(ctx, models.UpdateTargetParams{OrgID: org, TicketID: ticket.ID, AssetID: target.ID, Kind: kind, ExpectedUpdatedAt: preserved.UpdatedAt,
				ClearReplacementModule: kind == models.TicketCategoryModule, ClearReplacementHashboard: kind == models.TicketCategoryHashboard})
			require.NoError(t, err)
			cleared, err := store.GetRepairTicket(ctx, org, ticket.ID)
			require.NoError(t, err)
			expected := opening
			expected.Diagnosis = diagnosis
			require.Equal(t, expected, cleared.Targets[0], "reloading must retain original identity, placement and model evidence with no replacement link")
			for _, id := range []int64{module, replacementModule} {
				var actualModel string
				require.NoError(t, db.QueryRowContext(ctx, "SELECT model FROM container_module WHERE id=$1", id).Scan(&actualModel))
				require.Equal(t, "BM1368", actualModel, "removing the link must preserve physical inventory model evidence")
			}
		})
	}
}
