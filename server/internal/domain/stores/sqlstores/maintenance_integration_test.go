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

const maintenanceCreateRequestHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

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
		OrgID: orgID, IdempotencyKey: "crud-create", CreateRequestHash: maintenanceCreateRequestHash, Category: maintenancemodels.TicketCategoryInfrastructure,
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

func TestMaintenanceMinerLockSerializesConcurrentPlacementChange(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceReferenceStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertMaintenanceTestOrg(t, db, "miner-lock")
	identifier := insertMaintenanceTestDevice(t, db, orgID, "locked-maintenance-miner")

	err := transactor.RunInTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, store.LockMinerForTicket(txCtx, orgID, identifier))

		updateCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, updateErr := db.ExecContext(updateCtx, `
			UPDATE device SET site_id = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE org_id = $1 AND device_identifier = $2
		`, orgID, identifier)
		require.Error(t, updateErr, "placement update must wait for the maintenance miner lock")
		return nil
	})
	require.NoError(t, err)

	result, err := db.ExecContext(ctx, `
		UPDATE device SET site_id = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE org_id = $1 AND device_identifier = $2
	`, orgID, identifier)
	require.NoError(t, err)
	rows, err := result.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestMaintenanceMinerLockSerializesConcurrentGroupMembershipChange(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	maintenanceStore := sqlstores.NewSQLMaintenanceReferenceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertMaintenanceTestOrg(t, db, "group-lock")
	identifier := insertMaintenanceTestDevice(t, db, orgID, "group-locked-miner")
	var groupID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO device_set (org_id, type, label) VALUES ($1, 'group', 'Repair group') RETURNING id
	`, orgID).Scan(&groupID))

	err := transactor.RunInTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, maintenanceStore.LockMinerForTicket(txCtx, orgID, identifier))

		updateCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, updateErr := collectionStore.AddDevicesToCollection(updateCtx, orgID, groupID, []string{identifier})
		require.Error(t, updateErr, "group membership update must wait for the maintenance miner lock")
		return nil
	})
	require.NoError(t, err)

	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, groupID, []string{identifier})
	require.NoError(t, err)
	var membershipCount int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM device_set_membership
		WHERE org_id = $1 AND device_set_id = $2 AND device_identifier = $3
	`, orgID, groupID, identifier).Scan(&membershipCount))
	assert.Equal(t, 1, membershipCount)

	err = transactor.RunInTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, maintenanceStore.LockMinerForTicket(txCtx, orgID, identifier))

		updateCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, updateErr := collectionStore.RemoveDevicesFromCollection(updateCtx, orgID, groupID, []string{identifier})
		require.Error(t, updateErr, "group membership removal must wait for the maintenance miner lock")
		return nil
	})
	require.NoError(t, err)

	_, err = collectionStore.RemoveDevicesFromCollection(ctx, orgID, groupID, []string{identifier})
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM device_set_membership
		WHERE org_id = $1 AND device_set_id = $2 AND device_identifier = $3
	`, orgID, groupID, identifier).Scan(&membershipCount))
	assert.Zero(t, membershipCount)
}

func TestMaintenanceMinerLockAllowsDescriptiveGroupRename(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	maintenanceStore := sqlstores.NewSQLMaintenanceReferenceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertMaintenanceTestOrg(t, db, "group-label")
	identifier := insertMaintenanceTestDevice(t, db, orgID, "group-label-miner")
	var groupID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO device_set (org_id, type, label) VALUES ($1, 'group', 'Before') RETURNING id
	`, orgID).Scan(&groupID))
	_, err := collectionStore.AddDevicesToCollection(ctx, orgID, groupID, []string{identifier})
	require.NoError(t, err)

	err = transactor.RunInTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, maintenanceStore.LockMinerForTicket(txCtx, orgID, identifier))

		updateCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, updateErr := db.ExecContext(updateCtx, `UPDATE device_set SET label = 'After' WHERE id = $1`, groupID)
		require.NoError(t, updateErr, "descriptive renames need not wait for ticket placement locks")
		return nil
	})
	require.NoError(t, err)

	result, err := db.ExecContext(ctx, `UPDATE device_set SET label = 'After' WHERE id = $1`, groupID)
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

