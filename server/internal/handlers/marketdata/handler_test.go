package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/marketdata/v1"
	"github.com/block/proto-fleet/server/generated/grpc/marketdata/v1/marketdatav1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/marketdata"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/stretchr/testify/require"
)

type fakeReader struct{ calls int }

func (f *fakeReader) Get(context.Context) (marketdata.Snapshot, error) {
	f.calls++
	return marketdata.Snapshot{Enabled: true, RefreshIntervalSeconds: 60, Price: &marketdata.Metric{Value: 100000, RetrievedAt: time.Unix(1790726400, 0), Source: "Coinbase", Stale: true}}, nil
}

func TestHandlerAuthorizationAndMapping(t *testing.T) {
	for _, tt := range []struct {
		name string
		ctx  func() context.Context
		code connect.Code
	}{
		{name: "unauthenticated", ctx: t.Context, code: connect.CodeUnauthenticated},
		{name: "no fleet permission", ctx: func() context.Context { return handlerstest.CtxWithPermissions(t, 1, authz.PermSiteRead) }, code: connect.CodePermissionDenied},
		{name: "org reader", ctx: func() context.Context { return handlerstest.CtxWithPermissions(t, 1, authz.PermFleetRead) }},
		{name: "site reader", ctx: func() context.Context {
			return handlerstest.CtxWithAssignments(t, 1, handlerstest.SiteAssignment(7, authz.PermFleetRead))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeReader{}
			response, err := NewHandler(reader).GetMarketData(tt.ctx(), connect.NewRequest(&pb.GetMarketDataRequest{}))
			if tt.code != 0 {
				// Domain errors are mapped by the production interceptor.
				require.Error(t, err)
				require.Zero(t, reader.calls)
				return
			}
			require.NoError(t, err)
			require.True(t, response.Msg.Enabled)
			require.Equal(t, uint32(60), response.Msg.RefreshIntervalSeconds)
			require.Equal(t, 100000.0, response.Msg.BitcoinPriceUsd.Value)
			require.True(t, response.Msg.BitcoinPriceUsd.Stale)
			require.Equal(t, "Coinbase", response.Msg.BitcoinPriceUsd.Source)
			require.Equal(t, int64(1790726400), response.Msg.BitcoinPriceUsd.RetrievedAt.Seconds)
			require.Nil(t, response.Msg.NetworkHashrateHs)
			require.Nil(t, response.Msg.EstimatedHashpriceUsdPerPhDay)
		})
	}
}

func TestHandlerReportsDisabledFeature(t *testing.T) {
	svc, err := marketdata.NewService(marketdata.Config{}, nil)
	require.NoError(t, err)
	ctx := handlerstest.CtxWithPermissions(t, 1, authz.PermFleetRead)
	response, err := NewHandler(svc).GetMarketData(ctx, connect.NewRequest(&pb.GetMarketDataRequest{}))
	require.NoError(t, err)
	require.False(t, response.Msg.Enabled)
	require.Nil(t, response.Msg.BitcoinPriceUsd)
	require.Nil(t, response.Msg.EstimatedHashpriceUsdPerPhDay)
	require.Nil(t, response.Msg.NetworkHashrateHs)
}

func TestConnectRoundTrip(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "authorized"}[authorized], func(t *testing.T) {
			reader := &fakeReader{}
			path, handler := marketdatav1connect.NewMarketDataServiceHandler(NewHandler(reader),
				connect.WithInterceptors(interceptors.NewErrorMappingInterceptor()))
			mux := http.NewServeMux()
			mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				permissions := []string{}
				if authorized {
					permissions = append(permissions, authz.PermFleetRead)
				}
				ctx := handlerstest.CtxWithPermissions(t, 1, permissions...)
				handler.ServeHTTP(w, r.WithContext(ctx))
			}))
			server := httptest.NewServer(mux)
			defer server.Close()
			client := marketdatav1connect.NewMarketDataServiceClient(server.Client(), server.URL)
			response, err := client.GetMarketData(t.Context(), connect.NewRequest(&pb.GetMarketDataRequest{}))
			if !authorized {
				require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
				require.Zero(t, reader.calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 100000.0, response.Msg.BitcoinPriceUsd.Value)
			require.Equal(t, uint32(60), response.Msg.RefreshIntervalSeconds)
			require.True(t, response.Msg.BitcoinPriceUsd.Stale)
			require.Nil(t, response.Msg.NetworkHashrateHs)
		})
	}
}
