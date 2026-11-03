package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedReportingEvidencePauseResumePreservesHold(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	at := time.Now().UTC().Truncate(24 * time.Hour).Add(-3 * 24 * time.Hour)
	historyEpoch(t, f, at.Add(-time.Minute))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, at.Add(-time.Minute), miner)
	require.NoError(t, err)
	var epoch time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_reporting_evidence_epoch`).Scan(&epoch))
	writeMetric := func(day int, value float64) {
		t.Helper()
		_, writeErr := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,$2 FROM device WHERE id=$3`, at.Add(time.Duration(day)*24*time.Hour), value, miner)
		require.NoError(t, writeErr)
	}
	setCapture := func(statement string) {
		t.Helper()
		tx, changeErr := f.db.BeginTx(t.Context(), nil)
		require.NoError(t, changeErr)
		defer tx.Rollback()
		_, changeErr = tx.ExecContext(t.Context(), `SET LOCAL lock_timeout = '5s';
 LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hashboard_metrics,hashboard_diagnostic_observation,
 hosted_account_history_sample IN SHARE ROW EXCLUSIVE MODE`)
		require.NoError(t, changeErr)
		_, changeErr = tx.ExecContext(t.Context(), statement)
		require.NoError(t, changeErr)
		require.NoError(t, tx.Commit())
	}
	writeMetric(0, 100e12)
	setCapture(`DROP TRIGGER record_hosted_reporting_evidence ON hosted_account_history_sample`)
	writeMetric(1, 200e12)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence WHERE metric=1`).Scan(&count))
	require.Equal(t, 1, count, "pause keeps existing evidence without capturing new chart samples")
	setCapture(`CREATE TRIGGER record_hosted_reporting_evidence
 AFTER INSERT OR UPDATE OR DELETE ON hosted_account_history_sample
 FOR EACH ROW EXECUTE FUNCTION record_hosted_reporting_evidence()`)
	writeMetric(2, 300e12)
	var minValue, maxValue float64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),min(value),max(value) FROM hosted_account_reporting_evidence WHERE metric=1`).Scan(&count, &minValue, &maxValue))
	require.Equal(t, 2, count, "resume captures new chunks without backfilling the pause")
	require.Equal(t, 100e12, minValue)
	require.Equal(t, 300e12, maxValue)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample WHERE metric=1`).Scan(&count))
	require.Equal(t, 3, count, "chart normalization continues during the pause")
	var currentEpoch time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_reporting_evidence_epoch`).Scan(&currentEpoch))
	require.Equal(t, epoch, currentEpoch)
	var measuredAt time.Time
	var evidenceBytes int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT clock_timestamp(),pg_total_relation_size('hosted_account_reporting_evidence')+pg_total_relation_size('hosted_account_reporting_allocation_snapshot')`).Scan(&measuredAt, &evidenceBytes))
	require.Positive(t, evidenceBytes, "the storage monitor includes the retained ledger and allocation snapshots")
}
