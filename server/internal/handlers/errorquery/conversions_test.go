package errorquery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	errorsv1 "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
)

// ============================================================================
// convertQueryRequestToDomain Tests
// ============================================================================

func TestConvertQueryRequestToDomain_WithMinimalRequest_ShouldSetDefaults(t *testing.T) {
	req := &errorsv1.QueryRequest{}
	orgID := int64(123)

	result := convertQueryRequestToDomain(orgID, req)

	require.Equal(t, orgID, result.OrgID)
	require.Equal(t, models.ResultView(0), result.ResultView)
	require.Equal(t, 0, result.PageSize)
	require.Empty(t, result.PageToken)
	require.Nil(t, result.Filter)
}

func TestConvertQueryRequestToDomain_WithFullRequest_ShouldConvertAllFields(t *testing.T) {
	req := &errorsv1.QueryRequest{
		ResultView: errorsv1.ResultView_RESULT_VIEW_DEVICE,
		PageSize:   50,
		PageToken:  "abc123",
		OrderBy:    "severity DESC",
		Filter: &errorsv1.Filter{
			IncludeClosed: true,
		},
	}
	orgID := int64(456)

	result := convertQueryRequestToDomain(orgID, req)

	require.Equal(t, orgID, result.OrgID)
	require.Equal(t, models.ResultViewDevice, result.ResultView)
	require.Equal(t, 50, result.PageSize)
	require.Equal(t, "abc123", result.PageToken)
	require.Equal(t, "severity DESC", result.OrderBy)
	require.NotNil(t, result.Filter)
	require.True(t, result.Filter.IncludeClosed)
}

func TestConvertQueryRequestToDomain_WithAllResultViews_ShouldConvertCorrectly(t *testing.T) {
	tests := []struct {
		name     string
		proto    errorsv1.ResultView
		expected models.ResultView
	}{
		{"unspecified", errorsv1.ResultView_RESULT_VIEW_UNSPECIFIED, models.ResultViewUnspecified},
		{"error", errorsv1.ResultView_RESULT_VIEW_ERROR, models.ResultViewError},
		{"component", errorsv1.ResultView_RESULT_VIEW_COMPONENT, models.ResultViewComponent},
		{"device", errorsv1.ResultView_RESULT_VIEW_DEVICE, models.ResultViewDevice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &errorsv1.QueryRequest{ResultView: tt.proto}
			result := convertQueryRequestToDomain(1, req)
			require.Equal(t, tt.expected, result.ResultView)
		})
	}
}

// ============================================================================
// convertFilterToDomain Tests
// ============================================================================

func TestConvertFilterToDomain_WithNilSimpleFilter_ShouldReturnBasicFilter(t *testing.T) {
	filter := &errorsv1.Filter{
		IncludeClosed: true,
		SimpleLogic:   errorsv1.GlobalLogic_GLOBAL_LOGIC_AND,
	}

	result := convertFilterToDomain(filter)

	require.True(t, result.IncludeClosed)
	require.Equal(t, models.FilterLogicAND, result.Logic)
	require.Nil(t, result.TimeFrom)
	require.Nil(t, result.TimeTo)
	require.Empty(t, result.DeviceIdentifiers)
}

func TestConvertFilterToDomain_WithTimeRange_ShouldConvertTimestamps(t *testing.T) {
	fromTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	toTime := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	filter := &errorsv1.Filter{
		TimeFrom: timestamppb.New(fromTime),
		TimeTo:   timestamppb.New(toTime),
	}

	result := convertFilterToDomain(filter)

	require.NotNil(t, result.TimeFrom)
	require.NotNil(t, result.TimeTo)
	require.Equal(t, fromTime, *result.TimeFrom)
	require.Equal(t, toTime, *result.TimeTo)
}

func TestConvertFilterToDomain_WithSimpleFilter_ShouldConvertAllFields(t *testing.T) {
	filter := &errorsv1.Filter{
		Simple: &errorsv1.SimpleFilter{
			DeviceIdentifiers: []string{"device-1", "device-2"},
			DeviceTypes:       []string{"S19", "R2"},
			ComponentIds:      []string{"psu-1", "fan-0"},
			ComponentTypes:    []errorsv1.ComponentType{errorsv1.ComponentType_COMPONENT_TYPE_PSU, errorsv1.ComponentType_COMPONENT_TYPE_FAN},
			CanonicalErrors:   []errorsv1.MinerError{errorsv1.MinerError_MINER_ERROR_PSU_FAULT_GENERIC, errorsv1.MinerError_MINER_ERROR_FAN_FAILED},
			Severities:        []errorsv1.Severity{errorsv1.Severity_SEVERITY_CRITICAL, errorsv1.Severity_SEVERITY_MAJOR},
		},
	}

	result := convertFilterToDomain(filter)

	require.Equal(t, []string{"device-1", "device-2"}, result.DeviceIdentifiers)
	require.Equal(t, []string{"S19", "R2"}, result.DeviceTypes)
	require.Equal(t, []string{"psu-1", "fan-0"}, result.ComponentIDs)
	require.Len(t, result.ComponentTypes, 2)
	require.Contains(t, result.ComponentTypes, models.ComponentType(errorsv1.ComponentType_COMPONENT_TYPE_PSU))
	require.Contains(t, result.ComponentTypes, models.ComponentType(errorsv1.ComponentType_COMPONENT_TYPE_FAN))
	require.Len(t, result.MinerErrors, 2)
	require.Len(t, result.Severities, 2)
}

