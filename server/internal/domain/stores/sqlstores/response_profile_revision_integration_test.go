package sqlstores_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func mustResponseProfileRevision(t *testing.T, store *sqlstores.SQLCurtailmentStore, orgID, profileID int64) uuid.UUID {
	t.Helper()
	profile, err := store.GetResponseProfile(t.Context(), orgID, profileID)
	require.NoError(t, err)
	return profile.Revision
}

func TestSQLCurtailmentStore_ResponseProfileRevisionFencesExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	ctx := t.Context()
	profileID := seedResponseProfile(t, testContext.DatabaseService.DB, user.OrganizationID, "execution-revision-profile")

	original, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	updatedInput := *original
	updatedInput.ProfileName = "execution-revision-profile-updated"
	updatedInput.RestoreBatchSize++
	updated, err := store.UpdateResponseProfile(
		ctx,
		updatedInput,
		nil,
		nil,
		original.SiteID,
		original.ScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)

	staleExecution := curtailmentStoreClosedLoopFullFleetEvent(
		user.OrganizationID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeWholeOrg,
		0,
		"response-profile-stale-execution",
	)
	staleExecution.ResponseProfileID = profileID
	staleExecution.ResponseProfileRevision = original.Revision
	_, err = store.InsertEventWithTargets(ctx, staleExecution, nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))

	currentExecution := staleExecution
	currentExecution.EventUUID = uuid.New()
	currentExecution.ResponseProfileRevision = updated.Revision
	_, err = store.InsertEventWithTargets(ctx, currentExecution, nil)
	require.NoError(t, err)
}

func TestSQLCurtailmentStore_ResponseProfileRevisionIgnoresMetadataOnlyUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	ctx := t.Context()
	profileID := seedResponseProfile(t, testContext.DatabaseService.DB, user.OrganizationID, "metadata-revision-profile")

	original, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	updatedInput := *original
	updatedInput.ProfileName = "metadata-revision-profile-renamed"
	updated, err := store.UpdateResponseProfile(
		ctx,
		updatedInput,
		nil,
		nil,
		original.SiteID,
		original.ScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)
	assert.Equal(t, original.Revision, updated.Revision)
	assert.Equal(t, updatedInput.ProfileName, updated.ProfileName)
}

func TestSQLCurtailmentStore_ResponseProfileRevisionRejectsStaleUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	ctx := t.Context()
	profileID := seedResponseProfile(t, testContext.DatabaseService.DB, user.OrganizationID, "revisioned-profile")

	original, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	require.NotZero(t, original.Revision)

	firstUpdate := *original
	firstUpdate.ProfileName = "first-revision-update"
	firstUpdate.RestoreBatchSize++
	updated, err := store.UpdateResponseProfile(
		ctx,
		firstUpdate,
		nil,
		nil,
		original.SiteID,
		original.ScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)
	assert.NotEqual(t, original.Revision, updated.Revision)

	staleUpdate := *original
	staleUpdate.ProfileName = "stale-revision-update"
	_, err = store.UpdateResponseProfile(
		ctx,
		staleUpdate,
		nil,
		nil,
		original.SiteID,
		original.ScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))

	current, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	assert.Equal(t, updated.Revision, current.Revision)
	assert.Equal(t, "first-revision-update", current.ProfileName)
}

func TestSQLCurtailmentStore_AutomationRuleRetainsAndRefreshesBoundProfileRevision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	ctx := t.Context()
	profileID := seedResponseProfile(t, testContext.DatabaseService.DB, user.OrganizationID, "automation-revision-profile")
	profile, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	sourceID := seedMQTTSourceConfig(
		t,
		testContext.DatabaseService.DB,
		user.OrganizationID,
		user.DatabaseID,
		"automation-revision-source",
		true,
	)

	rule, err := store.CreateAutomationRule(ctx, models.AutomationRule{
		OrgID:                   user.OrganizationID,
		RuleName:                "automation-revision-rule",
		TriggerType:             models.AutomationTriggerTypeMQTT,
		MQTTSourceID:            sourceID,
		ResponseProfileID:       profileID,
		ResponseProfileRevision: profile.Revision,
		Enabled:                 true,
	}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	assert.Equal(t, profile.Revision, rule.ResponseProfileRevision)

	updatedInput := *profile
	updatedInput.ProfileName = "automation-revision-profile-updated"
	updatedInput.RestoreBatchSize++
	updated, err := store.UpdateResponseProfile(
		ctx,
		updatedInput,
		nil,
		nil,
		profile.SiteID,
		profile.ScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)
	assert.NotEqual(t, profile.Revision, updated.Revision)

	boundRule, err := store.GetAutomationRule(ctx, user.OrganizationID, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, profile.Revision, boundRule.ResponseProfileRevision)

	_, err = store.SetAutomationRuleEnabled(
		ctx,
		user.OrganizationID,
		rule.ID,
		false,
		uuid.Nil,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)
	reboundRule, err := store.SetAutomationRuleEnabled(
		ctx,
		user.OrganizationID,
		rule.ID,
		true,
		updated.Revision,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)
	assert.Equal(t, updated.Revision, reboundRule.ResponseProfileRevision)
}

