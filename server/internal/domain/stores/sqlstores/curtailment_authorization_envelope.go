package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/block/proto-fleet/server/generated/sqlc"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

func buildAuthorizationEnvelopeJSON(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	scopeType models.ScopeType,
	scopeJSON []byte,
	facilityFanSiteIDs []int64,
	expectedDeviceSites map[string]*int64,
) ([]byte, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInternalError("authorization envelope requires an organization")
	}
	if len(scopeJSON) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("curtailment scope_json must be an object")
	}
	scope, hasScope, err := domainCurtailment.ScopeFromJSON(scopeJSON)
	if err != nil {
		return nil, err
	}
	if !hasScope {
		scope.Type = models.ScopeTypeWholeOrg
	}
	if scopeType == "" {
		scopeType = scope.Type
	} else if scopeType != scope.Type {
		return nil, fleeterror.NewInvalidArgumentError("curtailment scope_type does not match scope_json")
	}

	envelope := models.AuthorizationEnvelope{
		SchemaVersion:           models.AuthorizationEnvelopeSchemaVersion,
		SelectedResourceSiteIDs: []int64{},
		CurrentMemberSiteIDs:    []int64{},
		FacilityFanSiteIDs:      uniqueSortedInt64s(facilityFanSiteIDs),
	}

	switch scopeType {
	case models.ScopeTypeWholeOrg:
		envelope.MinerScopeUnbounded = true
	case models.ScopeTypeSite:
		if scope.SiteID <= 0 {
			return nil, fleeterror.NewInvalidArgumentError("site scope must contain a positive site_id")
		}
		envelope.SelectedResourceSiteIDs = []int64{scope.SiteID}
	case models.ScopeTypeDeviceList:
		sites, unbounded, err := lockDeviceScopeCoverage(ctx, q, orgID, scope.DeviceIdentifiers, expectedDeviceSites)
		if err != nil {
			return nil, err
		}
		envelope.CurrentMemberSiteIDs = sites
		envelope.MinerScopeUnbounded = unbounded
	case models.ScopeTypeMixed:
		switch {
		case len(scope.SiteIDs) > 0:
			envelope.SelectedResourceSiteIDs = uniqueSortedInt64s(scope.SiteIDs)
		case len(scope.BuildingIDs) > 0, len(scope.RackIDs) > 0, len(scope.GroupIDs) > 0:
			coverage, err := resolveCurtailmentTopologyScope(ctx, q, interfaces.ListCandidatesParams{
				OrgID:       orgID,
				BuildingIDs: scope.BuildingIDs,
				RackIDs:     scope.RackIDs,
				GroupIDs:    scope.GroupIDs,
			})
			if err != nil {
				return nil, err
			}
			envelope.SelectedResourceSiteIDs = coverage.SelectedResourceSiteIDs
			envelope.CurrentMemberSiteIDs = coverage.CurrentMemberSiteIDs
			envelope.MinerScopeUnbounded = coverage.RequireOrgWide
		default:
			return nil, fleeterror.NewInvalidArgumentError("mixed scope has no terminal selector IDs")
		}
	default:
		return nil, fleeterror.NewInvalidArgumentErrorf("unsupported curtailment scope type: %q", scopeType)
	}
	envelope.SelectedResourceSiteIDs = nonNilInt64Slice(envelope.SelectedResourceSiteIDs)
	envelope.CurrentMemberSiteIDs = nonNilInt64Slice(envelope.CurrentMemberSiteIDs)
	envelope.FacilityFanSiteIDs = nonNilInt64Slice(envelope.FacilityFanSiteIDs)

	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to encode curtailment authorization envelope: %v", err)
	}
	return encoded, nil
}

func nonNilInt64Slice(values []int64) []int64 {
	if values == nil {
		return []int64{}
	}
	return values
}

func lockDeviceScopeCoverage(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	deviceIdentifiers []string,
	expectedDeviceSites map[string]*int64,
) ([]int64, bool, error) {
	identifiers := uniqueSortedStrings(deviceIdentifiers)
	if len(identifiers) == 0 {
		return nil, false, fleeterror.NewInvalidArgumentError("device scope must contain at least one device identifier")
	}
	rows, err := q.LockCurtailmentResponseProfileDeviceSitesByOrg(ctx, sqlc.LockCurtailmentResponseProfileDeviceSitesByOrgParams{
		OrgID:             orgID,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		return nil, false, fleeterror.NewInternalErrorf("failed to lock curtailment device scope: %v", err)
	}
	identifierSet := make(map[string]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		identifierSet[identifier] = struct{}{}
	}
	for identifier := range expectedDeviceSites {
		if _, ok := identifierSet[identifier]; !ok {
			return nil, false, fleeterror.NewFailedPreconditionError("curtailment device authorization changed before save; retry")
		}
	}
	rowsByIdentifier := make(map[string]sqlc.LockCurtailmentResponseProfileDeviceSitesByOrgRow, len(rows))
	for _, row := range rows {
		rowsByIdentifier[row.DeviceIdentifier] = row
	}
	sites := make([]int64, 0, len(rows))
	unbounded := false
	for _, identifier := range identifiers {
		row, exists := rowsByIdentifier[identifier]
		if expectedDeviceSites != nil {
			expectedSiteID, wasAuthorized := expectedDeviceSites[identifier]
			if wasAuthorized != exists || (wasAuthorized && !sameOptionalSiteID(expectedSiteID, row.SiteID)) {
				return nil, false, fleeterror.NewFailedPreconditionError("curtailment device authorization changed before save; retry")
			}
		}
		if !exists {
			unbounded = true
			continue
		}
		if row.SiteID.Valid {
			sites = append(sites, row.SiteID.Int64)
		} else {
			unbounded = true
		}
	}
	return uniqueSortedInt64s(sites), unbounded, nil
}

func sameOptionalSiteID(expected *int64, actual sql.NullInt64) bool {
	if expected == nil {
		return !actual.Valid
	}
	return actual.Valid && *expected == actual.Int64
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
