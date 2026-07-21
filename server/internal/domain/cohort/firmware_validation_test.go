package cohort

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

type fakeValidationTelemetryProvider struct {
	queries []telemetrymodels.CombinedMetricsQuery
	get     func(telemetrymodels.CombinedMetricsQuery) telemetrymodels.CombinedMetric
}

func (p *fakeValidationTelemetryProvider) GetCombinedMetrics(_ context.Context, query telemetrymodels.CombinedMetricsQuery) (telemetrymodels.CombinedMetric, error) {
	p.queries = append(p.queries, query)
	if p.get == nil {
		return telemetrymodels.CombinedMetric{}, nil
	}
	return p.get(query), nil
}

func TestGetCohortFirmwareValidation_GroupsEligibleMembersAndComparesWindows(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	telemetry := &fakeValidationTelemetryProvider{}
	svc := NewService(
		store,
		WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
			"fw-2": {FirmwareVersion: "2.0.0", TargetManufacturer: "Proto", TargetModel: "Rig"},
		}),
		WithValidationTelemetryProvider(telemetry),
	)
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	rollout := now.Add(-24 * time.Hour)
	svc.now = func() time.Time { return now }
	fileID := "fw-2"

	members := []models.CohortMember{
		validationMember("miner-1", rollout.Add(-time.Hour), fileID, "2.0.0", true),
		validationMember("miner-2", rollout.Add(-time.Hour), fileID, "2.0.0", true),
		validationMember("incomplete", rollout.Add(-time.Hour), fileID, "1.0.0", false),
		validationMember("added-later", rollout.Add(time.Minute), fileID, "2.0.0", true),
		validationMember("already-target", rollout.Add(-time.Hour), fileID, "2.0.0", true),
		validationMember("unknown", rollout.Add(-time.Hour), fileID, "2.0.0", true),
		validationMember("stabilizing", rollout.Add(-time.Hour), fileID, "2.0.0", true),
		validationMember("miner-3", rollout.Add(-time.Hour), fileID, "2.0.0", true),
		validationMember("untrusted", rollout.Add(-time.Hour), fileID, "2.0.0", true),
	}
	cohort := &models.Cohort{
		ID: 42, OrgID: 7, Label: "validation", State: models.CohortStateActive,
		Members: members,
		FirmwareTargets: []models.CohortFirmwareTarget{{
			CohortID: 42, OrgID: 7, Manufacturer: "Proto", Model: "Rig",
			FirmwareFileID: &fileID, UpdatedAt: rollout,
		}},
	}
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(cohort, nil)
	events := []models.FirmwareVersionEvent{
		validationEvent("miner-1", "1.0.0", rollout.Add(-time.Hour)),
		validationEvent("miner-2", "1.0.0", rollout.Add(-time.Hour)),
		validationEvent("incomplete", "1.0.0", rollout.Add(-time.Hour)),
		validationEvent("added-later", "1.0.0", rollout.Add(-time.Hour)),
		validationEvent("already-target", "2.0.0", rollout.Add(-time.Hour)),
		validationEvent("stabilizing", "1.0.0", rollout.Add(-time.Hour)),
		validationEvent("miner-3", "1.1.0", rollout.Add(-time.Hour)),
		validationEvent("untrusted", "1.0.0", rollout.Add(-time.Hour)),
		validationEvent("miner-1", "2.0.0", rollout.Add(time.Hour)),
		validationEvent("miner-2", "2.0.0", rollout.Add(2*time.Hour)),
		validationEvent("added-later", "2.0.0", rollout.Add(3*time.Hour)),
		validationEvent("miner-3", "2.0.0", rollout.Add(4*time.Hour)),
		validationEvent("stabilizing", "2.0.0", now.Add(-30*time.Minute)),
	}
	store.EXPECT().ListCohortFirmwareVersionEvents(gomock.Any(), int64(7), int64(42), rollout, now).Return(events, nil)

	telemetry.get = func(query telemetrymodels.CombinedMetricsQuery) telemetrymodels.CombinedMetric {
		value := 100.0
		if query.TimeRange.EndTime != nil && !query.TimeRange.EndTime.Equal(rollout) {
			value = 110
		}
		return validationMetricResponse(query, value)
	}

	result, err := svc.GetCohortFirmwareValidation(t.Context(), models.CohortFirmwareValidationParams{
		OrgID: 7, CohortID: 42, Manufacturer: "proto", Model: "rig",
		Window: models.CohortFirmwareValidationWindowOneHour,
	})
	require.NoError(t, err)
	assert.Equal(t, models.CohortFirmwareValidationStateAvailable, result.State)
	assert.Equal(t, int32(9), result.TargetedCount)
	assert.Equal(t, int32(8), result.CompleteCount)
	assert.True(t, result.Preliminary)
	assert.Equal(t, models.CohortFirmwareValidationExclusions{
		AddedAfterRolloutCount: 1, UnknownBaselineCount: 1, AlreadyOnTargetCount: 1,
		IncompleteCount: 1, StabilizingCount: 1, UntrustedTransitionCount: 1,
	}, result.Exclusions)
	require.Len(t, result.Baselines, 2)
	assert.Equal(t, "1.0.0", result.Baselines[0].PreviousFirmwareVersion)
	assert.Equal(t, int32(5), result.Baselines[0].MemberCount)
	assert.Equal(t, int32(2), result.Baselines[0].EligibleCount)
	assert.Equal(t, rollout.Add(2*time.Hour+15*time.Minute), result.Baselines[0].TargetStartTime)
	assert.Equal(t, "1.1.0", result.Baselines[1].PreviousFirmwareVersion)
	assert.Equal(t, int32(1), result.Baselines[1].EligibleCount)
	require.Len(t, telemetry.queries, 4)
	for _, query := range telemetry.queries {
		assert.Equal(t, int64(7), query.OrganizationID)
		assert.Equal(t, telemetrymodels.CombinedMetricsResolutionRaw, query.Resolution)
		assert.Equal(t, 5*time.Minute, *query.SlideInterval)
		assert.ElementsMatch(t, []telemetrymodels.MeasurementType{
			telemetrymodels.MeasurementTypeHashrate,
			telemetrymodels.MeasurementTypeEfficiency,
			telemetrymodels.MeasurementTypePower,
		}, query.MeasurementTypes)
	}
	metric := result.Baselines[0].Metrics[0]
	require.NotNil(t, metric.BaselineAverage)
	require.NotNil(t, metric.TargetAverage)
	require.NotNil(t, metric.PercentageDelta)
	assert.Equal(t, 100.0, *metric.BaselineAverage)
	assert.Equal(t, 110.0, *metric.TargetAverage)
	assert.InDelta(t, 10.0, *metric.PercentageDelta, 0.001)
	var powerMetric *models.CohortFirmwareValidationMetric
	for index := range result.Baselines[0].Metrics {
		if result.Baselines[0].Metrics[index].MeasurementType == telemetrymodels.MeasurementTypePower {
			powerMetric = &result.Baselines[0].Metrics[index]
			break
		}
	}
	require.NotNil(t, powerMetric)
	require.NotNil(t, powerMetric.BaselineAverage)
	assert.Equal(t, 50.0, *powerMetric.BaselineAverage, "power is normalized to an average per eligible miner")
}

