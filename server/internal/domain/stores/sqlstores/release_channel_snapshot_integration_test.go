package sqlstores_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/dbtypes"
)

func TestFirmwareRolloutBehaviorSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channelID := f.channel("snapshot")
	zero, coverage, errors := 0.0, 90.0, int32(0)
	want := dbtypes.RolloutBehaviorSnapshot{
		Method: "pilot_then_continue", OrderBy: "random", PilotSize: 3,
		ReviewAfterEachBatch: true, AutoContinue: true, StabilizationSeconds: 600,
		MaxHashrateDropPercent: &zero, MaxNewErrors: &errors,
		MinSampleCoveragePercent: &coverage, MaxConcurrentOffline: 2,
	}
	created, err := f.q.CreateFirmwareRollout(t.Context(), sqlc.CreateFirmwareRolloutParams{
		OrgID: f.org, ChannelID: channelID, Manufacturer: "Test", Model: "Rig",
		FirmwareChecksum: "snapshot-sum", FirmwareVersion: "v2", AssignmentGeneration: 1,
		Stage: "batch", BatchCount: 1, ActorType: "system", BehaviorSnapshot: want,
	})
	require.NoError(t, err)
	require.Equal(t, want, created.BehaviorSnapshot)

	// Editing channel behavior must not rewrite the in-flight configuration.
	// Dispatch still sees the channel's current budget, independently of the
	// historical budget returned in the behavior snapshot.
	f.exec(`UPDATE release_channel SET method = 'batched', batch_size = 8, max_concurrent_offline = 9 WHERE id = $1`, channelID)
	loaded, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{OrgID: f.org, RolloutID: created.ID})
	require.NoError(t, err)
	require.Equal(t, want, loaded.BehaviorSnapshot)
	active, err := f.q.ListActiveFirmwareRollouts(t.Context())
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, want, active[0].FirmwareRollout.BehaviorSnapshot)
	require.Equal(t, int32(9), active[0].ChannelMaxConcurrentOffline)
}

func TestReleaseChannelQueryProviderUsesTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	provider := sqlstores.NewSQLConnectionManager(f.db)
	rollback := errors.New("rollback test mutation")
	var channelID int64
	err := sqlstores.NewSQLTransactor(f.db).RunInTx(t.Context(), func(ctx context.Context) error {
		q := provider.GetQueries(ctx)
		created, err := q.CreateReleaseChannel(ctx, sqlc.CreateReleaseChannelParams{
			OrgID: f.org, Name: "must roll back", CreatedBy: 1,
			Method: "all_at_once", OrderBy: "least_efficient_first",
		})
		if err != nil {
			return err
		}
		channelID = created.ID
		_, err = q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{OrgID: f.org, ChannelID: channelID})
		require.NoError(t, err, "the transaction can read its own uncommitted insert")
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	_, err = provider.GetQueries(t.Context()).GetReleaseChannel(t.Context(), sqlc.GetReleaseChannelParams{OrgID: f.org, ChannelID: channelID})
	require.ErrorIs(t, err, sql.ErrNoRows, "a pool-bound querier would have leaked the insert")
}
