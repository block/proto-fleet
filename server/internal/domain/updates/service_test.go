package updates

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/admissionctx"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

var testPublishedAt = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

// fakeSnapshots is a mutable snapshotProvider so tests can model the checker
// picking up new releases between status calls.
type fakeSnapshots struct{ snap Snapshot }

func (f *fakeSnapshots) Snapshot() Snapshot { return f.snap }

type fakeExecutor struct {
	status       updaterapi.StatusResponse
	statusErr    error
	statusFunc   func(context.Context) (updaterapi.StatusResponse, error)
	operationIDs []string
	triggered    []string
	trigger      updaterapi.Operation
	triggerErr   error
	triggerFunc  func(context.Context, string, string) (updaterapi.Operation, error)
}

func (f *fakeExecutor) Status(ctx context.Context) (updaterapi.StatusResponse, error) {
	if f.statusFunc != nil {
		return f.statusFunc(ctx)
	}
	return f.status, f.statusErr
}

func (f *fakeExecutor) Trigger(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, error) {
	f.operationIDs = append(f.operationIDs, operationID)
	f.triggered = append(f.triggered, targetVersion)
	if f.triggerFunc != nil {
		return f.triggerFunc(ctx, operationID, targetVersion)
	}
	return f.trigger, f.triggerErr
}

// fakeChannelStore is an in-memory channelSettingQuerier: absent org rows
// return sql.ErrNoRows exactly like the generated sqlc query. getErr and
// upsertErr, when set, model a transient database failure.
type fakeChannelStore struct {
	channels  map[int64]string
	getErr    error
	upsertErr error
}

func newFakeChannelStore() *fakeChannelStore {
	return &fakeChannelStore{channels: map[int64]string{}}
}

func (f *fakeChannelStore) GetReleaseChannelSetting(_ context.Context, organizationID int64) (sqlc.ReleaseChannelSetting, error) {
	if f.getErr != nil {
		return sqlc.ReleaseChannelSetting{}, f.getErr
	}
	channel, ok := f.channels[organizationID]
	if !ok {
		return sqlc.ReleaseChannelSetting{}, sql.ErrNoRows
	}
	return sqlc.ReleaseChannelSetting{OrganizationID: organizationID, Channel: channel}, nil
}

func (f *fakeChannelStore) UpsertReleaseChannelSetting(_ context.Context, arg sqlc.UpsertReleaseChannelSettingParams) (sqlc.ReleaseChannelSetting, error) {
	if f.upsertErr != nil {
		return sqlc.ReleaseChannelSetting{}, f.upsertErr
	}
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
		LatestStable:    rel("v0.2.8"),
		LatestRC:        rc("v0.2.9-rc.1"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
	}}
	svc, _ := newTestService(t, "v0.2.8", snaps, newFakeChannelStore())

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, ChannelStable, status.Channel, "missing row must read as the stable default")
	assert.True(t, status.StatusAvailable)
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
		LatestStable:    rel("v0.2.8"),
		LatestRC:        rc("v0.2.9-rc.1"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
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

func TestStatusRCOnlySnapshotRemainsAvailableForStableAndRC(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v9.0.0"), // cached data whose revalidation failed
		LatestRC:        rc("v0.2.9-rc.1"),
		FetchedAt:       testPublishedAt,
		StableAvailable: false,
		RCAvailable:     true,
	}}

	stableStore := newFakeChannelStore()
	stableSvc, _ := newTestService(t, "v0.2.8", snaps, stableStore)
	stableStatus, err := stableSvc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, stableStatus.StatusAvailable)
	assert.False(t, stableStatus.UpdateAvailable)
	assert.Nil(t, stableStatus.LatestEligible)

	rcStore := newFakeChannelStore()
	rcStore.channels[1] = string(ChannelStableAndRC)
	rcSvc, _ := newTestService(t, "v0.2.8", snaps, rcStore)
	rcStatus, err := rcSvc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, rcStatus.StatusAvailable)
	assert.True(t, rcStatus.UpdateAvailable)
	require.NotNil(t, rcStatus.LatestEligible)
	assert.Equal(t, "v0.2.9-rc.1", rcStatus.LatestEligible.Version,
		"the unverified cached stable must not outrank the fresh RC")
}

