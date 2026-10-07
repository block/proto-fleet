package devicewrite_test

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
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
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device(org_id,discovered_device_id,device_identifier,mac_address)
 SELECT org_id,discovered_device_id,md5('deletion-miner-' || n::text),'00-1A-2B-3C-4D-5E'
 FROM device CROSS JOIN generate_series(1,3999) n WHERE id=$1`, seed.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status,paired_at) SELECT id,'PAIRED',statement_timestamp() FROM device WHERE org_id=$1`, user.OrganizationID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO miner_credentials(device_id,username_enc,password_enc) SELECT id,'fixture-encrypted-user','fixture-encrypted-password' FROM device WHERE org_id=$1`, user.OrganizationID)
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
	counter := &deletionQueryCounter{}
	started := time.Now()
	err = db.WithTransactionNoResult(t.Context(), database.DB, func(q sqlc.Querier) error {
		// Counts actual queries against the real transaction, without mocking data.
		return devicewrite.PrepareDeletion(t.Context(), sqlc.NewRetryingQuerier(q, counter), user.OrganizationID, identifiers)
	})
	require.NoError(t, err)
	t.Logf("4000 deletion targets: %d SQL calls, elapsed %s", counter.calls, time.Since(started))
	require.LessOrEqual(t, counter.calls, 8, "retirement query count must not grow with miner count")
	var epochs, canceled, permits int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_fence WHERE organization_id=$1 AND epoch=1 AND state='host'", user.OrganizationID).Scan(&epochs))
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM queue_message WHERE command_batch_log_uuid='deletion-batch' AND status='FAILED'").Scan(&canceled))
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_deletion_unpair_permit WHERE organization_id=$1 AND epoch=1 AND attempt_id IS NULL", user.OrganizationID).Scan(&permits))
	require.Equal(t, 4000, epochs)
	require.Equal(t, 4000, canceled)
	require.Equal(t, 4000, permits)
}
