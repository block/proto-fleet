package timescaledb

import (
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestUnlocatedFirstPlacementExcludesPriorSiteDiagnostics(t *testing.T) {
	r := newContainerRollupRig(t)
	var org, siteA, siteB int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT org_id FROM device WHERE id=$1", r.databaseID).Scan(&org))
	for i, target := range []*int64{&siteA, &siteB} {
		require.NoError(t, r.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES ($1,$2,$2) RETURNING id", org, []string{"A", "B"}[i]).Scan(target))
	}
	move := func(site int64) {
		_, err := r.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", site, r.databaseID)
		require.NoError(t, err)
	}
	poll := func(at, health, state time.Duration, mapped bool) *sdk.ContainerSnapshot {
		s := singleInventoryBoard(r, at, health, mapped)
		var b *sdk.ContainerHashboard
		if mapped {
			b = &s.Tanks[0].Modules[0].Hashboards[0]
		} else {
			b = &s.UnlocatedHashboards[0]
		}
		b.State = sdk.ModuleAttention
		b.StateObservedAt = r.base.Add(state)
		b.MiningState = "MINING_STATE_MINING"
		b.Isolated = new(true)
		b.HashrailOK = new(false)
		b.RefclksOK = new(false)
		b.HealthJSON = json.RawMessage(`{"fault":"sensitive diagnostic","hashrate":100}`)
		b.MetadataJSON = json.RawMessage(`{"firmwareVersion":"1.2.3","miningState":"MINING_STATE_MINING","isolated":true}`)
		r.store(t, s)
		return s
	}
	move(siteA)
	poll(0, 0, 0, false)
	move(siteB)
	reader := fleetinventory.NewService(sqlc.New(r.db))
	var moduleID, boardID int64
	read := func() *pb.HashboardReading {
		response, err := reader.Hashboards(t.Context(), org, &pb.GetModuleHashboardsRequest{ModuleIds: []int64{moduleID}}, []fleetinventory.Scope{{Sites: []int64{siteB}}})
		require.NoError(t, err)
		require.Len(t, response.Modules, 1)
		require.NotNil(t, response.Modules[0].Slots[0].Board)
		return response.Modules[0].Slots[0].Board
	}
	history := func() []*pb.HashboardReading {
		response, err := reader.HashboardHistory(t.Context(), org, &pb.ListHashboardHistoryRequest{HashboardId: boardID, StartTime: timestamppb.New(r.base.Add(-time.Second)), EndTime: timestamppb.New(r.base.Add(time.Minute))}, []fleetinventory.Scope{{Sites: []int64{siteB}}})
		require.NoError(t, err)
		return response.Samples
	}
	assertNoState := func(b *pb.HashboardReading) {
		require.Equal(t, "unknown", b.State)
		require.Nil(t, b.StateObservedAt)
		require.Empty(t, b.MiningState)
		require.Nil(t, b.Isolated)
		require.Nil(t, b.Health)
		if b.Metadata != nil {
			require.NotContains(t, b.Metadata.AsMap(), "miningState")
			require.NotContains(t, b.Metadata.AsMap(), "isolated")
		}
	}
	// First placement and repeated cached polls must not inherit the initial-discovery exception.
	for _, at := range []time.Duration{time.Second, 2 * time.Second} {
		s := poll(at, 0, 0, true)
		require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT id FROM container_module").Scan(&moduleID))
		require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT id FROM hashboard").Scan(&boardID))
		b := read()
		require.Nil(t, b.HashrateHs)
		require.Nil(t, b.HashrailOk)
		require.Nil(t, b.RefclksOk)
		assertNoState(b)
		require.Empty(t, history())
		var count int
		require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_metrics").Scan(&count))
		require.Zero(t, count, "cached health/state must not be persisted under the first installation")
		require.Equal(t, r.base, s.Tanks[0].Modules[0].Hashboards[0].ObservedAt, "source evidence must survive redaction")
		var source time.Time
		require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT source_at FROM hashboard_observation").Scan(&source))
		require.Equal(t, r.base, source.UTC())
	}
	// Fresh health cannot make old state/diagnostics current.
	poll(3*time.Second, 3*time.Second, 0, true)
	require.NotNil(t, read().HashrateHs)
	assertNoState(read())
	rows := history()
	require.Len(t, rows, 1)
	assertNoState(rows[0])
	// Conversely, fresh state remains useful while cached health is withheld.
	poll(4*time.Second, 0, 4*time.Second, true)
	require.Nil(t, read().HashrateHs)
	require.Nil(t, read().Health)
	require.Equal(t, "attention", read().State)
	require.Len(t, history(), 1)
	poll(5*time.Second, 5*time.Second, 5*time.Second, true)
	require.NotNil(t, read().HashrateHs)
	require.NotNil(t, read().Health)
	require.Equal(t, "attention", read().State)
	require.Len(t, history(), 2)
}
