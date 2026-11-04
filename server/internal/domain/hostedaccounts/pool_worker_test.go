package hostedaccounts_test

import (
	"context"
	"errors"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type poolDevice struct {
	interfaces.Miner
	writes          int
	pools           []dto.MiningPool
	unknownPassword bool
	different       bool
	writeError      error
	readError       error
	onWrite         func()
}

func (m *poolDevice) UpdateMiningPools(_ context.Context, p dto.UpdateMiningPoolsPayload) error {
	m.writes++
	if m.onWrite != nil {
		m.onWrite()
	}
	m.pools = []dto.MiningPool{p.DefaultPool}
	if p.Backup1Pool != nil {
		m.pools = append(m.pools, *p.Backup1Pool)
	}
	if p.Backup2Pool != nil {
		m.pools = append(m.pools, *p.Backup2Pool)
	}
	return m.writeError
}
func (m *poolDevice) GetMiningPools(context.Context) ([]interfaces.MinerConfiguredPool, error) {
	if m.readError != nil {
		return nil, m.readError
	}
	result := make([]interfaces.MinerConfiguredPool, len(m.pools))
	for i, p := range m.pools {
		result[i] = interfaces.MinerConfiguredPool{Priority: int32(p.Priority), URL: p.URL, Username: p.Username} // #nosec G115 -- fixture payload contains at most three slots.
	}
	if m.different && len(result) > 0 {
		result[0].Username = "externally-changed"
	}
	return result, nil
}
func (m *poolDevice) GetStoredMiningPools(context.Context) ([]interfaces.MinerStoredPool, error) {
	result := make([]interfaces.MinerStoredPool, len(m.pools))
	for i, p := range m.pools {
		result[i] = interfaces.MinerStoredPool{Priority: int32(p.Priority), URL: p.URL, Username: p.Username} // #nosec G115 -- fixture payload contains at most three slots.
		if !m.unknownPassword {
			password := p.Password
			result[i].Password = &password
		}
	}
	return result, nil
}

type poolMinerGetter struct{ miner interfaces.Miner }

func (g poolMinerGetter) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return g.miner, nil
}

func TestPoolWorkerUsesReviewedRevisionAndVerifiesReadback(t *testing.T) {
	for _, scenario := range []struct{ name, state, code string }{
		{"complete readback", "verified", ""},
		{"unrelated write before review", "verified", ""},
		{"unrelated write after confirmation", "verified", ""},
		{"conflicting write after confirmation", "failed", "interrupted"},
		{"hidden password", "applied_unverified", "readback_unavailable"},
		{"runtime differs", "different", "settings_differ"},
		{"lost acknowledgement", "failed", "write_uncertain"},
		{"terminal write failure", "failed", "write_failed"},
		{"refused readback", "applied_unverified", "readback_unavailable"},
		{"permission revoked", "failed", "rejected"},
		{"allocation recalled", "superseded", ""},
		{"worker changed", "failed", "rejected"},
		{"native SV2 unavailable", "failed", "rejected"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			caps := poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true, sdk.CapabilityNativeStratumV2: true}
			reviewer := poolapplications.NewReviewer(f.pools, caps)
			repo := poolconfigurations.NewRepository(cipher)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer)
			password := "never-log-pool-secret" // #nosec G101 -- synthetic credential used to verify redaction.
			poolURL := "stratum+tcp://pool.example"
			if scenario.name == "native SV2 unavailable" {
				poolURL = "stratum2+tcp://pool.example:3333/79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
			}
			config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{
				{URL: poolURL, Username: "payout.with.periods", Password: &password, WorkerTemplate: "{miner_name}"},
				{URL: "stratum+tcp://backup.example", Username: "backup", Password: new(string)},
			}})
			require.NoError(t, err)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			_, err = f.db.ExecContext(ctx, `UPDATE device SET worker_name='worker-1' WHERE id=$1`, miner)
			require.NoError(t, err)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			recordUncertain := func(kind commandtype.Type) {
				writes := devicewrite.NewService(f.db)
				require.Error(t, writes.WithHostAttempt(ctx, f.org, miner, 0, kind, func(ctx context.Context) error {
					return devicewrite.Dispatch(ctx, func(context.Context) error { return devicewrite.UncertainDelivery(errors.New("lost response")) })
				}))
			}
			if scenario.name == "unrelated write before review" {
				recordUncertain(commandtype.BlinkLED)
			}
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
			require.NoError(t, err)
			saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
			require.NoError(t, err)
			device := &poolDevice{}
			switch scenario.name {
			case "native SV2 unavailable":
				caps[sdk.CapabilityNativeStratumV2] = false
			case "unrelated write after confirmation":
				recordUncertain(commandtype.BlinkLED)
			case "conflicting write after confirmation":
				recordUncertain(commandtype.UpdateMiningPools)
			case "hidden password":
				device.unknownPassword = true
			case "runtime differs":
				device.different = true
			case "lost acknowledgement":
				device.writeError = errors.New(password)
			case "terminal write failure":
				device.writeError = sdk.WithWriteOutcome(errors.New(password), sdk.WriteCompleted, sdk.WriteStageDispatch, sdk.WriteReasonFinished)
			case "refused readback":
				device.readError = sdk.WriteRefused(errors.New("reader backend changed"), sdk.WriteStageBackendSelection, sdk.WriteReasonStaleBinding)
			case "permission revoked":
				require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false))
			case "allocation recalled":
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
			case "worker changed":
				_, err = f.db.ExecContext(ctx, `UPDATE device SET worker_name='worker-2' WHERE id=$1`, miner)
				require.NoError(t, err)
			}
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
			var state, code string
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT state,result_code FROM pool_configuration_operation_target WHERE operation_id=$1`, saved.Operation.ID).Scan(&state, &code))
			require.Equal(t, scenario.state, state)
			require.Equal(t, scenario.code, code)
			if code == "rejected" || code == "interrupted" || state == "superseded" {
				require.Zero(t, device.writes)
			} else {
				require.Equal(t, 1, device.writes)
				require.Len(t, device.pools, 2)
				require.Equal(t, "payout.with.periods.worker-1", device.pools[0].Username)
				require.Equal(t, password, device.pools[0].Password)
				require.Empty(t, device.pools[1].Password)
			}
			previous := device.writes
			require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
			require.Equal(t, previous, device.writes, "finished or uncertain work is never automatically replayed")
			retry, retryErr := service.ReviewRetry(ctx, f.account.AccountID, saved.Operation.ID, []poolconfigurations.AssignmentTarget{target})
			if scenario.name == "hidden password" || scenario.name == "runtime differs" || scenario.name == "worker changed" || scenario.name == "terminal write failure" || scenario.name == "refused readback" {
				require.NoError(t, retryErr)
				retried, err := service.ConfirmReview(ctx, f.account.AccountID, retry.ID, "actor-password")
				require.NoError(t, err)
				require.NotEqual(t, saved.Operation.ID, retried.Operation.ID)
				require.Equal(t, saved.Operation.RevisionID, retried.Operation.RevisionID, "retry retains the exact approved settings")
				old, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
				require.NoError(t, err)
				require.Equal(t, "superseded", old.Targets[0].State)
			} else {
				require.Error(t, retryErr, "completed, uncertain, superseded and unauthorized updates cannot be retried")
			}
		})
	}
}
