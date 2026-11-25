package sqlstores_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
)

func TestReconcileRebootRequiredStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	seedReconcileTestOrg(t, conn)
	statusTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	tests := []struct {
		name             string
		status           sqlc.DeviceStatusEnum
		pairing          sqlc.PairingStatusEnum
		bootOffset       time.Duration
		polledStatus     minermodels.MinerStatus
		wantStatus       sqlc.DeviceStatusEnum
		wantUpdated      bool
		hasBoundary      bool
		boundaryOffset   time.Duration
		missingTimestamp bool
		deletedDevice    bool
		deletedDiscovery bool
		inactive         bool
		revoked          bool
	}{
		{name: "later boot clears reboot", bootOffset: time.Second, wantUpdated: true},
		{name: "earlier boot preserves reboot", bootOffset: -time.Second},
		{name: "equal boot preserves reboot"},
		{name: "missing status timestamp preserves reboot", bootOffset: time.Second, missingTimestamp: true},
		{name: "earlier installation boundary clears later status timestamp", hasBoundary: true, boundaryOffset: -2 * time.Minute, bootOffset: -time.Minute, wantUpdated: true},
		{name: "boot before installation boundary preserves reboot", hasBoundary: true, boundaryOffset: -30 * time.Second, bootOffset: -time.Minute},
		{name: "equal installation boundary preserves reboot", hasBoundary: true, boundaryOffset: -time.Minute, bootOffset: -time.Minute},
		{name: "installation boundary works without status timestamp", hasBoundary: true, boundaryOffset: -time.Minute, missingTimestamp: true, wantUpdated: true},
		{name: "default password device clears", pairing: sqlc.PairingStatusEnumDEFAULTPASSWORD, bootOffset: time.Second, wantUpdated: true},
		{name: "current inactive status is retained", bootOffset: time.Second, polledStatus: minermodels.MinerStatusInactive, wantStatus: sqlc.DeviceStatusEnumINACTIVE, wantUpdated: true},
		{name: "current error status is retained", bootOffset: time.Second, polledStatus: minermodels.MinerStatusError, wantStatus: sqlc.DeviceStatusEnumERROR, wantUpdated: true},
		{name: "updating is protected", status: sqlc.DeviceStatusEnumUPDATING, bootOffset: time.Second},
		{name: "ordinary status is unchanged", status: sqlc.DeviceStatusEnumINACTIVE, bootOffset: time.Second},
		{name: "unpaired is protected", pairing: sqlc.PairingStatusEnumUNPAIRED, bootOffset: time.Second},
		{name: "authentication needed is protected", pairing: sqlc.PairingStatusEnumAUTHENTICATIONNEEDED, bootOffset: time.Second},
		{name: "pending pairing is protected", pairing: sqlc.PairingStatusEnumPENDING, bootOffset: time.Second},
		{name: "failed pairing is protected", pairing: sqlc.PairingStatusEnumFAILED, bootOffset: time.Second},
		{name: "deleted device is protected", bootOffset: time.Second, deletedDevice: true},
		{name: "deleted discovery is protected", bootOffset: time.Second, deletedDiscovery: true},
		{name: "inactive discovery does not block live paired telemetry", bootOffset: time.Second, inactive: true, wantUpdated: true},
		{name: "revoked node marker is protected", bootOffset: time.Second, revoked: true},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceID := int64(9000 + i)
			identifier := minermodels.DeviceIdentifier(fmt.Sprintf("reboot-reconcile-%d", i))
			if tt.status == "" {
				tt.status = sqlc.DeviceStatusEnumREBOOTREQUIRED
			}
			if tt.pairing == "" {
				tt.pairing = sqlc.PairingStatusEnumPAIRED
			}
			if tt.polledStatus == minermodels.MinerStatusUnknown {
				tt.polledStatus = minermodels.MinerStatusActive
			}
			if tt.wantStatus == "" {
				tt.wantStatus = tt.status
				if tt.wantUpdated {
					tt.wantStatus = sqlc.DeviceStatusEnumACTIVE
				}
			}
			seedReconcileTestDevice(t, ctx, conn, deviceID, deviceID, identifier.String(), fmt.Sprintf("AA:BB:CC:EE:00:%02X", i), tt.pairing)
			details := "firmware installation requires a reboot"
			if tt.revoked {
				details = "fleet_node_revoked"
			}
			originalTime := sql.NullTime{Time: statusTime, Valid: !tt.missingTimestamp}
			originalBoundary := sql.NullTime{Time: statusTime.Add(tt.boundaryOffset), Valid: tt.hasBoundary}
			_, err := conn.ExecContext(ctx, `
				INSERT INTO device_status (device_id, status, status_timestamp, status_details, reboot_required_after)
				VALUES ($1, $2, $3, $4, $5)
			`, deviceID, tt.status, originalTime, details, originalBoundary)
			require.NoError(t, err)
			if tt.deletedDevice {
				_, err = conn.ExecContext(ctx, `UPDATE device SET deleted_at = NOW() WHERE id = $1`, deviceID)
				require.NoError(t, err)
			}
			if tt.deletedDiscovery {
				_, err = conn.ExecContext(ctx, `UPDATE discovered_device SET deleted_at = NOW() WHERE id = $1`, deviceID)
				require.NoError(t, err)
			}
			if tt.inactive {
				_, err = conn.ExecContext(ctx, `UPDATE discovered_device SET is_active = FALSE WHERE id = $1`, deviceID)
				require.NoError(t, err)
			}

			updated, err := store.ReconcileRebootRequiredStatus(ctx, identifier, statusTime.Add(tt.bootOffset), tt.polledStatus)
			require.NoError(t, err)
			require.Equal(t, tt.wantUpdated, updated)

			var actualStatus sqlc.DeviceStatusEnum
			var actualTime sql.NullTime
			var actualDetails sql.NullString
			var actualBoundary sql.NullTime
			require.NoError(t, conn.QueryRowContext(ctx, `
				SELECT status, status_timestamp, status_details, reboot_required_after FROM device_status WHERE device_id = $1
			`, deviceID).Scan(&actualStatus, &actualTime, &actualDetails, &actualBoundary))
			require.Equal(t, tt.wantStatus, actualStatus)
			if tt.wantUpdated {
				require.True(t, actualTime.Valid)
				require.WithinDuration(t, time.Now(), actualTime.Time, 5*time.Second)
				require.False(t, actualDetails.Valid, "completed reboot must clear pending status details")
				require.False(t, actualBoundary.Valid, "completed reboot must clear its installation boundary")
			} else {
				require.Equal(t, originalTime.Valid, actualTime.Valid)
				if originalTime.Valid {
					require.True(t, originalTime.Time.Equal(actualTime.Time), "ignored sample must preserve status timestamp")
				}
				require.Equal(t, sql.NullString{String: details, Valid: true}, actualDetails)
				require.Equal(t, originalBoundary.Valid, actualBoundary.Valid)
				if originalBoundary.Valid {
					require.True(t, originalBoundary.Time.Equal(actualBoundary.Time), "ignored sample must preserve installation boundary")
				}
			}
		})
	}
}

