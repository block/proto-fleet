// Package notifications wires the notifications domain service to a
// Connect-style HTTP surface the SettingsNotifications UI calls.
//
// The handler is structured around plain http.Handler endpoints
// rather than the Connect-RPC generated bindings because the
// notifications.proto file in this repo lands ahead of the codegen
// step — once `buf generate` runs and produces the
// notificationsv1connect package, swapping each registerJSON call
// for a connect.NewServiceHandler call is a mechanical change with
// no shape difference at the wire level (Connect over JSON uses the
// same `POST /<service>/<Method>` shape this handler mounts).
//
// Every endpoint:
//
//   - Pulls the caller's organization id from session.Info (the auth
//     interceptor is the authentication boundary; we re-check here
//     as a defence-in-depth assertion).
//   - Decodes the JSON request body via the typed wire DTO.
//   - Calls into notifications.Service and lets it enforce the org
//     scoping invariants.
//   - Maps ErrNotFound → 404 (permission_denied so we don't leak
//     existence of other orgs' ids) and any other error → 500.
//
// Field names on the wire match the client-side hand-written
// types/index.ts: snake_case identifiers, ISO-8601 timestamps,
// channel kinds as the lowercase tokens "webhook" / "smtp" / "slack",
// scope kinds as the lowercase tokens "rule" / "group" / "site" /
// "device".
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
	notifications "github.com/block/proto-fleet/server/internal/domain/notifications"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler is the http.Handler that mounts every notifications RPC.
// The Connect-RPC AuthInterceptor only runs on the typed service
// handlers (which we don't have until codegen runs), so this handler
// reuses the same session-cookie verification path the firmware
// upload endpoints use, then loads the caller's effective permissions
// the same way the interceptor would so middleware.RequirePermission
// gates every route.
type Handler struct {
	svc         *notifications.Service
	sessionSvc  *session.Service
	userStore   interfaces.UserStore
	permissions *authz.PermissionResolver
	history     notificationhistory.Lister
}

// NewHandler returns a handler bound to the supplied service.
func NewHandler(svc *notifications.Service, sessionSvc *session.Service, userStore interfaces.UserStore, permissions *authz.PermissionResolver, history notificationhistory.Lister) *Handler {
	return &Handler{svc: svc, sessionSvc: sessionSvc, userStore: userStore, permissions: permissions, history: history}
}

// Routes returns the set of (path, handler) pairs that main.go
// wires under the global mux. Paths mirror the Connect-RPC URL
// scheme (`/<package>.<Service>/<Method>`) so the SettingsNotifications
// UI can keep one configurable base URL.
//
// Every route is gated on a catalog permission: reads sit on
// notification:read, every mutation — including TestChannel, which
// triggers an immediate outbound delivery — on notification:manage.
func (h *Handler) Routes() map[string]http.Handler {
	return map[string]http.Handler{
		"POST /notifications.v1.ChannelService/ListChannels":  h.authed(authz.PermNotificationRead, h.listChannels),
		"POST /notifications.v1.ChannelService/CreateChannel": h.authed(authz.PermNotificationManage, h.createChannel),
		"POST /notifications.v1.ChannelService/UpdateChannel": h.authed(authz.PermNotificationManage, h.updateChannel),
		"POST /notifications.v1.ChannelService/DeleteChannel": h.authed(authz.PermNotificationManage, h.deleteChannel),
		"POST /notifications.v1.ChannelService/TestChannel":   h.authed(authz.PermNotificationManage, h.testChannel),

		"POST /notifications.v1.RuleService/ListRules":  h.authed(authz.PermNotificationRead, h.listRules),
		"POST /notifications.v1.RuleService/PauseRule":  h.authed(authz.PermNotificationManage, h.pauseRule),
		"POST /notifications.v1.RuleService/ResumeRule": h.authed(authz.PermNotificationManage, h.resumeRule),

		"POST /notifications.v1.SilenceService/ListSilences":  h.authed(authz.PermNotificationRead, h.listSilences),
		"POST /notifications.v1.SilenceService/CreateSilence": h.authed(authz.PermNotificationManage, h.createSilence),
		"POST /notifications.v1.SilenceService/UpdateSilence": h.authed(authz.PermNotificationManage, h.updateSilence),
		"POST /notifications.v1.SilenceService/DeleteSilence": h.authed(authz.PermNotificationManage, h.deleteSilence),

		"POST /notifications.v1.HistoryService/ListNotifications": h.authed(authz.PermNotificationRead, h.listNotifications),
	}
}

