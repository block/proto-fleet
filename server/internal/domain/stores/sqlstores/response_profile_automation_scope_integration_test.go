package sqlstores_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	buildingsmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_TopologyProfilesCannotBeAutomated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	orgID := user.OrganizationID
	site, err := sqlstores.NewSQLSiteStore(database).CreateSite(ctx, sitesmodels.CreateSiteParams{
		OrgID: orgID,
		Name:  "Topology profile site",
	})
	require.NoError(t, err)
	building, err := sqlstores.NewSQLBuildingStore(database).CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID:  orgID,
		SiteID: &site.ID,
		Name:   "Topology profile building",
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

	_, err = store.CreateAutomationRule(ctx, models.AutomationRule{
		OrgID:                   orgID,
		RuleName:                "topology-create",
		TriggerType:             models.AutomationTriggerTypeMQTT,
		MQTTSourceID:            sourceID,
		ResponseProfileID:       topologyProfileID,
		ResponseProfileRevision: topologyProfile.Revision,
		Enabled:                 true,
	}, models.ResponseProfileFanSettings{})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))

	cleanProfileID := seedResponseProfile(t, database, orgID, "clean-profile")
	ruleID := seedAutomationRule(t, database, orgID, sourceID, cleanProfileID, "clean-rule", false)
	_, err = store.UpdateAutomationRule(ctx, models.AutomationRule{
		ID:                      ruleID,
		OrgID:                   orgID,
		RuleName:                "topology-update",
		MQTTSourceID:            sourceID,
		ResponseProfileID:       topologyProfileID,
		ResponseProfileRevision: topologyProfile.Revision,
	}, models.ResponseProfileFanSettings{})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))

	topologyRuleID := seedAutomationRule(t, database, orgID, sourceID, topologyProfileID, "topology-enable", false)
	_, err = store.SetAutomationRuleEnabled(ctx, orgID, topologyRuleID, true, topologyProfile.Revision, models.ResponseProfileFanSettings{})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))

	cleanProfile, err := store.GetResponseProfile(ctx, orgID, cleanProfileID)
	require.NoError(t, err)
	expectedCleanScopeJSON := append([]byte(nil), cleanProfile.ScopeJSON...)
	cleanProfile.ScopeJSON = []byte(`{"scope_schema_version":1,"rack_ids":[8]}`)
	_, err = store.UpdateResponseProfile(
		ctx,
		*cleanProfile,
		nil,
		nil,
		cleanProfile.SiteID,
		expectedCleanScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))

	unchanged, err := store.GetResponseProfile(ctx, orgID, cleanProfileID)
	require.NoError(t, err)
	assert.JSONEq(t, string(expectedCleanScopeJSON), string(unchanged.ScopeJSON))
}
