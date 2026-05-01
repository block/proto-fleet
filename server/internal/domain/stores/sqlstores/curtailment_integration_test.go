package sqlstores_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_ListPreviewDevices_DatabaseIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(conn)
	orgID := insertCurtailmentPreviewOrg(t, conn)
	now := time.Now().UTC()
	previousHour := now.Add(-2 * time.Hour).Truncate(time.Hour)

	availableID := insertCurtailmentPreviewDevice(t, conn, orgID, "curt-available", "00:00:00:00:10:01", "192.0.2.11")
	activeID := insertCurtailmentPreviewDevice(t, conn, orgID, "curt-active", "00:00:00:00:10:02", "192.0.2.12")
	cooldownID := insertCurtailmentPreviewDevice(t, conn, orgID, "curt-cooldown", "00:00:00:00:10:03", "192.0.2.13")
	terminalNonTerminalTargetID := insertCurtailmentPreviewDevice(t, conn, orgID, "curt-terminal-target", "00:00:00:00:10:04", "192.0.2.14")

	insertCurtailmentPreviewMetric(t, conn, "curt-available", now.Add(-1*time.Minute), 2200, 100e12, nil)
	insertCurtailmentPreviewMetric(t, conn, "curt-active", now.Add(-1*time.Minute), 2100, 100e12, nil)
	insertCurtailmentPreviewMetric(t, conn, "curt-cooldown", now.Add(-1*time.Minute), 2000, 100e12, nil)
	insertCurtailmentPreviewMetric(t, conn, "curt-terminal-target", now.Add(-1*time.Minute), 1900, 100e12, nil)
	rawEfficiency := 30e-12
	insertCurtailmentPreviewMetric(t, conn, "curt-available", previousHour.Add(5*time.Minute), 2200, 100e12, &rawEfficiency)
	refreshCurtailmentPreviewHourlyMetrics(t, conn, previousHour, previousHour.Add(time.Hour))

	activeEventID := insertCurtailmentPreviewEvent(t, conn, orgID, "active", nil)
	insertCurtailmentPreviewTarget(t, conn, activeEventID, "curt-active", "confirmed", nil, nil)
	terminalEndedAt := now.Add(-2 * time.Minute)
	terminalEventID := insertCurtailmentPreviewEvent(t, conn, orgID, "completed", &terminalEndedAt)
	releasedAt := now.Add(-3 * time.Minute)
	confirmedAt := now.Add(-4 * time.Minute)
	insertCurtailmentPreviewTarget(t, conn, terminalEventID, "curt-cooldown", "resolved", &releasedAt, &confirmedAt)
	failedEventID := insertCurtailmentPreviewEvent(t, conn, orgID, "failed", nil)
	insertCurtailmentPreviewTarget(t, conn, failedEventID, "curt-terminal-target", "confirmed", nil, nil)

	devices, err := store.ListPreviewDevices(ctx, interfaces.CurtailmentPreviewDeviceParams{
		OrgID:             orgID,
		ScopeType:         interfaces.CurtailmentScopeWholeOrg,
		DeviceSetIDs:      []int64{},
		DeviceIdentifiers: []string{},
		CooldownSince:     now.Add(-10 * time.Minute),
	})

	require.NoError(t, err)
	byID := mapCurtailmentPreviewDevices(devices)
	require.Contains(t, byID, "curt-available")
	require.Contains(t, byID, "curt-active")
	require.Contains(t, byID, "curt-cooldown")
	require.Contains(t, byID, "curt-terminal-target")
	require.Equal(t, availableID, byID["curt-available"].DeviceID)
	require.Equal(t, activeID, byID["curt-active"].DeviceID)
	require.Equal(t, cooldownID, byID["curt-cooldown"].DeviceID)
	require.Equal(t, terminalNonTerminalTargetID, byID["curt-terminal-target"].DeviceID)
	require.NotNil(t, byID["curt-available"].EfficiencyJH)
	require.InDelta(t, 30, *byID["curt-available"].EfficiencyJH, 0.0001)
	require.False(t, byID["curt-available"].InActiveCurtailment)
	require.False(t, byID["curt-available"].InCooldown)
	require.True(t, byID["curt-active"].InActiveCurtailment)
	require.False(t, byID["curt-active"].InCooldown)
	require.False(t, byID["curt-cooldown"].InActiveCurtailment)
	require.True(t, byID["curt-cooldown"].InCooldown)
	require.False(t, byID["curt-terminal-target"].InActiveCurtailment)
	require.False(t, byID["curt-terminal-target"].InCooldown)
}

func TestSQLCurtailmentStore_CurtailmentEventMaintenanceConsistency_DatabaseIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	orgID := insertCurtailmentPreviewOrg(t, conn)

	cases := []struct {
		name                    string
		includeMaintenance      bool
		forceIncludeMaintenance bool
		wantErr                 bool
	}{
		{
			name:                    "default excludes maintenance",
			includeMaintenance:      false,
			forceIncludeMaintenance: false,
		},
		{
			name:                    "explicit maintenance override",
			includeMaintenance:      true,
			forceIncludeMaintenance: true,
		},
		{
			name:                    "include requires force",
			includeMaintenance:      true,
			forceIncludeMaintenance: false,
			wantErr:                 true,
		},
		{
			name:                    "force requires include",
			includeMaintenance:      false,
			forceIncludeMaintenance: true,
			wantErr:                 true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := insertCurtailmentPreviewEventWithMaintenance(
				t,
				conn,
				orgID,
				tc.includeMaintenance,
				tc.forceIncludeMaintenance,
			)

			if tc.wantErr {
				require.Error(t, err)
				require.ErrorContains(t, err, "chk_curtailment_event_maintenance_consistency")
				return
			}
			require.NoError(t, err)
		})
	}
}

