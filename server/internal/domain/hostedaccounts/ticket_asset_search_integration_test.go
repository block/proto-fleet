package hostedaccounts_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTicketAssetSearchTreatsWildcardsLiterally(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	for _, tc := range []struct{ literal, decoy string }{{"rack_A", "rackXA"}, {"unit%100", "unitX100"}, {`row\one`, "rowone"}} {
		expected := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		decoy := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		for _, entry := range []struct {
			id    int64
			label string
		}{{expected, tc.literal}, {decoy, tc.decoy}} {
			_, err := f.db.ExecContext(t.Context(), "UPDATE device SET custom_name=$1 WHERE id=$2", entry.label, entry.id)
			require.NoError(t, err)
			require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, entry.id, 0)))
		}
		host, err := ticketServiceForTest(f.db).ListTicketAssetOptions(t.Context(), models.TicketAssetOptionsParams{OrgID: f.org, Kind: models.TicketCategoryMiner, Search: tc.literal})
		require.NoError(t, err)
		require.Len(t, host.Assets, 1)
		require.Equal(t, expected, host.Assets[0].ID)
		client, err := f.svc.ListAccountTicketAssets(t.Context(), f.user, f.org, &pb.ListAccountTicketAssetsRequest{AccountId: account.AccountID, Search: tc.literal})
		require.NoError(t, err)
		require.Len(t, client.Assets, 1)
		require.Equal(t, expected, client.Assets[0].Id)
	}
	module, _ := f.miningModule(t)
	var expected int64
	for position, serial := range []string{"board_A%", "boardXA100"} {
		var board int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=$2 AND removed_at IS NULL", module, position+1).Scan(&board))
		_, err := f.db.ExecContext(t.Context(), "INSERT INTO hashboard_identity(org_id,hashboard_id,kind,value) VALUES($1,$2,'serial',$3)", f.org, board, serial)
		require.NoError(t, err)
		if serial == "board_A%" {
			expected = board
		}
	}
	ticket, _, err := ticketServiceForTest(f.db).CreateRepairTicket(t.Context(), models.CreateParams{OrgID: f.org, Category: models.TicketCategoryModule, Component: "Board", IdempotencyKey: uuid.NewString(), Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}})
	require.NoError(t, err)
	options, err := ticketServiceForTest(f.db).ListTicketBoardOptions(t.Context(), models.TicketAssetOptionsParams{OrgID: f.org, TicketID: ticket.ID, Search: "board_A%"})
	require.NoError(t, err)
	require.Len(t, options.Assets, 1)
	require.Equal(t, expected, options.Assets[0].ID)
}

func TestAccountTicketAssetsPaginateAcrossKindsWithinAccountScope(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Paginated customer", f.user, hostedaccounts.DefaultCapabilities)
	for range 2 {
		miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	}
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	unallocated := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	request := &pb.ListAccountTicketAssetsRequest{AccountId: account.AccountID, PageSize: 1}
	seen := make(map[models.TicketAssetCursor]bool)
	kinds := make(map[models.TicketCategory]int)
	for {
		page, err := f.svc.ListAccountTicketAssets(t.Context(), f.user, f.org, request)
		require.NoError(t, err)
		require.Len(t, page.Assets, 1)
		asset := page.Assets[0]
		kind, valid := models.PhysicalTicketCategory(int32(asset.Kind))
		require.True(t, valid)
		key := models.TicketAssetCursor{Kind: kind, ID: asset.Id}
		require.False(t, seen[key], "pagination must advance without repeats")
		require.NotEqual(t, models.TicketAssetCursor{Kind: models.TicketCategoryMiner, ID: unallocated}, key)
		seen[key] = true
		kinds[key.Kind]++
		if page.NextPageToken == "" {
			break
		}
		require.NotEqual(t, request.PageToken, page.NextPageToken)
		request.PageToken = page.NextPageToken
	}
	require.Equal(t, map[models.TicketCategory]int{models.TicketCategoryMiner: 2, models.TicketCategoryModule: 1, models.TicketCategoryHashboard: 3}, kinds)
	request.PageToken = "invalid-token"
	_, err := f.svc.ListAccountTicketAssets(t.Context(), f.user, f.org, request)
	require.ErrorContains(t, err, "invalid asset page token")
}

func TestAccountTicketAssetPaginationFiltersHostAuthorityBeforeLimit(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Scoped pagination", f.user, hostedaccounts.DefaultCapabilities)
	q := sqlc.New(f.db)
	var allowedMiner int64
	var deniedSite int64
	for _, denied := range []bool{true, false} {
		label := "Allowed"
		if denied {
			label = "Denied"
		}
		site, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: label, Slug: label})
		require.NoError(t, err)
		miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		_, err = f.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", site.ID, miner)
		require.NoError(t, err)
		require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
		if denied {
			deniedSite = site.ID
		} else {
			allowedMiner = miner
		}
	}
	roles := authz.NewService(f.db, nil)
	reader, err := roles.CreateCustomRole(t.Context(), f.host, f.org, "Physical reader", "", []string{authz.PermAccountManage, authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead})
	require.NoError(t, err)
	manager, err := roles.CreateCustomRole(t.Context(), f.host, f.org, "Account manager", "", []string{authz.PermAccountManage})
	require.NoError(t, err)
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: reader.ID, ScopeType: "org"})
	require.NoError(t, err)
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: manager.ID, ScopeType: "site", ScopeID: sql.NullInt64{Int64: deniedSite, Valid: true}})
	require.NoError(t, err)
	page, err := f.svc.ListAccountTicketAssets(t.Context(), f.user, f.org, &pb.ListAccountTicketAssetsRequest{AccountId: account.AccountID, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, page.Assets, 1, "denied equipment must not consume a visible page")
	require.Equal(t, allowedMiner, page.Assets[0].Id)
	require.Empty(t, page.NextPageToken, "denied equipment must not extend the visible result set")
}

func TestHistoricalTicketBoardPaginationListsEachPhysicalBoardOnce(t *testing.T) {
	f := setup(t)
	module, _ := f.miningModule(t)
	var board int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL", module).Scan(&board))
	service := ticketServiceForTest(f.db)
	ticket, _, err := service.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: f.org, Category: models.TicketCategoryModule, Component: "Board", IdempotencyKey: uuid.NewString(), Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}, {Kind: models.TicketCategoryHashboard, ID: board, ParentModuleID: &module}}})
	require.NoError(t, err)
	correctedModel := "Corrected module model"
	_, err = service.UpdateTicketTarget(t.Context(), models.UpdateTargetParams{OrgID: f.org, TicketID: ticket.ID, Kind: models.TicketCategoryModule, AssetID: module, ExpectedUpdatedAt: ticket.UpdatedAt, ModuleModel: &correctedModel})
	require.NoError(t, err)
	// The module has three boards; the targeted board is already on the ticket,
	// so two selectable boards remain, one per page.
	request := models.TicketAssetOptionsParams{OrgID: f.org, TicketID: ticket.ID, PageSize: 1}
	first, err := service.ListTicketBoardOptions(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, first.Assets, 1)
	require.NotEmpty(t, first.NextPageToken)
	request.PageToken = first.NextPageToken
	last, err := service.ListTicketBoardOptions(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, last.Assets, 1)
	require.Empty(t, last.NextPageToken)
	require.NotEqual(t, first.Assets[0].ID, last.Assets[0].ID)
	for _, asset := range []models.TicketAsset{first.Assets[0], last.Assets[0]} {
		require.NotEqual(t, board, asset.ID, "boards already on the ticket are not offered again")
	}
}
