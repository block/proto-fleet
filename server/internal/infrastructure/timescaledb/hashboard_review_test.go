package timescaledb

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type hashboardReviewRig struct {
	db       *sql.DB
	store    *TimescaleTelemetryStore
	reader   *fleetinventory.Service
	orgID    int64
	deviceID string
	base     time.Time
	modules  []int64
}

func newHashboardReviewRig(t *testing.T) *hashboardReviewRig {
	t.Helper()
	dbSvc := testutil.NewDatabaseService(t, nil)
	user := dbSvc.CreateSuperAdminUser()
	device := dbSvc.CreateDevice(user.OrganizationID, "proto")
	_, err := dbSvc.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
	require.NoError(t, err)
	store, err := NewTelemetryStore(dbSvc.DB, DefaultConfig())
	require.NoError(t, err)
	r := &hashboardReviewRig{db: dbSvc.DB, store: store, reader: fleetinventory.NewService(sqlc.New(dbSvc.DB)), orgID: user.OrganizationID, deviceID: device.ID, base: time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)}
	r.poll(t, 0, reviewBoard("a", "serial-a"), reviewBoard("b", "serial-b"))
	rows, err := r.db.QueryContext(t.Context(), "SELECT id FROM container_module ORDER BY position")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		r.modules = append(r.modules, id)
	}
	require.NoError(t, rows.Err())
	return r
}
func reviewBoard(mac, serial string) sdk.ContainerHashboard {
	return sdk.ContainerHashboard{Position: 1, Occupancy: "occupied", MACAddress: mac, Serial: serial, State: sdk.MinerStatusActive, HashrateHS: new(100e12), HealthJSON: json.RawMessage(`{"hashrate":100}`)}
}
func (r *hashboardReviewRig) poll(t *testing.T, offset time.Duration, boards ...sdk.ContainerHashboard) {
	t.Helper()
	at := r.base.Add(offset)
	snapshot := &sdk.ContainerSnapshot{ObservedAt: at, TopologyComplete: true, Plant: &sdk.ContainerPlant{ObservedAt: at, TotalPowerW: new(3000.0)}, Controls: &sdk.ContainerControls{ObservedAt: at, Supported: new(true)}}
	tank := sdk.ContainerTank{Position: 1}
	for i, b := range boards {
		b.ObservedAt, b.StateObservedAt = at, at
		tank.Modules = append(tank.Modules, sdk.ContainerModule{Position: int32(i + 1), State: sdk.MinerStatusActive, ObservedAt: at, Metrics: &sdk.ContainerModuleMetrics{ObservedAt: at, HashrateHS: new(100e12)}, Hashboards: []sdk.ContainerHashboard{b}}) //nolint:gosec // fixture has two modules
	}
	snapshot.Tanks = []sdk.ContainerTank{tank}
	require.NoError(t, r.store.StoreDeviceMetrics(t.Context(), modelsV2.DeviceMetrics{DeviceIdentifier: r.deviceID, Timestamp: at, Health: modelsV2.HealthHealthyActive, Container: snapshot, HashrateHS: &modelsV2.MetricValue{Value: 200e12}}))
}
func (r *hashboardReviewRig) read(t *testing.T) []*pb.ModuleHashboards {
	t.Helper()
	response, err := r.reader.Hashboards(t.Context(), r.orgID, &pb.GetModuleHashboardsRequest{ModuleIds: r.modules}, []fleetinventory.Scope{{OrgWide: true}})
	require.NoError(t, err)
	return response.Modules
}

func TestHashboardConflictsPreserveControllerTelemetry(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second sdk.ContainerHashboard
		rejected      int
	}{
		{"contradictory aliases", reviewBoard("a", "serial-b"), reviewBoard("b", "serial-b"), 2},
		{"same hardware through different aliases", reviewBoard("a", ""), reviewBoard("", "serial-a"), 2},
		{"duplicate raw identity", reviewBoard("a", "serial-a"), reviewBoard("a", "serial-a"), 2},
		{"changed serial", reviewBoard("a", "different-serial"), reviewBoard("b", "serial-b"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newHashboardReviewRig(t)
			original := r.read(t)
			r.poll(t, time.Second, tc.first, tc.second)
			current := r.read(t)
			for i := range tc.rejected {
				require.Equal(t, "unknown", current[i].Slots[0].Occupancy)
				require.Equal(t, sdk.HashboardIdentityConflict, current[i].Slots[0].UnavailableReason)
				require.Nil(t, current[i].Slots[0].Board)
			}
			if tc.rejected == 1 {
				require.Equal(t, original[1].Slots[0].Board.HashboardId, current[1].Slots[0].Board.HashboardId)
			}
			for table, want := range map[string]int{"hashboard": 2, "hashboard_identity": 4, "hashboard_installation": 2, "container_module_metrics": 4, "container_plant_metrics": 2, "device_metrics": 2} {
				var count int
				require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count)) // fixture-controlled table names
				require.Equal(t, want, count, table)
			}
			var closed int
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE removed_at IS NOT NULL").Scan(&closed))
			require.Zero(t, closed)
			r.poll(t, 2*time.Second, reviewBoard("a", "serial-a"), reviewBoard("b", "serial-b"))
			recovered := r.read(t)
			require.Equal(t, original[0].Slots[0].Board.InstallationId, recovered[0].Slots[0].Board.InstallationId)
			require.Empty(t, recovered[0].Slots[0].UnavailableReason)
		})
	}
}

