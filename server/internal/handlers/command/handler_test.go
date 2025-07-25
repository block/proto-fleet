package command_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"

	proto_mocks "github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/integrationtesting"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
)

func TestCommandHandler(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)

	adminUser := testContext.DatabaseService.CreateSuperAdminUser()

	minerCallCounter := proto_mocks.NewMockMinerCallCounter()
	mockMinerServer := testutil.SetupMockMinerServer(t, minerCallCounter, true)

	testMinerIDs := testContext.DatabaseService.CreateTestMiners(adminUser.OrganizationID, 2, mockMinerServer.URL)
	authToken := getAuthToken(t, testContext, adminUser.Username, adminUser.Password)

	t.Run("StartMining should send commands to miners", func(t *testing.T) {
		req := connect.NewRequest(&pb.StartMiningRequest{
			DeviceSelector: &pb.DeviceSelector{SelectionType: &pb.DeviceSelector_IncludeDevices{IncludeDevices: &pb.DeviceList{DeviceIdentifiers: testMinerIDs}}},
		})

		req.Header().Set("Authorization", "Bearer "+authToken)
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

		_, err := testContext.InfrastructureProvider.CommandClient.StartMining(ctx, req)
		assert.NoError(t, err)

		assert.True(t, awaitedMinerCallsHappened(minerCallCounter, proto_mocks.MethodStartMining, 2))
	})

	t.Run("StopMining should send commands to miners", func(t *testing.T) {
		req := connect.NewRequest(&pb.StopMiningRequest{
			DeviceSelector: &pb.DeviceSelector{SelectionType: &pb.DeviceSelector_IncludeDevices{IncludeDevices: &pb.DeviceList{DeviceIdentifiers: testMinerIDs}}},
		})

		req.Header().Set("Authorization", "Bearer "+authToken)
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

		_, err := testContext.InfrastructureProvider.CommandClient.StopMining(ctx, req)
		assert.NoError(t, err)

		assert.True(t, awaitedMinerCallsHappened(minerCallCounter, proto_mocks.MethodStopMining, 2))
	})
}

func awaitedMinerCallsHappened(counter *proto_mocks.MockMinerCallCounter, method proto_mocks.MethodName, expectedCalls int32) bool {
	timeout := 5 * time.Second
	pollInterval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		callCount := counter.GetCount(method)
		if callCount == expectedCalls {
			return true
		}
		time.Sleep(pollInterval)
	}

	return false
}

// Helper functions

func getAuthToken(t *testing.T, testContext *testutil.TestContext, username, password string) string {
	authRequest := connect.NewRequest(&authv1.AuthenticateRequest{
		Username: username,
		Password: password,
	})

	authResponse, err := testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authRequest)
	assert.NoError(t, err)

	return authResponse.Msg.Token
}
