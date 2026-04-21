package foremanimport

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1"
	"github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1/foremanimportv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/foremanimport"
)

type Handler struct {
	svc *domain.Service
}

var _ foremanimportv1connect.ForemanImportServiceHandler = &Handler{}

func NewHandler(svc *domain.Service) *Handler {
	return &Handler{svc: svc}
}

func validateCredentials(creds *pb.ForemanCredentials) error {
	if creds == nil || creds.ApiKey == "" || creds.ClientId == "" {
		return fleeterror.NewInvalidArgumentError("credentials with api_key and client_id are required")
	}
	return nil
}

func (h *Handler) ImportFromForeman(ctx context.Context, r *connect.Request[pb.ImportFromForemanRequest]) (*connect.Response[pb.ImportFromForemanResponse], error) {
	if err := validateCredentials(r.Msg.Credentials); err != nil {
		return nil, err
	}

	resp, err := h.svc.ImportFromForeman(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) CompleteImport(ctx context.Context, r *connect.Request[pb.CompleteImportRequest]) (*connect.Response[pb.CompleteImportResponse], error) {
	if err := validateCredentials(r.Msg.Credentials); err != nil {
		return nil, err
	}

	resp, err := h.svc.CompleteImport(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}
