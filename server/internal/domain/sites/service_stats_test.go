package sites

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"go.uber.org/mock/gomock"

	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// fakeDeviceQueryer is a hand-rolled implementation of
// devicerollup.DeviceQueryer. Hand-rolled (vs mockgen) keeps the test
// file from pulling another generated mock package; the surface is
// three methods so the assertion overhead is trivial.
type fakeDeviceQueryer struct {
	deviceIDs       []string
	deviceIDsErr    error
	lastFilter      *interfaces.MinerFilter
	stateCounts     interfaces.MinerStateCounts
	stateCountErr   error
	collections     map[int64]interfaces.MinerStateCounts
	collErr         error
	componentCounts []interfaces.ComponentErrorCount
	componentErr    error
}

func (f *fakeDeviceQueryer) GetDeviceIdentifiersByOrgWithFilter(_ context.Context, _ int64, filter *interfaces.MinerFilter) ([]string, error) {
	f.lastFilter = filter
	return f.deviceIDs, f.deviceIDsErr
}

func (f *fakeDeviceQueryer) GetMinerStateCountsByDeviceIDs(_ context.Context, _ int64, _ []string) (interfaces.MinerStateCounts, error) {
	return f.stateCounts, f.stateCountErr
}

func (f *fakeDeviceQueryer) GetMinerStateCountsByCollections(_ context.Context, _ int64, _ []int64) (map[int64]interfaces.MinerStateCounts, error) {
	return f.collections, f.collErr
}

func (f *fakeDeviceQueryer) GetComponentErrorCounts(_ context.Context, _ int64, _ interfaces.ComponentErrorScope) ([]interfaces.ComponentErrorCount, error) {
	return f.componentCounts, f.componentErr
}

// fakeTelemetryCollector is a hand-rolled
// devicerollup.TelemetryCollector. Returns a fixed map regardless of
// the device-id list; tests pass a `metrics` map that matches the
// identifiers expected for the rollup.
type fakeTelemetryCollector struct {
	metrics map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics
	err     error
}

func (f *fakeTelemetryCollector) GetLatestDeviceMetrics(_ context.Context, _ []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	return f.metrics, f.err
}

func floatPtr(v float64) *modelsV2.MetricValue { return &modelsV2.MetricValue{Value: v} }

