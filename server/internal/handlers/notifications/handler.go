// Package notifications implements the generated notifications.v1
// Connect service handlers (ChannelService, RuleService,
// SilenceService, HistoryService) on top of the notifications domain
// service and the notification-history store.
//
// These replace an earlier hand-written JSON surface. Mounting the
// generated handlers means the routes now speak the real protobuf /
// Connect wire contract (protojson field names, enum constants), run
// through the shared interceptor chain (authentication, buf.validate
// request validation, error mapping, request-log redaction), and are
// reachable by generated clients.
//
// Security model unchanged from the hand-written version:
//
//   - Authentication is the AuthInterceptor's job; these procedures
//     are registered session-only (no API key) in
//     interceptors.SessionOnlyProcedures.
//   - Every method calls middleware.RequirePermission — reads on
//     notification:read, every mutation (including TestChannel, which
//     triggers an outbound delivery) on notification:manage.
//   - Org scoping, secret redaction, SSRF destination checks, silence
//     scope/device-id validation, and secret-carry-on-update all live
//     in the domain service and are exercised unchanged here.
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

// Handler implements all four notifications Connect services.
type Handler struct {
	svc     *notifications.Service
	history notificationhistory.Lister
}

// NewHandler returns a handler bound to the domain service and the
// notification-history lister.
func NewHandler(svc *notifications.Service, history notificationhistory.Lister) *Handler {
	return &Handler{svc: svc, history: history}
}

var (
	_ notificationsv1connect.ChannelServiceHandler = (*Handler)(nil)
	_ notificationsv1connect.RuleServiceHandler    = (*Handler)(nil)
	_ notificationsv1connect.SilenceServiceHandler = (*Handler)(nil)
	_ notificationsv1connect.HistoryServiceHandler = (*Handler)(nil)
)

const (
	historyDefaultPageSize = 50
	historyMaxPageSize     = 200
)

// authorize runs the per-route RBAC gate and returns the caller's org
// id. RequirePermission resolves the session.Info the AuthInterceptor
// stashed on the context; a zero org id is a wiring fault and fails
// closed.
func (h *Handler) authorize(ctx context.Context, permission string) (int64, error) {
	info, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{})
	if err != nil {
		return 0, err
	}
	if info.OrganizationID == 0 {
		return 0, fleeterror.NewUnauthenticatedError("organization id missing on session")
	}
	return info.OrganizationID, nil
}

// mapErr translates the domain's sentinel ErrNotFound (a plain error)
// into a fleeterror the error-mapping interceptor renders as a Connect
// NotFound; every other error is already a fleeterror (or is mapped to
// Internal by the interceptor).
func mapErr(err error) error {
	if errors.Is(err, notifications.ErrNotFound) {
		return fleeterror.NewNotFoundError(err.Error())
	}
	return err
}

// === ChannelService =====================================================

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
	dom, err := protoToChannel("", req.Msg.GetName(), req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSmtp(), req.Msg.GetSlack())
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
	dom, err := protoToChannel(req.Msg.GetId(), req.Msg.GetName(), req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSmtp(), req.Msg.GetSlack())
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
	// TestChannelRequest carries no name; the domain service uses the
	// stored receiver when an id is present and an org-prefixed
	// synthetic name otherwise.
	dom, err := protoToChannel(req.Msg.GetId(), "", req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSmtp(), req.Msg.GetSlack())
	if err != nil {
		return nil, err
	}
	ok, code, errMsg, err := h.svc.TestChannel(ctx, orgID, dom)
	if err != nil {
		// A genuine service error (unknown/foreign id, invalid
		// destination, Grafana unreachable) surfaces as a Connect error.
		// A reachable Grafana reporting a non-2xx test result comes back
		// in the ok/error/response_code fields.
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.TestChannelResponse{
		Ok:           ok,
		Error:        errMsg,
		ResponseCode: httpStatusToInt32(code),
	}), nil
}

// === RuleService ========================================================

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
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	rule, err := h.svc.PauseRule(ctx, orgID, req.Msg.GetId())
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

// === SilenceService =====================================================

func (h *Handler) ListSilences(ctx context.Context, _ *connect.Request[notificationsv1.ListSilencesRequest]) (*connect.Response[notificationsv1.ListSilencesResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationRead)
	if err != nil {
		return nil, err
	}
	silences, err := h.svc.ListSilences(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*notificationsv1.Silence, 0, len(silences))
	for _, s := range silences {
		out = append(out, silenceToProto(s))
	}
	return connect.NewResponse(&notificationsv1.ListSilencesResponse{Silences: out}), nil
}

