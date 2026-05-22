// Package alertmanagerwebhook implements the receiver fleet-api exposes for Grafana's built-in Alertmanager.
package alertmanagerwebhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
)

const Path = "/internal/alertmanager-webhook"

const authorizationScheme = "Bearer "

const maxBodyBytes = 1 << 20 // 1 MiB

const maxAlertsPerRequest = 100

const maxRowsPerRequest = 1000

const (
	statusFiring   = "firing"
	statusResolved = "resolved"
)

const (
	labelAlertName      = "alertname"
	labelOrganizationID = "organization_id"
	labelDeviceID       = "device_id"
	labelSeverity       = "severity"
	labelRuleGroup      = "rule_group"
	labelTemplate       = "template"
)

const ruleGroupSelfMonitoring = "proto-fleet-self"

type alertmanagerPayload struct {
	Status string              `json:"status"`
	Alerts []alertmanagerAlert `json:"alerts"`
}

type alertmanagerAlert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
	Fingerprint string            `json:"fingerprint"`
}

type OrgLister interface {
	ListActiveOrganizationIDs(ctx context.Context) ([]int64, error)
}

type Handler struct {
	activitySvc  *activity.Service
	webhookToken string
	orgLister    OrgLister
}

func NewHandler(activitySvc *activity.Service, webhookToken string, orgLister OrgLister) http.Handler {
	return &Handler{activitySvc: activitySvc, webhookToken: webhookToken, orgLister: orgLister}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !h.authorized(r) {
		// Don't leak whether the receiver is misconfigured vs. the
		// caller supplied a wrong token — a generic 401 is enough.
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// MaxBytesReader returns a *http.MaxBytesError once the cap is
		// exceeded; everything else is a network/read error.
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload exceeds limit")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var payload alertmanagerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	if len(payload.Alerts) == 0 {
		// An empty batch is well-formed but uninteresting; ack and
		// return so Grafana doesn't retry.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if len(payload.Alerts) > maxAlertsPerRequest {
		slog.Warn("alertmanager webhook: alert batch exceeds per-request cap; rejecting",
			"alerts", len(payload.Alerts),
			"limit", maxAlertsPerRequest,
		)
		writeError(w, http.StatusRequestEntityTooLarge, "alert batch exceeds limit")
		return
	}

	remainingRows := maxRowsPerRequest
	persisted := 0
	truncated := false
	orgIDs := []int64{}

	if h.orgLister != nil {
		orgIDs, err = h.orgLister.ListActiveOrganizationIDs(r.Context())
		if err != nil {
			slog.Warn("alertmanager webhook: failed to list orgs for self-monitoring fan-out; recording as unscoped",
				"error", err,
			)
		}
	}

	for i, alert := range payload.Alerts {
		if remainingRows <= 0 {
			slog.Warn("alertmanager webhook: per-request row cap reached; dropping remaining alerts",
				"limit", maxRowsPerRequest,
				"dropped_alerts", len(payload.Alerts)-i,
			)
			truncated = true
			break
		}
		attempted, written := h.persistAlert(r.Context(), alert, remainingRows, orgIDs)
		persisted += written
		remainingRows -= attempted
	}

	slog.Debug("alertmanager webhook delivered",
		"alerts", len(payload.Alerts),
		"persisted", persisted,
		"truncated", truncated,
		"status", payload.Status,
	)

	// return a 5xx so grafana retries delivery
	if persisted == 0 {
		writeError(w, http.StatusInternalServerError, "failed to persist alerts")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) authorized(r *http.Request) bool {
	if h.webhookToken == "" {
		return false
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, authorizationScheme) {
		return false
	}
	presented := header[len(authorizationScheme):]
	return subtle.ConstantTimeCompare([]byte(presented), []byte(h.webhookToken)) == 1
}

func (h *Handler) persistAlert(parent context.Context, alert alertmanagerAlert, budget int, orgIDs []int64) (attempted, persisted int) {
	if budget <= 0 {
		return 0, 0
	}
	event := alertToEvent(alert)

	// set timeout for persistence
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	// Org-scoped alert (the usual case)
	if event.OrganizationID != nil {
		return 1, h.insertEvent(ctx, event, alert)
	}

	// Unscoped alert — fan out only when this is a known global self-monitoring rule.
	if !isGlobalSelfMonitoringAlert(alert.Labels) {
		return 1, h.insertEvent(ctx, event, alert)
	}

	if len(orgIDs) == 0 {
		// Persist the unscoped event so the alert still lands somewhere
		return 1, h.insertEvent(ctx, event, alert)
	}

	// Cap fan-out at the remaining per-request budget.
	n := len(orgIDs)
	if n > budget {
		slog.Warn("alertmanager webhook: self-monitoring fan-out truncated by per-request row cap",
			"alertname", alert.Labels[labelAlertName],
			"fingerprint", alert.Fingerprint,
			"active_orgs", len(orgIDs),
			"fan_out_to", budget,
		)
		n = budget
	}

	for i := range n {
		scoped := event
		scoped.OrganizationID = &orgIDs[i]
		persisted += h.insertEvent(ctx, scoped, alert)
	}
	return n, persisted
}

func (h *Handler) insertEvent(ctx context.Context, event models.Event, alert alertmanagerAlert) int {
	if err := h.activitySvc.LogStrict(ctx, event); err != nil {
		// We persist on a best-effort basis within a batch
		slog.Error("alertmanager webhook: failed to insert activity event",
			"error", err,
			"fingerprint", alert.Fingerprint,
			"alertname", alert.Labels[labelAlertName],
		)
		return 0
	}
	return 1
}

func isGlobalSelfMonitoringAlert(labels map[string]string) bool {
	return labels[labelRuleGroup] == ruleGroupSelfMonitoring
}

func alertToEvent(alert alertmanagerAlert) models.Event {
	alertName := alert.Labels[labelAlertName]
	if alertName == "" {
		alertName = "unknown"
	}
	status := alert.Status
	if status == "" {
		status = statusFiring
	}

	description := alert.Annotations["summary"]
	if description == "" {
		description = fmt.Sprintf("alert %s %s", alertName, status)
	}

	result := models.ResultFailure
	if status == statusResolved {
		result = models.ResultSuccess
	}

	scopeType := alert.Labels[labelTemplate]
	scopeLabel := alert.Labels[labelDeviceID]
	scopeTypePtr := nilIfEmpty(scopeType)
	scopeLabelPtr := nilIfEmpty(scopeLabel)

	event := models.Event{
		Category:    models.CategorySystem,
		Type:        fmt.Sprintf("alert.%s.%s", alertName, status),
		Description: description,
		Result:      result,
		ScopeType:   scopeTypePtr,
		ScopeLabel:  scopeLabelPtr,
		ActorType:   models.ActorSystem,
		Metadata:    alertMetadata(alert),
	}

	if orgID, ok := parseOrgID(alert.Labels[labelOrganizationID]); ok {
		event.OrganizationID = &orgID
	}

	return event
}

func alertMetadata(alert alertmanagerAlert) map[string]any {
	meta := map[string]any{
		"status":      alert.Status,
		"labels":      alert.Labels,
		"annotations": alert.Annotations,
	}
	if !alert.StartsAt.IsZero() {
		meta["starts_at"] = alert.StartsAt.UTC().Format(time.RFC3339)
	}
	if !alert.EndsAt.IsZero() {
		meta["ends_at"] = alert.EndsAt.UTC().Format(time.RFC3339)
	}
	if alert.Fingerprint != "" {
		meta["fingerprint"] = alert.Fingerprint
	}
	if severity := alert.Labels[labelSeverity]; severity != "" {
		meta["severity"] = severity
	}
	if ruleGroup := alert.Labels[labelRuleGroup]; ruleGroup != "" {
		meta["rule_group"] = ruleGroup
	}
	return meta
}

func parseOrgID(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(errorResponse{Error: message})
	if err != nil {
		slog.Error("alertmanager webhook: failed encode json", "error", err)
	}
}
