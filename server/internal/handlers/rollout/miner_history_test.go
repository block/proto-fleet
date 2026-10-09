package rollout

import (
	"os"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

func TestListMinerFirmwareHistoryTranslation(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	f := &fakeService{history: []domain.MinerFirmwareHistoryEntry{{
		RolloutID: 19, ChannelID: 3, ChannelName: "Previous channel", Manufacturer: "Proto", Model: "Rig",
		FirmwareVersion: "1.5.0", FirmwareChecksum: checksum, RolloutStatus: domain.StatusCanceled,
		CancelReason: domain.CancelReasonSuperseded, Phase: domain.PhaseDone, Attempts: 2,
		LastError: "previous attempt failed", SkipNote: "operator note", Paused: true,
		CreatedAt: at, FinishedAt: &at, LastSentAt: &at, VerifiedAt: &at,
	}}}
	h := NewHandler(f)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)
	resp, err := h.ListMinerFirmwareHistory(ctx, connect.NewRequest(&pb.ListMinerFirmwareHistoryRequest{DeviceIdentifier: " miner-0 ", PageSize: 50, Cursor: "before"}))
	require.NoError(t, err)
	assert.Equal(t, int64(7), f.lastOrgID)
	assert.Equal(t, " miner-0 ", f.lastDeviceIdentifier)
	assert.Equal(t, int32(50), f.lastPage)
	assert.Equal(t, "before", f.lastCursor)
	assert.Equal(t, "more-history", resp.Msg.Cursor)
	require.Len(t, resp.Msg.Entries, 1)
	e := resp.Msg.Entries[0]
	assert.Equal(t, int64(19), e.RolloutId)
	assert.Equal(t, int64(3), e.ChannelId)
	assert.Equal(t, "Previous channel", e.ChannelName)
	assert.Equal(t, "Proto", e.Manufacturer)
	assert.Equal(t, "Rig", e.Model)
	assert.Equal(t, "1.5.0", e.FirmwareVersion)
	assert.Equal(t, checksum, e.FirmwareChecksum)
	assert.Equal(t, pb.RolloutStatus_ROLLOUT_STATUS_CANCELED, e.RolloutStatus)
	assert.Equal(t, pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_SUPERSEDED, e.CancelReason)
	assert.Equal(t, pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_DONE, e.Phase)
	assert.Equal(t, int32(2), e.Attempts)
	assert.Equal(t, "previous attempt failed", e.LastError)
	assert.Equal(t, "operator note", e.SkipNote)
	assert.True(t, e.Paused)
	assert.Equal(t, at, e.CreatedAt.AsTime())
	assert.Equal(t, at, e.FinishedAt.AsTime())
	assert.Equal(t, at, e.LastSentAt.AsTime())
	assert.Equal(t, at, e.VerifiedAt.AsTime())
	require.NoError(t, protovalidate.Validate(resp.Msg))
	f.err = fleeterror.NewNotFoundError("miner not found")
	_, err = h.ListMinerFirmwareHistory(ctx, connect.NewRequest(&pb.ListMinerFirmwareHistoryRequest{DeviceIdentifier: "unknown"}))
	require.ErrorIs(t, err, f.err)
}

func TestListMinerFirmwareHistoryDatabase(t *testing.T) {
	if testing.Short() || os.Getenv("DB_PASSWORD") == "" {
		t.Skip("rollout integration tests need a database (DB_PASSWORD)")
	}
	db := dbtest.GetTestDB(t)
	ctx := t.Context()
	var orgID, discoveredID, deviceID, channelID, rolloutID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ('history-handler', 'History') RETURNING id`).Scan(&orgID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO discovered_device (org_id, device_identifier, manufacturer, model, driver_name, firmware_version, ip_address, port, url_scheme)
	 VALUES ($1, 'miner-0', 'proto', 'Rig', 'proto', '1.0.0', '10.0.0.1', '80', 'http') RETURNING id`, orgID).Scan(&discoveredID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO device (org_id, device_identifier, mac_address, discovered_device_id) VALUES ($1, 'miner-0', '00:00:00:00:00:01', $2) RETURNING id`, orgID, discoveredID).Scan(&deviceID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO release_channel (org_id, name, created_by) VALUES ($1, 'Prior channel', 1) RETURNING id`, orgID).Scan(&channelID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model,
	 firmware_checksum, firmware_version, assignment_generation, status, finished_at)
	 VALUES ($1, $2, 'Proto', 'Rig', $3, '1.5.0', 1, 'completed_with_failures', now()) RETURNING id`, orgID, channelID, checksum).Scan(&rolloutID))
	_, err := db.ExecContext(ctx, `INSERT INTO firmware_rollout_device (rollout_id, device_id, attempts, verified_at, last_sent_at)
	 VALUES ($1, $2, 1, now(), now())`, rolloutID, deviceID)
	require.NoError(t, err)
	queries := sqlstores.NewSQLConnectionManager(db)
	h := NewHandler(domain.NewService(&queries, nil, nil, nil, nil))
	ctx = authn.SetInfo(ctxWithPermissions(t, authz.PermMinerFirmwareUpdate), &session.Info{OrganizationID: orgID, UserID: 1, AuthMethod: session.AuthMethodSession})
	resp, err := h.ListMinerFirmwareHistory(ctx, connect.NewRequest(&pb.ListMinerFirmwareHistoryRequest{DeviceIdentifier: "miner-0", PageSize: 1}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Entries, 1)
	assert.Equal(t, rolloutID, resp.Msg.Entries[0].RolloutId)
	assert.Equal(t, pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_DONE, resp.Msg.Entries[0].Phase)
	assert.Equal(t, pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES, resp.Msg.Entries[0].RolloutStatus)
	assert.Empty(t, resp.Msg.Cursor)
	require.NoError(t, protovalidate.Validate(resp.Msg))
	otherCtx := authn.SetInfo(ctx, &session.Info{OrganizationID: orgID + 1, UserID: 1, AuthMethod: session.AuthMethodSession})
	_, err = h.ListMinerFirmwareHistory(otherCtx, connect.NewRequest(&pb.ListMinerFirmwareHistoryRequest{DeviceIdentifier: "miner-0"}))
	require.True(t, fleeterror.IsNotFoundError(err), "%v", err)
	_, err = db.ExecContext(t.Context(), `DELETE FROM firmware_rollout_device WHERE rollout_id = $1`, rolloutID)
	require.NoError(t, err)
	resp, err = h.ListMinerFirmwareHistory(ctx, connect.NewRequest(&pb.ListMinerFirmwareHistoryRequest{DeviceIdentifier: "miner-0"}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Entries)
	assert.Empty(t, resp.Msg.Cursor)
}