func TestStatusRCOnlySnapshotCannotClaimUpToDate(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v9.0.0"), // cached data whose revalidation failed
		LatestRC:        rc("v0.2.9-rc.1"),
		FetchedAt:       testPublishedAt,
		StableAvailable: false,
		RCAvailable:     true,
	}}
	store := newFakeChannelStore()
	store.channels[1] = string(ChannelStableAndRC)
	svc, _ := newTestService(t, "v0.2.9", snaps, store)

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, status.StatusAvailable,
		"an older RC cannot prove the instance is current while stable discovery is unavailable")
	assert.False(t, status.UpdateAvailable)
	assert.Nil(t, status.LatestEligible)
	assert.Empty(t, status.InstallCommand)
}

func TestIncompleteRCViewOnlySuppressesStableAndRCChannel(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v0.2.9"),
		LatestRC:        rc("v9.0.0-rc.1"), // cached data whose revalidation failed
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     false,
	}}

	stableSvc, _ := newTestService(t, "v0.2.8", snaps, newFakeChannelStore())
	stableStatus, err := stableSvc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, stableStatus.StatusAvailable)
	assert.True(t, stableStatus.UpdateAvailable)
	require.NotNil(t, stableStatus.LatestEligible)
	assert.Equal(t, "v0.2.9", stableStatus.LatestEligible.Version)

	rcStore := newFakeChannelStore()
	rcStore.channels[1] = string(ChannelStableAndRC)
	rcSvc, _ := newTestService(t, "v0.2.8", snaps, rcStore)
	rcStatus, err := rcSvc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, rcStatus.StatusAvailable)
	assert.False(t, rcStatus.UpdateAvailable)
	assert.Nil(t, rcStatus.LatestEligible)
}

// Running an RC when its stable lands must offer the stable
// on BOTH channels — semver ranks v0.2.9 above v0.2.9-rc.5, and on
// stable_and_rc the max-compare picks the stable over the RC.
func TestStatusRCPromotedToStable(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v0.2.9"),
		LatestRC:        rc("v0.2.9-rc.5"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
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
		LatestStable:    rel("v9.9.9"),
		LatestRC:        rc("v9.9.10-rc.1"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
	}}
	store := newFakeChannelStore()
	store.channels[1] = string(ChannelStableAndRC)

	for _, current := range []string{"dev", "nightly-20260727-abc"} {
		svc, h := newTestService(t, current, snaps, store)

		status, err := svc.GetUpdateStatus(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, status.StatusAvailable, "current %q", current)
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
		LatestStable:    rel("v0.2.9"),
		LatestRC:        rc("v0.2.9-rc.5"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
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
		LatestStable:    rel("v0.2.8"),
		LatestRC:        rc("v0.2.9-rc.1"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
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
		LatestStable:    rel("v0.2.9"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
	}}
	svc, _ := newTestService(t, "v0.2.8", snaps, newFakeChannelStore())

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, status.UpdateAvailable)
	assert.Equal(t,
		`bash <(curl -fsSL "https://github.com/block/proto-fleet/releases/download/v0.2.9/install.sh") v0.2.9`,
		status.InstallCommand)
}

// Config validation is the first line of defense, but command composition
// must also reject an attacker-controlled base if a caller bypasses startup.
func TestInstallCommandRejectsUntrustedDownloadBase(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"https://example.com/releases/download",
		"https://github.com/block/proto-fleet/releases/download/$(touch /tmp/pwned)",
		"https://github.com/block/proto-fleet/releases/download/`touch /tmp/pwned`",
		`https://github.com/block/proto-fleet/releases/download/"quoted"`,
		`https://github.com/block/proto-fleet/releases/download/\backslash`,
		"https://github.com/block/proto-fleet/releases/download/with space",
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			command, ok := installCommand(value, "v0.2.9")
			assert.False(t, ok)
			assert.Empty(t, command)
		})
	}
}

func TestInstallCommandRequiresCanonicalReleaseTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag  string
		want bool
	}{
		{tag: "v1.2.3", want: true},
		{tag: "v1.2.3-rc.1", want: true},
		{tag: "v1"},
		{tag: "v1.2"},
		{tag: "v1.2.3+meta"},
		{tag: "v01.2.3"},
		{tag: "v1.2.3-rc.01"},
		{tag: "v1.2.3-pr.1"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			t.Parallel()
			command, ok := installCommand(downloadBaseURL, tt.tag)
			assert.Equal(t, tt.want, ok)
			if tt.want {
				assert.NotEmpty(t, command)
			} else {
				assert.Empty(t, command)
			}
		})
	}
}