func TestHashboardBatchFreshnessAndHistoryContract(t *testing.T) {
	r := newHashboardReviewRig(t)
	current := r.read(t)
	boardID := current[0].Slots[0].Board.HashboardId
	require.NotEqual(t, boardID, current[1].Slots[0].Board.HashboardId)
	require.Equal(t, r.modules[0], current[0].Slots[0].Board.ModuleId)
	require.Equal(t, r.modules[1], current[1].Slots[0].Board.ModuleId)
	scopes := []fleetinventory.Scope{{OrgWide: true}}
	request := &pb.ListHashboardHistoryRequest{HashboardId: boardID, StartTime: timestamppb.New(r.base.Add(-time.Second)), EndTime: timestamppb.New(r.base.Add(time.Minute)), PageSize: 1}
	// Identical occupied retries don't add samples or installations.
	r.poll(t, 0, reviewBoard("a", "serial-a"), reviewBoard("b", "serial-b"))
	for _, offset := range []time.Duration{time.Second, 2 * time.Second} {
		r.poll(t, offset, reviewBoard("a", "serial-a"), reviewBoard("b", "serial-b"))
	}
	first, err := r.reader.HashboardHistory(t.Context(), r.orgID, request, scopes)
	require.NoError(t, err)
	require.Len(t, first.Samples, 1)
	require.NotEmpty(t, first.NextPageToken)
	request.PageToken = first.NextPageToken
	second, err := r.reader.HashboardHistory(t.Context(), r.orgID, request, scopes)
	require.NoError(t, err)
	require.Len(t, second.Samples, 1)
	require.True(t, second.Samples[0].ObservedAt.AsTime().After(first.Samples[0].ObservedAt.AsTime()))
	thirdRequest, ok := proto.Clone(request).(*pb.ListHashboardHistoryRequest)
	require.True(t, ok)
	thirdRequest.PageToken = second.NextPageToken
	third, err := r.reader.HashboardHistory(t.Context(), r.orgID, thirdRequest, scopes)
	require.NoError(t, err)
	require.Len(t, third.Samples, 1)
	require.Empty(t, third.NextPageToken)
	for _, change := range []func(*pb.ListHashboardHistoryRequest){
		func(q *pb.ListHashboardHistoryRequest) { q.HashboardId = current[1].Slots[0].Board.HashboardId },
		func(q *pb.ListHashboardHistoryRequest) { q.EndTime = timestamppb.New(r.base.Add(2 * time.Minute)) },
	} {
		q, ok := proto.Clone(request).(*pb.ListHashboardHistoryRequest)
		require.True(t, ok)
		change(q)
		_, err := r.reader.HashboardHistory(t.Context(), r.orgID, q, scopes)
		require.Error(t, err)
	}
	_, err = r.reader.HashboardHistory(t.Context(), r.orgID, request, []fleetinventory.Scope{{OrgWide: true, Sites: []int64{999}}})
	require.Error(t, err, "cursor binds the authorization scope")
	request.PageToken = ""
	for _, id := range []int64{boardID + 9999} {
		q, ok := proto.Clone(request).(*pb.ListHashboardHistoryRequest)
		require.True(t, ok)
		q.HashboardId = id
		_, err := r.reader.HashboardHistory(t.Context(), r.orgID, q, scopes)
		requireHashboardNotFound(t, err)
	}
	_, err = r.reader.HashboardHistory(t.Context(), r.orgID+9999, request, scopes)
	requireHashboardNotFound(t, err)
	_, err = r.reader.HashboardHistory(t.Context(), r.orgID, request, []fleetinventory.Scope{{Sites: []int64{9999}}})
	requireHashboardNotFound(t, err)
	request.StartTime = timestamppb.New(r.base.Add(30 * time.Second))
	empty, err := r.reader.HashboardHistory(t.Context(), r.orgID, request, scopes)
	require.NoError(t, err)
	require.Empty(t, empty.Samples)
	// Closed historical installations remain readable, including when no current one exists.
	r.poll(t, 3*time.Second, sdk.ContainerHashboard{Position: 1, Occupancy: "empty"}, reviewBoard("b", "serial-b"))
	request.StartTime = timestamppb.New(r.base.Add(-time.Second))
	history, err := r.reader.HashboardHistory(t.Context(), r.orgID, request, scopes)
	require.NoError(t, err)
	require.Len(t, history.Samples, 1)
	// One retired module doesn't discard the other module in a batch.
	_, err = r.db.ExecContext(t.Context(), "UPDATE container_module SET retired_at=now() WHERE id=$1", r.modules[0])
	require.NoError(t, err)
	batch := r.read(t)
	require.Len(t, batch, 1)
	require.Equal(t, r.modules[1], batch[0].ModuleId)
	// Exact two-minute source boundary suppresses measurements; slot data may still be fresh.
	_, err = r.db.ExecContext(t.Context(), "WITH expired AS (UPDATE hashboard_latest_metrics SET time=now()-interval '2 minutes' RETURNING hashboard_id,time) UPDATE hashboard_slot s SET health_observed_at=e.time FROM expired e WHERE s.hashboard_id=e.hashboard_id")
	require.NoError(t, err)
	require.Nil(t, r.read(t)[0].Slots[0].Board.HashrateHs)
	_, err = r.db.ExecContext(t.Context(), "UPDATE hashboard_slot SET observed_at=now()-interval '2 minutes'")
	require.NoError(t, err)
	slot := r.read(t)[0].Slots[0]
	require.Equal(t, "unknown", slot.Occupancy)
	require.Nil(t, slot.Board)
}

