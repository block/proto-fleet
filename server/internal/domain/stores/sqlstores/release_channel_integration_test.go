package sqlstores_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// Scope resolution and target storage: every selector kind, identifiers that
// follow re-paired miners, and attribution to matching channels.
func TestReleaseChannelQueries_ScopeAndTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	q := f.q
	rack := f.rack("R1", f.building)
	group := f.group("G1")
	inRack := f.device("in-rack", "Bitmain", "S19", "v1")
	f.addToSet(rack, "rack", inRack)
	inGroup := f.device("in-group", "Bitmain", "S19", "v1")
	f.addToSet(group, "group", inGroup)
	byIdentifier := f.device("by-identifier", "Bitmain", "S19", "v1")
	deletedTwin := f.device("twin", "Bitmain", "S19", "v1")
	deletedTwin.softDelete()
	twin := f.device("twin", "Bitmain", "S19", "v1")

	other := f.channel("other")
	require.NoError(t, q.InsertReleaseChannelTargets(f.t.Context(), sqlc.InsertReleaseChannelTargetsParams{
		ChannelID: other, TargetTypes: []string{"site", "rack"}, TargetIds: []int64{f.site, rack},
	}))
	require.NoError(t, q.InsertReleaseChannelMinerTargets(f.t.Context(), sqlc.InsertReleaseChannelMinerTargetsParams{
		ChannelID: other, DeviceIdentifiers: []string{"twin"},
	}))

	targets, err := q.ListReleaseChannelTargets(f.t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, []sqlc.ListReleaseChannelTargetsRow{
		{ChannelID: other, TargetType: "miner", DeviceIdentifier: "twin"},
		{ChannelID: other, TargetType: "rack", TargetID: rack},
		{ChannelID: other, TargetType: "site", TargetID: f.site},
	}, targets)

	resolved, err := q.ResolveReleaseChannelScope(f.t.Context(), sqlc.ResolveReleaseChannelScopeParams{
		OrgID:             f.org,
		DeviceIdentifiers: []string{"twin", "by-identifier"},
		GroupIds:          []int64{group},
		RackIds:           []int64{rack},
		BuildingIds:       []int64{},
		SiteIds:           []int64{},
		ExcludeChannelID:  0,
	})
	require.NoError(t, err)
	owners := map[int64]int64{}
	for _, r := range resolved {
		owners[r.DeviceID] = r.OwnerChannelID
	}
	// Every live miner the selectors cover is already claimed by "other"; the
	// soft-deleted twin is not resolved even though it shares the identifier.
	require.Equal(t, map[int64]int64{inRack.id: other, inGroup.id: other, byIdentifier.id: other, twin.id: other}, owners)
	require.NotContains(t, owners, deletedTwin.id)
}

