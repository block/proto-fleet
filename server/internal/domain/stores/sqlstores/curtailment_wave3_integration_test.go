package sqlstores_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_FanCommandsDoNotReplayAfterCommitFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	for _, operation := range []string{"command", "admin terminate", "force release", "terminal recovery"} {
		t.Run(operation, func(t *testing.T) {
			fixture := testutil.InitializeDBServiceInfrastructure(t)
			user := fixture.DatabaseService.CreateSuperAdminUser()
			database := fixture.DatabaseService.DB
			ctx := t.Context()
			store := sqlstores.NewSQLCurtailmentStore(database)
			siteID, fanID := seedCurtailmentFacilityFan(t, database, user.OrganizationID, "no-replay")
			eventUUID := uuid.New()
			state := models.EventStateActive
			if operation == "admin terminate" {
				state = models.EventStatePending
			}
			event := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, state, "no-replay")
			event.FacilityFanDeviceIDs = []int64{fanID}
			event.ExpectedFacilityFanSites = map[int64]int64{fanID: siteID}
			inserted, err := store.InsertEventWithTargets(ctx, event, []models.InsertTargetParams{
				curtailmentStoreTestTarget("no-replay-miner", models.TargetStateUnavailable, models.DesiredStateCurtailed),
			})
			require.NoError(t, err)
			if operation == "terminal recovery" {
				_, err = store.ForceReleaseEvent(ctx, user.OrganizationID, eventUUID, "prepare terminal recovery")
				require.NoError(t, err)
				state = models.EventStateCancelled
			}
			// Fail only at commit, after the physical command has returned.
			// The old retrying transaction replays the command for SQLSTATE 40001.
			_, err = database.ExecContext(ctx, `
                CREATE FUNCTION reject_fan_commit() RETURNS trigger LANGUAGE plpgsql AS $$
                BEGIN RAISE EXCEPTION 'simulated serialization failure at commit' USING ERRCODE = '40001'; END
                $$;
                CREATE CONSTRAINT TRIGGER reject_fan_commit AFTER UPDATE ON curtailment_event
                DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_fan_commit();
            `)
			require.NoError(t, err)
			calls := 0
			command := func(context.Context) *string { calls++; return nil }
			now := time.Now().UTC()
			params := interfaces.UpdateCurtailmentFanStateParams{ExpectedEventState: state, FanOnSentAt: &now}
			switch operation {
			case "command":
				_, err = store.CommandFanState(ctx, inserted.ID, params, command)
			case "admin terminate":
				_, _, err = store.AdminTerminateEventWithFanRecovery(ctx, user.OrganizationID, eventUUID, models.EventStateCancelled, "stop", func(ctx context.Context, _ *models.Event) *string { return command(ctx) })
			case "force release":
				params.ExpectedEventState = models.EventStateCancelled
				_, err = store.ForceReleaseEventWithFanRecovery(ctx, user.OrganizationID, eventUUID, "release", inserted.ID, []int64{fanID}, []int64{siteID}, params, command)
			case "terminal recovery":
				err = store.RecoverTerminalFanState(ctx, inserted.ID, user.OrganizationID, []int64{fanID}, []int64{siteID}, params, command)
			}
			require.Error(t, err)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Equal(t, "40001", pgErr.Code)
			assert.Equal(t, 1, calls, "physical commands must not be replayed after commit failure")
			persisted, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
			require.NoError(t, err)
			assert.Equal(t, state, persisted.State, "failed transaction must roll back lifecycle changes")
			assert.Nil(t, persisted.FanOnSentAt)
		})
	}
}

