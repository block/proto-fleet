package poolconfigurations

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReviewedBindingRollbackPreservesRetainedEvidence(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	operation, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		config, err := repo.Create(ctx, q, scope, RevisionOptions{Name: "Production", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
		if err != nil {
			return Operation{}, err
		}
		return repo.Assign(ctx, q, scope, 1, config.ID, config.RevisionID, []AssignmentChange{{Target: target, ReviewedBinding: `{"Version":1,"Label":"reviewed miner"}`}})
	})
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001047_internal_pool_operation_bindings.down.sql")
	require.NoError(t, err)
	for _, state := range []string{"pending", "applying", "verified", "superseded"} {
		t.Run(state, func(t *testing.T) {
			_, err := conn.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET state=$1 WHERE operation_id=$2`, state, operation.ID)
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(down))
			require.ErrorContains(t, err, "cannot roll back with retained pool operation bindings")
			var binding, retainedState string
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT reviewed_binding,state FROM pool_configuration_operation_target WHERE operation_id=$1`, operation.ID).Scan(&binding, &retainedState))
			require.JSONEq(t, `{"Version":1,"Label":"reviewed miner"}`, binding)
			require.Equal(t, state, retainedState)
			_, err = conn.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET reviewed_binding='' WHERE operation_id=$1`, operation.ID)
			require.ErrorContains(t, err, "pool operation target identity is immutable", "refused rollback must preserve the immutability trigger")
		})
	}
}

func assignmentFixture(t *testing.T) (*sql.DB, Scope, *Repository, AssignmentTarget) {
	t.Helper()
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, discovery, deviceID int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(&org))
	identifier := uuid.NewString()
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name) VALUES($1,$2,'192.0.2.1','80','http','proto') RETURNING id`, org, identifier).Scan(&discovery))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:01',$3) RETURNING id`, org, identifier, discovery).Scan(&deviceID))
	scope := Scope{OrganizationID: org}
	repo := NewRepository(testCipher(t))
	return conn, scope, repo, AssignmentTarget{Kind: TargetDevice, ID: deviceID, DeviceID: deviceID}
}