func TestSQLCurtailmentStore_StaleAutomationEnableCannotMixProfileBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	ctx := t.Context()
	profileAID := seedResponseProfile(t, database, user.OrganizationID, "stale-enable-profile-a")
	profileBID := seedResponseProfile(t, database, user.OrganizationID, "stale-enable-profile-b")
	profileA, err := store.GetResponseProfile(ctx, user.OrganizationID, profileAID)
	require.NoError(t, err)
	profileB, err := store.GetResponseProfile(ctx, user.OrganizationID, profileBID)
	require.NoError(t, err)
	sourceID := seedMQTTSourceConfig(
		t,
		database,
		user.OrganizationID,
		user.DatabaseID,
		"stale-enable-source",
		true,
	)

	rule, err := store.CreateAutomationRule(ctx, models.AutomationRule{
		OrgID:                   user.OrganizationID,
		RuleName:                "stale-enable-rule",
		TriggerType:             models.AutomationTriggerTypeMQTT,
		MQTTSourceID:            sourceID,
		ResponseProfileID:       profileA.ID,
		ResponseProfileRevision: profileA.Revision,
		Enabled:                 true,
	}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)

	rule.ResponseProfileID = profileB.ID
	rule.ResponseProfileRevision = profileB.Revision
	_, err = store.UpdateAutomationRule(ctx, *rule, models.ResponseProfileFanSettings{})
	require.NoError(t, err)

	_, err = sqlc.New(database).SetCurtailmentAutomationRuleEnabled(
		ctx,
		sqlc.SetCurtailmentAutomationRuleEnabledParams{
			ID:                        rule.ID,
			OrgID:                     user.OrganizationID,
			Enabled:                   true,
			ResponseProfileRevision:   profileA.Revision,
			ExpectedResponseProfileID: profileA.ID,
		},
	)
	require.ErrorIs(t, err, sql.ErrNoRows)

	current, err := store.GetAutomationRule(ctx, user.OrganizationID, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, profileB.ID, current.ResponseProfileID)
	assert.Equal(t, profileB.Revision, current.ResponseProfileRevision)
}

func TestSQLCurtailmentStore_AutomationExecutionRejectsReboundRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	ctx := t.Context()
	profileAID := seedResponseProfile(t, database, user.OrganizationID, "execution-fence-profile-a")
	profileBID := seedResponseProfile(t, database, user.OrganizationID, "execution-fence-profile-b")
	profileA, err := store.GetResponseProfile(ctx, user.OrganizationID, profileAID)
	require.NoError(t, err)
	profileB, err := store.GetResponseProfile(ctx, user.OrganizationID, profileBID)
	require.NoError(t, err)
	sourceID := seedMQTTSourceConfig(
		t,
		database,
		user.OrganizationID,
		user.DatabaseID,
		"execution-fence-source",
		true,
	)

	rule, err := store.CreateAutomationRule(ctx, models.AutomationRule{
		OrgID:                   user.OrganizationID,
		RuleName:                "execution-fence-rule",
		TriggerType:             models.AutomationTriggerTypeMQTT,
		MQTTSourceID:            sourceID,
		ResponseProfileID:       profileA.ID,
		ResponseProfileRevision: profileA.Revision,
		Enabled:                 true,
	}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)

	rule.ResponseProfileID = profileB.ID
	rule.ResponseProfileRevision = profileB.Revision
	_, err = store.UpdateAutomationRule(ctx, *rule, models.ResponseProfileFanSettings{})
	require.NoError(t, err)

	staleExecution := curtailmentStoreClosedLoopFullFleetEvent(
		user.OrganizationID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeWholeOrg,
		0,
		"stale-automation-execution",
	)
	staleExecution.SourceActorType = models.SourceActorAutomation
	staleExecution.ResponseProfileID = profileA.ID
	staleExecution.ResponseProfileRevision = profileA.Revision
	staleExecution.AutomationRuleID = rule.ID
	staleExecution.AutomationMQTTSourceID = sourceID
	_, err = store.InsertEventWithTargets(ctx, staleExecution, nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))

	currentExecution := staleExecution
	currentExecution.EventUUID = uuid.New()
	currentExecution.ResponseProfileID = profileB.ID
	currentExecution.ResponseProfileRevision = profileB.Revision
	_, err = store.InsertEventWithTargets(ctx, currentExecution, nil)
	require.NoError(t, err)
}

