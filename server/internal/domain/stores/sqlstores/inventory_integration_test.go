package sqlstores_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	inventorymodels "github.com/block/proto-fleet/server/internal/domain/inventory/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInventoryStoreCRUDIsolationAndAllocationGuards(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLInventoryStore(db)
	orgID := insertInventoryTestOrg(t, db, "crud")
	otherOrgID := insertInventoryTestOrg(t, db, "crud-other")
	siteID := insertInventoryTestSite(t, db, orgID, "North Site")

	manufacturer := "Proto"
	partNumber := "HB-01"
	binLocation := "A-12"
	part, err := store.Create(ctx, inventorymodels.CreateParams{
		OrgID: orgID, Name: "Hashboard", Type: "board", Manufacturer: &manufacturer,
		PartNumber: &partNumber, SiteID: &siteID, OnHand: 5, ReorderPoint: 2, BinLocation: &binLocation,
	})
	require.NoError(t, err)
	assert.Equal(t, "North Site", part.SiteName)
	assert.Equal(t, int32(5), part.OnHand)

	got, err := store.Get(ctx, orgID, part.ID)
	require.NoError(t, err)
	assert.Equal(t, part.ID, got.ID)
	assert.Equal(t, "North Site", got.SiteName)

	_, err = store.Get(ctx, otherOrgID, part.ID)
	assert.True(t, fleeterror.IsNotFoundError(err), "cross-org reads must be hidden: %v", err)

	require.NoError(t, store.Reserve(ctx, orgID, part.ID, 3))
	currentOnHand := int32(5)
	belowAllocated := int32(2)
	_, err = store.Update(ctx, inventorymodels.UpdateParams{
		OrgID: orgID, ID: part.ID, OnHand: &belowAllocated, ExpectedOnHand: &currentOnHand,
	})
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "on_hand cannot fall below allocated: %v", err)

	staleOnHand := int32(4)
	updatedOnHand := int32(6)
	_, err = store.Update(ctx, inventorymodels.UpdateParams{
		OrgID: orgID, ID: part.ID, OnHand: &updatedOnHand, ExpectedOnHand: &staleOnHand,
	})
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "stale on_hand must be rejected: %v", err)
	assert.Contains(t, err.Error(), "refresh")

	updatedReorderPoint := int32(4)
	updatedBin := "B-7"
	got, err = store.Update(ctx, inventorymodels.UpdateParams{
		OrgID: orgID, ID: part.ID, OnHand: &updatedOnHand, ExpectedOnHand: &currentOnHand,
		ReorderPoint: &updatedReorderPoint, BinLocation: &updatedBin,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(6), got.OnHand)
	assert.Equal(t, int32(3), got.Allocated)
	assert.Equal(t, int32(4), got.ReorderPoint)
	assert.Equal(t, "B-7", *got.BinLocation)

	_, err = store.SoftDelete(ctx, orgID, part.ID)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "allocated parts cannot be deleted: %v", err)

	require.NoError(t, store.Release(ctx, orgID, part.ID, 3))
	rows, err := store.SoftDelete(ctx, orgID, part.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
	_, err = store.Get(ctx, orgID, part.ID)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestInventorySiteLockSerializesConcurrentSoftDelete(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLInventoryStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertInventoryTestOrg(t, db, "site-lock")
	siteID := insertInventoryTestSite(t, db, orgID, "Locked Site")

	err := transactor.RunInTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, store.LockSites(txCtx, orgID, []int64{siteID}))

		updateCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, updateErr := db.ExecContext(updateCtx, `UPDATE site SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, siteID)
		require.Error(t, updateErr, "soft delete must wait for the inventory site lock")
		return nil
	})
	require.NoError(t, err)

	result, err := db.ExecContext(ctx, `UPDATE site SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, siteID)
	require.NoError(t, err)
	rows, err := result.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestInventorySiteTransferMapsDuplicateNameToAlreadyExists(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLInventoryStore(db)
	orgID := insertInventoryTestOrg(t, db, "transfer-duplicate")
	siteID := insertInventoryTestSite(t, db, orgID, "Transfer Site")

	_, err := store.Create(ctx, inventorymodels.CreateParams{
		OrgID: orgID, Name: "Fan", Type: "cooling", SiteID: &siteID,
	})
	require.NoError(t, err)
	unassigned, err := store.Create(ctx, inventorymodels.CreateParams{
		OrgID: orgID, Name: "Fan", Type: "cooling",
	})
	require.NoError(t, err)

	_, err = store.Update(ctx, inventorymodels.UpdateParams{OrgID: orgID, ID: unassigned.ID, SiteID: &siteID})
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "duplicate transfer should be actionable: %v", err)
}