// Before the first successful fetch, status is explicitly unavailable rather
// than indistinguishable from a successful up-to-date result.
func TestStatusUnavailableBeforeFirstSuccessfulFetch(t *testing.T) {
	t.Parallel()

	for _, channel := range []Channel{ChannelStable, ChannelStableAndRC} {
		store := newFakeChannelStore()
		store.channels[1] = string(channel)
		svc, h := newTestService(t, "v0.2.8", &fakeSnapshots{}, store)

		status, err := svc.GetUpdateStatus(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, status.StatusAvailable, "channel %s", channel)
		assert.False(t, status.UpdateAvailable, "channel %s", channel)
		assert.Nil(t, status.LatestEligible, "channel %s", channel)
		assert.Empty(t, status.InstallCommand, "channel %s", channel)
		assert.Empty(t, h.recordsAbove(slog.LevelDebug))
	}
}

func TestStatusAvailableAfterSuccessfulEmptyFetch(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
	}}
	svc, _ := newTestService(t, "v0.2.8", snaps, newFakeChannelStore())

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, status.StatusAvailable)
	assert.False(t, status.UpdateAvailable)
	assert.Nil(t, status.LatestEligible)
	assert.Empty(t, status.InstallCommand)
}

func TestUnavailableStatusDoesNotOfferCachedRelease(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v0.2.9"),
		FetchedAt:       testPublishedAt,
		StableAvailable: false,
		RCAvailable:     false,
	}}
	svc, _ := newTestService(t, "v0.2.8", snaps, newFakeChannelStore())

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, status.StatusAvailable)
	assert.False(t, status.UpdateAvailable)
	assert.Nil(t, status.LatestEligible)
	assert.Empty(t, status.InstallCommand)
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

// A transient store failure must propagate as an error — never read as the
// stable default, which would silently mask an operator's RC opt-in.
func TestStatusPropagatesChannelReadError(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v0.2.9"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
	}}
	store := newFakeChannelStore()
	store.getErr = errors.New("connection reset by peer")
	svc, _ := newTestService(t, "v0.2.8", snaps, store)

	_, err := svc.GetUpdateStatus(context.Background(), 1)
	require.Error(t, err)
	assert.ErrorContains(t, err, "get release channel setting")
}

// A failed upsert must surface to the caller so the client can revert the
// control instead of showing a channel that was never persisted.
func TestSetReleaseChannelPropagatesUpsertError(t *testing.T) {
	t.Parallel()

	store := newFakeChannelStore()
	store.upsertErr = errors.New("deadlock detected")
	svc, _ := newTestService(t, "v0.2.8", &fakeSnapshots{}, store)

	err := svc.SetReleaseChannel(context.Background(), 1, ChannelStableAndRC)
	require.Error(t, err)
	assert.ErrorContains(t, err, "upsert release channel setting")
	assert.Empty(t, store.channels, "a failed upsert must not appear persisted")
}

// A candidate outside the canonical stable/RC grammar must never yield an
// install command, and update_available stays false — even though the checker
// should never cache such a release.
func TestNoncanonicalCandidateNeverOffered(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		// Build metadata is valid semver and newer than the current version,
		// but it is outside the canonical release-tag grammar.
		LatestStable:    rel("v0.3.0+build.1"),
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
	}}
	for _, channel := range []Channel{ChannelStable, ChannelStableAndRC} {
		store := newFakeChannelStore()
		store.channels[1] = string(channel)
		svc, h := newTestService(t, "v0.2.8", snaps, store)

		status, err := svc.GetUpdateStatus(context.Background(), 1)
		require.NoError(t, err)
		assert.True(t, status.StatusAvailable, "channel %s", channel)
		assert.False(t, status.UpdateAvailable, "channel %s", channel)
		assert.Empty(t, status.InstallCommand, "channel %s", channel)
		assert.Empty(t, h.recordsAbove(slog.LevelDebug))
	}

	cmd, ok := installCommand(downloadBaseURL, "v0.3.0+build.1")
	assert.False(t, ok, "installCommand must refuse a noncanonical tag")
	assert.Empty(t, cmd)

	cmd, ok = installCommand("https://example.com/dl", "v0.3.0")
	assert.False(t, ok, "installCommand must refuse an untrusted base URL independently")
	assert.Empty(t, cmd)
}
func TestStatusAdvertisesReachableOneClickExecutor(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v1.1.0"),
		StableAvailable: true,
		RCAvailable:     true,
	}}
	svc, _ := newTestService(t, "v1.0.0", snaps, newFakeChannelStore())
	svc.executor = &fakeExecutor{}

	status, err := svc.GetUpdateStatus(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, status.OneClickAvailable)
}