func requireHashboardNotFound(t *testing.T, err error) {
	t.Helper()
	var typed fleeterror.FleetError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, connect.CodeNotFound, typed.GRPCCode)
}

func TestMalformedHashboardIdentityStillProtectsAllPlacements(t *testing.T) {
	r := newHashboardReviewRig(t)
	malformed := reviewBoard("a", "")
	malformed.HealthJSON = json.RawMessage(`{"broken":`)
	r.poll(t, time.Second, malformed, reviewBoard("", "serial-a"))
	current := r.read(t)
	for _, module := range current {
		require.Equal(t, "unknown", module.Slots[0].Occupancy)
	}
	var removed int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE removed_at IS NOT NULL").Scan(&removed))
	require.Zero(t, removed)
}

func TestRejectedReplacementModulePreservesBoardInstallation(t *testing.T) {
	r := newHashboardReviewRig(t)
	original := r.read(t)[0].Slots[0].Board.InstallationId
	_, err := r.db.ExecContext(t.Context(), "UPDATE container_module SET reported_id='old-module' WHERE id=$1", r.modules[0])
	require.NoError(t, err)
	for _, offset := range []time.Duration{time.Second, 2 * time.Second} {
		at := r.base.Add(offset)
		b := reviewBoard("a", "changed-serial")
		snapshot := &sdk.ContainerSnapshot{ObservedAt: at, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, ReportedID: "new-module", Hashboards: []sdk.ContainerHashboard{b}}}}}}
		require.NoError(t, r.store.StoreDeviceMetrics(t.Context(), modelsV2.DeviceMetrics{DeviceIdentifier: r.deviceID, Timestamp: at, Container: snapshot}))
		var removed sql.NullTime
		require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT removed_at FROM hashboard_installation WHERE id=$1", original).Scan(&removed))
		require.False(t, removed.Valid, "rejected evidence cannot retire the hardware placement")
	}
	r.poll(t, 3*time.Second, reviewBoard("a", "serial-a"), reviewBoard("b", "serial-b"))
	var removed sql.NullTime
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT removed_at FROM hashboard_installation WHERE id=$1", original).Scan(&removed))
	require.True(t, removed.Valid, "trustworthy reinstall ends the protected interval")
}

