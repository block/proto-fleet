package pairing_test

import (
	"net/url"
	"testing"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"
	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	pairingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	miner_mocks "github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/integrationtesting"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestHandler_Pair(t *testing.T) {
	t.Run("should pair devices", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)

		testUser := testContext.DatabaseService.CreateSuperAdminUser()
		deviceIdentifications := testContext.DatabaseService.CreateAndAssignDevices(4, testUser.OrganizationID)

		authRequest := connect.NewRequest(&authv1.AuthenticateRequest{
			Username: testUser.Username,
			Password: testUser.Password,
		})

		authResponse, err := testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authRequest)
		assert.NoError(t, err)
		assert.NotEqual(t, "", authResponse.Msg.Token, "expected userId in response, go nil")

		for _, deviceIdentification := range deviceIdentifications {
			pairingRequest := connect.NewRequest(&pairingv1.PairRequest{DeviceIdentifiers: []string{deviceIdentification.ID}})
			pairingRequest.Header().Set("Authorization", "Bearer "+authResponse.Msg.Token)
			// currently not using response, as it does not yield any info
			_, err = testContext.InfrastructureProvider.PairingClient.Pair(t.Context(), pairingRequest)
			assert.NoError(t, err)
			pairingStatus, err := db.WithTransaction(t.Context(), testContext.DatabaseService.DB, func(q *sqlc.Queries) (sqlc.DevicePairingPairingStatus, error) {
				return q.GetDevicePairingStatusByDeviceDatabaseID(t.Context(), deviceIdentification.DatabaseID)
			})
			assert.NoError(t, err)
			assert.Equal(t, sqlc.DevicePairingPairingStatusPAIRED, pairingStatus)
		}
	})
}

func TestHandler_DiscoverAndPair(t *testing.T) {
	testCases := []struct {
		name           string
		useTLS         bool
		expectedScheme string
	}{
		{
			name:           "should discover and pair devices over HTTP",
			useTLS:         false,
			expectedScheme: "http",
		},
		{
			name:           "should discover and pair devices over HTTPS",
			useTLS:         true,
			expectedScheme: "https",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testContext := testutil.InitializeDBServiceInfrastructure(t)
			testUser := testContext.DatabaseService.CreateSuperAdminUser()

			minerCallCounter := miner_mocks.NewMockMinerCallCounter()
			mockMinerServer := testutil.SetupMockMinerServer(t, minerCallCounter, tc.useTLS)

			mockServerURL, err := url.Parse(mockMinerServer.URL)
			require.NoError(t, err)

			ipAddresses := []string{mockServerURL.Hostname()}
			ports := []string{"2121"}

			authRequest := connect.NewRequest(&authv1.AuthenticateRequest{
				Username: testUser.Username,
				Password: testUser.Password,
			})

			authResponse, err := testContext.InfrastructureProvider.AuthClient.Authenticate(t.Context(), authRequest)
			require.NoError(t, err)
			assert.NotEqual(t, "", authResponse.Msg.Token, "expected userId in response, go nil")

			discoverRequest := connect.NewRequest(&pairingv1.DiscoverRequest{
				Mode: &pairingv1.DiscoverRequest_IpList{
					IpList: &pairingv1.IPListModeRequest{
						IpAddresses: ipAddresses,
						Ports:       ports,
					},
				},
			})
			discoverRequest.Header().Set("Authorization", "Bearer "+authResponse.Msg.Token)

			discoverStream, err := testContext.InfrastructureProvider.PairingClient.Discover(t.Context(), discoverRequest)
			require.NoError(t, err)

			discoveredDevices := make([]*pairingv1.Device, 0)
			for discoverStream.Receive() {
				msg := discoverStream.Msg()
				if msg == nil {
					t.Fatal("received nil message from stream")
				}
				discoveredDevices = append(discoveredDevices, msg.Devices...)
			}
			require.NoError(t, discoverStream.Err())
			assert.Equal(t, 1, len(discoveredDevices))

			assert.Equal(t, tc.expectedScheme, discoveredDevices[0].UrlScheme)

			deviceIdentifier := discoveredDevices[0].DeviceIdentifier

			pairingRequest := connect.NewRequest(&pairingv1.PairRequest{DeviceIdentifiers: []string{deviceIdentifier}})
			pairingRequest.Header().Set("Authorization", "Bearer "+authResponse.Msg.Token)

			_, err = testContext.InfrastructureProvider.PairingClient.Pair(t.Context(), pairingRequest)
			require.NoError(t, err)
			devices, err := db.WithTransaction(t.Context(), testContext.DatabaseService.DB, func(q *sqlc.Queries) ([]sqlc.ListPairedMinersWithStatusRow, error) {
				return q.ListPairedMinersWithStatus(t.Context(), sqlc.ListPairedMinersWithStatusParams{
					OrgID: testUser.OrganizationID,
					Limit: 10,
				})
			})
			require.NoError(t, err)
			assert.Equal(t, 1, len(devices))
			assert.Equal(t, deviceIdentifier, devices[0].DeviceIdentifier)
			assert.Equal(t, tc.expectedScheme, devices[0].UrlScheme.String)
			mockMinerServer.Close()
		})
	}
}