func TestSQLCurtailmentStore_UpdateDurationPolicyRechecksCurrentRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	database := fixture.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(database)
	eventUUID := uuid.New()
	event := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "duration-policy")
	capSeconds := int32(600)
	event.MaxDurationSeconds = &capSeconds
	event.MinCurtailedDurationSec = 60
	inserted, err := store.InsertEventWithTargets(ctx, event, []models.InsertTargetParams{
		curtailmentStoreTestTarget("duration-policy-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	extension := int32(1200)
	_, err = store.UpdateOperatorFields(ctx, inserted.ID, user.OrganizationID, interfaces.UpdateOperatorFieldsParams{MaxDurationSeconds: &extension})
	require.NoError(t, err)
	// A caller who read 600 seconds would consider 900 an extension, but a
	// concurrent update has already made it an early-restore reduction.
	staleExtension := int32(900)
	_, err = store.UpdateOperatorFields(ctx, inserted.ID, user.OrganizationID, interfaces.UpdateOperatorFieldsParams{MaxDurationSeconds: &staleExtension})
	require.ErrorIs(t, err, interfaces.ErrCurtailmentEventStateRaceLoss)
	_, err = store.UpdateOperatorFields(ctx, inserted.ID, user.OrganizationID, interfaces.UpdateOperatorFieldsParams{MaxDurationSeconds: &staleExtension, CanShortenMaxDuration: true})
	require.NoError(t, err)
	belowMinimum := int32(30)
	_, err = store.UpdateOperatorFields(ctx, inserted.ID, user.OrganizationID, interfaces.UpdateOperatorFieldsParams{MaxDurationSeconds: &belowMinimum, CanShortenMaxDuration: true})
	require.ErrorIs(t, err, interfaces.ErrCurtailmentEventStateRaceLoss)
	_, err = database.ExecContext(ctx, `UPDATE curtailment_event SET allow_unbounded = TRUE, max_duration_seconds = NULL WHERE id = $1`, inserted.ID)
	require.NoError(t, err)
	_, err = store.UpdateOperatorFields(ctx, inserted.ID, user.OrganizationID, interfaces.UpdateOperatorFieldsParams{MaxDurationSeconds: &extension, CanShortenMaxDuration: true})
	require.ErrorIs(t, err, interfaces.ErrCurtailmentEventStateRaceLoss)
	reason := "reason-only edit remains valid"
	updated, err := store.UpdateOperatorFields(ctx, inserted.ID, user.OrganizationID, interfaces.UpdateOperatorFieldsParams{Reason: &reason})
	require.NoError(t, err)
	assert.Equal(t, reason, updated.Reason)
}

func TestSQLCurtailmentStore_AllPairedAdmissionPreservesTypedErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	other := fixture.DatabaseService.CreateSuperAdminUser2()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	eventUUID := uuid.New()
	inserted, err := store.InsertEventWithTargets(ctx,
		curtailmentStoreAllPairedEvent(user.OrganizationID, user.DatabaseID, eventUUID, "typed-admission"),
		[]models.InsertTargetParams{curtailmentStoreAllPairedTarget("typed-miner", models.TargetStateRestoreFailed, "failed")})
	require.NoError(t, err)
	_, err = store.ClaimAllPairedPolicyTargets(ctx, inserted.ID, other.OrganizationID, 1,
		[]models.InsertTargetParams{curtailmentStoreAllPairedTarget("typed-miner", models.TargetStatePending, "")})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "org mismatch must preserve InvalidArgument: %v", err)
	_, err = store.BulkRefreshAllPairedTargetReadiness(ctx, inserted.ID, other.OrganizationID, models.EventStateActive,
		[]interfaces.AllPairedReadinessUpdate{{DeviceIdentifier: "typed-miner", ExpectedState: models.TargetStateRestoreFailed, State: models.TargetStatePending}})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "org mismatch must preserve InvalidArgument: %v", err)
}

func TestSQLCurtailmentStore_ReleasePathsStampReleasedAt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	for _, operation := range []string{"force release", "undispatched restore", "policy release"} {
		t.Run(operation, func(t *testing.T) {
			fixture := testutil.InitializeDBServiceInfrastructure(t)
			user := fixture.DatabaseService.CreateSuperAdminUser()
			ctx := t.Context()
			store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
			eventUUID := uuid.New()
			event, err := store.InsertEventWithTargets(ctx,
				curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStatePending, "release-stamp"),
				[]models.InsertTargetParams{curtailmentStoreTestTarget("release-stamp-miner", models.TargetStatePending, models.DesiredStateCurtailed)})
			require.NoError(t, err)
			release := func() error {
				switch operation {
				case "force release":
					_, err := store.ForceReleaseEvent(ctx, user.OrganizationID, eventUUID, "release stamp")
					return err
				case "undispatched restore":
					_, err := store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
					return err
				default:
					return store.UpdateTargetState(ctx, event.ID, "release-stamp-miner", interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateReleased})
				}
			}
			require.NoError(t, release())
			targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
			require.NoError(t, err)
			require.Len(t, targets, 1)
			assert.Equal(t, models.TargetStateReleased, targets[0].State)
			require.NotNil(t, targets[0].ReleasedAt)
			firstRelease := *targets[0].ReleasedAt
			require.NoError(t, release())
			targets, err = store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
			require.NoError(t, err)
			require.NotNil(t, targets[0].ReleasedAt)
			assert.True(t, firstRelease.Equal(*targets[0].ReleasedAt), "idempotent releases preserve the original release timestamp")
		})
	}
}

func TestSQLCurtailmentStore_TopologyRestorePreservesFailedPrecondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	eventUUID := uuid.New()
	_, err := store.InsertEventWithTargets(ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "topology-restore-error"),
		[]models.InsertTargetParams{curtailmentStoreTestTarget("topology-error-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed)})
	require.NoError(t, err)
	event, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	// Simulates a stale topology selection against an event whose persisted
	// scope no longer permits the requested topology restoration.
	_, err = store.BeginCurtailmentTopologyTargetRestore(ctx, event, []string{"topology-error-miner"})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "persisted scope guard must retain its error code: %v", err)
}
