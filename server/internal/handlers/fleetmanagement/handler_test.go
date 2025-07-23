package fleetmanagement_test

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"
	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/fleetmanagement"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestHandler_ListMinerStateSnapshots(t *testing.T) {
	tests := []struct {
		name         string
		minerURLs    []string
		dataMode     pb.DataMode
		expectedURLs []string
	}{
		{
			name: "Proto miner with HTTPS",
			minerURLs: []string{
				"https://172.17.0.1:2121",
			},
			dataMode: pb.DataMode_DATA_MODE_SNAPSHOT,
			expectedURLs: []string{
				"https://172.17.0.1:80",
			},
		},
		{
			name: "Miner with HTTP",
			minerURLs: []string{
				"http://172.17.0.2:2121",
			},
			dataMode: pb.DataMode_DATA_MODE_SNAPSHOT,
			expectedURLs: []string{
				"http://172.17.0.2:80",
			},
		},
		{
			name: "Antminer",
			minerURLs: []string{
				"http://172.17.0.3:4028",
			},
			dataMode: pb.DataMode_DATA_MODE_SNAPSHOT,
			expectedURLs: []string{
				"http://172.17.0.3:80",
			},
		},
		{
			name: "Multiple miners",
			minerURLs: []string{
				"https://172.17.0.1:2121",
				"http://172.17.0.2:2121",
				"http://172.17.0.3:4028",
			},
			dataMode: pb.DataMode_DATA_MODE_SNAPSHOT,
			expectedURLs: []string{
				"https://172.17.0.1:80",
				"http://172.17.0.2:80",
				"http://172.17.0.3:80",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			testContext := testutil.InitializeDBServiceInfrastructure(t)
			testUser := testContext.DatabaseService.CreateSuperAdminUser()

			minerIDs := make([]string, len(tc.minerURLs))
			for i, url := range tc.minerURLs {
				minerIDs[i] = testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, url)[0]
			}

			authRequest := connect.NewRequest(&authv1.AuthenticateRequest{
				Username: testUser.Username,
				Password: testUser.Password,
			})
			authResponse, err := testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authRequest)
			require.NoError(t, err)
			authToken := authResponse.Msg.Token

			handler := fleetmanagement.NewHandler(testContext.ServiceProvider.FleetManagementService)
			req := connect.NewRequest(&pb.ListMinerStateSnapshotsRequest{
				PageSize: 5,
				DataMode: tc.dataMode,
			})
			req.Header().Set("Authorization", "Bearer "+authToken)

			// Act
			ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
			resp, err := handler.ListMinerStateSnapshots(ctx, req)

			// Assert
			require.NoError(t, err)
			require.Len(t, resp.Msg.Miners, len(tc.minerURLs))

			for i, miner := range resp.Msg.Miners {
				assert.Equal(t, miner.DeviceIdentifier, minerIDs[i])
				assert.Equal(t, tc.expectedURLs[i], miner.Url)
			}
		})
	}
}