func (h *Handler) CreateSilence(ctx context.Context, req *connect.Request[notificationsv1.CreateSilenceRequest]) (*connect.Response[notificationsv1.CreateSilenceResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	dom, err := protoToSilence("", req.Msg.GetScope(), req.Msg.GetStartsAt(), req.Msg.GetEndsAt(), req.Msg.GetComment())
	if err != nil {
		return nil, err
	}
	if info, infoErr := middleware.RequirePermission(ctx, authz.PermNotificationManage, authz.ResourceContext{}); infoErr == nil {
		dom.CreatedBy = info.Username
	}
	created, err := h.svc.CreateSilence(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.CreateSilenceResponse{Silence: silenceToProto(*created)}), nil
}

func (h *Handler) UpdateSilence(ctx context.Context, req *connect.Request[notificationsv1.UpdateSilenceRequest]) (*connect.Response[notificationsv1.UpdateSilenceResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	dom, err := protoToSilence(req.Msg.GetId(), req.Msg.GetScope(), req.Msg.GetStartsAt(), req.Msg.GetEndsAt(), req.Msg.GetComment())
	if err != nil {
		return nil, err
	}
	updated, err := h.svc.UpdateSilence(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.UpdateSilenceResponse{Silence: silenceToProto(*updated)}), nil
}

func (h *Handler) DeleteSilence(ctx context.Context, req *connect.Request[notificationsv1.DeleteSilenceRequest]) (*connect.Response[notificationsv1.DeleteSilenceResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteSilence(ctx, orgID, req.Msg.GetId()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.DeleteSilenceResponse{}), nil
}

// === HistoryService =====================================================

func (h *Handler) ListNotifications(ctx context.Context, req *connect.Request[notificationsv1.ListNotificationsRequest]) (*connect.Response[notificationsv1.ListNotificationsResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationRead)
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
	var beforeID *int64
	if s := req.Msg.GetBeforeId(); s != "" {
		v, parseErr := strconv.ParseInt(s, 10, 64)
		if parseErr != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid before_id: " + s)
		}
		beforeID = &v
	}
	// Fetch one extra row so has_more is exact rather than inferred from
	// a full page.
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
		out = append(out, historyEntryToProto(n))
	}
	return connect.NewResponse(&notificationsv1.ListNotificationsResponse{
		Notifications: out,
		HasMore:       hasMore,
	}), nil
}

// === proto ↔ domain =====================================================

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
		// URL is already redacted to host-only by the domain read path.
		out.Webhook = &notificationsv1.WebhookConfig{Url: c.Webhook.URL}
	}
	if c.SMTP != nil {
		out.Smtp = &notificationsv1.SmtpConfig{
			Host:     c.SMTP.Host,
			Port:     c.SMTP.Port,
			Username: c.SMTP.Username,
			From:     c.SMTP.From,
			To:       c.SMTP.To,
		}
	}
	if c.Slack != nil {
		// webhook_url deliberately omitted — it's the secret.
		out.Slack = &notificationsv1.SlackConfig{}
	}
	return out
}

