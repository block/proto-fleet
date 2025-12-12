// Package errorquery provides gRPC handlers for the error query service.
package errorquery

import (
	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
)

// ============================================================================
// Proto → Domain Conversions
// ============================================================================

// convertQueryRequestToDomain converts a proto QueryRequest to domain QueryOptions.
func convertQueryRequestToDomain(orgID int64, req *errorsv1.QueryRequest) *models.QueryOptions {
	opts := &models.QueryOptions{
		OrgID:      orgID,
		ResultView: models.ResultView(req.GetResultView()), // #nosec G115 -- ResultView enum bounded (max 3), safe for uint
		PageSize:   int(req.GetPageSize()),
		PageToken:  req.GetPageToken(),
		OrderBy:    req.GetOrderBy(),
	}

	if req.GetFilter() != nil {
		opts.Filter = convertFilterToDomain(req.GetFilter())
	}

	return opts
}

// convertFilterToDomain converts a proto Filter to domain QueryFilter.
func convertFilterToDomain(filter *errorsv1.Filter) *models.QueryFilter {
	domainFilter := &models.QueryFilter{
		IncludeClosed: filter.GetIncludeClosed(),
		Logic:         models.FilterLogic(filter.GetSimpleLogic()), // #nosec G115 -- GlobalLogic enum bounded (max 2), safe for uint
	}

	if filter.GetTimeFrom() != nil {
		t := filter.GetTimeFrom().AsTime()
		domainFilter.TimeFrom = &t
	}

	if filter.GetTimeTo() != nil {
		t := filter.GetTimeTo().AsTime()
		domainFilter.TimeTo = &t
	}

	simple := filter.GetSimple()
	if simple != nil {
		domainFilter.DeviceIdentifiers = simple.GetDeviceIdentifiers()
		domainFilter.DeviceTypes = simple.GetDeviceTypes()
		domainFilter.ComponentIDs = simple.GetComponentIds()

		for _, ct := range simple.GetComponentTypes() {
			// #nosec G115 -- ComponentType enum bounded (max 6), safe for uint
			domainFilter.ComponentTypes = append(domainFilter.ComponentTypes, models.ComponentType(ct))
		}

		for _, me := range simple.GetCanonicalErrors() {
			// #nosec G115 -- MinerError enum bounded by protobuf (max ~9000), safe for uint
			domainFilter.MinerErrors = append(domainFilter.MinerErrors, models.MinerError(me))
		}

		for _, sev := range simple.GetSeverities() {
			// #nosec G115 -- Severity enum bounded (max 4), safe for uint
			domainFilter.Severities = append(domainFilter.Severities, models.Severity(sev))
		}
	}

	return domainFilter
}

// ============================================================================
// Domain → Proto Conversions
// ============================================================================

// convertQueryResultToProto converts a domain QueryResult to proto QueryResponse.
func convertQueryResultToProto(result *models.QueryResult) *errorsv1.QueryResponse {
	resp := &errorsv1.QueryResponse{
		NextPageToken: result.NextPageToken,
		TotalCount:    result.TotalCount,
	}

	// Determine which result type to return based on what's populated
	switch {
	case len(result.DeviceErrs) > 0:
		resp.Result = &errorsv1.QueryResponse_Devices{
			Devices: &errorsv1.DeviceErrors{
				Items: convertDeviceErrorGroupsToProto(result.DeviceErrs),
			},
		}
	case len(result.ComponentErrs) > 0:
		resp.Result = &errorsv1.QueryResponse_Components{
			Components: &errorsv1.ComponentErrors{
				Items: convertComponentErrorsToProto(result.ComponentErrs),
			},
		}
	default:
		resp.Result = &errorsv1.QueryResponse_Errors{
			Errors: &errorsv1.Errors{
				Items: convertErrorMessagesToProto(result.Errors),
			},
		}
	}

	return resp
}

// convertErrorMessagesToProto converts domain ErrorMessages to proto ErrorMessages.
func convertErrorMessagesToProto(errors []models.ErrorMessage) []*errorsv1.ErrorMessage {
	result := make([]*errorsv1.ErrorMessage, len(errors))
	for i := range errors {
		result[i] = convertDomainErrorToProto(&errors[i])
	}
	return result
}

// convertDeviceErrorGroupsToProto converts domain DeviceErrorGroups to proto DeviceErrors.
func convertDeviceErrorGroupsToProto(groups []models.DeviceErrorGroup) []*errorsv1.DeviceError {
	result := make([]*errorsv1.DeviceError, len(groups))
	for i, group := range groups {
		// Get device identifier from first error in group (all errors in group share same device)
		deviceIdentifier := ""
		if len(group.Errors) > 0 {
			deviceIdentifier = group.Errors[0].DeviceID
		}
		result[i] = &errorsv1.DeviceError{
			DeviceIdentifier: deviceIdentifier,
			DeviceType:       group.DeviceType,
			Status:           errorsv1.Status(group.Status), // #nosec G115 -- Status enum bounded (max 3), safe for int32
			Summary:          convertSummaryToProto(group.Summary),
			Errors:           convertErrorMessagesToProto(group.Errors),
			CountsBySeverity: convertCountsBySeverityToProto(group.CountsBySeverity),
		}
	}
	return result
}

// convertComponentErrorsToProto converts domain ComponentErrors to proto ComponentErrors.
func convertComponentErrorsToProto(components []models.ComponentErrors) []*errorsv1.ComponentError {
	result := make([]*errorsv1.ComponentError, len(components))
	for i, comp := range components {
		// Get device identifier from first error in group (all errors in group share same device)
		deviceIdentifier := ""
		if len(comp.Errors) > 0 {
			deviceIdentifier = comp.Errors[0].DeviceID
		}
		result[i] = &errorsv1.ComponentError{
			ComponentId:      comp.ComponentID,
			ComponentType:    errorsv1.ComponentType(comp.ComponentType), // #nosec G115 -- ComponentType enum bounded (max 6), safe for int32
			DeviceIdentifier: deviceIdentifier,
			Status:           errorsv1.Status(comp.Status), // #nosec G115 -- Status enum bounded (max 3), safe for int32
			Summary:          convertSummaryToProto(comp.Summary),
			Errors:           convertErrorMessagesToProto(comp.Errors),
			CountsBySeverity: convertCountsBySeverityToProto(comp.CountsBySeverity),
		}
	}
	return result
}

// convertSummaryToProto converts a domain Summary to proto Summary.
func convertSummaryToProto(summary models.Summary) *errorsv1.Summary {
	return &errorsv1.Summary{
		Title:     summary.Title,
		Details:   summary.Details,
		Condensed: summary.Condensed,
	}
}

// convertCountsBySeverityToProto converts domain severity counts to proto format.
// Proto uses string keys for the map while domain uses Severity enum keys.
func convertCountsBySeverityToProto(counts map[models.Severity]int32) map[string]int32 {
	if counts == nil {
		return nil
	}
	result := make(map[string]int32, len(counts))
	for sev, count := range counts {
		result[severityToString(sev)] = count
	}
	return result
}

// severityToString converts a Severity enum to its string representation.
func severityToString(sev models.Severity) string {
	switch sev {
	case models.SeverityCritical:
		return "CRITICAL"
	case models.SeverityMajor:
		return "MAJOR"
	case models.SeverityMinor:
		return "MINOR"
	case models.SeverityInfo:
		return "INFO"
	default:
		return "UNSPECIFIED"
	}
}
