package devicewrite_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDeletionRejectsIdentityChangedBeforeBindingLocks(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := createDeletionBindingFixture(t, database, user.OrganizationID)
	other := createDeletionBindingFixture(t, database, user.OrganizationID)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	rediscovery, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = rediscovery.Rollback() }()
	// Rediscovery changes the endpoint but has not committed. Retirement's
	// initial identity read still sees the old endpoint, then waits on its row.
	_, err = sqlc.New(rediscovery).UpsertDiscoveredDevice(ctx, deletionRediscovery(user.OrganizationID, device.ID))
	require.NoError(t, err)
	retirement, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = retirement.Rollback() }()
	var retirementPID int
	require.NoError(t, retirement.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&retirementPID))
	finished := make(chan error, 1)
	go func() {
		finished <- devicewrite.PrepareDeletion(ctx, sqlc.New(retirement), user.OrganizationID, []string{device.ID, other.ID})
	}()
	waitForDeletionBindingLock(t, ctx, database.DB, retirementPID)
	require.NoError(t, rediscovery.Commit())
	require.Error(t, <-finished, "retirement must reject a snapshot whose identity stripes changed while waiting")
	require.NoError(t, retirement.Rollback())
	var fences, permits, deleted int
	require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT count(*) FROM device_write_fence WHERE organization_id=$1", user.OrganizationID).Scan(&fences))
	require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT count(*) FROM device_deletion_unpair_permit WHERE organization_id=$1", user.OrganizationID).Scan(&permits))
	require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT count(*) FROM device WHERE org_id=$1 AND deleted_at IS NOT NULL", user.OrganizationID).Scan(&deleted))
	require.Zero(t, fences, "the entire batch must roll back, including the unchanged miner")
	require.Zero(t, permits)
	require.Zero(t, deleted)
}

func TestDeletionHoldsCoherentBindingUntilRetirementCommit(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := createDeletionBindingFixture(t, database, user.OrganizationID)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	retirement, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = retirement.Rollback() }()
	q := sqlc.New(retirement)
	require.NoError(t, devicewrite.PrepareDeletion(ctx, q, user.OrganizationID, []string{device.ID}))
	binding, err := q.GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, device.ID)
	require.NoError(t, err)
	// The permit captures the pre-retirement epoch, before preparation advances it.
	binding.WriteEpoch--
	hash, err := devicewrite.DirectBindingHash(binding)
	require.NoError(t, err)
	var savedHash string
	var coherent bool
	require.NoError(t, retirement.QueryRowContext(ctx, `SELECT binding_hash,identity_keys=device_write_identity_keys(device_id)
 FROM device_deletion_unpair_permit WHERE device_id=$1`, device.DatabaseID).Scan(&savedHash, &coherent))
	require.Equal(t, hash, savedHash)
	require.True(t, coherent, "the permit's hash and identity keys must describe the same locked binding")
	rediscovery, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = rediscovery.Rollback() }()
	var rediscoveryPID int
	require.NoError(t, rediscovery.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&rediscoveryPID))
	finished := make(chan error, 1)
	go func() {
		_, err := sqlc.New(rediscovery).UpsertDiscoveredDevice(ctx, deletionRediscovery(user.OrganizationID, device.ID))
		finished <- err
	}()
	waitForDeletionBindingLock(t, ctx, database.DB, rediscoveryPID)
	// The locks must survive preparation and protect the actual soft delete.
	_, err = q.SoftDeleteDevices(ctx, sqlc.SoftDeleteDevicesParams{OrgID: user.OrganizationID, DeviceIdentifiers: []string{device.ID}})
	require.NoError(t, err)
	require.NoError(t, q.SoftDeleteDiscoveredDevicesForDeletedDevices(ctx, sqlc.SoftDeleteDiscoveredDevicesForDeletedDevicesParams{OrgID: user.OrganizationID, DeviceIdentifiers: []string{device.ID}}))
	require.NoError(t, retirement.Commit())
	require.NoError(t, <-finished)
	require.NoError(t, rediscovery.Commit())
	var persistedHash string
	require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT binding_hash FROM device_deletion_unpair_permit WHERE device_id=$1", device.DatabaseID).Scan(&persistedHash))
	require.Equal(t, savedHash, persistedHash, "later rediscovery must not retarget the retired permit")
}

func createDeletionBindingFixture(t *testing.T, database *testutil.DatabaseService, organizationID int64) testutil.DeviceIdentification {
	t.Helper()
	device := database.CreateDevice(organizationID, "proto")
	_, err := database.DB.ExecContext(t.Context(), "UPDATE device SET serial_number=$2::text,mac_address=substr($2::text,1,12) WHERE id=$1", device.DatabaseID, device.ID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "INSERT INTO device_pairing(device_id,pairing_status,paired_at) VALUES($1,'PAIRED',statement_timestamp())", device.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "INSERT INTO miner_credentials(device_id,username_enc,password_enc) VALUES($1,'fixture-encrypted-user','fixture-encrypted-password')", device.DatabaseID)
	require.NoError(t, err)
	return device
}

func deletionRediscovery(organizationID int64, identifier string) sqlc.UpsertDiscoveredDeviceParams {
	return sqlc.UpsertDiscoveredDeviceParams{
		OrgID: organizationID, DeviceIdentifier: identifier,
		Model: sql.NullString{String: "TestMiner", Valid: true}, Manufacturer: sql.NullString{String: "TestCorp", Valid: true},
		IpAddress: "192.0.2.183", Port: "8080", UrlScheme: "https", IsActive: true, DriverName: "proto", DirectDiscovery: true,
	}
}

func waitForDeletionBindingLock(t *testing.T, ctx context.Context, database *sql.DB, pid int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE pid=$1 AND locktype='transactionid' AND NOT granted)`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "the competing transaction must wait on an actual binding row lock")
}
