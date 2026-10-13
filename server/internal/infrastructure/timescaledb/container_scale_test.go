package timescaledb

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestStoredContainerRollupsRepresentativeScale(t *testing.T) {
	r := newContainerRollupRig(t)
	start := time.Now()
	for poll := range 12 {
		s := r.snapshot(time.Duration(poll)*time.Second, false, 100e12, 100e12)
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
		r.store(t, s)
	}
	t.Logf("288-board collection transactions: %s/poll (12 polls, local Docker DB)", time.Since(start)/12)
	var versions, observations int
	var topologyBytes, tankRowBytes float64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*),avg(pg_column_size(topology)) FROM container_topology_version WHERE device_id=$1", r.databaseID).Scan(&versions, &topologyBytes))
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_topology_observation WHERE device_id=$1", r.databaseID).Scan(&observations))
	require.Equal(t, 1, versions)
	require.Equal(t, 12, observations)
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT avg(pg_column_size(m)) FROM container_tank_metrics m").Scan(&tankRowBytes))
	start = time.Now()
	rows, err := sqlc.New(r.db).GetContainerPollHistory(t.Context(), sqlc.GetContainerPollHistoryParams{ContainerIdentifier: r.deviceID, StartTime: r.base, EndTime: r.base.Add(time.Minute), GranularitySeconds: 60})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	t.Logf("saved container chart query: %s; topology bytes/version: %.0f; tank bytes/row: %.0f", time.Since(start), topologyBytes, tankRowBytes)
}