func TestInventoryStoreListFiltersCursorInsightsAndSitePicker(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLInventoryStore(db)
	orgID := insertInventoryTestOrg(t, db, "list")
	otherOrgID := insertInventoryTestOrg(t, db, "list-other")
	siteA := insertInventoryTestSite(t, db, orgID, "Site A")
	siteB := insertInventoryTestSite(t, db, orgID, "Site B")

	create := func(name, typ string, siteID *int64, onHand, reorder int32) *inventorymodels.InventoryPart {
		t.Helper()
		part, err := store.Create(ctx, inventorymodels.CreateParams{
			OrgID: orgID, Name: name, Type: typ, SiteID: siteID, OnHand: onHand, ReorderPoint: reorder,
		})
		require.NoError(t, err)
		return part
	}
	first := create("A", "fan", &siteA, 10, 2)
	second := create("B", "fan", &siteA, 1, 2)
	third := create("C", "cable", &siteB, 0, 0)
	_, err := store.Create(ctx, inventorymodels.CreateParams{OrgID: otherOrgID, Name: "Hidden", Type: "fan", OnHand: 9})
	require.NoError(t, err)

	page1, err := store.List(ctx, inventorymodels.ListFilter{OrgID: orgID, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.Equal(t, []int64{third.ID, second.ID}, []int64{page1[0].ID, page1[1].ID})

	cursor := page1[1].ID
	page2, err := store.List(ctx, inventorymodels.ListFilter{OrgID: orgID, CursorID: &cursor, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, first.ID, page2[0].ID)

	lowStock, err := store.List(ctx, inventorymodels.ListFilter{OrgID: orgID, LowStockOnly: true, Limit: 20})
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{second.ID, third.ID}, inventoryPartIDs(lowStock))

	fanFilter := inventorymodels.ListFilter{OrgID: orgID, SiteIDs: []int64{siteA}, Types: []string{"fan"}, Limit: 20}
	fansAtA, err := store.List(ctx, fanFilter)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{first.ID, second.ID}, inventoryPartIDs(fansAtA))
	for _, part := range fansAtA {
		assert.Equal(t, "Site A", part.SiteName)
	}
	filteredCount, err := store.Count(ctx, fanFilter)
	require.NoError(t, err)
	assert.Equal(t, int32(2), filteredCount)

	availableAtA, err := store.ListPartsBySite(ctx, orgID, siteA)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{first.ID, second.ID}, inventoryPartIDs(availableAtA))

	insights, err := store.GetInsights(ctx, orgID)
	require.NoError(t, err)
	assert.Equal(t, &inventorymodels.InventoryInsights{
		TotalOnHand: 11, TotalAllocated: 0, LowStockCount: 2, SitesCount: 2,
		PartTypes: []string{"cable", "fan"},
	}, insights)
}

func TestInventoryStoreConcurrentReserveConsumeAndRelease(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLInventoryStore(db)
	orgID := insertInventoryTestOrg(t, db, "stock")
	part, err := store.Create(ctx, inventorymodels.CreateParams{OrgID: orgID, Name: "PSU", Type: "power", OnHand: 5})
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Reserve(context.Background(), orgID, part.ID, 3)
		}()
	}
	wg.Wait()
	close(errs)

	var success, precondition int
	for reserveErr := range errs {
		switch {
		case reserveErr == nil:
			success++
		case fleeterror.IsFailedPreconditionError(reserveErr):
			precondition++
		default:
			require.NoError(t, reserveErr)
		}
	}
	assert.Equal(t, 1, success)
	assert.Equal(t, 1, precondition)

	got, err := store.Get(ctx, orgID, part.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(3), got.Allocated)

	require.NoError(t, store.Release(ctx, orgID, part.ID, 1))
	require.NoError(t, store.ConsumeReserved(ctx, orgID, part.ID, 2))
	got, err = store.Get(ctx, orgID, part.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(3), got.OnHand)
	assert.Zero(t, got.Allocated)

	err = store.ConsumeReserved(ctx, orgID, part.ID, 1)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	err = store.Reserve(ctx, orgID, part.ID, 4)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.True(t, fleeterror.IsNotFoundError(store.Reserve(ctx, orgID+9999, part.ID, 1)))
}

func TestInventoryStoreResolveSiteAndBulkCreateRollsBack(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLInventoryStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	orgID := insertInventoryTestOrg(t, db, "bulk")
	otherOrgID := insertInventoryTestOrg(t, db, "bulk-other")
	siteID := insertInventoryTestSite(t, db, orgID, "Repair Depot")
	_ = insertInventoryTestSite(t, db, otherOrgID, "Other Depot")

	resolved, err := store.ResolveSiteByName(ctx, orgID, "Repair Depot")
	require.NoError(t, err)
	assert.Equal(t, siteID, resolved)
	_, err = store.ResolveSiteByName(ctx, orgID, "Other Depot")
	assert.True(t, fleeterror.IsNotFoundError(err), "site lookup must be org-scoped: %v", err)

	rows := []inventorymodels.ResolvedCsvRow{
		{Name: "Duplicate", Type: "fan", SiteID: &siteID, OnHand: 1},
		{Name: "Duplicate", Type: "fan", SiteID: &siteID, OnHand: 2},
	}
	err = transactor.RunInTx(ctx, func(txCtx context.Context) error {
		_, bulkErr := store.BulkCreate(txCtx, orgID, rows)
		return bulkErr
	})
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "duplicate import must fail: %v", err)

	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_part WHERE org_id = $1`, orgID).Scan(&count))
	assert.Zero(t, count, "the transaction must roll back every imported row")
}

func inventoryPartIDs(parts []inventorymodels.InventoryPart) []int64 {
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		ids = append(ids, part.ID)
	}
	return ids
}

func insertInventoryTestOrg(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO organization (org_id, name) VALUES ($1, $2) RETURNING id
	`, "inventory-store-"+suffix, "Inventory "+suffix).Scan(&id))
	return id
}

func insertInventoryTestSite(t *testing.T, db *sql.DB, orgID int64, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO site (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id
	`, orgID, name, fmt.Sprintf("inventory-store-%d-%s", orgID, name)).Scan(&id))
	return id
}
