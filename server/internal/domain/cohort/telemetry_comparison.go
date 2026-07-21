package cohort

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

const maxComparedCohorts = 5

// GetCohortTelemetryComparison compares every reporting miner with its own
// immediately preceding baseline. The resulting distributions remain useful
// when cohorts contain different hardware models, performance bins, or power
// profiles because absolute miner levels are not averaged together.
func (s *Service) GetCohortTelemetryComparison(ctx context.Context, params models.CohortTelemetryComparisonParams) (models.CohortTelemetryComparison, error) {
	window, ok := cohortTelemetryComparisonDuration(params.Window)
	if !ok {
		return models.CohortTelemetryComparison{}, fleeterror.NewInvalidArgumentError("Choose a supported comparison window.")
	}
	if len(params.CohortIDs) == 0 || len(params.CohortIDs) > maxComparedCohorts {
		return models.CohortTelemetryComparison{}, fleeterror.NewInvalidArgumentError("Choose between one and five cohorts.")
	}
	seen := make(map[int64]struct{}, len(params.CohortIDs))
	for _, cohortID := range params.CohortIDs {
		if cohortID <= 0 {
			return models.CohortTelemetryComparison{}, fleeterror.NewInvalidArgumentError("Choose valid cohorts to compare.")
		}
		if _, exists := seen[cohortID]; exists {
			return models.CohortTelemetryComparison{}, fleeterror.NewInvalidArgumentError("Choose each cohort only once.")
		}
		seen[cohortID] = struct{}{}
	}
	if s.store == nil {
		return models.CohortTelemetryComparison{}, fleeterror.NewInternalError("cohort store is not configured")
	}
	if s.outcomeTelemetry == nil {
		return models.CohortTelemetryComparison{}, fleeterror.NewInternalError("cohort outcome comparison is not configured")
	}

	memberships, err := s.store.ListCohortTelemetryComparisonMemberships(ctx, params.OrgID, params.CohortIDs)
	if err != nil {
		return models.CohortTelemetryComparison{}, err
	}
	if len(memberships) != len(params.CohortIDs) {
		return models.CohortTelemetryComparison{}, fleeterror.NewNotFoundError("one or more selected cohorts are unavailable")
	}
	membershipByID := make(map[int64]models.CohortTelemetryComparisonMembership, len(memberships))
	for _, membership := range memberships {
		membershipByID[membership.CohortID] = membership
	}

	comparisonEnd := s.now().UTC()
	comparisonStart := comparisonEnd.Add(-window)
	baselineEnd := comparisonStart
	baselineStart := baselineEnd.Add(-window)
	result := models.CohortTelemetryComparison{
		BaselineStart:   baselineStart,
		BaselineEnd:     baselineEnd,
		ComparisonStart: comparisonStart,
		ComparisonEnd:   comparisonEnd,
		Window:          params.Window,
		Series:          make([]models.CohortTelemetryComparisonSeries, len(params.CohortIDs)),
	}

	seriesByDeviceID := make(map[telemetrymodels.DeviceIdentifier][]int)
	uniqueDeviceIDs := make([]telemetrymodels.DeviceIdentifier, 0)
	for index, cohortID := range params.CohortIDs {
		membership := membershipByID[cohortID]
		result.Series[index] = models.CohortTelemetryComparisonSeries{
			CohortID:    membership.CohortID,
			Label:       membership.Label,
			IsDefault:   membership.IsDefault,
			MemberCount: int64(len(membership.DeviceIdentifiers)),
		}
		for _, deviceIdentifier := range membership.DeviceIdentifiers {
			deviceID := telemetrymodels.DeviceIdentifier(deviceIdentifier)
			if _, exists := seriesByDeviceID[deviceID]; !exists {
				uniqueDeviceIDs = append(uniqueDeviceIDs, deviceID)
			}
			seriesByDeviceID[deviceID] = append(seriesByDeviceID[deviceID], index)
		}
	}
	if len(uniqueDeviceIDs) == 0 {
		return result, nil
	}

	averages, err := s.outcomeTelemetry.GetDeviceOutcomeAverages(ctx, telemetrymodels.DeviceOutcomeComparisonQuery{
		DeviceIDs:       uniqueDeviceIDs,
		OrganizationID:  params.OrgID,
		BaselineStart:   baselineStart,
		BaselineEnd:     baselineEnd,
		ComparisonStart: comparisonStart,
		ComparisonEnd:   comparisonEnd,
	})
	if err != nil {
		return models.CohortTelemetryComparison{}, err
	}
	averagesBySeries := make([][]telemetrymodels.DeviceOutcomeAverages, len(result.Series))
	for _, device := range averages {
		for _, index := range seriesByDeviceID[device.DeviceID] {
			averagesBySeries[index] = append(averagesBySeries[index], device)
		}
	}
	for index := range result.Series {
		result.Series[index] = buildCohortOutcomeSeries(result.Series[index], averagesBySeries[index])
	}
	return result, nil
}

func cohortTelemetryComparisonDuration(window models.CohortTelemetryComparisonWindow) (time.Duration, bool) {
	switch window {
	case models.CohortTelemetryComparisonWindowOneHour:
		return time.Hour, true
	case models.CohortTelemetryComparisonWindowSixHours:
		return 6 * time.Hour, true
	case models.CohortTelemetryComparisonWindowTwentyFourHours:
		return 24 * time.Hour, true
	default:
		return 0, false
	}
}