func TestTriggerUpgradeRevalidatesTheEligibleTarget(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v1.1.0"),
		StableAvailable: true,
		RCAvailable:     true,
	}}
	svc, _ := newTestService(t, "v1.0.0", snaps, newFakeChannelStore())
	executor := &fakeExecutor{
		trigger: updaterapi.Operation{
			ID:            "operation-1",
			TargetVersion: "v1.1.0",
			Phase:         updaterapi.PhaseQueued,
		},
	}
	svc.executor = executor

	operation, err := svc.TriggerUpgrade(admittedUpgradeContext(context.Background()), 1, "v1.1.0")
	require.NoError(t, err)
	assert.Equal(t, "operation-1", operation.ID)
	assert.Equal(t, []string{"v1.1.0"}, executor.triggered)

	_, err = svc.TriggerUpgrade(admittedUpgradeContext(context.Background()), 1, "v1.2.0")
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Equal(t, []string{"v1.1.0"}, executor.triggered, "a stale or invented target must never reach the host executor")
}

func TestTriggerUpgradeMapsExecutorConflict(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v1.1.0"),
		StableAvailable: true,
		RCAvailable:     true,
	}}
	svc, _ := newTestService(t, "v1.0.0", snaps, newFakeChannelStore())
	svc.executor = &fakeExecutor{
		triggerErr: &updaterapi.HTTPError{
			StatusCode: http.StatusConflict,
			Message:    "upgrade already running",
		},
	}

	_, err := svc.TriggerUpgrade(admittedUpgradeContext(context.Background()), 1, "v1.1.0")
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err))
}

func newEligibleUpgradeService(t *testing.T) *Service {
	t.Helper()
	snaps := &fakeSnapshots{snap: Snapshot{
		LatestStable:    rel("v1.1.0"),
		StableAvailable: true,
		RCAvailable:     true,
	}}
	svc, _ := newTestService(t, "v1.0.0", snaps, newFakeChannelStore())
	return svc
}

func admittedUpgradeContext(ctx context.Context) context.Context {
	return admissionctx.WithActiveLifetime(ctx, context.Background())
}

func TestTriggerUpgradeFailsClosedWithoutActiveAdmission(t *testing.T) {
	t.Parallel()

	executor := &fakeExecutor{}
	svc := newEligibleUpgradeService(t)
	svc.executor = executor

	_, err := svc.TriggerUpgrade(context.Background(), 1, "v1.1.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing active-runtime admission")
	assert.Empty(t, executor.triggered)
}

func TestTriggerUpgradeRejectsEndedActiveAdmissionBeforeMutation(t *testing.T) {
	t.Parallel()

	executor := &fakeExecutor{}
	svc := newEligibleUpgradeService(t)
	svc.executor = executor
	activeCtx, cancelActive := context.WithCancel(context.Background())
	cancelActive()
	requestCtx := admissionctx.WithActiveLifetime(context.Background(), activeCtx)

	_, err := svc.TriggerUpgrade(requestCtx, 1, "v1.1.0")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, executor.triggered)
}

