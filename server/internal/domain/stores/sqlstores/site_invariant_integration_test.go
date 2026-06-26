package sqlstores_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteSite_ClearsAllDeviceSitePointers asserts the invariant the
// miner-list query JOIN relies on: after DeleteSite soft-deletes a site,
// no live device.site_id points at it. The fleetmanagement snapshot
// builder LEFT JOINs site with site.deleted_at IS NULL; without this
// invariant a device could surface with SiteId populated but SiteLabel
// empty (because the join misses the soft-deleted site row). PR B's
// DeleteSite step 4 (UnassignDevicesFromSite) is what makes the
// invariant hold; this test catches any regression in that contract.
func TestDeleteSite_ClearsAllDeviceSitePointers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 3, "https://172.17.0.1:80")

	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)
	transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)

	// Create a site and reassign 2 of the 3 devices to it.
	site, err := siteStore.CreateSite(t.Context(), sitesmodels.CreateSiteParams{
		OrgID: orgID,
		Name:  "Austin",
	})
	require.NoError(t, err)

	_, err = siteStore.AssignDevicesToSite(t.Context(), orgID, &site.ID, deviceIDs[:2])
	require.NoError(t, err)

	// Sanity: the two reassigned devices now point at the site.
	row := testContext.ServiceProvider.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM device WHERE org_id = $1 AND site_id = $2`, orgID, site.ID)
	var beforeCount int
	require.NoError(t, row.Scan(&beforeCount))
	assert.Equal(t, 2, beforeCount, "test setup: 2 devices should point at the site")

	// Execute DeleteSite via the same tx wrapper production uses.
	err = transactor.RunInTx(t.Context(), func(txCtx context.Context) error {
		if err := siteStore.LockSiteForWrite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if err := siteStore.LockBuildingsBySiteForWrite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.UnassignRacksFromBuildingsBySite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.SoftDeleteBuildingsBySite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.UnassignRacksFromSite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.UnassignDevicesFromSite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.SoftDeleteSite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	// Invariant: no live device.site_id may reference a soft-deleted site.
	row = testContext.ServiceProvider.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM device d
		 WHERE d.org_id = $1
		   AND d.site_id IS NOT NULL
		   AND NOT EXISTS (
		     SELECT 1 FROM site s
		     WHERE s.id = d.site_id
		       AND s.org_id = d.org_id
		       AND s.deleted_at IS NULL
		   )`, orgID)
	var stalePointers int
	require.NoError(t, row.Scan(&stalePointers))
	assert.Equal(t, 0, stalePointers,
		"no live device.site_id should reference a soft-deleted site after DeleteSite")

	// And the formerly-assigned devices land in the Unassigned bucket.
	row = testContext.ServiceProvider.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM device WHERE org_id = $1 AND site_id IS NULL AND deleted_at IS NULL`, orgID)
	var unassignedCount int
	require.NoError(t, row.Scan(&unassignedCount))
	assert.GreaterOrEqual(t, unassignedCount, 3, "all 3 devices should now have site_id IS NULL")
}

func TestDeleteCurtailmentResponseProfilesBySite_RemovesScopedProfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()
	db := testContext.ServiceProvider.DB
	siteStore := sqlstores.NewSQLSiteStore(db)

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Calgary"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Austin"})
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO curtailment_response_profile (
			org_id,
			profile_name,
			site_id,
			scope_json,
			mode,
			target_kw
		) VALUES
			($1, 'Legacy site', $2, jsonb_build_object('site_ids', jsonb_build_array($2::bigint)), 'FIXED_KW', 100),
			($1, 'Scoped site list', NULL, jsonb_build_object('site_ids', jsonb_build_array($2::bigint, $3::bigint)), 'FIXED_KW', 100),
			($1, 'Other site', NULL, jsonb_build_object('site_ids', jsonb_build_array($3::bigint)), 'FIXED_KW', 100)
	`, orgID, siteA.ID, siteB.ID)
	require.NoError(t, err)

	deleted, err := siteStore.DeleteCurtailmentResponseProfilesBySite(ctx, orgID, siteA.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	rows, err := db.QueryContext(ctx, `
		SELECT profile_name
		FROM curtailment_response_profile
		WHERE org_id = $1
		ORDER BY profile_name
	`, orgID)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"Other site"}, names)
}

func TestDeleteCurtailmentResponseProfilesBySite_BlocksAutomationBoundScopedProfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()
	db := testContext.ServiceProvider.DB
	siteStore := sqlstores.NewSQLSiteStore(db)

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Toronto"})
	require.NoError(t, err)

	var profileID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO curtailment_response_profile (
			org_id,
			profile_name,
			site_id,
			scope_json,
			mode,
			target_kw
		) VALUES (
			$1,
			'Automation profile',
			NULL,
			jsonb_build_object('site_ids', jsonb_build_array($2::bigint)),
			'FIXED_KW',
			100
		)
		RETURNING id
	`, orgID, site.ID).Scan(&profileID)
	require.NoError(t, err)

	var sourceID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO curtailment_mqtt_source_config (
			organization_id,
			service_user_id,
			source_name,
			topic,
			broker_primary_host,
			broker_secondary_host,
			broker_transport,
			mqtt_username,
			mqtt_password_enc,
			payload_format
		) VALUES (
			$1,
			$2,
			'Automation source',
			'maestro/target',
			'broker-primary.local',
			'broker-secondary.local',
			'tcp',
			'mqtt-user',
			'encrypted-password',
			'target_timestamp'
		)
		RETURNING id
	`, orgID, user.DatabaseID).Scan(&sourceID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO curtailment_automation_rule (
			org_id,
			rule_name,
			trigger_type,
			mqtt_source_id,
			response_profile_id,
			enabled
		) VALUES (
			$1,
			'Automation rule',
			'MQTT',
			$2,
			$3,
			TRUE
		)
	`, orgID, sourceID, profileID)
	require.NoError(t, err)

	deleted, err := siteStore.DeleteCurtailmentResponseProfilesBySite(ctx, orgID, site.ID)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition, got %v", err)
	assert.Equal(t, int64(0), deleted)

	row := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM curtailment_response_profile
		WHERE org_id = $1
		  AND id = $2
	`, orgID, profileID)
	var profileCount int
	require.NoError(t, row.Scan(&profileCount))
	assert.Equal(t, 1, profileCount)

	row = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM curtailment_automation_rule
		WHERE org_id = $1
		  AND response_profile_id = $2
	`, orgID, profileID)
	var ruleCount int
	require.NoError(t, row.Scan(&ruleCount))
	assert.Equal(t, 1, ruleCount)
}
