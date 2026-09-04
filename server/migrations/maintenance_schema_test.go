package migrations_test

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceSchemaRejectsCrossOrgInventoryAndOverAllocation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgA := insertMaintenanceTestOrg(t, db, "inventory-a")
	orgB := insertMaintenanceTestOrg(t, db, "inventory-b")
	siteA := insertMaintenanceTestSite(t, db, orgA, "Inventory Site A")

	_, err := db.ExecContext(ctx, `
		INSERT INTO inventory_part (org_id, name, type, site_id, on_hand, allocated)
		VALUES ($1, 'Cross-org part', 'hashboard', $2, 2, 0)
	`, orgB, siteA)
	require.Error(t, err, "inventory must not reference another organization's site")

	_, err = db.ExecContext(ctx, `
		INSERT INTO inventory_part (org_id, name, type, on_hand, allocated)
		VALUES ($1, 'Over-allocated part', 'hashboard', 1, 2)
	`, orgA)
	require.Error(t, err, "allocated stock must not exceed on-hand stock")
}

func TestMaintenanceSchemaRejectsInvalidTicketReferencesAndEnums(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgA := insertMaintenanceTestOrg(t, db, "ticket-a")
	orgB := insertMaintenanceTestOrg(t, db, "ticket-b")
	siteA := insertMaintenanceTestSite(t, db, orgA, "Ticket Site A")
	buildingA := insertMaintenanceTestBuilding(t, db, orgA, siteA, "Ticket Building A")

	for _, tc := range []struct {
		name         string
		ticketNumber string
		category     int
		status       int
	}{
		{name: "unspecified category", ticketNumber: "TK-BAD-CAT", category: 0, status: 1},
		{name: "unspecified status", ticketNumber: "TK-BAD-STATUS", category: 1, status: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, `
				INSERT INTO repair_ticket (org_id, ticket_number, category, status, component)
				VALUES ($1, $2, $3, $4, 'PSU')
			`, orgA, tc.ticketNumber, tc.category, tc.status)
			require.Error(t, err)
		})
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO repair_ticket (org_id, ticket_number, category, status, component, site_id)
		VALUES ($1, 'TK-CROSS-SITE', 1, 1, 'PSU', $2)
	`, orgB, siteA)
	require.Error(t, err, "ticket must not reference another organization's site")

	_, err = db.ExecContext(ctx, `
		INSERT INTO repair_ticket (org_id, ticket_number, category, status, component, building_id)
		VALUES ($1, 'TK-CROSS-BUILDING', 2, 1, 'Fan', $2)
	`, orgB, buildingA)
	require.Error(t, err, "ticket must not reference another organization's building")

	_, err = db.ExecContext(ctx, `
		INSERT INTO repair_ticket (org_id, ticket_number, category, status, component, assignee_user_id)
		VALUES ($1, 'TK-MISSING-USER', 1, 1, 'PSU', 9223372036854775807)
	`, orgA)
	require.Error(t, err, "ticket assignee must reference a real user")
}

func TestMaintenanceSchemaRequiresSameOrgInventoryForTicketParts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgA := insertMaintenanceTestOrg(t, db, "parts-a")
	orgB := insertMaintenanceTestOrg(t, db, "parts-b")

	var ticketID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO repair_ticket (org_id, ticket_number, category, status, component)
		VALUES ($1, 'TK-PARTS', 1, 1, 'Hashboard')
		RETURNING id
	`, orgA).Scan(&ticketID))

	partA := insertMaintenanceTestPart(t, db, orgA, "Same-org hashboard")
	partB := insertMaintenanceTestPart(t, db, orgB, "Other-org hashboard")

	_, err := db.ExecContext(ctx, `
		INSERT INTO repair_ticket_part
			(org_id, ticket_id, inventory_part_id, part_name, quantity)
		VALUES ($1, $2, $3, 'Same-org hashboard', 1)
	`, orgA, ticketID, partA)
	require.NoError(t, err, "same-org ticket and inventory part should be linkable")

	_, err = db.ExecContext(ctx, `
		INSERT INTO repair_ticket_part
			(org_id, ticket_id, inventory_part_id, part_name, quantity)
		VALUES ($1, $2, $3, 'Other-org hashboard', 1)
	`, orgA, ticketID, partB)
	require.Error(t, err, "ticket part must not reference another organization's inventory")
}

