package timescaledb

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestEmptyDiagnosticIssuesDoNotDiscardTelemetry(t *testing.T) {
	r := newContainerRollupRig(t)
	for _, offset := range []time.Duration{0, time.Second} {
		snapshot := r.snapshot(offset, false, 100e12, 200e12)
		for ti := range snapshot.Tanks {
			module := &snapshot.Tanks[ti].Modules[0]
			// A healthy collector report has source evidence but a nil issue slice,
			// which JSON encodes as null rather than an empty array.
			module.Issues = &sdk.ContainerModuleIssues{
				ObservedAt:  snapshot.ObservedAt,
				Complete:    true,
				SourceTimes: map[string]time.Time{"psu": snapshot.ObservedAt, "hashboard:a": snapshot.ObservedAt, "hashboard:b": snapshot.ObservedAt, "hashboard:c": snapshot.ObservedAt},
			}
		}
		r.store(t, snapshot)
	}
	var samples int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM device_metrics WHERE device_identifier=$1", r.deviceID).Scan(&samples))
	require.Equal(t, 2, samples, "healthy diagnostics must not roll back numeric telemetry")
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_diagnostic_observation WHERE issues='[]'::jsonb").Scan(&samples))
	require.Equal(t, 4, samples, "both boards retain an empty diagnostic observation on each poll")
}

func TestCurrentHashboardsAcceptNullDiagnosticItems(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	// Exercise reads of an existing valid report independently of ingestion.
	_, err := r.db.ExecContext(t.Context(), "UPDATE container_module SET issues=jsonb_build_object('items',NULL)")
	require.NoError(t, err)
	var orgID, moduleID int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT d.org_id,m.id FROM device d JOIN container_tank t ON t.device_id=d.id JOIN container_module m ON m.tank_id=t.id WHERE d.id=$1 AND t.position=1", r.databaseID).Scan(&orgID, &moduleID))
	response, err := fleetinventory.NewService(sqlc.New(r.db)).Hashboards(t.Context(), orgID,
		&pb.GetModuleHashboardsRequest{ModuleIds: []int64{moduleID}}, []fleetinventory.Scope{{OrgWide: true}})
	require.NoError(t, err)
	require.Len(t, response.Modules, 1)
	board := response.Modules[0].Slots[0].Board
	require.NotNil(t, board)
	require.Empty(t, board.Issues)
	require.NotNil(t, board.HashrateHs)
	require.Equal(t, 100e12, *board.HashrateHs)
}