// Rollout target bookkeeping: initial targets carry a baseline and an order,
// late joiners neither, re-included leavers keep theirs; in_scope follows
// membership and the folded pair.
func TestReleaseChannelQueries_RolloutDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	q := f.q
	channel := f.channel("devices")
	require.NoError(t, q.InsertReleaseChannelTargets(f.t.Context(), sqlc.InsertReleaseChannelTargetsParams{
		ChannelID: channel, TargetTypes: []string{"site"}, TargetIds: []int64{f.site},
	}))
	first := f.device("first", "Bitmain", "S19", "v1")
	second := f.device("second", " bitmain ", "s19", "v1")
	late := f.device("late", "Bitmain", "S19", "v1")
	otherModel := f.device("other-model", "Bitmain", "S21", "v1")
	f.status(first, "ACTIVE")
	f.status(second, "OFFLINE")
	rollout := f.rollout(channel, "Bitmain", "S19")

	// The snapshot runs inside the creation transaction, some time after it
	// began; the baseline is stamped when the snapshot reads, not when the
	// transaction started.
	tx, err := f.db.BeginTx(f.t.Context(), nil)
	require.NoError(t, err)
	var txStart time.Time
	require.NoError(t, tx.QueryRowContext(f.t.Context(), `SELECT now()`).Scan(&txStart))
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, sqlc.New(tx).SnapshotFirmwareRolloutDevices(f.t.Context(), sqlc.SnapshotFirmwareRolloutDevicesParams{
		RolloutID: rollout, BatchIndex: sql.NullInt32{Int32: 0, Valid: true}, PositionOffset: 10,
		DeviceIds: []int64{second.id, first.id, otherModel.id},
	}))
	require.NoError(t, tx.Commit())
	require.NoError(t, q.AppendFirmwareRolloutDevices(f.t.Context(), sqlc.AppendFirmwareRolloutDevicesParams{RolloutID: rollout, DeviceIds: []int64{late.id}}))
	// Snapshotting again must not rewrite what is already there.
	require.NoError(t, q.SnapshotFirmwareRolloutDevices(f.t.Context(), sqlc.SnapshotFirmwareRolloutDevicesParams{
		RolloutID: rollout, BatchIndex: sql.NullInt32{Int32: 7, Valid: true}, PositionOffset: 99, DeviceIds: []int64{first.id, late.id},
	}))
	require.NoError(t, q.ExcludeFirmwareRolloutDevices(f.t.Context(), sqlc.ExcludeFirmwareRolloutDevicesParams{RolloutID: rollout, DeviceIds: []int64{first.id}}))
	require.NoError(t, q.ReincludeFirmwareRolloutDevices(f.t.Context(), sqlc.ReincludeFirmwareRolloutDevicesParams{RolloutID: rollout, DeviceIds: []int64{first.id}}))

	devices, err := q.ListFirmwareRolloutDevices(f.t.Context(), rollout)
	require.NoError(t, err)
	byIdentifier := map[string]sqlc.ListFirmwareRolloutDevicesRow{}
	order := make([]string, 0, len(devices))
	for _, d := range devices {
		byIdentifier[d.DeviceIdentifier] = d
		order = append(order, d.DeviceIdentifier)
	}
	require.Equal(t, []string{"second", "first", "other-model", "late"}, order, "position order, late joiners last")

	require.Equal(t, sql.NullInt32{Int32: 11, Valid: true}, byIdentifier["second"].Position)
	require.Equal(t, sql.NullInt32{Int32: 12, Valid: true}, byIdentifier["first"].Position)
	require.Equal(t, sql.NullString{String: "OFFLINE", Valid: true}, byIdentifier["second"].BaselineStatus)
	require.Equal(t, sql.NullString{String: "ACTIVE", Valid: true}, byIdentifier["first"].BaselineStatus)
	require.False(t, byIdentifier["first"].ExcludedAt.Valid, "re-included")
	require.Equal(t, sql.NullInt32{Int32: 0, Valid: true}, byIdentifier["first"].BatchIndex, "re-snapshot keeps the original batch")

	require.False(t, byIdentifier["late"].Position.Valid)
	require.False(t, byIdentifier["late"].BatchIndex.Valid)
	require.False(t, byIdentifier["late"].BaselineStatus.Valid, "late joiners carry no baseline")
	require.False(t, byIdentifier["late"].BaselineAt.Valid)

	// Errors are judged against the baseline: one that was open at the
	// snapshot and closed since is not new, one opened after it is, even
	// though the open count is unchanged.
	f.openError(first, "-1 hour")
	require.True(t, byIdentifier["first"].BaselineAt.Valid)
	require.True(t, byIdentifier["first"].BaselineAt.Time.After(txStart), "baseline_at is the snapshot's time, not the transaction's start")
	require.Equal(t, int32(0), byIdentifier["first"].BaselineOpenErrors.Int32, "the baseline was taken before this error")
	f.exec(`UPDATE errors SET closed_at = now() WHERE device_id = $1`, first.id)
	f.openError(first, "+1 minute")
	devices, err = q.ListFirmwareRolloutDevices(f.t.Context(), rollout)
	require.NoError(t, err)
	for _, d := range devices {
		if d.DeviceIdentifier == "first" {
			require.Equal(t, int32(1), d.OpenErrors)
			require.Equal(t, int32(1), d.ErrorsSinceBaseline)
		}
		if d.DeviceIdentifier == "late" {
			require.Equal(t, int32(0), d.ErrorsSinceBaseline, "no baseline, nothing to count against")
		}
	}

	require.Equal(t, sql.NullBool{Bool: true, Valid: true}, byIdentifier["second"].InScope, "whitespace and case fold to the rollout's pair")
	require.Equal(t, sql.NullBool{Bool: false, Valid: true}, byIdentifier["other-model"].InScope)

	// Convergence is latched, and latching it is a change to the rollout.
	verified := func(identifier string) bool {
		t.Helper()
		devices, err := q.ListFirmwareRolloutDevices(f.t.Context(), rollout)
		require.NoError(t, err)
		for _, d := range devices {
			if d.DeviceIdentifier == identifier {
				return d.VerifiedAt.Valid
			}
		}
		t.Fatalf("%s not in rollout", identifier)
		return false
	}
	revision := f.revision(rollout)
	require.NoError(t, q.MarkFirmwareRolloutDevicesVerified(f.t.Context(), sqlc.MarkFirmwareRolloutDevicesVerifiedParams{RolloutID: rollout, DeviceIds: []int64{first.id, second.id}}))
	require.True(t, verified("first"))
	require.False(t, verified("late"))
	require.Equal(t, revision+1, f.revision(rollout))

	// Leaving and returning, or drifting while in scope, reopens convergence.
	require.NoError(t, q.ExcludeFirmwareRolloutDevices(f.t.Context(), sqlc.ExcludeFirmwareRolloutDevicesParams{RolloutID: rollout, DeviceIds: []int64{first.id}}))
	require.NoError(t, q.ReincludeFirmwareRolloutDevices(f.t.Context(), sqlc.ReincludeFirmwareRolloutDevicesParams{RolloutID: rollout, DeviceIds: []int64{first.id}}))
	require.False(t, verified("first"), "a returning miner must verify again")
	require.NoError(t, q.UnverifyFirmwareRolloutDevices(f.t.Context(), sqlc.UnverifyFirmwareRolloutDevicesParams{RolloutID: rollout, DeviceIds: []int64{second.id}}))
	require.False(t, verified("second"))
}

