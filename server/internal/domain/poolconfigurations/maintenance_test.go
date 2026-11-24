package poolconfigurations

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

type maintenanceAuthority struct {
	selectionAuthority
	accesses []Access
}

func (a *maintenanceAuthority) AuthorizePoolConfiguration(ctx context.Context, q sqlc.Querier, accountID string, access Access) (Principal, error) {
	a.accesses = append(a.accesses, access)
	return a.selectionAuthority.AuthorizePoolConfiguration(ctx, q, accountID, access)
}

func poolMaintenanceState(t *testing.T, conn *sql.DB, orgID int64) map[string]string {
	t.Helper()
	state := map[string]string{}
	for _, table := range []string{"pool_configuration", "pool_configuration_revision", "pool_configuration_review", "pool_configuration_assignment", "pool_configuration_operation", "pool_configuration_operation_target", "activity_log"} {
		var rows string
		require.NoError(t, conn.QueryRowContext(t.Context(), fmt.Sprintf(`SELECT COALESCE(jsonb_agg(to_jsonb(row) ORDER BY to_jsonb(row)::text),'[]')::text FROM %s row WHERE organization_id=$1`, table), orgID).Scan(&rows))
		state[table] = rows
	}
	return state
}

func TestMaintenanceFreezeRejectsServiceCreateAndEquipmentConfirmationAtomically(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	authority := &maintenanceAuthority{selectionAuthority: selectionAuthority{Principal{Scope: scope, ActorID: 1}}}
	equipment := &selectionEquipment{reviewAuthority{binding: "maintenance-reviewed-device"}}
	service := NewService(conn, repo, authority, equipment)
	options := RevisionOptions{Name: "Existing configuration", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}}
	config, err := service.Create(ctx, "", options)
	require.NoError(t, err)
	review, err := service.ReviewAssignment(ctx, "", config.ID, config.Version, []AssignmentTarget{target})
	require.NoError(t, err)
	require.Len(t, review.Targets, 1)
	require.NoError(t, db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
			return err
		}
		return q.FreezePoolConfigurationWriters(ctx, scope.OrganizationID)
	}))
	before := poolMaintenanceState(t, conn, scope.OrganizationID)
	_, err = service.Create(ctx, "", RevisionOptions{Name: "Blocked creation", Slots: options.Slots})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "maintenance must reject service Create: %v", err)
	require.Contains(t, err.Error(), "maintenance")
	require.Equal(t, before, poolMaintenanceState(t, conn, scope.OrganizationID))
	authority.accesses = nil
	_, err = service.ConfirmReview(ctx, "", review.ID, Confirmation{Password: "actor-password"})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "maintenance must reject equipment confirmation even with Read authority: %v", err)
	require.Contains(t, err.Error(), "maintenance")
	require.NotEmpty(t, authority.accesses)
	for _, access := range authority.accesses {
		require.Equal(t, Read, access, "equipment confirmation uses Read authority and needs its own maintenance guard")
	}
	require.Equal(t, before, poolMaintenanceState(t, conn, scope.OrganizationID), "rejection must preserve draft review, assignments, operations, configurations, and audit")
	got, err := service.Get(ctx, "", config.ID)
	require.NoError(t, err)
	require.Equal(t, config, got.Configuration)
	page, err := service.List(ctx, "", "", 100, false)
	require.NoError(t, err)
	require.Len(t, page.Configurations, 1)
	require.Equal(t, before, poolMaintenanceState(t, conn, scope.OrganizationID), "maintenance reads must remain available without side effects")
	// The maintenance gate records completion after its checks. Canonical
	// mutation resumes while the permanent legacy-writer fence remains active.
	require.NoError(t, db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return q.RecordPoolConfigurationCutover(ctx, sqlc.RecordPoolConfigurationCutoverParams{OrganizationID: scope.OrganizationID, Report: []byte(`{"complete":true}`)})
	}))
	complete, err := sqlc.New(conn).HasPoolConfigurationCutover(ctx, scope.OrganizationID)
	require.NoError(t, err)
	require.True(t, complete)
	created, err := service.Create(ctx, "", RevisionOptions{Name: "Creation resumed", Slots: options.Slots})
	require.NoError(t, err)
	require.NotEqual(t, config.ID, created.ID)
	confirmed, err := service.ConfirmReview(ctx, "", review.ID, Confirmation{Password: "actor-password"})
	require.NoError(t, err, "the same unconsumed review remains confirmable after maintenance completes")
	require.Len(t, confirmed.Operation.Targets, 1)
	require.Equal(t, target.ID, confirmed.Operation.Targets[0].ID)
	require.Equal(t, "pending", confirmed.Operation.Targets[0].State)
}