func TestSQLCurtailmentStore_AutomationExecutionSerializesConcurrentRebind(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	profileAID := seedResponseProfile(t, database, user.OrganizationID, "concurrent-execution-profile-a")
	profileBID := seedResponseProfile(t, database, user.OrganizationID, "concurrent-execution-profile-b")
	profileA, err := store.GetResponseProfile(ctx, user.OrganizationID, profileAID)
	require.NoError(t, err)
	profileB, err := store.GetResponseProfile(ctx, user.OrganizationID, profileBID)
	require.NoError(t, err)
	sourceID := seedMQTTSourceConfig(
		t,
		database,
		user.OrganizationID,
		user.DatabaseID,
		"concurrent-execution-source",
		true,
	)
	rule, err := store.CreateAutomationRule(ctx, models.AutomationRule{
		OrgID:                   user.OrganizationID,
		RuleName:                "concurrent-execution-rule",
		TriggerType:             models.AutomationTriggerTypeMQTT,
		MQTTSourceID:            sourceID,
		ResponseProfileID:       profileA.ID,
		ResponseProfileRevision: profileA.Revision,
		Enabled:                 true,
	}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)

	blocker, err := database.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	_, err = blocker.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtextextended(
				'curtailment_automation_rule:' || $1::bigint::text || ':' || $2::bigint::text,
				0
			)
		)`, user.OrganizationID, rule.ID)
	require.NoError(t, err)

	var blockerPID int
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
	countRuleFenceWaiters := func() (int, error) {
		var waiters int
		err := database.QueryRowContext(ctx, `
			SELECT count(*)
			FROM pg_locks waiting
			JOIN pg_locks blocker
			  ON blocker.locktype = waiting.locktype
			 AND blocker.database IS NOT DISTINCT FROM waiting.database
			 AND blocker.classid IS NOT DISTINCT FROM waiting.classid
			 AND blocker.objid IS NOT DISTINCT FROM waiting.objid
			 AND blocker.objsubid IS NOT DISTINCT FROM waiting.objsubid
			WHERE blocker.pid = $1
			  AND blocker.locktype = 'advisory'
			  AND blocker.granted
			  AND NOT waiting.granted`, blockerPID).Scan(&waiters)
		if err != nil {
			return 0, fmt.Errorf("count automation rule fence waiters: %w", err)
		}
		return waiters, nil
	}

	ruleReference := fmt.Sprintf("%d", rule.ID)
	externalSource := "curtailment_automation"
	idempotencyKey := "curtailment_automation_rule:" + ruleReference
	execution := curtailmentStoreClosedLoopFullFleetEvent(
		user.OrganizationID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeWholeOrg,
		0,
		ruleReference,
	)
	execution.SourceActorType = models.SourceActorAutomation
	execution.ResponseProfileID = profileA.ID
	execution.ResponseProfileRevision = profileA.Revision
	execution.AutomationRuleID = rule.ID
	execution.AutomationMQTTSourceID = sourceID
	execution.ExternalSource = &externalSource
	execution.ExternalReference = &ruleReference
	execution.IdempotencyKey = &idempotencyKey
	executionResult := make(chan error, 1)
	go func() {
		_, executionErr := store.InsertEventWithTargets(ctx, execution, nil)
		executionResult <- executionErr
	}()
	require.Eventually(t, func() bool {
		waiters, queryErr := countRuleFenceWaiters()
		return queryErr == nil && waiters >= 1
	}, 3*time.Second, 10*time.Millisecond, "execution did not wait for the automation rule fence")

	reboundRule := *rule
	reboundRule.ResponseProfileID = profileB.ID
	reboundRule.ResponseProfileRevision = profileB.Revision
	rebindResult := make(chan error, 1)
	go func() {
		_, rebindErr := store.UpdateAutomationRule(ctx, reboundRule, models.ResponseProfileFanSettings{})
		rebindResult <- rebindErr
	}()
	require.Eventually(t, func() bool {
		waiters, queryErr := countRuleFenceWaiters()
		return queryErr == nil && waiters >= 2
	}, 3*time.Second, 10*time.Millisecond, "rule rebind did not wait behind the execution fence")

	require.NoError(t, blocker.Commit())
	require.NoError(t, <-executionResult)
	rebindErr := <-rebindResult
	require.Error(t, rebindErr)
	assert.True(t, fleeterror.IsFailedPreconditionError(rebindErr))

	current, err := store.GetAutomationRule(ctx, user.OrganizationID, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, profileA.ID, current.ResponseProfileID)
	assert.Equal(t, profileA.Revision, current.ResponseProfileRevision)

	_, err = store.SetAutomationRuleEnabled(
		ctx,
		user.OrganizationID,
		rule.ID,
		true,
		profileA.Revision,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)

	updatedProfile := *profileA
	updatedProfile.ProfileName = "concurrent-execution-profile-a-updated"
	updatedProfile.RestoreBatchSize++
	updatedProfileResult, err := store.UpdateResponseProfile(
		ctx,
		updatedProfile,
		nil,
		nil,
		profileA.SiteID,
		profileA.ScopeJSON,
		models.ResponseProfileFanSettings{},
	)
	require.NoError(t, err)
	_, err = store.SetAutomationRuleEnabled(
		ctx,
		user.OrganizationID,
		rule.ID,
		true,
		updatedProfileResult.Revision,
		models.ResponseProfileFanSettings{},
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))

	current, err = store.GetAutomationRule(ctx, user.OrganizationID, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, profileA.Revision, current.ResponseProfileRevision)
}

func TestSQLCurtailmentStore_ProfileUpdateAndFanExecutionUseConsistentLockOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fanSiteID, fanID := seedCurtailmentFacilityFan(t, database, user.OrganizationID, "profile-revision-lock-order")

	var profileID int64
	require.NoError(t, database.QueryRowContext(ctx, `
		INSERT INTO curtailment_response_profile
			(org_id, profile_name, mode, facility_fan_device_ids, authorization_envelope_jsonb)
		VALUES (
			$1, 'fan lock order profile', 'FULL_FLEET', ARRAY[$2]::bigint[],
			jsonb_build_object(
				'schema_version', 1,
				'selected_resource_site_ids', '[]'::jsonb,
				'current_member_site_ids', '[]'::jsonb,
				'miner_scope_unbounded', true,
				'facility_fan_site_ids', jsonb_build_array($3::bigint),
				'facility_fan_scope_unbounded', false
			)
		)
		RETURNING id`, user.OrganizationID, fanID, fanSiteID).Scan(&profileID))
	profile, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	devices, err := store.ListResponseProfileInfrastructureDevices(ctx, user.OrganizationID, []int64{fanID})
	require.NoError(t, err)

	profileBlocker, err := database.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	defer func() { _ = profileBlocker.Rollback() }()
	var lockedProfileID int64
	require.NoError(t, profileBlocker.QueryRowContext(
		ctx,
		`SELECT id FROM curtailment_response_profile WHERE id = $1 FOR SHARE`,
		profileID,
	).Scan(&lockedProfileID))

	updateInput := *profile
	updateInput.ProfileName = "fan lock order profile updated"
	updateInput.RestoreBatchSize++
	updateResult := make(chan error, 1)
	go func() {
		_, updateErr := store.UpdateResponseProfile(
			ctx,
			updateInput,
			nil,
			devices,
			profile.SiteID,
			profile.ScopeJSON,
			models.ResponseProfileFanSettings{FacilityFanDeviceIDs: []int64{fanID}},
		)
		updateResult <- updateErr
	}()

	require.Eventually(t, func() bool {
		probe, probeErr := database.BeginTx(ctx, &sql.TxOptions{})
		if probeErr != nil {
			return false
		}
		defer func() { _ = probe.Rollback() }()
		_, probeErr = probe.ExecContext(
			ctx,
			`SELECT id FROM infrastructure_device WHERE id = $1 FOR UPDATE NOWAIT`,
			fanID,
		)
		return probeErr != nil
	}, 3*time.Second, 10*time.Millisecond, "profile update did not acquire the fan row lock")

	execution := curtailmentStoreClosedLoopFullFleetEvent(
		user.OrganizationID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeWholeOrg,
		0,
		"response-profile-fan-lock-order",
	)
	execution.ResponseProfileID = profileID
	execution.ResponseProfileRevision = profile.Revision
	execution.FacilityFanDeviceIDs = []int64{fanID}
	execution.ExpectedFacilityFanSites = map[int64]int64{fanID: fanSiteID}
	executionResult := make(chan error, 1)
	go func() {
		_, executionErr := store.InsertEventWithTargets(ctx, execution, nil)
		executionResult <- executionErr
	}()
	require.Eventually(t, func() bool {
		probe, probeErr := database.BeginTx(ctx, &sql.TxOptions{})
		if probeErr != nil {
			return false
		}
		defer func() { _ = probe.Rollback() }()
		var acquired bool
		if probeErr := probe.QueryRowContext(
			ctx,
			`SELECT pg_try_advisory_xact_lock(hashtextextended('curtailment-fan:' || $1::text, 0))`,
			fanID,
		).Scan(&acquired); probeErr != nil {
			return false
		}
		return !acquired
	}, 3*time.Second, 10*time.Millisecond, "execution did not reach the fan claim lock")

	require.NoError(t, profileBlocker.Commit())
	require.NoError(t, <-updateResult)
	executionErr := <-executionResult
	require.Error(t, executionErr)
	assert.True(t, fleeterror.IsFailedPreconditionError(executionErr), "expected stale revision, got %v", executionErr)
}