type pairedMetricValues struct {
	baseline   *float64
	comparison *float64
}

func buildCohortOutcomeSeries(series models.CohortTelemetryComparisonSeries, devices []telemetrymodels.DeviceOutcomeAverages) models.CohortTelemetryComparisonSeries {
	metricValues := map[models.CohortTelemetryComparisonMetric][]pairedMetricValues{
		models.CohortTelemetryComparisonMetricHashrate:   make([]pairedMetricValues, 0, len(devices)),
		models.CohortTelemetryComparisonMetricEfficiency: make([]pairedMetricValues, 0, len(devices)),
		models.CohortTelemetryComparisonMetricPower:      make([]pairedMetricValues, 0, len(devices)),
	}

	var baselineAggregatePower, comparisonAggregatePower float64
	var baselineAggregateHashrate, comparisonAggregateHashrate float64
	for _, device := range devices {
		metricValues[models.CohortTelemetryComparisonMetricHashrate] = append(metricValues[models.CohortTelemetryComparisonMetricHashrate], pairedMetricValues{
			baseline: device.BaselineHashrate, comparison: device.ComparisonHashrate,
		})
		metricValues[models.CohortTelemetryComparisonMetricEfficiency] = append(metricValues[models.CohortTelemetryComparisonMetricEfficiency], pairedMetricValues{
			baseline: device.BaselineEfficiency, comparison: device.ComparisonEfficiency,
		})
		metricValues[models.CohortTelemetryComparisonMetricPower] = append(metricValues[models.CohortTelemetryComparisonMetricPower], pairedMetricValues{
			baseline: device.BaselinePower, comparison: device.ComparisonPower,
		})

		if finiteValue(device.ComparisonHashrate) && *device.ComparisonHashrate <= 0 {
			series.CurrentNonHashingDeviceCount++
		}
		if finiteValue(device.BaselineHashrate) && finiteValue(device.ComparisonHashrate) &&
			finiteValue(device.BaselinePower) && finiteValue(device.ComparisonPower) {
			baselineAggregateHashrate += *device.BaselineHashrate
			comparisonAggregateHashrate += *device.ComparisonHashrate
			baselineAggregatePower += *device.BaselinePower
			comparisonAggregatePower += *device.ComparisonPower
			series.AggregateEfficiencyDeviceCount++
		}
	}

	for _, metric := range []models.CohortTelemetryComparisonMetric{
		models.CohortTelemetryComparisonMetricHashrate,
		models.CohortTelemetryComparisonMetricEfficiency,
		models.CohortTelemetryComparisonMetricPower,
	} {
		series.Distributions = append(series.Distributions, buildOutcomeDistribution(metric, metricValues[metric]))
	}
	if series.AggregateEfficiencyDeviceCount > 0 && baselineAggregateHashrate > 0 {
		value := baselineAggregatePower / baselineAggregateHashrate
		series.BaselineAggregateEfficiency = &value
	}
	if series.AggregateEfficiencyDeviceCount > 0 && comparisonAggregateHashrate > 0 {
		value := comparisonAggregatePower / comparisonAggregateHashrate
		series.ComparisonAggregateEfficiency = &value
	}
	if series.BaselineAggregateEfficiency != nil && series.ComparisonAggregateEfficiency != nil && *series.BaselineAggregateEfficiency != 0 {
		value := (*series.ComparisonAggregateEfficiency - *series.BaselineAggregateEfficiency) / math.Abs(*series.BaselineAggregateEfficiency) * 100
		series.AggregateEfficiencyPercentageChange = &value
	}
	return series
}

func buildOutcomeDistribution(metric models.CohortTelemetryComparisonMetric, values []pairedMetricValues) models.CohortTelemetryComparisonDistribution {
	distribution := models.CohortTelemetryComparisonDistribution{Metric: metric}
	baselineValues := make([]float64, 0, len(values))
	comparisonValues := make([]float64, 0, len(values))
	changes := make([]float64, 0, len(values))
	for _, pair := range values {
		baselineOK := finiteValue(pair.baseline)
		comparisonOK := finiteValue(pair.comparison)
		if baselineOK {
			distribution.BaselineReportingDeviceCount++
		}
		if comparisonOK {
			distribution.CurrentReportingDeviceCount++
		}
		if baselineOK && *pair.baseline == 0 {
			distribution.ZeroBaselineDeviceCount++
		}
		if !baselineOK || !comparisonOK || *pair.baseline == 0 {
			continue
		}
		baselineValues = append(baselineValues, *pair.baseline)
		comparisonValues = append(comparisonValues, *pair.comparison)
		changes = append(changes, (*pair.comparison-*pair.baseline)/math.Abs(*pair.baseline)*100)
	}
	distribution.EligibleDeviceCount = int32(len(changes)) //nolint:gosec // cohort sizes fit in int32 API counts
	if len(changes) == 0 {
		return distribution
	}
	distribution.BaselineMedian = percentile(baselineValues, 0.5)
	distribution.ComparisonMedian = percentile(comparisonValues, 0.5)
	distribution.P25PercentageChange = percentile(changes, 0.25)
	distribution.MedianPercentageChange = percentile(changes, 0.5)
	distribution.P75PercentageChange = percentile(changes, 0.75)
	return distribution
}

func percentile(values []float64, fraction float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	position := fraction * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	value := sorted[lower]
	if lower != upper {
		value += (sorted[upper] - sorted[lower]) * (position - float64(lower))
	}
	return &value
}

func finiteValue(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0)
}
