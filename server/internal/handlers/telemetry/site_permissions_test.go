package telemetry

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	mock "github.com/block/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGetCombinedMetricsSitePermissions(t *testing.T) {
	siteID := int64(7)
	for _, tt := range []struct {
		name       string
		sites      []int64
		unassigned bool
		allowed    bool
	}{
		{name: "container in authorized site", sites: []int64{7}, allowed: true},
		{name: "unauthorized site", sites: []int64{8}},
		{name: "mixed authorized and unauthorized sites", sites: []int64{7, 8}},
		{name: "unscoped request"},
		{name: "unassigned request", unassigned: true},
		{name: "site plus unassigned", sites: []int64{7}, unassigned: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			dataStore := mock.NewMockTelemetryDataStore(ctrl)
			deviceStore := storesMocks.NewMockDeviceStore(ctrl)
			service := telemetry.NewTelemetryService(telemetry.Config{}, dataStore, nil, nil, deviceStore, nil)
			handler := NewHandler(service)
			ctx := authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 42})
			ctx = middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &siteID, Permissions: []string{authz.PermFleetRead, authz.PermSiteRead, authz.PermMinerRead}}}))
			if tt.allowed {
				deviceStore.EXPECT().GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(42), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, filter *stores.MinerFilter) ([]string, error) {
					require.Equal(t, []int64{7}, filter.SiteIDs)
					require.True(t, filter.IncludeContainers)
					return []string{"ct1"}, nil
				})
				dataStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
					require.Equal(t, []models.DeviceIdentifier{"ct1"}, query.DeviceIDs)
					require.Equal(t, int64(42), query.OrganizationID)
					return models.CombinedMetric{Metrics: []models.Metric{{MeasurementType: models.MeasurementTypeTemperature, AggregatedValues: []models.AggregatedValue{{Type: models.AggregationTypeAverage, Value: 65}}}}}, nil
				})
			}
			response, err := handler.GetCombinedMetrics(ctx, connect.NewRequest(&telemetryv1.GetCombinedMetricsRequest{
				SiteIds: tt.sites, IncludeUnassigned: tt.unassigned,
				DeviceSelector:   &telemetryv1.DeviceSelector{SelectorValue: &telemetryv1.DeviceSelector_DeviceList{DeviceList: &telemetryv1.DeviceList{DeviceIds: []string{"ct1", "outside-site"}}}},
				MeasurementTypes: []telemetryv1.MeasurementType{telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE},
			}))
			if tt.allowed {
				require.NoError(t, err)
				require.Len(t, response.Msg.Metrics, 1)
				require.Equal(t, 65.0, response.Msg.Metrics[0].AggregatedValues[0].Value)
			} else {
				require.True(t, fleeterror.IsForbiddenError(err), "expected permission denial, got %v", err)
			}
		})
	}
}
