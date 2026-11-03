package poolconfigurations

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestEquipmentResultsKeepDeviceIdentityWhenLabelsAreIdentical(t *testing.T) {
	for _, identifier := range []string{"miner-a", "miner-b"} {
		binding := `{"Label":"Same worker name","Device":{"DeviceIdentifier":"` + identifier + `","IdentityKeys":["private-route"]},"Usernames":["private-payout"]}`
		reviewed := reviewedEquipment([]reviewedTarget{{Change: AssignmentChange{Target: AssignmentTarget{Kind: TargetDevice, ID: 7}}, Binding: binding}})
		require.Equal(t, identifier, reviewed[0].DeviceIdentifier)
		result := publicOperationTarget(sqlc.PoolConfigurationOperationTarget{TargetKind: TargetDevice, TargetID: 7, ReviewedBinding: binding})
		require.Equal(t, identifier, result.DeviceIdentifier)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-route")
		require.NotContains(t, string(encoded), "private-payout")
	}
}

func TestOperationDisplayRetainsReviewedIdentityAfterEquipmentChanges(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	identifier, err := q.GetDeviceIdentifierByID(ctx, target.DeviceID)
	require.NoError(t, err)
	binding := fmt.Sprintf(`{"Label":"Original worker","Device":{"ID":%d,"DeviceIdentifier":%q}}`, target.DeviceID, identifier)
	operation, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		config, err := repo.Create(ctx, q, scope, RevisionOptions{Name: "Original configuration", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
		if err != nil {
			return Operation{}, err
		}
		return repo.Assign(ctx, q, scope, 1, config.ID, config.RevisionID, []AssignmentChange{{Target: target, ReviewedBinding: binding}})
	})
	require.NoError(t, err)
	// Current metadata may belong to a subsequent equipment owner. Historical
	// results must use the identity authorized when the operation was reviewed.
	_, err = conn.ExecContext(ctx, `UPDATE device SET custom_name='Next customer miner',mac_address='02:00:00:00:00:99' WHERE id=$1`, target.DeviceID)
	require.NoError(t, err)
	result, err := repo.GetOperation(ctx, q, scope, operation.ID)
	require.NoError(t, err)
	require.Len(t, result.Targets, 1)
	require.Equal(t, "Original worker", result.Targets[0].Label)
	require.Equal(t, identifier, result.Targets[0].DeviceIdentifier)
	require.Empty(t, result.Targets[0].MACAddress, "do not fill missing historical fields with current metadata")
}
