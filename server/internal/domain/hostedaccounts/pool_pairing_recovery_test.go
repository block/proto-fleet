package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestPairingRecoveryPreservesManualPoolAssignment(t *testing.T) {
	for _, class := range []commonpb.DeviceClass{commonpb.DeviceClass_DEVICE_CLASS_RIG, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER} {
		t.Run(class.String(), func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			repo := poolconfigurations.NewRepository(cipher)
			scope := poolconfigurations.Scope{OrganizationID: f.org}
			create := func(name string) poolconfigurations.Configuration {
				config, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
					return repo.Create(t.Context(), q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
				})
				require.NoError(t, err)
				return config
			}
			defaultConfig, manual := create("default"), create("manual")
			device := f.miningDeviceWithPairingStatus(t, class, "AUTHENTICATION_NEEDED")
			q := sqlc.New(f.db)
			pair := func(status sqlc.PairingStatusEnum) {
				_, err := q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device, PairingStatus: status})
				require.NoError(t, err)
			}
			countEvents := func() int {
				var count int
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing'`, device).Scan(&count))
				return count
			}
			require.Zero(t, countEvents(), "awaiting first credentials must not enqueue pools")
			pair(sqlc.PairingStatusEnumPAIRED)
			require.Equal(t, 1, countEvents(), "first successful pairing must apply the default")
			operation, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
				return repo.Assign(t.Context(), q, scope, f.host, manual.ID, manual.RevisionID, []poolconfigurations.AssignmentChange{{Target: poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: device, DeviceID: device}}})
			})
			require.NoError(t, err)
			// Exercise both a status-only authentication failure and the upsert
			// used when recovered credentials or a recovered endpoint are saved.
			_, err = f.db.ExecContext(t.Context(), `UPDATE device_pairing SET pairing_status='AUTHENTICATION_NEEDED' WHERE device_id=$1`, device)
			require.NoError(t, err)
			pair(sqlc.PairingStatusEnumPAIRED)
			pair(sqlc.PairingStatusEnumDEFAULTPASSWORD)
			pair(sqlc.PairingStatusEnumPAIRED)
			require.Equal(t, 1, countEvents(), "credential recovery and paired-like updates must not enqueue another default")
			assignment, err := q.GetPoolConfigurationAssignment(t.Context(), sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: poolconfigurations.TargetDevice, TargetID: device})
			require.NoError(t, err)
			require.Equal(t, manual.ID, assignment.ConfigurationID)
			require.Equal(t, operation.ID, assignment.OperationID.String)

			// Local unpair updates status without setting unpaired_at. The next
			// credentials-needed upsert also clears that timestamp.
			_, err = f.db.ExecContext(t.Context(), `UPDATE device_pairing SET pairing_status='UNPAIRED' WHERE device_id=$1`, device)
			require.NoError(t, err)
			pair(sqlc.PairingStatusEnumAUTHENTICATIONNEEDED)
			pair(sqlc.PairingStatusEnumDEFAULTPASSWORD)
			require.Equal(t, 2, countEvents(), "deliberate re-pairing must enqueue the default again")
			var captured string
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(configuration_id,pairing_defaults->'0'->>'configuration_id') FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing' ORDER BY created_at DESC LIMIT 1`, device).Scan(&captured))
			require.Equal(t, defaultConfig.ID, captured)
		})
	}
}

func TestOperatorAuthenticationRecoveryPreservesAllocatedHardware(t *testing.T) {
	for _, container := range []bool{false, true} {
		t.Run(map[bool]string{false: "miner", true: "container"}[container], func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			repo := poolconfigurations.NewRepository(cipher)
			clientScope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}
			create := func(scope poolconfigurations.Scope, name string) poolconfigurations.Configuration {
				config, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
					return repo.Create(t.Context(), q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
				})
				require.NoError(t, err)
				return config
			}
			create(poolconfigurations.Scope{OrganizationID: f.org}, "operator-default")
			create(clientScope, "client-default")
			manual := create(clientScope, "client-manual")
			var device int64
			if container {
				var module int64
				module, device = f.miningModule(t)
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			} else {
				device = f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, device, 0)))
			}
			operation, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
				return repo.Assign(t.Context(), q, clientScope, f.host, manual.ID, manual.RevisionID, []poolconfigurations.AssignmentChange{{Target: poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: device, DeviceID: device}}})
			})
			require.NoError(t, err)
			snapshot := func() (string, int) {
				var allocation string
				var events int
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT jsonb_agg(jsonb_build_array(kind,asset_id,client_id,subclient_id,revision) ORDER BY kind,asset_id)::text FROM hosted_account_asset WHERE device_id=$1`, device).Scan(&allocation))
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1`, device).Scan(&events))
				return allocation, events
			}
			beforeAllocation, beforeEvents := snapshot()
			q := sqlc.New(f.db)
			// Pairing is controlled by the operator even when the current pool
			// assignment and all installed hardware belong to a client.
			for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumAUTHENTICATIONNEEDED, sqlc.PairingStatusEnumPAIRED} {
				_, err := q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device, PairingStatus: status})
				require.NoError(t, err)
			}
			afterAllocation, afterEvents := snapshot()
			require.Equal(t, beforeAllocation, afterAllocation, "operator recovery must preserve client ownership and allocation revision")
			require.Equal(t, beforeEvents, afterEvents, "neither account's default may be reapplied")
			assignment, err := q.GetPoolConfigurationAssignment(t.Context(), sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: poolconfigurations.TargetDevice, TargetID: device})
			require.NoError(t, err)
			require.Equal(t, manual.ID, assignment.ConfigurationID)
			require.Equal(t, operation.ID, assignment.OperationID.String)
		})
	}
}