func TestGetSiteStats_notFoundWhenSiteMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	store.EXPECT().SiteBelongsToOrg(gomock.Any(), testOrgID, int64(99)).Return(false, nil)

	svc := NewService(store, mocks.NewMockBuildingStore(ctrl), nil, &fakeDeviceQueryer{}, &fakeTelemetryCollector{}, &fakeTransactor{}, nil)
	_, err := svc.GetSiteStats(context.Background(), testOrgID, 99)
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeNotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestGetSiteStats_rollsUpEverything(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	store.EXPECT().SiteBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().CountBuildingsBySite(gomock.Any(), testOrgID, int64(1)).Return(int64(3), nil)
	store.EXPECT().CountRacksBySite(gomock.Any(), testOrgID, int64(1)).Return(int64(2), nil)

	buildingStore := mocks.NewMockBuildingStore(ctrl)

	devices := &fakeDeviceQueryer{
		deviceIDs: []string{"d1", "d2", "d3"},
		stateCounts: interfaces.MinerStateCounts{
			HashingCount:  2,
			BrokenCount:   1,
			OfflineCount:  0,
			SleepingCount: 0,
		},
	}
	telemetry := &fakeTelemetryCollector{
		metrics: map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics{
			"d1": {
				HashrateHS:   floatPtr(100e12), // 100 TH/s → 100 hashes-per-second × 1e12
				PowerW:       floatPtr(3000),   // 3 kW
				EfficiencyJH: floatPtr(30e-12), // 30 J/TH → joules per hash × 1e-12
			},
			"d2": {
				HashrateHS:   floatPtr(200e12),
				PowerW:       floatPtr(6000),
				EfficiencyJH: floatPtr(25e-12),
			},
			// d3 missing from telemetry → counts as non-reporting
		},
	}

	svc := NewService(store, buildingStore, nil, devices, telemetry, &fakeTransactor{}, nil)
	stats, err := svc.GetSiteStats(context.Background(), testOrgID, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.SiteID != 1 {
		t.Errorf("SiteID: got %d want 1", stats.SiteID)
	}
	if stats.BuildingCount != 3 {
		t.Errorf("BuildingCount: got %d want 3", stats.BuildingCount)
	}
	if stats.RackCount != 2 {
		t.Errorf("RackCount: got %d want 2", stats.RackCount)
	}
	if stats.DeviceCount != 3 {
		t.Errorf("DeviceCount: got %d want 3", stats.DeviceCount)
	}
	if stats.ReportingCount != 2 {
		t.Errorf("ReportingCount: got %d want 2", stats.ReportingCount)
	}
	if stats.HashingCount != 2 || stats.BrokenCount != 1 {
		t.Errorf("state counts plumb-through wrong: hashing=%d broken=%d", stats.HashingCount, stats.BrokenCount)
	}
	if stats.TotalHashrateThs != 300 {
		t.Errorf("TotalHashrateThs: got %g want 300", stats.TotalHashrateThs)
	}
	if stats.TotalPowerKw != 9 {
		t.Errorf("TotalPowerKw: got %g want 9", stats.TotalPowerKw)
	}
	// Floating-point drift: 30+25 = 55 / 2 = 27.5 ± 1e-12 across the
	// hash-to-tera-hash conversions.
	if delta := stats.AvgEfficiencyJth - 27.5; delta > 1e-6 || delta < -1e-6 {
		t.Errorf("AvgEfficiencyJth: got %g want ~27.5", stats.AvgEfficiencyJth)
	}
}

func TestGetSiteStats_includesActionablePairingStatusesInFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	store.EXPECT().SiteBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().CountBuildingsBySite(gomock.Any(), testOrgID, int64(1)).Return(int64(0), nil)
	store.EXPECT().CountRacksBySite(gomock.Any(), testOrgID, int64(1)).Return(int64(0), nil)

	buildingStore := mocks.NewMockBuildingStore(ctrl)

	devices := &fakeDeviceQueryer{deviceIDs: nil} // no devices → telemetry not called
	svc := NewService(store, buildingStore, nil, devices, &fakeTelemetryCollector{}, &fakeTransactor{}, nil)
	_, err := svc.GetSiteStats(context.Background(), testOrgID, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if devices.lastFilter == nil {
		t.Fatal("expected filter to be passed to GetDeviceIdentifiersByOrgWithFilter")
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
	if devices.lastFilter.Limit != MaxDevicesPerSiteStatsRequest+1 {
		t.Errorf("expected SQL-level Limit=cap+1 (%d); got %d", MaxDevicesPerSiteStatsRequest+1, devices.lastFilter.Limit)
	}
}

func TestGetSiteStats_failsFastOverCap(t *testing.T) {
	// When the SQL returns cap+1 identifiers we treat it as over-cap and
	// bail without fanning out to the state/telemetry queries. The Limit
	// in the filter is what kept the query bounded in the first place;
	// the service just enforces the threshold.
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	store.EXPECT().SiteBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().CountBuildingsBySite(gomock.Any(), testOrgID, int64(1)).Return(int64(0), nil)
	store.EXPECT().CountRacksBySite(gomock.Any(), testOrgID, int64(1)).Return(int64(0), nil)
	buildingStore := mocks.NewMockBuildingStore(ctrl)

	overCap := make([]string, MaxDevicesPerSiteStatsRequest+1)
	for i := range overCap {
		overCap[i] = "d"
	}
	telemetry := &fakeTelemetryCollector{err: errors.New("should not be called")}
	devices := &fakeDeviceQueryer{deviceIDs: overCap}

	svc := NewService(store, buildingStore, nil, devices, telemetry, &fakeTransactor{}, nil)
	_, err := svc.GetSiteStats(context.Background(), testOrgID, 1)
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeInternal {
		t.Fatalf("expected Internal over-cap error; got %v", err)
	}
}

func TestGetSiteStats_emptyDevicesShortCircuits(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	store.EXPECT().SiteBelongsToOrg(gomock.Any(), testOrgID, int64(1)).Return(true, nil)
	store.EXPECT().CountBuildingsBySite(gomock.Any(), testOrgID, int64(1)).Return(int64(0), nil)
	store.EXPECT().CountRacksBySite(gomock.Any(), testOrgID, int64(1)).Return(int64(0), nil)
	buildingStore := mocks.NewMockBuildingStore(ctrl)

	// Telemetry should never fire when device list is empty — set err so
	// any unexpected call would surface.
	telemetry := &fakeTelemetryCollector{err: errors.New("should not be called")}
	devices := &fakeDeviceQueryer{deviceIDs: nil}

	svc := NewService(store, buildingStore, nil, devices, telemetry, &fakeTransactor{}, nil)
	stats, err := svc.GetSiteStats(context.Background(), testOrgID, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.DeviceCount != 0 || stats.ReportingCount != 0 {
		t.Errorf("expected zero counts; got device=%d reporting=%d", stats.DeviceCount, stats.ReportingCount)
	}
}

func TestGetSiteStats_internalErrorWhenStatsDepsMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	// Nil deviceQueryer should short-circuit before any store call.
	svc := NewService(store, nil, nil, nil, nil, &fakeTransactor{}, nil)
	_, err := svc.GetSiteStats(context.Background(), testOrgID, 1)
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeInternal {
		t.Fatalf("expected Internal error; got %v", err)
	}
}

func TestListSites_degradesWhenListTelemetryFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	store.EXPECT().ListSites(gomock.Any(), testOrgID).Return([]models.SiteWithCounts{
		{
			Site:          models.Site{ID: 1, OrgID: testOrgID, Name: "Site 1"},
			BuildingCount: 2,
			RackCount:     3,
		},
	}, nil)

	devices := &fakeDeviceQueryer{
		deviceIDs: []string{"d1"},
		stateCounts: interfaces.MinerStateCounts{
			HashingCount: 1,
		},
		componentCounts: []interfaces.ComponentErrorCount{
			{ScopeID: 1, ComponentType: 3, DeviceCount: 1},
		},
	}
	telemetry := &fakeTelemetryCollector{err: errors.New("telemetry unavailable")}
	svc := NewService(store, nil, nil, devices, telemetry, &fakeTransactor{}, nil)

	rows, err := svc.ListSites(context.Background(), testOrgID, fleetlistfilter.Filter{}, func(int64) bool { return true })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ListStats == nil {
		t.Fatalf("expected one row with list stats, got %+v", rows)
	}
	stats := rows[0].ListStats
	if stats.BuildingCount != 2 || stats.RackCount != 3 || stats.DeviceCount != 1 {
		t.Fatalf("structural counts not preserved after telemetry failure: %+v", stats)
	}
	if stats.HashingCount != 1 || stats.FanIssueCount != 1 {
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
		t.Fatalf("expected site list stats to include DEFAULT_PASSWORD pairing status; got %v", devices.lastFilter.PairingStatuses)
	}
}

func TestListSites_returnsEmptyWhenStatsFilterHasNoAuthorizedRows(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	store.EXPECT().ListSites(gomock.Any(), testOrgID).Return([]models.SiteWithCounts{
		{
			Site:          models.Site{ID: 1, OrgID: testOrgID, Name: "Site 1"},
			BuildingCount: 2,
			RackCount:     3,
		},
	}, nil)

	svc := NewService(store, nil, nil, nil, nil, &fakeTransactor{}, nil)
	rows, err := svc.ListSites(context.Background(), testOrgID, fleetlistfilter.Filter{
		ErrorComponentTypes: []int32{int32(errorspb.ComponentType_COMPONENT_TYPE_FAN)},
	}, func(int64) bool { return false })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected stats-filtered request without authorized stats to return no rows, got %+v", rows)
	}
}

func TestListSites_returnsErrorWhenTelemetryFilterCannotFetchTelemetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	store.EXPECT().ListSites(gomock.Any(), testOrgID).Return([]models.SiteWithCounts{
		{
			Site:          models.Site{ID: 1, OrgID: testOrgID, Name: "Site 1"},
			BuildingCount: 2,
			RackCount:     3,
		},
	}, nil)

	devices := &fakeDeviceQueryer{deviceIDs: []string{"d1"}}
	telemetry := &fakeTelemetryCollector{err: errors.New("telemetry unavailable")}
	svc := NewService(store, nil, nil, devices, telemetry, &fakeTransactor{}, nil)

	minHashrate := 1.0
	_, err := svc.ListSites(context.Background(), testOrgID, fleetlistfilter.Filter{
		TelemetryRanges: []interfaces.NumericRange{{
			Field:        interfaces.NumericFilterFieldHashrateTHs,
			Min:          &minHashrate,
			MinInclusive: true,
		}},
	}, func(int64) bool { return true })
	if err == nil {
		t.Fatal("expected telemetry fetch error for telemetry-filtered list")
	}
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) || fe.GRPCCode != connect.CodeInternal {
		t.Fatalf("expected Internal error, got %v", err)
	}
}