// Wire-contract limits. These mirror the buf.validate constraints on
// notifications.proto — the hand-written routes don't run the Connect
// validateInterceptor, so the same bounds are enforced here until the
// generated handlers take over.
const (
	maxRequestBodyBytes  = 1 << 20 // 1 MiB
	maxChannelNameLen    = 255
	maxSilenceCommentLen = 1024
)

// === Channel handlers ===================================================

func (h *Handler) listChannels(ctx context.Context, _ json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := h.svc.ListChannels(ctx, orgID)
	if err != nil {
		return nil, err
	}
	wireOut := make([]channelWire, 0, len(channels))
	for _, c := range channels {
		wireOut = append(wireOut, channelToWire(c))
	}
	return map[string]any{"channels": wireOut}, nil
}

func (h *Handler) createChannel(ctx context.Context, body json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req channelMutationWire
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
	}
	dom, err := wireToChannel(req)
	if err != nil {
		return nil, err
	}
	created, err := h.svc.CreateChannel(ctx, orgID, dom)
	if err != nil {
		return nil, err
	}
	return map[string]any{"channel": channelToWire(*created)}, nil
}

func (h *Handler) updateChannel(ctx context.Context, body json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req channelMutationWire
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
	}
	if req.ID == "" {
		return nil, fleeterror.NewInvalidArgumentError("id is required")
	}
	dom, err := wireToChannel(req)
	if err != nil {
		return nil, err
	}
	updated, err := h.svc.UpdateChannel(ctx, orgID, dom)
	if err != nil {
		return nil, err
	}
	return map[string]any{"channel": channelToWire(*updated)}, nil
}

func (h *Handler) deleteChannel(ctx context.Context, body json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
	}
	if req.ID == "" {
		return nil, fleeterror.NewInvalidArgumentError("id is required")
	}
	if err := h.svc.DeleteChannel(ctx, orgID, req.ID); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (h *Handler) testChannel(ctx context.Context, body json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req channelMutationWire
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
	}
	dom, err := wireToChannel(req)
	if err != nil {
		return nil, err
	}
	ok, code, errMsg, err := h.svc.TestChannel(ctx, orgID, dom)
	if err != nil {
		// A genuine service error (unknown/foreign channel id, invalid
		// destination, Grafana unreachable) is surfaced as a 4xx/5xx so
		// the client distinguishes it from a delivery failure — the
		// latter being a reachable Grafana reporting a non-2xx test
		// result via the ok/error/response_code body below.
		return nil, err
	}
	return map[string]any{
		"ok":            ok,
		"error":         errMsg,
		"response_code": code,
	}, nil
}

// === Rule handlers ======================================================

func (h *Handler) listRules(ctx context.Context, _ json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := h.svc.ListRules(ctx, orgID)
	if err != nil {
		return nil, err
	}
	wireOut := make([]ruleWire, 0, len(rules))
	for _, r := range rules {
		wireOut = append(wireOut, ruleToWire(r))
	}
	return map[string]any{"rules": wireOut}, nil
}

func (h *Handler) pauseRule(ctx context.Context, body json.RawMessage) (any, error) {
	return h.setRulePaused(ctx, body, true)
}

func (h *Handler) resumeRule(ctx context.Context, body json.RawMessage) (any, error) {
	return h.setRulePaused(ctx, body, false)
}

func (h *Handler) setRulePaused(ctx context.Context, body json.RawMessage, paused bool) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
	}
	if req.ID == "" {
		return nil, fleeterror.NewInvalidArgumentError("id is required")
	}
	var rule *notifications.Rule
	if paused {
		rule, err = h.svc.PauseRule(ctx, orgID, req.ID)
	} else {
		rule, err = h.svc.ResumeRule(ctx, orgID, req.ID)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"rule": ruleToWire(*rule)}, nil
}

// === Silence handlers ===================================================

func (h *Handler) listSilences(ctx context.Context, _ json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	silences, err := h.svc.ListSilences(ctx, orgID)
	if err != nil {
		return nil, err
	}
	wireOut := make([]silenceWire, 0, len(silences))
	for _, s := range silences {
		wireOut = append(wireOut, silenceToWire(s))
	}
	return map[string]any{"silences": wireOut}, nil
}

