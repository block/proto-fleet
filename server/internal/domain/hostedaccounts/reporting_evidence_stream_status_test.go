package hostedaccounts_test

import (
	"database/sql"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedReportingCaptureStatusModelScansNullableStreams(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	readStatus := func() sqlc.HostedAccountReportingCaptureStatus {
		t.Helper()
		var status sqlc.HostedAccountReportingCaptureStatus
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT
 last_event_id,last_event_operation,last_event_captured_at,
 last_observation_event_id,last_observation_captured_at,last_observation_value,last_observation_complete,
 last_observation_origin_key,last_observation_origin_time,last_observation_collected_at,
 last_observation_source_oldest_at,last_observation_source_newest_at
 FROM hosted_account_reporting_capture_status WHERE asset_id=$1 AND kind=5 AND source_id=1 AND metric=1`, miner).Scan(
			&status.LastEventID, &status.LastEventOperation, &status.LastEventCapturedAt,
			&status.LastObservationEventID, &status.LastObservationCapturedAt, &status.LastObservationValue, &status.LastObservationComplete,
			&status.LastObservationOriginKey, &status.LastObservationOriginTime, &status.LastObservationCollectedAt,
			&status.LastObservationSourceOldestAt, &status.LastObservationSourceNewestAt,
		))
		return status
	}

	status := readStatus()
	require.Equal(t, sql.NullInt64{}, status.LastEventID)
	require.Equal(t, sql.NullString{}, status.LastEventOperation)
	require.Equal(t, sql.NullTime{}, status.LastEventCapturedAt)
	require.Equal(t, sql.NullInt64{}, status.LastObservationEventID)
	require.Equal(t, sql.NullTime{}, status.LastObservationCapturedAt)
	require.Equal(t, sql.NullFloat64{}, status.LastObservationValue)
	require.Equal(t, sql.NullBool{}, status.LastObservationComplete)
	require.Equal(t, sql.NullString{}, status.LastObservationOriginKey)
	require.Equal(t, sql.NullTime{}, status.LastObservationOriginTime)
	require.Equal(t, sql.NullTime{}, status.LastObservationCollectedAt)
	require.Equal(t, sql.NullTime{}, status.LastObservationSourceOldestAt)
	require.Equal(t, sql.NullTime{}, status.LastObservationSourceNewestAt)

	at := time.Now().UTC().Truncate(time.Microsecond)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,100e12,3000 FROM device WHERE id=$2`, at, miner)
	require.NoError(t, err)
	var eventID int64
	var capturedAt, originTime, collectedAt, oldestAt, newestAt time.Time
	var originKey string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT event_id,captured_at,origin_key,origin_time,origin_collected_at,source_oldest_at,source_newest_at
 FROM hosted_account_reporting_evidence WHERE metric=1`).Scan(&eventID, &capturedAt, &originKey, &originTime, &collectedAt, &oldestAt, &newestAt))
	status = readStatus()
	require.Equal(t, sql.NullInt64{Int64: eventID, Valid: true}, status.LastEventID)
	require.Equal(t, sql.NullString{String: "INSERT", Valid: true}, status.LastEventOperation)
	require.Equal(t, sql.NullTime{Time: capturedAt, Valid: true}, status.LastEventCapturedAt)
	require.Equal(t, sql.NullInt64{Int64: eventID, Valid: true}, status.LastObservationEventID)
	require.Equal(t, sql.NullTime{Time: capturedAt, Valid: true}, status.LastObservationCapturedAt)
	require.Equal(t, sql.NullFloat64{Float64: 100e12, Valid: true}, status.LastObservationValue)
	require.Equal(t, sql.NullBool{Bool: true, Valid: true}, status.LastObservationComplete)
	require.Equal(t, sql.NullString{String: originKey, Valid: true}, status.LastObservationOriginKey)
	require.Equal(t, sql.NullTime{Time: originTime, Valid: true}, status.LastObservationOriginTime)
	require.Equal(t, sql.NullTime{Time: collectedAt, Valid: true}, status.LastObservationCollectedAt)
	require.Equal(t, sql.NullTime{Time: oldestAt, Valid: true}, status.LastObservationSourceOldestAt)
	require.Equal(t, sql.NullTime{Time: newestAt, Valid: true}, status.LastObservationSourceNewestAt)
}

func TestHostedReportingEvidenceStatusKeepsStreamsIndependent(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, _ := f.miningModule(t)
	board := f.moduleSelection(t, module).Boards[0].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0), selected(7, board, 0)))
	var unseen int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_capture_status WHERE last_event_id IS NULL`).Scan(&unseen))
	require.Equal(t, 8, unseen, "each allocation exposes its three metric streams and state stream before capture")
	at := time.Now().UTC().Truncate(time.Microsecond)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,100e12,3000 FROM device WHERE id=$2`, at, miner)
	require.NoError(t, err)
	var hashEvent int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT last_observation_event_id FROM hosted_account_reporting_capture_status WHERE asset_id=$1 AND kind=5 AND source_id=1 AND metric=1`, miner).Scan(&hashEvent))
	stateAt := at.Add(time.Minute)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,site_id,device_identifier,state) SELECT $1,org_id,site_id,device_identifier,3 FROM device WHERE id=$2`, stateAt, miner)
	require.NoError(t, err)
	rows, err := f.db.QueryContext(t.Context(), `SELECT source_id,metric,last_observation_event_id,last_observation_origin_time,last_observation_value,last_observation_complete FROM hosted_account_reporting_capture_status WHERE asset_id=$1 AND kind=5 ORDER BY source_id,metric`, miner)
	require.NoError(t, err)
	defer rows.Close()
	var seen int
	for rows.Next() {
		var source, metric int
		var event int64
		var observed time.Time
		var value sql.NullFloat64
		var complete sql.NullBool
		require.NoError(t, rows.Scan(&source, &metric, &event, &observed, &value, &complete))
		seen++
		if source == 2 {
			require.Equal(t, 4, metric)
			require.Equal(t, stateAt, observed.UTC())
			require.Equal(t, sql.NullFloat64{Float64: 3, Valid: true}, value)
			require.False(t, complete.Valid)
			continue
		}
		require.Equal(t, 1, source)
		require.Equal(t, at, observed.UTC(), "new state must not advance any metric's source time")
		switch metric {
		case 1:
			require.Equal(t, hashEvent, event, "state must not replace the hashrate observation")
			require.Equal(t, sql.NullFloat64{Float64: 100e12, Valid: true}, value)
			require.Equal(t, sql.NullBool{Bool: true, Valid: true}, complete)
		case 2:
			require.Equal(t, sql.NullFloat64{Float64: 3000, Valid: true}, value)
			require.Equal(t, sql.NullBool{Bool: true, Valid: true}, complete)
		case 3:
			require.False(t, value.Valid, "missing temperature remains visible")
			require.False(t, complete.Valid)
		default:
			t.Fatalf("unexpected metric %d", metric)
		}
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 4, seen)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_capture_status WHERE kind=7 AND asset_id=$1 AND last_observation_event_id IS NULL`, board).Scan(&unseen))
	require.Equal(t, 4, unseen, "another asset's state cannot hide never-observed hashboard streams")
}
