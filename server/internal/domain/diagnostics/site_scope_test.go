package diagnostics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// pairedLikeStatuses mirrors the fleet-visible set the site resolver requests.
var pairedLikeStatuses = []fm.PairingStatus{
	fm.PairingStatus_PAIRING_STATUS_PAIRED,
	fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
	fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
}

// TestApplySiteScope covers translating a site scope into device identifiers
// for the errors query: the errors path has no site_id join, so a site filter
// is resolved to the site's current devices and applied via device_identifiers.
func TestApplySiteScope(t *testing.T) {
	newSvc := func(t *testing.T) (*Service, *storeMocks.MockDeviceStore) {
		t.Helper()
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		resolver := storeMocks.NewMockDeviceStore(ctrl)
		return (&Service{}).WithDeviceScopeResolver(resolver), resolver
	}

	t.Run("no site scope is a no-op", func(t *testing.T) {
		svc, _ := newSvc(t)
		opts := &models.QueryOptions{OrgID: 1, Filter: &models.QueryFilter{}}
		scoped, err := svc.applySiteScope(t.Context(), opts)
		require.NoError(t, err)
		assert.True(t, scoped)
		assert.Empty(t, opts.Filter.DeviceIdentifiers)
	})

	t.Run("nil filter is a no-op", func(t *testing.T) {
		svc, _ := newSvc(t)
		opts := &models.QueryOptions{OrgID: 1}
		scoped, err := svc.applySiteScope(t.Context(), opts)
		require.NoError(t, err)
		assert.True(t, scoped)
	})

	t.Run("resolves site to device identifiers", func(t *testing.T) {
		svc, resolver := newSvc(t)
		resolver.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), &stores.MinerFilter{SiteIDs: []int64{7}, PairingStatuses: pairedLikeStatuses}).
			Return([]string{"d1", "d2"}, nil)

		opts := &models.QueryOptions{OrgID: 1, Filter: &models.QueryFilter{SiteIDs: []int64{7}}}
		scoped, err := svc.applySiteScope(t.Context(), opts)
		require.NoError(t, err)
		assert.True(t, scoped)
		assert.Equal(t, []string{"d1", "d2"}, opts.Filter.DeviceIdentifiers)
	})

	t.Run("empty resolution yields scoped=false", func(t *testing.T) {
		svc, resolver := newSvc(t)
		resolver.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), gomock.Any()).
			Return([]string{}, nil)

		opts := &models.QueryOptions{OrgID: 1, Filter: &models.QueryFilter{SiteIDs: []int64{99}}}
		scoped, err := svc.applySiteScope(t.Context(), opts)
		require.NoError(t, err)
		assert.False(t, scoped)
	})

	t.Run("intersects an explicit device list with the site scope", func(t *testing.T) {
		svc, resolver := newSvc(t)
		resolver.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), gomock.Any()).
			Return([]string{"d1", "d2", "d3"}, nil)

		opts := &models.QueryOptions{OrgID: 1, Filter: &models.QueryFilter{
			SiteIDs:           []int64{7},
			DeviceIdentifiers: []string{"d2", "d9"},
		}}
		scoped, err := svc.applySiteScope(t.Context(), opts)
		require.NoError(t, err)
		assert.True(t, scoped)
		assert.Equal(t, []string{"d2"}, opts.Filter.DeviceIdentifiers)
	})

	t.Run("disjoint device list and site scope yields scoped=false", func(t *testing.T) {
		svc, resolver := newSvc(t)
		resolver.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), gomock.Any()).
			Return([]string{"d1"}, nil)

		opts := &models.QueryOptions{OrgID: 1, Filter: &models.QueryFilter{
			SiteIDs:           []int64{7},
			DeviceIdentifiers: []string{"d9"},
		}}
		scoped, err := svc.applySiteScope(t.Context(), opts)
		require.NoError(t, err)
		assert.False(t, scoped)
	})

	t.Run("site scope without a resolver errors", func(t *testing.T) {
		opts := &models.QueryOptions{OrgID: 1, Filter: &models.QueryFilter{IncludeUnassigned: true}}
		_, err := (&Service{}).applySiteScope(t.Context(), opts)
		require.Error(t, err)
	})
}
