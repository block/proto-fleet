package containers

import (
	"context"
	"strconv"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Controller logs can name operators and hosts, so reading them requires the
// log-download permission at the container's current site, on top of the
// container read permissions. The audit trail records who did what from
// where, as Fleet's own activity log does, so it also requires activity read.
var (
	controllerLogsPermissions  = []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerDownloadLogs}
	controllerAuditPermissions = append(append([]string{}, controllerLogsPermissions...), authz.PermActivityRead)
)

func authorizeAt(permissions []string) func(context.Context, *int64) error {
	return func(ctx context.Context, site *int64) error {
		for _, permission := range permissions {
			if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: site}); err != nil {
				return err
			}
		}
		return nil
	}
}

var (
	authorizeControllerLogs  = authorizeAt(controllerLogsPermissions)
	authorizeControllerAudit = authorizeAt(controllerAuditPermissions)
)

var severityToSDK = map[pb.ControllerLogSeverity]sdk.ControllerLogSeverity{
	pb.ControllerLogSeverity_CONTROLLER_LOG_SEVERITY_DEBUG: sdk.ControllerLogDebug,
	pb.ControllerLogSeverity_CONTROLLER_LOG_SEVERITY_INFO:  sdk.ControllerLogInfo,
	pb.ControllerLogSeverity_CONTROLLER_LOG_SEVERITY_WARN:  sdk.ControllerLogWarn,
	pb.ControllerLogSeverity_CONTROLLER_LOG_SEVERITY_ERROR: sdk.ControllerLogError,
}

var severityToProto = map[sdk.ControllerLogSeverity]pb.ControllerLogSeverity{
	sdk.ControllerLogDebug: pb.ControllerLogSeverity_CONTROLLER_LOG_SEVERITY_DEBUG,
	sdk.ControllerLogInfo:  pb.ControllerLogSeverity_CONTROLLER_LOG_SEVERITY_INFO,
	sdk.ControllerLogWarn:  pb.ControllerLogSeverity_CONTROLLER_LOG_SEVERITY_WARN,
	sdk.ControllerLogError: pb.ControllerLogSeverity_CONTROLLER_LOG_SEVERITY_ERROR,
}

var coverageToProto = map[sdk.ControllerLogCoverage]pb.ControllerLogCoverage{
	sdk.ControllerLogRetained:     pb.ControllerLogCoverage_CONTROLLER_LOG_COVERAGE_RETAINED,
	sdk.ControllerLogLimitReached: pb.ControllerLogCoverage_CONTROLLER_LOG_COVERAGE_LIMIT_REACHED,
	sdk.ControllerLogInterrupted:  pb.ControllerLogCoverage_CONTROLLER_LOG_COVERAGE_INTERRUPTED,
	sdk.ControllerLogNoResponse:   pb.ControllerLogCoverage_CONTROLLER_LOG_COVERAGE_NO_RESPONSE,
}

func optionalTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func (h *Handler) GetControllerLogs(ctx context.Context, req *connect.Request[pb.GetControllerLogsRequest]) (*connect.Response[pb.GetControllerLogsResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("container commands unavailable")
	}
	query := sdk.ControllerLogQuery{Limit: req.Msg.Limit, MinSeverity: severityToSDK[req.Msg.MinSeverity], Component: req.Msg.Component}
	result, err := h.commands.ReadControllerRecords(ctx, req.Msg.DeviceIdentifier, sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindLogs, ControllerID: req.Msg.ControllerId, Logs: &query}, authorizeControllerLogs)
	if err != nil {
		return nil, err
	}
	logs := result.Logs
	response := &pb.GetControllerLogsResponse{
		Entries:       make([]*pb.ControllerLogEntry, 0, len(logs.Entries)),
		Limit:         query.Limit,
		Coverage:      coverageToProto[logs.Coverage],
		Interruption:  logs.Interruption,
		ControllerId:  result.ControllerID,
		ControllerIds: result.ControllerIDs,
		ReadAt:        timestamppb.Now(),
	}
	for _, e := range logs.Entries {
		response.Entries = append(response.Entries, &pb.ControllerLogEntry{Time: optionalTimestamp(e.Time), Severity: severityToProto[e.Severity], Component: e.Component, Message: e.Message, MessageTruncated: e.MessageTruncated})
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) ListControllerAuditEvents(ctx context.Context, req *connect.Request[pb.ListControllerAuditEventsRequest]) (*connect.Response[pb.ListControllerAuditEventsResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("container commands unavailable")
	}
	query := sdk.ControllerAuditQuery{Start: req.Msg.Start.AsTime(), End: req.Msg.End.AsTime(), Limit: req.Msg.Limit, Result: req.Msg.Result}
	result, err := h.commands.ReadControllerRecords(ctx, req.Msg.DeviceIdentifier, sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindAudit, ControllerID: req.Msg.ControllerId, Audit: &query}, authorizeControllerAudit)
	if err != nil {
		return nil, err
	}
	audit := result.Audit
	response := &pb.ListControllerAuditEventsResponse{
		Events:        make([]*pb.ControllerAuditEvent, 0, len(audit.Events)),
		Limit:         query.Limit,
		LimitReached:  audit.LimitReached,
		ControllerId:  result.ControllerID,
		ControllerIds: result.ControllerIDs,
		ReadAt:        timestamppb.Now(),
	}
	for _, e := range audit.Events {
		response.Events = append(response.Events, &pb.ControllerAuditEvent{
			Id: formatAuditID(e.ID), Time: optionalTimestamp(e.Time), Actor: e.Actor, Source: e.Source, Node: e.Node,
			Action: e.Action, Target: e.Target, Result: e.Result, Error: e.Error, DetailsJson: e.DetailsJSON, Truncated: e.Truncated,
		})
	}
	return connect.NewResponse(response), nil
}

func formatAuditID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
