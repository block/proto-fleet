package notifications

import (
	"context"
	"errors"
	"math"
	"strconv"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationsv1 "github.com/block/proto-fleet/server/generated/grpc/notifications/v1"
	"github.com/block/proto-fleet/server/generated/grpc/notifications/v1/notificationsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
	notifications "github.com/block/proto-fleet/server/internal/domain/notifications"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	svc     *notifications.Service
	history notificationhistory.Lister
}

func NewHandler(svc *notifications.Service, history notificationhistory.Lister) *Handler {
	return &Handler{svc: svc, history: history}
}

var (
	_ notificationsv1connect.ChannelServiceHandler           = (*Handler)(nil)
	_ notificationsv1connect.RuleServiceHandler              = (*Handler)(nil)
	_ notificationsv1connect.MaintenanceWindowServiceHandler = (*Handler)(nil)
	_ notificationsv1connect.HistoryServiceHandler           = (*Handler)(nil)
)

const (
	historyDefaultPageSize = 50
	historyMaxPageSize     = 200
)

func (h *Handler) authorize(ctx context.Context, permission string) (int64, error) {
	orgID, _, err := h.authorizeActor(ctx, permission)
	return orgID, err
}

// Like authorize, but also returns the authenticated username for actions that record an actor.
func (h *Handler) authorizeActor(ctx context.Context, permission string) (int64, string, error) {
	info, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{})
	if err != nil {
		return 0, "", err
	}
	if info.OrganizationID == 0 {
		return 0, "", fleeterror.NewUnauthenticatedError("organization id missing on session")
	}
	return info.OrganizationID, info.Username, nil
}

func mapErr(err error) error {
	if errors.Is(err, notifications.ErrNotFound) {
		return fleeterror.NewNotFoundError(err.Error())
	}
	return err
}

func (h *Handler) ListChannels(ctx context.Context, _ *connect.Request[notificationsv1.ListChannelsRequest]) (*connect.Response[notificationsv1.ListChannelsResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationRead)
	if err != nil {
		return nil, err
	}
	channels, err := h.svc.ListChannels(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*notificationsv1.Channel, 0, len(channels))
	for _, c := range channels {
		out = append(out, channelToProto(c))
	}
	return connect.NewResponse(&notificationsv1.ListChannelsResponse{Channels: out}), nil
}

