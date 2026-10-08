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

type deletionQueryCounter struct{ calls int }

func (c *deletionQueryCounter) RetryQuery(_ context.Context, _ string, call func() error) error {
	c.calls++
	return call()
}

func TestFleetDeletionPreparationUsesBoundedSQLCalls(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	seed := database.CreateDevice(user.OrganizationID, "proto")
	_, err = database.DB.ExecContext(t.Context(), `UPDATE device SET serial_number='deletion-seed' WHERE id=$1`, seed.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `WITH discovered AS (
 INSERT INTO discovered_device(org_id,device_identifier,model,manufacturer,ip_address,port,url_scheme,is_active,driver_name)
 SELECT dd.org_id,md5('deletion-miner-' || n::text),dd.model,dd.manufacturer,
 '10.64.' || (n/256)::text || '.' || (n%256)::text,dd.port,dd.url_scheme,true,dd.driver_name
 FROM discovered_device dd JOIN device d ON d.discovered_device_id=dd.id
 CROSS JOIN generate_series(1,3999) n WHERE d.id=$1
 RETURNING id,org_id,device_identifier
 ) INSERT INTO device(org_id,discovered_device_id,device_identifier,mac_address,serial_number)
 SELECT org_id,id,device_identifier,substr(device_identifier,1,12),'serial-' || device_identifier FROM discovered`, seed.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status,paired_at) SELECT id,'PAIRED',statement_timestamp() FROM device WHERE org_id=$1`, user.OrganizationID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO miner_credentials(device_id,username_enc,password_enc) SELECT id,'fixture-encrypted-user','fixture-encrypted-password' FROM device WHERE org_id=$1`, user.OrganizationID)
	require.NoError(t, err)
	q := sqlc.New(database.DB)
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: "deletion-batch", Type: "Reboot", CreatedBy: user.DatabaseID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 4000, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true}})
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) SELECT 'deletion-batch',id,'Reboot','PENDING',0 FROM device WHERE org_id=$1`, user.OrganizationID)
	require.NoError(t, err)
	rows, err := database.DB.QueryContext(t.Context(), "SELECT device_identifier FROM device WHERE org_id=$1 ORDER BY id", user.OrganizationID)
	require.NoError(t, err)
	defer rows.Close()
	identifiers := []string{}
	for rows.Next() {
		var identifier string
		require.NoError(t, rows.Scan(&identifier))
		identifiers = append(identifiers, identifier)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Len(t, identifiers, 4000)
	var distinctIdentities int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(DISTINCT identity)
 FROM device d CROSS JOIN LATERAL unnest(device_write_identity_keys(d.id)) identity
 WHERE d.org_id=$1`, user.OrganizationID).Scan(&distinctIdentities))
	require.Equal(t, 12000, distinctIdentities, "each miner must have its own serial, MAC and endpoint")
	counter := &deletionQueryCounter{}
	started := time.Now()
	tx, err := database.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	// Counts actual queries against the real transaction, without mocking data.
	err = devicewrite.PrepareDeletion(t.Context(), sqlc.NewRetryingQuerier(sqlc.New(tx), counter), user.OrganizationID, identifiers)
	require.NoError(t, err)
	var locks int
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory'").Scan(&locks))
	require.LessOrEqual(t, locks, 256, "fleet retirement must not exhaust the shared advisory lock pool")
	require.NoError(t, tx.Commit())
	t.Logf("4000 deletion targets: %d SQL calls, elapsed %s", counter.calls, time.Since(started))
	require.LessOrEqual(t, counter.calls, 8, "retirement query count must not grow with miner count")
	var epochs, canceled, permits int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_fence WHERE organization_id=$1 AND epoch=1 AND state='host'", user.OrganizationID).Scan(&epochs))
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM queue_message WHERE command_batch_log_uuid='deletion-batch' AND status='FAILED'").Scan(&canceled))
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_deletion_unpair_permit WHERE organization_id=$1 AND epoch=1 AND attempt_id IS NULL", user.OrganizationID).Scan(&permits))
	require.Equal(t, 4000, epochs)
	require.Equal(t, 4000, canceled)
	require.Equal(t, 4000, permits)
	counts, err := q.GetBatchStatusAndDeviceCounts(t.Context(), "deletion-batch")
	require.NoError(t, err)
	require.Equal(t, int64(4000), counts.FailedDevices)
	require.Zero(t, counts.SuccessfulDevices)
	var results int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM command_on_device_log WHERE command_batch_log_id=(SELECT id FROM command_batch_log WHERE uuid='deletion-batch') AND status='FAILED'").Scan(&results))
	require.Equal(t, 4000, results, "bulk deletion must retain every canceled miner's terminal result")

}
