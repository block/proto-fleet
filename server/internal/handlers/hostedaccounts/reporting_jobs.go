package hostedaccounts

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
)

func (h *Handler) ListHostReportQueue(ctx context.Context, req *connect.Request[pb.ListHostReportQueueRequest]) (*connect.Response[pb.ListHostReportQueueResponse], error) {
	principal, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	page, err := h.accounts.HostReportQueue(ctx, principal.UserID, principal.OrganizationID, req.Msg.PageSize, req.Msg.PageToken)
	if err != nil {
		return nil, err
	}
	out := &pb.ListHostReportQueueResponse{NextPageToken: page.NextPageToken}
	setups := map[string]hostedaccounts.ReportingSetup{}
	deniedAccounts := map[string]bool{}
	for _, row := range page.Items {
		if deniedAccounts[row.AccountID] {
			continue
		}
		item := &pb.HostReportQueueItem{OwnerUsername: row.OwnerUsername, QueueId: row.QueueID, AccountId: row.AccountID, AccountName: row.AccountName, Kind: row.Kind, OwnerUserId: row.OwnerUserID, Deadlines: reportingDeadlinesDTO(row.Deadlines), Reasons: row.Reasons, DeliveryStatus: row.DeliveryStatus, NotificationStatus: row.NotificationStatus, Generation: row.Generation, Attempts: row.Attempts, CanAcknowledge: row.CanAcknowledge, CanRetry: row.CanRetry}
		if !row.Period.Start.IsZero() {
			item.Period = reportingPeriodDTO(row.Period)
		}
		if row.Report != nil {
			setup, ok := setups[row.AccountID]
			if !ok {
				var err error
				setup, err = h.accounts.HostReportingSetup(ctx, principal.UserID, principal.OrganizationID, row.AccountID)
				if fleeterror.IsForbiddenError(err) {
					deniedAccounts[row.AccountID] = true
					continue
				}
				if err != nil {
					return nil, err
				}
				setups[row.AccountID] = setup
			}
			item.Report = hostReportSummaryDTO(*row.Report, setup)
		}
		if row.Request != nil {
			item.Request = hostReportRequestDTO(*row.Request)
		}
		for _, event := range row.Events {
			item.Events = append(item.Events, &pb.HostReportQueueEvent{Generation: event.Generation, Kind: event.Kind, ActorUserId: event.ActorUserID, Reason: event.Reason, CreatedAt: reportingTimeDTO(event.CreatedAt)})
		}
		out.Items = append(out.Items, item)
	}
	return connect.NewResponse(out), nil
}
func (h *Handler) AcknowledgeHostReportQueue(ctx context.Context, req *connect.Request[pb.AcknowledgeHostReportQueueRequest]) (*connect.Response[pb.AcknowledgeHostReportQueueResponse], error) {
	principal, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.accounts.AcknowledgeReportQueue(ctx, principal.UserID, principal.OrganizationID, req.Msg.AccountId, req.Msg.QueueId, req.Msg.ExpectedGeneration, req.Msg.Reason); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AcknowledgeHostReportQueueResponse{}), nil
}
func (h *Handler) RetryHostReportQueueNotification(ctx context.Context, req *connect.Request[pb.RetryHostReportQueueNotificationRequest]) (*connect.Response[pb.RetryHostReportQueueNotificationResponse], error) {
	principal, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.accounts.RetryReportQueueNotification(ctx, principal.UserID, principal.OrganizationID, req.Msg.AccountId, req.Msg.QueueId, req.Msg.ExpectedGeneration, req.Msg.Reason); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RetryHostReportQueueNotificationResponse{}), nil
}
