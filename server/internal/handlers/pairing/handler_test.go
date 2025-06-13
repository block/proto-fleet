package pairing_test

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"
	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	pairingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
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
