package migrations_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

// The release channel migration must round-trip: down removes every object it
// created and up restores them. Functions are checked explicitly because a
// down that forgot them would still succeed, and so would the CREATE OR
// REPLACE in the next up.
func TestReleaseChannelsMigrationDownAndUp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	downSQL, err := migrations.Migrations.ReadFile("000148_release_channels.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000148_release_channels.up.sql")
	require.NoError(t, err)

	relations := []string{
		"release_channel", "release_channel_target", "release_channel_firmware",
		"firmware_rollout", "firmware_rollout_device", "device_firmware_deployment",
		"release_channel_placement", "release_channel_match", "release_channel_resolution",
		"release_channel_member", "release_channel_conflict", "firmware_rollout_suppressed_device",
	}
	functions := []string{"release_channel_pair_key", "firmware_rollout_bump_revision", "firmware_rollout_touch_from_rows"}
	triggers := []string{
		"firmware_rollout_revision", "firmware_rollout_device_inserted", "firmware_rollout_device_updated",
		"device_firmware_deployment_inserted", "device_firmware_deployment_updated",
	}
	count := func(query, name string) int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, query, name).Scan(&n))
		return n
	}
	requireObjects := func(present bool) {
		t.Helper()
		for _, name := range relations {
			require.Equal(t, present, count(`SELECT count(*) FROM pg_class WHERE relname = $1 AND relkind IN ('r', 'v')`, name) == 1, name)
		}
		for _, name := range functions {
			require.Equal(t, present, count(`SELECT count(*) FROM pg_proc WHERE proname = $1`, name) == 1, name)
		}
		for _, name := range triggers {
			require.Equal(t, present, count(`SELECT count(*) FROM pg_trigger WHERE tgname = $1 AND NOT tgisinternal`, name) == 1, name)
		}
	}

	requireObjects(true)
	_, err = db.ExecContext(ctx, string(downSQL))
	require.NoError(t, err)
	requireObjects(false)
	_, err = db.ExecContext(ctx, string(upSQL))
	require.NoError(t, err)
	requireObjects(true)

	var members int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM release_channel_member`).Scan(&members))
	require.Equal(t, 0, members)
}

// Membership resolution against live placement: the most specific selector
// wins, ties belong to no channel, a rack's building overrides the device's,
// and soft-deleted devices never match.
func TestReleaseChannelsSchemaResolvesMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	f := newReleaseChannelFixture(t, db)
	rack := f.rack("R1", f.buildingA)
	group := f.group("G1")
	inRack := f.device("in-rack", f.buildingB)
	f.addToSet(rack, "rack", inRack)
	inGroup := f.device("in-group", f.buildingB)
	f.addToSet(group, "group", inGroup)
	siteOnly := f.device("site-only", 0)
	tied := f.device("tied", 0)
	f.addToSet(group, "group", tied)
	deletedTwin := f.device("twin", 0)
	deletedTwin.softDelete()
	twin := f.device("twin", 0)

	site := f.channel("site")
	f.target(site, "site", f.site)
	byRack := f.channel("rack")
	f.target(byRack, "rack", rack)
	byBuildingB := f.channel("building-b")
	f.target(byBuildingB, "building", f.buildingB)
	byGroup := f.channel("group")
	f.target(byGroup, "group", group)
	byGroupToo := f.channel("group-too")
	f.target(byGroupToo, "group", group)
	byMiner := f.channel("miner")
	f.minerTarget(byMiner, "in-group")
	f.minerTarget(byMiner, "tied")
	byTwin := f.channel("twin")
	f.minerTarget(byTwin, "twin")

	// in-rack: site (5) and rack (3); its rack's building is A, so building-b
	// does not match even though device.building_id is B.
	f.requireMember(inRack, byRack, true)
	// in-group: site, building-b (4), group (2), group-too (2), miner (1).
	f.requireMember(inGroup, byMiner, true)
	f.requireResolution(inGroup, map[int64]string{byMiner: "winner", byGroup: "loser", byGroupToo: "loser", byBuildingB: "loser", site: "loser"})
	f.requireMember(siteOnly, site, false)
	// tied: site (5), two groups (2) and miner (1); only the miner selector
	// makes it a member.
	f.requireMember(tied, byMiner, true)
	// The miner selector follows the identifier to the live row only.
	f.requireMember(twin, byTwin, true)
	f.requireNoMember(deletedTwin)

	// Remove the miner selector: the two group channels tie and exclude it.
	f.exec(`DELETE FROM release_channel_target WHERE channel_id = $1 AND device_identifier = 'tied'`, byMiner)
	f.requireNoMember(tied)
	f.requireResolution(tied, map[int64]string{byGroup: "excluded_tie", byGroupToo: "excluded_tie", site: "loser"})
}

// The folded pair key drives assignment identity, the one-active-rollout
// invariant, and observed-identity matching.
func TestReleaseChannelsSchemaFoldsPairKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	f := newReleaseChannelFixture(t, db)
	channel := f.channel("pairs")

	// The fold trims exactly what strings.TrimSpace trims, ASCII and Unicode.
	for _, padded := range []string{" \tBitmain\n", "\u00a0Bitmain\u3000", "\u2003\u0085Bitmain\u202f"} {
		require.Equal(t, "bitmain", strings.ToLower(strings.TrimSpace(padded)))
		var folded string
		require.NoError(t, db.QueryRowContext(f.t.Context(), `SELECT release_channel_pair_key($1)`, padded).Scan(&folded))
		require.Equal(t, "bitmain", folded, "%q", padded)
	}
	var folded string
	require.NoError(t, db.QueryRowContext(f.t.Context(), `SELECT release_channel_pair_key(NULL)`).Scan(&folded))
	require.Equal(t, "", folded)

	upsert := `INSERT INTO release_channel_firmware (channel_id, manufacturer, model, firmware_checksum, assigned_by)
		VALUES ($1, $2, $3, 'sum', 1)
		ON CONFLICT (channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model))
		DO UPDATE SET assignment_generation = release_channel_firmware.assignment_generation + 1`
	f.exec(upsert, channel, "Bitmain", "S19")
	f.exec(upsert, channel, "bitmain", "s19")
	var manufacturer string
	var generation int64
	require.NoError(t, db.QueryRowContext(f.t.Context(), `SELECT manufacturer, assignment_generation FROM release_channel_firmware WHERE channel_id = $1`, channel).Scan(&manufacturer, &generation))
	require.Equal(t, "Bitmain", manufacturer, "the stored key keeps its original case")
	require.Equal(t, int64(1), generation)

	first := f.rollout(channel, "Bitmain", "S19")
	_, err := db.ExecContext(f.t.Context(), `INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation)
		VALUES ($1, $2, 'bitmain', 's19', 'sum', 'v1', 1)`, f.org, channel)
	require.Error(t, err, "a second active rollout for the same folded pair must be rejected")
	f.exec(`UPDATE firmware_rollout SET status = 'canceled' WHERE id = $1`, first)
	f.rollout(channel, "bitmain", "s19")
}

// The revision rule: a rollout starts at 1 whatever else its creating
// transaction does, then advances exactly once per transaction that changes
// the rollout row, its device rows or the provenance referencing it.
func TestReleaseChannelsSchemaBumpsRevisionOncePerTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	f := newReleaseChannelFixture(t, db)
	channel := f.channel("revision")
	a := f.device("a", 0)
	b := f.device("b", 0)

	var rollout int64
	f.inTransaction(func(tx *sql.Tx) {
		require.NoError(t, tx.QueryRowContext(f.t.Context(), `INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation)
			VALUES ($1, $2, 'Bitmain', 'S19', 'sum', 'v1', 1) RETURNING id`, f.org, channel).Scan(&rollout))
		_, err := tx.ExecContext(f.t.Context(), `INSERT INTO firmware_rollout_device (rollout_id, device_id) VALUES ($1, $2), ($1, $3)`, rollout, a.id, b.id)
		require.NoError(t, err)
		_, err = tx.ExecContext(f.t.Context(), `UPDATE firmware_rollout SET stage = 'batch' WHERE id = $1`, rollout)
		require.NoError(t, err)
	})
	require.Equal(t, int64(1), f.revision(rollout), "the creating transaction owns revision 1")

	f.inTransaction(func(tx *sql.Tx) {
		_, err := tx.ExecContext(f.t.Context(), `UPDATE firmware_rollout_device SET attempts = 1 WHERE rollout_id = $1`, rollout)
		require.NoError(t, err)
		_, err = tx.ExecContext(f.t.Context(), `UPDATE firmware_rollout SET current_batch = 1 WHERE id = $1`, rollout)
		require.NoError(t, err)
	})
	require.Equal(t, int64(2), f.revision(rollout), "two statements in one transaction are one change")

	f.exec(`INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version, rollout_id) VALUES ($1, 'sum', 'v1', $2)`, a.id, rollout)
	require.Equal(t, int64(3), f.revision(rollout), "recording provenance is a change")
	f.exec(`UPDATE device_firmware_deployment SET deployed_at = now() WHERE device_id = $1`, a.id)
	require.Equal(t, int64(4), f.revision(rollout))

	f.exec(`UPDATE firmware_rollout_device SET attempts = 2 WHERE rollout_id = $1 AND device_id = -1`, rollout)
	require.Equal(t, int64(4), f.revision(rollout), "a statement that changes no rows is not a change")

	later := f.rollout(channel, "Bitmain", "S21")
	f.exec(`UPDATE device_firmware_deployment SET rollout_id = $1 WHERE device_id = $2`, later, a.id)
	require.Equal(t, int64(5), f.revision(rollout), "provenance moving away changes the earlier rollout")
	require.Equal(t, int64(2), f.revision(later), "and the later one")
}

type releaseChannelFixture struct {
	t         *testing.T
	db        *sql.DB
	org       int64
	site      int64
	buildingA int64
	buildingB int64
	seq       int
}

type fixtureDevice struct {
	f          *releaseChannelFixture
	id         int64
	identifier string
}

func newReleaseChannelFixture(t *testing.T, db *sql.DB) *releaseChannelFixture {
	t.Helper()
	f := &releaseChannelFixture{t: t, db: db}
	f.org = insertMaintenanceTestOrg(t, db, "release-channels")
	f.site = insertMaintenanceTestSite(t, db, f.org, "Release Channel Site")
	f.buildingA = insertMaintenanceTestBuilding(t, db, f.org, f.site, "A")
	f.buildingB = insertMaintenanceTestBuilding(t, db, f.org, f.site, "B")
	return f
}

func (f *releaseChannelFixture) exec(query string, args ...any) {
	f.t.Helper()
	_, err := f.db.ExecContext(f.t.Context(), query, args...)
	require.NoError(f.t, err)
}

func (f *releaseChannelFixture) scanID(query string, args ...any) int64 {
	f.t.Helper()
	var id int64
	require.NoError(f.t, f.db.QueryRowContext(f.t.Context(), query, args...).Scan(&id))
	return id
}

func (f *releaseChannelFixture) inTransaction(fn func(tx *sql.Tx)) {
	f.t.Helper()
	tx, err := f.db.BeginTx(f.t.Context(), nil)
	require.NoError(f.t, err)
	fn(tx)
	require.NoError(f.t, tx.Commit())
}

// device creates a live device at the fixture's site, optionally in a
// building (0 for none). Identifiers may repeat once the earlier row is
// soft-deleted, as in the fleet after a miner is removed and paired again.
func (f *releaseChannelFixture) device(identifier string, buildingID int64) fixtureDevice {
	f.t.Helper()
	f.seq++
	discovered := f.scanID(`
		INSERT INTO discovered_device (org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
		VALUES ($1, $2, 'S19', 'Bitmain', 'proto', $3, '4028', 'http', TRUE)
		RETURNING id`, f.org, identifier, fmt.Sprintf("10.9.%d.%d", f.org%250, f.seq))
	var building sql.NullInt64
	if buildingID != 0 {
		building = sql.NullInt64{Int64: buildingID, Valid: true}
	}
	id := f.scanID(`
		INSERT INTO device (org_id, discovered_device_id, device_identifier, mac_address, site_id, building_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`, f.org, discovered, identifier, fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", f.org%256, f.seq>>16, f.seq>>8, f.seq), f.site, building)
	return fixtureDevice{f: f, id: id, identifier: identifier}
}

func (d fixtureDevice) softDelete() {
	d.f.exec(`UPDATE device SET deleted_at = now() WHERE id = $1`, d.id)
	d.f.exec(`UPDATE discovered_device SET deleted_at = now() WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)`, d.id)
}

func (f *releaseChannelFixture) rack(label string, buildingID int64) int64 {
	id := f.scanID(`INSERT INTO device_set (org_id, type, label) VALUES ($1, 'rack', $2) RETURNING id`, f.org, label)
	f.exec(`INSERT INTO device_set_rack (device_set_id, rows, columns, org_id, site_id, building_id) VALUES ($1, 4, 4, $2, $3, $4)`, id, f.org, f.site, buildingID)
	return id
}

func (f *releaseChannelFixture) group(label string) int64 {
	return f.scanID(`INSERT INTO device_set (org_id, type, label) VALUES ($1, 'group', $2) RETURNING id`, f.org, label)
}

func (f *releaseChannelFixture) addToSet(setID int64, setType string, d fixtureDevice) {
	f.exec(`INSERT INTO device_set_membership (org_id, device_set_id, device_set_type, device_id, device_identifier) VALUES ($1, $2, $3, $4, $5)`,
		f.org, setID, setType, d.id, d.identifier)
}

func (f *releaseChannelFixture) channel(name string) int64 {
	return f.scanID(`INSERT INTO release_channel (org_id, name, created_by) VALUES ($1, $2, 1) RETURNING id`, f.org, name)
}

func (f *releaseChannelFixture) target(channelID int64, targetType string, targetID int64) {
	f.exec(`INSERT INTO release_channel_target (channel_id, target_type, target_id) VALUES ($1, $2, $3)`, channelID, targetType, targetID)
}

func (f *releaseChannelFixture) minerTarget(channelID int64, identifier string) {
	f.exec(`INSERT INTO release_channel_target (channel_id, target_type, device_identifier) VALUES ($1, 'miner', $2)`, channelID, identifier)
}

func (f *releaseChannelFixture) rollout(channelID int64, manufacturer, model string) int64 {
	return f.scanID(`INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation)
		VALUES ($1, $2, $3, $4, 'sum', 'v1', 1) RETURNING id`, f.org, channelID, manufacturer, model)
}

func (f *releaseChannelFixture) revision(rolloutID int64) int64 {
	return f.scanID(`SELECT revision FROM firmware_rollout WHERE id = $1`, rolloutID)
}

func (f *releaseChannelFixture) memberships(d fixtureDevice) int64 {
	return f.scanID(`SELECT count(*) FROM release_channel_member WHERE device_id = $1`, d.id)
}

func (f *releaseChannelFixture) requireMember(d fixtureDevice, channelID int64, conflicted bool) {
	f.t.Helper()
	require.Equal(f.t, int64(1), f.memberships(d), "%s must belong to exactly one channel", d.identifier)
	var gotChannel int64
	var gotConflicted bool
	require.NoError(f.t, f.db.QueryRowContext(f.t.Context(), `SELECT channel_id, conflicted FROM release_channel_member WHERE device_id = $1`, d.id).Scan(&gotChannel, &gotConflicted))
	require.Equal(f.t, channelID, gotChannel, "%s belongs to the wrong channel", d.identifier)
	require.Equal(f.t, conflicted, gotConflicted, "%s conflicted flag", d.identifier)
}

func (f *releaseChannelFixture) requireNoMember(d fixtureDevice) {
	f.t.Helper()
	require.Zero(f.t, f.memberships(d), "%s must belong to no channel", d.identifier)
}

func (f *releaseChannelFixture) requireResolution(d fixtureDevice, want map[int64]string) {
	f.t.Helper()
	rows, err := f.db.QueryContext(f.t.Context(), `SELECT channel_id, resolution FROM release_channel_conflict WHERE device_id = $1`, d.id)
	require.NoError(f.t, err)
	defer rows.Close()
	got := map[int64]string{}
	for rows.Next() {
		var channelID int64
		var resolution string
		require.NoError(f.t, rows.Scan(&channelID, &resolution))
		got[channelID] = resolution
	}
	require.NoError(f.t, rows.Err())
	require.Equal(f.t, want, got, "%s conflict resolution", d.identifier)
}
