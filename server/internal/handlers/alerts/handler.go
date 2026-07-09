package alerts

import (
	"context"
	"errors"
	"math"
	"strconv"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	alertsv1 "github.com/block/proto-fleet/server/generated/grpc/alerts/v1"
	"github.com/block/proto-fleet/server/generated/grpc/alerts/v1/alertsv1connect"
	alerts "github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	svc     *alerts.Service
	history notificationhistory.Lister
}

func NewHandler(svc *alerts.Service, history notificationhistory.Lister) *Handler {
	return &Handler{svc: svc, history: history}
}

var (
	_ alertsv1connect.ChannelServiceHandler           = (*Handler)(nil)
	_ alertsv1connect.RuleServiceHandler              = (*Handler)(nil)
	_ alertsv1connect.MaintenanceWindowServiceHandler = (*Handler)(nil)
	_ alertsv1connect.HistoryServiceHandler           = (*Handler)(nil)
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

// requireMinerRead gates channel mutations behind org-wide miner:read: a saved channel delivers
// device identity (id/name/MAC) for the whole org, so a caller whose miner:read is narrowed to a
// subset of sites must not be able to route other sites' device data to an external destination.
func (h *Handler) requireMinerRead(ctx context.Context) error {
	_, err := middleware.RequireOrgWidePermission(ctx, authz.PermMinerRead)
	return err
}

func mapErr(err error) error {
	if errors.Is(err, alerts.ErrNotFound) {
		return fleeterror.NewNotFoundError(err.Error())
	}
	return err
}

func (h *Handler) ListChannels(ctx context.Context, _ *connect.Request[alertsv1.ListChannelsRequest]) (*connect.Response[alertsv1.ListChannelsResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertRead)
	if err != nil {
		return nil, err
	}
	channels, err := h.svc.ListChannels(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*alertsv1.Channel, 0, len(channels))
	for _, c := range channels {
		out = append(out, channelToProto(c))
	}
	return connect.NewResponse(&alertsv1.ListChannelsResponse{Channels: out}), nil
}

func (h *Handler) CreateChannel(ctx context.Context, req *connect.Request[alertsv1.CreateChannelRequest]) (*connect.Response[alertsv1.CreateChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	if err := h.requireMinerRead(ctx); err != nil {
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
	return connect.NewResponse(&alertsv1.CreateChannelResponse{Channel: channelToProto(*created)}), nil
}

func (h *Handler) UpdateChannel(ctx context.Context, req *connect.Request[alertsv1.UpdateChannelRequest]) (*connect.Response[alertsv1.UpdateChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	if err := h.requireMinerRead(ctx); err != nil {
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
	return connect.NewResponse(&alertsv1.UpdateChannelResponse{Channel: channelToProto(*updated)}), nil
}

func (h *Handler) DeleteChannel(ctx context.Context, req *connect.Request[alertsv1.DeleteChannelRequest]) (*connect.Response[alertsv1.DeleteChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteChannel(ctx, orgID, req.Msg.GetId()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&alertsv1.DeleteChannelResponse{}), nil
}

func (h *Handler) TestChannel(ctx context.Context, req *connect.Request[alertsv1.TestChannelRequest]) (*connect.Response[alertsv1.TestChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	// A saved-channel test needs only the id; TestChannel loads the stored contact point and ignores kind/config.
	dom := alerts.Channel{ID: req.Msg.GetId()}
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
	return connect.NewResponse(&alertsv1.TestChannelResponse{
		Ok:           ok,
		Error:        errMsg,
		ResponseCode: httpStatusToInt32(code),
	}), nil
}

func (h *Handler) ListRules(ctx context.Context, _ *connect.Request[alertsv1.ListRulesRequest]) (*connect.Response[alertsv1.ListRulesResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertRead)
	if err != nil {
		return nil, err
	}
	rules, err := h.svc.ListRules(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*alertsv1.Rule, 0, len(rules))
	for _, r := range rules {
		out = append(out, ruleToProto(r))
	}
	return connect.NewResponse(&alertsv1.ListRulesResponse{Rules: out}), nil
}

func (h *Handler) PauseRule(ctx context.Context, req *connect.Request[alertsv1.PauseRuleRequest]) (*connect.Response[alertsv1.PauseRuleResponse], error) {
	orgID, actor, err := h.authorizeActor(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	rule, err := h.svc.PauseRule(ctx, orgID, req.Msg.GetId(), actor)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&alertsv1.PauseRuleResponse{Rule: ruleToProto(*rule)}), nil
}

func (h *Handler) ResumeRule(ctx context.Context, req *connect.Request[alertsv1.ResumeRuleRequest]) (*connect.Response[alertsv1.ResumeRuleResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	rule, err := h.svc.ResumeRule(ctx, orgID, req.Msg.GetId())
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&alertsv1.ResumeRuleResponse{Rule: ruleToProto(*rule)}), nil
}

func (h *Handler) ListMaintenanceWindows(ctx context.Context, _ *connect.Request[alertsv1.ListMaintenanceWindowsRequest]) (*connect.Response[alertsv1.ListMaintenanceWindowsResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertRead)
	if err != nil {
		return nil, err
	}
	silences, err := h.svc.ListMaintenanceWindows(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*alertsv1.MaintenanceWindow, 0, len(silences))
	for _, s := range silences {
		out = append(out, maintenanceWindowToProto(s))
	}
	return connect.NewResponse(&alertsv1.ListMaintenanceWindowsResponse{MaintenanceWindows: out}), nil
}

func (h *Handler) CreateMaintenanceWindow(ctx context.Context, req *connect.Request[alertsv1.CreateMaintenanceWindowRequest]) (*connect.Response[alertsv1.CreateMaintenanceWindowResponse], error) {
	orgID, actor, err := h.authorizeActor(ctx, authz.PermAlertManage)
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
	return connect.NewResponse(&alertsv1.CreateMaintenanceWindowResponse{MaintenanceWindow: maintenanceWindowToProto(*created)}), nil
}

func (h *Handler) UpdateMaintenanceWindow(ctx context.Context, req *connect.Request[alertsv1.UpdateMaintenanceWindowRequest]) (*connect.Response[alertsv1.UpdateMaintenanceWindowResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
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
	return connect.NewResponse(&alertsv1.UpdateMaintenanceWindowResponse{MaintenanceWindow: maintenanceWindowToProto(*updated)}), nil
}

func (h *Handler) DeleteMaintenanceWindow(ctx context.Context, req *connect.Request[alertsv1.DeleteMaintenanceWindowRequest]) (*connect.Response[alertsv1.DeleteMaintenanceWindowResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteMaintenanceWindow(ctx, orgID, req.Msg.GetId()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&alertsv1.DeleteMaintenanceWindowResponse{}), nil
}

func (h *Handler) ListAlerts(ctx context.Context, req *connect.Request[alertsv1.ListAlertsRequest]) (*connect.Response[alertsv1.ListAlertsResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertRead)
	if err != nil {
		return nil, err
	}
	// Device identity (id/name/mac) is miner data, so gate those fields on org-scope miner:read rather than leaking them via alert:read.
	includeDevice, err := middleware.HasPermission(ctx, authz.PermMinerRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	var rows []notificationhistory.StoredNotification
	var pageLimit int32
	if req.Msg.GetActiveOnly() {
		// Active-only is a current-state view, not a feed: return the latest firing row per alert without keyset
		// paging. Over-fetch by one so the response can flag (rather than silently swallow) an alert storm past the cap.
		pageLimit = historyMaxPageSize
		rows, err = h.history.ListActive(ctx, orgID, pageLimit+1)
	} else {
		pageLimit = req.Msg.GetPageSize()
		if pageLimit <= 0 {
			pageLimit = historyDefaultPageSize
		}
		if pageLimit > historyMaxPageSize {
			pageLimit = historyMaxPageSize
		}
		var beforeID *int64
		if s := req.Msg.GetBeforeId(); s != "" {
			v, parseErr := strconv.ParseInt(s, 10, 64)
			if parseErr != nil {
				return nil, fleeterror.NewInvalidArgumentError("invalid before_id: " + s)
			}
			beforeID = &v
		}
		rows, err = h.history.List(ctx, orgID, beforeID, pageLimit+1)
	}
	if err != nil {
		return nil, err
	}

	hasMore := len(rows) > int(pageLimit)
	if hasMore {
		rows = rows[:pageLimit]
	}
	out := make([]*alertsv1.AlertHistoryEntry, 0, len(rows))
	for _, n := range rows {
		out = append(out, historyEntryToProto(n, includeDevice))
	}
	return connect.NewResponse(&alertsv1.ListAlertsResponse{
		Alerts:  out,
		HasMore: hasMore,
	}), nil
}

func channelToProto(c alerts.Channel) *alertsv1.Channel {
	out := &alertsv1.Channel{
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
		out.Webhook = &alertsv1.WebhookConfig{Url: c.Webhook.URL}
	}
	if c.Slack != nil {
		// webhook_url deliberately omitted: it's the secret.
		out.Slack = &alertsv1.SlackConfig{}
	}
	return out
}

func protoToChannel(id, name string, kind alertsv1.ChannelKind, wh *alertsv1.WebhookConfig, slack *alertsv1.SlackConfig) (alerts.Channel, error) {
	dk, err := protoToChannelKind(kind)
	if err != nil {
		return alerts.Channel{}, err
	}
	dom := alerts.Channel{ID: id, Name: name, Kind: dk}
	if wh != nil {
		dom.Webhook = &alerts.WebhookConfig{URL: wh.GetUrl(), BearerHeader: wh.GetBearerHeader(), ClearBearer: wh.GetClearBearerHeader()}
	}
	if slack != nil {
		dom.Slack = &alerts.SlackConfig{WebhookURL: slack.GetWebhookUrl()}
	}
	return dom, nil
}

func ruleToProto(r alerts.Rule) *alertsv1.Rule {
	return &alertsv1.Rule{
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

func maintenanceWindowToProto(s alerts.MaintenanceWindow) *alertsv1.MaintenanceWindow {
	out := &alertsv1.MaintenanceWindow{
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

func scopeToProto(sc alerts.MaintenanceWindowScope) *alertsv1.MaintenanceWindowScope {
	return &alertsv1.MaintenanceWindowScope{
		Kind:      scopeKindToProto(sc.Kind),
		RuleId:    sc.RuleID,
		GroupId:   sc.GroupID,
		SiteId:    sc.SiteID,
		DeviceIds: sc.DeviceIDs,
	}
}

func protoToMaintenanceWindow(id string, scope *alertsv1.MaintenanceWindowScope, startsAt, endsAt *timestamppb.Timestamp, comment string) (alerts.MaintenanceWindow, error) {
	if scope == nil {
		return alerts.MaintenanceWindow{}, fleeterror.NewInvalidArgumentError("scope is required")
	}
	dk, err := protoToScopeKind(scope.GetKind())
	if err != nil {
		return alerts.MaintenanceWindow{}, err
	}
	if startsAt == nil {
		return alerts.MaintenanceWindow{}, fleeterror.NewInvalidArgumentError("starts_at is required")
	}
	dom := alerts.MaintenanceWindow{
		ID: id,
		Scope: alerts.MaintenanceWindowScope{
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

// includeDevice gates miner data behind miner:read: the structured device fields plus the free-text summary,
// which is sourced from alert annotations and routinely names the device. Rule-level fields — including the
// template label, a rule-type slug the rules list already exposes — stay visible to any alert:read caller.
func historyEntryToProto(n notificationhistory.StoredNotification, includeDevice bool) *alertsv1.AlertHistoryEntry {
	out := &alertsv1.AlertHistoryEntry{
		Id:          strconv.FormatInt(n.ID, 10),
		ReceivedAt:  timestamppb.New(n.ReceivedAt),
		AlertName:   n.AlertName,
		Status:      n.Status,
		Severity:    n.Severity,
		RuleGroup:   n.RuleGroup,
		Fingerprint: n.Fingerprint,
		Template:    n.Template,
	}
	if includeDevice {
		out.DeviceId = n.DeviceID
		out.DeviceName = n.DeviceName
		out.DeviceMac = n.DeviceMAC
	}
	// Summary follows the template's scope: on device templates it names the
	// miner, on source-level templates only the MQTT source, which is not
	// miner identity and stays visible to any alert:read caller.
	if includeDevice || isSourceLevelTemplate(n.Template) {
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

// isSourceLevelTemplate reports whether the template scopes the alert to an
// MQTT curtailment source rather than a device. All rules are operator-
// provisioned, so the template label is trustworthy.
func isSourceLevelTemplate(t string) bool {
	tmpl := alerts.RuleTemplate(t)
	return tmpl == alerts.RuleTemplateMQTTCurtailment || tmpl == alerts.RuleTemplateMQTTDisconnected
}

func channelKindToProto(k alerts.ChannelKind) alertsv1.ChannelKind {
	switch k {
	case alerts.ChannelKindWebhook:
		return alertsv1.ChannelKind_CHANNEL_KIND_WEBHOOK
	case alerts.ChannelKindSlack:
		return alertsv1.ChannelKind_CHANNEL_KIND_SLACK
	}
	return alertsv1.ChannelKind_CHANNEL_KIND_UNSPECIFIED
}

func protoToChannelKind(k alertsv1.ChannelKind) (alerts.ChannelKind, error) {
	switch k {
	case alertsv1.ChannelKind_CHANNEL_KIND_WEBHOOK:
		return alerts.ChannelKindWebhook, nil
	case alertsv1.ChannelKind_CHANNEL_KIND_SLACK:
		return alerts.ChannelKindSlack, nil
	// SMTP is not offered in this slice; it ships in the SMTP channel slice.
	case alertsv1.ChannelKind_CHANNEL_KIND_UNSPECIFIED, alertsv1.ChannelKind_CHANNEL_KIND_SMTP:
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

func validationStateToProto(s alerts.ValidationState) alertsv1.ValidationState {
	switch s {
	case alerts.ValidationPending:
		return alertsv1.ValidationState_VALIDATION_STATE_PENDING
	case alerts.ValidationOK:
		return alertsv1.ValidationState_VALIDATION_STATE_OK
	case alerts.ValidationFailed:
		return alertsv1.ValidationState_VALIDATION_STATE_FAILED
	}
	return alertsv1.ValidationState_VALIDATION_STATE_UNSPECIFIED
}

func ruleTemplateToProto(t alerts.RuleTemplate) alertsv1.RuleTemplate {
	switch t {
	case alerts.RuleTemplateOffline:
		return alertsv1.RuleTemplate_RULE_TEMPLATE_OFFLINE
	case alerts.RuleTemplateHashrate:
		return alertsv1.RuleTemplate_RULE_TEMPLATE_HASHRATE
	case alerts.RuleTemplateTemperature:
		return alertsv1.RuleTemplate_RULE_TEMPLATE_TEMPERATURE
	case alerts.RuleTemplatePool:
		return alertsv1.RuleTemplate_RULE_TEMPLATE_POOL
	case alerts.RuleTemplateCommandFailure:
		return alertsv1.RuleTemplate_RULE_TEMPLATE_COMMAND_FAILURE
	case alerts.RuleTemplateTelemetryPoll:
		return alertsv1.RuleTemplate_RULE_TEMPLATE_TELEMETRY_POLL
	case alerts.RuleTemplateMQTTCurtailment:
		return alertsv1.RuleTemplate_RULE_TEMPLATE_MQTT_CURTAILMENT
	case alerts.RuleTemplateMQTTDisconnected:
		return alertsv1.RuleTemplate_RULE_TEMPLATE_MQTT_DISCONNECTED
	}
	return alertsv1.RuleTemplate_RULE_TEMPLATE_UNSPECIFIED
}

func scopeKindToProto(k alerts.MaintenanceWindowScopeKind) alertsv1.MaintenanceWindowScopeKind {
	switch k {
	case alerts.MaintenanceWindowScopeRule:
		return alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_RULE
	case alerts.MaintenanceWindowScopeGroup:
		return alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_GROUP
	case alerts.MaintenanceWindowScopeSite:
		return alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_SITE
	case alerts.MaintenanceWindowScopeDevice:
		return alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_DEVICE
	}
	return alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_UNSPECIFIED
}

func protoToScopeKind(k alertsv1.MaintenanceWindowScopeKind) (alerts.MaintenanceWindowScopeKind, error) {
	switch k {
	case alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_RULE:
		return alerts.MaintenanceWindowScopeRule, nil
	case alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_GROUP:
		return alerts.MaintenanceWindowScopeGroup, nil
	case alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_SITE:
		return alerts.MaintenanceWindowScopeSite, nil
	case alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_DEVICE:
		return alerts.MaintenanceWindowScopeDevice, nil
	case alertsv1.MaintenanceWindowScopeKind_MAINTENANCE_WINDOW_SCOPE_KIND_UNSPECIFIED:
	}
	return "", fleeterror.NewInvalidArgumentErrorf("unknown maintenance window scope kind: %s", k)
}