func TestMaintenanceStoreClearsRMAEta(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "clear-rma-eta")

	number, err := store.NextTicketNumber(ctx, orgID)
	require.NoError(t, err)
	ticket, err := store.CreateRepairTicket(ctx, maintenancemodels.CreateParams{
		OrgID: orgID, IdempotencyKey: "rma-eta-create", CreateRequestHash: maintenanceCreateRequestHash, Category: maintenancemodels.TicketCategoryInfrastructure, Component: "Transformer",
	}, fmt.Sprintf("TK-%04d", number))
	require.NoError(t, err)
	eta := time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC)
	updated, err := store.UpdateRepairTicket(ctx, maintenancemodels.UpdateParams{OrgID: orgID, ID: ticket.ID, RMAEta: &eta})
	require.NoError(t, err)
	require.NotNil(t, updated.RMAEta)

	updated, err = store.UpdateRepairTicket(ctx, maintenancemodels.UpdateParams{OrgID: orgID, ID: ticket.ID, ClearRMAEta: true})
	require.NoError(t, err)
	assert.Nil(t, updated.RMAEta)
}

func TestCompletedTicketRetainsAssigneeNameAfterUserDeactivation(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "completed-assignee")
	userID := insertMaintenanceTestUser(t, db, orgID, "former-technician")

	number, err := store.NextTicketNumber(ctx, orgID)
	require.NoError(t, err)
	ticket, err := store.CreateRepairTicket(ctx, maintenancemodels.CreateParams{
		OrgID: orgID, IdempotencyKey: "historical-assignee-create", CreateRequestHash: maintenanceCreateRequestHash, Category: maintenancemodels.TicketCategoryInfrastructure,
		Component: "Transformer", AssigneeUserID: &userID,
	}, fmt.Sprintf("TK-%04d", number))
	require.NoError(t, err)
	completed := maintenancemodels.TicketStatusCompleted
	_, err = store.UpdateRepairTicket(ctx, maintenancemodels.UpdateParams{OrgID: orgID, ID: ticket.ID, Status: &completed})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE "user" SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, userID)
	require.NoError(t, err)

	detail, err := store.GetRepairTicket(ctx, orgID, ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, "former-technician", detail.AssigneeName)
	history, err := store.ListCompletedTickets(ctx, maintenancemodels.CompletedFilter{OrgID: orgID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "former-technician", history[0].AssigneeName)
	assignees, err := store.ListCompletedTicketAssignees(ctx, maintenancemodels.CompletedFilter{OrgID: orgID})
	require.NoError(t, err)
	require.Len(t, assignees, 1)
	assert.Equal(t, userID, assignees[0].UserID)
	assert.Equal(t, "former-technician", assignees[0].Username)
	activeAssignees, err := sqlstores.NewSQLMaintenanceReferenceStore(db).ListAssignees(ctx, orgID)
	require.NoError(t, err)
	assert.Empty(t, activeAssignees, "inactive historical users must not be offered for new assignments")
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

func TestMaintenanceStoreSerializesConcurrentCreatesByIdempotencyKey(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLMaintenanceStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertMaintenanceTestOrg(t, db, "create-idempotency")
	const key = "same-create-operation"

	start := make(chan struct{})
	ids := make(chan int64, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var ticketID int64
			err := transactor.RunInTx(t.Context(), func(txCtx context.Context) error {
				if err := store.LockRepairTicketCreateKey(txCtx, orgID, key); err != nil {
					return err
				}
				existing, _, err := store.GetRepairTicketByIdempotencyKey(txCtx, orgID, key)
				if err == nil {
					ticketID = existing.ID
					return nil
				}
				if !fleeterror.IsNotFoundError(err) {
					return err
				}
				number, err := store.NextTicketNumber(txCtx, orgID)
				if err != nil {
					return err
				}
				created, err := store.CreateRepairTicket(txCtx, maintenancemodels.CreateParams{
					OrgID: orgID, IdempotencyKey: key, CreateRequestHash: maintenanceCreateRequestHash, Category: maintenancemodels.TicketCategoryInfrastructure,
					Component: "Network",
				}, fmt.Sprintf("TK-%04d", number))
				if err != nil {
					return err
				}
				ticketID = created.ID
				return nil
			})
			ids <- ticketID
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	var firstID int64
	for id := range ids {
		if firstID == 0 {
			firstID = id
		}
		assert.Equal(t, firstID, id)
	}
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM repair_ticket WHERE org_id = $1 AND idempotency_key = $2`, orgID, key,
	).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestMaintenanceStoreSerializesConcurrentCommentsByIdempotencyKey(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLMaintenanceStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertMaintenanceTestOrg(t, db, "comment-idem")
	userID := insertMaintenanceTestUser(t, db, orgID, "comment-author")
	number, err := store.NextTicketNumber(t.Context(), orgID)
	require.NoError(t, err)
	ticket, err := store.CreateRepairTicket(t.Context(), maintenancemodels.CreateParams{
		OrgID: orgID, IdempotencyKey: "comment-ticket", CreateRequestHash: maintenanceCreateRequestHash,
		Category: maintenancemodels.TicketCategoryInfrastructure, Component: "Network",
	}, fmt.Sprintf("TK-%04d", number))
	require.NoError(t, err)
	const key = "same-comment-operation"

	start := make(chan struct{})
	ids := make(chan int64, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var commentID int64
			err := transactor.RunInTx(t.Context(), func(txCtx context.Context) error {
				if err := store.LockTicketCommentCreateKey(txCtx, orgID, key); err != nil {
					return err
				}
				existing, _, err := store.GetTicketCommentByIdempotencyKey(txCtx, orgID, key)
				if err == nil {
					commentID = existing.ID
					return nil
				}
				if !fleeterror.IsNotFoundError(err) {
					return err
				}
				created, err := store.CreateTicketComment(
					txCtx, orgID, ticket.ID, userID, "Replaced fan", key, maintenanceCreateRequestHash,
				)
				if err != nil {
					return err
				}
				commentID = created.ID
				return nil
			})
			ids <- commentID
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	var firstID int64
	for id := range ids {
		if firstID == 0 {
			firstID = id
		}
		assert.Equal(t, firstID, id)
	}
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM repair_ticket_comment WHERE org_id = $1 AND idempotency_key = $2
	`, orgID, key).Scan(&count))
	assert.Equal(t, 1, count)
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
			OrgID: orgID, IdempotencyKey: fmt.Sprintf("cursor-create-%d", n), CreateRequestHash: maintenanceCreateRequestHash, Category: maintenancemodels.TicketCategoryInfrastructure,
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
			OrgID: orgID, IdempotencyKey: fmt.Sprintf("completed-cursor-create-%d", number), CreateRequestHash: maintenanceCreateRequestHash, Category: maintenancemodels.TicketCategoryInfrastructure,
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
			OrgID: orgID, IdempotencyKey: fmt.Sprintf("inventory-create-%d", number), CreateRequestHash: maintenanceCreateRequestHash, Category: maintenancemodels.TicketCategoryInfrastructure,
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

func TestMaintenanceStorePreservesHistoryAndCommentDeletionIsAuthorOnly(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	orgID := insertMaintenanceTestOrg(t, db, "history")
	authorID := insertMaintenanceTestUser(t, db, orgID, "maint-author")
	otherUserID := insertMaintenanceTestUser(t, db, orgID, "maint-other-user")
	one, err := store.CreateRepairTicket(ctx, maintenancemodels.CreateParams{
		OrgID: orgID, IdempotencyKey: "history-create", CreateRequestHash: maintenanceCreateRequestHash,
		Category: maintenancemodels.TicketCategoryInfrastructure, Component: "One",
	}, "TK-0001")
	require.NoError(t, err)

	comment, err := store.CreateTicketComment(ctx, orgID, one.ID, authorID, "private repair note", "comment-store-key", maintenanceCreateRequestHash)
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

	rows, err := store.SoftDeleteTicketComment(ctx, orgID, otherUserID, comment.ID)
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