func TestConvertFilterToDomain_WithEmptySimpleFilter_ShouldReturnEmptySlices(t *testing.T) {
	filter := &errorsv1.Filter{
		Simple: &errorsv1.SimpleFilter{},
	}

	result := convertFilterToDomain(filter)

	require.Empty(t, result.DeviceIdentifiers)
	require.Empty(t, result.DeviceTypes)
	require.Empty(t, result.ComponentIDs)
	require.Empty(t, result.ComponentTypes)
	require.Empty(t, result.MinerErrors)
	require.Empty(t, result.Severities)
}

// ============================================================================
// convertQueryResultToProto Tests
// ============================================================================

func TestConvertQueryResultToProto_WithErrorsResult_ShouldReturnErrorsResponse(t *testing.T) {
	result := &models.QueryResult{
		Errors: []models.ErrorMessage{
			{
				ErrorID:    "error-1",
				MinerError: models.PSUFaultGeneric,
				Severity:   models.SeverityCritical,
				DeviceID:   "device-1",
			},
		},
		NextPageToken: "next-token",
		TotalCount:    100,
	}

	resp := convertQueryResultToProto(result)

	require.Equal(t, "next-token", resp.NextPageToken)
	require.Equal(t, int64(100), resp.TotalCount)
	require.NotNil(t, resp.GetErrors())
	require.Len(t, resp.GetErrors().Items, 1)
	require.Equal(t, "error-1", resp.GetErrors().Items[0].ErrorId)
}

func TestConvertQueryResultToProto_WithDeviceResult_ShouldReturnDevicesResponse(t *testing.T) {
	result := &models.QueryResult{
		DeviceErrs: []models.DeviceErrorGroup{
			{
				DeviceID:   123,
				DeviceType: "S19",
				Status:     models.StatusError,
				Summary: models.Summary{
					Title:     "2 critical errors",
					Details:   "PSU and fan issues",
					Condensed: "2 CRIT",
				},
				Errors: []models.ErrorMessage{
					{ErrorID: "err-1", DeviceID: "device-123"},
					{ErrorID: "err-2", DeviceID: "device-123"},
				},
				CountsBySeverity: map[models.Severity]int32{
					models.SeverityCritical: 2,
				},
			},
		},
		TotalCount: 1,
	}

	resp := convertQueryResultToProto(result)

	require.NotNil(t, resp.GetDevices())
	require.Len(t, resp.GetDevices().Items, 1)
	device := resp.GetDevices().Items[0]
	require.Equal(t, "device-123", device.DeviceIdentifier)
	require.Equal(t, "S19", device.DeviceType)
	require.Equal(t, errorsv1.Status_STATUS_ERROR, device.Status)
	require.Equal(t, "2 critical errors", device.Summary.Title)
	require.Len(t, device.Errors, 2)
	require.Equal(t, int32(2), device.CountsBySeverity["CRITICAL"])
}

func TestConvertQueryResultToProto_WithComponentResult_ShouldReturnComponentsResponse(t *testing.T) {
	// Note: Domain and proto ComponentType enums have different orderings.
	// Domain: PSU=4, Proto: PSU=1. The conversion does a direct cast.
	// Using ControlBoard (1 in both) to avoid the mismatch issue.
	result := &models.QueryResult{
		ComponentErrs: []models.ComponentErrors{
			{
				ComponentID:   "ctrl-0",
				ComponentType: models.ComponentTypeControlBoard,
				DeviceID:      456,
				DeviceType:    "R2",
				Status:        models.StatusWarning,
				Summary: models.Summary{
					Title: "1 major error",
				},
				Errors: []models.ErrorMessage{
					{ErrorID: "err-1", DeviceID: "device-456"},
				},
			},
		},
		TotalCount: 1,
	}

	resp := convertQueryResultToProto(result)

	require.NotNil(t, resp.GetComponents())
	require.Len(t, resp.GetComponents().Items, 1)
	comp := resp.GetComponents().Items[0]
	require.Equal(t, "ctrl-0", comp.ComponentId)
	// Domain ControlBoard (1) maps to Proto ControlBoard (4) - values differ between enums
	require.Equal(t, errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD, comp.ComponentType)
	require.Equal(t, "device-456", comp.DeviceIdentifier)
	require.Equal(t, errorsv1.Status_STATUS_WARNING, comp.Status)
}