// The enforcement predicates: mismatch by version, provenance or an
// outstanding command for another file; suppression after a halt in the
// latest rollout of the generation; provenance writes bump the revision once.
func TestReleaseChannelQueries_MismatchAndSuppression(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	q := f.q
	channel := f.channel("mismatch")
	require.NoError(t, q.InsertReleaseChannelTargets(f.t.Context(), sqlc.InsertReleaseChannelTargetsParams{
		ChannelID: channel, TargetTypes: []string{"site"}, TargetIds: []int64{f.site},
	}))
	f.device("stale", "Bitmain", "S19", "v1")
	current := f.device("current", "\tBitmain", "S19 ", "v2")
	queued := f.device("queued", "Bitmain", "S19", "v2")
	halted := f.device("halted", "Bitmain", "S19", "v1")
	rollout := f.rollout(channel, "Bitmain", "S19")

	mismatched := func(assignedFiles []string) []string {
		t.Helper()
		rows, err := q.ListReleaseChannelMismatchedMembers(f.t.Context(), sqlc.ListReleaseChannelMismatchedMembersParams{
			OrgID: f.org, ChannelID: channel, Manufacturer: "bitmain", Model: "s19",
			FirmwareVersion: "v2", FirmwareChecksum: "sum", AssignedFileIds: assignedFiles,
			RolloutID: rollout, AssignmentGeneration: 1,
		})
		require.NoError(t, err)
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.DeviceIdentifier)
		}
		return ids
	}

	// Nobody has provenance yet, so every member is mismatched.
	require.Equal(t, []string{"current", "halted", "queued", "stale"}, mismatched(nil))

	require.NoError(t, q.RecordFirmwareDeployment(f.t.Context(), sqlc.RecordFirmwareDeploymentParams{
		DeviceIds: []int64{current.id, queued.id, queued.id}, FirmwareChecksum: "sum", FirmwareVersion: "v2",
		RolloutID: sql.NullInt64{Int64: rollout, Valid: true},
	}))
	require.Equal(t, int64(2), f.revision(rollout), "recording provenance bumps the rollout once")
	require.Equal(t, []string{"halted", "stale"}, mismatched(nil))

	// A queued update for another file keeps the miner mismatched until it
	// drains; one for an assigned file does not.
	f.queueFirmwareUpdate(queued, "file-other")
	require.Equal(t, []string{"halted", "queued", "stale"}, mismatched([]string{"file-assigned"}))
	require.Equal(t, []string{"halted", "queued", "stale"}, mismatched(nil))
	require.Equal(t, []string{"halted", "stale"}, mismatched([]string{"file-other"}))

	// Halting in the generation's latest rollout suppresses; a newer rollout
	// of the generation that does not hold the miner lifts it.
	require.NoError(t, q.AppendFirmwareRolloutDevices(f.t.Context(), sqlc.AppendFirmwareRolloutDevicesParams{RolloutID: rollout, DeviceIds: []int64{halted.id}}))
	require.NoError(t, q.HaltFirmwareRolloutDevices(f.t.Context(), sqlc.HaltFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: []int64{halted.id}, HaltReason: "failed", LastError: "timeout",
	}))
	next := f.rolloutAfterCancel(rollout, channel, "Bitmain", "S19")
	mismatchedForNext := func() []string {
		t.Helper()
		rows, err := q.ListReleaseChannelMismatchedMembers(f.t.Context(), sqlc.ListReleaseChannelMismatchedMembersParams{
			OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19",
			FirmwareVersion: "v2", FirmwareChecksum: "sum", AssignedFileIds: []string{"file-other"},
			RolloutID: next, AssignmentGeneration: 1,
		})
		require.NoError(t, err)
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.DeviceIdentifier)
		}
		return ids
	}
	require.Equal(t, []string{"stale"}, mismatchedForNext(), "halted stays suppressed")
	suppressedIDs := func() []int64 {
		t.Helper()
		rows, err := q.ListReleaseChannelSuppressedMembers(f.t.Context(), sqlc.ListReleaseChannelSuppressedMembersParams{
			OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", AssignmentGeneration: 1,
		})
		require.NoError(t, err)
		ids := make([]int64, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.DeviceID)
		}
		return ids
	}
	require.Equal(t, []int64{halted.id}, suppressedIDs())

	// Retrying the active rollout re-queues the generation's suppressed
	// miners into it although the halt was recorded by the earlier rollout,
	// which keeps its history.
	requeued, err := q.RequeueFirmwareRolloutDevices(f.t.Context(), sqlc.RequeueFirmwareRolloutDevicesParams{RolloutID: next, DeviceIds: suppressedIDs()})
	require.NoError(t, err)
	require.Equal(t, []int64{halted.id}, requeued)
	require.Empty(t, suppressedIDs(), "the newest rollout holding the miner is no longer a halt")
	require.Equal(t, []string{"stale"}, mismatchedForNext(), "halted is now a target of the active rollout")
	var stillHaltedInOld bool
	require.NoError(t, f.db.QueryRowContext(f.t.Context(), `SELECT halted_at IS NOT NULL FROM firmware_rollout_device WHERE rollout_id = $1 AND device_id = $2`, rollout, halted.id).Scan(&stillHaltedInOld))
	require.True(t, stillHaltedInOld)

	// A halted miner that left the scope is not the retry's to reset; it comes
	// back still halted and is picked up by the next retry.
	require.NoError(t, q.HaltFirmwareRolloutDevices(f.t.Context(), sqlc.HaltFirmwareRolloutDevicesParams{RolloutID: next, DeviceIds: []int64{halted.id}, HaltReason: "failed"}))
	require.NoError(t, q.ExcludeFirmwareRolloutDevices(f.t.Context(), sqlc.ExcludeFirmwareRolloutDevicesParams{RolloutID: next, DeviceIds: []int64{halted.id}}))
	requeued, err = q.RequeueFirmwareRolloutDevices(f.t.Context(), sqlc.RequeueFirmwareRolloutDevicesParams{RolloutID: next, DeviceIds: []int64{halted.id}})
	require.NoError(t, err)
	require.Empty(t, requeued)
	require.NoError(t, q.ReincludeFirmwareRolloutDevices(f.t.Context(), sqlc.ReincludeFirmwareRolloutDevicesParams{RolloutID: next, DeviceIds: []int64{halted.id}}))
	require.Equal(t, []int64{halted.id}, suppressedIDs(), "back in scope, still halted, still suppressed")
	requeued, err = q.RequeueFirmwareRolloutDevices(f.t.Context(), sqlc.RequeueFirmwareRolloutDevicesParams{RolloutID: next, DeviceIds: suppressedIDs()})
	require.NoError(t, err)
	require.Equal(t, []int64{halted.id}, requeued)
	require.Empty(t, suppressedIDs())
}

