package sqlstores_test

import (
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesdomain "github.com/block/proto-fleet/server/internal/domain/sites"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	storesmocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestInfrastructureControlSubnetsPersistenceAndOrgMasking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	userA := testContext.DatabaseService.CreateSuperAdminUser()
	userB := testContext.DatabaseService.CreateSuperAdminUser2()
	ctx := t.Context()

	store := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)
	transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
	activitySvc := activity.NewService(sqlstores.NewSQLActivityStore(testContext.ServiceProvider.DB))
	service := sitesdomain.NewService(store, nil, nil, nil, nil, transactor, activitySvc)

	siteA, err := store.CreateSite(ctx, sitesmodels.CreateSiteParams{
		OrgID: userA.OrganizationID,
		Name:  "Commissioned Site",
	})
	require.NoError(t, err)
	siteB, err := store.CreateSite(ctx, sitesmodels.CreateSiteParams{
		OrgID: userB.OrganizationID,
		Name:  "Other Organization Site",
	})
	require.NoError(t, err)

	// Migration default is an empty, non-null decommissioned allowlist.
	got, err := service.GetInfrastructureControlSubnets(ctx, userA.OrganizationID, siteA.ID)
	require.NoError(t, err)
	assert.Empty(t, got)

	// A required audit failure rolls the site mutation back.
	ctrl := gomock.NewController(t)
	failingActivityStore := storesmocks.NewMockActivityStore(ctrl)
	failingActivityStore.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(errors.New("audit unavailable"))
	failingService := sitesdomain.NewService(
		store,
		nil,
		nil,
		nil,
		nil,
		transactor,
		activity.NewService(failingActivityStore),
	)
	_, err = failingService.SetInfrastructureControlSubnets(
		ctx,
		userA.OrganizationID,
		siteA.ID,
		[]string{"10.60.0.1/32"},
	)
	require.Error(t, err)
	got, err = service.GetInfrastructureControlSubnets(ctx, userA.OrganizationID, siteA.ID)
	require.NoError(t, err)
	assert.Empty(t, got)

	// Canonical sorting and masking survive a real persistence round trip.
	got, err = service.SetInfrastructureControlSubnets(
		ctx,
		userA.OrganizationID,
		siteA.ID,
		[]string{"fd12:3456::8/128", "10.70.4.99/24"},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"10.70.4.0/24", "fd12:3456::8/128"}, got)

	got, err = service.GetInfrastructureControlSubnets(ctx, userA.OrganizationID, siteA.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"10.70.4.0/24", "fd12:3456::8/128"}, got)

	// Replacement removes the prior values rather than appending.
	got, err = service.SetInfrastructureControlSubnets(
		ctx,
		userA.OrganizationID,
		siteA.ID,
		[]string{"192.168.44.9/32"},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"192.168.44.9/32"}, got)

	// Empty replacement decommissions the site.
	got, err = service.SetInfrastructureControlSubnets(
		ctx,
		userA.OrganizationID,
		siteA.ID,
		nil,
	)
	require.NoError(t, err)
	assert.Empty(t, got)

	// Cross-org and missing IDs share the same NotFound mask.
	for _, siteID := range []int64{siteB.ID, siteB.ID + 1_000_000} {
		_, err = service.GetInfrastructureControlSubnets(ctx, userA.OrganizationID, siteID)
		assert.True(t, fleeterror.IsNotFoundError(err), "Get site %d: %v", siteID, err)

		_, err = service.SetInfrastructureControlSubnets(
			ctx,
			userA.OrganizationID,
			siteID,
			[]string{"10.80.0.1/32"},
		)
		assert.True(t, fleeterror.IsNotFoundError(err), "Set site %d: %v", siteID, err)
	}

	// Soft-deleted sites are excluded by both focused queries.
	rows, err := store.SoftDeleteSite(ctx, userA.OrganizationID, siteA.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	_, err = service.GetInfrastructureControlSubnets(ctx, userA.OrganizationID, siteA.ID)
	assert.True(t, fleeterror.IsNotFoundError(err), "Get deleted site: %v", err)
	_, err = service.SetInfrastructureControlSubnets(
		ctx,
		userA.OrganizationID,
		siteA.ID,
		[]string{"10.90.0.1/32"},
	)
	assert.True(t, fleeterror.IsNotFoundError(err), "Set deleted site: %v", err)
}
