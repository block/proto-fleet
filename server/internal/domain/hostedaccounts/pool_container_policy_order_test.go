package hostedaccounts_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWholeContainerPreparationUsesNewestCapturedPolicy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		firstDefault bool
	}{
		{name: "clear then default"},
		{name: "default then different default", firstDefault: true},
	} {
		for _, newestFirst := range []bool{false, true} {
			name := tc.name + "/" + map[bool]string{false: "oldest requested", true: "newest requested"}[newestFirst]
			t.Run(name, func(t *testing.T) {
				f := newPoolStepUpFixture(t)
				_, _, _, cipher, _ := poolFixture(t, f.fixture)
				repo := poolconfigurations.NewRepository(cipher)
				scope := poolconfigurations.Scope{OrganizationID: f.org}
				module, controller := f.miningModule(t)
				_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','02:00:00:00:00:0'||position) WHERE device_id=$1`, controller)
				require.NoError(t, err)
				setDefault := func(name string) poolconfigurations.Configuration {
					t.Helper()
					c, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
						c, err := repo.Create(t.Context(), q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
						if err != nil {
							return c, err
						}
						return c, repo.SetDefault(t.Context(), q, scope, c.ID, c.Version)
					})
					require.NoError(t, err)
					return c
				}
				capture := func(position int) uuid.UUID {
					t.Helper()
					var id uuid.UUID
					require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,configuration_id,revision_id)
 SELECT $1,$2,7,i.hashboard_id,'ownership',c.configuration_id,c.current_revision_id FROM hashboard_installation i
 LEFT JOIN pool_configuration c ON c.organization_id=$1 AND c.account_id IS NULL AND c.is_default AND NOT c.archived
 WHERE i.module_id=$3 AND i.position=$4 AND i.removed_at IS NULL RETURNING transition_id`, f.org, controller, module, position).Scan(&id))
					return id
				}
				if tc.firstDefault {
					setDefault("first")
				}
				older := capture(1)
				expected := setDefault("second")
				newest := capture(2)
				// The chosen event's capture wins, not whatever default exists at execution.
				setDefault("third")
				reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
				device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}}
				getter := poolMinerGetter{device}
				checker := poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)
				worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher).WithRemovals(checker)
				requested := older
				if newestFirst {
					requested = newest
				}
				require.NoError(t, worker.ProcessLifecycle(t.Context(), requested))
				q := sqlc.New(f.db)
				old, err := q.GetPoolLifecycleTransition(t.Context(), older)
				require.NoError(t, err)
				require.Equal(t, "superseded", old.State)
				require.False(t, old.OperationID.Valid)
				require.False(t, old.PhysicalAttemptID.Valid)
				latest, err := q.GetPoolLifecycleTransition(t.Context(), newest)
				require.NoError(t, err)
				require.Equal(t, expected.ID, latest.ConfigurationID.String)
				require.Equal(t, poolconfigurations.TargetDevice, latest.TargetKind)
				require.Equal(t, "applying", latest.State)
				require.Zero(t, device.writes, "older clear must not dispatch before the newer default")
				require.NoError(t, worker.Execute(t.Context(), f.org, latest.OperationID.String, poolconfigurations.AssignmentTarget{Kind: latest.TargetKind, ID: latest.TargetID, DeviceID: controller}))
				require.Equal(t, "second", device.pools[0].Username)
				require.Equal(t, 1, device.writes)
				require.NoError(t, worker.ProcessLifecycle(t.Context(), older))
				require.NoError(t, worker.ProcessLifecycle(t.Context(), newest))
				require.Equal(t, 1, device.writes)
			})
		}
	}
}

func TestContainerPolicySelectionPreservesIneligibleAndNewerWork(t *testing.T) {
	f := newPoolStepUpFixture(t)
	module, controller := f.miningModule(t)
	var board int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=1`, module).Scan(&board))
	q := sqlc.New(f.db)
	capture := func(state string, revision int64, reason string, cycle any, asset int64, operation any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state,allocation_revision,pairing_cycle_id,operation_id)
 VALUES($1,$2,7,$3,$4,$5,$6,$7,$8) RETURNING transition_id`, f.org, controller, asset, reason, state, revision, cycle, operation).Scan(&id))
		return id
	}
	accepted := capture("ready", 0, "ownership", nil, board, nil)
	linked := capture("pending", 0, "ownership", nil, board, uuid.NewString())
	older := capture("pending", 0, "ownership", nil, board, nil)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	config, err := poolconfigurations.NewRepository(cipher).Create(t.Context(), q, poolconfigurations.Scope{OrganizationID: f.org}, poolOptions(nil))
	require.NoError(t, err)
	var olderApply uuid.UUID
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,configuration_id,revision_id)
 VALUES($1,$2,7,$3,'ownership',$4,$5) RETURNING transition_id`, f.org, controller, board, config.ID, config.RevisionID).Scan(&olderApply))
	selected := capture("pending", 0, "ownership", nil, board, nil)
	// Neither stale ownership nor an absent installation can displace valid work.
	capture("pending", 1, "ownership", nil, board, nil)
	capture("pending", 0, "ownership", nil, 0, nil)
	// Pairing without a default requests no change, and ended cycles are obsolete.
	var cycle uuid.UUID
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT cycle_id FROM pool_pairing_cycle WHERE device_id=$1`, controller).Scan(&cycle))
	capture("pending", 0, "pairing", cycle, board, nil)
	capture("pending", 0, "pairing", uuid.New(), board, nil)
	capture("ready", 0, "ownership", nil, board, nil)
	capture("pending", 0, "ownership", nil, board, uuid.NewString())
	got, err := q.GetPreferredContainerPoolPolicy(t.Context(), older)
	require.NoError(t, err)
	require.Equal(t, selected, got)
	// A capture arriving after selection must survive the supersession update.
	newer := capture("pending", 0, "ownership", nil, board, nil)
	require.NoError(t, q.SupersedeContainerPoolLifecycleTransitions(t.Context(), sqlc.SupersedeContainerPoolLifecycleTransitionsParams{
		OrganizationID: f.org, DeviceID: controller, TransitionID: selected,
	}))
	for _, tc := range []struct {
		id    uuid.UUID
		state string
	}{
		{older, "pending"}, {olderApply, "superseded"}, {selected, "pending"}, {newer, "pending"}, {accepted, "ready"}, {linked, "pending"},
	} {
		row, err := q.GetPoolLifecycleTransition(t.Context(), tc.id)
		require.NoError(t, err)
		require.Equal(t, tc.state, row.State)
	}
}