type releaseChannelQueryFixture struct {
	t        *testing.T
	db       *sql.DB
	q        sqlc.Querier
	org      int64
	site     int64
	building int64
	seq      int
}

type queryFixtureDevice struct {
	f          *releaseChannelQueryFixture
	id         int64
	identifier string
}

func newReleaseChannelQueryFixture(t *testing.T) *releaseChannelQueryFixture {
	t.Helper()
	db := testutil.GetTestDB(t)
	f := &releaseChannelQueryFixture{t: t, db: db}
	f.q = sqlstores.NewSQLReleaseChannelStore(db).Queries(f.t.Context())
	f.org = f.scanID(`INSERT INTO organization (org_id, name) VALUES ('release-channel-queries', 'Release Channel Queries') RETURNING id`)
	f.site = f.scanID(`INSERT INTO site (org_id, name, slug) VALUES ($1, 'Site', 'release-channel-queries') RETURNING id`, f.org)
	f.building = f.scanID(`INSERT INTO building (org_id, site_id, name) VALUES ($1, $2, 'Building') RETURNING id`, f.org, f.site)
	return f
}

func (f *releaseChannelQueryFixture) exec(query string, args ...any) {
	f.t.Helper()
	_, err := f.db.ExecContext(f.t.Context(), query, args...)
	require.NoError(f.t, err)
}

