package updates

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	instancev1 "github.com/block/proto-fleet/server/generated/grpc/instance/v1"
	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	updates "github.com/block/proto-fleet/server/internal/domain/updates"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

var testPublishedAt = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

type fakeSnapshots struct{ snap updates.Snapshot }

func (f *fakeSnapshots) Snapshot() updates.Snapshot { return f.snap }

// fakeChannelStore mirrors the generated sqlc query contract: absent org rows
// return sql.ErrNoRows.
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

func newTestService(t *testing.T, current string, snapshots *fakeSnapshots, store *fakeChannelStore) *updates.Service {
	t.Helper()
	cfg := updates.Config{DownloadBaseURL: "https://github.com/block/proto-fleet/releases/download"}
	return updates.NewService(cfg, current, snapshots, store)
}

// GetUpdateStatus threads the session's org ID through the service and maps
// every response field, including the eligible release and install command.
func TestGetUpdateStatusMapsFields(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{snap: updates.Snapshot{
		LatestStable: &updates.Release{
			Version:     "v0.2.9",
			NotesURL:    "https://github.com/block/proto-fleet/releases/tag/v0.2.9",
			PublishedAt: testPublishedAt,
		},
		FetchedAt:       testPublishedAt,
		StableAvailable: true,
		RCAvailable:     true,
	}}
	h := NewHandler(newTestService(t, "v0.2.8", snaps, newFakeChannelStore()))
	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermInstanceUpdate)

	resp, err := h.GetUpdateStatus(ctx, connect.NewRequest(&instancev1.GetUpdateStatusRequest{}))
	require.NoError(t, err)
	msg := resp.Msg
	assert.Equal(t, "v0.2.8", msg.GetCurrentVersion())
	assert.Equal(t, instancev1.ReleaseChannel_RELEASE_CHANNEL_STABLE, msg.GetChannel())
	assert.True(t, msg.GetStatusAvailable())
	assert.True(t, msg.GetUpdateAvailable())
	require.NotNil(t, msg.GetLatestEligible())
	assert.Equal(t, "v0.2.9", msg.GetLatestEligible().GetVersion())
	assert.Equal(t, "https://github.com/block/proto-fleet/releases/tag/v0.2.9", msg.GetLatestEligible().GetReleaseNotesUrl())
	assert.Equal(t, testPublishedAt, msg.GetLatestEligible().GetPublishedAt().AsTime())
	assert.False(t, msg.GetLatestEligible().GetPrerelease())
	assert.Equal(t,
		`bash <(curl -fsSL "https://github.com/block/proto-fleet/releases/download/v0.2.9/install.sh") v0.2.9`,
		msg.GetInstallCommand())
}

// A no-update status keeps latest_eligible unset and install_command empty.
func TestGetUpdateStatusNoUpdate(t *testing.T) {
	t.Parallel()

	h := NewHandler(newTestService(t, "v0.2.8", &fakeSnapshots{}, newFakeChannelStore()))
	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermInstanceUpdate)

	resp, err := h.GetUpdateStatus(ctx, connect.NewRequest(&instancev1.GetUpdateStatusRequest{}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetStatusAvailable())
	assert.False(t, resp.Msg.GetUpdateAvailable())
	assert.Nil(t, resp.Msg.GetLatestEligible())
	assert.Empty(t, resp.Msg.GetInstallCommand())
}

// SetReleaseChannel persists the caller's org channel and the next status
// call reflects it.
func TestSetReleaseChannelPersistsAndStatusReflects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel instancev1.ReleaseChannel
		stored  string
	}{
		{name: "stable", channel: instancev1.ReleaseChannel_RELEASE_CHANNEL_STABLE, stored: "stable"},
		{name: "stable and RC", channel: instancev1.ReleaseChannel_RELEASE_CHANNEL_STABLE_AND_RC, stored: "stable_and_rc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeChannelStore()
			h := NewHandler(newTestService(t, "v0.2.8", &fakeSnapshots{}, store))
			ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermInstanceUpdate)

			_, err := h.SetReleaseChannel(ctx, connect.NewRequest(&instancev1.SetReleaseChannelRequest{
				Channel: tt.channel,
			}))
			require.NoError(t, err)
			assert.Equal(t, tt.stored, store.channels[7], "the session's org id must key the row")

			resp, err := h.GetUpdateStatus(ctx, connect.NewRequest(&instancev1.GetUpdateStatusRequest{}))
			require.NoError(t, err)
			assert.Equal(t, tt.channel, resp.Msg.GetChannel())
		})
	}
}

func TestUpgradePhaseMapping(t *testing.T) {
	t.Parallel()

	tests := map[updaterapi.Phase]instancev1.UpgradePhase{
		updaterapi.PhaseQueued:      instancev1.UpgradePhase_UPGRADE_PHASE_QUEUED,
		updaterapi.PhaseDownloading: instancev1.UpgradePhase_UPGRADE_PHASE_DOWNLOADING,
		updaterapi.PhaseVerifying:   instancev1.UpgradePhase_UPGRADE_PHASE_VERIFYING,
		updaterapi.PhaseStaging:     instancev1.UpgradePhase_UPGRADE_PHASE_STAGING,
		updaterapi.PhasePreflight:   instancev1.UpgradePhase_UPGRADE_PHASE_PREFLIGHT,
		updaterapi.PhaseActivating:  instancev1.UpgradePhase_UPGRADE_PHASE_ACTIVATING,
		updaterapi.PhaseSucceeded:   instancev1.UpgradePhase_UPGRADE_PHASE_SUCCEEDED,
		updaterapi.PhaseFailed:      instancev1.UpgradePhase_UPGRADE_PHASE_FAILED,
	}
	for phase, expected := range tests {
		assert.Equal(t, expected, phaseToProto(phase), "phase %s", phase)
	}
}

func TestUpgradeOperationMapsOutcomeRevision(t *testing.T) {
	t.Parallel()

	operation := operationToProto(updaterapi.Operation{
		ID:              "11111111-1111-4111-8111-111111111111",
		OutcomeRevision: 7,
	})

	assert.Equal(t, uint64(7), operation.GetOutcomeRevision())
}
