package curtailment

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

var authorizationEnvelopeFields = [...]string{
	"schema_version",
	"selected_resource_site_ids",
	"current_member_site_ids",
	"miner_scope_unbounded",
	"facility_fan_site_ids",
	"facility_fan_scope_unbounded",
}

// AuthorizationEnvelopeAllows reports whether the current permission snapshot
// covers both the persisted authorization envelope and any current topology
// coverage discovered at a later execution fence.
func AuthorizationEnvelopeAllows(
	effective *authz.EffectivePermissions,
	envelope models.AuthorizationEnvelope,
	currentSelectedResourceSiteIDs []int64,
	currentMemberSiteIDs []int64,
	currentRequiresOrgWide bool,
) bool {
	if effective == nil {
		return false
	}
	requireOrgWideManage := envelope.MinerScopeUnbounded ||
		envelope.FacilityFanScopeUnbounded ||
		currentRequiresOrgWide
	if requireOrgWideManage && !effective.HasOrgWide(authz.PermCurtailmentManage) {
		return false
	}
	manageSites := make(map[int64]struct{})
	for _, sites := range [][]int64{
		envelope.SelectedResourceSiteIDs,
		envelope.CurrentMemberSiteIDs,
		envelope.FacilityFanSiteIDs,
		currentSelectedResourceSiteIDs,
		currentMemberSiteIDs,
	} {
		for _, siteID := range sites {
			manageSites[siteID] = struct{}{}
		}
	}
	for siteID := range manageSites {
		id := siteID
		if !effective.Has(authz.PermCurtailmentManage, authz.ResourceContext{SiteID: &id}) {
			return false
		}
	}
	if envelope.FacilityFanScopeUnbounded && !effective.HasOrgWide(authz.PermSiteRead) {
		return false
	}
	for _, siteID := range envelope.FacilityFanSiteIDs {
		id := siteID
		if !effective.Has(authz.PermSiteRead, authz.ResourceContext{SiteID: &id}) {
			return false
		}
	}
	return true
}

// AuthorizationEnvelopeFromJSON parses the persisted authorization snapshot.
// It is intentionally stricter than the database shape checks so every caller
// fails closed on missing, unknown, null, or invalid coverage.
func AuthorizationEnvelopeFromJSON(raw []byte) (models.AuthorizationEnvelope, error) {
	var fields map[string]json.RawMessage
	if len(raw) == 0 {
		return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("must be set")
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("must be a JSON object: %v", err)
	}
	if fields == nil {
		return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("must be a JSON object")
	}
	if len(fields) != len(authorizationEnvelopeFields) {
		return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("must contain exactly the supported fields")
	}
	for _, field := range authorizationEnvelopeFields {
		if _, ok := fields[field]; !ok {
			return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("is missing %q", field)
		}
	}
	for _, field := range [...]string{"miner_scope_unbounded", "facility_fan_scope_unbounded"} {
		if bytes.Equal(bytes.TrimSpace(fields[field]), []byte("null")) {
			return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("%s must be a boolean", field)
		}
	}

	var envelope models.AuthorizationEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("contains an invalid field: %v", err)
	}
	if envelope.SchemaVersion != models.AuthorizationEnvelopeSchemaVersion {
		return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope(
			"has unsupported schema_version %d", envelope.SchemaVersion,
		)
	}
	if envelope.SelectedResourceSiteIDs == nil ||
		envelope.CurrentMemberSiteIDs == nil ||
		envelope.FacilityFanSiteIDs == nil {
		return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("site ID fields must be arrays")
	}
	if err := validateAuthorizationEnvelopeSiteIDs(envelope.SelectedResourceSiteIDs, "selected_resource_site_ids"); err != nil {
		return models.AuthorizationEnvelope{}, err
	}
	if err := validateAuthorizationEnvelopeSiteIDs(envelope.CurrentMemberSiteIDs, "current_member_site_ids"); err != nil {
		return models.AuthorizationEnvelope{}, err
	}
	if err := validateAuthorizationEnvelopeSiteIDs(envelope.FacilityFanSiteIDs, "facility_fan_site_ids"); err != nil {
		return models.AuthorizationEnvelope{}, err
	}
	if !envelope.MinerScopeUnbounded &&
		len(envelope.SelectedResourceSiteIDs) == 0 &&
		len(envelope.CurrentMemberSiteIDs) == 0 {
		return models.AuthorizationEnvelope{}, invalidAuthorizationEnvelope("has no bounded miner coverage")
	}
	return envelope, nil
}

func validateAuthorizationEnvelopeSiteIDs(siteIDs []int64, field string) error {
	for _, siteID := range siteIDs {
		if siteID <= 0 {
			return invalidAuthorizationEnvelope("%s must contain only positive IDs", field)
		}
	}
	return nil
}

func invalidAuthorizationEnvelope(format string, args ...any) error {
	return fleeterror.NewInvalidArgumentError(fmt.Sprintf("curtailment authorization envelope "+format, args...))
}
