package sqlstores_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	infrastructuremodels "github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_FacilityFanClaimSnapshotAndRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	db := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(db)
	siteID, fanID := seedCurtailmentFacilityFan(t, db, user.OrganizationID, "facility-fan-claim")

	firstUUID := uuid.New()
	first := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, firstUUID, models.EventStateActive, "facility-fan-first")
	first.FacilityFanDeviceIDs = []int64{fanID}
	first.ExpectedFacilityFanSites = map[int64]int64{fanID: siteID}
	firstResult, err := store.InsertEventWithTargets(ctx, first, []models.InsertTargetParams{
		curtailmentStoreTestTarget("facility-fan-miner-a", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)

	loaded, err := store.GetEventByUUID(ctx, user.OrganizationID, firstUUID)
	require.NoError(t, err)
	assert.Equal(t, []int64{fanID}, loaded.FacilityFanDeviceIDs)
	assert.Equal(t, []int64{siteID}, loaded.FacilityFanSiteIDs)

	second := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateActive, "facility-fan-second")
	second.FacilityFanDeviceIDs = []int64{fanID}
	second.ExpectedFacilityFanSites = map[int64]int64{fanID: siteID}
	_, err = store.InsertEventWithTargets(ctx, second, []models.InsertTargetParams{
		curtailmentStoreTestTarget("facility-fan-miner-b", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "overlapping fan claim must fail, got %v", err)

	_, err = store.ForceReleaseEvent(ctx, user.OrganizationID, firstUUID, "release facility fan claim")
	require.NoError(t, err)
	recoveryEntered := make(chan struct{})
	completeRecovery := make(chan struct{})
	recoveryResult := make(chan error, 1)
	go func() {
		recoveryResult <- store.RecoverTerminalFanState(
			ctx,
			firstResult.ID,
			user.OrganizationID,
			[]int64{fanID},
			[]int64{siteID},
			interfaces.UpdateCurtailmentFanStateParams{ExpectedEventState: models.EventStateCancelled},
			func(context.Context) *string {
				close(recoveryEntered)
				<-completeRecovery
				return nil
			},
		)
	}()
	<-recoveryEntered
	mutationResult := make(chan error, 1)
	go func() {
		_, updateErr := db.ExecContext(ctx, `UPDATE infrastructure_device SET name = name || '-updated' WHERE id = $1`, fanID)
		mutationResult <- updateErr
	}()
	select {
	case mutationErr := <-mutationResult:
		require.Failf(t, "device mutation completed during terminal fan command", "error: %v", mutationErr)
	case <-time.After(100 * time.Millisecond):
	}
	close(completeRecovery)
	require.NoError(t, <-recoveryResult)
	require.NoError(t, <-mutationResult)

	_, err = store.InsertEventWithTargets(ctx, second, []models.InsertTargetParams{
		curtailmentStoreTestTarget("facility-fan-miner-b", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)

	commandCalled := false
	err = store.RecoverTerminalFanState(
		ctx,
		firstResult.ID,
		user.OrganizationID,
		[]int64{fanID},
		[]int64{siteID},
		interfaces.UpdateCurtailmentFanStateParams{ExpectedEventState: models.EventStateCancelled},
		func(context.Context) *string {
			commandCalled = true
			return nil
		},
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.False(t, commandCalled, "stale terminal recovery must not override the newer active fan claim")
}

func TestSQLCurtailmentStore_ForceReleaseKeepsFanClaimUntilRecoveryCompletes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	db := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(db)
	siteID, fanID := seedCurtailmentFacilityFan(t, db, user.OrganizationID, "facility-fan-force-release-claim")

	firstUUID := uuid.New()
	first := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, firstUUID, models.EventStateActive, "facility-fan-first-owner")
	first.FacilityFanDeviceIDs = []int64{fanID}
	first.ExpectedFacilityFanSites = map[int64]int64{fanID: siteID}
	inserted, err := store.InsertEventWithTargets(ctx, first, []models.InsertTargetParams{
		curtailmentStoreTestTarget("facility-fan-first-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)

	second := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateActive, "facility-fan-second-owner")
	second.FacilityFanDeviceIDs = []int64{fanID}
	second.ExpectedFacilityFanSites = map[int64]int64{fanID: siteID}
	secondTargets := []models.InsertTargetParams{
		curtailmentStoreTestTarget("facility-fan-second-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	}

	recoveryEntered := make(chan struct{})
	completeRecovery := make(chan struct{})
	releaseResult := make(chan error, 1)
	now := time.Now().UTC()
	go func() {
		_, releaseErr := store.ForceReleaseEventWithFanRecovery(
			ctx,
			user.OrganizationID,
			firstUUID,
			"release facility fan claim",
			inserted.ID,
			[]int64{fanID},
			[]int64{siteID},
			interfaces.UpdateCurtailmentFanStateParams{
				ExpectedEventState: models.EventStateCancelled,
				FanOnSentAt:        &now,
			},
			func(context.Context) *string {
				close(recoveryEntered)
				<-completeRecovery
				return nil
			},
		)
		releaseResult <- releaseErr
	}()
	<-recoveryEntered

	startResult := make(chan error, 1)
	go func() {
		_, startErr := store.InsertEventWithTargets(ctx, second, secondTargets)
		startResult <- startErr
	}()
	select {
	case startErr := <-startResult:
		require.Failf(t, "new fan owner started before force-release recovery completed", "error: %v", startErr)
	case <-time.After(100 * time.Millisecond):
	}

	close(completeRecovery)
	require.NoError(t, <-releaseResult)
	require.NoError(t, <-startResult)
}

func TestSQLCurtailmentStore_ForceReleaseSerializesWithFanCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	eventUUID := uuid.New()
	event := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "facility-fan-force-release")
	inserted, err := store.InsertEventWithTargets(ctx, event, []models.InsertTargetParams{
		curtailmentStoreTestTarget("facility-fan-force-release-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)

	commandEntered := make(chan struct{})
	completeCommand := make(chan struct{})
	commandResult := make(chan error, 1)
	go func() {
		_, commandErr := store.CommandFanState(
			ctx,
			inserted.ID,
			interfaces.UpdateCurtailmentFanStateParams{ExpectedEventState: models.EventStateActive},
			func(context.Context) *string {
				close(commandEntered)
				<-completeCommand
				return nil
			},
		)
		commandResult <- commandErr
	}()
	<-commandEntered

	releaseStarted := make(chan struct{})
	releaseResult := make(chan error, 1)
	go func() {
		close(releaseStarted)
		_, releaseErr := store.ForceReleaseEvent(ctx, user.OrganizationID, eventUUID, "operator release")
		releaseResult <- releaseErr
	}()
	<-releaseStarted
	select {
	case releaseErr := <-releaseResult:
		require.Failf(t, "Force Release completed before the fan command", "error: %v", releaseErr)
	case <-time.After(100 * time.Millisecond):
	}

	close(completeCommand)
	require.NoError(t, <-commandResult)
	require.NoError(t, <-releaseResult)

	staleCommandCalled := false
	_, err = store.CommandFanState(
		ctx,
		inserted.ID,
		interfaces.UpdateCurtailmentFanStateParams{ExpectedEventState: models.EventStateActive},
		func(context.Context) *string {
			staleCommandCalled = true
			return nil
		},
	)
	require.ErrorIs(t, err, interfaces.ErrCurtailmentEventStateRaceLoss)
	assert.False(t, staleCommandCalled, "a stale fan command must lose its lifecycle guard before touching hardware")
}

func TestSQLCurtailmentStore_AirflowReopenPreservesFirstFailureThenStampsSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	eventUUID := uuid.New()
	event := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "facility-fan-reopen-timing")
	inserted, err := store.InsertEventWithTargets(ctx, event, []models.InsertTargetParams{
		curtailmentStoreTestTarget("facility-fan-reopen-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)

	firstAttemptAt := time.Now().UTC().Add(-time.Minute)
	firstError := "fan ON failed"
	_, err = store.CommandFanState(
		ctx,
		inserted.ID,
		interfaces.UpdateCurtailmentFanStateParams{
			ExpectedEventState:            models.EventStateActive,
			FanAirflowReopenedAt:          &firstAttemptAt,
			FanAirflowReopenedAtOnSuccess: &firstAttemptAt,
		},
		func(context.Context) *string { return &firstError },
	)
	require.NoError(t, err)
	failed, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	assert.Equal(t, &firstAttemptAt, failed.FanAirflowReopenedAt)
	assert.Equal(t, &firstError, failed.FanLastError)

	successAt := time.Now().UTC()
	_, err = store.CommandFanState(
		ctx,
		inserted.ID,
		interfaces.UpdateCurtailmentFanStateParams{
			ExpectedEventState:            models.EventStateActive,
			FanAirflowReopenedAtOnSuccess: &successAt,
		},
		func(context.Context) *string { return nil },
	)
	require.NoError(t, err)
	recovered, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	assert.Equal(t, &successAt, recovered.FanAirflowReopenedAt)
	assert.Nil(t, recovered.FanLastError)
}

func TestSQLCurtailmentStore_FacilityFanAuthorizationSnapshotRejectsSiteDrift(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	db := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(db)
	siteID, fanID := seedCurtailmentFacilityFan(t, db, user.OrganizationID, "facility-fan-drift")
	otherSite, err := sqlstores.NewSQLSiteStore(db).CreateSite(ctx, sitesmodels.CreateSiteParams{
		OrgID: user.OrganizationID,
		Name:  "facility-fan-drift-other-site",
	})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE infrastructure_device SET site_id = $1 WHERE id = $2`, otherSite.ID, fanID)
	require.NoError(t, err)

	event := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateActive, "facility-fan-drift")
	event.FacilityFanDeviceIDs = []int64{fanID}
	event.ExpectedFacilityFanSites = map[int64]int64{fanID: siteID}
	_, err = store.InsertEventWithTargets(ctx, event, []models.InsertTargetParams{
		curtailmentStoreTestTarget("facility-fan-drift-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "site drift must invalidate authorization, got %v", err)
}

func TestSQLCurtailmentStore_ConcurrentIdempotentFacilityFanStartsHaveOneWinner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	db := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(db)
	siteID, fanID := seedCurtailmentFacilityFan(t, db, user.OrganizationID, "facility-fan-idempotent")
	idempotencyKey := "facility-fan-concurrent-start"

	start := func(eventUUID uuid.UUID) error {
		event := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "facility-fan-idempotent")
		event.FacilityFanDeviceIDs = []int64{fanID}
		event.ExpectedFacilityFanSites = map[int64]int64{fanID: siteID}
		event.IdempotencyKey = &idempotencyKey
		_, err := store.InsertEventWithTargets(ctx, event, []models.InsertTargetParams{
			curtailmentStoreTestTarget("facility-fan-idempotent-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
		})
		return err
	}

	errs := make([]error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	begin := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	for index := range errs {
		go func() {
			defer workers.Done()
			ready.Done()
			<-begin
			errs[index] = start(uuid.New())
		}()
	}
	ready.Wait()
	close(begin)
	workers.Wait()

	successes := 0
	replays := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, interfaces.ErrCurtailmentReplayRaceLoss):
			replays++
		default:
			t.Errorf("unexpected concurrent Start error: %v", err)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, replays)
}

func seedCurtailmentFacilityFan(t *testing.T, db *sql.DB, orgID int64, name string) (int64, int64) {
	t.Helper()
	ctx := t.Context()
	site, err := sqlstores.NewSQLSiteStore(db).CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: name + "-site"})
	require.NoError(t, err)
	device, err := sqlstores.NewSQLInfrastructureDeviceStore(db).CreateInfrastructureDevice(ctx, infrastructuremodels.CreateParams{
		OrgID:        orgID,
		SiteID:       site.ID,
		BuildingName: "Fan building",
		Name:         name,
		DeviceKind:   infrastructuremodels.KindFanGroup,
		FanCount:     4,
		Enabled:      true,
		DriverType:   "modbus_tcp",
		DriverConfig: []byte(`{"endpoint":"127.0.0.1"}`),
	})
	require.NoError(t, err)
	return site.ID, device.ID
}
