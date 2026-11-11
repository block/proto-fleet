package hostedaccounts_test

import (
	"errors"
	"testing"

	"connectrpc.com/authn"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWholeContainerPoolStatusAppearsOnEveryBoard(t *testing.T) {
	for _, operator := range []bool{false, true} {
		for _, clear := range []bool{false, true} {
			name := map[bool]string{false: "client", true: "operator"}[operator] + "/" + map[bool]string{false: "apply", true: "clear"}[clear]
			t.Run(name, func(t *testing.T) {
				f := newPoolStepUpFixture(t)
				_, _, _, cipher, ctx := poolFixture(t, f.fixture)
				repo := poolconfigurations.NewRepository(cipher)
				scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}
				accountID := f.account.AccountID
				if operator {
					scope.AccountID = 0
					accountID = ""
					ctx = authn.SetInfo(ctx, &session.Info{UserID: f.host, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
				}
				var err error
				if !operator {
					ctx, err = hostedaccounts.WithRequestAccountID(ctx, accountID)
					require.NoError(t, err)
				}
				var config poolconfigurations.Configuration
				if !clear {
					config, err = db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
						return repo.Create(ctx, q, scope, poolconfigurations.RevisionOptions{Name: "Default", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "owner"}}})
					})
					require.NoError(t, err)
				}
				module, controller := f.miningModule(t)
				_, err = f.db.ExecContext(ctx, `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','02:00:00:00:00:0'||position) WHERE device_id=$1`, controller)
				require.NoError(t, err)
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
				if operator {
					require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(6, module, 1)))
				}
				reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
				device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}}
				if clear {
					device.onWrite = func() { device.poolDevice.readError = errors.New("verification unavailable") }
				}
				getter := poolMinerGetter{device}
				checker := poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)
				worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher).WithRemovals(checker)
				service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer)
				var id uuid.UUID
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='ownership' AND owner_account_id=$2 AND state='pending' ORDER BY created_at LIMIT 1`, controller, scope.AccountID).Scan(&id))
				require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
				q := sqlc.New(f.db)
				event, err := q.GetPoolLifecycleTransition(ctx, id)
				require.NoError(t, err)
				require.Equal(t, "device", event.TargetKind)
				operationID := event.OperationID.String
				if clear {
					operationID = id.String()
				}
				assertBoards := func(state string) {
					t.Helper()
					got, err := service.GetBoardAssignments(ctx, accountID, poolconfigurations.BoardSelection{ModuleIDs: []int64{module}})
					require.NoError(t, err)
					require.Len(t, got.Assignments, 3)
					require.EqualValues(t, 3, got.SelectedCount)
					for _, a := range got.Assignments {
						require.Positive(t, a.InstallationID)
						require.Equal(t, operationID, a.OperationID, "every board must link to the whole-container operation")
						require.Equal(t, config.ID, a.ConfigurationID)
						require.Equal(t, state, a.State)
					}
				}
				if clear {
					assertBoards("uncertain")
					device.poolDevice.readError = nil
					require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
					assertBoards("cleared")
				} else {
					assertBoards("pending")
					require.NoError(t, worker.Execute(t.Context(), f.org, operationID, poolconfigurations.AssignmentTarget{Kind: "device", ID: controller, DeviceID: controller}))
					assertBoards("verified")
				}
				require.Equal(t, 1, device.writes)
				// The other workspace must not see this owner's container policy.
				otherCtx := authn.SetInfo(t.Context(), &session.Info{UserID: f.host, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
				otherAccount := ""
				if operator {
					otherCtx = f.sessionContext(t)
					otherAccount = f.account.AccountID
					otherCtx, err = hostedaccounts.WithRequestAccountID(otherCtx, otherAccount)
					require.NoError(t, err)
				}
				hidden, err := service.GetBoardAssignments(otherCtx, otherAccount, poolconfigurations.BoardSelection{ModuleIDs: []int64{module}})
				require.NoError(t, err)
				require.Empty(t, hidden.Assignments)
				if !clear {
					// A later board-specific assignment must replace container status.
					_, err = f.db.ExecContext(ctx, `UPDATE container_mining_group_state SET observed='{"supported":true,"enabled":true}' WHERE device_id=$1`, controller)
					require.NoError(t, err)
					var installation int64
					require.NoError(t, f.db.QueryRowContext(ctx, `SELECT id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, module).Scan(&installation))
					direct, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
						current, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: "device", TargetID: controller})
						if err != nil {
							return poolconfigurations.Operation{}, err
						}
						if err := repo.RetireAssignment(ctx, q, scope, poolconfigurations.AssignmentTarget{Kind: "device", ID: controller, DeviceID: controller}, current.Version); err != nil {
							return poolconfigurations.Operation{}, err
						}
						return repo.Assign(ctx, q, scope, f.host, config.ID, config.RevisionID, []poolconfigurations.AssignmentChange{{Target: poolconfigurations.AssignmentTarget{Kind: "hashboard_installation", ID: installation, DeviceID: controller}}})
					})
					require.NoError(t, err)
					got, err := service.GetBoardAssignments(ctx, accountID, poolconfigurations.BoardSelection{ModuleIDs: []int64{module}})
					require.NoError(t, err)
					require.Len(t, got.Assignments, 3)
					for _, a := range got.Assignments {
						if a.InstallationID == installation {
							require.Equal(t, direct.ID, a.OperationID)
						} else {
							require.Empty(t, a.OperationID, "enabled groups must not inherit obsolete container work")
						}
					}
				}
			})
		}
	}
}
