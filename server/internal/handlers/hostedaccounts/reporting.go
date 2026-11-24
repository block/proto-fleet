package hostedaccounts

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/grpc/hosted/v1/hostedv1connect"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
)

const reportingMutationReadMaxBytes = 4 << 20

func ReportingRequestReadLimitOption() connect.HandlerOption {
	return connect.WithConditionalHandlerOptions(func(spec connect.Spec) []connect.HandlerOption {
		if spec.Procedure == hostedv1connect.HostedAccountServiceMutateHostReportingInputsProcedure {
			return []connect.HandlerOption{connect.WithReadMaxBytes(reportingMutationReadMaxBytes)}
		}
		if spec.Procedure == hostedv1connect.HostedAccountServiceListHostReportingFactsProcedure {
			return []connect.HandlerOption{connect.WithReadMaxBytes(32 << 10)}
		}
		return nil
	})
}

func (h *Handler) ListHostReportingAccounts(ctx context.Context, req *connect.Request[pb.ListHostReportingAccountsRequest]) (*connect.Response[pb.ListHostReportingAccountsResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.accounts.HostReportingAccounts(ctx, info.UserID, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := &pb.ListHostReportingAccountsResponse{}
	for _, r := range rows {
		out.Accounts = append(out.Accounts, &pb.HostReportingAccount{AccountId: r.AccountID, Name: r.Name, CanCreate: r.CanCreate, CanAdminister: r.CanAdminister})
	}
	return connect.NewResponse(out), nil
}
func (h *Handler) GetHostReportingSetup(ctx context.Context, req *connect.Request[pb.GetHostReportingSetupRequest]) (*connect.Response[pb.GetHostReportingSetupResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	row, err := h.accounts.HostReportingSetup(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetHostReportingSetupResponse{Setup: reportingSetupDTO(row)}), nil
}
func (h *Handler) SetHostReportingAuthority(ctx context.Context, req *connect.Request[pb.SetHostReportingAuthorityRequest]) (*connect.Response[pb.SetHostReportingAuthorityResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	a := req.Msg.GetAuthority()
	grant := domain.ReportingAuthority{UserID: a.GetUserId(), CanCreate: a.GetCanCreate(), AllSites: a.GetAllSites(), SiteIDs: append([]int64(nil), a.GetSiteIds()...)}
	if err := h.accounts.SetReportingAuthority(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, grant, req.Msg.ExpectedRevision); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetHostReportingAuthorityResponse{}), nil
}
func (h *Handler) MutateHostReportingInputs(ctx context.Context, req *connect.Request[pb.MutateHostReportingInputsRequest]) (*connect.Response[pb.MutateHostReportingInputsResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	changes := make([]reporting.FactMutation, 0, len(req.Msg.Mutations))
	for _, mutation := range req.Msg.Mutations {
		change, err := reportingFactMutationDomain(mutation)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	var config *reporting.Configuration
	if req.Msg.Configuration != nil {
		d := &reportingDecoder{}
		v := decodeReportingConfiguration(req.Msg.Configuration, d)
		if d.err != nil {
			return nil, d.err
		}
		config = &v
	}
	row, err := h.accounts.MutateReportingInputs(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.ExpectedRevision, req.Msg.SourceRef, config, changes)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.MutateHostReportingInputsResponse{Input: reportingInputsMetadataDTO(row)}), nil
}
func (h *Handler) ListHostReportingFacts(ctx context.Context, req *connect.Request[pb.ListHostReportingFactsRequest]) (*connect.Response[pb.ListHostReportingFactsResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	kind, err := reportingFactKindDomain(req.Msg.Kind)
	if err != nil {
		return nil, err
	}
	page, err := h.accounts.ReportingFactPage(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, kind, req.Msg.ExpectedRevision, req.Msg.Cursor, req.Msg.PageSize)
	if err != nil {
		return nil, err
	}
	out := &pb.ListHostReportingFactsResponse{Revision: page.Revision, NextCursor: page.NextCursor}
	for _, f := range page.Facts {
		value, err := reportingFactDTO(f)
		if err != nil {
			return nil, err
		}
		out.Facts = append(out.Facts, value)
	}
	return connect.NewResponse(out), nil
}
func (h *Handler) PrepareHostReport(ctx context.Context, req *connect.Request[pb.PrepareHostReportRequest]) (*connect.Response[pb.PrepareHostReportResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	period, err := reportingPeriodDomain(req.Msg.Period)
	if err != nil {
		return nil, err
	}
	setup, err := h.accounts.HostReportingSetup(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	row, err := h.accounts.PrepareReport(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, domain.DraftReportOptions{Period: period, InputRevision: req.Msg.InputRevision, ReportID: req.Msg.GetReportId(), ExpectedRevision: req.Msg.ExpectedRevision, CorrectionOf: req.Msg.GetCorrectionOf(), CorrectionReason: req.Msg.CorrectionReason, OwnerUserID: req.Msg.GetOwnerUserId()})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PrepareHostReportResponse{Report: hostReportDTO(row, setup)}), nil
}
func (h *Handler) ListHostReports(ctx context.Context, req *connect.Request[pb.ListHostReportsRequest]) (*connect.Response[pb.ListHostReportsResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	setup, err := h.accounts.HostReportingSetup(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	rows, err := h.accounts.HostReports(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, reportListOptions(req.Msg.PageSize, req.Msg.PageToken, req.Msg.Kind, req.Msg.Search, req.Msg.SeriesId))
	if err != nil {
		return nil, err
	}
	out := &pb.ListHostReportsResponse{NextPageToken: rows.NextPageToken}
	for _, row := range rows.Reports {
		out.Reports = append(out.Reports, hostReportSummaryDTO(row, setup))
	}
	return connect.NewResponse(out), nil
}
func (h *Handler) GetHostReport(ctx context.Context, req *connect.Request[pb.GetHostReportRequest]) (*connect.Response[pb.GetHostReportResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	setup, err := h.accounts.HostReportingSetup(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	row, err := h.accounts.HostReport(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.ReportId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetHostReportResponse{Report: hostReportDTO(row, setup)}), nil
}
func (h *Handler) ListAccountReports(ctx context.Context, req *connect.Request[pb.ListAccountReportsRequest]) (*connect.Response[pb.ListAccountReportsResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	rows, err := h.accounts.AccountReports(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, reportListOptions(req.Msg.PageSize, req.Msg.PageToken, req.Msg.Kind, req.Msg.Search, req.Msg.SeriesId))
	if err != nil {
		return nil, err
	}
	out := &pb.ListAccountReportsResponse{NextPageToken: rows.NextPageToken}
	for _, row := range rows.Reports {
		if dto := accountReportSummaryDTO(row); dto != nil {
			out.Reports = append(out.Reports, dto)
		}
	}
	return connect.NewResponse(out), nil
}
func (h *Handler) GetAccountReport(ctx context.Context, req *connect.Request[pb.GetAccountReportRequest]) (*connect.Response[pb.GetAccountReportResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	row, err := h.accounts.AccountReport(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.ReportId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetAccountReportResponse{Report: accountReportDTO(row)}), nil
}
func (h *Handler) RecordHostReportDelivery(ctx context.Context, req *connect.Request[pb.RecordHostReportDeliveryRequest]) (*connect.Response[pb.RecordHostReportDeliveryResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	d := &reportingDecoder{}
	delivery := domain.DeliveryEvidence{RequestKey: req.Msg.RequestId, Channel: req.Msg.Channel, Status: req.Msg.Status, SentAt: d.optionalTime(req.Msg.SentAt), ReceivedAt: d.optionalTime(req.Msg.ReceivedAt), SourceRef: req.Msg.SourceRef, Evidence: req.Msg.Evidence, FailureCategory: req.Msg.FailureCategory}
	if d.err != nil {
		return nil, d.err
	}
	if err := h.accounts.RecordReportDelivery(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.ReportId, delivery); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RecordHostReportDeliveryResponse{}), nil
}
func (h *Handler) CreateHostReportRequest(ctx context.Context, req *connect.Request[pb.CreateHostReportRequestRequest]) (*connect.Response[pb.CreateHostReportRequestResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	d := &reportingDecoder{}
	m := req.Msg.GetRequest()
	input := domain.ReportRequest{ReceivedAt: d.time(m.GetReceivedAt()), SourceRef: m.GetSourceRef(), Requester: m.GetRequester(), Scope: m.GetScope(), OwnerUserID: m.GetOwnerUserId(), TargetAt: d.time(m.GetTargetAt()), Limitations: m.GetLimitations(), CommunicationRef: m.GetCommunicationRef(), DeliveryRef: m.GetDeliveryRef()}
	if d.err != nil {
		return nil, d.err
	}
	row, err := h.accounts.CreateReportRequest(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, input)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateHostReportRequestResponse{Request: hostReportRequestDTO(row)}), nil
}
func (h *Handler) ListHostReportRequests(ctx context.Context, req *connect.Request[pb.ListHostReportRequestsRequest]) (*connect.Response[pb.ListHostReportRequestsResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	page, err := h.accounts.ReportRequests(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, domain.ReportRequestListOptions{PageSize: req.Msg.PageSize, PageToken: req.Msg.PageToken})
	if err != nil {
		return nil, err
	}
	out := &pb.ListHostReportRequestsResponse{NextPageToken: page.NextPageToken}
	for _, row := range page.Requests {
		out.Requests = append(out.Requests, hostReportRequestDTO(row))
	}
	return connect.NewResponse(out), nil
}

func (h *Handler) RecordHostReportRequestFulfillment(ctx context.Context, req *connect.Request[pb.RecordHostReportRequestFulfillmentRequest]) (*connect.Response[pb.RecordHostReportRequestFulfillmentResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	d := reportingDecoder{}
	entry := domain.ReportRequestFulfillment{EntryKey: req.Msg.EntryKey, DeliveredAt: d.time(req.Msg.DeliveredAt), DeliveryRef: req.Msg.DeliveryRef, CommunicationRef: req.Msg.CommunicationRef, Evidence: req.Msg.Evidence}
	if err := d.err; err != nil {
		return nil, err
	}
	if err := h.accounts.RecordReportRequestFulfillment(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.RequestId, entry); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RecordHostReportRequestFulfillmentResponse{}), nil
}

func reportListOptions(size int32, token, kind, search, series string) domain.ReportListOptions {
	return domain.ReportListOptions{PageSize: size, PageToken: token, Kind: kind, Search: search, SeriesID: series}
}
