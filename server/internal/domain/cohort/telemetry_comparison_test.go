package cohort

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

type comparisonOutcomeProvider struct {
	mu      sync.Mutex
	queries []telemetrymodels.DeviceOutcomeComparisonQuery
	get     func(telemetrymodels.DeviceOutcomeComparisonQuery) []telemetrymodels.DeviceOutcomeAverages
}

func (p *comparisonOutcomeProvider) GetDeviceOutcomeAverages(_ context.Context, query telemetrymodels.DeviceOutcomeComparisonQuery) ([]telemetrymodels.DeviceOutcomeAverages, error) {
	p.mu.Lock()
	p.queries = append(p.queries, query)
	p.mu.Unlock()
	return p.get(query), nil
}

func (p *comparisonOutcomeProvider) allQueries() []telemetrymodels.DeviceOutcomeComparisonQuery {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]telemetrymodels.DeviceOutcomeComparisonQuery(nil), p.queries...)
}

func TestGetCohortTelemetryComparisonUsesEachMinerBaselineAndReportsDistribution(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	provider := &comparisonOutcomeProvider{get: func(query telemetrymodels.DeviceOutcomeComparisonQuery) []telemetrymodels.DeviceOutcomeAverages {
		if len(query.DeviceIDs) != 5 {
			return nil
		}
		return []telemetrymodels.DeviceOutcomeAverages{
			outcomeAverages("miner-1", 100, 110, 20, 18, 1000, 1100),
			outcomeAverages("miner-2", 200, 180, 30, 33, 2000, 1800),
			outcomeAverages("miner-3", 0, 100, 0, 0, 0, 100),
			outcomeAverages("miner-4", 50, 0, 25, 20, 500, 0),
		}
	}}
	svc := NewService(store, WithOutcomeTelemetryProvider(provider))
	svc.now = func() time.Time { return now }

	store.EXPECT().ListCohortTelemetryComparisonMemberships(gomock.Any(), int64(7), []int64{42, 1}).Return(
		[]models.CohortTelemetryComparisonMembership{
			{CohortID: 1, Label: "Default", IsDefault: true, DeviceIdentifiers: []string{"default-1"}},
			{CohortID: 42, Label: "Rollout A", DeviceIdentifiers: []string{"miner-1", "miner-2", "miner-3", "miner-4"}},
		}, nil,
	)

	result, err := svc.GetCohortTelemetryComparison(t.Context(), models.CohortTelemetryComparisonParams{
		OrgID: 7, CohortIDs: []int64{42, 1}, Window: models.CohortTelemetryComparisonWindowSixHours,
	})
	require.NoError(t, err)
	assert.Equal(t, now.Add(-12*time.Hour), result.BaselineStart)
	assert.Equal(t, now.Add(-6*time.Hour), result.BaselineEnd)
	assert.Equal(t, result.BaselineEnd, result.ComparisonStart)
	assert.Equal(t, now, result.ComparisonEnd)
	require.Len(t, result.Series, 2)
	assert.Equal(t, int64(42), result.Series[0].CohortID, "response preserves selector order")
	assert.True(t, result.Series[1].IsDefault)

	rollout := result.Series[0]
	assert.Equal(t, int32(1), rollout.CurrentNonHashingDeviceCount)
	require.Len(t, rollout.Distributions, 3)
	hashrate := rollout.Distributions[0]
	assert.Equal(t, models.CohortTelemetryComparisonMetricHashrate, hashrate.Metric)
	assert.Equal(t, int32(4), hashrate.BaselineReportingDeviceCount)
	assert.Equal(t, int32(4), hashrate.CurrentReportingDeviceCount)
	assert.Equal(t, int32(1), hashrate.ZeroBaselineDeviceCount)
	assert.Equal(t, int32(3), hashrate.EligibleDeviceCount)
	assert.InDelta(t, 100, *hashrate.BaselineMedian, 0.001)
	assert.InDelta(t, 110, *hashrate.ComparisonMedian, 0.001)
	assert.InDelta(t, -55, *hashrate.P25PercentageChange, 0.001)
	assert.InDelta(t, -10, *hashrate.MedianPercentageChange, 0.001)
	assert.InDelta(t, 0, *hashrate.P75PercentageChange, 0.001)

	assert.Equal(t, int32(4), rollout.AggregateEfficiencyDeviceCount)
	assert.InDelta(t, 10, *rollout.BaselineAggregateEfficiency, 0.001)
	assert.InDelta(t, 3000.0/390.0, *rollout.ComparisonAggregateEfficiency, 0.001)
	assert.InDelta(t, -23.0769, *rollout.AggregateEfficiencyPercentageChange, 0.001)

	queries := provider.allQueries()
	require.Len(t, queries, 1, "selected cohorts share one organization-scoped telemetry scan")
	for _, query := range queries {
		assert.Equal(t, int64(7), query.OrganizationID)
		assert.Equal(t, now.Add(-12*time.Hour), query.BaselineStart)
		assert.Equal(t, now.Add(-6*time.Hour), query.BaselineEnd)
		assert.Equal(t, query.BaselineEnd, query.ComparisonStart)
		assert.Equal(t, now, query.ComparisonEnd)
		assert.ElementsMatch(t, []telemetrymodels.DeviceIdentifier{
			"miner-1", "miner-2", "miner-3", "miner-4", "default-1",
		}, query.DeviceIDs)
	}
}

