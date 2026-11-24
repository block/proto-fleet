package poolconfigurations

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestDeviceDeletionExplicitlyRetiresPoolAssignments(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "success"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			conn, scope, repo, target := assignmentFixture(t)
			ctx := t.Context()
			q := sqlc.New(conn)
			var identifier string
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT device_identifier FROM device WHERE id=$1`, target.DeviceID).Scan(&identifier))
			operation, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
				config, err := repo.Create(ctx, q, scope, RevisionOptions{Name: "Deleted miner", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
				if err != nil {
					return Operation{}, err
				}
				return repo.Assign(ctx, q, scope, 1, config.ID, config.RevisionID, []AssignmentChange{{Target: target}})
			})
			require.NoError(t, err)
			if rollback {
				// Fail after assignment retirement to prove the whole deletion rolls back.
				_, err = conn.ExecContext(ctx, `CREATE FUNCTION reject_discovery_deletion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected discovery deletion failure'; END; $$;
     CREATE TRIGGER reject_discovery_deletion BEFORE UPDATE OF deleted_at ON discovered_device FOR EACH ROW EXECUTE FUNCTION reject_discovery_deletion();`)
				require.NoError(t, err)
			}
			count, err := sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(ctx, []string{identifier}, scope.OrganizationID)
			if rollback {
				require.ErrorContains(t, err, "injected discovery deletion failure")
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
			}
			var retired, deleted bool
			var version int64
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT a.retired_at IS NOT NULL,a.version,d.deleted_at IS NOT NULL FROM pool_configuration_assignment a JOIN device d ON d.id=a.device_id WHERE a.organization_id=$1 AND a.target_id=$2`, scope.OrganizationID, target.ID).Scan(&retired, &version, &deleted))
			require.Equal(t, !rollback, retired)
			require.Equal(t, !rollback, deleted)
			result, err := repo.GetOperation(ctx, q, scope, operation.ID)
			require.NoError(t, err)
			if rollback {
				require.EqualValues(t, 1, version)
				require.Equal(t, "pending", result.Targets[0].State)
			} else {
				require.EqualValues(t, 2, version)
				require.Equal(t, "superseded", result.Targets[0].State)
				// Idempotent retries do not advance the retired generation again.
				require.NoError(t, q.RetirePoolConfigurationEquipmentAssignments(ctx, sqlc.RetirePoolConfigurationEquipmentAssignmentsParams{OrganizationID: scope.OrganizationID, DeviceIdentifiers: []string{identifier}}))
				assignment, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
				require.NoError(t, err)
				require.Equal(t, version, assignment.Version)
				_, err = repo.ClaimTarget(ctx, q, scope, operation.ID, target)
				require.Error(t, err, "retired work cannot dispatch")
			}
		})
	}
}