func (h *Handler) createSilence(ctx context.Context, body json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req silenceMutationWire
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
	}
	dom, err := wireToSilence(req, "")
	if err != nil {
		return nil, err
	}
	info, _ := session.GetInfo(ctx)
	if info != nil {
		dom.CreatedBy = info.Username
	}
	created, err := h.svc.CreateSilence(ctx, orgID, dom)
	if err != nil {
		return nil, err
	}
	return map[string]any{"silence": silenceToWire(*created)}, nil
}

func (h *Handler) updateSilence(ctx context.Context, body json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req silenceMutationWire
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
	}
	if req.ID == "" {
		return nil, fleeterror.NewInvalidArgumentError("id is required")
	}
	dom, err := wireToSilence(req, req.ID)
	if err != nil {
		return nil, err
	}
	updated, err := h.svc.UpdateSilence(ctx, orgID, dom)
	if err != nil {
		return nil, err
	}
	return map[string]any{"silence": silenceToWire(*updated)}, nil
}

func (h *Handler) deleteSilence(ctx context.Context, body json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
	}
	if req.ID == "" {
		return nil, fleeterror.NewInvalidArgumentError("id is required")
	}
	if err := h.svc.DeleteSilence(ctx, orgID, req.ID); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// === History handlers ===================================================

const (
	historyDefaultPageSize = 50
	historyMaxPageSize     = 200
)

