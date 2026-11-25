package timescaledb

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestInventoryExplicitlyRetiresChangedPoolAssignments(t *testing.T) {
	for _, scenario := range []string{"unchanged", "board removed", "MAC changed", "module removed", "module moved and returned", "cross-controller move", "cross-controller board move", "conflicting controller", "stale snapshot", "incomplete snapshot", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			r, other := replacementControllers(t)
			r.base = r.base.Add(-5 * time.Minute)
			snapshot := func(at time.Time, position int32, mac string) *sdk.ContainerSnapshot {
				board := identifiedBoard(at, "board-a")
				board.MACAddress = mac
				return modulePSUSnapshot(at, sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(at, position, "psu-a", board)}})
			}
			require.NoError(t, r.store(t, snapshot(r.base, 1, "02:00:00:00:00:01")))
			var installation int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, r.boardID(t, "board-a")).Scan(&installation))
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: cfg.ServiceMasterKey})
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(secrets)
			scope := poolconfigurations.Scope{OrganizationID: r.orgID}
			op, err := db.WithTransaction(t.Context(), r.db, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
				c, err := repo.Create(t.Context(), q, scope, poolconfigurations.RevisionOptions{Name: "Assigned", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
				if err != nil {
					return poolconfigurations.Operation{}, err
				}
				return repo.Assign(t.Context(), q, scope, 1, c.ID, c.RevisionID, []poolconfigurations.AssignmentChange{{Target: poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: installation, DeviceID: r.deviceID}}})
			})
			require.NoError(t, err)
			at := r.base.Add(time.Second)
			next := snapshot(at, 1, "02:00:00:00:00:01")
			switch scenario {
			case "board removed", "rollback":
				next.Tanks[0].Modules[0].Hashboards = []sdk.ContainerHashboard{{Position: 1, Occupancy: "empty"}}
			case "MAC changed":
				next = snapshot(at, 1, "02:00:00:00:00:99")
			case "module removed":
				next.Tanks[0].Modules = nil
			case "stale snapshot":
				next = snapshot(r.base.Add(-time.Second), 2, "02:00:00:00:00:01")
			case "incomplete snapshot":
				next.TopologyComplete = false
				next.Tanks[0].Modules = nil
			case "cross-controller move", "cross-controller board move":
				next = snapshot(r.base.Add(sdk.ContainerReadingMaxAge+time.Second), 1, "02:00:00:00:00:01")
				if scenario == "cross-controller board move" {
					next.Tanks[0].Modules[0].ReportedID = "destination-psu"
				}
			case "module moved and returned":
				next = snapshot(at, 2, "02:00:00:00:00:01")
			}
			if scenario == "rollback" {
				tx, err := r.db.BeginTx(t.Context(), nil)
				require.NoError(t, err)
				defer tx.Rollback()
				require.NoError(t, storeContainerInventory(t.Context(), sqlc.New(tx), r.device, next))
				var retired bool
				require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT retired_at IS NOT NULL FROM pool_configuration_assignment WHERE target_id=$1`, installation).Scan(&retired))
				require.True(t, retired)
				require.NoError(t, tx.Rollback())
			} else if scenario == "cross-controller move" || scenario == "cross-controller board move" || scenario == "conflicting controller" {
				require.NoError(t, other.store(t, next))
			} else {
				require.NoError(t, r.store(t, next))
			}
			if scenario == "module moved and returned" {
				require.NoError(t, r.store(t, snapshot(at.Add(time.Second), 1, "02:00:00:00:00:01")))
			}
			var retired bool
			var version int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT retired_at IS NOT NULL,version FROM pool_configuration_assignment WHERE target_id=$1`, installation).Scan(&retired, &version))
			wantRetired := scenario != "unchanged" && scenario != "rollback" && scenario != "stale snapshot" && scenario != "incomplete snapshot" && scenario != "conflicting controller"
			require.Equal(t, wantRetired, retired)
			wantVersion := int64(1)
			wantState := "pending"
			if wantRetired {
				wantVersion = 2
				wantState = "superseded"
			}
			require.Equal(t, wantVersion, version, "retirement advances the generation exactly once")
			result, err := repo.GetOperation(t.Context(), sqlc.New(r.db), scope, op.ID)
			require.NoError(t, err)
			require.Equal(t, wantState, result.Targets[0].State)
		})
	}
}
