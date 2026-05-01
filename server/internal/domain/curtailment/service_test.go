package curtailment

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestPreviewCurtailmentPlan_FixedKWFiltersRanksAndSelects(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	missingStatus := previewDevice("missing-status", 2000, 2000, 100, ptr(60), now)
	missingStatus.DeviceStatus = nil
	unknownStatus := previewDevice("unknown-status", 2000, 2000, 100, ptr(58), now)
	unknownStatusValue := "UNKNOWN"
	unknownStatus.DeviceStatus = &unknownStatusValue
	activeCurtailment := previewDevice("active-curtailment", 2000, 2000, 100, ptr(57), now)
	activeCurtailment.InActiveCurtailment = true
	store := &fakeCurtailmentStore{devices: []interfaces.CurtailmentPreviewDevice{
		previewDevice("worse", 2100, 2100, 100, ptr(35), now),
		previewDevice("better", 1800, 1800, 100, ptr(25), now),
		previewDevice("unknown-efficiency", 1900, 1900, 100, nil, now),
		stalePreviewDevice("stale"),
		previewDevice("phantom", 1600, 1600, 0, ptr(40), now),
		previewDevice("current-low", 100, 2000, 100, ptr(45), now),
		previewDevice("bad-power", 100, 100, 100, ptr(45), now),
		missingStatus,
		unknownStatus,
		activeCurtailment,
		previewDevice("unsupported", 2000, 2000, 100, ptr(50), now),
		cooldownPreviewDevice("cooldown", 2200, 2200, 100, ptr(55), now),
	}}
	service := NewService(store, fakeCapabilitiesProvider{unsupported: map[string]bool{"unsupported": true}}, Config{})
	service.now = func() time.Time { return now }

	resp, err := service.PreviewCurtailmentPlan(authCtx(t), fixedKWRequest(3.9, nil))

	require.NoError(t, err)
	require.Len(t, resp.Candidates, 2)
	assert.Equal(t, "worse", resp.Candidates[0].DeviceIdentifier)
	assert.Equal(t, "better", resp.Candidates[1].DeviceIdentifier)
	assert.InDelta(t, 3.9, resp.EstimatedReductionKw, 0.0001)
	assert.InDelta(t, 1.9, resp.EstimatedRemainingPowerKw, 0.0001)
	assert.Equal(t, pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW, resp.Mode)
	require.NotNil(t, resp.GetFixedKw())
	assert.Equal(t, 3.9, resp.GetFixedKw().TargetKw)

	reasons := skippedReasons(resp.SkippedCandidates)
	assert.Equal(t, reasonStale, reasons["stale"])
	assert.Equal(t, reasonPhantomLoadNoHash, reasons["phantom"])
	assert.Equal(t, reasonPowerTelemetryUnreliable, reasons["current-low"])
	assert.Equal(t, reasonPowerTelemetryUnreliable, reasons["bad-power"])
	assert.Equal(t, reasonUnreachableResidualLoad, reasons["missing-status"])
	assert.Equal(t, reasonUnreachableResidualLoad, reasons["unknown-status"])
	assert.Equal(t, reasonActiveCurtailment, reasons["active-curtailment"])
	assert.Equal(t, reasonCurtailFullUnsupported, reasons["unsupported"])
	assert.Equal(t, reasonCooldown, reasons["cooldown"])
}

