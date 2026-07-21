package cohort

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

const rawValidationRetention = 10 * 24 * time.Hour

type firmwareValidationWindowConfig struct {
	window      time.Duration
	stabilize   time.Duration
	granularity time.Duration
}

type firmwareValidationGroup struct {
	result                 models.CohortFirmwareValidationBaseline
	eligibleDeviceIDs      []telemetrymodels.DeviceIdentifier
	latestTargetTransition time.Time
	stabilizingCount       int32
}

// GetCohortFirmwareValidation compares model-specific telemetry before a
// target assignment with the first stable window after eligible miners reach
// that target. The comparison deliberately aggregates each window across the
// same device set; it is not a per-device causal analysis.
func (s *Service) GetCohortFirmwareValidation(ctx context.Context, params models.CohortFirmwareValidationParams) (models.CohortFirmwareValidation, error) {
	params.Manufacturer = strings.TrimSpace(params.Manufacturer)
	params.Model = strings.TrimSpace(params.Model)
	config, ok := firmwareValidationConfig(params.Window)
	if !ok {
		return models.CohortFirmwareValidation{}, fleeterror.NewInvalidArgumentError("Choose a supported comparison window.")
	}
	if params.Manufacturer == "" || params.Model == "" {
		return models.CohortFirmwareValidation{}, fleeterror.NewInvalidArgumentError("Manufacturer and model are required.")
	}

	result := models.CohortFirmwareValidation{
		Manufacturer:     params.Manufacturer,
		Model:            params.Model,
		Window:           params.Window,
		StabilizationGap: config.stabilize,
		ChartGranularity: config.granularity,
		State:            models.CohortFirmwareValidationStateNoTarget,
	}

	cohort, err := s.GetCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return models.CohortFirmwareValidation{}, err
	}
	if cohort.IsDefault {
		return models.CohortFirmwareValidation{}, fleeterror.NewInvalidArgumentError("Firmware validation is available for explicit cohorts only.")
	}
	if cohort.State == models.CohortStateReleased {
		return models.CohortFirmwareValidation{}, fleeterror.NewInvalidArgumentError("Firmware validation is unavailable after a cohort is released.")
	}

	target := findFirmwareValidationTarget(cohort.FirmwareTargets, params.Manufacturer, params.Model)
	if target == nil || target.FirmwareFileID == nil || strings.TrimSpace(*target.FirmwareFileID) == "" {
		return result, nil
	}
	result.Manufacturer = target.Manufacturer
	result.Model = target.Model
	result.TargetFirmwareFileID = strings.TrimSpace(*target.FirmwareFileID)
	result.RolloutStartedAt = target.UpdatedAt.UTC()
	result.TargetFirmwareVersion = s.resolveFirmwareValidationTargetVersion(cohort, result.TargetFirmwareFileID)
	if result.TargetFirmwareVersion == "" {
		result.State = models.CohortFirmwareValidationStateTargetVersionUnknown
		return result, nil
	}

	now := s.now().UTC()
	events, err := s.store.ListCohortFirmwareVersionEvents(ctx, params.OrgID, params.CohortID, result.RolloutStartedAt, now)
	if err != nil {
		return models.CohortFirmwareValidation{}, err
	}
	eventsByDevice := firmwareValidationEventsByDevice(events)
	groups := make(map[string]*firmwareValidationGroup)

	for i := range cohort.Members {
		member := &cohort.Members[i]
		if !sameFirmwareTarget(member.Display.Manufacturer, member.Display.Model, target.Manufacturer, target.Model) {
			continue
		}
		result.TargetedCount++
		complete := firmwareValidationMemberComplete(member, result.TargetFirmwareFileID, result.TargetFirmwareVersion)
		if complete {
			result.CompleteCount++
		}
		if member.AddedAt.After(result.RolloutStartedAt) {
			result.Exclusions.AddedAfterRolloutCount++
			continue
		}

		baselineVersion, targetTransition, trustworthy := firmwareValidationTransitions(
			eventsByDevice[member.DeviceIdentifier],
			result.RolloutStartedAt,
			result.TargetFirmwareVersion,
		)
		if baselineVersion == "" {
			result.Exclusions.UnknownBaselineCount++
			continue
		}
		if baselineVersion == result.TargetFirmwareVersion {
			result.Exclusions.AlreadyOnTargetCount++
			continue
		}

		group := groups[baselineVersion]
		if group == nil {
			group = &firmwareValidationGroup{result: models.CohortFirmwareValidationBaseline{
				PreviousFirmwareVersion: baselineVersion,
				State:                   models.CohortFirmwareValidationStateNoBaseline,
			}}
			groups[baselineVersion] = group
		}
		group.result.MemberCount++
		if !complete {
			result.Exclusions.IncompleteCount++
			group.stabilizingCount++
			continue
		}
		if !trustworthy {
			result.Exclusions.UntrustedTransitionCount++
			continue
		}
		if targetTransition.Add(config.stabilize + config.window).After(now) {
			result.Exclusions.StabilizingCount++
			group.stabilizingCount++
			group.result.State = models.CohortFirmwareValidationStateStabilizing
			continue
		}
		group.eligibleDeviceIDs = append(group.eligibleDeviceIDs, telemetrymodels.DeviceIdentifier(member.DeviceIdentifier))
		if targetTransition.After(group.latestTargetTransition) {
			group.latestTargetTransition = targetTransition
		}
	}

	result.Preliminary = result.CompleteCount < result.TargetedCount
	if len(groups) == 0 {
		result.State = models.CohortFirmwareValidationStateNoBaseline
		return result, nil
	}

	orderedGroups := make([]*firmwareValidationGroup, 0, len(groups))
	for _, group := range groups {
		// Cohort membership mutations are capped at 10,000 devices.
		group.result.EligibleCount = int32(len(group.eligibleDeviceIDs)) //nolint:gosec // bounded by the cohort membership cap
		orderedGroups = append(orderedGroups, group)
	}
	sort.Slice(orderedGroups, func(i, j int) bool {
		if orderedGroups[i].result.EligibleCount != orderedGroups[j].result.EligibleCount {
			return orderedGroups[i].result.EligibleCount > orderedGroups[j].result.EligibleCount
		}
		if orderedGroups[i].result.MemberCount != orderedGroups[j].result.MemberCount {
			return orderedGroups[i].result.MemberCount > orderedGroups[j].result.MemberCount
		}
		return orderedGroups[i].result.PreviousFirmwareVersion < orderedGroups[j].result.PreviousFirmwareVersion
	})

	baselineStart := result.RolloutStartedAt.Add(-config.window)
	if baselineStart.Before(now.AddDate(0, -3, 0)) {
		result.State = models.CohortFirmwareValidationStateHistoryExpired
		for _, group := range orderedGroups {
			group.result.State = models.CohortFirmwareValidationStateHistoryExpired
			group.result.BaselineStartTime = baselineStart
			group.result.BaselineEndTime = result.RolloutStartedAt
			result.Baselines = append(result.Baselines, group.result)
		}
		return result, nil
	}

	resolution := telemetrymodels.CombinedMetricsResolutionRaw
	result.TelemetryResolution = models.CohortFirmwareValidationTelemetryResolutionRaw
	if baselineStart.Before(now.Add(-rawValidationRetention)) {
		resolution = telemetrymodels.CombinedMetricsResolutionHourly
		result.TelemetryResolution = models.CohortFirmwareValidationTelemetryResolutionHourly
		result.ChartGranularity = time.Hour
	}

	if s.validationTelemetry == nil {
		return models.CohortFirmwareValidation{}, fleeterror.NewInternalError("cohort firmware validation telemetry is not configured")
	}

	available := false
	stabilizing := false
	for _, group := range orderedGroups {
		group.result.BaselineStartTime = baselineStart
		group.result.BaselineEndTime = result.RolloutStartedAt
		if len(group.eligibleDeviceIDs) == 0 {
			if group.stabilizingCount > 0 {
				group.result.State = models.CohortFirmwareValidationStateStabilizing
				stabilizing = true
			} else {
				group.result.State = models.CohortFirmwareValidationStateInsufficientTelemetry
			}
			result.Baselines = append(result.Baselines, group.result)
			continue
		}

		group.result.TargetStartTime = group.latestTargetTransition.Add(config.stabilize)
		group.result.TargetEndTime = group.result.TargetStartTime.Add(config.window)
		baselineMetrics, err := s.validationMetrics(ctx, params.OrgID, group.eligibleDeviceIDs, baselineStart, result.RolloutStartedAt, result.ChartGranularity, resolution)
		if err != nil {
			return models.CohortFirmwareValidation{}, err
		}
		targetMetrics, err := s.validationMetrics(ctx, params.OrgID, group.eligibleDeviceIDs, group.result.TargetStartTime, group.result.TargetEndTime, result.ChartGranularity, resolution)
		if err != nil {
			return models.CohortFirmwareValidation{}, err
		}
		group.result.Metrics, group.result.State = buildFirmwareValidationMetrics(
			baselineMetrics,
			targetMetrics,
			baselineStart,
			group.result.TargetStartTime,
		)
		if group.result.State == models.CohortFirmwareValidationStateAvailable {
			available = true
		}
		result.Baselines = append(result.Baselines, group.result)
	}

	switch {
	case available:
		result.State = models.CohortFirmwareValidationStateAvailable
	case stabilizing:
		result.State = models.CohortFirmwareValidationStateStabilizing
	default:
		result.State = models.CohortFirmwareValidationStateInsufficientTelemetry
	}
	return result, nil
}