// protoToChannel builds a domain Channel from the channel fields shared
// by Create / Update / Test requests. id and name are empty when the
// caller's request type doesn't carry them.
func protoToChannel(id, name string, kind notificationsv1.ChannelKind, wh *notificationsv1.WebhookConfig, smtp *notificationsv1.SmtpConfig, slack *notificationsv1.SlackConfig) (notifications.Channel, error) {
	dk, err := protoToChannelKind(kind)
	if err != nil {
		return notifications.Channel{}, err
	}
	dom := notifications.Channel{ID: id, Name: name, Kind: dk}
	if wh != nil {
		dom.Webhook = &notifications.WebhookConfig{URL: wh.GetUrl(), BearerHeader: wh.GetBearerHeader()}
	}
	if smtp != nil {
		dom.SMTP = &notifications.SMTPConfig{
			Host:     smtp.GetHost(),
			Port:     smtp.GetPort(),
			Username: smtp.GetUsername(),
			From:     smtp.GetFrom(),
			To:       smtp.GetTo(),
			Password: smtp.GetPassword(),
		}
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

func silenceToProto(s notifications.Silence) *notificationsv1.Silence {
	out := &notificationsv1.Silence{
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

func scopeToProto(sc notifications.SilenceScope) *notificationsv1.SilenceScope {
	return &notificationsv1.SilenceScope{
		Kind:      scopeKindToProto(sc.Kind),
		RuleId:    sc.RuleID,
		GroupId:   sc.GroupID,
		SiteId:    sc.SiteID,
		DeviceIds: sc.DeviceIDs,
	}
}

// protoToSilence builds a domain Silence from the fields shared by
// Create / Update requests. The domain service validates scope targets
// and device ids; starts_at is required.
func protoToSilence(id string, scope *notificationsv1.SilenceScope, startsAt, endsAt *timestamppb.Timestamp, comment string) (notifications.Silence, error) {
	if scope == nil {
		return notifications.Silence{}, fleeterror.NewInvalidArgumentError("scope is required")
	}
	dk, err := protoToScopeKind(scope.GetKind())
	if err != nil {
		return notifications.Silence{}, err
	}
	if startsAt == nil {
		return notifications.Silence{}, fleeterror.NewInvalidArgumentError("starts_at is required")
	}
	dom := notifications.Silence{
		ID: id,
		Scope: notifications.SilenceScope{
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

func historyEntryToProto(n notificationhistory.StoredNotification) *notificationsv1.NotificationHistoryEntry {
	out := &notificationsv1.NotificationHistoryEntry{
		Id:          strconv.FormatInt(n.ID, 10),
		ReceivedAt:  timestamppb.New(n.ReceivedAt),
		AlertName:   n.AlertName,
		Status:      n.Status,
		Severity:    n.Severity,
		RuleGroup:   n.RuleGroup,
		Fingerprint: n.Fingerprint,
		DeviceId:    n.DeviceID,
		DeviceName:  n.DeviceName,
		DeviceMac:   n.DeviceMAC,
		Template:    n.Template,
		Summary:     n.Summary,
	}
	if n.StartsAt != nil {
		out.StartsAt = timestamppb.New(*n.StartsAt)
	}
	if n.EndsAt != nil {
		out.EndsAt = timestamppb.New(*n.EndsAt)
	}
	return out
}

// === enum mapping =======================================================

func channelKindToProto(k notifications.ChannelKind) notificationsv1.ChannelKind {
	switch k {
	case notifications.ChannelKindWebhook:
		return notificationsv1.ChannelKind_CHANNEL_KIND_WEBHOOK
	case notifications.ChannelKindSMTP:
		return notificationsv1.ChannelKind_CHANNEL_KIND_SMTP
	case notifications.ChannelKindSlack:
		return notificationsv1.ChannelKind_CHANNEL_KIND_SLACK
	}
	return notificationsv1.ChannelKind_CHANNEL_KIND_UNSPECIFIED
}

func protoToChannelKind(k notificationsv1.ChannelKind) (notifications.ChannelKind, error) {
	switch k {
	case notificationsv1.ChannelKind_CHANNEL_KIND_WEBHOOK:
		return notifications.ChannelKindWebhook, nil
	case notificationsv1.ChannelKind_CHANNEL_KIND_SMTP:
		return notifications.ChannelKindSMTP, nil
	case notificationsv1.ChannelKind_CHANNEL_KIND_SLACK:
		return notifications.ChannelKindSlack, nil
	case notificationsv1.ChannelKind_CHANNEL_KIND_UNSPECIFIED:
	}
	return "", fleeterror.NewInvalidArgumentErrorf("unknown channel kind: %s", k)
}

// httpStatusToInt32 narrows an HTTP status code to the proto int32
// response field. Status codes are always in range; the clamp keeps
// the conversion provably safe.
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

func scopeKindToProto(k notifications.SilenceScopeKind) notificationsv1.SilenceScopeKind {
	switch k {
	case notifications.SilenceScopeRule:
		return notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_RULE
	case notifications.SilenceScopeGroup:
		return notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_GROUP
	case notifications.SilenceScopeSite:
		return notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_SITE
	case notifications.SilenceScopeDevice:
		return notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_DEVICE
	}
	return notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_UNSPECIFIED
}

func protoToScopeKind(k notificationsv1.SilenceScopeKind) (notifications.SilenceScopeKind, error) {
	switch k {
	case notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_RULE:
		return notifications.SilenceScopeRule, nil
	case notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_GROUP:
		return notifications.SilenceScopeGroup, nil
	case notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_SITE:
		return notifications.SilenceScopeSite, nil
	case notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_DEVICE:
		return notifications.SilenceScopeDevice, nil
	case notificationsv1.SilenceScopeKind_SILENCE_SCOPE_KIND_UNSPECIFIED:
	}
	return "", fleeterror.NewInvalidArgumentErrorf("unknown silence scope kind: %s", k)
}