func TestTriggerUpgradeReconcilesAcceptedOperationAndAuditsAfterCallerCancellation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	activityStore := storemocks.NewMockActivityStore(ctrl)
	var recorded activitymodels.Event
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, event *activitymodels.Event) error {
			assert.NoError(t, ctx.Err(), "accepted-operation audit must be detached from caller cancellation")
			recorded = *event
			return nil
		},
	)

	svc := newEligibleUpgradeService(t)
	svc.activitySvc = activity.NewService(activityStore)
	executor := &fakeExecutor{}
	var accepted *updaterapi.Operation
	executor.statusFunc = func(context.Context) (updaterapi.StatusResponse, error) {
		return updaterapi.StatusResponse{Operation: accepted}, nil
	}

	requestCtx, cancelRequest := context.WithCancel(authn.SetInfo(context.Background(), &session.Info{
		OrganizationID: 1,
		ExternalUserID: "user-1",
		Username:       "test-operator",
	}))
	requestCtx = admittedUpgradeContext(requestCtx)
	executor.triggerFunc = func(triggerCtx context.Context, operationID, targetVersion string) (updaterapi.Operation, error) {
		accepted = &updaterapi.Operation{
			ID:            operationID,
			TargetVersion: targetVersion,
			Phase:         updaterapi.PhaseQueued,
		}
		cancelRequest()
		assert.NoError(t, triggerCtx.Err(), "host mutation must outlive browser cancellation")
		return updaterapi.Operation{}, &updaterapi.TransportError{Cause: errors.New("connection reset after accept")}
	}
	svc.executor = executor

	operation, err := svc.TriggerUpgrade(requestCtx, 1, "v1.1.0")
	require.NoError(t, err)
	require.NotNil(t, accepted)
	assert.Equal(t, *accepted, operation)
	assert.Equal(t, "instance_upgrade_triggered", recorded.Type)
	require.NotNil(t, recorded.UserID)
	require.NotNil(t, recorded.Username)
	assert.Equal(t, "user-1", *recorded.UserID)
	assert.Equal(t, "test-operator", *recorded.Username)
	assert.Equal(t, operation.ID, recorded.Metadata["operation_id"])
	assert.Equal(t, "v1.1.0", recorded.Metadata["target_version"])
}

func TestTriggerUpgradeCancelsHostMutationWhenActiveRuntimeEnds(t *testing.T) {
	svc := newEligibleUpgradeService(t)
	executor := &fakeExecutor{}
	triggerStarted := make(chan struct{})
	statusCalls := 0
	triggerCalls := 0
	executor.statusFunc = func(context.Context) (updaterapi.StatusResponse, error) {
		statusCalls++
		return updaterapi.StatusResponse{}, nil
	}
	executor.triggerFunc = func(ctx context.Context, _, _ string) (updaterapi.Operation, error) {
		triggerCalls++
		if triggerCalls == 1 {
			close(triggerStarted)
		}
		<-ctx.Done()
		return updaterapi.Operation{}, &updaterapi.TransportError{Cause: ctx.Err()}
	}
	svc.executor = executor

	activeCtx, cancelActive := context.WithCancel(t.Context())
	requestCtx := admissionctx.WithActiveLifetime(t.Context(), activeCtx)
	result := make(chan error, 1)
	go func() {
		_, err := svc.TriggerUpgrade(requestCtx, 1, "v1.1.0")
		result <- err
	}()

	select {
	case <-triggerStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for host mutation")
	}
	cancelActive()

	select {
	case err := <-result:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("active-runtime cancellation did not stop host mutation")
	}
	assert.Equal(t, 1, triggerCalls, "demotion must prevent the same-ID retry")
	assert.Equal(t, 1, statusCalls, "demotion must prevent reconciliation after the availability probe")
}

func TestTriggerUpgradeDoesNotClaimAnotherOperationAfterAmbiguousFailure(t *testing.T) {
	t.Parallel()

	svc := newEligibleUpgradeService(t)
	executor := &fakeExecutor{}
	triggered := false
	executor.statusFunc = func(context.Context) (updaterapi.StatusResponse, error) {
		if !triggered {
			return updaterapi.StatusResponse{}, nil
		}
		return updaterapi.StatusResponse{Operation: &updaterapi.Operation{
			ID:            "22222222-2222-4222-8222-222222222222",
			TargetVersion: "v1.1.0",
			Phase:         updaterapi.PhaseQueued,
		}}, nil
	}
	executor.triggerFunc = func(context.Context, string, string) (updaterapi.Operation, error) {
		triggered = true
		return updaterapi.Operation{}, &updaterapi.TransportError{Cause: errors.New("connection reset")}
	}
	svc.executor = executor

	_, err := svc.TriggerUpgrade(admittedUpgradeContext(context.Background()), 1, "v1.1.0")
	require.Error(t, err)
	assert.True(t, fleeterror.IsUnavailableError(err))
}