func firmwareValidationConfig(window models.CohortFirmwareValidationWindow) (firmwareValidationWindowConfig, bool) {
	switch window {
	case models.CohortFirmwareValidationWindowOneHour:
		return firmwareValidationWindowConfig{window: time.Hour, stabilize: 15 * time.Minute, granularity: 5 * time.Minute}, true
	case models.CohortFirmwareValidationWindowSixHours:
		return firmwareValidationWindowConfig{window: 6 * time.Hour, stabilize: 30 * time.Minute, granularity: 30 * time.Minute}, true
	case models.CohortFirmwareValidationWindowTwentyFourHours:
		return firmwareValidationWindowConfig{window: 24 * time.Hour, stabilize: time.Hour, granularity: time.Hour}, true
	default:
		return firmwareValidationWindowConfig{}, false
	}
}

func findFirmwareValidationTarget(targets []models.CohortFirmwareTarget, manufacturer, model string) *models.CohortFirmwareTarget {
	for i := range targets {
		if sameFirmwareTarget(targets[i].Manufacturer, targets[i].Model, manufacturer, model) {
			return &targets[i]
		}
	}
	return nil
}

func sameFirmwareTarget(manufacturerA, modelA, manufacturerB, modelB string) bool {
	return strings.EqualFold(strings.TrimSpace(manufacturerA), strings.TrimSpace(manufacturerB)) &&
		strings.EqualFold(strings.TrimSpace(modelA), strings.TrimSpace(modelB))
}