func TestAssignmentSwitchSupersedesOldWorkWithoutChangingItsRevision(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	create := func(name string) Configuration {
		t.Helper()
		configuration, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
			return repo.Create(ctx, q, scope, RevisionOptions{Name: name, Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
		})
		require.NoError(t, err)
		return configuration
	}
	first, second := create("First"), create("Second")
	assign := func(configuration Configuration, version int64) (Operation, error) {
		return db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
			return repo.Assign(ctx, q, scope, int64(1), configuration.ID, configuration.RevisionID, []AssignmentChange{{Target: target, ExpectedVersion: version}})
		})
	}
	old, err := assign(first, 0)
	require.NoError(t, err)
	require.Len(t, old.Targets, 1)
	require.Equal(t, "pending", old.Targets[0].State)
	wrongDevice := target
	wrongDevice.DeviceID++
	_, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (int32, error) {
		return repo.ClaimTarget(ctx, q, scope, old.ID, wrongDevice)
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "a write fence for another device cannot claim this target")
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.RetireAssignment(ctx, q, scope, wrongDevice, old.Targets[0].AssignmentVersion)
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	claim, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (int32, error) {
		return repo.ClaimTarget(ctx, q, scope, old.ID, target)
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, claim)
	next, err := assign(second, old.Targets[0].AssignmentVersion)
	require.NoError(t, err)
	require.NotEqual(t, old.ID, next.ID)
	var revision, state, current string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT o.revision_id,t.state FROM pool_configuration_operation o JOIN pool_configuration_operation_target t USING(operation_id) WHERE o.operation_id=$1`, old.ID).Scan(&revision, &state))
	require.Equal(t, first.RevisionID, revision)
	require.Equal(t, "superseded", state)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT configuration_id FROM pool_configuration_assignment WHERE organization_id=$1 AND target_kind='device' AND target_id=$2`, scope.OrganizationID, target.ID).Scan(&current))
	require.Equal(t, second.ID, current)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.FinishTarget(ctx, q, scope, old.ID, target, claim, "verified", "")
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "late completion cannot revive superseded work")
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.RetryTarget(ctx, q, scope, old.ID, target)
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "retry cannot move equipment back to an old revision")
	_, err = assign(first, old.Targets[0].AssignmentVersion)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "a stale selection must not move a target back")
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation WHERE organization_id=$1`, scope.OrganizationID).Scan(&count))
	require.Equal(t, 2, count, "failed admission must not leave partial work")
	claim, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (int32, error) {
		return repo.ClaimTarget(ctx, q, scope, next.ID, target)
	})
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.FinishTarget(ctx, q, scope, next.ID, target, claim, "failed", "unavailable")
	})
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.RetryTarget(ctx, q, scope, next.ID, target)
	})
	require.NoError(t, err)
	claim, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (int32, error) {
		return repo.ClaimTarget(ctx, q, scope, next.ID, target)
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, claim)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.FinishTarget(ctx, q, scope, next.ID, target, 1, "verified", "")
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "a previous attempt cannot finish a retry")
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.FinishTarget(ctx, q, scope, next.ID, wrongDevice, claim, "verified", "")
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.FinishTarget(ctx, q, scope, next.ID, target, claim, "failed", "write_uncertain")
	})
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.RetryTarget(ctx, q, scope, next.ID, target)
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "uncertain writes need reconciliation before another dispatch")
}

func TestReplacingUnverifiedAssignmentSupersedesItsOperation(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	err := db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		configuration, err := repo.Create(ctx, q, scope, RevisionOptions{Name: "Pools", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
		require.NoError(t, err)
		first, err := repo.Assign(ctx, q, scope, 1, configuration.ID, configuration.RevisionID, []AssignmentChange{{Target: target}})
		require.NoError(t, err)
		attempt, err := repo.ClaimTarget(ctx, q, scope, first.ID, target)
		require.NoError(t, err)
		require.NoError(t, repo.FinishTarget(ctx, q, scope, first.ID, target, attempt, "applied_unverified", "readback_unavailable"))
		_, err = repo.Assign(ctx, q, scope, 1, configuration.ID, configuration.RevisionID, []AssignmentChange{{Target: target, ExpectedVersion: first.Targets[0].AssignmentVersion}})
		require.NoError(t, err)
		previous, err := repo.GetOperation(ctx, q, scope, first.ID)
		require.NoError(t, err)
		require.Equal(t, "superseded", previous.Targets[0].State)
		require.True(t, fleeterror.IsFailedPreconditionError(repo.RetryTarget(ctx, q, scope, first.ID, target)))
		return nil
	})
	require.NoError(t, err)
}

func TestWholeContainerAndInstalledBoardAssignments(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	_, err := conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, target.DeviceID)
	require.NoError(t, err)
	var identifier string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT device_identifier FROM device WHERE id=$1`, target.DeviceID).Scan(&identifier))
	_, err = sqlc.New(conn).AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: identifier, ObservedAt: time.Now()})
	require.NoError(t, err)
	config, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, scope, RevisionOptions{Name: "Container pools", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	})
	require.NoError(t, err)
	assign := func(target AssignmentTarget, expected int64) (Operation, error) {
		return db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
			return repo.Assign(ctx, q, scope, 1, config.ID, config.RevisionID, []AssignmentChange{{Target: target, ExpectedVersion: expected}})
		})
	}
	whole, err := assign(target, 0)
	require.NoError(t, err, "a container without mining groups uses whole-device intent")
	require.Len(t, whole.Targets, 1)
	var tank, module, board, installation int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, target.DeviceID).Scan(&tank))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,1,'module',now(),now()) RETURNING id`, tank).Scan(&module))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, scope.OrganizationID).Scan(&board))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,1,now(),now()) RETURNING id`, board, module).Scan(&installation))
	boardTarget := AssignmentTarget{Kind: TargetInstallation, ID: installation, DeviceID: target.DeviceID}
	_, err = assign(boardTarget, 0)
	require.Error(t, err, "whole-container and board intent cannot be active simultaneously")
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.RetireAssignment(ctx, q, scope, target, whole.Targets[0].AssignmentVersion)
	})
	require.NoError(t, err)
	assigned, err := assign(boardTarget, 0)
	require.NoError(t, err)
	// A module can move before inventory closes its previous installation. The
	// installation's captured controller must still prevent cross-device writes.
	otherIdentifier := uuid.NewString()
	var otherDiscovery, otherDevice, otherTank int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.2','80','http','proto',2) RETURNING id`, scope.OrganizationID, otherIdentifier).Scan(&otherDiscovery))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:02',$3) RETURNING id`, scope.OrganizationID, otherIdentifier, otherDiscovery).Scan(&otherDevice))
	_, err = sqlc.New(conn).AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: otherIdentifier, ObservedAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, otherDevice).Scan(&otherTank))
	_, err = conn.ExecContext(ctx, `UPDATE container_module SET tank_id=$1 WHERE id=$2`, otherTank, module)
	require.NoError(t, err)
	_, err = sqlc.New(conn).GetPoolAssignmentInstallation(ctx, sqlc.GetPoolAssignmentInstallationParams{OrgID: scope.OrganizationID, ID: installation, DeviceID: otherDevice})
	require.ErrorIs(t, err, sql.ErrNoRows, "moving a module does not move its old installation identity")
	_, err = conn.ExecContext(ctx, `UPDATE container_module SET tank_id=$1 WHERE id=$2`, tank, module)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE id=$1`, installation)
	require.NoError(t, err)
	var replacement int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,1,now(),now()) RETURNING id`, board, module).Scan(&replacement))
	_, err = assign(boardTarget, assigned.Targets[0].AssignmentVersion)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "an old installation cannot target the replacement occupant")
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE target_kind='hashboard_installation' AND target_id=$1`, replacement).Scan(&count))
	require.Zero(t, count, "replacement installations never inherit intent")
}
