package telemetry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	mock "github.com/block/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

func newSiteScopeService(t *testing.T) (*TelemetryService, *mock.MockTelemetryDataStore, *storesMocks.MockDeviceStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	dataStore := mock.NewMockTelemetryDataStore(ctrl)
	deviceStore := storesMocks.NewMockDeviceStore(ctrl)
	svc := NewTelemetryService(Config{
		StalenessThreshold: time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, dataStore, nil, nil, deviceStore, nil)
	return svc, dataStore, deviceStore
}

// TestGetCombinedMetrics_SiteScope verifies the telemetry site scope is
// applied by resolving device identifiers (the continuous aggregates have no
// site_id column) and feeding them to the existing device-list path.
func TestGetCombinedMetrics_SiteScope(t *testing.T) {
	t.Run("resolves site to device identifiers and queries those", func(t *testing.T) {
		svc, dataStore, deviceStore := newSiteScopeService(t)

		deviceStore.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(42), &stores.MinerFilter{SiteIDs: []int64{7}, PairingStatuses: pairedLikeStatuses}).
			Return([]string{"device-a", "device-b"}, nil)

		// The metrics query must carry the resolved device identifiers.
		dataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, q models.CombinedMetricsQuery) (models.CombinedMetric, error) {
				assert.Equal(t, []models.DeviceIdentifier{"device-a", "device-b"}, q.DeviceIDs)
				return models.CombinedMetric{Metrics: []models.Metric{}}, nil
			})

		// Live uptime bar reuses the resolved device set.
		deviceStore.EXPECT().
			GetMinerStateCounts(gomock.Any(), int64(42), &stores.MinerFilter{DeviceIdentifiers: []string{"device-a", "device-b"}}).
			Return(&telemetryv1.MinerStateCounts{}, nil)

		_, err := svc.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
			OrganizationID: 42,
			SiteIDs:        []int64{7},
		})
		require.NoError(t, err)
	})

	t.Run("unassigned scope resolves with include_unassigned", func(t *testing.T) {
		svc, dataStore, deviceStore := newSiteScopeService(t)

		deviceStore.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(42), &stores.MinerFilter{IncludeUnassigned: true, PairingStatuses: pairedLikeStatuses}).
			Return([]string{"device-c"}, nil)
		dataStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(models.CombinedMetric{}, nil)
		deviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), int64(42), gomock.Any()).
			Return(&telemetryv1.MinerStateCounts{}, nil)

		_, err := svc.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
			OrganizationID:    42,
			IncludeUnassigned: true,
		})
		require.NoError(t, err)
	})

	t.Run("empty resolution returns empty without querying metrics", func(t *testing.T) {
		svc, dataStore, deviceStore := newSiteScopeService(t)

		deviceStore.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(42), gomock.Any()).
			Return([]string{}, nil)
		// No metrics query and no state-count call when the site has no devices.
		dataStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).Times(0)
		deviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		result, err := svc.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
			OrganizationID: 42,
			SiteIDs:        []int64{99},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Metrics)
		assert.Nil(t, result.MinerStateCounts)
	})

	t.Run("no site scope skips resolution", func(t *testing.T) {
		svc, dataStore, deviceStore := newSiteScopeService(t)

		// No resolution call when neither site_ids nor include_unassigned set.
		dataStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(models.CombinedMetric{}, nil)
		deviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), int64(42), gomock.Any()).
			Return(&telemetryv1.MinerStateCounts{}, nil)

		_, err := svc.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{OrganizationID: 42})
		require.NoError(t, err)
	})

	t.Run("intersects site scope with an explicit device list", func(t *testing.T) {
		svc, dataStore, deviceStore := newSiteScopeService(t)

		// Site 7 currently has device-a and device-b; caller asked for a and c.
		deviceStore.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(42), &stores.MinerFilter{SiteIDs: []int64{7}, PairingStatuses: pairedLikeStatuses}).
			Return([]string{"device-a", "device-b"}, nil)

		// Only the intersection (device-a) is queried — site scope is AND'd
		// with the device list, not a replacement.
		dataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, q models.CombinedMetricsQuery) (models.CombinedMetric, error) {
				assert.Equal(t, []models.DeviceIdentifier{"device-a"}, q.DeviceIDs)
				return models.CombinedMetric{}, nil
			})
		deviceStore.EXPECT().
			GetMinerStateCounts(gomock.Any(), int64(42), &stores.MinerFilter{DeviceIdentifiers: []string{"device-a"}}).
			Return(&telemetryv1.MinerStateCounts{}, nil)

		_, err := svc.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
			OrganizationID: 42,
			SiteIDs:        []int64{7},
			DeviceIDs:      []models.DeviceIdentifier{"device-a", "device-c"},
		})
		require.NoError(t, err)
	})

	t.Run("disjoint device list and site scope returns empty", func(t *testing.T) {
		svc, dataStore, deviceStore := newSiteScopeService(t)

		deviceStore.EXPECT().
			GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(42), gomock.Any()).
			Return([]string{"device-a"}, nil)
		dataStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).Times(0)
		deviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		result, err := svc.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
			OrganizationID: 42,
			SiteIDs:        []int64{7},
			DeviceIDs:      []models.DeviceIdentifier{"device-z"},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Metrics)
	})
}
