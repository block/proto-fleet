package timescaledb

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPSUModuleHistorySurvivesControllerSoftDelete(t *testing.T) {
	r := newContainerRollupRig(t)
	var org, recordedSite, otherSite int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT org_id FROM device WHERE id=$1`, r.databaseID).Scan(&org))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Recorded','recorded-history') RETURNING id`, org).Scan(&recordedSite))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Other','other-history') RETURNING id`, org).Scan(&otherSite))
	_, err := r.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, recordedSite, r.databaseID)
	require.NoError(t, err)

	snapshot := r.snapshot(0, false, 100e12, 200e12)
	for ti := range snapshot.Tanks {
		snapshot.Tanks[ti].Modules[0].IdentityConfirmed = true
	}
	r.store(t, snapshot)
	var module int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `
		SELECT module_id FROM container_module_identity
		WHERE org_id=$1 AND reported_module_id='module-1'`, org,
	).Scan(&module))
	_, err = r.db.ExecContext(t.Context(), `UPDATE device SET deleted_at=$1 WHERE id=$2`, r.base.Add(time.Second), r.databaseID)
	require.NoError(t, err)

	reader := fleetinventory.NewService(sqlc.New(r.db))
	request := &pb.ListModuleHistoryRequest{
		ModuleId:  module,
		StartTime: timestamppb.New(r.base.Add(-time.Second)),
		EndTime:   timestamppb.New(r.base.Add(time.Minute)),
	}
	history, err := reader.History(t.Context(), org, request, []fleetinventory.Scope{{Sites: []int64{recordedSite}}})
	require.NoError(t, err)
	require.Len(t, history.Samples, 1)
	require.NotNil(t, history.Samples[0].HashrateHs)
	require.Equal(t, 100e12, *history.Samples[0].HashrateHs)

	_, err = reader.History(t.Context(), org, request, []fleetinventory.Scope{{Sites: []int64{otherSite}}})
	require.ErrorContains(t, err, "module history is not accessible")
	_, err = reader.History(t.Context(), org+999, request, []fleetinventory.Scope{{OrgWide: true}})
	require.ErrorContains(t, err, "module not found")
}