func TestPreviewCurtailmentPlan_FixedKWToleranceSemantics(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	devices := []interfaces.CurtailmentPreviewDevice{
		previewDevice("one", 2000, 2000, 100, ptr(40), now),
		previewDevice("two", 2000, 2000, 100, ptr(30), now),
	}

	t.Run("omitted tolerance is strict", func(t *testing.T) {
		t.Parallel()
		service := newPreviewTestService(devices, now)
		req := fixedKWRequest(5, nil)

		_, err := service.PreviewCurtailmentPlan(authCtx(t), req)

		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("explicit zero tolerance is strict", func(t *testing.T) {
		t.Parallel()
		service := newPreviewTestService(devices, now)
		zero := 0.0
		req := fixedKWRequest(5, &zero)

		_, err := service.PreviewCurtailmentPlan(authCtx(t), req)

		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("positive tolerance accepts near miss", func(t *testing.T) {
		t.Parallel()
		service := newPreviewTestService(devices, now)
		tolerance := 1.0
		req := fixedKWRequest(5, &tolerance)

		resp, err := service.PreviewCurtailmentPlan(authCtx(t), req)

		require.NoError(t, err)
		require.Len(t, resp.Candidates, 2)
		assert.InDelta(t, 4.0, resp.EstimatedReductionKw, 0.0001)
		require.NotNil(t, resp.GetFixedKw())
		require.NotNil(t, resp.GetFixedKw().ToleranceKw)
		assert.Equal(t, 1.0, resp.GetFixedKw().GetToleranceKw())
	})
}

func TestPreviewCurtailmentPlan_FixedKWOvershootsWithAtomicMiners(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	service := newPreviewTestService([]interfaces.CurtailmentPreviewDevice{
		previewDevice("first", 2000, 2000, 100, ptr(40), now),
		previewDevice("second", 1000, 1000, 100, ptr(30), now),
		previewDevice("third", 1000, 1000, 100, ptr(20), now),
	}, now)

	resp, err := service.PreviewCurtailmentPlan(authCtx(t), fixedKWRequest(2.5, nil))

	require.NoError(t, err)
	require.Len(t, resp.Candidates, 2)
	assert.Equal(t, "first", resp.Candidates[0].DeviceIdentifier)
	assert.Equal(t, "second", resp.Candidates[1].DeviceIdentifier)
	assert.InDelta(t, 3.0, resp.EstimatedReductionKw, 0.0001)
	assert.InDelta(t, 1.0, resp.EstimatedRemainingPowerKw, 0.0001)
	require.NotNil(t, resp.GetFixedKw())
	require.NotNil(t, resp.GetFixedKw().ToleranceKw)
	assert.Equal(t, 0.0, resp.GetFixedKw().GetToleranceKw())
}

func TestPreviewCurtailmentPlan_ExplicitDeviceScopeRequiresOrgOwnedDevices(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	store := &fakeCurtailmentStore{devices: []interfaces.CurtailmentPreviewDevice{
		previewDevice("owned", 2000, 2000, 100, ptr(40), now),
	}}
	service := NewService(store, fakeCapabilitiesProvider{}, Config{})
	service.now = func() time.Time { return now }
	req := fixedKWRequest(1, nil)
	req.Scope = &pb.PreviewCurtailmentPlanRequest_DeviceIdentifiers{
		DeviceIdentifiers: &pb.ScopeDeviceList{DeviceIdentifiers: []string{"owned", "missing"}},
	}

	_, err := service.PreviewCurtailmentPlan(authCtx(t), req)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Equal(t, interfaces.CurtailmentScopeDeviceList, store.params.ScopeType)
	assert.Equal(t, []string{"owned", "missing"}, store.params.DeviceIdentifiers)
}

func TestPreviewCurtailmentPlan_DeviceSetScopeRequiresOrgOwnedSets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	t.Run("rejects unresolved set IDs", func(t *testing.T) {
		t.Parallel()

		store := &fakeCurtailmentStore{
			devices:           []interfaces.CurtailmentPreviewDevice{previewDevice("owned", 2000, 2000, 100, ptr(40), now)},
			validDeviceSetIDs: []int64{101},
		}
		service := NewService(store, fakeCapabilitiesProvider{}, Config{})
		service.now = func() time.Time { return now }
		req := fixedKWRequest(1, nil)
		req.Scope = &pb.PreviewCurtailmentPlanRequest_DeviceSetIds{
			DeviceSetIds: &pb.ScopeDeviceSets{DeviceSetIds: []string{"101", "999"}},
		}

		_, err := service.PreviewCurtailmentPlan(authCtx(t), req)

		require.Error(t, err)
		assert.True(t, fleeterror.IsInvalidArgumentError(err))
		assert.Equal(t, int64(20), store.validatedOrgID)
		assert.Equal(t, []int64{101, 999}, store.validatedDeviceSetIDs)
		assert.False(t, store.listPreviewCalled)
	})

	t.Run("accepts duplicate resolved set IDs", func(t *testing.T) {
		t.Parallel()

		store := &fakeCurtailmentStore{
			devices:           []interfaces.CurtailmentPreviewDevice{previewDevice("owned", 2000, 2000, 100, ptr(40), now)},
			validDeviceSetIDs: []int64{101},
		}
		service := NewService(store, fakeCapabilitiesProvider{}, Config{})
		service.now = func() time.Time { return now }
		req := fixedKWRequest(1, nil)
		req.Scope = &pb.PreviewCurtailmentPlanRequest_DeviceSetIds{
			DeviceSetIds: &pb.ScopeDeviceSets{DeviceSetIds: []string{"101", "101"}},
		}

		resp, err := service.PreviewCurtailmentPlan(authCtx(t), req)

		require.NoError(t, err)
		require.Len(t, resp.Candidates, 1)
		assert.Equal(t, []int64{101, 101}, store.params.DeviceSetIDs)
		assert.True(t, store.listPreviewCalled)
	})
}

func TestPreviewCurtailmentPlan_ActiveCurtailmentSkippedForEmergency(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	active := previewDevice("active-curtailment", 3000, 3000, 100, ptr(50), now)
	active.InActiveCurtailment = true
	service := newPreviewTestService([]interfaces.CurtailmentPreviewDevice{
		active,
		previewDevice("available", 2000, 2000, 100, ptr(40), now),
	}, now)
	req := fixedKWRequest(1, nil)
	req.Priority = pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY

	resp, err := service.PreviewCurtailmentPlan(authCtx(t), req)

	require.NoError(t, err)
	require.Len(t, resp.Candidates, 1)
	assert.Equal(t, "available", resp.Candidates[0].DeviceIdentifier)
	reasons := skippedReasons(resp.SkippedCandidates)
	assert.Equal(t, reasonActiveCurtailment, reasons["active-curtailment"])
}

func TestPreviewCurtailmentPlan_MaintenanceRequiresExplicitOverride(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	maintenance := previewDevice("maintenance", 2000, 2000, 100, ptr(40), now)
	status := "MAINTENANCE"
	maintenance.DeviceStatus = &status

	t.Run("skips maintenance by default", func(t *testing.T) {
		t.Parallel()
		service := newPreviewTestService([]interfaces.CurtailmentPreviewDevice{maintenance}, now)
		_, err := service.PreviewCurtailmentPlan(authCtx(t), fixedKWRequest(1, nil))

		require.Error(t, err)
		assert.True(t, fleeterror.IsInvalidArgumentError(err))
	})

	t.Run("includes maintenance with forced override", func(t *testing.T) {
		t.Parallel()
		service := newPreviewTestService([]interfaces.CurtailmentPreviewDevice{maintenance}, now)
		req := fixedKWRequest(1, nil)
		req.IncludeMaintenance = true
		req.ForceIncludeMaintenance = true

		resp, err := service.PreviewCurtailmentPlan(authCtx(t), req)

		require.NoError(t, err)
		require.Len(t, resp.Candidates, 1)
		assert.Equal(t, "maintenance", resp.Candidates[0].DeviceIdentifier)
	})
}

func newPreviewTestService(devices []interfaces.CurtailmentPreviewDevice, now time.Time) *Service {
	service := NewService(&fakeCurtailmentStore{devices: devices}, fakeCapabilitiesProvider{}, Config{CandidateMinPowerW: 1})
	service.now = func() time.Time { return now }
	return service
}

func authCtx(t *testing.T) context.Context {
	t.Helper()
	return testutil.MockAuthContextForTesting(t.Context(), 10, 20)
}

func fixedKWRequest(target float64, tolerance *float64) *pb.PreviewCurtailmentPlanRequest {
	return &pb.PreviewCurtailmentPlanRequest{
		Scope: &pb.PreviewCurtailmentPlanRequest_WholeOrg{
			WholeOrg: &pb.ScopeWholeOrg{},
		},
		Mode: pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		ModeParams: &pb.PreviewCurtailmentPlanRequest_FixedKw{
			FixedKw: &pb.FixedKwParams{
				TargetKw:    target,
				ToleranceKw: tolerance,
			},
		},
	}
}

func previewDevice(id string, currentPowerW, recentPowerW, recentHashRateHS float64, efficiencyJH *float64, now time.Time) interfaces.CurtailmentPreviewDevice {
	status := "ACTIVE"
	return interfaces.CurtailmentPreviewDevice{
		DeviceID:         1,
		DeviceIdentifier: id,
		Manufacturer:     "Bitmain",
		Model:            "S19",
		DriverName:       "antminer",
		PairingStatus:    "PAIRED",
		DeviceStatus:     &status,
		LatestMetricAt:   &now,
		CurrentPowerW:    &currentPowerW,
		RecentPowerW:     &recentPowerW,
		RecentHashRateHS: &recentHashRateHS,
		EfficiencyJH:     efficiencyJH,
	}
}

func stalePreviewDevice(id string) interfaces.CurtailmentPreviewDevice {
	device := previewDevice(id, 0, 0, 0, nil, time.Time{})
	device.LatestMetricAt = nil
	device.CurrentPowerW = nil
	device.RecentPowerW = nil
	device.RecentHashRateHS = nil
	return device
}

func cooldownPreviewDevice(id string, currentPowerW, recentPowerW, recentHashRateHS float64, efficiencyJH *float64, now time.Time) interfaces.CurtailmentPreviewDevice {
	device := previewDevice(id, currentPowerW, recentPowerW, recentHashRateHS, efficiencyJH, now)
	device.InCooldown = true
	return device
}

func ptr(v float64) *float64 {
	return &v
}

func skippedReasons(skipped []*pb.SkippedCandidate) map[string]string {
	reasons := make(map[string]string, len(skipped))
	for _, candidate := range skipped {
		reasons[candidate.DeviceIdentifier] = candidate.Reason
	}
	return reasons
}

type fakeCurtailmentStore struct {
	params                interfaces.CurtailmentPreviewDeviceParams
	devices               []interfaces.CurtailmentPreviewDevice
	validDeviceSetIDs     []int64
	validatedOrgID        int64
	validatedDeviceSetIDs []int64
	listPreviewCalled     bool
}

func (s *fakeCurtailmentStore) ListValidDeviceSetIDs(_ context.Context, orgID int64, deviceSetIDs []int64) ([]int64, error) {
	s.validatedOrgID = orgID
	s.validatedDeviceSetIDs = append([]int64(nil), deviceSetIDs...)
	return append([]int64(nil), s.validDeviceSetIDs...), nil
}

func (s *fakeCurtailmentStore) ListPreviewDevices(_ context.Context, params interfaces.CurtailmentPreviewDeviceParams) ([]interfaces.CurtailmentPreviewDevice, error) {
	s.params = params
	s.listPreviewCalled = true
	return s.devices, nil
}

type fakeCapabilitiesProvider struct {
	unsupported map[string]bool
}

func (p fakeCapabilitiesProvider) GetMinerCapabilitiesForDevice(_ context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities {
	supported := !p.unsupported[device.DeviceIdentifier]
	return &capabilitiespb.MinerCapabilities{
		Commands: &capabilitiespb.CommandCapabilities{
			CurtailFullSupported: supported,
		},
	}
}
