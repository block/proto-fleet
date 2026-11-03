package mininggroups

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations/migration"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestImportRetainsCanonicalBoardIntentAndDistinctControllerGroups(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	initialRevision, boards := preparePermissionPoolAssignment(t, ctx, f)
	password := "retained-controller-password"
	slots, err := poolconfigurations.EncryptSlots(f.service.secrets, []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker", Password: &password}}, nil)
	require.NoError(t, err)
	firstRevision := uuid.NewString()
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots)
 SELECT $2,organization_id,account_id,pool_id,2,name,$3 FROM hosted_account_pool_revision WHERE revision_id=$1`, initialRevision, firstRevision, slots)
	require.NoError(t, err)
	var root int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT account_id FROM hosted_account_pool_revision WHERE revision_id=$1`, firstRevision).Scan(&root))
	secondPool, secondRevision := uuid.NewString(), uuid.NewString()
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, secondPool, f.target.OrganizationID, root)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) VALUES($1,$2,$3,$4,1,'Duplicate contents',$5)`, secondRevision, f.target.OrganizationID, root, secondPool, slots)
	require.NoError(t, err)
	var secondBoard int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&secondBoard))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata)
 SELECT $1,module_id,2,now(),now(),'{"macAddress":"02:00:00:00:00:02"}' FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, secondBoard, boards[0])
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at)
 SELECT module_id,$1,2,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, secondBoard)
	require.NoError(t, err)
	publishSharedBoardInventory(t, ctx, f)
	boards = append(boards, secondBoard)
	macs := []string{"02:00:00:00:00:01", "02:00:00:00:00:02"}
	for index, revision := range []string{firstRevision, secondRevision} {
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_pool_intent(organization_id,hashboard_id,device_id,installation_id,mac_address,account_id,pool_revision_id,operation_id)
 SELECT $1,hashboard_id,device_id,id,$3,$4,$5,$6 FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, f.target.OrganizationID, boards[index], macs[index], root, revision, uuid.New())
		require.NoError(t, err)
	}
	row := f.state(t)
	row.Bindings, err = encode(map[string]binding{
		"retained-a": {ConfigID: firstRevision, Configured: true},
		"retained-b": {ConfigID: secondRevision, Configured: true},
	})
	require.NoError(t, err)
	require.NoError(t, save(ctx, f.service.queries, row))
	observed := sdk.MiningGroupResult{Supported: true, Enabled: true, MaxGroups: 4, Groups: []sdk.MiningGroup{
		{ID: "default", Revision: 1, Mining: true},
		{ID: "retained-a", Revision: 7, Mining: true, MACs: macs[:1], Pools: []sdk.MiningGroupPool{{URL: "stratum+tcp://pool.example:3333", Username: "worker", Enabled: true}}},
		{ID: "retained-b", Revision: 11, Mining: true, MACs: macs[1:], Pools: []sdk.MiningGroupPool{{URL: "stratum+tcp://pool.example:3333", Username: "worker", Enabled: true}}},
	}}
	before, err := f.service.plannerSnapshot(ctx, f.service.queries, row, observed)
	require.NoError(t, err)
	step, err := Plan(map[string]string{macs[0]: firstRevision, macs[1]: secondRevision}, before)
	require.NoError(t, err)
	require.Equal(t, Done, step.Kind, "the source state already matches the controller")
	controllerCalls := len(f.miner.calls)
	var originalIntents []sqlc.ListPoolGroupIntentsRow
	var originalBindings []byte
	for run := range 2 {
		report, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
			return migration.Import(ctx, q, f.service.secrets, f.target.OrganizationID)
		})
		require.NoError(t, err)
		require.Empty(t, report.Unresolved)
		require.Len(t, report.AdoptedAssignments, 2)
		require.Equal(t, report.AdoptedAssignments[0].ConfigurationID, report.AdoptedAssignments[1].ConfigurationID, "the user-facing library may deduplicate identical settings")
		intents, err := f.service.queries.ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
		require.NoError(t, err)
		require.Len(t, intents, 2, "every adopted board needs canonical execution intent without a new operation")
		row = f.state(t)
		bindings := map[string]binding{}
		require.NoError(t, decode(row.Bindings, &bindings))
		require.Len(t, bindings, 2)
		require.NotEqual(t, bindings["retained-a"].ConfigID, bindings["retained-b"].ConfigID, "deduplicating settings must preserve separate controller group identities")
		desired := map[string]string{}
		for index, intent := range intents {
			require.Equal(t, boards[index], intent.HashboardID)
			require.False(t, intent.PhysicalAttemptID.Valid, "adoption must not fabricate a hardware attempt")
			require.True(t, intent.GroupSnapshotID.Valid)
			group := []string{"retained-a", "retained-b"}[index]
			require.Equal(t, bindings[group].ConfigID, intent.GroupSnapshotID.String)
			require.True(t, bindings[group].Configured)
			snapshot, err := f.service.queries.GetPoolGroupSnapshot(ctx, sqlc.GetPoolGroupSnapshotParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID, SnapshotID: intent.GroupSnapshotID.String})
			require.NoError(t, err, "retained controller bindings must resolve to immutable execution material")
			stored, err := poolconfigurations.DecodeSlots(snapshot.Slots)
			require.NoError(t, err)
			require.Len(t, stored, 1)
			secret, err := f.service.secrets.Decrypt(stored[0].PasswordCiphertext)
			require.NoError(t, err)
			require.Equal(t, password, string(secret))
			desired[intent.MacAddress] = intent.GroupSnapshotID.String
		}
		after, err := f.service.plannerSnapshot(ctx, f.service.queries, row, observed)
		require.NoError(t, err)
		step, err = Plan(desired, after)
		require.NoError(t, err)
		require.Equal(t, Done, step.Kind, "import must not configure, start, or regroup already applied boards")
		if run == 0 {
			originalIntents, originalBindings = intents, row.Bindings
		} else {
			require.Zero(t, report.CreatedConfigurations)
			require.Equal(t, originalIntents, intents, "reruns preserve execution identities")
			require.JSONEq(t, string(originalBindings), string(row.Bindings))
		}
	}
	var attempts, queued, snapshots int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.target.DeviceID).Scan(&attempts))
	require.Zero(t, attempts)
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message WHERE device_id=$1`, f.target.DeviceID).Scan(&queued))
	require.Zero(t, queued)
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_group_snapshot WHERE device_id=$1`, f.target.DeviceID).Scan(&snapshots))
	require.Equal(t, 2, snapshots, "reruns must not accumulate redundant execution snapshots")
	work, err := f.service.queries.ListPendingPoolConfigurationWork(ctx)
	require.NoError(t, err)
	require.Empty(t, work)
	require.Len(t, f.miner.calls, controllerCalls, "import must not contact the controller")
	// The final marker selects the canonical runtime. Deny every legacy query to
	// prove subsequent planning/cleanup does not silently depend on old storage.
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		if err := q.FreezePoolConfigurationWriters(ctx, f.target.OrganizationID); err != nil {
			return err
		}
		_, err := migration.CompleteCutover(ctx, q, f.target.OrganizationID)
		return err
	}))
	canonicalQueries := sqlc.NewRetryingQuerier(f.service.queries, canonicalQueryGuard{})
	desired, err := f.service.liveIntent(ctx, canonicalQueries, f.state(t), nil)
	require.NoError(t, err)
	require.Len(t, desired, 2)
	after, err := f.service.plannerSnapshot(ctx, canonicalQueries, f.state(t), observed)
	require.NoError(t, err)
	step, err = Plan(desired, after)
	require.NoError(t, err)
	require.Equal(t, Done, step.Kind)
	detached, err := hasDetachedIntents(ctx, canonicalQueries, f.target.OrganizationID, f.target.DeviceID)
	require.NoError(t, err)
	require.False(t, detached)
	require.NoError(t, pruneDetachedIntents(ctx, canonicalQueries, f.target.OrganizationID, f.target.DeviceID))

}

type canonicalQueryGuard struct{}

func (canonicalQueryGuard) RetryQuery(_ context.Context, name string, call func() error) error {
	if strings.Contains(name, "HashboardPoolIntent") || strings.Contains(name, "HostMiningGroupPoolRevision") {
		return fmt.Errorf("legacy runtime query after cutover: %s", name)
	}
	return call()
}
