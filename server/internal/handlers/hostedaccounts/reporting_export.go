package hostedaccounts

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
)

func (h *Handler) PublishHostReport(ctx context.Context, request *connect.Request[pb.PublishHostReportRequest]) (*connect.Response[pb.PublishHostReportResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	report, err := h.accounts.PublishReport(ctx, info.UserID, info.OrganizationID, request.Msg.AccountId, request.Msg.ReportId, request.Msg.ExpectedRevision, request.Msg.Exception)
	if err != nil {
		return nil, err
	}
	setup, err := h.accounts.HostReportingSetup(ctx, info.UserID, info.OrganizationID, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PublishHostReportResponse{Report: hostReportDTO(report, setup)}), nil
}
func (h *Handler) ExportAccountReport(ctx context.Context, request *connect.Request[pb.ExportAccountReportRequest], stream *connect.ServerStream[pb.ExportAccountReportResponse]) error {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return err
	}
	stream.ResponseHeader().Set("X-Content-Type-Options", "nosniff")
	return streamReportArtifact(ctx, h.accounts, info.UserID, info.OrganizationID, request.Msg.AccountId, request.Msg.ReportId, request.Msg.Name, false, func(page domain.ReportArtifactChunk) error {
		return stream.Send(&pb.ExportAccountReportResponse{Chunk: page.Data, Filename: page.Name, MediaType: page.MediaType, Sha256: page.SHA256})
	})
}
func (h *Handler) ExportHostReport(ctx context.Context, request *connect.Request[pb.ExportHostReportRequest], stream *connect.ServerStream[pb.ExportHostReportResponse]) error {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return err
	}
	stream.ResponseHeader().Set("X-Content-Type-Options", "nosniff")
	return streamReportArtifact(ctx, h.accounts, info.UserID, info.OrganizationID, request.Msg.AccountId, request.Msg.ReportId, request.Msg.Name, true, func(page domain.ReportArtifactChunk) error {
		return stream.Send(&pb.ExportHostReportResponse{Chunk: page.Data, Filename: page.Name, MediaType: page.MediaType, Sha256: page.SHA256})
	})
}
func streamReportArtifact(ctx context.Context, accounts *domain.Service, actor, org int64, account, report, name string, host bool, send func(domain.ReportArtifactChunk) error) error {
	var fingerprint string
	for sequence := int64(0); ; sequence++ {
		page, err := accounts.ReportArtifactPage(ctx, actor, org, account, report, name, sequence, host)
		if err != nil {
			return err
		}
		if sequence == 0 {
			fingerprint = page.AuthorityFingerprint
		} else if fingerprint != page.AuthorityFingerprint {
			return fleeterror.NewPlainError("report access changed during download; restart the download", connect.CodeAborted)
		}
		if err = send(page); err != nil {
			return err
		}
		if sequence+1 == page.Chunks {
			return nil
		}
	}
}