func (h *Handler) listNotifications(ctx context.Context, body json.RawMessage) (any, error) {
	orgID, err := orgIDFrom(ctx)
	if err != nil {
		return nil, err
	}
	var req struct {
		BeforeID string `json:"before_id"`
		PageSize int32  `json:"page_size"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid request body: " + err.Error())
		}
	}
	limit := req.PageSize
	if limit <= 0 {
		limit = historyDefaultPageSize
	}
	if limit > historyMaxPageSize {
		limit = historyMaxPageSize
	}
	var beforeID *int64
	if req.BeforeID != "" {
		v, err := strconv.ParseInt(req.BeforeID, 10, 64)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid before_id: " + req.BeforeID)
		}
		beforeID = &v
	}
	// Fetch one extra row so has_more is exact rather than inferred
	// from a full page.
	rows, err := h.history.List(ctx, orgID, beforeID, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	wireOut := make([]historyEntryWire, 0, len(rows))
	for _, n := range rows {
		wireOut = append(wireOut, historyEntryToWire(n))
	}
	return map[string]any{
		"notifications": wireOut,
		"has_more":      hasMore,
	}, nil
}

// === wire shapes ========================================================

type webhookWire struct {
	URL          string `json:"url"`
	BearerHeader string `json:"bearer_header"`
}

type smtpWire struct {
	Host     string   `json:"host"`
	Port     int32    `json:"port"`
	Username string   `json:"username"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	Password string   `json:"password,omitempty"`
}

// slackWire carries the Slack incoming-webhook URL. Write-only: the
// URL embeds the capability token, so reads return the empty string
// and presence is signalled by has_secret.
type slackWire struct {
	WebhookURL string `json:"webhook_url"`
}

type channelWire struct {
	ID              string       `json:"id"`
	OrganizationID  string       `json:"organization_id"`
	Name            string       `json:"name"`
	Kind            string       `json:"kind"`
	Webhook         *webhookWire `json:"webhook"`
	SMTP            *smtpWire    `json:"smtp"`
	Slack           *slackWire   `json:"slack"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
	ValidatedAt     *string      `json:"validated_at"`
	ValidationState string       `json:"validation_state"`
	ValidationError string       `json:"validation_error,omitempty"`
	HasSecret       bool         `json:"has_secret"`
}

type channelMutationWire struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Kind    string       `json:"kind"`
	Webhook *webhookWire `json:"webhook"`
	SMTP    *smtpWire    `json:"smtp"`
	Slack   *slackWire   `json:"slack"`
}

type ruleWire struct {
	ID              string `json:"id"`
	OrganizationID  string `json:"organization_id"`
	Name            string `json:"name"`
	Template        string `json:"template"`
	Group           string `json:"group"`
	Severity        string `json:"severity"`
	Summary         string `json:"summary"`
	Description     string `json:"description"`
	DurationSeconds int32  `json:"duration_seconds"`
	Enabled         bool   `json:"enabled"`
}

type silenceScopeWire struct {
	Kind      string   `json:"kind"`
	RuleID    *string  `json:"rule_id"`
	GroupID   *string  `json:"group_id"`
	SiteID    *string  `json:"site_id"`
	DeviceIDs []string `json:"device_ids"`
}

type silenceWire struct {
	ID             string           `json:"id"`
	OrganizationID string           `json:"organization_id"`
	Scope          silenceScopeWire `json:"scope"`
	StartsAt       string           `json:"starts_at"`
	EndsAt         *string          `json:"ends_at"`
	Comment        string           `json:"comment"`
	CreatedBy      string           `json:"created_by"`
	CreatedAt      string           `json:"created_at"`
	Active         bool             `json:"active"`
}

type silenceMutationWire struct {
	ID       string           `json:"id"`
	Scope    silenceScopeWire `json:"scope"`
	StartsAt string           `json:"starts_at"`
	EndsAt   *string          `json:"ends_at"`
	Comment  string           `json:"comment"`
}

type historyEntryWire struct {
	ID          string  `json:"id"`
	ReceivedAt  string  `json:"received_at"`
	AlertName   string  `json:"alert_name"`
	Status      string  `json:"status"`
	Severity    string  `json:"severity"`
	RuleGroup   string  `json:"rule_group"`
	Fingerprint string  `json:"fingerprint"`
	DeviceID    string  `json:"device_id"`
	DeviceName  string  `json:"device_name"`
	DeviceMAC   string  `json:"device_mac"`
	Template    string  `json:"template"`
	Summary     string  `json:"summary"`
	StartsAt    *string `json:"starts_at"`
	EndsAt      *string `json:"ends_at"`
}

// === wire ↔ domain ======================================================

func channelToWire(c notifications.Channel) channelWire {
	out := channelWire{
		ID:              c.ID,
		OrganizationID:  fmt.Sprintf("%d", c.OrganizationID),
		Name:            c.Name,
		Kind:            string(c.Kind),
		CreatedAt:       c.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:       c.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ValidationState: string(c.ValidationState),
		ValidationError: c.ValidationError,
		HasSecret:       c.HasSecret,
	}
	if c.ValidatedAt != nil {
		v := c.ValidatedAt.UTC().Format(time.RFC3339Nano)
		out.ValidatedAt = &v
	}
	if c.Webhook != nil {
		out.Webhook = &webhookWire{URL: c.Webhook.URL}
	}
	if c.SMTP != nil {
		out.SMTP = &smtpWire{
			Host:     c.SMTP.Host,
			Port:     c.SMTP.Port,
			Username: c.SMTP.Username,
			From:     c.SMTP.From,
			To:       c.SMTP.To,
		}
	}
	if c.Slack != nil {
		// URL deliberately omitted — it's the secret.
		out.Slack = &slackWire{}
	}
	return out
}

func wireToChannel(req channelMutationWire) (notifications.Channel, error) {
	kind := notifications.ChannelKind(strings.ToLower(req.Kind))
	switch kind {
	case notifications.ChannelKindWebhook, notifications.ChannelKindSMTP, notifications.ChannelKindSlack:
	default:
		return notifications.Channel{}, fleeterror.NewInvalidArgumentError("unknown channel kind: " + req.Kind)
	}
	if len(req.Name) > maxChannelNameLen {
		return notifications.Channel{}, fleeterror.NewInvalidArgumentErrorf("name must be at most %d characters", maxChannelNameLen)
	}
	dom := notifications.Channel{
		ID:   req.ID,
		Name: req.Name,
		Kind: kind,
	}
	if req.Webhook != nil {
		dom.Webhook = &notifications.WebhookConfig{
			URL:          req.Webhook.URL,
			BearerHeader: req.Webhook.BearerHeader,
		}
	}
	if req.SMTP != nil {
		dom.SMTP = &notifications.SMTPConfig{
			Host:     req.SMTP.Host,
			Port:     req.SMTP.Port,
			Username: req.SMTP.Username,
			From:     req.SMTP.From,
			To:       req.SMTP.To,
			Password: req.SMTP.Password,
		}
	}
	if req.Slack != nil {
		dom.Slack = &notifications.SlackConfig{
			WebhookURL: req.Slack.WebhookURL,
		}
	}
	return dom, nil
}

func ruleToWire(r notifications.Rule) ruleWire {
	return ruleWire{
		ID:              r.ID,
		OrganizationID:  fmt.Sprintf("%d", r.OrganizationID),
		Name:            r.Name,
		Template:        string(r.Template),
		Group:           r.Group,
		Severity:        r.Severity,
		Summary:         r.Summary,
		Description:     r.Description,
		DurationSeconds: r.DurationSeconds,
		Enabled:         r.Enabled,
	}
}

func silenceToWire(s notifications.Silence) silenceWire {
	scope := silenceScopeWire{
		Kind:      string(s.Scope.Kind),
		DeviceIDs: s.Scope.DeviceIDs,
	}
	if s.Scope.RuleID != "" {
		v := s.Scope.RuleID
		scope.RuleID = &v
	}
	if s.Scope.GroupID != "" {
		v := s.Scope.GroupID
		scope.GroupID = &v
	}
	if s.Scope.SiteID != "" {
		v := s.Scope.SiteID
		scope.SiteID = &v
	}
	out := silenceWire{
		ID:             s.ID,
		OrganizationID: fmt.Sprintf("%d", s.OrganizationID),
		Scope:          scope,
		StartsAt:       s.StartsAt.UTC().Format(time.RFC3339Nano),
		Comment:        s.Comment,
		CreatedBy:      s.CreatedBy,
		CreatedAt:      s.CreatedAt.UTC().Format(time.RFC3339Nano),
		Active:         s.Active,
	}
	if !s.EndsAt.IsZero() {
		v := s.EndsAt.UTC().Format(time.RFC3339Nano)
		out.EndsAt = &v
	}
	return out
}

func historyEntryToWire(n notificationhistory.StoredNotification) historyEntryWire {
	out := historyEntryWire{
		ID:          strconv.FormatInt(n.ID, 10),
		ReceivedAt:  n.ReceivedAt.UTC().Format(time.RFC3339Nano),
		AlertName:   n.AlertName,
		Status:      n.Status,
		Severity:    n.Severity,
		RuleGroup:   n.RuleGroup,
		Fingerprint: n.Fingerprint,
		DeviceID:    n.DeviceID,
		DeviceName:  n.DeviceName,
		DeviceMAC:   n.DeviceMAC,
		Template:    n.Template,
		Summary:     n.Summary,
	}
	if n.StartsAt != nil {
		v := n.StartsAt.UTC().Format(time.RFC3339Nano)
		out.StartsAt = &v
	}
	if n.EndsAt != nil {
		v := n.EndsAt.UTC().Format(time.RFC3339Nano)
		out.EndsAt = &v
	}
	return out
}

func wireToSilence(req silenceMutationWire, id string) (notifications.Silence, error) {
	starts, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		return notifications.Silence{}, fleeterror.NewInvalidArgumentError("invalid starts_at: " + err.Error())
	}
	var ends time.Time
	if req.EndsAt != nil && *req.EndsAt != "" {
		ends, err = time.Parse(time.RFC3339, *req.EndsAt)
		if err != nil {
			return notifications.Silence{}, fleeterror.NewInvalidArgumentError("invalid ends_at: " + err.Error())
		}
	}
	if len(req.Comment) > maxSilenceCommentLen {
		return notifications.Silence{}, fleeterror.NewInvalidArgumentErrorf("comment must be at most %d characters", maxSilenceCommentLen)
	}
	kind := notifications.SilenceScopeKind(strings.ToLower(req.Scope.Kind))
	switch kind {
	case notifications.SilenceScopeRule, notifications.SilenceScopeGroup,
		notifications.SilenceScopeSite, notifications.SilenceScopeDevice:
	default:
		return notifications.Silence{}, fleeterror.NewInvalidArgumentError("unknown silence scope kind: " + req.Scope.Kind)
	}
	dom := notifications.Silence{
		ID:       id,
		Scope:    notifications.SilenceScope{Kind: kind, DeviceIDs: req.Scope.DeviceIDs},
		StartsAt: starts,
		EndsAt:   ends,
		Comment:  req.Comment,
	}
	if req.Scope.RuleID != nil {
		dom.Scope.RuleID = *req.Scope.RuleID
	}
	if req.Scope.GroupID != nil {
		dom.Scope.GroupID = *req.Scope.GroupID
	}
	if req.Scope.SiteID != nil {
		dom.Scope.SiteID = *req.Scope.SiteID
	}
	return dom, nil
}

// === plumbing ===========================================================

type jsonRPC func(ctx context.Context, body json.RawMessage) (any, error)

// authed wraps a typed JSON handler with session-cookie
// authentication followed by an RBAC check on the supplied catalog
// permission. The Connect-RPC interceptor chain only runs on the
// typed service handlers; this method reproduces the same
// session-cookie validation path used by the firmware HTTP handlers
// and the same RequirePermission gate the Connect handlers apply.
func (h *Handler) authed(permission string, fn jsonRPC) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, err := h.authenticate(r)
		if err != nil {
			writeJSONError(w, statusForErr(err), codeForErr(err), err.Error())
			return
		}
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{}); err != nil {
			writeJSONError(w, statusForErr(err), codeForErr(err), err.Error())
			return
		}
		var body json.RawMessage
		if r.Body != nil && r.ContentLength != 0 {
			// Cap the body — these routes decode into json.RawMessage
			// without the Connect server's built-in read limit, so an
			// oversized payload would otherwise buffer unbounded.
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON body: "+err.Error())
				return
			}
		}
		out, err := fn(ctx, body)
		if err != nil {
			writeJSONError(w, statusForErr(err), codeForErr(err), err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

// authenticate mirrors the session-cookie path the firmware
// handler uses, then loads the caller's effective permissions the
// same way the Connect auth interceptor does so RequirePermission
// can resolve. We don't accept API key auth here because the
// notifications surface is settings-screen UI only.
func (h *Handler) authenticate(r *http.Request) (context.Context, error) {
	cookie, err := r.Cookie(h.sessionSvc.CookieName())
	if err != nil || cookie.Value == "" {
		return r.Context(), fleeterror.NewUnauthenticatedError("session cookie required")
	}
	sess, err := h.sessionSvc.Validate(r.Context(), cookie.Value)
	if err != nil {
		return r.Context(), err
	}
	user, err := h.userStore.GetUserByID(r.Context(), sess.UserID)
	if err != nil {
		return r.Context(), fleeterror.NewUnauthenticatedErrorf("user with id %d not found", sess.UserID)
	}
	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		SessionID:      sess.SessionID,
		UserID:         sess.UserID,
		OrganizationID: sess.OrganizationID,
		ExternalUserID: user.UserID,
		Username:       user.Username,
	}
	eff, err := h.permissions.LoadEffective(r.Context(), sess.UserID, sess.OrganizationID)
	if err != nil {
		// Fail closed: without the effective set, RequirePermission
		// would reject anyway; surface the resolver failure directly.
		return r.Context(), fleeterror.NewInternalError("effective permissions lookup failed")
	}
	return middleware.WithEffectivePermissions(authn.SetInfo(r.Context(), info), eff), nil
}

// orgIDFrom requires session.Info on the context and returns the
// caller's organization id. The auth interceptor populated it; we
// re-check non-zero so a misconfigured route can't slip past.
func orgIDFrom(ctx context.Context) (int64, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return 0, err
	}
	if info.OrganizationID == 0 {
		return 0, fleeterror.NewUnauthenticatedError("organization id missing on session")
	}
	return info.OrganizationID, nil
}

// statusForErr / codeForErr translate domain errors into HTTP status
// codes and the lightweight code string the UI's getErrorMessage
// helper reads.
func statusForErr(err error) int {
	switch {
	case errors.Is(err, notifications.ErrNotFound):
		return http.StatusNotFound
	case fleeterror.IsInvalidArgumentError(err):
		return http.StatusBadRequest
	case fleeterror.IsAuthenticationError(err):
		return http.StatusUnauthorized
	case fleeterror.IsForbiddenError(err):
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

func codeForErr(err error) string {
	switch {
	case errors.Is(err, notifications.ErrNotFound):
		return "not_found"
	case fleeterror.IsInvalidArgumentError(err):
		return "invalid_argument"
	case fleeterror.IsAuthenticationError(err):
		return "unauthenticated"
	case fleeterror.IsForbiddenError(err):
		return "permission_denied"
	}
	return "internal"
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{
		Code:    code,
		Message: message,
	}); err != nil {
		// writer is gone, nothing to do
		return
	}
}