func TestGetCohortTelemetryComparisonSkipsTelemetryForEmptyCohort(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	provider := &comparisonOutcomeProvider{get: func(telemetrymodels.DeviceOutcomeComparisonQuery) []telemetrymodels.DeviceOutcomeAverages {
		return nil
	}}
	svc := NewService(store, WithOutcomeTelemetryProvider(provider))
	store.EXPECT().ListCohortTelemetryComparisonMemberships(gomock.Any(), int64(7), []int64{1}).Return(
		[]models.CohortTelemetryComparisonMembership{{CohortID: 1, Label: "Default", IsDefault: true}}, nil,
	)

	result, err := svc.GetCohortTelemetryComparison(t.Context(), models.CohortTelemetryComparisonParams{
		OrgID: 7, CohortIDs: []int64{1}, Window: models.CohortTelemetryComparisonWindowOneHour,
	})
	require.NoError(t, err)
	require.Len(t, result.Series, 1)
	assert.Empty(t, result.Series[0].Distributions)
	assert.Empty(t, provider.allQueries(), "an empty device selector must never fall through to organization-wide telemetry")
}

func TestGetCohortTelemetryComparisonRejectsInvalidAndUnavailableSelections(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store, WithOutcomeTelemetryProvider(&comparisonOutcomeProvider{
		get: func(telemetrymodels.DeviceOutcomeComparisonQuery) []telemetrymodels.DeviceOutcomeAverages { return nil },
	}))

	_, err := svc.GetCohortTelemetryComparison(t.Context(), models.CohortTelemetryComparisonParams{
		OrgID: 7, CohortIDs: []int64{1, 1}, Window: models.CohortTelemetryComparisonWindowOneHour,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	store.EXPECT().ListCohortTelemetryComparisonMemberships(gomock.Any(), int64(7), []int64{1, 99}).Return(
		[]models.CohortTelemetryComparisonMembership{{CohortID: 1, Label: "Default", IsDefault: true}}, nil,
	)
	_, err = svc.GetCohortTelemetryComparison(t.Context(), models.CohortTelemetryComparisonParams{
		OrgID: 7, CohortIDs: []int64{1, 99}, Window: models.CohortTelemetryComparisonWindowOneHour,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func outcomeAverages(id string, baselineHashrate, comparisonHashrate, baselineEfficiency, comparisonEfficiency, baselinePower, comparisonPower float64) telemetrymodels.DeviceOutcomeAverages {
	return telemetrymodels.DeviceOutcomeAverages{
		DeviceID:             telemetrymodels.DeviceIdentifier(id),
		BaselineHashrate:     float64Pointer(baselineHashrate),
		ComparisonHashrate:   float64Pointer(comparisonHashrate),
		BaselineEfficiency:   float64Pointer(baselineEfficiency),
		ComparisonEfficiency: float64Pointer(comparisonEfficiency),
		BaselinePower:        float64Pointer(baselinePower),
		ComparisonPower:      float64Pointer(comparisonPower),
	}
}

func float64Pointer(value float64) *float64 { return &value }
