package buildings

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"go.uber.org/mock/gomock"

	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// fakeDeviceQueryer is a hand-rolled devicerollup.DeviceQueryer.
type fakeDeviceQueryer struct {
	deviceIDs       []string
	deviceIDsErr    error
	lastFilter      *interfaces.MinerFilter
	stateCounts     interfaces.MinerStateCounts
	collections     map[int64]interfaces.MinerStateCounts
	componentCounts []interfaces.ComponentErrorCount
	componentErr    error
}

func (f *fakeDeviceQueryer) GetDeviceIdentifiersByOrgWithFilter(_ context.Context, _ int64, filter *interfaces.MinerFilter) ([]string, error) {
	f.lastFilter = filter
	return f.deviceIDs, f.deviceIDsErr
}

func (f *fakeDeviceQueryer) GetMinerStateCountsByDeviceIDs(_ context.Context, _ int64, _ []string) (interfaces.MinerStateCounts, error) {
	return f.stateCounts, nil
}

func (f *fakeDeviceQueryer) GetMinerStateCountsByCollections(_ context.Context, _ int64, _ []int64) (map[int64]interfaces.MinerStateCounts, error) {
	return f.collections, nil
}

func (f *fakeDeviceQueryer) GetComponentErrorCounts(_ context.Context, _ int64, _ interfaces.ComponentErrorScope) ([]interfaces.ComponentErrorCount, error) {
	return f.componentCounts, f.componentErr
}

type fakeTelemetryCollector struct {
	metrics map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics
	err     error
}

func (f *fakeTelemetryCollector) GetLatestDeviceMetrics(_ context.Context, _ []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	return f.metrics, f.err
}

func intPtr(v int32) *int32                      { return &v }
func floatPtr(v float64) *modelsV2.MetricValue   { return &modelsV2.MetricValue{Value: v} }
func newTx() *fakeTransactor                     { return &fakeTransactor{} }
func newDevices(ids []string) *fakeDeviceQueryer { return &fakeDeviceQueryer{deviceIDs: ids} }
func newTelemetry() *fakeTelemetryCollector      { return &fakeTelemetryCollector{} }
func buildingWith(aisles, racksPerAisle int32) *models.Building {
	return &models.Building{Aisles: aisles, RacksPerAisle: racksPerAisle}
}

