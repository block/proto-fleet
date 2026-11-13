package hostedaccounts_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedReportingCaptureStatusModelScansNullableStreams(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	readStatus := func() sqlc.HostedAccountReportingCaptureStatus {
		t.Helper()
		var status sqlc.HostedAccountReportingCaptureStatus
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT
 allocation_ended_at,closer_last_success_at,closer_lag_seconds,has_observation,
 last_observation_bucket,last_observation_revision,last_observation_captured_at,last_observation_source_newest_at,
 last_closed_bucket,last_closed_revision,last_closed_no_source_data,missing_hours,gap_hours,
 last_gap_from_bucket,last_gap_to_bucket,last_gap_reason,last_gap_recorded_at
 FROM hosted_account_reporting_capture_status WHERE interval_id=$1 AND source_id=1 AND metric=1`, interval).Scan(
			&status.AllocationEndedAt, &status.CloserLastSuccessAt, &status.CloserLagSeconds, &status.HasObservation,
			&status.LastObservationBucket, &status.LastObservationRevision, &status.LastObservationCapturedAt, &status.LastObservationSourceNewestAt,
			&status.LastClosedBucket, &status.LastClosedRevision, &status.LastClosedNoSourceData, &status.MissingHours, &status.GapHours,
			&status.LastGapFromBucket, &status.LastGapToBucket, &status.LastGapReason, &status.LastGapRecordedAt,
		))
		return status
	}

	status := readStatus()
	require.False(t, status.HasObservation)
	require.Equal(t, sql.NullTime{}, status.AllocationEndedAt)
	require.Equal(t, sql.NullTime{}, status.CloserLastSuccessAt)
	require.Equal(t, sql.NullTime{}, status.LastObservationBucket)
	require.Equal(t, sql.NullInt32{}, status.LastObservationRevision)
	require.Equal(t, sql.NullTime{}, status.LastObservationCapturedAt)
	require.Equal(t, sql.NullTime{}, status.LastObservationSourceNewestAt)
	require.Equal(t, sql.NullTime{}, status.LastClosedBucket)
	require.Equal(t, sql.NullInt32{}, status.LastClosedRevision)
	require.Equal(t, sql.NullBool{}, status.LastClosedNoSourceData)
	require.Equal(t, sql.NullTime{}, status.LastGapFromBucket)
	require.Equal(t, sql.NullTime{}, status.LastGapToBucket)
	require.Equal(t, sql.NullString{}, status.LastGapReason)
	require.Equal(t, sql.NullTime{}, status.LastGapRecordedAt)
	require.Zero(t, status.MissingHours)
	require.Zero(t, status.GapHours)

	at := hour.Add(10 * time.Minute)
	insertReportingHourSamples(t, f, interval, reportingHourSample{at: at, metric: 1, value: 100e12, complete: true, source: 1})
	require.Equal(t, 1, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 1)
	status = readStatus()
	require.True(t, status.HasObservation)
	require.True(t, status.CloserLastSuccessAt.Valid)
	require.True(t, status.LastObservationBucket.Valid && hour.Equal(status.LastObservationBucket.Time))
	require.Equal(t, sql.NullInt32{Int32: 1, Valid: true}, status.LastObservationRevision)
	require.True(t, status.LastObservationCapturedAt.Valid && rows[0].capturedAt.Equal(status.LastObservationCapturedAt.Time))
	require.True(t, status.LastObservationSourceNewestAt.Valid && at.Equal(status.LastObservationSourceNewestAt.Time))
	require.True(t, status.LastClosedBucket.Valid && hour.Equal(status.LastClosedBucket.Time))
	require.Equal(t, sql.NullInt32{Int32: 1, Valid: true}, status.LastClosedRevision)
	require.Equal(t, sql.NullBool{Bool: false, Valid: true}, status.LastClosedNoSourceData)
}

func TestHostedReportingEvidenceStatusKeepsStreamsIndependent(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	interval, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	board := reportingHourBoards(t, f, hour, 1)[0]
	var unseen int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_capture_status WHERE NOT has_observation`).Scan(&unseen))
	require.Equal(t, 8, unseen, "each allocation exposes its three metric streams and state stream before capture")
	insertReportingHourSamples(t, f, interval,
		reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1},
		reportingHourSample{at: hour.Add(10 * time.Minute), metric: 2, value: 3000, complete: true, source: 1},
		reportingHourSample{at: hour.Add(20 * time.Minute), metric: 3, value: nil, source: 1},
		reportingHourSample{at: hour.Add(30 * time.Minute), metric: 4, value: 3, source: 2},
	)
	require.Equal(t, 2, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))
	rows, err := f.db.QueryContext(t.Context(), `SELECT source_id,metric,has_observation,last_observation_source_newest_at
 FROM hosted_account_reporting_capture_status WHERE interval_id=$1 ORDER BY source_id,metric`, interval)
	require.NoError(t, err)
	defer rows.Close()
	type stream struct {
		source, metric int
		observed       bool
		newest         *time.Time
	}
	var streams []stream
	for rows.Next() {
		var s stream
		require.NoError(t, rows.Scan(&s.source, &s.metric, &s.observed, &s.newest))
		streams = append(streams, s)
	}
	require.NoError(t, rows.Err())
	require.Len(t, streams, 4)
	for _, s := range streams {
		switch s.metric {
		case 1, 2:
			require.True(t, s.observed)
			require.True(t, hour.Add(10*time.Minute).Equal(*s.newest), "state must not advance any metric's source time")
		case 3:
			require.False(t, s.observed, "a reported missing temperature is not an observation")
			require.Nil(t, s.newest)
		case 4:
			require.Equal(t, 2, s.source)
			require.True(t, s.observed)
			require.True(t, hour.Add(30*time.Minute).Equal(*s.newest))
		}
	}
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_capture_status WHERE interval_id=$1 AND NOT has_observation`, board).Scan(&unseen))
	require.Equal(t, 4, unseen, "another asset's observations cannot hide never-observed hashboard streams")
}

func TestHostReportingSetupCountsCaptureFromHourlyEvidence(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	q := sqlc.New(f.db)
	allowed, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Allowed", Slug: "allowed"})
	require.NoError(t, err)
	hidden, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Hidden", Slug: "hidden"})
	require.NoError(t, err)
	var hostID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&hostID))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID,
		hostedaccounts.ReportingAuthority{UserID: hostID, CanCreate: true, SiteIDs: []int64{allowed.ID}}, 0))
	observed, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	silent, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	other, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	ended, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	for interval, site := range map[int64]int64{observed: allowed.ID, silent: allowed.ID, other: hidden.ID, ended: allowed.ID} {
		_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET site_id=$1 WHERE id=$2`, site, interval)
		require.NoError(t, err)
	}
	at := hour.Add(10 * time.Minute)
	insertReportingHourSamples(t, f, observed, reportingHourSample{at: at, metric: 1, value: 100e12, complete: true, source: 1})
	require.Equal(t, 4, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET ended_at=now() WHERE id=$1`, ended)
	require.NoError(t, err)
	var captured time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT captured_at FROM hosted_account_reporting_hour WHERE interval_id=$1`, observed).Scan(&captured))

	workspace, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.Len(t, workspace.Capture, 4, "one group per expected miner stream")
	for _, stream := range workspace.Capture {
		require.Equal(t, 2, stream.Expected, "only current allocations at the actor's sites are expected")
		if stream.SourceID == 1 && stream.Metric == 1 {
			require.Equal(t, 1, stream.Missing, "the silent miner has no hashrate observation")
			require.NotNil(t, stream.OldestCapture)
			require.True(t, captured.Equal(*stream.OldestCapture))
			require.NotNil(t, stream.OldestSource)
			require.True(t, at.Equal(*stream.OldestSource))
			continue
		}
		require.Equal(t, 2, stream.Missing)
		require.Nil(t, stream.OldestCapture)
		require.Nil(t, stream.OldestSource)
	}
}
