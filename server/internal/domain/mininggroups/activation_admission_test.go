package mininggroups

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Unlike the controller simulator, the real plugin checks admission for every read.
type admissionCheckedActivationMiner struct{ *activationPoolMiner }

func (m admissionCheckedActivationMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}
func (m admissionCheckedActivationMiner) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (out sdk.MiningGroupResult, err error) {
	err = devicewrite.Observe(ctx, func(ctx context.Context) error { out, err = m.activationPoolMiner.MiningGroups(ctx, r); return err })
	return
}

func TestActivationInventoryUsesCommittedObservationAdmission(t *testing.T) {
	for _, withPools := range []bool{false, true} {
		name := "without pools"
		if withPools {
			name = "with pools"
		}
		t.Run(name, func(t *testing.T) {
			ctx, f, miner, _, _, _, _ := newActivationPoolFixture(t)
			if !withPools {
				_, err := f.conn.ExecContext(ctx, `UPDATE pool_configuration_assignment SET retired_at=now(),version=version+1 WHERE device_id=$1`, f.target.DeviceID)
				require.NoError(t, err)
			}
			f.service.miners = admissionCheckedActivationMiner{miner}
			reads := 0
			miner.beforeCall = func(_ context.Context, r sdk.MiningGroupRequest) {
				if r.Action != "activation_inventory" {
					return
				}
				reads++
				lockCtx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				tx, err := f.conn.BeginTx(lockCtx, nil)
				require.NoError(t, err)
				defer tx.Rollback()
				_, err = sqlc.New(tx).LockPoolConfigurationImportOrganization(lockCtx, f.target.OrganizationID)
				require.NoError(t, err, "inventory read must not hold the organization update lock")
			}
			request := uuid.NewString()
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, request)
			require.NoError(t, err)
			require.Equal(t, 1, reads)
			require.Equal(t, "pending", f.state(t).Status)
			_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, request)
			require.NoError(t, err)
			require.Equal(t, 1, reads, "replaying an accepted request must not require another controller read")
		})
	}
}

func TestActivationCaptureRejectsChangedPreflightEvidence(t *testing.T) {
	for _, scenario := range []string{"binding", "stale inventory", "permissions"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, f, miner, _, _, _, _ := newActivationPoolFixture(t)
			info, err := session.GetInfo(ctx)
			require.NoError(t, err)
			evidence, err := f.service.prepareActivationInventory(ctx, f.target.OrganizationID, info.UserID, f.target.DeviceID)
			require.NoError(t, err)
			row := f.state(t)
			row.ActorID = sql.NullInt64{Int64: info.UserID, Valid: true}
			expected := ""
			switch scenario {
			case "binding":
				_, err = f.conn.ExecContext(ctx, `UPDATE device SET worker_name='changed-after-read' WHERE id=$1`, f.target.DeviceID)
				require.NoError(t, err)
				expected = activationInventoryBindingChanged
			case "stale inventory":
				evidence.Inventory.ObservedAt = time.Now().Add(-3 * time.Minute)
				expected = "Hashboard inventory is stale"
			case "permissions":
				setMiningGroupPermissions(t, ctx, f, []string{"site:read", "fleet:read", "miner:read"})
				expected = "permission to manage this container is required"
			}
			err = db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
				_, err := f.service.captureActivationPools(ctx, q, row, miner.snapshot, evidence)
				return err
			})
			require.ErrorContains(t, err, expected)
			var captures int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM mining_group_pool_handoff WHERE device_id=$1`, f.target.DeviceID).Scan(&captures))
			require.Zero(t, captures)
		})
	}
}
