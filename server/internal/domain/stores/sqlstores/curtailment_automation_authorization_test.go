package sqlstores

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
)

func TestAutomationGrantPermissionsKeepFanReadAuthorityWithinAuthorizedFanSites(t *testing.T) {
	t.Parallel()
	minerSite, fanSite := int64(1), int64(2)
	permissions := automationGrantPermissions(models.AuthorizationEnvelope{SelectedResourceSiteIDs: []int64{minerSite}, FacilityFanSiteIDs: []int64{fanSite}})
	assert.True(t, permissions.Has(authz.PermCurtailmentManage, authz.ResourceContext{SiteID: &minerSite}))
	assert.False(t, permissions.Has(authz.PermSiteRead, authz.ResourceContext{SiteID: &minerSite}))
	assert.True(t, permissions.Has(authz.PermCurtailmentManage, authz.ResourceContext{SiteID: &fanSite}))
	assert.True(t, permissions.Has(authz.PermSiteRead, authz.ResourceContext{SiteID: &fanSite}))
	otherSite := int64(3)
	assert.False(t, permissions.Has(authz.PermCurtailmentManage, authz.ResourceContext{SiteID: &otherSite}))
}

func TestAutomationGrantDoesNotTreatFacilityFanSiteAsMinerScope(t *testing.T) {
	t.Parallel()
	grant := models.AuthorizationEnvelope{SelectedResourceSiteIDs: []int64{1}, FacilityFanSiteIDs: []int64{2}}
	event := grant
	assert.True(t, automationGrantCoversScope(grant, event, []int64{1}, []int64{1}, false))
	assert.False(t, automationGrantCoversScope(grant, event, []int64{2}, []int64{2}, false))
	event.FacilityFanSiteIDs = []int64{1}
	assert.False(t, automationGrantCoversScope(grant, event, []int64{1}, nil, false))
}

func TestAutomationDispatchInvalidIdentityIsPolicyDenial(t *testing.T) {
	t.Parallel()
	for _, identity := range []string{"not-a-rule", "0", "-1", "9223372036854775808"} {
		t.Run(identity, func(t *testing.T) {
			t.Parallel()
			event := &models.Event{SourceActorID: &identity}
			allowed, err := (&SQLCurtailmentStore{}).AuthorizeAutomationCurtailDispatch(t.Context(), event, interfaces.CurtailmentTopologyScopeCoverage{})
			assert.NoError(t, err)
			assert.False(t, allowed)
			assert.True(t, fleeterror.IsFailedPreconditionError(lockAutomationGrantForDispatch(t.Context(), nil, event)))
		})
	}
}
