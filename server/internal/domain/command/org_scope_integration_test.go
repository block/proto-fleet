package command

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func insertOtherOrgDevice(t *testing.T, conn *sql.DB) (int64, string) {
	t.Helper()
	ctx := t.Context()
	_, err := conn.ExecContext(ctx, `INSERT INTO organization (id, org_id, name) VALUES (2, 'test-org-2', 'Test Organization 2')`)
	require.NoError(t, err)
	q := sqlc.New(conn)
	identifier := "other-org-miner"
	discoveredID, err := q.UpsertDiscoveredDevice(ctx, sqlc.UpsertDiscoveredDeviceParams{OrgID: 2, DeviceIdentifier: "discovered-" + identifier, DriverName: "proto", IpAddress: "192.0.2.20", Port: "443", UrlScheme: "https", IsActive: true})
	require.NoError(t, err)
	deviceID, err := q.InsertDevice(ctx, sqlc.InsertDeviceParams{OrgID: 2, DiscoveredDeviceID: discoveredID, DeviceIdentifier: identifier, MacAddress: "00:11:22:33:44:99"})
	require.NoError(t, err)
	_, err = q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: deviceID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	return deviceID, identifier
}

func queuedMessagesForDevice(t *testing.T, conn *sql.DB, deviceID int64) int {
	t.Helper()
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message WHERE device_id = $1`, deviceID).Scan(&count))
	return count
}

func TestIncludeDevicesSelectorResolvesOnlyWithinSessionOrg(t *testing.T) {
	t.Run("mixed selector dispatches only the caller's device", func(t *testing.T) {
		svc, conn, identifier, finalizers := newTransactionalCommandFixture(t)
		foreignID, foreignIdentifier := insertOtherOrgDevice(t, conn)

		result, err := svc.Reboot(manualSessionCtx(1), includeSelector(foreignIdentifier, identifier))

		require.NoError(t, err)
		require.Equal(t, 1, result.DispatchedCount)
		require.Equal(t, []string{identifier}, result.DispatchedDeviceIdentifiers)
		require.Zero(t, queuedMessagesForDevice(t, conn, foreignID))
		require.Equal(t, 1, *finalizers)
	})

	t.Run("manual selector naming only another org's device is rejected", func(t *testing.T) {
		svc, conn, _, finalizers := newTransactionalCommandFixture(t)
		_, foreignIdentifier := insertOtherOrgDevice(t, conn)

		result, err := svc.Reboot(manualSessionCtx(1), includeSelector(foreignIdentifier))

		require.Nil(t, result)
		require.True(t, fleeterror.IsInvalidArgumentError(err), "foreign devices must look like unknown ones: %v", err)
		require.ErrorContains(t, err, "no devices matched selector")
		assertCommandDispatchRows(t, conn, 0)
		require.Zero(t, *finalizers)
	})

	t.Run("scheduler selector naming only another org's device is a no-op", func(t *testing.T) {
		svc, conn, _, finalizers := newTransactionalCommandFixture(t)
		_, foreignIdentifier := insertOtherOrgDevice(t, conn)

		result, err := svc.Reboot(schedulerSessionCtx(1), includeSelector(foreignIdentifier))

		require.NoError(t, err)
		require.Empty(t, result.BatchIdentifier)
		require.Zero(t, result.DispatchedCount)
		assertCommandDispatchRows(t, conn, 0)
		require.Zero(t, *finalizers)
	})

	t.Run("worker-name reapply rejects another org's device", func(t *testing.T) {
		svc, conn, _, finalizers := newTransactionalCommandFixture(t)
		_, foreignIdentifier := insertOtherOrgDevice(t, conn)

		batch, err := svc.ReapplyCurrentPoolsWithWorkerNames(manualSessionCtx(1), map[string]string{foreignIdentifier: "new-worker"})

		require.Empty(t, batch)
		require.True(t, fleeterror.IsNotFoundError(err), "foreign devices must look like unknown ones: %v", err)
		assertCommandDispatchRows(t, conn, 0)
		require.Zero(t, *finalizers)
	})
}
