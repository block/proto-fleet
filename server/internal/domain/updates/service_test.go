package updates

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

var testPublishedAt = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

// fakeSnapshots is a mutable snapshotProvider so tests can model the checker
// picking up new releases between status calls.
type fakeSnapshots struct{ snap Snapshot }

func (f *fakeSnapshots) Snapshot() Snapshot { return f.snap }

// fakeChannelStore is an in-memory channelSettingQuerier: absent org rows
// return sql.ErrNoRows exactly like the generated sqlc query.
type fakeChannelStore struct {
	channels map[int64]string
}

func newFakeChannelStore() *fakeChannelStore {
	return &fakeChannelStore{channels: map[int64]string{}}
}

func (f *fakeChannelStore) GetReleaseChannelSetting(_ context.Context, organizationID int64) (sqlc.ReleaseChannelSetting, error) {
	channel, ok := f.channels[organizationID]
	if !ok {
		return sqlc.ReleaseChannelSetting{}, sql.ErrNoRows
	}
	return sqlc.ReleaseChannelSetting{OrganizationID: organizationID, Channel: channel}, nil
}

func (f *fakeChannelStore) UpsertReleaseChannelSetting(_ context.Context, arg sqlc.UpsertReleaseChannelSettingParams) (sqlc.ReleaseChannelSetting, error) {
	f.channels[arg.OrganizationID] = arg.Channel
	return sqlc.ReleaseChannelSetting{OrganizationID: arg.OrganizationID, Channel: arg.Channel}, nil
}

func rel(version string) *Release {
	return &Release{
		Version:     version,
		NotesURL:    "https://github.com/block/proto-fleet/releases/tag/" + version,
		PublishedAt: testPublishedAt,
	}
}

func rc(version string) *Release {
	r := rel(version)
	r.Prerelease = true
	return r
}

func newTestService(t *testing.T, current string, snapshots *fakeSnapshots, store *fakeChannelStore) (*Service, *recordingHandler) {
	t.Helper()
	h := &recordingHandler{}
	cfg := Config{DownloadBaseURL: "https://github.com/block/proto-fleet/releases/download"}
	return newService(cfg, current, snapshots, store, slog.New(h)), h
}

// On the stable channel a newer RC alone must not surface;
// the moment the snapshot gains a newer stable, that stable is offered.
func TestStatusStableChannelIgnoresRC(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable: rel("v0.2.8"),
		LatestRC:     rc("v0.2.9-rc.1"),
		FetchedAt:    testPublishedAt,
	}}
	svc, _ := newTestService(t, "v0.2.8", snaps, newFakeChannelStore())

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, ChannelStable, status.Channel, "missing row must read as the stable default")
	assert.False(t, status.UpdateAvailable)
	assert.Nil(t, status.LatestEligible)
	assert.Empty(t, status.InstallCommand)

	snaps.snap.LatestStable = rel("v0.2.9")
	status, err = svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, status.UpdateAvailable)
	require.NotNil(t, status.LatestEligible)
	assert.Equal(t, "v0.2.9", status.LatestEligible.Version)
}

// On stable_and_rc a newer RC is eligible, and an RC newer than
// the latest stable wins the semver max.
func TestStatusStableAndRCOffersNewestRC(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable: rel("v0.2.8"),
		LatestRC:     rc("v0.2.9-rc.1"),
		FetchedAt:    testPublishedAt,
	}}
	store := newFakeChannelStore()
	store.channels[1] = string(ChannelStableAndRC)
	svc, _ := newTestService(t, "v0.2.8", snaps, store)

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, ChannelStableAndRC, status.Channel)
	assert.True(t, status.UpdateAvailable)
	require.NotNil(t, status.LatestEligible)
	assert.Equal(t, "v0.2.9-rc.1", status.LatestEligible.Version)
	assert.True(t, status.LatestEligible.Prerelease)
}

// Running an RC when its stable lands must offer the stable
// on BOTH channels — semver ranks v0.2.9 above v0.2.9-rc.5, and on
// stable_and_rc the max-compare picks the stable over the RC.
func TestStatusRCPromotedToStable(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable: rel("v0.2.9"),
		LatestRC:     rc("v0.2.9-rc.5"),
		FetchedAt:    testPublishedAt,
	}}

	for _, channel := range []Channel{ChannelStable, ChannelStableAndRC} {
		store := newFakeChannelStore()
		store.channels[1] = string(channel)
		svc, _ := newTestService(t, "v0.2.9-rc.5", snaps, store)

		status, err := svc.GetUpdateStatus(context.Background(), 1)
		require.NoError(t, err)
		assert.True(t, status.UpdateAvailable, "channel %s", channel)
		require.NotNil(t, status.LatestEligible, "channel %s", channel)
		assert.Equal(t, "v0.2.9", status.LatestEligible.Version, "channel %s", channel)
	}
}

// A non-semver running version (dev builds, nightlies) never
// reports an update regardless of the snapshot, logs at most one Debug
// record, and nothing above Debug.
func TestStatusNonSemverCurrentNeverOffers(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable: rel("v9.9.9"),
		LatestRC:     rc("v9.9.10-rc.1"),
		FetchedAt:    testPublishedAt,
	}}
	store := newFakeChannelStore()
	store.channels[1] = string(ChannelStableAndRC)

	for _, current := range []string{"dev", "nightly-20260727-abc"} {
		svc, h := newTestService(t, current, snaps, store)

		status, err := svc.GetUpdateStatus(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, status.UpdateAvailable, "current %q", current)
		assert.Nil(t, status.LatestEligible, "current %q", current)
		assert.Empty(t, status.InstallCommand, "current %q", current)
		assert.Equal(t, current, status.CurrentVersion)
		assert.Empty(t, h.recordsAbove(slog.LevelDebug), "nothing above Debug on any failure path")
		assert.LessOrEqual(t, len(h.records), 1, "at most one Debug record per status call")
	}
}