func TestReconcileRebootRequiredStatus_DeviceIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	seedReconcileTestOrg(t, conn)
	statusTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	for i, identifier := range []string{"reboot-target", "reboot-other-device"} {
		deviceID := int64(9100 + i)
		seedReconcileTestDevice(t, ctx, conn, deviceID, deviceID, identifier, fmt.Sprintf("AA:BB:CC:EE:01:%02X", i), sqlc.PairingStatusEnumPAIRED)
		_, err := conn.ExecContext(ctx, `
			INSERT INTO device_status (device_id, status, status_timestamp)
			VALUES ($1, 'REBOOT_REQUIRED', $2)
		`, deviceID, statusTime)
		require.NoError(t, err)
	}

	updated, err := store.ReconcileRebootRequiredStatus(ctx, "missing-device", statusTime.Add(time.Second), minermodels.MinerStatusActive)
	require.NoError(t, err)
	require.False(t, updated)
	updated, err = store.ReconcileRebootRequiredStatus(ctx, "reboot-target", statusTime.Add(time.Second), minermodels.MinerStatusActive)
	require.NoError(t, err)
	require.True(t, updated)

	q := sqlc.New(conn)
	status, err := q.GetDeviceStatus(ctx, 9100)
	require.NoError(t, err)
	require.Equal(t, sqlc.DeviceStatusEnumACTIVE, status)
	status, err = q.GetDeviceStatus(ctx, 9101)
	require.NoError(t, err)
	require.Equal(t, sqlc.DeviceStatusEnumREBOOTREQUIRED, status)
}

