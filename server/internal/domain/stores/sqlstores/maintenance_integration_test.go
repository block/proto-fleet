package sqlstores_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	maintenancemodels "github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceStoreCRUDHydrationAndOrganizationIsolation(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "crud")
	otherOrgID := insertMaintenanceTestOrg(t, db, "crud-other")
	siteID := insertMaintenanceTestSite(t, db, orgID, "Mine One")
	buildingID := insertMaintenanceTestBuilding(t, db, orgID, siteID, "Building A")
	assigneeID := insertMaintenanceTestUser(t, db, orgID, "maint-crud")

	number, err := store.NextTicketNumber(ctx, orgID)
	require.NoError(t, err)
	ticket, err := store.CreateRepairTicket(ctx, maintenancemodels.CreateParams{
		OrgID: orgID, Category: maintenancemodels.TicketCategoryInfrastructure,
		Component: "Transformer", AssigneeUserID: &assigneeID, SiteID: &siteID, BuildingID: &buildingID,
	}, fmt.Sprintf("TK-%04d", number))
	require.NoError(t, err)
	assert.Equal(t, "Mine One", ticket.SiteName)
	assert.Equal(t, "Building A", ticket.BuildingName)
	assert.Equal(t, "maint-crud", ticket.AssigneeName)

	got, err := store.GetRepairTicket(ctx, orgID, ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, ticket.ID, got.ID)
	_, err = store.GetRepairTicket(ctx, otherOrgID, ticket.ID)
	assert.True(t, fleeterror.IsNotFoundError(err), "cross-org reads must be hidden: %v", err)

	component := "Switchgear"
	updated, err := store.UpdateRepairTicket(ctx, maintenancemodels.UpdateParams{OrgID: orgID, ID: ticket.ID, Component: &component})
	require.NoError(t, err)
	assert.Equal(t, component, updated.Component)

	rows, err := store.SoftDeleteRepairTicket(ctx, orgID, ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
	_, err = store.GetRepairTicket(ctx, orgID, ticket.ID)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestMaintenanceSiteLockSerializesConcurrentSoftDelete(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertMaintenanceTestOrg(t, db, "site-lock")
	siteID := insertMaintenanceTestSite(t, db, orgID, "Locked Ticket Site")

	err := transactor.RunInTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, store.LockSiteForTicket(txCtx, orgID, siteID))

		deleteCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, deleteErr := db.ExecContext(deleteCtx, `UPDATE site SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, siteID)
		require.Error(t, deleteErr, "soft delete must wait for the maintenance site lock")
		return nil
	})
	require.NoError(t, err)

	result, err := db.ExecContext(ctx, `UPDATE site SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, siteID)
	require.NoError(t, err)
	rows, err := result.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestMaintenanceBuildingLockSerializesConcurrentSoftDelete(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertMaintenanceTestOrg(t, db, "building-lock")
	siteID := insertMaintenanceTestSite(t, db, orgID, "Building Lock Site")
	buildingID := insertMaintenanceTestBuilding(t, db, orgID, siteID, "Locked Ticket Building")

	err := transactor.RunInTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, store.LockBuildingForTicket(txCtx, orgID, buildingID))

		deleteCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, deleteErr := db.ExecContext(deleteCtx, `UPDATE building SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, buildingID)
		require.Error(t, deleteErr, "soft delete must wait for the maintenance building lock")
		return nil
	})
	require.NoError(t, err)

	result, err := db.ExecContext(ctx, `UPDATE building SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, buildingID)
	require.NoError(t, err)
	rows, err := result.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestMaintenanceAssigneeResolutionSerializesConcurrentDeactivation(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceReferenceStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertMaintenanceTestOrg(t, db, "assignee-lock")
	userID := insertMaintenanceTestUser(t, db, orgID, "locked-maintenance-assignee")

	err := transactor.RunInTx(ctx, func(txCtx context.Context) error {
		_, err := store.ResolveAssignee(txCtx, orgID, userID)
		require.NoError(t, err)

		deleteCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, deleteErr := db.ExecContext(deleteCtx, `UPDATE "user" SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, userID)
		require.Error(t, deleteErr, "deactivation must wait for the maintenance assignee lock")
		return nil
	})
	require.NoError(t, err)

	result, err := db.ExecContext(ctx, `UPDATE "user" SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, userID)
	require.NoError(t, err)
	rows, err := result.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestMaintenanceStoreConcurrentTicketNumbers(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "counter")

	const workers = 12
	numbers := make(chan int64, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := store.NextTicketNumber(t.Context(), orgID)
			numbers <- n
			errs <- err
		}()
	}
	wg.Wait()
	close(numbers)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	seen := make(map[int64]bool, workers)
	for n := range numbers {
		assert.False(t, seen[n], "duplicate ticket number sequence %d", n)
		seen[n] = true
	}
	assert.Len(t, seen, workers)
}

func TestMaintenanceStoreStableSortCursorsAndFilteredStats(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "cursor")
	siteID := insertMaintenanceTestSite(t, db, orgID, "Cursor Site")

	create := func(component string, status maintenancemodels.TicketStatus, urgent bool) *maintenancemodels.RepairTicket {
		t.Helper()
		n, err := store.NextTicketNumber(ctx, orgID)
		require.NoError(t, err)
		ticket, err := store.CreateRepairTicket(ctx, maintenancemodels.CreateParams{
			OrgID: orgID, Category: maintenancemodels.TicketCategoryInfrastructure,
			Component: component, SiteID: &siteID, Urgent: urgent,
		}, fmt.Sprintf("TK-%04d", n))
		require.NoError(t, err)
		if status != maintenancemodels.TicketStatusOpen {
			updated, updateErr := store.UpdateRepairTicket(ctx, maintenancemodels.UpdateParams{OrgID: orgID, ID: ticket.ID, Status: &status})
			require.NoError(t, updateErr)
			return updated
		}
		return ticket
	}
	first := create("Shared", maintenancemodels.TicketStatusOpen, false)
	second := create("Shared", maintenancemodels.TicketStatusInProgress, true)
	third := create("Other", maintenancemodels.TicketStatusOpen, true)
	_, err := db.ExecContext(ctx, `UPDATE repair_ticket SET created_at = '2026-01-01T00:00:00Z' WHERE id = ANY($1)`, []int64{first.ID, second.ID, third.ID})
	require.NoError(t, err)

	page1, err := store.ListRepairTickets(ctx, maintenancemodels.ListFilter{OrgID: orgID, SortField: maintenancemodels.TicketSortFieldCreatedAt, SortDirection: maintenancemodels.SortDirectionAscending, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	cursor := page1[1].Cursor
	page2, err := store.ListRepairTickets(ctx, maintenancemodels.ListFilter{OrgID: orgID, SortField: maintenancemodels.TicketSortFieldCreatedAt, SortDirection: maintenancemodels.SortDirectionAscending, Cursor: &cursor, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.NotEqual(t, page1[0].ID, page1[1].ID)
	assert.NotEqual(t, page1[1].ID, page2[0].ID)

	stats, err := store.GetTicketStats(ctx, maintenancemodels.ListFilter{OrgID: orgID, SiteIDs: []int64{siteID}, SearchQuery: "Shared"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), stats.CountByStatus[maintenancemodels.TicketStatusOpen])
	assert.Equal(t, int32(1), stats.CountByStatus[maintenancemodels.TicketStatusInProgress])
	assert.Equal(t, int32(1), stats.Urgent)

	completed := maintenancemodels.TicketStatusCompleted
	for _, id := range []int64{first.ID, second.ID} {
		_, err = store.UpdateRepairTicket(ctx, maintenancemodels.UpdateParams{OrgID: orgID, ID: id, Status: &completed})
		require.NoError(t, err)
	}
	component := "Shared"
	completedFilter := maintenancemodels.CompletedFilter{OrgID: orgID, Component: &component, Limit: 1}
	history, err := store.ListCompletedTickets(ctx, completedFilter)
	require.NoError(t, err)
	require.Len(t, history, 1)
	total, err := store.CountCompletedTickets(ctx, completedFilter)
	require.NoError(t, err)
	assert.Equal(t, int32(2), total, "history count must describe the full filtered result, not one page")
	completedFilter.Cursor = &history[0].Cursor
	nextHistory, err := store.ListCompletedTickets(ctx, completedFilter)
	require.NoError(t, err)
	require.Len(t, nextHistory, 1)
	assert.NotEqual(t, history[0].ID, nextHistory[0].ID)
}

func TestMaintenanceStoreEverySortUsesIDAsStableTieBreaker(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "sort-ties")
	siteID := insertMaintenanceTestSite(t, db, orgID, "Same Site")

	var ids []int64
	for range 3 {
		number, err := store.NextTicketNumber(ctx, orgID)
		require.NoError(t, err)
		ticket, err := store.CreateRepairTicket(ctx, maintenancemodels.CreateParams{
			OrgID: orgID, Category: maintenancemodels.TicketCategoryInfrastructure,
			Component: "Same Component", SiteID: &siteID,
		}, fmt.Sprintf("TK-%04d", number))
		require.NoError(t, err)
		ids = append(ids, ticket.ID)
		_, err = db.ExecContext(ctx, `UPDATE repair_ticket SET created_at = '2026-01-01T00:00:00Z' WHERE id = $1`, ticket.ID)
		require.NoError(t, err)
	}

	fields := []maintenancemodels.TicketSortField{
		maintenancemodels.TicketSortFieldComponent,
		maintenancemodels.TicketSortFieldAsset,
		maintenancemodels.TicketSortFieldLocation,
		maintenancemodels.TicketSortFieldStatus,
		maintenancemodels.TicketSortFieldCreatedAt,
	}
	for _, field := range fields {
		for _, direction := range []maintenancemodels.SortDirection{
			maintenancemodels.SortDirectionAscending,
			maintenancemodels.SortDirectionDescending,
		} {
			t.Run(fmt.Sprintf("field-%d-direction-%d", field, direction), func(t *testing.T) {
				var got []int64
				var cursor *maintenancemodels.TicketCursor
				for range 3 {
					page, err := store.ListRepairTickets(ctx, maintenancemodels.ListFilter{
						OrgID: orgID, SortField: field, SortDirection: direction, Cursor: cursor, Limit: 1,
					})
					require.NoError(t, err)
					require.Len(t, page, 1)
					got = append(got, page[0].ID)
					value := page[0].Cursor
					cursor = &value
				}
				if direction == maintenancemodels.SortDirectionAscending {
					assert.Equal(t, ids, got)
				} else {
					assert.Equal(t, []int64{ids[2], ids[1], ids[0]}, got)
				}
			})
		}
	}
}

func TestCompletedTicketsSortAndPaginateByCompletionTime(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "completed-order")

	create := func(component string) *maintenancemodels.RepairTicket {
		t.Helper()
		number, err := store.NextTicketNumber(ctx, orgID)
		require.NoError(t, err)
		ticket, err := store.CreateRepairTicket(ctx, maintenancemodels.CreateParams{
			OrgID: orgID, Category: maintenancemodels.TicketCategoryInfrastructure,
			Component: component,
		}, fmt.Sprintf("TK-%04d", number))
		require.NoError(t, err)
		return ticket
	}
	completedMostRecently := create("Older creation")
	completedEarlier := create("Newer creation")
	_, err := db.ExecContext(ctx, `
		UPDATE repair_ticket
		SET status = 5,
		    created_at = CASE id WHEN $1 THEN TIMESTAMPTZ '2024-01-01' ELSE TIMESTAMPTZ '2025-01-01' END,
		    completed_at = CASE id WHEN $1 THEN TIMESTAMPTZ '2026-01-01' ELSE TIMESTAMPTZ '2025-06-01' END
		WHERE id IN ($1, $2)
	`, completedMostRecently.ID, completedEarlier.ID)
	require.NoError(t, err)

	firstPage, err := store.ListCompletedTickets(ctx, maintenancemodels.CompletedFilter{
		OrgID: orgID, SortField: maintenancemodels.TicketSortFieldCompletedAt, SortDirection: maintenancemodels.SortDirectionDescending, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	assert.Equal(t, completedMostRecently.ID, firstPage[0].ID)

	cursor := firstPage[0].Cursor
	secondPage, err := store.ListCompletedTickets(ctx, maintenancemodels.CompletedFilter{
		OrgID: orgID, SortField: maintenancemodels.TicketSortFieldCompletedAt, SortDirection: maintenancemodels.SortDirectionDescending, Cursor: &cursor, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	assert.Equal(t, completedEarlier.ID, secondPage[0].ID)
}

func TestMaintenanceStoreBulkIDsAreExactAndCommentDeletionIsAuthorOnly(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "bulk")
	otherOrgID := insertMaintenanceTestOrg(t, db, "bulk-other")
	authorID := insertMaintenanceTestUser(t, db, orgID, "maint-author")
	otherUserID := insertMaintenanceTestUser(t, db, orgID, "maint-other-user")

	create := func(org int64, component string) *maintenancemodels.RepairTicket {
		t.Helper()
		n, err := store.NextTicketNumber(ctx, org)
		require.NoError(t, err)
		ticket, err := store.CreateRepairTicket(ctx, maintenancemodels.CreateParams{
			OrgID: org, Category: maintenancemodels.TicketCategoryInfrastructure, Component: component,
		}, fmt.Sprintf("TK-%04d", n))
		require.NoError(t, err)
		return ticket
	}
	one := create(orgID, "One")
	two := create(orgID, "Two")
	hidden := create(otherOrgID, "Hidden")

	_, err := store.BulkMarkUrgent(ctx, orgID, []int64{one.ID, hidden.ID})
	assert.True(t, fleeterror.IsNotFoundError(err), "partial cross-org bulk mutation must fail: %v", err)
	got, err := store.GetRepairTicket(ctx, orgID, one.ID)
	require.NoError(t, err)
	assert.False(t, got.Urgent)

	rows, err := store.BulkMarkUrgent(ctx, orgID, []int64{one.ID, one.ID, two.ID})
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)

	comment, err := store.CreateTicketComment(ctx, orgID, one.ID, authorID, "private repair note")
	require.NoError(t, err)
	assert.Equal(t, "maint-author", comment.UserName)
	_, err = db.ExecContext(ctx, `UPDATE "user" SET username = 'maint-renamed-author' WHERE id = $1`, authorID)
	require.NoError(t, err)
	comments, err := store.ListTicketComments(ctx, orgID, one.ID)
	require.NoError(t, err)
	require.Len(t, comments, 1)
	assert.Equal(t, "maint-author", comments[0].UserName, "comment history preserves the recorded author name")

	var inventoryPartID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO inventory_part (org_id, name, type, on_hand, allocated, reorder_point)
		VALUES ($1, 'Fan', 'cooling', 5, 1, 1) RETURNING id
	`, orgID).Scan(&inventoryPartID))
	require.NoError(t, store.InsertTicketPart(ctx, orgID, one.ID, inventoryPartID, "stale fan name", 1))
	parts, err := store.ListTicketParts(ctx, orgID, one.ID)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "Fan", parts[0].PartName, "part labels are hydrated from inventory")
	require.NoError(t, store.MarkTicketPartsConsumed(ctx, orgID, one.ID))
	parts, err = store.ListTicketParts(ctx, orgID, one.ID)
	require.NoError(t, err)
	require.NotNil(t, parts[0].ConsumedAt)
	require.NoError(t, store.SetTicketParts(ctx, orgID, one.ID))
	parts, err = store.ListTicketParts(ctx, orgID, one.ID)
	require.NoError(t, err)
	assert.Len(t, parts, 1, "replacing active reservations must preserve consumed history")

	rows, err = store.SoftDeleteTicketComment(ctx, orgID, otherUserID, comment.ID)
	require.NoError(t, err)
	assert.Zero(t, rows)
	rows, err = store.SoftDeleteTicketComment(ctx, orgID, authorID, comment.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestMaintenanceReferenceStoreRejectsCrossOrganizationAssetsAndListsLiveAssignees(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	refs := sqlstores.NewSQLMaintenanceReferenceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "refs")
	otherOrgID := insertMaintenanceTestOrg(t, db, "refs-other")
	userID := insertMaintenanceTestUser(t, db, orgID, "maint-live-assignee")
	deletedUserID := insertMaintenanceTestUser(t, db, orgID, "maint-deleted-assignee")
	_, err := db.ExecContext(ctx, `UPDATE user_organization SET deleted_at = NOW() WHERE user_id = $1 AND organization_id = $2`, deletedUserID, orgID)
	require.NoError(t, err)
	otherDevice := insertMaintenanceTestDevice(t, db, otherOrgID, "maint-cross-org-device")
	localDevice := insertMaintenanceTestDevice(t, db, orgID, "maint-local-device")
	otherSiteID := insertMaintenanceTestSite(t, db, otherOrgID, "Other Site")

	_, err = refs.ResolveMinerContext(ctx, orgID, otherDevice)
	assert.True(t, fleeterror.IsNotFoundError(err), "cross-org miner must be hidden: %v", err)
	localContext, err := refs.ResolveMinerContext(ctx, orgID, localDevice)
	require.NoError(t, err)
	assert.Equal(t, localDevice, localContext.MinerIdentifier)
	_, err = refs.ResolveLocationContext(ctx, orgID, &otherSiteID, nil)
	assert.True(t, fleeterror.IsNotFoundError(err), "cross-org locations must be hidden: %v", err)
	assignee, err := refs.ResolveAssignee(ctx, orgID, userID)
	require.NoError(t, err)
	assert.Equal(t, "maint-live-assignee", assignee.Username)
	_, err = refs.ResolveAssignee(ctx, orgID, deletedUserID)
	assert.True(t, fleeterror.IsNotFoundError(err))

	assignees, err := refs.ListAssignees(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, assignees, 1)
	assert.Equal(t, userID, assignees[0].UserID)
	assert.Equal(t, "maint-live-assignee", assignees[0].Username)
}

func insertMaintenanceTestOrg(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO organization (org_id, name) VALUES ($1, $2) RETURNING id
	`, "maintenance-store-"+suffix, "Maintenance "+suffix).Scan(&id))
	return id
}

func insertMaintenanceTestSite(t *testing.T, db *sql.DB, orgID int64, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO site (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id
	`, orgID, name, fmt.Sprintf("maintenance-store-%d-%s", orgID, name)).Scan(&id))
	return id
}

func insertMaintenanceTestBuilding(t *testing.T, db *sql.DB, orgID, siteID int64, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO building (org_id, site_id, name) VALUES ($1, $2, $3) RETURNING id
	`, orgID, siteID, name).Scan(&id))
	return id
}

func insertMaintenanceTestUser(t *testing.T, db *sql.DB, orgID int64, username string) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO "user" (user_id, username, password_hash) VALUES ($1, $2, 'hash') RETURNING id
	`, username+"-id", username).Scan(&userID))
	var roleID int64
	err := db.QueryRowContext(t.Context(), `
		INSERT INTO role (name, is_builtin, builtin_key, organization_id)
		VALUES ('Admin', TRUE, 'admin', $1)
		ON CONFLICT (organization_id, builtin_key) WHERE is_builtin = TRUE AND deleted_at IS NULL
		DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, orgID).Scan(&roleID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO user_organization (user_id, organization_id, role_id)
		VALUES ($1, $2, $3)
	`, userID, orgID, roleID)
	require.NoError(t, err)
	return userID
}

func insertMaintenanceTestDevice(t *testing.T, db *sql.DB, orgID int64, identifier string) string {
	t.Helper()
	var discoveredID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name)
		VALUES ($1, $2, '192.0.2.1', '80', 'http', 'virtual') RETURNING id
	`, orgID, identifier).Scan(&discoveredID))
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO device (device_identifier, mac_address, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4)
	`, identifier, "00:00:00:00:00:01", orgID, discoveredID)
	require.NoError(t, err)
	return identifier
}
