package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	requiredTargetDeviceIdentifiers []string,
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
		if err := lockSiteScopeTargets(
			ctx, q, orgID, envelope.SelectedResourceSiteIDs, requiredTargetDeviceIdentifiers,
		); err != nil {
			return nil, err
		}
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
			if err := lockSiteScopeTargets(
				ctx, q, orgID, envelope.SelectedResourceSiteIDs, requiredTargetDeviceIdentifiers,
			); err != nil {
				return nil, err
			}
		case len(scope.BuildingIDs) > 0, len(scope.RackIDs) > 0, len(scope.GroupIDs) > 0:
			params := interfaces.ListCandidatesParams{
				OrgID:       orgID,
				BuildingIDs: scope.BuildingIDs,
				RackIDs:     scope.RackIDs,
				GroupIDs:    scope.GroupIDs,
			}
			lockedSites, lockedUnbounded, err := lockTopologyScopeCoverage(
				ctx,
				q,
				params,
				requiredTargetDeviceIdentifiers,
			)
			if err != nil {
				return nil, err
			}
			coverage, err := resolveCurtailmentTopologyScope(ctx, q, params)
			if err != nil {
				return nil, err
			}
			envelope.SelectedResourceSiteIDs = coverage.SelectedResourceSiteIDs
			envelope.CurrentMemberSiteIDs = uniqueSortedInt64s(append(
				append([]int64(nil), coverage.CurrentMemberSiteIDs...),
				lockedSites...,
			))
			envelope.MinerScopeUnbounded = coverage.RequireOrgWide || lockedUnbounded
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

func lockSiteScopeTargets(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	selectedSiteIDs []int64,
	requiredDeviceIdentifiers []string,
) error {
	identifiers := uniqueSortedStrings(requiredDeviceIdentifiers)
	if len(identifiers) == 0 {
		return nil
	}
	lockedSites, unbounded, err := lockDeviceScopeCoverage(ctx, q, orgID, identifiers, nil)
	if err != nil {
		return err
	}
	allowedSites := make(map[int64]struct{}, len(selectedSiteIDs))
	for _, siteID := range selectedSiteIDs {
		allowedSites[siteID] = struct{}{}
	}
	if unbounded {
		return fleeterror.NewFailedPreconditionError("curtailment site membership changed before save; retry")
	}
	for _, siteID := range lockedSites {
		if _, allowed := allowedSites[siteID]; !allowed {
			return fleeterror.NewFailedPreconditionError("curtailment site membership changed before save; retry")
		}
	}
	return nil
}

func lockTopologyScopeCoverage(
	ctx context.Context,
	q sqlc.Querier,
	params interfaces.ListCandidatesParams,
	requiredDeviceIdentifiers []string,
) ([]int64, bool, error) {
	groupIDs := uniqueSortedInt64s(params.GroupIDs)
	if len(groupIDs) > 0 {
		lockedGroupIDs, err := q.LockCurtailmentGroupsForWrite(ctx, sqlc.LockCurtailmentGroupsForWriteParams{
			OrgID: params.OrgID, GroupIds: groupIDs,
		})
		if err != nil {
			return nil, false, fleeterror.NewInternalErrorf("failed to lock curtailment group scope: %v", err)
		}
		if len(lockedGroupIDs) != len(groupIDs) {
			return nil, false, fleeterror.NewNotFoundError("one or more curtailment groups were not found")
		}
	}
	for _, buildingID := range uniqueSortedInt64s(params.BuildingIDs) {
		if _, err := q.LockBuildingForWrite(ctx, sqlc.LockBuildingForWriteParams{
			ID: buildingID, OrgID: params.OrgID,
		}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, fleeterror.NewNotFoundErrorf("building not found: %d", buildingID)
			}
			return nil, false, fleeterror.NewInternalErrorf("failed to lock curtailment building scope: %v", err)
		}
	}
	for _, rackID := range uniqueSortedInt64s(params.RackIDs) {
		if _, err := q.LockRackPlacementForWrite(ctx, sqlc.LockRackPlacementForWriteParams{
			DeviceSetID: rackID, OrgID: params.OrgID,
		}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, fleeterror.NewNotFoundErrorf("rack not found: %d", rackID)
			}
			return nil, false, fleeterror.NewInternalErrorf("failed to lock curtailment rack scope: %v", err)
		}
	}

	rows, err := q.LockCurtailmentTopologyMemberDeviceSitesByOrg(
		ctx,
		sqlc.LockCurtailmentTopologyMemberDeviceSitesByOrgParams{
			OrgID:       params.OrgID,
			BuildingIds: params.BuildingIDs,
			RackIds:     params.RackIDs,
			GroupIds:    params.GroupIDs,
		},
	)
	if err != nil {
		return nil, false, fleeterror.NewInternalErrorf("failed to lock curtailment topology members: %v", err)
	}
	memberSites := make([]int64, 0, len(rows))
	members := make(map[string]struct{}, len(rows))
	unbounded := false
	for _, row := range rows {
		members[row.DeviceIdentifier] = struct{}{}
		if row.SiteID.Valid {
			memberSites = append(memberSites, row.SiteID.Int64)
		} else {
			unbounded = true
		}
	}
	for _, identifier := range uniqueSortedStrings(requiredDeviceIdentifiers) {
		if _, ok := members[identifier]; !ok {
			return nil, false, fleeterror.NewFailedPreconditionError(
				"curtailment topology changed before save; retry",
			)
		}
	}
	return uniqueSortedInt64s(memberSites), unbounded, nil
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
