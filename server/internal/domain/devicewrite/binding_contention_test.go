package devicewrite_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// Uses committed admission and the production binding gate, retaining all SQL
// locks while the controlled physical call remains in progress.
func holdBindingDispatch(t *testing.T, database *testutil.DatabaseService, organizationID, deviceID int64) func() {
	t.Helper()
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginHost(t.Context(), organizationID, deviceID, 0)
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- service.WithBindingDispatch(attempt.Context(t.Context()), deviceID,
			func(sqlc.Querier) error { return nil },
			func(context.Context) error { close(entered); <-release; return nil })
	}()
	finish := sync.OnceFunc(func() {
		close(release)
		require.NoError(t, <-done)
		require.NoError(t, attempt.Finish(t.Context(), nil))
	})
	t.Cleanup(finish)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("physical dispatch did not start")
	}
	return finish
}

func TestBindingDispatchAllowsQueueAndLogForeignKeyInserts(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := createDeletionBindingFixture(t, database, user.OrganizationID)
	finish := holdBindingDispatch(t, database, user.OrganizationID, device.DatabaseID)
	q := sqlc.New(database.DB)
	batch := id.GenerateID()
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{
		Uuid: batch, Type: "Reboot", CreatedBy: user.DatabaseID, CreatedAt: time.Now(),
		Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	require.NoError(t, q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
		CommandBatchLogUuid: batch, CommandType: "Reboot", DeviceID: device.DatabaseID, Status: sqlc.QueueStatusEnumPENDING,
	}), "queue insertion must complete while physical dispatch is still active")
	require.NoError(t, q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
		Uuid: batch, DeviceID: device.DatabaseID, Status: sqlc.DeviceCommandStatusEnumSUCCESS, UpdatedAt: time.Now(),
	}), "result insertion must complete while physical dispatch is still active")
	rows, err := q.GetQueueMessagesByBatch(ctx, batch)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	finish()
}

func TestBindingDispatchProtectsMissingChildInsertions(t *testing.T) {
	for _, child := range []string{"credentials", "pairing", "route"} {
		t.Run(child, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			var nodeID int64
			require.NoError(t, database.DB.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
 VALUES($1,'missing-child-node',$2,$3,'CONFIRMED') RETURNING id`, user.OrganizationID, []byte("fixture-identity"), []byte("01234567890123456789012345678901")).Scan(&nodeID))
			finish := holdBindingDispatch(t, database, user.OrganizationID, device.DatabaseID)
			writer, err := database.DB.Conn(t.Context())
			require.NoError(t, err)
			defer writer.Close()
			var pid int
			require.NoError(t, writer.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
			done := make(chan error, 1)
			go func() {
				q := sqlc.New(writer)
				var err error
				switch child {
				case "credentials":
					err = q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: device.DatabaseID, UsernameEnc: "fixture-user", PasswordEnc: "fixture-password"})
				case "pairing":
					_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
				case "route":
					_, err = q.PairDeviceToFleetNode(t.Context(), sqlc.PairDeviceToFleetNodeParams{DeviceID: device.DatabaseID, OrgID: user.OrganizationID, FleetNodeID: nodeID})
				}
				done <- err
			}()
			waitForDeletionBindingLock(t, t.Context(), database.DB, pid)
			select {
			case err := <-done:
				t.Fatalf("missing child changed during dispatch: %v", err)
			default:
			}
			finish()
			require.NoError(t, <-done)
		})
	}
}

func TestDeletionRefusesActiveDispatchWithoutRetiringAnyBatchMember(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	// Lock an unchanged member before reaching the busy member.
	other := createDeletionBindingFixture(t, database, user.OrganizationID)
	busy := createDeletionBindingFixture(t, database, user.OrganizationID)
	finish := holdBindingDispatch(t, database, user.OrganizationID, busy.DatabaseID)
	result := make(chan error, 1)
	go func() {
		_, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{other.ID, busy.ID}, user.OrganizationID)
		result <- err
	}()
	select {
	case err := <-result:
		require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("retirement must not wait for physical dispatch")
	}
	var retired, permits, advanced, fences, credentials int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device WHERE org_id=$1 AND deleted_at IS NOT NULL", user.OrganizationID).Scan(&retired))
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_deletion_unpair_permit WHERE organization_id=$1", user.OrganizationID).Scan(&permits))
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_fence WHERE organization_id=$1 AND epoch<>0", user.OrganizationID).Scan(&advanced))
	require.Zero(t, retired)
	require.Zero(t, permits)
	require.Zero(t, advanced)
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_fence WHERE organization_id=$1", user.OrganizationID).Scan(&fences))
	require.Equal(t, 1, fences, "the unchanged miner's newly created fence must roll back")
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM miner_credentials mc JOIN device d ON d.id=mc.device_id WHERE d.org_id=$1", user.OrganizationID).Scan(&credentials))
	require.Equal(t, 2, credentials)
	finish()
	count, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{other.ID, busy.ID}, user.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
}

func TestDeletionBoundsConcurrentFenceCreation(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := createDeletionBindingFixture(t, database, user.OrganizationID)
	writer, err := database.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	require.NoError(t, sqlc.New(writer).EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))
	result := make(chan error, 1)
	go func() {
		_, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{device.ID}, user.OrganizationID)
		result <- err
	}()
	select {
	case err := <-result:
		require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("uncommitted fence creation must not strand identity locks")
	}
	require.NoError(t, writer.Rollback())
	count, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{device.ID}, user.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestBindingChildWritersRejectMissingDevice(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	var nodeID int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
 VALUES($1,'missing-parent-node',$2,$3,'CONFIRMED') RETURNING id`, user.OrganizationID, []byte("fixture-identity"), []byte("01234567890123456789012345678901")).Scan(&nodeID))
	q := sqlc.New(database.DB)
	require.Error(t, q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: -1, UsernameEnc: "fixture-user", PasswordEnc: "fixture-password"}), "missing credentials parent must not become a successful no-op")
	_, err = q.PairDeviceToFleetNode(t.Context(), sqlc.PairDeviceToFleetNodeParams{DeviceID: -1, OrgID: user.OrganizationID, FleetNodeID: nodeID})
	require.Error(t, err, "missing route parent must not become a successful no-op")
}