func (f *releaseChannelQueryFixture) scanID(query string, args ...any) int64 {
	f.t.Helper()
	var id int64
	require.NoError(f.t, f.db.QueryRowContext(f.t.Context(), query, args...).Scan(&id))
	return id
}

func (f *releaseChannelQueryFixture) device(identifier, manufacturer, model, version string) queryFixtureDevice {
	f.t.Helper()
	f.seq++
	discovered := f.scanID(`
		INSERT INTO discovered_device (org_id, device_identifier, manufacturer, model, firmware_version, driver_name, ip_address, port, url_scheme, is_active)
		VALUES ($1, $2, $3, $4, $5, 'proto', $6, '4028', 'http', TRUE)
		RETURNING id`, f.org, identifier, manufacturer, model, version, fmt.Sprintf("10.8.0.%d", f.seq))
	id := f.scanID(`
		INSERT INTO device (org_id, discovered_device_id, device_identifier, mac_address, site_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`, f.org, discovered, identifier, fmt.Sprintf("02:00:00:00:%02x:%02x", f.seq>>8, f.seq), f.site)
	return queryFixtureDevice{f: f, id: id, identifier: identifier}
}

func (d queryFixtureDevice) softDelete() {
	d.f.exec(`UPDATE device SET deleted_at = now() WHERE id = $1`, d.id)
	d.f.exec(`UPDATE discovered_device SET deleted_at = now() WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)`, d.id)
}

