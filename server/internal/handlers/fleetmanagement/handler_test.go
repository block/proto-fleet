package fleetmanagement_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_ListMinerStateSnapshots(t *testing.T) {
	tests := []struct {
		name         string
		minerURLs    []string
		expectedURLs []string
	}{
		{
			name: "Proto miner with HTTPS",
			minerURLs: []string{
				"https://172.17.0.1:80",
			},
			expectedURLs: []string{
				"https://172.17.0.1",
			},
		},
		{
			name: "Miner with HTTP",
			minerURLs: []string{
				"http://172.17.0.2:80",
			},
			expectedURLs: []string{
				"http://172.17.0.2",
			},
		},
		{
			name: "Antminer",
			minerURLs: []string{
				"http://172.17.0.3:4028",
			},
			expectedURLs: []string{
				"http://172.17.0.3",
			},
		},
		{
			name: "Multiple miners",
			minerURLs: []string{
				"https://172.17.0.1:80",
				"http://172.17.0.2:80",
				"http://172.17.0.3:4028",
			},
			expectedURLs: []string{
				"https://172.17.0.1",
				"http://172.17.0.2",
				"http://172.17.0.3",
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

			ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
			service := testContext.ServiceProvider.FleetManagementService

			req := &pb.ListMinerStateSnapshotsRequest{
				PageSize: 5,
			}

			// Act
			resp, err := service.ListMinerStateSnapshots(ctx, req)

			// Assert
			require.NoError(t, err)
			require.Len(t, resp.Miners, len(tc.minerURLs))

			for i, miner := range resp.Miners {
				assert.Equal(t, miner.DeviceIdentifier, minerIDs[i])
				assert.Equal(t, tc.expectedURLs[i], miner.Url)
			}
		})
	}
}