func TestTriggerUpgradePreservesUnknownOutcomeAfterDefinitiveRetryFailure(t *testing.T) {
	t.Parallel()

	svc := newEligibleUpgradeService(t)
	executor := &fakeExecutor{}
	attempt := 0
	executor.triggerFunc = func(context.Context, string, string) (updaterapi.Operation, error) {
		attempt++
		if attempt == 1 {
			return updaterapi.Operation{}, &updaterapi.TransportError{Cause: errors.New("connection reset after accept")}
		}
		return updaterapi.Operation{}, &updaterapi.HTTPError{
			StatusCode: http.StatusServiceUnavailable,
			Message:    "updater is restarting",
		}
	}
	svc.executor = executor

	_, err := svc.TriggerUpgrade(admittedUpgradeContext(context.Background()), 1, "v1.1.0")
	require.Error(t, err)
	assert.True(t, fleeterror.IsUnavailableError(err))
	assert.Contains(t, err.Error(), "did not confirm the upgrade")
	assert.NotContains(t, err.Error(), "use the install command")
	assert.Equal(t, 2, attempt)
}

func TestTriggerUpgradeMapsExecutorFailures(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{
			name:  "socket unavailable",
			err:   updaterapi.ErrUnavailable,
			check: fleeterror.IsUnavailableError,
		},
		{
			name:  "transport timeout",
			err:   &updaterapi.TransportError{Cause: context.DeadlineExceeded},
			check: fleeterror.IsUnavailableError,
		},
		{
			name:  "bad request",
			err:   &updaterapi.HTTPError{StatusCode: http.StatusBadRequest, Message: "invalid target"},
			check: fleeterror.IsFailedPreconditionError,
		},
		{
			name:  "host precondition",
			err:   &updaterapi.HTTPError{StatusCode: http.StatusPreconditionFailed, Message: "target is not newer"},
			check: fleeterror.IsFailedPreconditionError,
		},
		{
			name:  "conflict",
			err:   &updaterapi.HTTPError{StatusCode: http.StatusConflict, Message: "upgrade already running"},
			check: fleeterror.IsAlreadyExistsError,
		},
		{
			name: "updater closing",
			err:  &updaterapi.HTTPError{StatusCode: http.StatusServiceUnavailable, Message: "privileged detail"},
			check: func(err error) bool {
				return fleeterror.IsUnavailableError(err) && !strings.Contains(err.Error(), "privileged detail")
			},
		},
		{
			name: "updater internal fault",
			err:  &updaterapi.HTTPError{StatusCode: http.StatusInternalServerError, Message: "/root/secret/path"},
			check: func(err error) bool {
				return !fleeterror.IsUnavailableError(err) &&
					strings.Contains(err.Error(), "HTTP status 500") &&
					!strings.Contains(err.Error(), "/root/secret/path")
			},
		},
		{
			name: "malformed accepted response",
			err:  &updaterapi.ProtocolError{Cause: errors.New("malformed JSON")},
			check: func(err error) bool {
				return fleeterror.IsUnavailableError(err) &&
					strings.Contains(err.Error(), "check its status") &&
					!strings.Contains(err.Error(), "malformed JSON") &&
					!strings.Contains(err.Error(), "use the install command")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			svc := newEligibleUpgradeService(t)
			svc.executor = &fakeExecutor{triggerErr: test.err}

			_, err := svc.TriggerUpgrade(admittedUpgradeContext(context.Background()), 1, "v1.1.0")
			require.Error(t, err)
			assert.True(t, test.check(err), "unexpected mapped error: %v", err)
		})
	}
}

func TestTriggerUpgradeHonorsCanceledCallerBeforeMutation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		ctx  func() context.Context
		want error
	}{
		{
			name: "canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			want: context.Canceled,
		},
		{
			name: "deadline",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)
				return ctx
			},
			want: context.DeadlineExceeded,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			executor := &fakeExecutor{}
			svc := newEligibleUpgradeService(t)
			svc.executor = executor

			_, err := svc.TriggerUpgrade(admittedUpgradeContext(test.ctx()), 1, "v1.1.0")
			assert.ErrorIs(t, err, test.want)
			assert.Empty(t, executor.triggered)
		})
	}
}

func TestGetUpgradeStatusReturnsDurableOperation(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t, "v1.0.0", &fakeSnapshots{}, newFakeChannelStore())
	svc.executor = &fakeExecutor{status: updaterapi.StatusResponse{Operation: &updaterapi.Operation{
		ID:            "operation-1",
		TargetVersion: "v1.1.0",
		Phase:         updaterapi.PhaseActivating,
	}}}

	status := svc.GetUpgradeStatus(context.Background())
	assert.True(t, status.ExecutorAvailable)
	require.NotNil(t, status.Operation)
	assert.Equal(t, updaterapi.PhaseActivating, status.Operation.Phase)
}
