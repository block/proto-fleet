package fleetmanagement_test

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	handlerpkg "github.com/block/proto-fleet/server/internal/handlers/fleetmanagement"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refreshAuthContext(ctx context.Context, userID, orgID int64, assignments ...authz.Assignment) context.Context {
	info := &session.Info{
		SessionID:      "test-session-id",
		UserID:         userID,
		OrganizationID: orgID,
	}
	return middleware.WithEffectivePermissions(authn.SetInfo(ctx, info), authz.NewEffectivePermissions(assignments))
}

func orgAssignment(permissions ...string) authz.Assignment {
	return authz.Assignment{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  permissions,
	}
}

func siteAssignment(siteID int64, permissions ...string) authz.Assignment {
	return authz.Assignment{
		AssignmentID: 2,
		ScopeType:    authz.ScopeSite,
		SiteID:       &siteID,
		Permissions:  permissions,
	}
}

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

func TestHandler_RefreshMiners_UsesSiteScopedMinerRead(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, "https://172.17.0.1:80")
	require.Len(t, deviceIDs, 1)

	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)
	site, err := siteStore.CreateSite(t.Context(), sitesmodels.CreateSiteParams{
		OrgID: testUser.OrganizationID,
		Name:  "Austin",
	})
	require.NoError(t, err)

	_, err = siteStore.ReassignDevicesToSite(t.Context(), testUser.OrganizationID, &site.ID, deviceIDs)
	require.NoError(t, err)

	handler := handlerpkg.NewHandler(testContext.ServiceProvider.FleetManagementService)
	ctx := refreshAuthContext(
		t.Context(),
		testUser.DatabaseID,
		testUser.OrganizationID,
		orgAssignment(authz.PermMinerRead),
		siteAssignment(site.ID),
	)

	resp, err := handler.RefreshMiners(ctx, connect.NewRequest(&pb.RefreshMinersRequest{DeviceIds: deviceIDs}))

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}