func TestGetCohortFirmwareValidation_UsesHourlyResolutionForOlderRollout(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	telemetry := &fakeValidationTelemetryProvider{get: func(query telemetrymodels.CombinedMetricsQuery) telemetrymodels.CombinedMetric {
		return validationMetricResponse(query, 100)
	}}
	svc := NewService(
		store,
		WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{"fw-2": {FirmwareVersion: "2.0.0"}}),
		WithValidationTelemetryProvider(telemetry),
	)
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	rollout := now.Add(-20 * 24 * time.Hour)
	svc.now = func() time.Time { return now }
	fileID := "fw-2"
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(&models.Cohort{
		ID: 42, OrgID: 7, State: models.CohortStateActive,
		FirmwareTargets: []models.CohortFirmwareTarget{{Manufacturer: "Proto", Model: "Rig", FirmwareFileID: &fileID, UpdatedAt: rollout}},
		Members:         []models.CohortMember{validationMember("miner-1", rollout.Add(-time.Hour), fileID, "2.0.0", true)},
	}, nil)
	store.EXPECT().ListCohortFirmwareVersionEvents(gomock.Any(), int64(7), int64(42), rollout, now).Return([]models.FirmwareVersionEvent{
		validationEvent("miner-1", "1.0.0", rollout.Add(-time.Hour)),
		validationEvent("miner-1", "2.0.0", rollout.Add(time.Hour)),
	}, nil)

	result, err := svc.GetCohortFirmwareValidation(t.Context(), models.CohortFirmwareValidationParams{
		OrgID: 7, CohortID: 42, Manufacturer: "Proto", Model: "Rig",
		Window: models.CohortFirmwareValidationWindowSixHours,
	})
	require.NoError(t, err)
	assert.Equal(t, models.CohortFirmwareValidationTelemetryResolutionHourly, result.TelemetryResolution)
	assert.Equal(t, time.Hour, result.ChartGranularity)
	require.Len(t, telemetry.queries, 2)
	for _, query := range telemetry.queries {
		assert.Equal(t, telemetrymodels.CombinedMetricsResolutionHourly, query.Resolution)
		assert.Equal(t, time.Hour, *query.SlideInterval)
	}
}

