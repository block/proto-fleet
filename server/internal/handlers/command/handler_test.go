package command_test

import (
	"testing"

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

	var minerCallCounter = proto_mocks.NewMockMinerCallCounter()
	mockMinerServer := testutil.SetupMockMinerServer(t, minerCallCounter)

	testMinerIDs := testContext.DatabaseService.CreateTestMiners(adminUser.OrganizationID, 2, mockMinerServer.URL)
	authToken := getAuthToken(t, testContext, adminUser.Username, adminUser.Password)

	t.Run("StartMining should send commands to miners", func(t *testing.T) {
		req := connect.NewRequest(&pb.StartMiningRequest{
			DeviceIdentifiers: testMinerIDs,
		})

		req.Header().Set("Authorization", "Bearer "+authToken)
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

		_, err := testContext.InfrastructureProvider.CommandClient.StartMining(ctx, req)

		assert.NoError(t, err)
		minerCallCounter.AssertCalls(t, proto_mocks.MethodStartMining, 2)
	})

	t.Run("StopMining should send commands to miners", func(t *testing.T) {
		req := connect.NewRequest(&pb.StopMiningRequest{
			DeviceIdentifiers: testMinerIDs,
		})

		req.Header().Set("Authorization", "Bearer "+authToken)
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

		_, err := testContext.InfrastructureProvider.CommandClient.StopMining(ctx, req)

		assert.NoError(t, err)
		minerCallCounter.AssertCalls(t, proto_mocks.MethodStopMining, 2)
	})
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