func TestMarkFirmwareRebootRequiredStatus_LatePoll(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	seedReconcileTestOrg(t, conn)
	const deviceID int64 = 9150
	const identifier minermodels.DeviceIdentifier = "reboot-late-poll"
	seedReconcileTestDevice(t, ctx, conn, deviceID, deviceID, identifier.String(), "AA:BB:CC:EE:01:50", sqlc.PairingStatusEnumPAIRED)
	require.NoError(t, store.UpsertDeviceStatus(ctx, identifier, minermodels.MinerStatusUpdating, ""))

	installationTime := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	require.NoError(t, store.MarkFirmwareRebootRequiredStatus(ctx, identifier, installationTime))
	var status sqlc.DeviceStatusEnum
	var statusTime time.Time
	var boundary time.Time
	require.NoError(t, conn.QueryRowContext(ctx, `
		SELECT status, status_timestamp, reboot_required_after FROM device_status WHERE device_id = $1
	`, deviceID).Scan(&status, &statusTime, &boundary))
	require.Equal(t, sqlc.DeviceStatusEnumREBOOTREQUIRED, status)
	require.True(t, boundary.Equal(installationTime))
	require.WithinDuration(t, time.Now(), statusTime, 5*time.Second)

	// A boot after installation but before Fleet observes the terminal firmware
	// state satisfies the installation even though the status marker is newer.
	updated, err := store.ReconcileRebootRequiredStatus(ctx, identifier, installationTime.Add(time.Second), minermodels.MinerStatusActive)
	require.NoError(t, err)
	require.True(t, updated)
}

