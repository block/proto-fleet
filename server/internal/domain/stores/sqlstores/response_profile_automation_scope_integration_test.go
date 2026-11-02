package sqlstores_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	buildingsmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_TopologyProfilesCanBeAutomated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	buildingStore := sqlstores.NewSQLBuildingStore(database)
	orgID := user.OrganizationID
	site, err := sqlstores.NewSQLSiteStore(database).CreateSite(ctx, sitesmodels.CreateSiteParams{
		OrgID: orgID,
		Name:  "Topology profile site",
	})
	require.NoError(t, err)
	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID:  orgID,
		SiteID: &site.ID,
		Name:   "Topology profile building",
	})
	require.NoError(t, err)
	replacementBuilding, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID:  orgID,
		SiteID: &site.ID,
		Name:   "Replacement topology profile building",
	})
	require.NoError(t, err)
	sourceID := seedMQTTSourceConfig(t, database, orgID, user.DatabaseID, "topology-profile-source", true)
	topologyProfileID := seedResponseProfile(t, database, orgID, "topology-profile")
	topologyProfile, err := store.GetResponseProfile(ctx, orgID, topologyProfileID)
	require.NoError(t, err)
	expectedTopologyScopeJSON := append([]byte(nil), topologyProfile.ScopeJSON...)
	topologyProfile.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"building_ids":[%d]}`, building.ID))
	topologyProfile, err = store.UpdateResponseProfile(
		ctx,
		*topologyProfile,
		nil,
		nil,
		topologyProfile.SiteID,
		expectedTopologyScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)

	createdRule, err := store.CreateAutomationRule(ctx, models.AutomationRule{
		AdminControlsGranted:    true,
		OrgID:                   orgID,
		RuleName:                "topology-create",
		TriggerType:             models.AutomationTriggerTypeMQTT,
		MQTTSourceID:            sourceID,
		ResponseProfileID:       topologyProfileID,
		ResponseProfileRevision: topologyProfile.Revision,
		Enabled:                 true,
	}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	require.NotNil(t, createdRule)
	assert.Equal(t, topologyProfile.Revision, createdRule.ResponseProfileRevision)

	cleanProfileID := seedResponseProfile(t, database, orgID, "clean-profile")
	ruleID := seedAutomationRule(t, database, orgID, sourceID, cleanProfileID, "clean-rule", false)
	updatedRule, err := store.UpdateAutomationRule(ctx, models.AutomationRule{
		AdminControlsGranted:    true,
		ID:                      ruleID,
		OrgID:                   orgID,
		RuleName:                "topology-update",
		MQTTSourceID:            sourceID,
		ResponseProfileID:       topologyProfileID,
		ResponseProfileRevision: topologyProfile.Revision,
	}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	require.NotNil(t, updatedRule)
	assert.Equal(t, topologyProfileID, updatedRule.ResponseProfileID)
	assert.Equal(t, topologyProfile.Revision, updatedRule.ResponseProfileRevision)

	topologyRuleID := seedAutomationRule(t, database, orgID, sourceID, topologyProfileID, "topology-enable", false)
	enabledRule, err := store.SetAutomationRuleEnabled(ctx, orgID, topologyRuleID, true, topologyProfile.Revision, models.ResponseProfileFanSettings{}, true)
	require.NoError(t, err)
	require.NotNil(t, enabledRule)
	assert.True(t, enabledRule.Enabled)
	assert.Equal(t, topologyProfile.Revision, enabledRule.ResponseProfileRevision)

	expectedTopologyScopeJSON = append([]byte(nil), topologyProfile.ScopeJSON...)
	topologyProfile.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"building_ids":[%d]}`, replacementBuilding.ID))
	_, err = store.UpdateResponseProfile(
		ctx,
		*topologyProfile,
		nil,
		nil,
		topologyProfile.SiteID,
		expectedTopologyScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "enabled automation rules")
	for _, boundRuleID := range []int64{createdRule.ID, enabledRule.ID} {
		_, err = store.SetAutomationRuleEnabled(ctx, orgID, boundRuleID, false, uuid.Nil, models.ResponseProfileFanSettings{}, true)
		require.NoError(t, err)
	}
	updatedProfile, err := store.UpdateResponseProfile(
		ctx,
		*topologyProfile,
		nil,
		nil,
		topologyProfile.SiteID,
		expectedTopologyScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)
	require.NotNil(t, updatedProfile)
	assert.JSONEq(t, string(topologyProfile.ScopeJSON), string(updatedProfile.ScopeJSON))
	assert.NotEqual(t, topologyProfile.Revision, updatedProfile.Revision)

	_, found, err := buildingStore.SoftDeleteBuilding(ctx, orgID, replacementBuilding.ID)
	require.NoError(t, err)
	require.True(t, found)
	_, err = store.CreateAutomationRule(ctx, models.AutomationRule{
		AdminControlsGranted:    true,
		OrgID:                   orgID,
		RuleName:                "deleted-topology-create",
		TriggerType:             models.AutomationTriggerTypeMQTT,
		MQTTSourceID:            sourceID,
		ResponseProfileID:       topologyProfileID,
		ResponseProfileRevision: updatedProfile.Revision,
		Enabled:                 true,
	}, models.ResponseProfileFanSettings{})
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}