func (f *releaseChannelQueryFixture) status(d queryFixtureDevice, status string) {
	f.exec(`INSERT INTO device_status (device_id, status) VALUES ($1, $2::device_status_enum)`, d.id, status)
}

func (f *releaseChannelQueryFixture) rack(label string, buildingID int64) int64 {
	id := f.scanID(`INSERT INTO device_set (org_id, type, label) VALUES ($1, 'rack', $2) RETURNING id`, f.org, label)
	f.exec(`INSERT INTO device_set_rack (device_set_id, rows, columns, org_id, site_id, building_id) VALUES ($1, 4, 4, $2, $3, $4)`, id, f.org, f.site, buildingID)
	return id
}

func (f *releaseChannelQueryFixture) group(label string) int64 {
	return f.scanID(`INSERT INTO device_set (org_id, type, label) VALUES ($1, 'group', $2) RETURNING id`, f.org, label)
}

func (f *releaseChannelQueryFixture) addToSet(setID int64, setType string, d queryFixtureDevice) {
	f.exec(`INSERT INTO device_set_membership (org_id, device_set_id, device_set_type, device_id, device_identifier) VALUES ($1, $2, $3, $4, $5)`,
		f.org, setID, setType, d.id, d.identifier)
}

func (f *releaseChannelQueryFixture) channel(name string) int64 {
	return f.scanID(`INSERT INTO release_channel (org_id, name, created_by) VALUES ($1, $2, 1) RETURNING id`, f.org, name)
}

func (f *releaseChannelQueryFixture) rollout(channelID int64, manufacturer, model string) int64 {
	return f.scanID(`INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation)
		VALUES ($1, $2, $3, $4, 'sum', 'v2', 1) RETURNING id`, f.org, channelID, manufacturer, model)
}

// rolloutAfterCancel cancels a rollout and starts the next one of the same
// pair and generation, strictly later so the suppression view ranks it newest.
func (f *releaseChannelQueryFixture) rolloutAfterCancel(rolloutID, channelID int64, manufacturer, model string) int64 {
	f.exec(`UPDATE firmware_rollout SET status = 'canceled', finished_at = now() WHERE id = $1`, rolloutID)
	f.exec(`UPDATE firmware_rollout SET created_at = created_at - INTERVAL '1 minute' WHERE id = $1`, rolloutID)
	return f.rollout(channelID, manufacturer, model)
}

func (f *releaseChannelQueryFixture) revision(rolloutID int64) int64 {
	return f.scanID(`SELECT revision FROM firmware_rollout WHERE id = $1`, rolloutID)
}

// openError opens an error on the device first seen at now() + offset.
func (f *releaseChannelQueryFixture) openError(d queryFixtureDevice, offset string) {
	f.seq++
	f.exec(`INSERT INTO errors (error_id, org_id, miner_error, severity, summary, first_seen_at, last_seen_at, device_id)
		VALUES ($1, $2, 1, 2, 'test error', now() + $3::interval, now() + $3::interval, $4)`,
		fmt.Sprintf("err-%d", f.seq), f.org, offset, d.id)
}

func (f *releaseChannelQueryFixture) queueFirmwareUpdate(d queryFixtureDevice, fileID string) {
	f.exec(`INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload)
		VALUES ('00000000-0000-0000-0000-000000000001', $1, 'FirmwareUpdate', 'PENDING', 0, jsonb_build_object('firmware_file_id', $2::text))`, d.id, fileID)
}