// A running version newer than every published release reports no
// update on either channel.
func TestStatusCurrentNewerThanEveryRelease(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable: rel("v0.2.9"),
		LatestRC:     rc("v0.2.9-rc.5"),
		FetchedAt:    testPublishedAt,
	}}

	for _, channel := range []Channel{ChannelStable, ChannelStableAndRC} {
		store := newFakeChannelStore()
		store.channels[1] = string(channel)
		svc, _ := newTestService(t, "v0.3.0", snaps, store)

		status, err := svc.GetUpdateStatus(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, status.UpdateAvailable, "channel %s", channel)
		assert.Nil(t, status.LatestEligible, "channel %s", channel)
		assert.Empty(t, status.InstallCommand, "channel %s", channel)
	}
}

// Flipping stable_and_rc → stable with an RC pending recomputes
// on the next status call and drops the RC offer.
func TestChannelFlipDropsPendingRC(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable: rel("v0.2.8"),
		LatestRC:     rc("v0.2.9-rc.1"),
		FetchedAt:    testPublishedAt,
	}}
	store := newFakeChannelStore()
	svc, _ := newTestService(t, "v0.2.8", snaps, store)

	require.NoError(t, svc.SetReleaseChannel(context.Background(), 1, ChannelStableAndRC))
	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, status.UpdateAvailable, "test needs a pending RC offer")
	assert.Equal(t, "v0.2.9-rc.1", status.LatestEligible.Version)

	require.NoError(t, svc.SetReleaseChannel(context.Background(), 1, ChannelStable))
	status, err = svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, ChannelStable, status.Channel)
	assert.False(t, status.UpdateAvailable)
	assert.Nil(t, status.LatestEligible)
	assert.Empty(t, status.InstallCommand)
}

// Install_command matches the exact template for the eligible tag.
func TestInstallCommandExactTemplate(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable: rel("v0.2.9"),
		FetchedAt:    testPublishedAt,
	}}
	svc, _ := newTestService(t, "v0.2.8", snaps, newFakeChannelStore())

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, status.UpdateAvailable)
	assert.Equal(t,
		`bash <(curl -fsSL "https://github.com/block/proto-fleet/releases/download/v0.2.9/install.sh") v0.2.9`,
		status.InstallCommand)
}

// Before the first successful fetch (or with no releases) the
// snapshot is empty — no update, no eligible release, no command, on both
// channels.
func TestStatusEmptySnapshot(t *testing.T) {
	t.Parallel()

	for _, channel := range []Channel{ChannelStable, ChannelStableAndRC} {
		store := newFakeChannelStore()
		store.channels[1] = string(channel)
		svc, h := newTestService(t, "v0.2.8", &fakeSnapshots{}, store)

		status, err := svc.GetUpdateStatus(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, status.UpdateAvailable, "channel %s", channel)
		assert.Nil(t, status.LatestEligible, "channel %s", channel)
		assert.Empty(t, status.InstallCommand, "channel %s", channel)
		assert.Empty(t, h.recordsAbove(slog.LevelDebug))
	}
}

// SetReleaseChannel persists per org and the next status call
// reads it back; an unknown channel value is rejected as invalid argument
// before touching storage.
func TestSetReleaseChannelPersists(t *testing.T) {
	t.Parallel()

	store := newFakeChannelStore()
	svc, _ := newTestService(t, "v0.2.8", &fakeSnapshots{}, store)

	require.NoError(t, svc.SetReleaseChannel(context.Background(), 7, ChannelStableAndRC))
	assert.Equal(t, string(ChannelStableAndRC), store.channels[7])

	status, err := svc.GetUpdateStatus(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, ChannelStableAndRC, status.Channel)

	err = svc.SetReleaseChannel(context.Background(), 7, Channel("weekly"))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Equal(t, string(ChannelStableAndRC), store.channels[7], "a rejected channel must not overwrite the stored one")
}

// A candidate that fails semver.IsValid must never
// yield an install command, and update_available stays false — even though
// the checker should never cache such a release.
func TestNonSemverCandidateNeverOffered(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		// "0.3.0" (no leading v) is invalid for golang.org/x/mod/semver.
		LatestStable: rel("0.3.0"),
		LatestRC:     rc("nightly-20260727-abc"),
		FetchedAt:    testPublishedAt,
	}}
	for _, channel := range []Channel{ChannelStable, ChannelStableAndRC} {
		store := newFakeChannelStore()
		store.channels[1] = string(channel)
		svc, h := newTestService(t, "v0.2.8", snaps, store)

		status, err := svc.GetUpdateStatus(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, status.UpdateAvailable, "channel %s", channel)
		assert.Empty(t, status.InstallCommand, "channel %s", channel)
		assert.Empty(t, h.recordsAbove(slog.LevelDebug))
	}

	cmd, ok := installCommand("https://example.com/dl", "0.3.0")
	assert.False(t, ok, "installCommand must refuse a non-semver tag")
	assert.Empty(t, cmd)
}

func TestCurrentVersion(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t, "v1.2.3", &fakeSnapshots{}, newFakeChannelStore())
	assert.Equal(t, "v1.2.3", svc.CurrentVersion())
}
