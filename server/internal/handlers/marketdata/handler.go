package marketdata

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/marketdata/v1"
	"github.com/block/proto-fleet/server/generated/grpc/marketdata/v1/marketdatav1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/marketdata"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type reader interface {
	Get(ctx context.Context) (marketdata.Snapshot, error)
}

type Handler struct {
	service reader
}

var _ marketdatav1connect.MarketDataServiceHandler = (*Handler)(nil)

func NewHandler(service reader) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetMarketData(ctx context.Context, _ *connect.Request[pb.GetMarketDataRequest]) (*connect.Response[pb.GetMarketDataResponse], error) {
	// Public market data contains no tenant resources. Site-scoped fleet readers
	// may see it too; requiring org-wide fleet:read would exclude operators.
	if _, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermFleetRead); err != nil {
		return nil, err
	}
	snapshot, err := h.service.Get(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetMarketDataResponse{
		Enabled:                       snapshot.Enabled,
		RefreshIntervalSeconds:        snapshot.RefreshIntervalSeconds,
		BitcoinPriceUsd:               metric(snapshot.Price),
		EstimatedHashpriceUsdPerPhDay: metric(snapshot.Hashprice),
		NetworkHashrateHs:             metric(snapshot.Hashrate),
	}), nil
}

func metric(sample *marketdata.Metric) *pb.MarketMetric {
	if sample == nil {
		return nil
	}
	return &pb.MarketMetric{Value: sample.Value, RetrievedAt: timestamppb.New(sample.RetrievedAt), Source: sample.Source, Stale: sample.Stale}
}
