package rollout

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	curtailment "github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	domain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestDelegatedAdvanceRollsBackRealCommandPreflightPartialDispatch(t *testing.T) {
	if testing.Short() || os.Getenv("DB_PASSWORD") == "" {
		t.Skip("delegated preflight integration test needs a database (DB_PASSWORD)")
	}
	t.Chdir(t.TempDir())
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, config)
	owner := database.CreateSuperAdminUser()
	provider := testutil.NewServiceProvider(t, database.DB, config)
	available := database.CreateDevice(owner.OrganizationID, "proto")
	curtailed := database.CreateDevice(owner.OrganizationID, "proto")
	identifiers := []string{available.ID, curtailed.ID}
	ctx := authn.SetInfo(ctxWithPermissions(t, authz.PermMinerFirmwareUpdate), &session.Info{
		OrganizationID: owner.OrganizationID, UserID: owner.DatabaseID, Username: owner.Username, AuthMethod: session.AuthMethodSession,
	})
	fileID, err := provider.FilesService.SaveFirmwareFile("update.swu", strings.NewReader("firmware payload"), files.FirmwareMetadata{
		TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "2.0.0",
	})
	require.NoError(t, err)
	queries := sqlstores.NewSQLConnectionManager(database.DB)
	svc := domain.NewService(&queries, sqlstores.NewSQLTransactor(database.DB), provider.CommandService, provider.FilesService, nil)
	handler := NewHandler(svc)
	channel, err := handler.CreateReleaseChannel(ctx, connect.NewRequest(&pb.CreateReleaseChannelRequest{
		Name: "Controlled", Scope: &pb.ReleaseChannelScope{DeviceIdentifiers: identifiers},
		Behavior: &pb.RolloutBehavior{Method: pb.RolloutMethod_ROLLOUT_METHOD_DELEGATED},
	}))
	require.NoError(t, err)
	applied, err := handler.ApplyReleaseChannelFirmware(ctx, connect.NewRequest(&pb.ApplyReleaseChannelFirmwareRequest{
		ChannelId: channel.Msg.Channel.Id, Assignments: []*pb.FirmwareAssignment{{Manufacturer: "TestCorp", Model: "TestMiner", FirmwareFileId: fileID}},
	}))
	require.NoError(t, err)
	require.Len(t, applied.Msg.StartedRollouts, 1)
	rollout := applied.Msg.StartedRollouts[0]
	require.EqualValues(t, 2, rollout.DeviceCounts.Queued)

	curtailmentStore := sqlstores.NewSQLCurtailmentStore(database.DB)
	provider.CommandService.RegisterFilter(command.NewCurtailmentActiveFilter(curtailmentStore))
	scope, err := json.Marshal(map[string]any{"device_identifiers": []string{curtailed.ID}})
	require.NoError(t, err)
	_, err = curtailmentStore.InsertEventWithTargets(ctx, curtailment.InsertEventParams{
		EventUUID: uuid.New(), OrgID: owner.OrganizationID, State: curtailment.EventStateActive,
		Mode: curtailment.ModeFixedKw, Strategy: curtailment.StrategyLeastEfficientFirst,
		Level: curtailment.LevelFull, Priority: curtailment.PriorityNormal, LoopType: curtailment.LoopTypeOpen,
		ScopeType: curtailment.ScopeTypeDeviceList, ScopeJSON: scope, ModeParamsJSON: []byte(`{"target_kw":1,"tolerance_kw":0}`),
		RestoreBatchSize: 10, EffectiveBatchSize: 10, DecisionSnapshotJSON: []byte(`{}`),
		SourceActorType: curtailment.SourceActorUser, CreatedByUserID: owner.DatabaseID, Reason: "Protect curtailed miner",
	}, []curtailment.InsertTargetParams{{
		DeviceIdentifier: curtailed.ID, TargetType: "miner", State: curtailment.TargetStateConfirmed, DesiredState: curtailment.DesiredStateCurtailed,
	}})
	require.NoError(t, err)
	blocked, err := curtailmentStore.ListActiveCurtailedDevices(ctx, owner.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, []string{curtailed.ID}, blocked)
	beforeEvents, _, err := svc.ListRolloutEvents(ctx, owner.OrganizationID, domain.EventFilter{RolloutID: rollout.Id})
	require.NoError(t, err)

	_, err = handler.AdvanceRollout(ctx, connect.NewRequest(&pb.AdvanceRolloutRequest{
		RolloutId: rollout.Id, ExpectedRevision: rollout.Revision,
		Selection: &pb.AdvanceRolloutRequest_Devices{Devices: &pb.RolloutDeviceSelection{DeviceIdentifiers: identifiers}},
	}))
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	var rpcError *connect.Error
	require.ErrorAs(t, err, &rpcError)
	var info *pb.RolloutErrorInfo
	for _, detail := range rpcError.Details() {
		message, err := detail.Value()
		require.NoError(t, err)
		if reason, ok := message.(*pb.RolloutErrorInfo); ok {
			info = reason
		}
	}
	require.NotNil(t, info, "preflight rejection must carry a typed reason")
	assert.Equal(t, pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_DEVICE_NOT_DISPATCHABLE, info.Reason)
	require.Equal(t, []string{curtailed.ID}, info.DeviceIdentifiers, "the command service admitted the available miner before the enclosing transaction rolled back")
	var batches, messages, attempts, reservations int
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM command_batch_log),
		(SELECT count(*) FROM queue_message),
		(SELECT coalesce(sum(attempts), 0) FROM firmware_rollout_device),
		(SELECT count(*) FROM firmware_rollout_reservation)`).Scan(&batches, &messages, &attempts, &reservations))
	assert.Equal(t, []int{0, 0, 0, 0}, []int{batches, messages, attempts, reservations}, "partial preflight dispatch must roll back queue and bookkeeping together")
	after, err := svc.GetRollout(ctx, owner.OrganizationID, rollout.Id)
	require.NoError(t, err)
	assert.Equal(t, rollout.Revision, after.Revision)
	assert.EqualValues(t, 2, after.DeviceCounts.Queued)
	afterEvents, _, err := svc.ListRolloutEvents(ctx, owner.OrganizationID, domain.EventFilter{RolloutID: rollout.Id})
	require.NoError(t, err)
	assert.Equal(t, beforeEvents, afterEvents, "a rejected advance must not publish a lifecycle event")
}
