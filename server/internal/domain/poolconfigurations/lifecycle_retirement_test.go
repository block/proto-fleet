package poolconfigurations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeviceDeletionRetiresDefaultPoolTransitions(t *testing.T) {
	const applying = "applying"
	for _, state := range []string{"pending", "ready", applying} {
		t.Run(state, func(t *testing.T) {
			conn, scope, repo, target, config := defaultRetirementFixture(t)
			ctx := t.Context()
			q := sqlc.New(conn)
			id := seedDefaultTransition(t, conn, scope, target, config)
			if state != "pending" {
				_, err := q.PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: id, TargetKind: target.Kind, TargetID: target.ID, ReviewedBinding: "{}"})
				require.NoError(t, err)
			}
			if state == applying {
				lifecycleCtx := WithLifecycleTransition(ctx, id)
				operation, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
					return repo.Assign(lifecycleCtx, q, scope, 0, config.ID, config.RevisionID, []AssignmentChange{{Target: target, ReviewedBinding: "{}"}})
				})
				require.NoError(t, err)
				require.NoError(t, q.LinkPoolLifecycleOperation(ctx, sqlc.LinkPoolLifecycleOperationParams{TransitionID: id, OperationID: sql.NullString{String: operation.ID, Valid: true}}))
			}
			require.ErrorContains(t, archiveDefault(t, conn, repo, scope, config), "automatic pool transitions")
			identifier, err := q.GetDeviceIdentifierByID(ctx, target.DeviceID)
			require.NoError(t, err)
			deleted, err := sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(ctx, []string{identifier}, scope.OrganizationID)
			require.NoError(t, err)
			require.EqualValues(t, 1, deleted)
			row, err := q.GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			require.Equal(t, "superseded", row.State)
			require.Equal(t, config.ID, row.ConfigurationID.String, "keep captured policy as history")
			require.NoError(t, archiveDefault(t, conn, repo, scope, config))
			require.NoError(t, q.ReconcilePoolLifecycleOperations(ctx))
			after, err := q.GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			require.Equal(t, row, after, "reconciliation must not revive retired history")
		})
	}
}

func TestContainerDeletionReleasesPairingAndBoardDefaultReferences(t *testing.T) {
	conn, scope, repo, target, config := defaultRetirementFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	var account, board int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT $1,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$2 AND depth=0 RETURNING id`, uuid.NewString(), scope.OrganizationID).Scan(&account))
	clientScope := Scope{OrganizationID: scope.OrganizationID, AccountID: account}
	clientConfig, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, clientScope, RevisionOptions{Name: "Client default", Slots: []SlotOptions{{URL: "stratum+tcp://client.example", Username: "client"}}})
	})
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, target.DeviceID)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, scope.OrganizationID).Scan(&board))
	var pairing, child uuid.UUID
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id,pairing_defaults) VALUES($1,$2,0,$2,'pairing',gen_random_uuid(),jsonb_build_object('0',jsonb_build_object('configuration_id',$3::text,'revision_id',$4::text),$5::bigint::text,jsonb_build_object('configuration_id',$6::text,'revision_id',$7::text))) RETURNING transition_id`, scope.OrganizationID, target.DeviceID, config.ID, config.RevisionID, account, clientConfig.ID, clientConfig.RevisionID).Scan(&pairing))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,configuration_id,revision_id) VALUES($1,$2,7,$3,'ownership',$4,$5) RETURNING transition_id`, scope.OrganizationID, target.DeviceID, board, config.ID, config.RevisionID).Scan(&child))
	require.Error(t, archiveDefault(t, conn, repo, scope, config))
	require.Error(t, archiveDefault(t, conn, repo, clientScope, clientConfig))
	identifier, err := q.GetDeviceIdentifierByID(ctx, target.DeviceID)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(ctx, []string{identifier}, scope.OrganizationID)
	require.NoError(t, err)
	for _, id := range []uuid.UUID{pairing, child} {
		row, err := q.GetPoolLifecycleTransition(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "superseded", row.State)
	}
	require.NoError(t, archiveDefault(t, conn, repo, scope, config))
	require.NoError(t, archiveDefault(t, conn, repo, clientScope, clientConfig))
}

func TestDeletionFailureRollsBackDefaultTransitionRetirement(t *testing.T) {
	conn, scope, repo, target, config := defaultRetirementFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	id := seedDefaultTransition(t, conn, scope, target, config)
	_, err := conn.ExecContext(ctx, `CREATE FUNCTION reject_lifecycle_discovery_deletion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected discovery deletion failure'; END; $$;
 CREATE TRIGGER reject_lifecycle_discovery_deletion BEFORE UPDATE OF deleted_at ON discovered_device FOR EACH ROW EXECUTE FUNCTION reject_lifecycle_discovery_deletion();`)
	require.NoError(t, err)
	identifier, err := q.GetDeviceIdentifierByID(ctx, target.DeviceID)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(ctx, []string{identifier}, scope.OrganizationID)
	require.ErrorContains(t, err, "injected discovery deletion failure")
	row, err := q.GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "pending", row.State)
	var live bool
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT deleted_at IS NULL FROM device WHERE id=$1`, target.DeviceID).Scan(&live))
	require.True(t, live)
	require.ErrorContains(t, archiveDefault(t, conn, repo, scope, config), "automatic pool transitions")
}