func TestDeviceStatusWrites_ResetRebootBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	seedReconcileTestOrg(t, conn)
	installationTime := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	tests := []struct {
		name   string
		status minermodels.MinerStatus
	}{
		{name: "ordinary status", status: minermodels.MinerStatusActive},
		{name: "new firmware install", status: minermodels.MinerStatusUpdating},
		{name: "generic reboot marker", status: minermodels.MinerStatusRebootRequired},
		{name: "reboot marker without installation boundary", status: minermodels.MinerStatusRebootRequired},
		{name: "bulk ordinary status", status: minermodels.MinerStatusInactive},
		{name: "reaped firmware command", status: minermodels.MinerStatusActive},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceID := int64(9160 + i)
			identifier := minermodels.DeviceIdentifier(fmt.Sprintf("reboot-reset-%d", i))
			seedReconcileTestDevice(t, ctx, conn, deviceID, deviceID, identifier.String(), fmt.Sprintf("AA:BB:CC:EE:01:%02X", 60+i), sqlc.PairingStatusEnumPAIRED)
			require.NoError(t, store.MarkFirmwareRebootRequiredStatus(ctx, identifier, installationTime))
			switch tt.name {
			case "bulk ordinary status":
				// Verify an obsolete boundary on a non-firmware state cannot leak
				// into a subsequent status written by the bulk telemetry path.
				_, err := conn.ExecContext(ctx, `UPDATE device_status SET status = 'ACTIVE' WHERE device_id = $1`, deviceID)
				require.NoError(t, err)
				require.NoError(t, store.UpsertDeviceStatuses(ctx, []interfaces.DeviceStatusUpdate{{DeviceIdentifier: identifier, Status: tt.status}}))
			case "reaped firmware command":
				require.NoError(t, sqlc.New(conn).ResetReapedFirmwareStatuses(ctx, []int64{deviceID}))
			case "reboot marker without installation boundary":
				require.NoError(t, store.MarkFirmwareRebootRequiredStatus(ctx, identifier, time.Time{}))
			default:
				require.NoError(t, store.UpsertDeviceStatus(ctx, identifier, tt.status, ""))
			}
			var boundary sql.NullTime
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT reboot_required_after FROM device_status WHERE device_id = $1`, deviceID).Scan(&boundary))
			require.False(t, boundary.Valid, "unrelated status updates must discard the old installation boundary")
			if tt.status == minermodels.MinerStatusRebootRequired {
				updated, err := store.ReconcileRebootRequiredStatus(ctx, identifier, installationTime.Add(time.Second), minermodels.MinerStatusActive)
				require.NoError(t, err)
				require.False(t, updated, "a generic marker must fall back to its own timestamp")
			}
		})
	}
}

func TestRebootBoundaryMigration_PreservesPendingStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	seedReconcileTestOrg(t, conn)
	const deviceID int64 = 9180
	const identifier minermodels.DeviceIdentifier = "reboot-migration"
	seedReconcileTestDevice(t, ctx, conn, deviceID, deviceID, identifier.String(), "AA:BB:CC:EE:01:80", sqlc.PairingStatusEnumPAIRED)
	installationTime := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	require.NoError(t, store.MarkFirmwareRebootRequiredStatus(ctx, identifier, installationTime))
	var originalStatusTime time.Time
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT status_timestamp FROM device_status WHERE device_id = $1`, deviceID).Scan(&originalStatusTime))
	down, err := migrations.Current.ReadFile("current/001024_internal_device_reboot_required_after.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001024_internal_device_reboot_required_after.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	var hasBoundary bool
	require.NoError(t, conn.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'device_status' AND column_name = 'reboot_required_after')
	`).Scan(&hasBoundary))
	require.False(t, hasBoundary)
	var status sqlc.DeviceStatusEnum
	var statusTime time.Time
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT status, status_timestamp FROM device_status WHERE device_id = $1`, deviceID).Scan(&status, &statusTime))
	require.Equal(t, sqlc.DeviceStatusEnumREBOOTREQUIRED, status)
	require.True(t, originalStatusTime.Equal(statusTime), "rollback preserves the pending status and its legacy boundary")

	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	var boundary sql.NullTime
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT reboot_required_after FROM device_status WHERE device_id = $1`, deviceID).Scan(&boundary))
	require.False(t, boundary.Valid, "upgrade must not invent an installation boundary for existing rows")
	updated, err := store.ReconcileRebootRequiredStatus(ctx, identifier, originalStatusTime.Add(-time.Second), minermodels.MinerStatusActive)
	require.NoError(t, err)
	require.False(t, updated)
	updated, err = store.ReconcileRebootRequiredStatus(ctx, identifier, originalStatusTime.Add(time.Second), minermodels.MinerStatusActive)
	require.NoError(t, err)
	require.True(t, updated, "upgraded existing rows retain the conservative status timestamp fallback")
}

func TestReconcileRebootRequiredStatus_ConcurrentFirmwareInstallWins(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	seedReconcileTestOrg(t, conn)
	statusTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	for i, newStatus := range []sqlc.DeviceStatusEnum{sqlc.DeviceStatusEnumREBOOTREQUIRED, sqlc.DeviceStatusEnumUPDATING} {
		t.Run(string(newStatus), func(t *testing.T) {
			deviceID := int64(9200 + i)
			identifier := minermodels.DeviceIdentifier(fmt.Sprintf("reboot-concurrent-%d", i))
			seedReconcileTestDevice(t, ctx, conn, deviceID, deviceID, identifier.String(), fmt.Sprintf("AA:BB:CC:EE:02:%02X", i), sqlc.PairingStatusEnumPAIRED)
			_, err := conn.ExecContext(ctx, `
				INSERT INTO device_status (device_id, status, status_timestamp, reboot_required_after)
				VALUES ($1, 'REBOOT_REQUIRED', $2, $3)
			`, deviceID, statusTime.Add(time.Hour), statusTime)
			require.NoError(t, err)

			tx, err := conn.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			newStatusTime := statusTime.Add(2 * time.Hour)
			newBoundary := sql.NullTime{Time: statusTime.Add(time.Minute), Valid: newStatus == sqlc.DeviceStatusEnumREBOOTREQUIRED}
			_, err = tx.ExecContext(ctx, `
				UPDATE device_status SET status = $2, status_timestamp = $3, reboot_required_after = $4 WHERE device_id = $1
			`, deviceID, newStatus, newStatusTime, newBoundary)
			require.NoError(t, err)

			type result struct {
				updated bool
				err     error
			}
			results := make(chan result, 1)
			reconcileCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			go func() {
				updated, reconcileErr := store.ReconcileRebootRequiredStatus(reconcileCtx, identifier, statusTime.Add(time.Second), minermodels.MinerStatusActive)
				results <- result{updated: updated, err: reconcileErr}
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				err := conn.QueryRowContext(ctx, `
					SELECT EXISTS (
						SELECT 1 FROM pg_stat_activity
						WHERE datname = current_database() AND pid <> pg_backend_pid()
						  AND wait_event_type = 'Lock'
						  AND query LIKE '%ReconcileRebootRequiredStatus%'
					)
				`).Scan(&blocked)
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "reconciliation must wait on the newer status write")
			require.NoError(t, tx.Commit())

			select {
			case result := <-results:
				require.NoError(t, result.err)
				require.False(t, result.updated, "old reboot evidence must not clear a newer firmware installation")
			case <-reconcileCtx.Done():
				t.Fatal("timed out waiting for reboot reconciliation")
			}
			var actualStatus sqlc.DeviceStatusEnum
			var actualTime time.Time
			var actualBoundary sql.NullTime
			require.NoError(t, conn.QueryRowContext(ctx, `
				SELECT status, status_timestamp, reboot_required_after FROM device_status WHERE device_id = $1
			`, deviceID).Scan(&actualStatus, &actualTime, &actualBoundary))
			require.Equal(t, newStatus, actualStatus)
			require.True(t, newStatusTime.Equal(actualTime))
			require.Equal(t, newBoundary.Valid, actualBoundary.Valid)
			if newBoundary.Valid {
				require.True(t, newBoundary.Time.Equal(actualBoundary.Time))
			}
		})
	}
}

func TestUpsertDeviceStatuses_PreservesFirmwareStatuses(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	seedReconcileTestOrg(t, conn)
	statusTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	statuses := []sqlc.DeviceStatusEnum{sqlc.DeviceStatusEnumREBOOTREQUIRED, sqlc.DeviceStatusEnumUPDATING}
	updates := make([]interfaces.DeviceStatusUpdate, 0, len(statuses))
	for i, status := range statuses {
		deviceID := int64(9300 + i)
		identifier := minermodels.DeviceIdentifier(fmt.Sprintf("bulk-firmware-status-%d", i))
		seedReconcileTestDevice(t, ctx, conn, deviceID, deviceID, identifier.String(), fmt.Sprintf("AA:BB:CC:EE:03:%02X", i), sqlc.PairingStatusEnumPAIRED)
		_, err := conn.ExecContext(ctx, `
			INSERT INTO device_status (device_id, status, status_timestamp, reboot_required_after)
			VALUES ($1, $2, $3, $3)
		`, deviceID, status, statusTime)
		require.NoError(t, err)
		updates = append(updates, interfaces.DeviceStatusUpdate{DeviceIdentifier: identifier, Status: minermodels.MinerStatusActive})
	}

	require.NoError(t, store.UpsertDeviceStatuses(ctx, updates))
	for i, status := range statuses {
		actual, err := sqlc.New(conn).GetDeviceStatus(ctx, int64(9300+i))
		require.NoError(t, err)
		require.Equal(t, status, actual, "ordinary telemetry still needs boot evidence to clear firmware states")
		var boundary time.Time
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT reboot_required_after FROM device_status WHERE device_id = $1`, int64(9300+i)).Scan(&boundary))
		require.True(t, boundary.Equal(statusTime), "protected firmware status retains its installation boundary")
	}
}