func (s *Service) resolveFirmwareValidationTargetVersion(cohort *models.Cohort, fileID string) string {
	if s.firmwareMetadata != nil {
		if metadata, err := s.firmwareMetadata.GetFirmwareMetadata(fileID); err == nil {
			if version := strings.TrimSpace(metadata.FirmwareVersion); version != "" {
				return version
			}
		}
	}
	for _, member := range cohort.Members {
		if member.FirmwareStatus != nil && strings.TrimSpace(member.FirmwareStatus.TargetFirmwareFileID) == fileID {
			if version := strings.TrimSpace(member.FirmwareStatus.TargetFirmwareVersion); version != "" {
				return version
			}
		}
	}
	return ""
}

func firmwareValidationEventsByDevice(events []models.FirmwareVersionEvent) map[string][]models.FirmwareVersionEvent {
	result := make(map[string][]models.FirmwareVersionEvent)
	for _, event := range events {
		result[event.DeviceIdentifier] = append(result[event.DeviceIdentifier], event)
	}
	return result
}

func firmwareValidationMemberComplete(member *models.CohortMember, fileID, targetVersion string) bool {
	status := member.FirmwareStatus
	return status != nil &&
		status.State == models.CohortFirmwareRolloutStateComplete &&
		strings.TrimSpace(status.TargetFirmwareFileID) == fileID &&
		strings.TrimSpace(status.CurrentFirmwareVersion) == targetVersion
}

func firmwareValidationTransitions(events []models.FirmwareVersionEvent, rolloutStartedAt time.Time, targetVersion string) (string, time.Time, bool) {
	var baselineVersion string
	var targetTransition time.Time
	var latestVersion string
	for _, event := range events {
		version := strings.TrimSpace(event.FirmwareVersion)
		if event.ObservedAt.Before(rolloutStartedAt) {
			baselineVersion = version
			latestVersion = version
			continue
		}
		latestVersion = version
		if version == targetVersion {
			targetTransition = event.ObservedAt.UTC()
		}
	}
	trustworthy := !targetTransition.IsZero() && latestVersion == targetVersion
	return baselineVersion, targetTransition, trustworthy
}

