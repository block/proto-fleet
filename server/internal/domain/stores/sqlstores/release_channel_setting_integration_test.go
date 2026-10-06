package sqlstores_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// Exercises the release_channel_setting sqlc queries end to end against a
// migrated database: the missing-row read (the service layer maps
// sql.ErrNoRows to the 'stable' default), the insert and overwrite paths of
// the upsert, and the CHECK constraint on channel values.
func TestReleaseChannelSettingQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	q := sqlc.New(db)
	ctx := context.Background()

	orgID := seedOrg(t, db, "release-channel-org")

	// No row yet: callers see sql.ErrNoRows, not a default row.
	_, err := q.GetReleaseChannelSetting(ctx, orgID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	// First upsert inserts.
	created, err := q.UpsertReleaseChannelSetting(ctx, sqlc.UpsertReleaseChannelSettingParams{
		OrganizationID: orgID,
		Channel:        "stable_and_rc",
	})
	require.NoError(t, err)
	assert.Equal(t, orgID, created.OrganizationID)
	assert.Equal(t, "stable_and_rc", created.Channel)

	got, err := q.GetReleaseChannelSetting(ctx, orgID)
	require.NoError(t, err)
	assert.Equal(t, "stable_and_rc", got.Channel)

	// Second upsert overwrites in place and bumps updated_at.
	overwritten, err := q.UpsertReleaseChannelSetting(ctx, sqlc.UpsertReleaseChannelSettingParams{
		OrganizationID: orgID,
		Channel:        "stable",
	})
	require.NoError(t, err)
	assert.Equal(t, created.OrganizationID, overwritten.OrganizationID)
	assert.Equal(t, "stable", overwritten.Channel)
	assert.True(t, overwritten.UpdatedAt.After(created.UpdatedAt),
		"overwrite must bump updated_at")

	got, err = q.GetReleaseChannelSetting(ctx, orgID)
	require.NoError(t, err)
	assert.Equal(t, created.OrganizationID, got.OrganizationID)
	assert.Equal(t, "stable", got.Channel)

	// The CHECK constraint rejects anything outside the known channels.
	_, err = q.UpsertReleaseChannelSetting(ctx, sqlc.UpsertReleaseChannelSettingParams{
		OrganizationID: orgID,
		Channel:        "nightly",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "ck_release_channel_setting_channel")
}
