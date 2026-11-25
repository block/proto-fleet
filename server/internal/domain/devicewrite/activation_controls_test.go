package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPendingActivationQueuedControls(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    commandtype.Type
		payload string
		allowed bool
	}{
		{"stop", commandtype.StopMining, "", true},
		{"reboot", commandtype.Reboot, "", true},
		{"container stop", commandtype.ContainerMining, `{"request":{"action":"set_mining","mining":false}}`, true},
		{"container start", commandtype.ContainerMining, `{"request":{"action":"set_mining","mining":true}}`, false},
		{"container power", commandtype.ContainerMining, `{"request":{"action":"set_power_target","power":{"watts":1000}}}`, false},
		{"missing state", commandtype.ContainerMining, `{"request":{"action":"set_mining"}}`, false},
		{"mixed request", commandtype.ContainerMining, `{"request":{"action":"set_mining","mining":false,"power":{"watts":1000}}}`, false},
		{"malformed request", commandtype.ContainerMining, `{`, false},
		{"empty request", commandtype.ContainerMining, `null`, false},
		{"pools", commandtype.UpdateMiningPools, "", false},
		{"unpair", commandtype.Unpair, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, org, device, message, _ := queuedFenceFixture(t, tc.kind)
			_, err := database.DB.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'activate','complete')`, device, org)
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), `WITH request AS (INSERT INTO mining_group_request(organization_id,request_id,device_id,fingerprint) VALUES($1,gen_random_uuid(),$2,'test') RETURNING *) INSERT INTO mining_group_pool_handoff(organization_id,operation_id,device_id,captured,placement_pending) SELECT organization_id,request_id,device_id,'{}',true FROM request`, org, device)
			require.NoError(t, err)
			message.Payload = []byte(tc.payload)
			service := devicewrite.NewService(database.DB)
			attempt, err := service.BeginQueuedHost(t.Context(), message)
			if !tc.allowed {
				require.ErrorContains(t, err, devicewrite.ActivationPoolPlacementPending)
				return
			}
			require.NoError(t, err)
			require.NoError(t, attempt.Finish(t.Context(), nil))
			// Both a running call and an uncertain result still block these controls.
			q := sqlc.New(database.DB)
			physical := uuid.New()
			require.NoError(t, q.CreateDeviceWriteAttempt(t.Context(), sqlc.CreateDeviceWriteAttemptParams{AttemptID: physical, DeviceID: device, OrganizationID: org, Epoch: message.WriteEpoch, OwnerInstanceID: uuid.New(), OperationKind: "mutation", CommandType: "UpdateMiningPools", IdentityKeys: []string{}}))
			for _, status := range []string{"running", "unknown"} {
				if status == "unknown" {
					_, err = q.FinishDeviceWriteAttempt(t.Context(), sqlc.FinishDeviceWriteAttemptParams{AttemptID: physical, Status: status, WriteOutcome: []byte(`{}`)})
					require.NoError(t, err)
				}
				_, err = service.BeginQueuedHost(t.Context(), message)
				require.ErrorContains(t, err, "unresolved configuration operation")
				for _, kind := range []commandtype.Type{commandtype.StopMining, commandtype.Reboot} {
					err = service.WithHostAttempt(t.Context(), org, device, message.WriteEpoch, kind, func(context.Context) error { t.Fatal("unresolved write must block control"); return nil })
					require.ErrorContains(t, err, "unresolved configuration operation")
				}
			}
		})
	}
}
