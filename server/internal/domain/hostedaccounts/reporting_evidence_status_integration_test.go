package hostedaccounts_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedReportingEvidenceStatusNeverObservedAndReallocated(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	next := f.client(t, "Next customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	old := time.Now().UTC().Add(-time.Minute)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, old, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	// A new raw row with a pre-allocation source time also remains unauthorized.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, old.Add(time.Second), miner)
	require.NoError(t, err)
	var initialInterval int64
	var capture sql.NullTime
	var operation sql.NullString
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT interval_id,last_event_captured_at,last_event_operation FROM hosted_account_reporting_capture_status WHERE asset_id=$1 AND kind=5 AND source_id=1 AND metric=1`, miner).Scan(&initialInterval, &capture, &operation))
	require.False(t, capture.Valid, "never-observed allocation remains visible")
	require.False(t, operation.Valid)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&count))
	require.Zero(t, count, "unallocated and source-unauthorized raw observations never enter the hold")
	at := time.Now().UTC()
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, at, miner)
	require.NoError(t, err)
	var identifier string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, miner).Scan(&identifier))
	// Move and reassign through the existing ledger transitions. The captured
	// identity must continue to describe the allocation which admitted the sample.
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Moved','moved') RETURNING id`, f.org).Scan(&site))
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset SET client_id=$1,revision=revision+1 WHERE kind=5 AND asset_id=$2`, next.ID, miner)
	require.NoError(t, err)
	var currentInterval, currentClient int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT interval_id,client_id,last_event_captured_at FROM hosted_account_reporting_capture_status WHERE asset_id=$1 AND kind=5 AND source_id=1 AND metric=1`, miner).Scan(&currentInterval, &currentClient, &capture))
	require.NotEqual(t, initialInterval, currentInterval)
	require.Equal(t, next.ID, currentClient)
	require.False(t, capture.Valid, "reallocation does not inherit the previous capture")
	var savedOrg, savedClient, savedAsset, savedDevice int64
	var savedSite sql.NullInt64
	var savedIdentifier string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT organization_id,client_id,asset_id,device_id,site_id,device_identifier FROM hosted_account_reporting_allocation_snapshot WHERE interval_id=$1`, initialInterval).Scan(&savedOrg, &savedClient, &savedAsset, &savedDevice, &savedSite, &savedIdentifier))
	require.Equal(t, f.org, savedOrg)
	require.Equal(t, client.ID, savedClient)
	require.Equal(t, miner, savedAsset)
	require.Equal(t, miner, savedDevice)
	require.False(t, savedSite.Valid)
	require.Equal(t, identifier, savedIdentifier)
	// Corrections with the old source window still belong to the closed interval.
	_, err = f.db.ExecContext(t.Context(), `UPDATE device_metrics SET hash_rate_hs=200e12 WHERE time=$1`, at)
	require.NoError(t, err)
	var corrected, unseen int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence WHERE interval_id=$1 AND metric=1 AND operation='INSERT' AND value=200e12`, initialInterval).Scan(&corrected))
	require.Equal(t, 1, corrected)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_capture_status WHERE interval_id=$1 AND last_observation_event_id IS NULL`, currentInterval).Scan(&unseen))
	require.Equal(t, 4, unseen, "an old correction cannot make the new owner's streams look observed")
}

func TestHostedReportingEvidenceStatusDistinguishesSourceReceiptAndDeletion(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	board := f.moduleSelection(t, module).Boards[0].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
	source := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	receipt, stored := source.Add(time.Second), source
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs)
 SELECT $1,id,$2,$3,'active','',100e12 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board, stored, receipt)
	require.NoError(t, err)
	var captured, origin, collected, oldest, newest, epoch time.Time
	var eventID int64
	var operation string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT last_observation_event_id,last_event_operation,last_observation_captured_at,last_observation_origin_time,last_observation_collected_at,last_observation_source_oldest_at,last_observation_source_newest_at,hold_started_at FROM hosted_account_reporting_capture_status WHERE asset_id=$1 AND kind=7 AND source_id=5 AND metric=1`, board).Scan(&eventID, &operation, &captured, &origin, &collected, &oldest, &newest, &epoch))
	require.Equal(t, "INSERT", operation)
	require.Equal(t, stored, origin.UTC())
	require.Equal(t, receipt, collected.UTC())
	require.Equal(t, source, oldest.UTC())
	require.Equal(t, source, newest.UTC())
	require.False(t, captured.Before(epoch))
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM hashboard_metrics WHERE hashboard_id=$1`, board)
	require.NoError(t, err)
	var after time.Time
	var afterID, lastEventID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT last_event_operation,last_event_id,last_observation_event_id,last_observation_captured_at FROM hosted_account_reporting_capture_status WHERE asset_id=$1 AND kind=7 AND source_id=5 AND metric=1`, board).Scan(&operation, &lastEventID, &afterID, &after))
	require.Equal(t, "DELETE", operation, "normalized deletion remains explicit")
	require.Greater(t, lastEventID, eventID)
	require.Equal(t, eventID, afterID)
	require.Equal(t, captured, after, "deletion must not advance the observation timestamp")
	// Both status lookups must stay bounded by the interval/source/metric.
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SET LOCAL enable_seqscan=off`)
	require.NoError(t, err)
	for _, tc := range []struct{ query, index string }{
		{`EXPLAIN SELECT * FROM hosted_account_reporting_capture_status`, "idx_hosted_account_reporting_evidence_stream_event"},
		{`EXPLAIN SELECT * FROM hosted_account_reporting_capture_status`, "idx_hosted_account_reporting_evidence_stream_observation"},
	} {
		t.Run(tc.index, func(t *testing.T) {
			rows, err := tx.QueryContext(t.Context(), tc.query)
			require.NoError(t, err)
			defer rows.Close()
			var lines []string
			for rows.Next() {
				var line string
				require.NoError(t, rows.Scan(&line))
				lines = append(lines, line)
			}
			require.NoError(t, rows.Err())
			require.Contains(t, strings.Join(lines, "\n"), tc.index)
		})
	}
}
