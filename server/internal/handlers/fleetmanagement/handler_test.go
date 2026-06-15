package fleetmanagement_test

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	fleetmanagementsvc "github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	"github.com/block/proto-fleet/server/internal/domain/session"
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

type refreshHandlerTestService struct {
	*fleetmanagementsvc.Service

	contexts      map[string]authz.ResourceContext
	refreshCalled bool
}

func (s *refreshHandlerTestService) RefreshMinerResourceContexts(_ context.Context, _ *pb.RefreshMinersRequest) (map[string]authz.ResourceContext, error) {
	return s.contexts, nil
}

func (s *refreshHandlerTestService) RefreshMiners(_ context.Context, _ *pb.RefreshMinersRequest) (*pb.RefreshMinersResponse, error) {
	s.refreshCalled = true
	return &pb.RefreshMinersResponse{}, nil
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
	const (
		userID   = int64(1)
		orgID    = int64(2)
		siteID   = int64(3)
		deviceID = "site-scoped-device"
	)

	service := &refreshHandlerTestService{
		contexts: map[string]authz.ResourceContext{
			deviceID: {SiteID: ptr(siteID)},
		},
	}
	handler := handlerpkg.NewHandler(service)
	ctx := refreshAuthContext(
		t.Context(),
		userID,
		orgID,
		orgAssignment(authz.PermMinerRead),
		siteAssignment(siteID),
	)

	resp, err := handler.RefreshMiners(ctx, connect.NewRequest(&pb.RefreshMinersRequest{DeviceIds: []string{deviceID}}))

	require.Error(t, err)
	assert.Nil(t, resp)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr))
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.False(t, service.refreshCalled)
}

func ptr[T any](value T) *T {
	return &value
}
