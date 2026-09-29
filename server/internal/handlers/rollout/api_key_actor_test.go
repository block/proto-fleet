package rollout

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestAuthenticatedAPIKeysKeepDistinctRolloutActors(t *testing.T) {
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, config)
	provider := testutil.NewServiceProvider(t, database.DB, config)
	owner := database.CreateSuperAdminUser()
	user, err := provider.UserStore.GetUserByID(t.Context(), owner.DatabaseID)
	require.NoError(t, err)
	svc := newFakeService()
	auth := interceptors.NewAuthInterceptor(
		provider.SessionService, provider.UserStore, provider.UserStore,
		provider.ApiKeyService, provider.PermissionResolver, nil, nil, nil,
	)
	mux := http.NewServeMux()
	mux.Handle(rolloutv1connect.NewRolloutServiceHandler(NewHandler(svc), connect.WithInterceptors(auth)))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := rolloutv1connect.NewRolloutServiceClient(server.Client(), server.URL)

	var previousActor rollout.Actor
	for _, name := range []string{"canary controller", "production controller"} {
		// Both keys belong to the same user. The persisted key identity must
		// survive authentication while command ownership remains that user.
		rawKey, _, err := provider.ApiKeyService.Create(
			t.Context(), owner.DatabaseID, owner.OrganizationID,
			user.UserID, owner.Username, name, nil,
		)
		require.NoError(t, err)
		key, err := provider.ApiKeyService.Validate(t.Context(), rawKey)
		require.NoError(t, err)
		require.NotZero(t, key.ID)
		expected := rollout.Actor{Type: rollout.ActorTypeAPIKey, ID: key.ID, Name: name, OwnerUserID: owner.DatabaseID}

		apply := connect.NewRequest(&pb.ApplyReleaseChannelFirmwareRequest{
			ChannelId:   3,
			Assignments: []*pb.FirmwareAssignment{{Manufacturer: "Proto", Model: "Rig", FirmwareFileId: "fw-2"}},
		})
		apply.Header().Set("Authorization", "Bearer "+rawKey)
		_, err = client.ApplyReleaseChannelFirmware(t.Context(), apply)
		require.NoError(t, err)
		assert.Equal(t, expected, svc.lastActor)
		assert.NotEqual(t, previousActor.ID, svc.lastActor.ID)
		previousActor = svc.lastActor

		pause := connect.NewRequest(&pb.PauseRolloutRequest{RolloutId: 9, ExpectedRevision: 4})
		pause.Header().Set("Authorization", "Bearer "+rawKey)
		_, err = client.PauseRollout(t.Context(), pause)
		require.NoError(t, err)
		assert.Equal(t, expected, svc.lastMutation.Actor)

		advance := connect.NewRequest(&pb.AdvanceRolloutRequest{
			RolloutId: 9, ExpectedRevision: 4, Note: "canary healthy",
			Selection: &pb.AdvanceRolloutRequest_Count{Count: 1},
		})
		advance.Header().Set("Authorization", "Bearer "+rawKey)
		_, err = client.AdvanceRollout(t.Context(), advance)
		require.NoError(t, err)
		assert.Equal(t, expected, svc.lastMutation.Actor)
		assert.Equal(t, "canary healthy", svc.lastMutation.Note)

		skip := connect.NewRequest(&pb.SkipRolloutDevicesRequest{
			RolloutId: 9, ExpectedRevision: 4, Note: "maintenance window",
			Devices: &pb.RolloutDeviceSelection{DeviceIdentifiers: []string{"miner-0"}},
		})
		skip.Header().Set("Authorization", "Bearer "+rawKey)
		_, err = client.SkipRolloutDevices(t.Context(), skip)
		require.NoError(t, err)
		assert.Equal(t, expected, svc.lastMutation.Actor)
		assert.Equal(t, "maintenance window", svc.lastMutation.Note)

		complete := connect.NewRequest(&pb.CompleteRolloutRequest{RolloutId: 9, ExpectedRevision: 4, Note: "controller finished"})
		complete.Header().Set("Authorization", "Bearer "+rawKey)
		_, err = client.CompleteRollout(t.Context(), complete)
		require.NoError(t, err)
		assert.Equal(t, expected, svc.lastMutation.Actor)
		assert.Equal(t, "controller finished", svc.lastMutation.Note)
	}
}