func (s *Service) validationMetrics(
	ctx context.Context,
	orgID int64,
	deviceIDs []telemetrymodels.DeviceIdentifier,
	startTime time.Time,
	endTime time.Time,
	granularity time.Duration,
	resolution telemetrymodels.CombinedMetricsResolution,
) (telemetrymodels.CombinedMetric, error) {
	return s.validationTelemetry.GetCombinedMetrics(ctx, telemetrymodels.CombinedMetricsQuery{
		DeviceIDs: deviceIDs,
		MeasurementTypes: []telemetrymodels.MeasurementType{
			telemetrymodels.MeasurementTypeHashrate,
			telemetrymodels.MeasurementTypeEfficiency,
			telemetrymodels.MeasurementTypePower,
		},
		AggregationTypes: []telemetrymodels.AggregationType{telemetrymodels.AggregationTypeAverage},
		TimeRange:        telemetrymodels.TimeRange{StartTime: &startTime, EndTime: &endTime},
		SlideInterval:    &granularity,
		OrganizationID:   orgID,
		Resolution:       resolution,
	})
}

func buildFirmwareValidationMetrics(
	baseline telemetrymodels.CombinedMetric,
	target telemetrymodels.CombinedMetric,
	baselineStart time.Time,
	targetStart time.Time,
) ([]models.CohortFirmwareValidationMetric, models.CohortFirmwareValidationState) {
	types := []telemetrymodels.MeasurementType{
		telemetrymodels.MeasurementTypeHashrate,
		telemetrymodels.MeasurementTypeEfficiency,
		telemetrymodels.MeasurementTypePower,
	}
	result := make([]models.CohortFirmwareValidationMetric, 0, len(types))
	available := false
	for _, measurementType := range types {
		baselinePoints, baselineAverage, baselineReporting := firmwareValidationMetricPoints(baseline.Metrics, measurementType, baselineStart)
		targetPoints, targetAverage, targetReporting := firmwareValidationMetricPoints(target.Metrics, measurementType, targetStart)
		metric := models.CohortFirmwareValidationMetric{
			MeasurementType:              measurementType,
			BaselinePoints:               baselinePoints,
			TargetPoints:                 targetPoints,
			BaselineAverage:              baselineAverage,
			TargetAverage:                targetAverage,
			BaselineReportingDeviceCount: baselineReporting,
			TargetReportingDeviceCount:   targetReporting,
		}
		if baselineAverage != nil && targetAverage != nil {
			absolute := *targetAverage - *baselineAverage
			metric.AbsoluteDelta = &absolute
			if math.Abs(*baselineAverage) > 0 {
				percentage := absolute / *baselineAverage * 100
				metric.PercentageDelta = &percentage
			}
			available = true
		}
		result = append(result, metric)
	}
	if available {
		return result, models.CohortFirmwareValidationStateAvailable
	}
	return result, models.CohortFirmwareValidationStateInsufficientTelemetry
}

func firmwareValidationMetricPoints(
	metrics []telemetrymodels.Metric,
	measurementType telemetrymodels.MeasurementType,
	start time.Time,
) ([]models.CohortFirmwareValidationPoint, *float64, int32) {
	points := make([]models.CohortFirmwareValidationPoint, 0)
	var weightedSum float64
	var totalWeight int64
	var reporting int32
	for _, metric := range metrics {
		if metric.MeasurementType != measurementType {
			continue
		}
		value, ok := firmwareValidationAverage(metric.AggregatedValues)
		if !ok {
			continue
		}
		// Hashrate remains a cohort total, while power is normalized per miner so
		// fleet-size coverage changes do not masquerade as a firmware power delta.
		if measurementType == telemetrymodels.MeasurementTypePower && metric.DeviceCount > 0 {
			value /= float64(metric.DeviceCount)
		}
		points = append(points, models.CohortFirmwareValidationPoint{
			Elapsed:     metric.OpenTime.Sub(start),
			Value:       value,
			DeviceCount: metric.DeviceCount,
		})
		weight := int64(metric.DeviceCount)
		if weight <= 0 {
			weight = 1
		}
		weightedSum += value * float64(weight)
		totalWeight += weight
		if metric.DeviceCount > reporting {
			reporting = metric.DeviceCount
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Elapsed < points[j].Elapsed })
	if totalWeight == 0 {
		return points, nil, reporting
	}
	average := weightedSum / float64(totalWeight)
	return points, &average, reporting
}

func firmwareValidationAverage(values []telemetrymodels.AggregatedValue) (float64, bool) {
	for _, value := range values {
		if value.Type == telemetrymodels.AggregationTypeAverage {
			return value.Value, true
		}
	}
	return 0, false
}