func TestGetCohortFirmwareValidation_ReturnsExpiredWithoutTelemetryQueries(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	telemetry := &fakeValidationTelemetryProvider{}
	svc := NewService(
		store,
		WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{"fw-2": {FirmwareVersion: "2.0.0"}}),
		WithValidationTelemetryProvider(telemetry),
	)
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	rollout := now.AddDate(0, -4, 0)
	svc.now = func() time.Time { return now }
	fileID := "fw-2"
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(&models.Cohort{
		ID: 42, OrgID: 7, State: models.CohortStateActive,
		FirmwareTargets: []models.CohortFirmwareTarget{{Manufacturer: "Proto", Model: "Rig", FirmwareFileID: &fileID, UpdatedAt: rollout}},
		Members:         []models.CohortMember{validationMember("miner-1", rollout.Add(-time.Hour), fileID, "2.0.0", true)},
	}, nil)
	store.EXPECT().ListCohortFirmwareVersionEvents(gomock.Any(), int64(7), int64(42), rollout, now).Return([]models.FirmwareVersionEvent{
		validationEvent("miner-1", "1.0.0", rollout.Add(-time.Hour)),
		validationEvent("miner-1", "2.0.0", rollout.Add(time.Hour)),
	}, nil)

	result, err := svc.GetCohortFirmwareValidation(t.Context(), models.CohortFirmwareValidationParams{
		OrgID: 7, CohortID: 42, Manufacturer: "Proto", Model: "Rig",
		Window: models.CohortFirmwareValidationWindowTwentyFourHours,
	})
	require.NoError(t, err)
	assert.Equal(t, models.CohortFirmwareValidationStateHistoryExpired, result.State)
	require.Len(t, result.Baselines, 1)
	assert.Equal(t, models.CohortFirmwareValidationStateHistoryExpired, result.Baselines[0].State)
	assert.Empty(t, telemetry.queries)
}

func TestBuildFirmwareValidationMetrics_OmitsPercentageForZeroBaseline(t *testing.T) {
	start := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	baseline := telemetrymodels.CombinedMetric{Metrics: []telemetrymodels.Metric{{
		MeasurementType:  telemetrymodels.MeasurementTypeHashrate,
		OpenTime:         start,
		DeviceCount:      1,
		AggregatedValues: []telemetrymodels.AggregatedValue{{Type: telemetrymodels.AggregationTypeAverage, Value: 0}},
	}}}
	target := telemetrymodels.CombinedMetric{Metrics: []telemetrymodels.Metric{{
		MeasurementType:  telemetrymodels.MeasurementTypeHashrate,
		OpenTime:         start,
		DeviceCount:      1,
		AggregatedValues: []telemetrymodels.AggregatedValue{{Type: telemetrymodels.AggregationTypeAverage, Value: 10}},
	}}}

	metrics, state := buildFirmwareValidationMetrics(baseline, target, start, start)
	assert.Equal(t, models.CohortFirmwareValidationStateAvailable, state)
	require.Len(t, metrics, 3)
	require.NotNil(t, metrics[0].AbsoluteDelta)
	assert.Equal(t, 10.0, *metrics[0].AbsoluteDelta)
	assert.Nil(t, metrics[0].PercentageDelta)
}

func TestFirmwareValidationTransitions_UsesOnlyPreAssignmentEventForBaseline(t *testing.T) {
	rollout := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	events := []models.FirmwareVersionEvent{
		validationEvent("miner-1", "1.0.0", rollout.Add(-time.Minute)),
		validationEvent("miner-1", "1.5.0", rollout),
		validationEvent("miner-1", "2.0.0", rollout.Add(time.Hour)),
	}

	baseline, transition, trustworthy := firmwareValidationTransitions(events, rollout, "2.0.0")

	assert.Equal(t, "1.0.0", baseline)
	assert.Equal(t, rollout.Add(time.Hour), transition)
	assert.True(t, trustworthy)
}

func validationMember(deviceID string, addedAt time.Time, fileID, currentVersion string, complete bool) models.CohortMember {
	status := &models.CohortFirmwareStatus{
		DeviceIdentifier:       deviceID,
		TargetFirmwareFileID:   fileID,
		TargetFirmwareVersion:  "2.0.0",
		CurrentFirmwareVersion: currentVersion,
	}
	if complete {
		status.State = models.CohortFirmwareRolloutStateComplete
	}
	return models.CohortMember{
		CohortID: 42, OrgID: 7, DeviceIdentifier: deviceID, AddedAt: addedAt,
		Display:        models.CohortDeviceDisplay{Manufacturer: "Proto", Model: "Rig", FirmwareVersion: currentVersion},
		FirmwareStatus: status,
	}
}

func validationEvent(deviceID, version string, observedAt time.Time) models.FirmwareVersionEvent {
	return models.FirmwareVersionEvent{DeviceIdentifier: deviceID, FirmwareVersion: version, ObservedAt: observedAt}
}

func validationMetricResponse(query telemetrymodels.CombinedMetricsQuery, value float64) telemetrymodels.CombinedMetric {
	start := *query.TimeRange.StartTime
	metrics := make([]telemetrymodels.Metric, 0, len(query.MeasurementTypes)*2)
	for _, measurementType := range query.MeasurementTypes {
		for index := range 2 {
			metrics = append(metrics, telemetrymodels.Metric{
				MeasurementType: measurementType,
				OpenTime:        start.Add(time.Duration(index) * *query.SlideInterval),
				DeviceCount:     int32(len(query.DeviceIDs)), //nolint:gosec // test fixtures contain only a handful of devices
				AggregatedValues: []telemetrymodels.AggregatedValue{{
					Type: telemetrymodels.AggregationTypeAverage, Value: value,
				}},
			})
		}
	}
	return telemetrymodels.CombinedMetric{Metrics: metrics}
}

var _ ValidationTelemetryProvider = (*fakeValidationTelemetryProvider)(nil)
