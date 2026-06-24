package sqlstores_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// TestGetRunningPowerTargetScheduleOverlaps_SiteAndBuilding pins the SQL that
// the SET_POWER_TARGET conflict filter relies on: a running site/building
// power-target schedule must surface its devices so a lower-priority command
// can't silently override them. Site targets resolve by device.site_id;
// building targets by device.building_id OR rack membership
// (device_set_rack.building_id) — mirroring the MinerFilter expansion path.
func TestGetRunningPowerTargetScheduleOverlaps_SiteAndBuilding(t *testing.T) {
	conn := testutil.GetTestDB(t)
	ctx := context.Background()
	seedOverlapFixture(t, conn)

	store := sqlstores.NewSQLScheduleStore(conn)
	overlaps, err := store.GetRunningPowerTargetScheduleOverlaps(ctx, 1, []string{
		"m-site", "m-bldg-direct", "m-bldg-rack", "m-none",
	})
	require.NoError(t, err)

	// schedule 1 = site target "7"; schedule 2 = building target "9".
	got := map[string][]int64{}
	for _, o := range overlaps {
		got[o.DeviceIdentifier] = append(got[o.DeviceIdentifier], o.ScheduleID)
	}

	require.ElementsMatch(t, []int64{1}, got["m-site"], "site target should own its site's miner")
	require.ElementsMatch(t, []int64{2}, got["m-bldg-direct"], "building target should own a directly-assigned miner")
	require.ElementsMatch(t, []int64{2}, got["m-bldg-rack"], "building target should own a miner via its rack")
	require.Empty(t, got["m-none"], "an unscoped miner must not overlap any site/building schedule")
}

func seedOverlapFixture(t *testing.T, conn *sql.DB) {
	t.Helper()
	exec := func(query string, args ...any) {
		_, err := conn.Exec(query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO organization (id, org_id, name) VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org')`)
	exec(`INSERT INTO site (id, org_id, name) VALUES (7, 1, 'Site Seven')`)
	exec(`INSERT INTO building (id, org_id, site_id, name) VALUES (9, 1, 7, 'Building Nine')`)

	// device_identifier, site_id, building_id (NULL = unset)
	devices := []struct {
		id         int64
		ident      string
		mac        string
		siteID     sql.NullInt64
		buildingID sql.NullInt64
	}{
		{1, "m-site", "AA:BB:CC:DD:EE:01", sql.NullInt64{Int64: 7, Valid: true}, sql.NullInt64{}},
		{2, "m-bldg-direct", "AA:BB:CC:DD:EE:02", sql.NullInt64{}, sql.NullInt64{Int64: 9, Valid: true}},
		{3, "m-bldg-rack", "AA:BB:CC:DD:EE:03", sql.NullInt64{}, sql.NullInt64{}},
		{4, "m-none", "AA:BB:CC:DD:EE:04", sql.NullInt64{}, sql.NullInt64{}},
	}
	for _, d := range devices {
		exec(`INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'm', 'mfr', 'proto', '10.0.0.1', '50051', 'grpc', TRUE)`, d.id, d.ident)
		exec(`INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address, site_id, building_id)
			VALUES ($1, 1, $1, $2, $3, $4, $5)`, d.id, d.ident, d.mac, d.siteID, d.buildingID)
	}

	// Rack in building 9 with m-bldg-rack as a member (building-via-rack path).
	exec(`INSERT INTO device_set (id, org_id, type, label) VALUES (100, 1, 'rack', 'Rack 100')`)
	exec(`INSERT INTO device_set_rack (device_set_id, org_id, rows, columns, building_id) VALUES (100, 1, 1, 1, 9)`)
	exec(`INSERT INTO device_set_membership (org_id, device_set_id, device_set_type, device_id, device_identifier)
		VALUES (1, 100, 'rack', 3, 'm-bldg-rack')`)

	// Two running set_power_target schedules: site(7) priority 1, building(9) priority 2.
	insertSchedule := func(id, priority int64, targetType, targetID string) {
		exec(`INSERT INTO schedule (id, org_id, name, action, schedule_type, start_date, start_time, timezone, status, priority, created_by)
			VALUES ($1, 1, $2, 'set_power_target', 'one_time', '2026-01-01', '00:00', 'UTC', 'running', $3, 1)`,
			id, "sched-"+targetType, priority)
		exec(`INSERT INTO schedule_target (schedule_id, target_type, target_id) VALUES ($1, $2, $3)`, id, targetType, targetID)
	}
	insertSchedule(1, 1, "site", "7")
	insertSchedule(2, 2, "building", "9")
}