func TestDeletedLifecycleReconciliationPreservesLivePolicyAndCompletedHistory(t *testing.T) {
	conn, scope, repo, target, config := defaultRetirementFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	dead := seedDefaultTransition(t, conn, scope, target, config)
	var liveDevice int64
	require.NoError(t, conn.QueryRowContext(ctx, `WITH discovery AS (
 INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name)
 VALUES($1,$2,'192.0.2.2','80','http','proto') RETURNING id)
 INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id)
 SELECT $1,$2,'02:00:00:00:00:02',id FROM discovery RETURNING id`, scope.OrganizationID, uuid.NewString()).Scan(&liveDevice))
	live := seedDefaultTransition(t, conn, scope, AssignmentTarget{Kind: TargetDevice, ID: liveDevice, DeviceID: liveDevice}, config)
	history := seedDefaultTransition(t, conn, scope, target, config)
	_, err := q.FinishPoolLifecycleTransition(ctx, sqlc.FinishPoolLifecycleTransitionParams{TransitionID: history, State: "completed"})
	require.NoError(t, err)
	// Model an older deletion that left lifecycle work pending.
	_, err = conn.ExecContext(ctx, `UPDATE device SET deleted_at=now() WHERE id=$1`, target.DeviceID)
	require.NoError(t, err)
	require.NoError(t, q.ReconcilePoolLifecycleOperations(ctx))
	row, err := q.GetPoolLifecycleTransition(ctx, dead)
	require.NoError(t, err)
	require.Equal(t, "superseded", row.State)
	row, err = q.GetPoolLifecycleTransition(ctx, history)
	require.NoError(t, err)
	require.Equal(t, "completed", row.State, "completed history is not rewritten")
	row, err = q.GetPoolLifecycleTransition(ctx, live)
	require.NoError(t, err)
	require.Equal(t, "pending", row.State, "live equipment still needs its captured configuration")
	require.ErrorContains(t, archiveDefault(t, conn, repo, scope, config), "automatic pool transitions")
	identifier, err := q.GetDeviceIdentifierByID(ctx, liveDevice)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(ctx, []string{identifier}, scope.OrganizationID)
	require.NoError(t, err)
	require.NoError(t, archiveDefault(t, conn, repo, scope, config))
}

