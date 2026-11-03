package timescaledb

import (
	"fmt"
	"slices"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func (r *containerRollupRig) representativeSnapshot(offset time.Duration) *sdk.ContainerSnapshot {
	s := r.snapshot(offset, false, 100e12, 100e12)
	s.Tanks = s.Tanks[:1]
	tank := s.Tanks[0]
	module := tank.Modules[0]
	s.Tanks = nil
	for ti := int32(1); ti <= 6; ti++ {
		next := tank
		next.Position = ti
		next.Modules = nil
		for mi := int32(1); mi <= 16; mi++ {
			m := module
			m.Position = mi
			m.ReportedID = fmt.Sprintf("module-%d-%d", ti, mi)
			m.Hashboards = nil
			// Every module must own its mutable rollup.
			metrics := *module.Metrics
			rollup := *module.Metrics.Rollup
			metrics.Rollup = &rollup
			m.Metrics = &metrics
			for bi := int32(1); bi <= 3; bi++ {
				board := module.Hashboards[0]
				board.Position = bi
				board.Serial = fmt.Sprintf("board-%d-%d-%d", ti, mi, bi)
				m.Hashboards = append(m.Hashboards, board)
			}
			next.Modules = append(next.Modules, m)
		}
		power := *tank.Metrics
		power.PowerSource = fmt.Sprintf("pdu-%d", ti)
		power.SourceTimes = map[string]time.Time{power.PowerSource: s.ObservedAt}
		next.Metrics = &power
		s.Tanks = append(s.Tanks, next)
	}
	sdk.CalculateContainerRollups(s)
	return s
}

func TestStoredContainerRollupsReportingEvidenceRepresentativeScale(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.representativeSnapshot(0))
	var client int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
 SELECT gen_random_uuid()::text,organization_id,id,1,'Capture sizing',capabilities FROM hosted_account
 WHERE depth=0 AND organization_id=(SELECT org_id FROM device WHERE id=$1) RETURNING id`, r.databaseID).Scan(&client))
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id)
 SELECT d.org_id,7,i.hashboard_id,d.id,i.hashboard_id,$1 FROM hashboard_installation i JOIN device d ON d.id=i.device_id WHERE d.id=$2 AND i.removed_at IS NULL`, client, r.databaseID)
	require.NoError(t, err)
	// Keep the same narrowed normalization path in both modes so this measures
	// retained capture rather than the topology-stamping optimization itself.
	var captureTrigger string
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT pg_get_triggerdef(oid) FROM pg_trigger
 WHERE tgrelid='hosted_account_history_sample'::regclass AND tgname='record_hosted_reporting_evidence'`).Scan(&captureTrigger))
	_, err = r.db.ExecContext(t.Context(), `DROP TRIGGER record_hosted_reporting_evidence ON hosted_account_history_sample`)
	require.NoError(t, err)
	r.base = time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	const polls = 20
	measure := func(first int) []time.Duration {
		t.Helper()
		durations := make([]time.Duration, 0, polls)
		for poll := range polls {
			snapshot := r.representativeSnapshot(time.Duration(first+poll) * time.Millisecond)
			start := time.Now()
			r.store(t, snapshot)
			durations = append(durations, time.Since(start))
		}
		return durations
	}
	without := measure(0)
	_, err = r.db.ExecContext(t.Context(), captureTrigger)
	require.NoError(t, err)
	var before, after int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT pg_total_relation_size('hosted_account_reporting_evidence')+pg_total_relation_size('hosted_account_reporting_allocation_snapshot')`).Scan(&before))
	with := measure(polls)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT pg_total_relation_size('hosted_account_reporting_evidence')+pg_total_relation_size('hosted_account_reporting_allocation_snapshot')`).Scan(&after))
	var events, snapshots int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&events))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_allocation_snapshot`).Scan(&snapshots))
	// Topology stamping must not re-normalize unchanged measurements.
	require.Equal(t, 288*4*polls, events, "each board retains only its three metrics and state observation")
	var metricDeletes, metricInserts, states int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FILTER(WHERE source_id=5 AND operation='DELETE'),count(*) FILTER(WHERE source_id=5 AND operation='INSERT'),count(*) FILTER(WHERE source_id=6) FROM hosted_account_reporting_evidence`).Scan(&metricDeletes, &metricInserts, &states))
	require.Zero(t, metricDeletes, "topology-only updates do not manufacture corrections")
	require.Equal(t, 288*3*polls, metricInserts)
	require.Equal(t, 288*polls, states)
	require.Equal(t, 288, snapshots, "identity is stored once per hashboard interval")
	var last time.Time
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT max(collected_at) FROM hashboard_metrics`).Scan(&last))
	start := time.Now()
	_, err = r.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET hashrate_hs=hashrate_hs+1 WHERE collected_at=$1`, last)
	require.NoError(t, err)
	correctionTime := time.Since(start)
	var corrected int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&corrected))
	require.Equal(t, 288*3*2, corrected-events, "corrections retain all three before and after metric images")
	start = time.Now()
	var captured, observed int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(last_event_id),count(last_observation_event_id) FROM hosted_account_reporting_capture_status`).Scan(&captured, &observed))
	statusTime := time.Since(start)
	require.Equal(t, 288*4, captured)
	require.Equal(t, captured, observed)
	describe := func(values []time.Duration) string {
		var total time.Duration
		for _, value := range values {
			total += value
		}
		sorted := slices.Clone(values)
		slices.Sort(sorted)
		return fmt.Sprintf("mean=%s p95=%s", total/time.Duration(len(values)), sorted[(len(sorted)*95-1)/100])
	}
	t.Logf("96-module / 288-hashboard reporting hold, %d polls each: disabled %s; enabled %s; events/poll=%d; heap+index+snapshot bytes/event=%.1f; correction=%s; status=%s (local Docker DB; WAL/backups excluded)", polls, describe(without), describe(with), events/polls, float64(after-before)/float64(events), correctionTime, statusTime)
}