func TestMaintenanceTicketCounterAllocatesUniqueSequentialNumbersConcurrently(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertMaintenanceTestOrg(t, db, "counter")

	const nextNumberSQL = `
		INSERT INTO repair_ticket_counter (org_id, next_number)
		VALUES ($1, 2)
		ON CONFLICT (org_id) DO UPDATE
		SET next_number = repair_ticket_counter.next_number + 1
		RETURNING next_number - 1
	`

	var wg sync.WaitGroup
	numbers := make(chan int64, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var number int64
			err := db.QueryRowContext(ctx, nextNumberSQL, orgID).Scan(&number)
			if err != nil {
				errs <- err
				return
			}
			numbers <- number
		}()
	}
	wg.Wait()
	close(numbers)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	got := make([]int64, 0, 2)
	for number := range numbers {
		got = append(got, number)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	require.Equal(t, []int64{1, 2}, got)
}

func TestMaintenancePermissionMigrationBackfillsBuiltinRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertMaintenanceTestOrg(t, db, "permissions")
	for _, key := range []string{"ADMIN", "FIELD_TECH"} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO role (name, description, is_builtin, builtin_key, organization_id)
			VALUES ($1, $2, TRUE, $1, $3)
		`, key, key+" test role", orgID)
		require.NoError(t, err)
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO role (name, description, is_builtin, organization_id)
		VALUES ('Maintenance observer', 'custom role', FALSE, $1)
	`, orgID)
	require.NoError(t, err)

	upSQL, err := migrations.Migrations.ReadFile("000150_seed_maintenance_permissions.up.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(upSQL))
	require.NoError(t, err)

	for _, key := range []string{"ADMIN", "FIELD_TECH"} {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM role_permission rp
			JOIN role r ON r.id = rp.role_id
			JOIN permission p ON p.id = rp.permission_id
			WHERE r.organization_id = $1
			  AND r.builtin_key = $2
			  AND p.key IN ('maintenance:read', 'maintenance:manage')
		`, orgID, key).Scan(&count))
		require.Equal(t, 2, count, "%s should receive both maintenance permissions", key)
	}

	var customCount int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM role_permission rp
		JOIN role r ON r.id = rp.role_id
		JOIN permission p ON p.id = rp.permission_id
		WHERE r.organization_id = $1
		  AND r.name = 'Maintenance observer'
		  AND p.key IN ('maintenance:read', 'maintenance:manage')
	`, orgID).Scan(&customCount))
	require.Zero(t, customCount, "custom roles must not be changed")
}

func insertMaintenanceTestOrg(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(t.Context(), `
		INSERT INTO organization (org_id, name)
		VALUES ($1, $2)
		RETURNING id
	`, "maintenance-"+suffix, "Maintenance "+suffix).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertMaintenanceTestSite(t *testing.T, db *sql.DB, orgID int64, name string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(t.Context(), `
		INSERT INTO site (org_id, name, slug)
		VALUES ($1, $2, $3)
		RETURNING id
	`, orgID, name, fmt.Sprintf("maintenance-site-%d", orgID)).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertMaintenanceTestBuilding(t *testing.T, db *sql.DB, orgID, siteID int64, name string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(t.Context(), `
		INSERT INTO building (org_id, site_id, name)
		VALUES ($1, $2, $3)
		RETURNING id
	`, orgID, siteID, name).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertMaintenanceTestPart(t *testing.T, db *sql.DB, orgID int64, name string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(t.Context(), `
		INSERT INTO inventory_part (org_id, name, type, on_hand, allocated)
		VALUES ($1, $2, 'hashboard', 2, 0)
		RETURNING id
	`, orgID, name).Scan(&id)
	require.NoError(t, err)
	return id
}