func TestDeletedAutomaticClearKeepsUnknownWriteProtection(t *testing.T) {
	conn, scope, _, target, _ := defaultRetirementFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	var id uuid.UUID
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,5,$2,'ownership') RETURNING transition_id`, scope.OrganizationID, target.DeviceID).Scan(&id))
	_, err := q.PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: id, TargetKind: target.Kind, TargetID: target.ID, ReviewedBinding: "{}"})
	require.NoError(t, err)
	var attempt uuid.UUID
	err = devicewrite.NewService(conn).WithRecordedHostAttempt(devicewrite.WithPoolOperation(ctx, id), scope.OrganizationID, target.DeviceID, 0, commandtype.UpdateMiningPools, func(q sqlc.Querier, physical uuid.UUID) error {
		attempt = physical
		_, err := q.AcceptPoolRemoval(ctx, sqlc.AcceptPoolRemovalParams{RemovalID: id, PhysicalAttemptID: uuid.NullUUID{UUID: physical, Valid: true}})
		return err
	}, func(ctx context.Context) error {
		return devicewrite.Dispatch(ctx, func(context.Context) error { return devicewrite.UncertainDelivery(context.DeadlineExceeded) })
	})
	require.Error(t, err)
	before, err := q.LockDeviceWriteAttemptForRecovery(ctx, attempt)
	require.NoError(t, err)
	require.Equal(t, "unknown", before.Status)
	identifier, err := q.GetDeviceIdentifierByID(ctx, target.DeviceID)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(ctx, []string{identifier}, scope.OrganizationID)
	require.NoError(t, err)
	require.NoError(t, q.ReconcilePoolLifecycleOperations(ctx))
	row, err := q.GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "superseded", row.State)
	require.Equal(t, attempt, row.PhysicalAttemptID.UUID)
	after, err := q.LockDeviceWriteAttemptForRecovery(ctx, attempt)
	require.NoError(t, err)
	require.Equal(t, before, after, "retiring intent must preserve physical write evidence")
	drained, err := q.IsRetiredDeviceWriteDrained(ctx, before.IdentityKeys)
	require.NoError(t, err)
	require.False(t, drained, "the same hardware must remain protected after rediscovery")
}

func defaultRetirementFixture(t *testing.T) (*sql.DB, Scope, *Repository, AssignmentTarget, Configuration) {
	t.Helper()
	conn, scope, repo, target := assignmentFixture(t)
	config, err := db.WithTransaction(t.Context(), conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(t.Context(), q, scope, RevisionOptions{Name: "Default", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
	})
	require.NoError(t, err)
	return conn, scope, repo, target, config
}

func seedDefaultTransition(t *testing.T, conn *sql.DB, scope Scope, target AssignmentTarget, config Configuration) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,configuration_id,revision_id) VALUES($1,$2,5,$2,'ownership',$3,$4) RETURNING transition_id`, scope.OrganizationID, target.DeviceID, config.ID, config.RevisionID).Scan(&id))
	return id
}

func archiveDefault(t *testing.T, conn *sql.DB, repo *Repository, scope Scope, config Configuration) error {
	t.Helper()
	return db.WithTransactionNoResult(t.Context(), conn, func(q sqlc.Querier) error {
		return repo.Archive(t.Context(), q, scope, config.ID, config.Version)
	})
}

func TestDeletedLifecycleRetirementWinsOverOperationCompletion(t *testing.T) {
	conn, scope, repo, target, config := defaultRetirementFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	id := seedDefaultTransition(t, conn, scope, target, config)
	_, err := q.PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: id, TargetKind: target.Kind, TargetID: target.ID, ReviewedBinding: "{}"})
	require.NoError(t, err)
	operation, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		return repo.Assign(WithLifecycleTransition(ctx, id), q, scope, 0, config.ID, config.RevisionID, []AssignmentChange{{Target: target, ReviewedBinding: "{}"}})
	})
	require.NoError(t, err)
	require.NoError(t, q.LinkPoolLifecycleOperation(ctx, sqlc.LinkPoolLifecycleOperationParams{TransitionID: id, OperationID: sql.NullString{String: operation.ID, Valid: true}}))
	// Model deletion by the older code: assignment retirement completed, but the
	// transition still says applying when a completed operation is reconciled.
	_, err = conn.ExecContext(ctx, `UPDATE device SET deleted_at=now() WHERE id=$1`, target.DeviceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration_assignment SET retired_at=now(),version=version+1 WHERE operation_id=$1`, operation.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET state='verified' WHERE operation_id=$1`, operation.ID)
	require.NoError(t, err)
	require.NoError(t, q.ReconcilePoolLifecycleOperations(ctx))
	row, err := q.GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "superseded", row.State)
	require.Equal(t, "device_deleted", row.ResultCode)
	require.NoError(t, archiveDefault(t, conn, repo, scope, config))
}