func (h *Handler) CreateChannel(ctx context.Context, req *connect.Request[notificationsv1.CreateChannelRequest]) (*connect.Response[notificationsv1.CreateChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	dom, err := protoToChannel("", req.Msg.GetName(), req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSlack())
	if err != nil {
		return nil, err
	}
	created, err := h.svc.CreateChannel(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.CreateChannelResponse{Channel: channelToProto(*created)}), nil
}

func (h *Handler) UpdateChannel(ctx context.Context, req *connect.Request[notificationsv1.UpdateChannelRequest]) (*connect.Response[notificationsv1.UpdateChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	dom, err := protoToChannel(req.Msg.GetId(), req.Msg.GetName(), req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSlack())
	if err != nil {
		return nil, err
	}
	updated, err := h.svc.UpdateChannel(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.UpdateChannelResponse{Channel: channelToProto(*updated)}), nil
}

func (h *Handler) DeleteChannel(ctx context.Context, req *connect.Request[notificationsv1.DeleteChannelRequest]) (*connect.Response[notificationsv1.DeleteChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteChannel(ctx, orgID, req.Msg.GetId()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.DeleteChannelResponse{}), nil
}

func (h *Handler) TestChannel(ctx context.Context, req *connect.Request[notificationsv1.TestChannelRequest]) (*connect.Response[notificationsv1.TestChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	// A saved-channel test needs only the id; TestChannel loads the stored contact point and ignores kind/config.
	dom := notifications.Channel{ID: req.Msg.GetId()}
	if dom.ID == "" {
		dom, err = protoToChannel("", "", req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSlack())
		if err != nil {
			return nil, err
		}
	}
	ok, code, errMsg, err := h.svc.TestChannel(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.TestChannelResponse{
		Ok:           ok,
		Error:        errMsg,
		ResponseCode: httpStatusToInt32(code),
	}), nil
}

func (h *Handler) ListRules(ctx context.Context, _ *connect.Request[notificationsv1.ListRulesRequest]) (*connect.Response[notificationsv1.ListRulesResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationRead)
	if err != nil {
		return nil, err
	}
	rules, err := h.svc.ListRules(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*notificationsv1.Rule, 0, len(rules))
	for _, r := range rules {
		out = append(out, ruleToProto(r))
	}
	return connect.NewResponse(&notificationsv1.ListRulesResponse{Rules: out}), nil
}

func (h *Handler) PauseRule(ctx context.Context, req *connect.Request[notificationsv1.PauseRuleRequest]) (*connect.Response[notificationsv1.PauseRuleResponse], error) {
	orgID, actor, err := h.authorizeActor(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	rule, err := h.svc.PauseRule(ctx, orgID, req.Msg.GetId(), actor)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.PauseRuleResponse{Rule: ruleToProto(*rule)}), nil
}

func (h *Handler) ResumeRule(ctx context.Context, req *connect.Request[notificationsv1.ResumeRuleRequest]) (*connect.Response[notificationsv1.ResumeRuleResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	rule, err := h.svc.ResumeRule(ctx, orgID, req.Msg.GetId())
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.ResumeRuleResponse{Rule: ruleToProto(*rule)}), nil
}

func (h *Handler) ListMaintenanceWindows(ctx context.Context, _ *connect.Request[notificationsv1.ListMaintenanceWindowsRequest]) (*connect.Response[notificationsv1.ListMaintenanceWindowsResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationRead)
	if err != nil {
		return nil, err
	}
	silences, err := h.svc.ListMaintenanceWindows(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*notificationsv1.MaintenanceWindow, 0, len(silences))
	for _, s := range silences {
		out = append(out, maintenanceWindowToProto(s))
	}
	return connect.NewResponse(&notificationsv1.ListMaintenanceWindowsResponse{MaintenanceWindows: out}), nil
}

func (h *Handler) CreateMaintenanceWindow(ctx context.Context, req *connect.Request[notificationsv1.CreateMaintenanceWindowRequest]) (*connect.Response[notificationsv1.CreateMaintenanceWindowResponse], error) {
	orgID, actor, err := h.authorizeActor(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	dom, err := protoToMaintenanceWindow("", req.Msg.GetScope(), req.Msg.GetStartsAt(), req.Msg.GetEndsAt(), req.Msg.GetComment())
	if err != nil {
		return nil, err
	}
	dom.CreatedBy = actor
	created, err := h.svc.CreateMaintenanceWindow(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.CreateMaintenanceWindowResponse{MaintenanceWindow: maintenanceWindowToProto(*created)}), nil
}

func (h *Handler) UpdateMaintenanceWindow(ctx context.Context, req *connect.Request[notificationsv1.UpdateMaintenanceWindowRequest]) (*connect.Response[notificationsv1.UpdateMaintenanceWindowResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	dom, err := protoToMaintenanceWindow(req.Msg.GetId(), req.Msg.GetScope(), req.Msg.GetStartsAt(), req.Msg.GetEndsAt(), req.Msg.GetComment())
	if err != nil {
		return nil, err
	}
	updated, err := h.svc.UpdateMaintenanceWindow(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.UpdateMaintenanceWindowResponse{MaintenanceWindow: maintenanceWindowToProto(*updated)}), nil
}

func (h *Handler) DeleteMaintenanceWindow(ctx context.Context, req *connect.Request[notificationsv1.DeleteMaintenanceWindowRequest]) (*connect.Response[notificationsv1.DeleteMaintenanceWindowResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteMaintenanceWindow(ctx, orgID, req.Msg.GetId()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.DeleteMaintenanceWindowResponse{}), nil
}

func (h *Handler) ListNotifications(ctx context.Context, req *connect.Request[notificationsv1.ListNotificationsRequest]) (*connect.Response[notificationsv1.ListNotificationsResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationRead)
	if err != nil {
		return nil, err
	}
	// Device identity (id/name/mac) is miner data, so gate those fields on org-scope miner:read rather than leaking them via notification:read.
	includeDevice, err := middleware.HasPermission(ctx, authz.PermMinerRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	limit := req.Msg.GetPageSize()
	if limit <= 0 {
		limit = historyDefaultPageSize
	}
	if limit > historyMaxPageSize {
		limit = historyMaxPageSize
	}

	// Active-only is a current-state view, not a feed: return the latest firing row per alert without keyset paging.
	// Over-fetch by one so the response can flag (rather than silently swallow) an alert storm past the cap.
	if req.Msg.GetActiveOnly() {
		rows, err := h.history.ListActive(ctx, orgID, historyMaxPageSize+1)
		if err != nil {
			return nil, err
		}
		hasMore := len(rows) > historyMaxPageSize
		if hasMore {
			rows = rows[:historyMaxPageSize]
		}
		out := make([]*notificationsv1.NotificationHistoryEntry, 0, len(rows))
		for _, n := range rows {
			out = append(out, historyEntryToProto(n, includeDevice))
		}
		return connect.NewResponse(&notificationsv1.ListNotificationsResponse{Notifications: out, HasMore: hasMore}), nil
	}

	var beforeID *int64
	if s := req.Msg.GetBeforeId(); s != "" {
		v, parseErr := strconv.ParseInt(s, 10, 64)
		if parseErr != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid before_id: " + s)
		}
		beforeID = &v
	}
	rows, err := h.history.List(ctx, orgID, beforeID, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]*notificationsv1.NotificationHistoryEntry, 0, len(rows))
	for _, n := range rows {
		out = append(out, historyEntryToProto(n, includeDevice))
	}
	return connect.NewResponse(&notificationsv1.ListNotificationsResponse{
		Notifications: out,
		HasMore:       hasMore,
	}), nil
}

func channelToProto(c notifications.Channel) *notificationsv1.Channel {
	out := &notificationsv1.Channel{
		Id:              c.ID,
		OrganizationId:  c.OrganizationID,
		Name:            c.Name,
		Kind:            channelKindToProto(c.Kind),
		CreatedAt:       timestamppb.New(c.CreatedAt),
		UpdatedAt:       timestamppb.New(c.UpdatedAt),
		ValidationState: validationStateToProto(c.ValidationState),
		ValidationError: c.ValidationError,
		HasSecret:       c.HasSecret,
	}
	if c.ValidatedAt != nil {
		out.ValidatedAt = timestamppb.New(*c.ValidatedAt)
	}
	if c.Webhook != nil {
		out.Webhook = &notificationsv1.WebhookConfig{Url: c.Webhook.URL}
	}
	if c.Slack != nil {
		// webhook_url deliberately omitted: it's the secret.
		out.Slack = &notificationsv1.SlackConfig{}
	}
	return out
}

func protoToChannel(id, name string, kind notificationsv1.ChannelKind, wh *notificationsv1.WebhookConfig, slack *notificationsv1.SlackConfig) (notifications.Channel, error) {
	dk, err := protoToChannelKind(kind)
	if err != nil {
		return notifications.Channel{}, err
	}
	dom := notifications.Channel{ID: id, Name: name, Kind: dk}
	if wh != nil {
		dom.Webhook = &notifications.WebhookConfig{URL: wh.GetUrl(), BearerHeader: wh.GetBearerHeader()}
	}
	if slack != nil {
		dom.Slack = &notifications.SlackConfig{WebhookURL: slack.GetWebhookUrl()}
	}
	return dom, nil
}

func ruleToProto(r notifications.Rule) *notificationsv1.Rule {
	return &notificationsv1.Rule{
		Id:              r.ID,
		OrganizationId:  r.OrganizationID,
		Name:            r.Name,
		Template:        ruleTemplateToProto(r.Template),
		Group:           r.Group,
		Severity:        r.Severity,
		Summary:         r.Summary,
		Description:     r.Description,
		DurationSeconds: r.DurationSeconds,
		Enabled:         r.Enabled,
	}
}

func maintenanceWindowToProto(s notifications.MaintenanceWindow) *notificationsv1.MaintenanceWindow {
	out := &notificationsv1.MaintenanceWindow{
		Id:             s.ID,
		OrganizationId: s.OrganizationID,
		Scope:          scopeToProto(s.Scope),
		StartsAt:       timestamppb.New(s.StartsAt),
		Comment:        s.Comment,
		CreatedBy:      s.CreatedBy,
		CreatedAt:      timestamppb.New(s.CreatedAt),
		Active:         s.Active,
	}
	if !s.EndsAt.IsZero() {
		out.EndsAt = timestamppb.New(s.EndsAt)
	}
	return out
}

func scopeToProto(sc notifications.MaintenanceWindowScope) *notificationsv1.MaintenanceWindowScope {
	return &notificationsv1.MaintenanceWindowScope{
		Kind:      scopeKindToProto(sc.Kind),
		RuleId:    sc.RuleID,
		GroupId:   sc.GroupID,
		SiteId:    sc.SiteID,
		DeviceIds: sc.DeviceIDs,
	}
}

func protoToMaintenanceWindow(id string, scope *notificationsv1.MaintenanceWindowScope, startsAt, endsAt *timestamppb.Timestamp, comment string) (notifications.MaintenanceWindow, error) {
	if scope == nil {
		return notifications.MaintenanceWindow{}, fleeterror.NewInvalidArgumentError("scope is required")
	}
	dk, err := protoToScopeKind(scope.GetKind())
	if err != nil {
		return notifications.MaintenanceWindow{}, err
	}
	if startsAt == nil {
		return notifications.MaintenanceWindow{}, fleeterror.NewInvalidArgumentError("starts_at is required")
	}
	dom := notifications.MaintenanceWindow{
		ID: id,
		Scope: notifications.MaintenanceWindowScope{
			Kind:      dk,
			RuleID:    scope.GetRuleId(),
			GroupID:   scope.GetGroupId(),
			SiteID:    scope.GetSiteId(),
			DeviceIDs: scope.GetDeviceIds(),
		},
		StartsAt: startsAt.AsTime(),
		Comment:  comment,
	}
	if endsAt != nil {
		dom.EndsAt = endsAt.AsTime()
	}
	return dom, nil
}

// includeDevice gates miner data behind miner:read: the structured device fields plus the free-text summary/template,
// which are sourced from alert annotations and routinely name the device. Rule-level fields stay visible to any notification:read caller.
func historyEntryToProto(n notificationhistory.StoredNotification, includeDevice bool) *notificationsv1.NotificationHistoryEntry {
	out := &notificationsv1.NotificationHistoryEntry{
		Id:          strconv.FormatInt(n.ID, 10),
		ReceivedAt:  timestamppb.New(n.ReceivedAt),
		AlertName:   n.AlertName,
		Status:      n.Status,
		Severity:    n.Severity,
		RuleGroup:   n.RuleGroup,
		Fingerprint: n.Fingerprint,
	}
	if includeDevice {
		out.DeviceId = n.DeviceID
		out.DeviceName = n.DeviceName
		out.DeviceMac = n.DeviceMAC
		out.Template = n.Template
		out.Summary = n.Summary
	}
	if n.StartsAt != nil {
		out.StartsAt = timestamppb.New(*n.StartsAt)
	}
	if n.EndsAt != nil {
		out.EndsAt = timestamppb.New(*n.EndsAt)
	}
	return out
}

func channelKindToProto(k notifications.ChannelKind) notificationsv1.ChannelKind {
	switch k {
	case notifications.ChannelKindWebhook:
		return notificationsv1.ChannelKind_CHANNEL_KIND_WEBHOOK
	case notifications.ChannelKindSlack:
		return notificationsv1.ChannelKind_CHANNEL_KIND_SLACK
	}
	return notificationsv1.ChannelKind_CHANNEL_KIND_UNSPECIFIED
}

func protoToChannelKind(k notificationsv1.ChannelKind) (notifications.ChannelKind, error) {
	switch k {
	case notificationsv1.ChannelKind_CHANNEL_KIND_WEBHOOK:
		return notifications.ChannelKindWebhook, nil
	case notificationsv1.ChannelKind_CHANNEL_KIND_SLACK:
		return notifications.ChannelKindSlack, nil
	// SMTP is not offered in this slice; it ships in the SMTP channel slice.
	case notificationsv1.ChannelKind_CHANNEL_KIND_UNSPECIFIED, notificationsv1.ChannelKind_CHANNEL_KIND_SMTP:
	}
	return "", fleeterror.NewInvalidArgumentErrorf("unknown channel kind: %s", k)
}

func httpStatusToInt32(code int) int32 {
	if code < 0 {
		return 0
	}
	if code > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(code)
}

func validationStateToProto(s notifications.ValidationState) notificationsv1.ValidationState {
	switch s {
	case notifications.ValidationPending:
		return notificationsv1.ValidationState_VALIDATION_STATE_PENDING
	case notifications.ValidationOK:
		return notificationsv1.ValidationState_VALIDATION_STATE_OK
	case notifications.ValidationFailed:
		return notificationsv1.ValidationState_VALIDATION_STATE_FAILED
	}
	return notificationsv1.ValidationState_VALIDATION_STATE_UNSPECIFIED
}

func ruleTemplateToProto(t notifications.RuleTemplate) notificationsv1.RuleTemplate {
	switch t {
	case notifications.RuleTemplateOffline:
		return notificationsv1.RuleTemplate_RULE_TEMPLATE_OFFLINE
	case notifications.RuleTemplateHashrate:
		return notificationsv1.RuleTemplate_RULE_TEMPLATE_HASHRATE
	case notifications.RuleTemplateTemperature:
		return notificationsv1.RuleTemplate_RULE_TEMPLATE_TEMPERATURE
	case notifications.RuleTemplatePool:
		return notificationsv1.RuleTemplate_RULE_TEMPLATE_POOL
	case notifications.RuleTemplateCommandFailure:
		return notificationsv1.RuleTemplate_RULE_TEMPLATE_COMMAND_FAILURE
	case notifications.RuleTemplateTelemetryPoll:
		return notificationsv1.RuleTemplate_RULE_TEMPLATE_TELEMETRY_POLL
	}
	return notificationsv1.RuleTemplate_RULE_TEMPLATE_UNSPECIFIED
}

func scopeKindToProto(k notifications.MaintenanceWindowScopeKind) notificationsv1.MaintenanceWindowScopeKind {
	switch k {
	case notifications.MaintenanceWindowScopeRule:
		return notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_RULE
	case notifications.MaintenanceWindowScopeGroup:
		return notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_GROUP
	case notifications.MaintenanceWindowScopeSite:
		return notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_SITE
	case notifications.MaintenanceWindowScopeDevice:
		return notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_DEVICE
	}
	return notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_UNSPECIFIED
}

func protoToScopeKind(k notificationsv1.MaintenanceWindowScopeKind) (notifications.MaintenanceWindowScopeKind, error) {
	switch k {
	case notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_RULE:
		return notifications.MaintenanceWindowScopeRule, nil
	case notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_GROUP:
		return notifications.MaintenanceWindowScopeGroup, nil
	case notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_SITE:
		return notifications.MaintenanceWindowScopeSite, nil
	case notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_DEVICE:
		return notifications.MaintenanceWindowScopeDevice, nil
	case notificationsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_UNSPECIFIED:
	}
	return "", fleeterror.NewInvalidArgumentErrorf("unknown maintenance window scope kind: %s", k)
}