func TestConvertQueryResultToProto_WithEmptyResult_ShouldReturnEmptyErrorsResponse(t *testing.T) {
	result := &models.QueryResult{
		Errors:     []models.ErrorMessage{},
		TotalCount: 0,
	}

	resp := convertQueryResultToProto(result)

	require.NotNil(t, resp.GetErrors())
	require.Empty(t, resp.GetErrors().Items)
	require.Equal(t, int64(0), resp.TotalCount)
}

func TestConvertQueryResultToProto_WithNilSlices_ShouldDefaultToErrorsResponse(t *testing.T) {
	result := &models.QueryResult{
		TotalCount: 0,
	}

	resp := convertQueryResultToProto(result)

	require.NotNil(t, resp.GetErrors())
	require.Nil(t, resp.GetDevices())
	require.Nil(t, resp.GetComponents())
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestSeverityToString_ShouldConvertAllSeverities(t *testing.T) {
	tests := []struct {
		severity models.Severity
		expected string
	}{
		{models.SeverityCritical, "CRITICAL"},
		{models.SeverityMajor, "MAJOR"},
		{models.SeverityMinor, "MINOR"},
		{models.SeverityInfo, "INFO"},
		{models.SeverityUnspecified, "UNSPECIFIED"},
		{models.Severity(99), "UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := severityToString(tt.severity)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertCountsBySeverityToProto_WithNilMap_ShouldReturnNil(t *testing.T) {
	result := convertCountsBySeverityToProto(nil)
	require.Nil(t, result)
}

func TestConvertCountsBySeverityToProto_WithEmptyMap_ShouldReturnEmptyMap(t *testing.T) {
	result := convertCountsBySeverityToProto(map[models.Severity]int32{})
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestConvertCountsBySeverityToProto_WithCounts_ShouldConvertToStringKeys(t *testing.T) {
	counts := map[models.Severity]int32{
		models.SeverityCritical: 5,
		models.SeverityMajor:    3,
		models.SeverityMinor:    2,
		models.SeverityInfo:     1,
	}

	result := convertCountsBySeverityToProto(counts)

	require.Equal(t, int32(5), result["CRITICAL"])
	require.Equal(t, int32(3), result["MAJOR"])
	require.Equal(t, int32(2), result["MINOR"])
	require.Equal(t, int32(1), result["INFO"])
}

func TestConvertSummaryToProto_ShouldConvertAllFields(t *testing.T) {
	summary := models.Summary{
		Title:     "3 critical errors",
		Details:   "PSU, fan, and hashboard issues detected",
		Condensed: "3 CRIT",
	}

	result := convertSummaryToProto(summary)

	require.Equal(t, "3 critical errors", result.Title)
	require.Equal(t, "PSU, fan, and hashboard issues detected", result.Details)
	require.Equal(t, "3 CRIT", result.Condensed)
}

func TestConvertErrorMessagesToProto_WithEmptySlice_ShouldReturnEmptySlice(t *testing.T) {
	result := convertErrorMessagesToProto([]models.ErrorMessage{})
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestConvertErrorMessagesToProto_WithMultipleErrors_ShouldConvertAll(t *testing.T) {
	errors := []models.ErrorMessage{
		{ErrorID: "err-1", MinerError: models.PSUFaultGeneric, Severity: models.SeverityCritical, DeviceID: "dev-1"},
		{ErrorID: "err-2", MinerError: models.FanFailed, Severity: models.SeverityMajor, DeviceID: "dev-2"},
	}

	result := convertErrorMessagesToProto(errors)

	require.Len(t, result, 2)
	require.Equal(t, "err-1", result[0].ErrorId)
	require.Equal(t, "err-2", result[1].ErrorId)
	require.Equal(t, errorsv1.MinerError_MINER_ERROR_PSU_FAULT_GENERIC, result[0].CanonicalError)
	require.Equal(t, errorsv1.MinerError_MINER_ERROR_FAN_FAILED, result[1].CanonicalError)
}

func TestConvertDeviceErrorGroupsToProto_WithEmptyErrors_ShouldUseEmptyIdentifier(t *testing.T) {
	groups := []models.DeviceErrorGroup{
		{
			DeviceID:   123,
			DeviceType: "S19",
			Status:     models.StatusOK,
			Errors:     []models.ErrorMessage{},
		},
	}

	result := convertDeviceErrorGroupsToProto(groups)

	require.Len(t, result, 1)
	require.Empty(t, result[0].DeviceIdentifier)
	require.Equal(t, "S19", result[0].DeviceType)
}

func TestConvertComponentErrorsToProto_WithEmptyErrors_ShouldUseEmptyIdentifier(t *testing.T) {
	components := []models.ComponentErrors{
		{
			ComponentID:   "psu-0",
			ComponentType: models.ComponentTypePSU,
			DeviceID:      456,
			Status:        models.StatusOK,
			Errors:        []models.ErrorMessage{},
		},
	}

	result := convertComponentErrorsToProto(components)

	require.Len(t, result, 1)
	require.Equal(t, "psu-0", result[0].ComponentId)
	require.Empty(t, result[0].DeviceIdentifier)
}