func TestInvalidHashboardDoesNotDiscardHealthyModules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*sdk.ContainerHashboard)
	}{
		{"missing identity", func(b *sdk.ContainerHashboard) { b.MACAddress = ""; b.Serial = "" }},
		{"invalid state", func(b *sdk.ContainerHashboard) { b.State = sdk.MinerStatus(999) }},
		{"invalid diagnostics", func(b *sdk.ContainerHashboard) { b.HealthJSON = json.RawMessage(`[]`) }},
		{"invalid position", func(b *sdk.ContainerHashboard) { b.Position = 4 }},
		{"empty with identity", func(b *sdk.ContainerHashboard) { b.Occupancy = "empty" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newHashboardReviewRig(t)
			original := r.read(t)[0].Slots[0].Board.InstallationId
			bad := reviewBoard("a", "serial-a")
			tc.mutate(&bad)
			r.poll(t, time.Second, bad, reviewBoard("b", "serial-b"))
			current := r.read(t)
			require.Equal(t, "unknown", current[0].Slots[0].Occupancy)
			require.NotEmpty(t, current[0].Slots[0].UnavailableReason)
			require.NotNil(t, current[1].Slots[0].Board.HashrateHs)
			var removed sql.NullTime
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT removed_at FROM hashboard_installation WHERE id=$1", original).Scan(&removed))
			require.False(t, removed.Valid)
		})
	}
}

func TestHashboardDatabaseFailureStillRollsBackTelemetry(t *testing.T) {
	r := newHashboardReviewRig(t)
	// Only this disposable test database rejects otherwise valid hardware writes.
	_, err := r.db.ExecContext(t.Context(), "ALTER TABLE hashboard ADD CONSTRAINT reject_writes CHECK (id < 0) NOT VALID")
	require.NoError(t, err)
	at := r.base.Add(time.Second)
	snapshot := &sdk.ContainerSnapshot{ObservedAt: at, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, Hashboards: []sdk.ContainerHashboard{reviewBoard("a", "serial-a")}}}}}}
	err = r.store.StoreDeviceMetrics(t.Context(), modelsV2.DeviceMetrics{DeviceIdentifier: r.deviceID, Timestamp: at, Container: snapshot})
	require.ErrorContains(t, err, "reject_writes")
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM device_metrics").Scan(&count))
	require.Equal(t, 1, count)
	var latest time.Time
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT observed_at FROM container_inventory").Scan(&latest))
	require.True(t, r.base.Equal(latest))
}

func TestHashboardJSONBRejectionPreservesTelemetry(t *testing.T) {
	for _, diagnostic := range []string{`{"serial":"bad\u0000"}`, `{"value":"\ud800"}`, `{"value":1e200000}`, `{"value":1e-200000}`} {
		t.Run(diagnostic, func(t *testing.T) {
			r := newHashboardReviewRig(t)
			bad := reviewBoard("a", "serial-a")
			bad.HealthJSON = json.RawMessage(diagnostic)
			r.poll(t, time.Second, bad, reviewBoard("b", "serial-b"))
			current := r.read(t)
			require.Equal(t, "unknown", current[0].Slots[0].Occupancy)
			require.NotNil(t, current[1].Slots[0].Board.HashrateHs)
			for table, want := range map[string]int{"hashboard_metrics": 3, "container_module_metrics": 4, "container_plant_metrics": 2, "device_metrics": 2} {
				var count int
				require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
				require.Equal(t, want, count, table)
			}
		})
	}
	t.Run("NUL identity", func(t *testing.T) {
		r := newHashboardReviewRig(t)
		r.poll(t, time.Second, reviewBoard("a", "serial-a\x00"), reviewBoard("b", "serial-b"))
		require.Equal(t, "unknown", r.read(t)[0].Slots[0].Occupancy)
		require.NotNil(t, r.read(t)[1].Slots[0].Board)
	})
}

func TestRejectedHashboardClaimsPreventFalseMoves(t *testing.T) {
	r := newHashboardReviewRig(t)
	original := r.read(t)
	rejected := sdk.UnavailableHashboard(1, sdk.HashboardInvalidObservation)
	rejected.IdentityClaims = []sdk.HashboardIdentityClaim{{MACAddress: "a"}, {Serial: "bad\x00"}}
	r.poll(t, time.Second, rejected, reviewBoard("", "serial-a"))
	for _, module := range r.read(t) {
		require.Equal(t, "unknown", module.Slots[0].Occupancy)
		require.Equal(t, sdk.HashboardIdentityConflict, module.Slots[0].UnavailableReason)
	}
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE removed_at IS NOT NULL").Scan(&count))
	require.Zero(t, count)
	// An unknown claim must not create hardware or enrich existing aliases.
	rejected.IdentityClaims = []sdk.HashboardIdentityClaim{{MACAddress: "new-board", Serial: "new-serial"}}
	r.poll(t, 2*time.Second, rejected, reviewBoard("b", "serial-b"))
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_identity").Scan(&count))
	require.Equal(t, 4, count)
	r.poll(t, 3*time.Second, reviewBoard("a", "serial-a"), reviewBoard("b", "serial-b"))
	require.Equal(t, original[0].Slots[0].Board.InstallationId, r.read(t)[0].Slots[0].Board.InstallationId)
}
