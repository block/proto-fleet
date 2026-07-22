package alerts

import (
	"context"
	"errors"
	"math"
	"net/http"
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

// requireMinerRead gates channel and rule mutations behind org-wide miner:read: both surfaces
// deliver device identity (id/name/MAC) for the whole org, so a caller whose miner:read is
// narrowed to a subset of sites must not be able to route other sites' device data outward.
func (h *Handler) requireMinerRead(ctx context.Context) error {
	_, err := middleware.RequireOrgWidePermission(ctx, authz.PermMinerRead)
	return err
}

func mapErr(err error) error {
	if errors.Is(err, alerts.ErrNotFound) {
		return fleeterror.NewNotFoundError(err.Error())
	}
	// Surface Grafana contract rejections (e.g. duplicate rule titles) as
	// client errors instead of opaque internals; messages are pre-redacted.
	var ge *alerts.GrafanaError
	if errors.As(err, &ge) {
		switch ge.StatusCode {
		case http.StatusConflict:
			return fleeterror.NewAlreadyExistsError(ge.Message)
		case http.StatusBadRequest:
			return fleeterror.NewInvalidArgumentError(ge.Message)
		case http.StatusNotFound:
			return fleeterror.NewNotFoundError(ge.Message)
		}
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

// Rule create/update mirror channel mutations' requireMinerRead: a rule
// evaluates every org device and fans per-device alerts out to channels.
func (h *Handler) CreateRule(ctx context.Context, req *connect.Request[alertsv1.CreateRuleRequest]) (*connect.Response[alertsv1.CreateRuleResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	if err := h.requireMinerRead(ctx); err != nil {
		return nil, err
	}
	cfg, err := protoToRuleConfig(req.Msg.GetConfig())
	if err != nil {
		return nil, err
	}
	// Absent routing means default; a present-but-unspecified mode is a client bug and rejected.
	mode := alerts.RouteModeDefault
	var channelIDs []string
	if routing := req.Msg.GetRouting(); routing != nil {
		mode, channelIDs, err = protoToRouting(routing)
		if err != nil {
			return nil, err
		}
	}
	rule, err := h.svc.CreateRule(ctx, orgID, cfg, mode, channelIDs)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&alertsv1.CreateRuleResponse{Rule: ruleToProto(*rule)}), nil
}

func (h *Handler) UpdateRule(ctx context.Context, req *connect.Request[alertsv1.UpdateRuleRequest]) (*connect.Response[alertsv1.UpdateRuleResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	if err := h.requireMinerRead(ctx); err != nil {
		return nil, err
	}
	cfg, err := protoToRuleConfig(req.Msg.GetConfig())
	if err != nil {
		return nil, err
	}
	rule, err := h.svc.UpdateRule(ctx, orgID, req.Msg.GetId(), cfg)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&alertsv1.UpdateRuleResponse{Rule: ruleToProto(*rule)}), nil
}

// SetRuleRouting mirrors channel mutations' requireMinerRead: routing decides which destinations receive the org's per-device alert stream.
func (h *Handler) SetRuleRouting(ctx context.Context, req *connect.Request[alertsv1.SetRuleRoutingRequest]) (*connect.Response[alertsv1.SetRuleRoutingResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	if err := h.requireMinerRead(ctx); err != nil {
		return nil, err
	}
	mode, channelIDs, err := protoToRouting(req.Msg.GetRouting())
	if err != nil {
		return nil, err
	}
	rule, err := h.svc.SetRuleRouting(ctx, orgID, req.Msg.GetRuleId(), mode, channelIDs)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&alertsv1.SetRuleRoutingResponse{Rule: ruleToProto(*rule)}), nil
}

func (h *Handler) DeleteRule(ctx context.Context, req *connect.Request[alertsv1.DeleteRuleRequest]) (*connect.Response[alertsv1.DeleteRuleResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermAlertManage)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteRule(ctx, orgID, req.Msg.GetId()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&alertsv1.DeleteRuleResponse{}), nil
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
	out := &alertsv1.Rule{
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
		Origin:          ruleOriginToProto(r.Origin),
	}
	if r.Config != nil {
		out.Config = ruleConfigToProto(*r.Config)
	}
	out.Routing = routingToProto(r.Routing)
	return out
}

func routingToProto(p *alerts.RoutePolicy) *alertsv1.RuleRouting {
	out := &alertsv1.RuleRouting{Mode: alertsv1.RoutingMode_ROUTING_MODE_DEFAULT}
	switch {
	case p == nil:
	case p.Mode == alerts.RouteModeCustom:
		out.Mode = alertsv1.RoutingMode_ROUTING_MODE_CUSTOM
		out.ChannelIds = make([]string, 0, len(p.ChannelIDs))
		for _, id := range p.ChannelIDs {
			out.ChannelIds = append(out.ChannelIds, strconv.FormatInt(id, 10))
		}
	case p.Mode == alerts.RouteModeNone:
		out.Mode = alertsv1.RoutingMode_ROUTING_MODE_NONE
	}
	return out
}

// protoToRouting maps a RuleRouting message; nil-safe (nil reads as unspecified mode and is rejected).
func protoToRouting(r *alertsv1.RuleRouting) (alerts.RouteMode, []string, error) {
	mode, err := protoToRouteMode(r.GetMode())
	if err != nil {
		return "", nil, err
	}
	return mode, r.GetChannelIds(), nil
}

func protoToRouteMode(m alertsv1.RoutingMode) (alerts.RouteMode, error) {
	switch m {
	case alertsv1.RoutingMode_ROUTING_MODE_DEFAULT:
		return alerts.RouteModeDefault, nil
	case alertsv1.RoutingMode_ROUTING_MODE_CUSTOM:
		return alerts.RouteModeCustom, nil
	case alertsv1.RoutingMode_ROUTING_MODE_NONE:
		return alerts.RouteModeNone, nil
	case alertsv1.RoutingMode_ROUTING_MODE_UNSPECIFIED:
	}
	return "", fleeterror.NewInvalidArgumentError("routing mode is required")
}

func ruleOriginToProto(o alerts.RuleOrigin) alertsv1.RuleOrigin {
	switch o {
	case alerts.RuleOriginUser:
		return alertsv1.RuleOrigin_RULE_ORIGIN_USER
	case alerts.RuleOriginProvisioned:
		return alertsv1.RuleOrigin_RULE_ORIGIN_PROVISIONED
	}
	return alertsv1.RuleOrigin_RULE_ORIGIN_UNSPECIFIED
}

func ruleConfigToProto(c alerts.RuleConfig) *alertsv1.RuleConfig {
	out := &alertsv1.RuleConfig{
		Name:            c.Name,
		DurationSeconds: c.DurationSeconds,
	}
	switch {
	case c.Offline != nil:
		out.TemplateConfig = &alertsv1.RuleConfig_Offline{Offline: &alertsv1.OfflineConfig{}}
	case c.Hashrate != nil:
		out.TemplateConfig = &alertsv1.RuleConfig_Hashrate{Hashrate: &alertsv1.HashrateConfig{
			Mode:  hashrateModeToProto(c.Hashrate.Mode),
			Value: c.Hashrate.Value,
			Unit:  hashrateUnitToProto(c.Hashrate.Unit),
		}}
	case c.Temperature != nil:
		out.TemplateConfig = &alertsv1.RuleConfig_Temperature{Temperature: &alertsv1.TemperatureConfig{
			MaxCelsius: c.Temperature.MaxCelsius,
		}}
	}
	return out
}

func protoToRuleConfig(c *alertsv1.RuleConfig) (alerts.RuleConfig, error) {
	if c == nil {
		return alerts.RuleConfig{}, fleeterror.NewInvalidArgumentError("rule config is required")
	}
	out := alerts.RuleConfig{
		Name:            c.GetName(),
		DurationSeconds: c.GetDurationSeconds(),
	}
	switch tc := c.GetTemplateConfig().(type) {
	case *alertsv1.RuleConfig_Offline:
		out.Offline = &alerts.OfflineRuleConfig{}
	case *alertsv1.RuleConfig_Hashrate:
		mode, err := protoToHashrateMode(tc.Hashrate.GetMode())
		if err != nil {
			return alerts.RuleConfig{}, err
		}
		out.Hashrate = &alerts.HashrateRuleConfig{
			Mode:  mode,
			Value: tc.Hashrate.GetValue(),
			Unit:  protoToHashrateUnit(tc.Hashrate.GetUnit()),
		}
	case *alertsv1.RuleConfig_Temperature:
		out.Temperature = &alerts.TemperatureRuleConfig{MaxCelsius: tc.Temperature.GetMaxCelsius()}
	default:
		return alerts.RuleConfig{}, fleeterror.NewInvalidArgumentError("rule template config is required")
	}
	return out, nil
}

func hashrateModeToProto(m alerts.HashrateMode) alertsv1.HashrateMode {
	switch m {
	case alerts.HashrateModePctExpected:
		return alertsv1.HashrateMode_HASHRATE_MODE_PCT_EXPECTED
	case alerts.HashrateModeAbsolute:
		return alertsv1.HashrateMode_HASHRATE_MODE_ABSOLUTE
	}
	return alertsv1.HashrateMode_HASHRATE_MODE_UNSPECIFIED
}

func protoToHashrateMode(m alertsv1.HashrateMode) (alerts.HashrateMode, error) {
	switch m {
	case alertsv1.HashrateMode_HASHRATE_MODE_PCT_EXPECTED:
		return alerts.HashrateModePctExpected, nil
	case alertsv1.HashrateMode_HASHRATE_MODE_ABSOLUTE:
		return alerts.HashrateModeAbsolute, nil
	case alertsv1.HashrateMode_HASHRATE_MODE_UNSPECIFIED:
	}
	return "", fleeterror.NewInvalidArgumentError("hashrate mode is required")
}

func hashrateUnitToProto(u alerts.HashrateUnit) alertsv1.HashrateUnit {
	switch u {
	case alerts.HashrateUnitTerahash:
		return alertsv1.HashrateUnit_HASHRATE_UNIT_TERAHASH
	case alerts.HashrateUnitPetahash:
		return alertsv1.HashrateUnit_HASHRATE_UNIT_PETAHASH
	}
	return alertsv1.HashrateUnit_HASHRATE_UNIT_UNSPECIFIED
}

func protoToHashrateUnit(u alertsv1.HashrateUnit) alerts.HashrateUnit {
	switch u {
	case alertsv1.HashrateUnit_HASHRATE_UNIT_TERAHASH:
		return alerts.HashrateUnitTerahash
	case alertsv1.HashrateUnit_HASHRATE_UNIT_PETAHASH:
		return alerts.HashrateUnitPetahash
	case alertsv1.HashrateUnit_HASHRATE_UNIT_UNSPECIFIED:
	}
	return ""
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
// MQTT curtailment source rather than a device. The label stays trustworthy:
// user rules only emit offline/hashrate/temperature (compileUserRule).
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
