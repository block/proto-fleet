package rollout

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Seed saved history without making it depend on today's channel membership,
// telemetry, or the rollout dispatch engine.
func seedMinerHistory(t *testing.T, f *fixture, channelID, deviceID int64, at time.Time, status string, phase persistedPhase) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `
		INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model,
		 firmware_checksum, firmware_version, assignment_generation, status,
		 cancel_reason, created_at, finished_at)
		VALUES ($1, $2, 'Proto', 'Rig', $3, '1.5.0', 1, $4,
		 CASE WHEN $4 = 'canceled' THEN 'superseded' ELSE '' END, $5,
		 CASE WHEN $4 = 'active' THEN NULL ELSE $5::timestamptz + INTERVAL '1 minute' END)
		RETURNING id`, f.orgID, channelID, checksum1, status, at).Scan(&id))
	_, err := f.conn.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_device (rollout_id, device_id, attempts,
		 last_sent_at, verified_at, halted_at, excluded_at, halt_reason, last_error, skip_note)
		VALUES ($1, $2, $3,
		 CASE WHEN $3 > 0 THEN $4::timestamptz END,
		 CASE WHEN $5 THEN $4::timestamptz END,
		 CASE WHEN $6 THEN $4::timestamptz END,
		 CASE WHEN $7 THEN $4::timestamptz END, $8,
		 CASE WHEN $8 = 'failed' THEN 'update timed out' ELSE '' END,
		 CASE WHEN $8 = 'skipped' THEN 'maintenance' ELSE '' END)`,
		id, deviceID, phase.Attempts, at, phase.Verified, phase.Halted, phase.Excluded, phase.HaltReason)
	require.NoError(t, err)
	return id
}

func TestMinerFirmwareHistoryAcrossRetainedChannels(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	prior := f.channel(t, Behavior{}, "miner-0")
	_, err := f.svc.UpdateChannel(ctx, f.orgID, prior.ID, ChannelSpec{Name: "Prior channel"})
	require.NoError(t, err)
	current, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Current channel", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}})
	require.NoError(t, err)
	at := time.Now().UTC().Truncate(time.Microsecond)
	cases := []struct {
		status string
		saved  persistedPhase
		phase  string
	}{
		{StatusCompletedWithFailures, persistedPhase{Verified: true, Attempts: 1}, PhaseDone},
		// Retry released the halt; a finished failed target still failed.
		{StatusCompletedWithFailures, persistedPhase{HaltReason: HaltReasonFailed, Attempts: 3}, PhaseFailed},
		{StatusCanceled, persistedPhase{Excluded: true, Verified: true, Halted: true, HaltReason: HaltReasonCanceled, Attempts: 1}, PhaseExcluded},
		{StatusCompleted, persistedPhase{Halted: true, HaltReason: HaltReasonSkipped}, PhaseSkipped},
		{StatusCanceled, persistedPhase{Halted: true, HaltReason: HaltReasonCanceled}, PhaseQueued},
		{StatusCanceled, persistedPhase{Halted: true, HaltReason: HaltReasonCanceled, Attempts: 3}, PhaseInProgress},
		{StatusActive, persistedPhase{Attempts: 2}, PhaseRetrying},
	}
	ids := make([]int64, len(cases))
	for i, tc := range cases {
		channelID := prior.ID
		if tc.status == StatusActive {
			channelID = current.ID
		}
		ids[i] = seedMinerHistory(t, f, channelID, f.deviceIDs["miner-0"], at.Add(time.Duration(i)*time.Minute), tc.status, tc.saved)
	}
	_, err = f.conn.ExecContext(ctx, `UPDATE firmware_rollout SET paused_at = now() WHERE id = $1`, ids[len(ids)-1])
	require.NoError(t, err)
	// Another miner's target in the same channel must never enter this list.
	seedMinerHistory(t, f, prior.ID, f.deviceIDs["miner-1"], at.Add(time.Hour), StatusCompleted, persistedPhase{Verified: true})
	// A later reported model/version and missing live health cannot rewrite history.
	_, err = f.conn.ExecContext(ctx, `UPDATE discovered_device SET model = 'Renamed', firmware_version = '9.0' WHERE device_identifier = 'miner-0'`)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `DELETE FROM device_status WHERE device_id = $1`, f.deviceIDs["miner-0"])
	require.NoError(t, err)
	entries, cursor, err := f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 0, "")
	require.NoError(t, err)
	require.Len(t, entries, len(cases))
	require.Empty(t, cursor)
	for i, entry := range entries {
		idx := len(cases) - 1 - i
		tc := cases[idx]
		assert.Equal(t, ids[idx], entry.RolloutID)
		assert.Equal(t, tc.phase, entry.Phase)
		assert.Equal(t, tc.status, entry.RolloutStatus)
		assert.Equal(t, tc.saved.Attempts, entry.Attempts)
		assert.Equal(t, "Proto", entry.Manufacturer)
		assert.Equal(t, "Rig", entry.Model)
		assert.Equal(t, "1.5.0", entry.FirmwareVersion)
		assert.Equal(t, checksum1, entry.FirmwareChecksum)
		assert.True(t, at.Add(time.Duration(idx)*time.Minute).Equal(entry.CreatedAt))
		assert.Equal(t, tc.status == StatusActive, entry.Paused)
		assert.Equal(t, tc.status != StatusActive, entry.FinishedAt != nil)
		assert.Equal(t, tc.saved.Attempts > 0, entry.LastSentAt != nil)
		assert.Equal(t, tc.saved.Verified, entry.VerifiedAt != nil)
		if tc.status == StatusActive {
			assert.Equal(t, "Current channel", entry.ChannelName)
		} else {
			assert.Equal(t, "Prior channel", entry.ChannelName)
		}
		if tc.status == StatusCanceled {
			assert.Equal(t, CancelReasonSuperseded, entry.CancelReason)
		}
		if tc.saved.HaltReason == HaltReasonFailed {
			assert.Equal(t, "update timed out", entry.LastError)
		}
		if tc.saved.HaltReason == HaltReasonSkipped {
			assert.Equal(t, "maintenance", entry.SkipNote)
		}
	}
	// Existing deletion policy still removes the deleted channel's history.
	require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, prior.ID))
	entries, _, err = f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 0, "")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, current.ID, entries[0].ChannelID)
}

func TestMinerFirmwareHistoryProgress(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, Behavior{}, "miner-0")
	started := f.apply(t, "fw-2")
	check := func(phase string, attempts int32) MinerFirmwareHistoryEntry {
		t.Helper()
		entries, _, err := f.svc.ListMinerFirmwareHistory(t.Context(), f.orgID, "miner-0", 1, "")
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, started.ID, entries[0].RolloutID)
		assert.Equal(t, phase, entries[0].Phase)
		assert.Equal(t, attempts, entries[0].Attempts)
		return entries[0]
	}
	check(PhaseQueued, 0)
	f.svc.EnforceTick(t.Context())
	check(PhaseInProgress, 1)
	f.backdateSends(t)
	f.svc.EnforceTick(t.Context())
	check(PhaseRetrying, 2)
	_, err := f.svc.PauseRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	assert.True(t, check(PhaseRetrying, 2).Paused)
	_, err = f.svc.ResumeRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	entry := check(PhaseDone, 2)
	assert.Equal(t, StatusCompleted, entry.RolloutStatus)
	assert.NotNil(t, entry.VerifiedAt)
	assert.NotNil(t, entry.FinishedAt)
}

func TestMinerFirmwareHistoryPagination(t *testing.T) {
	f := newFixture(t, 2)
	ch := f.channel(t, Behavior{})
	at := time.Now().UTC().Truncate(time.Microsecond)
	var ids []int64
	for i := range 103 {
		// Equal timestamps exercise the ID tie-breaker.
		ids = append(ids, seedMinerHistory(t, f, ch.ID, f.deviceIDs["miner-0"], at.Add(time.Duration(i/3)*time.Minute), StatusCompleted, persistedPhase{Verified: true}))
	}
	entries, _, err := f.svc.ListMinerFirmwareHistory(t.Context(), f.orgID, "miner-0", 0, "")
	require.NoError(t, err)
	require.Len(t, entries, int(DefaultPageSize))
	page, cursor, err := f.svc.ListMinerFirmwareHistory(t.Context(), f.orgID, "miner-0", 3, "")
	require.NoError(t, err)
	require.Len(t, page, 3)
	require.NotEmpty(t, cursor)
	require.NoError(t, protovalidate.Validate(&pb.ListMinerFirmwareHistoryRequest{DeviceIdentifier: "miner-0", Cursor: cursor}))
	// Inserts above the anchor cannot duplicate or shift remaining pages.
	newest := seedMinerHistory(t, f, ch.ID, f.deviceIDs["miner-0"], at.Add(24*time.Hour), StatusCompleted, persistedPhase{Verified: true})
	var got []int64
	for _, entry := range page {
		got = append(got, entry.RolloutID)
	}
	firstCursor := cursor
	for cursor != "" {
		page, cursor, err = f.svc.ListMinerFirmwareHistory(t.Context(), f.orgID, "miner-0", 7, cursor)
		require.NoError(t, err)
		for _, entry := range page {
			got = append(got, entry.RolloutID)
		}
	}
	require.Len(t, got, len(ids))
	for i, id := range got {
		assert.Equal(t, ids[len(ids)-1-i], id)
	}
	assert.NotContains(t, got, newest)
	page, _, err = f.svc.ListMinerFirmwareHistory(t.Context(), f.orgID, "miner-0", 1, "")
	require.NoError(t, err)
	assert.Equal(t, newest, page[0].RolloutID)
	_, _, err = f.svc.ListMinerFirmwareHistory(t.Context(), f.orgID, "miner-1", 1, firstCursor)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	parts := []string{minerHistoryCursorVersion, strconv.FormatInt(f.orgID, 10), strconv.FormatInt(f.deviceIDs["miner-0"], 10), strconv.FormatInt(at.UnixMicro(), 10), "1"}
	for i, value := range []string{"wrong-version", "99999", "99999", "not-time", "0"} {
		invalid := append([]string(nil), parts...)
		invalid[i] = value
		_, _, err = f.svc.ListMinerFirmwareHistory(t.Context(), f.orgID, "miner-0", 1, encodeCursor(invalid...))
		require.True(t, fleeterror.IsInvalidArgumentError(err), "cursor %v: %v", invalid, err)
	}
	for _, invalid := range []string{"%", encodeCursor("1"), strings.Repeat("a", 257)} {
		_, _, err = f.svc.ListMinerFirmwareHistory(t.Context(), f.orgID, "miner-0", 1, invalid)
		require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	}
}

func TestMinerFirmwareHistoryIdentityAndOrganization(t *testing.T) {
	f := newFixture(t, 1)
	ch := f.channel(t, Behavior{})
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	oldDeviceID := f.deviceIDs["miner-0"]
	for range 2 {
		seedMinerHistory(t, f, ch.ID, oldDeviceID, at, StatusCompleted, persistedPhase{Verified: true})
	}
	_, oldCursor, err := f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 1, "")
	require.NoError(t, err)
	require.NotEmpty(t, oldCursor)
	_, _, err = f.svc.ListMinerFirmwareHistory(ctx, f.orgID+1, "miner-0", 1, "")
	require.True(t, fleeterror.IsNotFoundError(err), "%v", err)
	var otherOrg int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ('other', 'Other') RETURNING id`).Scan(&otherOrg))
	other := &fixture{conn: f.conn, orgID: otherOrg, deviceIDs: map[string]int64{}}
	other.addMiner(t, "other-miner", "Rig")
	entries, cursor, err := f.svc.ListMinerFirmwareHistory(ctx, otherOrg, "other-miner", 1, "")
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.Empty(t, cursor)
	_, _, err = f.svc.ListMinerFirmwareHistory(ctx, otherOrg, "other-miner", 1, oldCursor)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	// Unpairing and pairing again keep the device record, so history and its
	// cursors survive both states.
	q := sqlc.New(f.conn)
	_, err = q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: oldDeviceID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	require.NoError(t, q.UpdateDevicePairingStatusByIdentifier(ctx, sqlc.UpdateDevicePairingStatusByIdentifierParams{DeviceIdentifier: "miner-0", PairingStatus: sqlc.PairingStatusEnumUNPAIRED}))
	for _, repaired := range []bool{false, true} {
		if repaired {
			_, err = q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: oldDeviceID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
			require.NoError(t, err)
		}
		entries, _, err = f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 0, "")
		require.NoError(t, err)
		assert.Len(t, entries, 2, "re-paired=%v", repaired)
		page, _, err := f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 1, oldCursor)
		require.NoError(t, err, "re-paired=%v", repaired)
		assert.Len(t, page, 1, "re-paired=%v", repaired)
	}
	_, err = f.conn.ExecContext(ctx, `UPDATE device SET deleted_at = now() WHERE id = $1`, oldDeviceID)
	require.NoError(t, err)
	_, _, err = f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 1, "")
	require.True(t, fleeterror.IsNotFoundError(err), "%v", err)
	var replacementID int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO device (org_id, device_identifier, mac_address, discovered_device_id)
	 SELECT org_id, device_identifier, 'replacement', discovered_device_id FROM device WHERE id = $1 RETURNING id`, oldDeviceID).Scan(&replacementID))
	entries, _, err = f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 1, "")
	require.NoError(t, err)
	assert.Empty(t, entries)
	_, _, err = f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 1, oldCursor)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	replacementRollout := seedMinerHistory(t, f, ch.ID, replacementID, at.Add(time.Hour), StatusCompleted, persistedPhase{Verified: true})
	entries, _, err = f.svc.ListMinerFirmwareHistory(ctx, f.orgID, "miner-0", 0, "")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, replacementRollout, entries[0].RolloutID)
}

func TestMinerFirmwareHistoryRequestValidation(t *testing.T) {
	t.Parallel()
	for _, request := range []*pb.ListMinerFirmwareHistoryRequest{
		{}, {DeviceIdentifier: "miner-0", PageSize: -1}, {DeviceIdentifier: "miner-0", PageSize: 1001},
		{DeviceIdentifier: strings.Repeat("x", 256)}, {DeviceIdentifier: "miner\x00"},
		{DeviceIdentifier: "miner-0", Cursor: strings.Repeat("x", 257)},
	} {
		t.Run(fmt.Sprintf("%v", request), func(t *testing.T) {
			t.Parallel()
			require.Error(t, protovalidate.Validate(request))
			// Domain validation also bounds callers that bypass the interceptor.
			_, _, err := (&Service{}).ListMinerFirmwareHistory(t.Context(), 1, request.DeviceIdentifier, request.PageSize, request.Cursor)
			require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
		})
	}
	require.NoError(t, protovalidate.Validate(&pb.ListMinerFirmwareHistoryRequest{DeviceIdentifier: strings.Repeat("😀", 255), PageSize: 1000}))
}
