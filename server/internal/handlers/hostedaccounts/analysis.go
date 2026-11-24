package hostedaccounts

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
)

func (h *Handler) GenerateAccountAnalysis(ctx context.Context, request *connect.Request[pb.GenerateAccountAnalysisRequest]) (*connect.Response[pb.GenerateAccountAnalysisResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	analysis, err := h.accounts.GenerateAnalysis(ctx, info.UserID, info.OrganizationID, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GenerateAccountAnalysisResponse{Analysis: analysis}), nil
}

func (h *Handler) ListAccountAnalyses(ctx context.Context, request *connect.Request[pb.ListAccountAnalysesRequest]) (*connect.Response[pb.ListAccountAnalysesResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	analyses, err := h.accounts.AccountAnalyses(ctx, info.UserID, info.OrganizationID, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(analyses), nil
}

func (h *Handler) GetAccountAnalysis(ctx context.Context, request *connect.Request[pb.GetAccountAnalysisRequest]) (*connect.Response[pb.GetAccountAnalysisResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	analysis, err := h.accounts.AccountAnalysis(ctx, info.UserID, info.OrganizationID, request.Msg.AccountId, request.Msg.AnalysisId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetAccountAnalysisResponse{Analysis: analysis}), nil
}

func (h *Handler) DeleteAccountAnalysis(ctx context.Context, request *connect.Request[pb.DeleteAccountAnalysisRequest]) (*connect.Response[pb.DeleteAccountAnalysisResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err := h.accounts.DeleteAnalysis(ctx, info.UserID, info.OrganizationID, request.Msg.AccountId, request.Msg.AnalysisId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteAccountAnalysisResponse{}), nil
}

func (h *Handler) ExportAccountAnalysis(ctx context.Context, request *connect.Request[pb.ExportAccountAnalysisRequest], stream *connect.ServerStream[pb.ExportAccountAnalysisResponse]) error {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return err
	}
	stream.ResponseHeader().Set("X-Content-Type-Options", "nosniff")
	for sequence := int64(0); ; sequence++ {
		page, err := h.accounts.AnalysisArtifactPage(ctx, info.UserID, info.OrganizationID, request.Msg.AccountId, request.Msg.AnalysisId, sequence)
		if err != nil {
			return err
		}
		if err = stream.Send(&pb.ExportAccountAnalysisResponse{Chunk: page.Data, Filename: page.Name, MediaType: page.MediaType, Sha256: page.SHA256}); err != nil {
			return fmt.Errorf("send analysis download: %w", err)
		}
		if sequence+1 == page.Chunks {
			return nil
		}
	}
}