func insertCurtailmentPreviewOrg(t *testing.T, conn *sql.DB) int64 {
	t.Helper()

	var orgID int64
	err := conn.QueryRowContext(t.Context(), `
		INSERT INTO organization (org_id, name, miner_auth_private_key)
		VALUES ('curtailment-preview-org', 'Curtailment Preview Org', 'test-key')
		RETURNING id
	`).Scan(&orgID)
	require.NoError(t, err)
	return orgID
}

func insertCurtailmentPreviewDevice(t *testing.T, conn *sql.DB, orgID int64, identifier string, macAddress string, ipAddress string) int64 {
	t.Helper()

	var discoveredID int64
	err := conn.QueryRowContext(t.Context(), `
		INSERT INTO discovered_device (
			org_id, device_identifier, manufacturer, model, driver_name, firmware_version, ip_address, port, url_scheme
		)
		VALUES ($1, $2, 'Bitmain', 'S19', 'antminer', '1.0.0', $3, '80', 'http')
		RETURNING id
	`, orgID, identifier, ipAddress).Scan(&discoveredID)
	require.NoError(t, err)

	var deviceID int64
	err = conn.QueryRowContext(t.Context(), `
		INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, identifier, macAddress, identifier+"-serial", orgID, discoveredID).Scan(&deviceID)
	require.NoError(t, err)

	_, err = conn.ExecContext(t.Context(), `
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES ($1, 'PAIRED', NOW())
	`, deviceID)
	require.NoError(t, err)

	_, err = conn.ExecContext(t.Context(), `
		INSERT INTO device_status (device_id, status)
		VALUES ($1, 'ACTIVE')
	`, deviceID)
	require.NoError(t, err)
	return deviceID
}

func insertCurtailmentPreviewMetric(t *testing.T, conn *sql.DB, identifier string, at time.Time, powerW float64, hashRateHS float64, efficiencyJH *float64) {
	t.Helper()

	_, err := conn.ExecContext(t.Context(), `
		INSERT INTO device_metrics (time, device_identifier, power_w, hash_rate_hs, efficiency_jh)
		VALUES ($1, $2, $3, $4, $5)
	`, at, identifier, powerW, hashRateHS, efficiencyJH)
	require.NoError(t, err)
}

func refreshCurtailmentPreviewHourlyMetrics(t *testing.T, conn *sql.DB, start time.Time, end time.Time) {
	t.Helper()

	_, err := conn.ExecContext(t.Context(), `
		CALL refresh_continuous_aggregate('device_metrics_hourly', $1::timestamptz, $2::timestamptz)
	`, start, end)
	require.NoError(t, err)
}

func insertCurtailmentPreviewEvent(t *testing.T, conn *sql.DB, orgID int64, state string, endedAt *time.Time) int64 {
	t.Helper()

	var eventID int64
	err := conn.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_event (
			event_uuid, org_id, state, mode, strategy, level, priority, loop_type, scope_type,
			scope_jsonb, mode_params_jsonb, restore_batch_size, restore_batch_interval_sec,
			source_actor_type, reason, ended_at
		)
		VALUES (
			gen_random_uuid(), $1, $2, 'fixed_kw', 'least_efficient_first', 'full', 'normal', 'open', 'whole_org',
			'{}'::jsonb, '{}'::jsonb, 1, 60, 'test', 'test event', $3
		)
		RETURNING id
	`, orgID, state, endedAt).Scan(&eventID)
	require.NoError(t, err)
	return eventID
}

func insertCurtailmentPreviewEventWithMaintenance(
	t *testing.T,
	conn *sql.DB,
	orgID int64,
	includeMaintenance bool,
	forceIncludeMaintenance bool,
) error {
	t.Helper()

	_, err := conn.ExecContext(t.Context(), `
		INSERT INTO curtailment_event (
			event_uuid, org_id, state, mode, strategy, level, priority, loop_type, scope_type,
			scope_jsonb, mode_params_jsonb, restore_batch_size, restore_batch_interval_sec,
			include_maintenance, force_include_maintenance, source_actor_type, reason
		)
		VALUES (
			gen_random_uuid(), $1, 'pending', 'fixed_kw', 'least_efficient_first', 'full', 'normal', 'open', 'whole_org',
			'{}'::jsonb, '{}'::jsonb, 1, 60, $2, $3, 'test', 'test maintenance consistency'
		)
	`, orgID, includeMaintenance, forceIncludeMaintenance)
	if err != nil {
		return fmt.Errorf("insert curtailment preview event with maintenance flags: %w", err)
	}
	return nil
}

func insertCurtailmentPreviewTarget(t *testing.T, conn *sql.DB, eventID int64, identifier string, state string, releasedAt *time.Time, confirmedAt *time.Time) {
	t.Helper()

	desiredState := "curtailed"
	if state == "resolved" || state == "restore_failed" || state == "released" {
		desiredState = "active"
	}
	_, err := conn.ExecContext(t.Context(), `
		INSERT INTO curtailment_target (
			curtailment_event_id, device_identifier, state, desired_state, released_at, confirmed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, eventID, identifier, state, desiredState, releasedAt, confirmedAt)
	require.NoError(t, err)
}

func mapCurtailmentPreviewDevices(devices []interfaces.CurtailmentPreviewDevice) map[string]interfaces.CurtailmentPreviewDevice {
	byID := make(map[string]interfaces.CurtailmentPreviewDevice, len(devices))
	for _, device := range devices {
		byID[device.DeviceIdentifier] = device
	}
	return byID
}
