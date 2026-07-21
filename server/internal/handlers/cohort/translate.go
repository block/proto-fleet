package cohort

import (
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

func toCreateCohortParams(req *pb.CreateCohortRequest, info *session.Info) (models.CreateCohortParams, error) {
	desiredConfig := desiredConfigFromProto(req.GetDesiredConfig())
	desiredConfigJSON, err := desiredConfig.MarshalJSON()
	if err != nil {
		return models.CreateCohortParams{}, err
	}
	var sourceDeviceSetID *int64
	if x, ok := req.GetInitialMembers().(*pb.CreateCohortRequest_SourceDeviceSetId); ok {
		sourceDeviceSetID = &x.SourceDeviceSetId
	}
	var selector *models.CohortDeviceSelector
	if x, ok := req.GetInitialMembers().(*pb.CreateCohortRequest_Select); ok && x.Select != nil {
		selector = &models.CohortDeviceSelector{
			Count:   x.Select.GetCount(),
			Product: stringPtrFromOptional(x.Select.Product),
			Model:   stringPtrFromOptional(x.Select.Model),
		}
	}
	var ownerUserID *int64
	var ownerUsername *string
	if req.GetClaimOwnership() || req.GetExpiresAt() != nil {
		ownerUserID = &info.UserID
		username := info.Username
		ownerUsername = &username
	}
	return models.CreateCohortParams{
		OrgID:                 info.OrganizationID,
		Label:                 req.GetLabel(),
		OwnerUserID:           ownerUserID,
		OwnerUsername:         ownerUsername,
		ExpiresAt:             timestampToPtr(req.GetExpiresAt()),
		DesiredFirmwareFileID: nonEmptyPtr(req.GetDesiredFirmwareFileId()),
		DesiredConfig:         desiredConfig,
		DesiredConfigJSON:     desiredConfigJSON,
		Purpose:               req.GetPurpose(),
		SourceActorType:       deriveSourceActorType(info),
		SourceActorID:         deriveSourceActorID(info),
		IdempotencyKey:        nonEmptyPtr(req.GetIdempotencyKey()),
		DeviceIdentifiers:     req.GetDeviceIdentifiers().GetDeviceIdentifiers(),
		SourceDeviceSetID:     sourceDeviceSetID,
		DeviceSelector:        selector,
	}, nil
}

func toUpdateCohortParams(req *pb.UpdateCohortRequest, orgID int64) (models.UpdateCohortParams, error) {
	desiredConfig := desiredConfigFromProto(req.GetDesiredConfig())
	desiredConfigJSON, err := desiredConfig.MarshalJSON()
	if err != nil {
		return models.UpdateCohortParams{}, err
	}
	return models.UpdateCohortParams{
		OrgID:                    orgID,
		CohortID:                 req.GetCohortId(),
		Label:                    stringPtrFromOptional(req.Label),
		Purpose:                  stringPtrFromOptional(req.Purpose),
		ExpiresAt:                timestampToPtr(req.GetExpiresAt()),
		ClearExpiresAt:           req.GetClearExpiresAt(),
		DesiredFirmwareFileID:    stringPtrFromOptional(req.DesiredFirmwareFileId),
		DesiredFirmwareFileIDSet: req.DesiredFirmwareFileId != nil,
		DesiredConfig:            desiredConfig,
		DesiredConfigJSON:        desiredConfigJSON,
		DesiredConfigJSONSet:     req.GetDesiredConfig() != nil,
		ClearDesiredConfig:       req.GetClearDesiredConfig(),
	}, nil
}

func toSetCohortFirmwareTargetParams(req *pb.SetCohortFirmwareTargetRequest, info *session.Info) models.SetCohortFirmwareTargetParams {
	return models.SetCohortFirmwareTargetParams{
		OrgID:          info.OrganizationID,
		CohortID:       req.GetCohortId(),
		ActorUserID:    info.UserID,
		ActorRole:      info.Role,
		Manufacturer:   stringPtrFromOptional(req.Manufacturer),
		Model:          stringPtrFromOptional(req.Model),
		FirmwareFileID: stringPtrFromOptional(req.FirmwareFileId),
	}
}

func toListCohortsParams(req *pb.ListCohortsRequest, orgID int64) models.ListCohortsParams {
	return models.ListCohortsParams{
		OrgID:           orgID,
		IncludeReleased: req.GetIncludeReleased(),
		PageSize:        req.GetPageSize(),
		PageToken:       req.GetPageToken(),
		Search:          req.GetSearch(),
	}
}

func toCohortFirmwareVersionHistoryParams(req *pb.GetCohortFirmwareVersionHistoryRequest, orgID int64) models.CohortFirmwareVersionHistoryParams {
	params := models.CohortFirmwareVersionHistoryParams{OrgID: orgID, CohortID: req.GetCohortId()}
	if req.GetStartTime() != nil {
		params.StartTime = req.GetStartTime().AsTime()
	}
	if req.GetEndTime() != nil {
		params.EndTime = req.GetEndTime().AsTime()
	}
	if req.GetGranularity() != nil {
		params.Granularity = req.GetGranularity().AsDuration()
	}
	return params
}

func toCohortFirmwareValidationParams(req *pb.GetCohortFirmwareValidationRequest, orgID int64) models.CohortFirmwareValidationParams {
	return models.CohortFirmwareValidationParams{
		OrgID:        orgID,
		CohortID:     req.GetCohortId(),
		Manufacturer: req.GetManufacturer(),
		Model:        req.GetModel(),
		Window:       firmwareValidationWindowFromProto(req.GetComparisonWindow()),
	}
}

func toCohortTelemetryComparisonParams(req *pb.GetCohortTelemetryComparisonRequest, orgID int64) models.CohortTelemetryComparisonParams {
	return models.CohortTelemetryComparisonParams{
		OrgID:     orgID,
		CohortIDs: req.GetCohortIds(),
		Window:    cohortTelemetryComparisonWindowFromProto(req.GetComparisonWindow()),
	}
}

func cohortTelemetryComparisonWindowFromProto(window pb.CohortTelemetryComparisonWindow) models.CohortTelemetryComparisonWindow {
	switch window {
	case pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_ONE_HOUR:
		return models.CohortTelemetryComparisonWindowOneHour
	case pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_SIX_HOURS:
		return models.CohortTelemetryComparisonWindowSixHours
	case pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_TWENTY_FOUR_HOURS:
		return models.CohortTelemetryComparisonWindowTwentyFourHours
	case pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func firmwareValidationWindowFromProto(window pb.CohortFirmwareValidationWindow) models.CohortFirmwareValidationWindow {
	switch window {
	case pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_UNSPECIFIED:
		return ""
	case pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_ONE_HOUR:
		return models.CohortFirmwareValidationWindowOneHour
	case pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_SIX_HOURS:
		return models.CohortFirmwareValidationWindowSixHours
	case pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_TWENTY_FOUR_HOURS:
		return models.CohortFirmwareValidationWindowTwentyFourHours
	default:
		return ""
	}
}

func toListCohortsByOwnerParams(req *pb.GetMyCohortsRequest, info *session.Info) models.ListCohortsByOwnerParams {
	return models.ListCohortsByOwnerParams{
		OrgID:           info.OrganizationID,
		OwnerUserID:     info.UserID,
		IncludeReleased: req.GetIncludeReleased(),
		PageSize:        req.GetPageSize(),
		PageToken:       req.GetPageToken(),
		Search:          req.GetSearch(),
	}
}

func toMembershipMutationParams(orgID int64, userID int64, role string, cohortID int64, deviceIdentifiers []string) models.MembershipMutationParams {
	return models.MembershipMutationParams{
		OrgID:             orgID,
		CohortID:          cohortID,
		ActorUserID:       userID,
		ActorRole:         role,
		DeviceIdentifiers: deviceIdentifiers,
	}
}

func toListDevicesParams(req *pb.ListDevicesRequest, orgID int64) models.ListDevicesParams {
	return models.ListDevicesParams{
		OrgID:     orgID,
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
		Filter:    toCohortDeviceFilter(req.GetFilter()),
	}
}

func toCohortDeviceFilter(filter *pb.CohortDeviceFilter) models.CohortDeviceFilter {
	if filter == nil {
		return models.CohortDeviceFilter{}
	}
	assignments := make([]models.CohortDeviceAssignment, 0, len(filter.GetAssignments()))
	for _, assignment := range filter.GetAssignments() {
		switch assignment {
		case pb.CohortDeviceAssignment_COHORT_DEVICE_ASSIGNMENT_UNSPECIFIED:
			continue
		case pb.CohortDeviceAssignment_COHORT_DEVICE_ASSIGNMENT_AVAILABLE:
			assignments = append(assignments, models.CohortDeviceAssignmentAvailable)
		case pb.CohortDeviceAssignment_COHORT_DEVICE_ASSIGNMENT_RESERVED:
			assignments = append(assignments, models.CohortDeviceAssignmentReserved)
		}
	}
	return models.CohortDeviceFilter{
		Assignments:    assignments,
		CohortIDs:      filter.GetCohortIds(),
		OwnerUserIDs:   filter.GetOwnerUserIds(),
		IncludeUnowned: filter.GetIncludeUnowned(),
		Manufacturers:  filter.GetManufacturers(),
		Models:         filter.GetModels(),
		Search:         filter.GetSearch(),
	}
}

func toProtoCohort(cohort *models.Cohort) *pb.Cohort {
	if cohort == nil {
		return nil
	}
	return &pb.Cohort{
		Summary:         toProtoCohortSummary(cohort),
		Members:         toProtoMembers(cohort.Members),
		FirmwareTargets: toProtoFirmwareTargets(cohort.FirmwareTargets),
	}
}

func toProtoCohortFirmwareVersionHistory(history models.CohortFirmwareVersionHistory) *pb.GetCohortFirmwareVersionHistoryResponse {
	points := make([]*pb.CohortFirmwareVersionHistoryPoint, 0, len(history.Points))
	for _, point := range history.Points {
		versions := make([]*pb.CohortFirmwareVersionCount, 0, len(point.Versions))
		for _, version := range point.Versions {
			versions = append(versions, &pb.CohortFirmwareVersionCount{
				FirmwareVersion: version.FirmwareVersion,
				DeviceCount:     version.DeviceCount,
			})
		}
		points = append(points, &pb.CohortFirmwareVersionHistoryPoint{
			Timestamp: timestamppb.New(point.Timestamp),
			Versions:  versions,
		})
	}
	return &pb.GetCohortFirmwareVersionHistoryResponse{MemberCount: history.MemberCount, Points: points}
}

func toProtoCohortFirmwareValidation(validation models.CohortFirmwareValidation) *pb.GetCohortFirmwareValidationResponse {
	baselines := make([]*pb.CohortFirmwareValidationBaseline, 0, len(validation.Baselines))
	for _, baseline := range validation.Baselines {
		metrics := make([]*pb.CohortFirmwareValidationMetric, 0, len(baseline.Metrics))
		for _, metric := range baseline.Metrics {
			metrics = append(metrics, &pb.CohortFirmwareValidationMetric{
				MeasurementType:              validationMeasurementTypeToProto(metric.MeasurementType),
				BaselinePoints:               toProtoFirmwareValidationPoints(metric.BaselinePoints),
				TargetPoints:                 toProtoFirmwareValidationPoints(metric.TargetPoints),
				BaselineAverage:              metric.BaselineAverage,
				TargetAverage:                metric.TargetAverage,
				AbsoluteDelta:                metric.AbsoluteDelta,
				PercentageDelta:              metric.PercentageDelta,
				BaselineReportingDeviceCount: metric.BaselineReportingDeviceCount,
				TargetReportingDeviceCount:   metric.TargetReportingDeviceCount,
			})
		}
		baselines = append(baselines, &pb.CohortFirmwareValidationBaseline{
			PreviousFirmwareVersion: baseline.PreviousFirmwareVersion,
			MemberCount:             baseline.MemberCount,
			EligibleCount:           baseline.EligibleCount,
			State:                   firmwareValidationStateToProto(baseline.State),
			BaselineStartTime:       validationTimestamp(baseline.BaselineStartTime),
			BaselineEndTime:         validationTimestamp(baseline.BaselineEndTime),
			TargetStartTime:         validationTimestamp(baseline.TargetStartTime),
			TargetEndTime:           validationTimestamp(baseline.TargetEndTime),
			Metrics:                 metrics,
		})
	}
	return &pb.GetCohortFirmwareValidationResponse{
		State:                 firmwareValidationStateToProto(validation.State),
		Manufacturer:          validation.Manufacturer,
		Model:                 validation.Model,
		TargetFirmwareFileId:  validation.TargetFirmwareFileID,
		TargetFirmwareVersion: validation.TargetFirmwareVersion,
		RolloutStartedAt:      validationTimestamp(validation.RolloutStartedAt),
		ComparisonWindow:      firmwareValidationWindowToProto(validation.Window),
		StabilizationGap:      durationpb.New(validation.StabilizationGap),
		ChartGranularity:      durationpb.New(validation.ChartGranularity),
		TelemetryResolution:   firmwareValidationResolutionToProto(validation.TelemetryResolution),
		TargetedCount:         validation.TargetedCount,
		CompleteCount:         validation.CompleteCount,
		Preliminary:           validation.Preliminary,
		Exclusions: &pb.CohortFirmwareValidationExclusions{
			AddedAfterRolloutCount:   validation.Exclusions.AddedAfterRolloutCount,
			UnknownBaselineCount:     validation.Exclusions.UnknownBaselineCount,
			AlreadyOnTargetCount:     validation.Exclusions.AlreadyOnTargetCount,
			IncompleteCount:          validation.Exclusions.IncompleteCount,
			StabilizingCount:         validation.Exclusions.StabilizingCount,
			UntrustedTransitionCount: validation.Exclusions.UntrustedTransitionCount,
		},
		Baselines: baselines,
	}
}

func toProtoCohortTelemetryComparison(comparison models.CohortTelemetryComparison) *pb.GetCohortTelemetryComparisonResponse {
	series := make([]*pb.CohortTelemetryComparisonSeries, 0, len(comparison.Series))
	for _, cohortSeries := range comparison.Series {
		distributions := make([]*pb.CohortTelemetryComparisonDistribution, 0, len(cohortSeries.Distributions))
		for _, distribution := range cohortSeries.Distributions {
			distributions = append(distributions, &pb.CohortTelemetryComparisonDistribution{
				Metric:                       cohortTelemetryComparisonMetricToProto(distribution.Metric),
				BaselineMedian:               distribution.BaselineMedian,
				ComparisonMedian:             distribution.ComparisonMedian,
				MedianPercentageChange:       distribution.MedianPercentageChange,
				P25PercentageChange:          distribution.P25PercentageChange,
				P75PercentageChange:          distribution.P75PercentageChange,
				EligibleDeviceCount:          distribution.EligibleDeviceCount,
				BaselineReportingDeviceCount: distribution.BaselineReportingDeviceCount,
				CurrentReportingDeviceCount:  distribution.CurrentReportingDeviceCount,
				ZeroBaselineDeviceCount:      distribution.ZeroBaselineDeviceCount,
			})
		}
		series = append(series, &pb.CohortTelemetryComparisonSeries{
			CohortId:                            cohortSeries.CohortID,
			Label:                               cohortSeries.Label,
			IsDefault:                           cohortSeries.IsDefault,
			MemberCount:                         cohortSeries.MemberCount,
			Distributions:                       distributions,
			CurrentNonHashingDeviceCount:        cohortSeries.CurrentNonHashingDeviceCount,
			BaselineAggregateEfficiency:         cohortSeries.BaselineAggregateEfficiency,
			ComparisonAggregateEfficiency:       cohortSeries.ComparisonAggregateEfficiency,
			AggregateEfficiencyPercentageChange: cohortSeries.AggregateEfficiencyPercentageChange,
			AggregateEfficiencyDeviceCount:      cohortSeries.AggregateEfficiencyDeviceCount,
		})
	}
	return &pb.GetCohortTelemetryComparisonResponse{
		BaselineStartTime:   timestamppb.New(comparison.BaselineStart),
		BaselineEndTime:     timestamppb.New(comparison.BaselineEnd),
		ComparisonStartTime: timestamppb.New(comparison.ComparisonStart),
		ComparisonEndTime:   timestamppb.New(comparison.ComparisonEnd),
		ComparisonWindow:    cohortTelemetryComparisonWindowToProto(comparison.Window),
		Series:              series,
	}
}

func cohortTelemetryComparisonMetricToProto(metric models.CohortTelemetryComparisonMetric) pb.CohortTelemetryComparisonMetric {
	switch metric {
	case models.CohortTelemetryComparisonMetricHashrate:
		return pb.CohortTelemetryComparisonMetric_COHORT_TELEMETRY_COMPARISON_METRIC_HASHRATE
	case models.CohortTelemetryComparisonMetricEfficiency:
		return pb.CohortTelemetryComparisonMetric_COHORT_TELEMETRY_COMPARISON_METRIC_EFFICIENCY
	case models.CohortTelemetryComparisonMetricPower:
		return pb.CohortTelemetryComparisonMetric_COHORT_TELEMETRY_COMPARISON_METRIC_POWER
	default:
		return pb.CohortTelemetryComparisonMetric_COHORT_TELEMETRY_COMPARISON_METRIC_UNSPECIFIED
	}
}

func cohortTelemetryComparisonWindowToProto(window models.CohortTelemetryComparisonWindow) pb.CohortTelemetryComparisonWindow {
	switch window {
	case models.CohortTelemetryComparisonWindowOneHour:
		return pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_ONE_HOUR
	case models.CohortTelemetryComparisonWindowSixHours:
		return pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_SIX_HOURS
	case models.CohortTelemetryComparisonWindowTwentyFourHours:
		return pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_TWENTY_FOUR_HOURS
	default:
		return pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_UNSPECIFIED
	}
}

func toProtoFirmwareValidationPoints(points []models.CohortFirmwareValidationPoint) []*pb.CohortFirmwareValidationPoint {
	out := make([]*pb.CohortFirmwareValidationPoint, 0, len(points))
	for _, point := range points {
		out = append(out, &pb.CohortFirmwareValidationPoint{
			Elapsed:     durationpb.New(point.Elapsed),
			Value:       point.Value,
			DeviceCount: point.DeviceCount,
		})
	}
	return out
}

func validationTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func firmwareValidationWindowToProto(window models.CohortFirmwareValidationWindow) pb.CohortFirmwareValidationWindow {
	switch window {
	case models.CohortFirmwareValidationWindowOneHour:
		return pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_ONE_HOUR
	case models.CohortFirmwareValidationWindowSixHours:
		return pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_SIX_HOURS
	case models.CohortFirmwareValidationWindowTwentyFourHours:
		return pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_TWENTY_FOUR_HOURS
	default:
		return pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_UNSPECIFIED
	}
}

func firmwareValidationStateToProto(state models.CohortFirmwareValidationState) pb.CohortFirmwareValidationState {
	switch state {
	case models.CohortFirmwareValidationStateAvailable:
		return pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_AVAILABLE
	case models.CohortFirmwareValidationStateNoTarget:
		return pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_NO_TARGET
	case models.CohortFirmwareValidationStateTargetVersionUnknown:
		return pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_TARGET_VERSION_UNKNOWN
	case models.CohortFirmwareValidationStateNoBaseline:
		return pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_NO_BASELINE
	case models.CohortFirmwareValidationStateStabilizing:
		return pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_STABILIZING
	case models.CohortFirmwareValidationStateInsufficientTelemetry:
		return pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_INSUFFICIENT_TELEMETRY
	case models.CohortFirmwareValidationStateHistoryExpired:
		return pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_HISTORY_EXPIRED
	default:
		return pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_UNSPECIFIED
	}
}

func firmwareValidationResolutionToProto(resolution models.CohortFirmwareValidationTelemetryResolution) pb.CohortFirmwareValidationTelemetryResolution {
	switch resolution {
	case models.CohortFirmwareValidationTelemetryResolutionRaw:
		return pb.CohortFirmwareValidationTelemetryResolution_COHORT_FIRMWARE_VALIDATION_TELEMETRY_RESOLUTION_RAW
	case models.CohortFirmwareValidationTelemetryResolutionHourly:
		return pb.CohortFirmwareValidationTelemetryResolution_COHORT_FIRMWARE_VALIDATION_TELEMETRY_RESOLUTION_HOURLY
	default:
		return pb.CohortFirmwareValidationTelemetryResolution_COHORT_FIRMWARE_VALIDATION_TELEMETRY_RESOLUTION_UNSPECIFIED
	}
}

func validationMeasurementTypeToProto(measurementType telemetrymodels.MeasurementType) telemetrypb.MeasurementType {
	switch measurementType {
	case telemetrymodels.MeasurementTypeHashrate:
		return telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE
	case telemetrymodels.MeasurementTypeEfficiency:
		return telemetrypb.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY
	case telemetrymodels.MeasurementTypePower:
		return telemetrypb.MeasurementType_MEASUREMENT_TYPE_POWER
	case telemetrymodels.MeasurementTypeUnknown,
		telemetrymodels.MeasurementTypeTemperature,
		telemetrymodels.MeasurementTypeFanSpeed,
		telemetrymodels.MeasurementTypeVoltage,
		telemetrymodels.MeasurementTypeCurrent,
		telemetrymodels.MeasurementTypeUptime,
		telemetrymodels.MeasurementTypeErrorRate:
		return telemetrypb.MeasurementType_MEASUREMENT_TYPE_UNSPECIFIED
	default:
		return telemetrypb.MeasurementType_MEASUREMENT_TYPE_UNSPECIFIED
	}
}

func toProtoCohortSummary(cohort *models.Cohort) *pb.CohortSummary {
	if cohort == nil {
		return nil
	}
	out := &pb.CohortSummary{
		Id:                    cohort.ID,
		Label:                 cohort.Label,
		IsDefault:             cohort.IsDefault,
		OwnerUsername:         ptrToString(cohort.OwnerUsername),
		ExpiresAt:             timePtrToTimestamp(cohort.ExpiresAt),
		DesiredFirmwareFileId: ptrToString(cohort.DesiredFirmwareFileID),
		DesiredConfig:         desiredConfigToProto(cohort.DesiredConfig),
		State:                 toProtoState(cohort.State),
		Purpose:               cohort.Purpose,
		SourceActorType:       string(cohort.SourceActorType),
		SourceActorId:         ptrToString(cohort.SourceActorID),
		IdempotencyKey:        ptrToString(cohort.IdempotencyKey),
		CreatedAt:             timestamppb.New(cohort.CreatedAt),
		UpdatedAt:             timestamppb.New(cohort.UpdatedAt),
		ExplicitMemberCount:   cohort.ExplicitMemberCount,
		FirmwareTargets:       toProtoFirmwareTargets(cohort.FirmwareTargets),
		FirmwareProgress:      toProtoFirmwareProgress(cohort.FirmwareProgress),
		ConfigProgress:        toProtoConfigProgress(cohort.ConfigProgress),
	}
	if cohort.OwnerUserID != nil {
		out.OwnerUserId = cohort.OwnerUserID
	}
	return out
}

func toProtoCohortSummaries(cohorts []*models.Cohort) []*pb.CohortSummary {
	out := make([]*pb.CohortSummary, 0, len(cohorts))
	for _, cohort := range cohorts {
		out = append(out, toProtoCohortSummary(cohort))
	}
	return out
}

func toProtoMembers(members []models.CohortMember) []*pb.CohortMember {
	out := make([]*pb.CohortMember, 0, len(members))
	for _, member := range members {
		pbMember := &pb.CohortMember{
			CohortId:         member.CohortID,
			DeviceIdentifier: member.DeviceIdentifier,
			AddedAt:          timestamppb.New(member.AddedAt),
			Display:          toProtoDeviceDisplay(member.Display),
			FirmwareStatus:   toProtoFirmwareStatus(member.FirmwareStatus),
			ConfigStatuses:   toProtoConfigStatuses(member.ConfigStatuses),
		}
		out = append(out, pbMember)
	}
	return out
}

func toProtoFirmwareTargets(targets []models.CohortFirmwareTarget) []*pb.CohortFirmwareTarget {
	out := make([]*pb.CohortFirmwareTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, &pb.CohortFirmwareTarget{
			Manufacturer:   target.Manufacturer,
			Model:          target.Model,
			FirmwareFileId: ptrToString(target.FirmwareFileID),
		})
	}
	return out
}

func toProtoCohortDevices(devices []models.CohortDevice) []*pb.CohortDevice {
	out := make([]*pb.CohortDevice, 0, len(devices))
	for _, device := range devices {
		pbDevice := &pb.CohortDevice{
			DeviceIdentifier: device.DeviceIdentifier,
			EffectiveCohort:  toProtoCohortSummary(&device.EffectiveCohort),
			Display:          toProtoDeviceDisplay(device.Display),
			FirmwareStatus:   toProtoFirmwareStatus(device.FirmwareStatus),
			ConfigStatuses:   toProtoConfigStatuses(device.ConfigStatuses),
		}
		out = append(out, pbDevice)
	}
	return out
}

func toProtoConfigStatuses(statuses []models.CohortConfigStatus) []*pb.CohortConfigStatus {
	out := make([]*pb.CohortConfigStatus, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, &pb.CohortConfigStatus{
			Dimension: toProtoConfigDimension(status.Dimension), Supported: status.Supported,
			State: toProtoConfigLifecycleState(status.State), RetryCount: status.RetryCount,
			LastError: ptrToString(status.LastError), LastDispatchedAt: timePtrToTimestamp(status.LastDispatchedAt),
			ConfirmedAt: timePtrToTimestamp(status.ConfirmedAt), ObservedAt: timePtrToTimestamp(status.ObservedAt),
		})
	}
	return out
}

func toProtoConfigProgress(progress []models.CohortConfigProgress) []*pb.CohortConfigProgress {
	out := make([]*pb.CohortConfigProgress, 0, len(progress))
	for _, item := range progress {
		out = append(out, &pb.CohortConfigProgress{
			Dimension: toProtoConfigDimension(item.Dimension), TargetedCount: item.TargetedCount,
			UnsupportedCount: item.UnsupportedCount, WaitingCount: item.WaitingCount,
			ApplyingCount: item.ApplyingCount, VerifyingCount: item.VerifyingCount,
			ConvergedCount: item.ConvergedCount, HeldCount: item.HeldCount, FailedCount: item.FailedCount,
		})
	}
	return out
}

func toProtoConfigDimension(dimension models.CohortConfigDimension) pb.CohortConfigDimension {
	if dimension == models.CohortConfigDimensionPools {
		return pb.CohortConfigDimension_COHORT_CONFIG_DIMENSION_POOLS
	}
	return pb.CohortConfigDimension_COHORT_CONFIG_DIMENSION_UNSPECIFIED
}

func toProtoConfigLifecycleState(state models.CohortConfigLifecycleState) pb.CohortConfigLifecycleState {
	switch state {
	case models.CohortConfigStateUnsupported:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_UNSUPPORTED
	case models.CohortConfigStateWaitingForObservation:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_WAITING_FOR_OBSERVATION
	case models.CohortConfigStateApplying:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_APPLYING
	case models.CohortConfigStateVerifying:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_VERIFYING
	case models.CohortConfigStateConverged:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_CONVERGED
	case models.CohortConfigStateHeld:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_HELD
	case models.CohortConfigStateFailed:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_FAILED
	default:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_UNSPECIFIED
	}
}

func toProtoDeviceDisplay(display models.CohortDeviceDisplay) *pb.CohortDeviceDisplay {
	return &pb.CohortDeviceDisplay{
		Name:            display.Name,
		WorkerName:      display.WorkerName,
		Manufacturer:    display.Manufacturer,
		Model:           display.Model,
		IpAddress:       display.IPAddress,
		SerialNumber:    display.SerialNumber,
		FirmwareVersion: display.FirmwareVersion,
	}
}

func toProtoFirmwareStatus(status *models.CohortFirmwareStatus) *pb.CohortFirmwareStatus {
	if status == nil {
		return nil
	}
	return &pb.CohortFirmwareStatus{
		TargetFirmwareFileId:   status.TargetFirmwareFileID,
		TargetFirmwareVersion:  status.TargetFirmwareVersion,
		CurrentFirmwareVersion: status.CurrentFirmwareVersion,
		State:                  toProtoFirmwareRolloutState(status.State),
		RetryCount:             status.RetryCount,
		LastError:              ptrToString(status.LastError),
		LastDispatchedAt:       timePtrToTimestamp(status.LastDispatchedAt),
		ConfirmedAt:            timePtrToTimestamp(status.ConfirmedAt),
		ObservedAt:             timePtrToTimestamp(status.ObservedAt),
	}
}

func toProtoFirmwareProgress(progress models.CohortFirmwareProgress) *pb.CohortFirmwareProgress {
	if progress.TargetedCount == 0 {
		return nil
	}
	return &pb.CohortFirmwareProgress{
		TargetedCount:       progress.TargetedCount,
		CompleteCount:       progress.CompleteCount,
		QueuedCount:         progress.QueuedCount,
		UpdatingCount:       progress.UpdatingCount,
		VerifyingCount:      progress.VerifyingCount,
		NeedsAttentionCount: progress.NeedsAttentionCount,
		UnknownCount:        progress.UnknownCount,
	}
}

func toProtoState(state models.CohortState) pb.CohortState {
	switch state {
	case models.CohortStateActive:
		return pb.CohortState_COHORT_STATE_ACTIVE
	case models.CohortStateReleased:
		return pb.CohortState_COHORT_STATE_RELEASED
	default:
		return pb.CohortState_COHORT_STATE_UNSPECIFIED
	}
}

func toProtoFirmwareRolloutState(state models.CohortFirmwareRolloutState) pb.CohortFirmwareRolloutState {
	switch state {
	case models.CohortFirmwareRolloutStateNoTarget:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_NO_TARGET
	case models.CohortFirmwareRolloutStateQueued:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_QUEUED
	case models.CohortFirmwareRolloutStateUpdating:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_UPDATING
	case models.CohortFirmwareRolloutStateVerifying:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_VERIFYING
	case models.CohortFirmwareRolloutStateComplete:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_COMPLETE
	case models.CohortFirmwareRolloutStateNeedsAttention:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_NEEDS_ATTENTION
	case models.CohortFirmwareRolloutStateUnknown:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_UNKNOWN
	default:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_UNSPECIFIED
	}
}

func desiredConfigFromProto(config *pb.CohortDesiredConfig) *models.CohortDesiredConfig {
	if config == nil || config.GetPools() == nil {
		return nil
	}
	return &models.CohortDesiredConfig{Pools: &models.CohortPoolDesiredConfig{
		PrimaryPoolID: config.GetPools().GetPrimaryPoolId(),
		Backup1PoolID: config.GetPools().Backup_1PoolId,
		Backup2PoolID: config.GetPools().Backup_2PoolId,
	}}
}

func desiredConfigToProto(config *models.CohortDesiredConfig) *pb.CohortDesiredConfig {
	if config == nil || config.Pools == nil {
		return nil
	}
	return &pb.CohortDesiredConfig{Pools: &pb.CohortPoolDesiredConfig{
		PrimaryPoolId:  config.Pools.PrimaryPoolID,
		Backup_1PoolId: config.Pools.Backup1PoolID,
		Backup_2PoolId: config.Pools.Backup2PoolID,
	}}
}

func timestampToPtr(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}

func timePtrToTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stringPtrFromOptional(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func deriveSourceActorType(info *session.Info) models.SourceActorType {
	if info == nil {
		return models.SourceActorUser
	}
	if info.Actor == session.ActorScheduler {
		return models.SourceActorScheduler
	}
	if info.Actor == session.ActorCohort {
		return models.SourceActorCohort
	}
	if info.AuthMethod == session.AuthMethodAPIKey {
		return models.SourceActorAPIKey
	}
	return models.SourceActorUser
}

func deriveSourceActorID(info *session.Info) *string {
	if info == nil || info.Actor == session.ActorScheduler {
		return nil
	}
	id := info.CredentialID()
	if id == "" {
		return nil
	}
	return &id
}