func TestGetBuildingStats_notFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().BuildingBelongsToOrg(gomock.Any(), testOrgID, int64(42)).Return(false, nil)

	svc := NewService(store, nil, nil, newDevices(nil), newTelemetry(), newTx(), nil)
	_, err := svc.GetBuildingStats(context.Background(), testOrgID, 42, nil)
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeNotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestGetBuildingStats_notFoundWhenSiteMovedDuringAuthz(t *testing.T) {
	// Race: handler resolved the building at site A, but a concurrent
	// AssignBuildingsToSite moved it to site B before the service read.
	// Expectation: surface NotFound rather than return stats the caller
	// wasn't authorized for in the new site-scope.
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().BuildingBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().ListBuildingRacks(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(nil, "", nil)
	siteB := int64(2)
	store.EXPECT().GetBuilding(gomock.Any(), testOrgID, int64(1)).Return(&models.Building{Aisles: 1, RacksPerAisle: 1, SiteID: &siteB}, nil)

	svc := NewService(store, nil, nil, newDevices(nil), newTelemetry(), newTx(), nil)
	siteA := int64(1)
	_, err := svc.GetBuildingStats(context.Background(), testOrgID, 1, &siteA)
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeNotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestGetBuildingStats_notFoundWhenSiteMovedAfterReads(t *testing.T) {
	// Sharper race: handler + initial site read both saw site A, so the
	// rollup proceeded. AssignBuildingsToSite then commits to site B
	// before the service hits its post-read re-check. Expectation: the
	// post-read guard catches the move and returns NotFound rather than
	// handing the caller a snapshot built under stale authz.
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().BuildingBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().ListBuildingRacks(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(nil, "", nil)
	siteA := int64(1)
	siteB := int64(2)
	// First GetBuilding (layout bounds + initial site check) returns A;
	// second (post-read re-check) returns B, simulating a mid-call move.
	gomock.InOrder(
		store.EXPECT().GetBuilding(gomock.Any(), testOrgID, int64(1)).Return(&models.Building{Aisles: 1, RacksPerAisle: 1, SiteID: &siteA}, nil),
		store.EXPECT().GetBuilding(gomock.Any(), testOrgID, int64(1)).Return(&models.Building{Aisles: 1, RacksPerAisle: 1, SiteID: &siteB}, nil),
	)

	svc := NewService(store, nil, nil, newDevices(nil), newTelemetry(), newTx(), nil)
	_, err := svc.GetBuildingStats(context.Background(), testOrgID, 1, &siteA)
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeNotFound {
		t.Fatalf("expected NotFound from post-read re-check, got %v", err)
	}
}

func TestGetBuildingStats_internalErrorWhenDepsMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	svc := NewService(store, nil, nil, nil, nil, newTx(), nil)
	_, err := svc.GetBuildingStats(context.Background(), testOrgID, 1, nil)
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeInternal {
		t.Fatalf("expected Internal, got %v", err)
	}
}

func TestGetBuildingStats_includesActionablePairingStatusesInFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().BuildingBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().ListBuildingRacks(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(nil, "", nil)
	// GetBuilding called twice: layout-bounds read + post-read race re-check.
	store.EXPECT().GetBuilding(gomock.Any(), testOrgID, int64(1)).Return(buildingWith(2, 4), nil).Times(2)

	devices := newDevices(nil)
	svc := NewService(store, nil, nil, devices, newTelemetry(), newTx(), nil)
	_, err := svc.GetBuildingStats(context.Background(), testOrgID, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hasPaired, hasAuthNeeded, hasDefaultPassword := false, false, false
	for _, s := range devices.lastFilter.PairingStatuses {
		if s == fm.PairingStatus_PAIRING_STATUS_PAIRED {
			hasPaired = true
		}
		if s == fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED {
			hasAuthNeeded = true
		}
		if s == fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD {
			hasDefaultPassword = true
		}
	}
	if !hasPaired || !hasAuthNeeded || !hasDefaultPassword {
		t.Errorf("expected PAIRED+AUTH_NEEDED+DEFAULT_PASSWORD filter; got %v", devices.lastFilter.PairingStatuses)
	}
	if devices.lastFilter.Limit != MaxDevicesPerStatsResponse+1 {
		t.Errorf("expected SQL-level Limit=cap+1 (%d); got %d", MaxDevicesPerStatsResponse+1, devices.lastFilter.Limit)
	}
}

func TestGetBuildingStats_failsFastOverCap(t *testing.T) {
	// SQL returns cap+1; service must trip the over-cap guard and never
	// call into state-counts or telemetry.
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().BuildingBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().ListBuildingRacks(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(nil, "", nil)
	store.EXPECT().GetBuilding(gomock.Any(), testOrgID, int64(1)).Return(buildingWith(1, 1), nil)

	overCap := make([]string, MaxDevicesPerStatsResponse+1)
	for i := range overCap {
		overCap[i] = "d"
	}
	telemetry := &fakeTelemetryCollector{err: errors.New("should not be called")}
	svc := NewService(store, nil, nil, &fakeDeviceQueryer{deviceIDs: overCap}, telemetry, newTx(), nil)
	_, err := svc.GetBuildingStats(context.Background(), testOrgID, 1, nil)
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeInternal {
		t.Fatalf("expected Internal over-cap error; got %v", err)
	}
}

func TestGetBuildingStats_clearsOutOfBoundsRackPositions(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().BuildingBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	// Building is 2 aisles × 3 racks/aisle. Rack r2's position (aisle=5)
	// is outside the floor plan; the service should null its position
	// fields so the FE skips that grid cell.
	store.EXPECT().ListBuildingRacks(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(
		[]models.BuildingRack{
			{RackID: 10, RackLabel: "R1", AisleIndex: intPtr(0), PositionInAisle: intPtr(1)},
			{RackID: 20, RackLabel: "R2", AisleIndex: intPtr(5), PositionInAisle: intPtr(0)},
			{RackID: 30, RackLabel: "R3", AisleIndex: intPtr(1), PositionInAisle: intPtr(7)},
		},
		"",
		nil,
	)
	store.EXPECT().GetBuilding(gomock.Any(), testOrgID, int64(1)).Return(buildingWith(2, 3), nil).Times(2)

	svc := NewService(store, nil, nil, newDevices(nil), newTelemetry(), newTx(), nil)
	stats, err := svc.GetBuildingStats(context.Background(), testOrgID, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.RackCount != 3 {
		t.Fatalf("expected 3 racks in count; got %d", stats.RackCount)
	}
	got := make(map[int64]bool, len(stats.RackHealth))
	for _, r := range stats.RackHealth {
		got[r.RackID] = r.AisleIndex != nil && r.PositionInAisle != nil
	}
	if !got[10] {
		t.Errorf("R1 should keep position; rackHealth=%+v", stats.RackHealth)
	}
	if got[20] {
		t.Errorf("R2 has out-of-bounds aisle; expected position cleared")
	}
	if got[30] {
		t.Errorf("R3 has out-of-bounds position; expected position cleared")
	}
}

func TestGetBuildingStats_rollsUpDeviceMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().BuildingBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().ListBuildingRacks(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(
		[]models.BuildingRack{
			{RackID: 100, RackLabel: "R1", AisleIndex: intPtr(0), PositionInAisle: intPtr(0)},
		},
		"",
		nil,
	)
	store.EXPECT().GetBuilding(gomock.Any(), testOrgID, int64(1)).Return(buildingWith(1, 1), nil).Times(2)

	devices := &fakeDeviceQueryer{
		deviceIDs:   []string{"d1", "d2"},
		stateCounts: interfaces.MinerStateCounts{HashingCount: 2},
		collections: map[int64]interfaces.MinerStateCounts{
			100: {HashingCount: 2, BrokenCount: 0},
		},
	}
	telemetry := &fakeTelemetryCollector{
		metrics: map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics{
			"d1": {HashrateHS: floatPtr(100e12), PowerW: floatPtr(2000), EfficiencyJH: floatPtr(20e-12)},
			"d2": {HashrateHS: floatPtr(50e12), PowerW: floatPtr(1000), EfficiencyJH: floatPtr(20e-12)},
		},
	}

	svc := NewService(store, nil, nil, devices, telemetry, newTx(), nil)
	stats, err := svc.GetBuildingStats(context.Background(), testOrgID, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.DeviceCount != 2 || stats.ReportingCount != 2 {
		t.Errorf("device/reporting: got %d/%d want 2/2", stats.DeviceCount, stats.ReportingCount)
	}
	if stats.TotalHashrateThs != 150 {
		t.Errorf("hashrate: got %g want 150", stats.TotalHashrateThs)
	}
	if stats.TotalPowerKw != 3 {
		t.Errorf("power: got %g want 3", stats.TotalPowerKw)
	}
	if delta := stats.AvgEfficiencyJth - 20; delta > 1e-6 || delta < -1e-6 {
		t.Errorf("efficiency: got %g want ~20", stats.AvgEfficiencyJth)
	}
	if len(stats.DeviceIdentifiers) != 2 {
		t.Errorf("device identifiers not plumbed through: %v", stats.DeviceIdentifiers)
	}
	if len(stats.RackHealth) != 1 || stats.RackHealth[0].HashingCount != 2 {
		t.Errorf("rack health not populated correctly: %+v", stats.RackHealth)
	}
}

func TestListBuildings_degradesWhenListTelemetryFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().ListBuildings(gomock.Any(), models.ListFilter{OrgID: testOrgID, IncludeStats: true}).Return([]models.BuildingWithCounts{
		{
			Building:    models.Building{ID: 1, OrgID: testOrgID, Name: "Building 1"},
			RackCount:   2,
			DeviceCount: 1,
		},
	}, nil)

	devices := &fakeDeviceQueryer{
		deviceIDs: []string{"d1"},
		stateCounts: interfaces.MinerStateCounts{
			HashingCount: 1,
		},
		componentCounts: []interfaces.ComponentErrorCount{
			{ScopeID: 1, ComponentType: 4, DeviceCount: 1},
		},
	}
	telemetry := &fakeTelemetryCollector{err: errors.New("telemetry unavailable")}
	svc := NewService(store, nil, nil, devices, telemetry, newTx(), nil)

	rows, err := svc.ListBuildings(context.Background(), models.ListFilter{OrgID: testOrgID, IncludeStats: true}, fleetlistfilter.Filter{}, func(*int64) bool { return true })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ListStats == nil {
		t.Fatalf("expected one row with list stats, got %+v", rows)
	}
	stats := rows[0].ListStats
	if stats.RackCount != 2 || stats.DeviceCount != 1 {
		t.Fatalf("structural counts not preserved after telemetry failure: %+v", stats)
	}
	if stats.HashingCount != 1 || stats.ControlBoardIssueCount != 1 {
		t.Fatalf("non-telemetry stats not preserved after telemetry failure: %+v", stats)
	}
	if stats.ReportingCount != 0 || stats.HashrateReportingCount != 0 || stats.PowerReportingCount != 0 || stats.TemperatureReportingCount != 0 {
		t.Fatalf("telemetry reporting counts should be zero after telemetry failure: %+v", stats)
	}
	hasDefaultPassword := false
	for _, s := range devices.lastFilter.PairingStatuses {
		if s == fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD {
			hasDefaultPassword = true
			break
		}
	}
	if !hasDefaultPassword {
		t.Fatalf("expected building list stats to include DEFAULT_PASSWORD pairing status; got %v", devices.lastFilter.PairingStatuses)
	}
}

func TestListBuildings_returnsEmptyWhenStatsFilterHasNoAuthorizedRows(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().ListBuildings(gomock.Any(), models.ListFilter{OrgID: testOrgID, IncludeStats: true}).Return([]models.BuildingWithCounts{
		{
			Building:    models.Building{ID: 1, OrgID: testOrgID, Name: "Building 1"},
			RackCount:   2,
			DeviceCount: 1,
		},
	}, nil)

	svc := NewService(store, nil, nil, nil, nil, newTx(), nil)
	rows, err := svc.ListBuildings(context.Background(), models.ListFilter{OrgID: testOrgID, IncludeStats: true}, fleetlistfilter.Filter{
		ErrorComponentTypes: []int32{int32(errorspb.ComponentType_COMPONENT_TYPE_FAN)},
	}, func(*int64) bool { return false })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected stats-filtered request without authorized stats to return no rows, got %+v", rows)
	}
}

func TestListBuildings_returnsErrorWhenTelemetryFilterCannotFetchTelemetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	store.EXPECT().ListBuildings(gomock.Any(), models.ListFilter{OrgID: testOrgID, IncludeStats: true}).Return([]models.BuildingWithCounts{
		{
			Building:    models.Building{ID: 1, OrgID: testOrgID, Name: "Building 1"},
			RackCount:   2,
			DeviceCount: 1,
		},
	}, nil)

	devices := &fakeDeviceQueryer{deviceIDs: []string{"d1"}}
	telemetry := &fakeTelemetryCollector{err: errors.New("telemetry unavailable")}
	svc := NewService(store, nil, nil, devices, telemetry, newTx(), nil)

	minHashrate := 1.0
	_, err := svc.ListBuildings(context.Background(), models.ListFilter{OrgID: testOrgID, IncludeStats: true}, fleetlistfilter.Filter{
		TelemetryRanges: []interfaces.NumericRange{{
			Field:        interfaces.NumericFilterFieldHashrateTHs,
			Min:          &minHashrate,
			MinInclusive: true,
		}},
	}, func(*int64) bool { return true })
	if err == nil {
		t.Fatal("expected telemetry fetch error for telemetry-filtered list")
	}
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeInternal {
		t.Fatalf("expected Internal error, got %v", err)
	}
}
