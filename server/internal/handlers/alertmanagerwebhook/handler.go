// Package alertmanagerwebhook implements the receiver fleet-api exposes for Grafana's built-in Alertmanager.
package alertmanagerwebhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	alertsdomain "github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
)

const Path = "/internal/alertmanager-webhook"

// deliverTimeout bounds the per-request fan-out so a slow destination can't hold the response open until Grafana times out and retries.
const deliverTimeout = 25 * time.Second

const authorizationScheme = "Bearer "

const maxBodyBytes = 1 << 20 // 1 MiB

const maxAlertsPerRequest = 100

const maxRowsPerRequest = 1000

const insertsTimeout = 10 * time.Second

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
	ListOrganizations(ctx context.Context) ([]sqlc.Organization, error)
}

// Deliverer fans a parsed alert batch out to each org's channels (implemented by alerts.Deliverer).
type Deliverer interface {
	Deliver(ctx context.Context, alerts []alertsdomain.Alert)
}

type Handler struct {
	store        notificationhistory.Store
	webhookToken string
	orgLister    OrgLister
	deliverer    Deliverer
}

func NewHandler(store notificationhistory.Store, webhookToken string, orgLister OrgLister, deliverer Deliverer) http.Handler {
	return &Handler{store: store, webhookToken: webhookToken, orgLister: orgLister, deliverer: deliverer}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !h.authorized(r) {
		// Generic 401 — don't leak whether the receiver is misconfigured
		// vs. the caller supplied a wrong token.
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
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
		// Well-formed but uninteresting; ack so Grafana doesn't retry.
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

	orgIDs := h.fanOutOrgIDs(r.Context())

	remainingRows := maxRowsPerRequest
	persisted := 0
	truncated := false
	persistCtx, cancel := context.WithTimeout(r.Context(), insertsTimeout)
	defer cancel()

	for i, alert := range payload.Alerts {
		if remainingRows <= 0 {
			slog.Warn("alertmanager webhook: per-request row cap reached; dropping remaining alerts",
				"limit", maxRowsPerRequest,
				"dropped_alerts", len(payload.Alerts)-i,
			)
			truncated = true
			break
		}
		attempted, written := h.persistAlert(persistCtx, alert, remainingRows, orgIDs)
		persisted += written
		remainingRows -= attempted
	}

	slog.Debug("alertmanager webhook delivered",
		"alerts", len(payload.Alerts),
		"persisted", persisted,
		"truncated", truncated,
		"status", payload.Status,
	)

	// Return 5xx so Grafana retries delivery.
	if persisted == 0 {
		writeError(w, http.StatusInternalServerError, "failed to persist alerts")
		return
	}

	// Fan out to org channels once history is stored. Delivery failures are logged inside the
	// deliverer, never surfaced here, so a bad destination can't trigger a Grafana retry.
	h.deliver(r.Context(), payload.Alerts)

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deliver(ctx context.Context, alerts []alertmanagerAlert) {
	if h.deliverer == nil {
		return
	}
	// Best-effort: a panic here must not abort the request before the 204, or Grafana retries and re-sends.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("alertmanager webhook: delivery panicked; alerts persisted but not delivered", "panic", r)
		}
	}()
	out := make([]alertsdomain.Alert, 0, len(alerts))
	for _, a := range alerts {
		out = append(out, alertsdomain.Alert{Status: a.Status, Labels: a.Labels, Annotations: a.Annotations})
	}
	deliverCtx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()
	h.deliverer.Deliver(deliverCtx, out)
}

// fanOutOrgIDs returns the orgs eligible for self-monitoring fan-out, or
// nil when the lister is unset or errors — both fall back to an unscoped row.
func (h *Handler) fanOutOrgIDs(ctx context.Context) []int64 {
	if h.orgLister == nil {
		return nil
	}
	orgs, err := h.orgLister.ListOrganizations(ctx)
	if err != nil {
		slog.Warn("alertmanager webhook: failed to list orgs for self-monitoring fan-out; recording as unscoped",
			"error", err,
		)
		return nil
	}
	orgIDs := make([]int64, len(orgs))
	for i := range orgs {
		orgIDs[i] = orgs[i].ID
	}
	return orgIDs
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

func (h *Handler) persistAlert(ctx context.Context, alert alertmanagerAlert, budget int, orgIDs []int64) (attempted, persisted int) {
	if budget <= 0 {
		return 0, 0
	}
	row := alertToRow(alert)

	// Org-scoped alert (the usual case).
	if row.OrganizationID != nil {
		return 1, h.insert(ctx, row, alert)
	}

	// Unscoped + not self-monitoring → keep historic single-row behaviour.
	if !isGlobalSelfMonitoringAlert(alert.Labels) || len(orgIDs) == 0 {
		return 1, h.insert(ctx, row, alert)
	}

	// Self-monitoring fan-out, capped at the remaining row budget.
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
		scoped := row
		scoped.OrganizationID = &orgIDs[i]
		persisted += h.insert(ctx, scoped, alert)
	}
	return n, persisted
}

func (h *Handler) insert(ctx context.Context, row notificationhistory.Notification, alert alertmanagerAlert) int {
	if err := h.store.Insert(ctx, &row); err != nil {
		// Best-effort within a batch — log and let the caller count successes.
		slog.Error("alertmanager webhook: failed to insert notification_history row",
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

func alertToRow(alert alertmanagerAlert) notificationhistory.Notification {
	alertName := alert.Labels[labelAlertName]
	if alertName == "" {
		alertName = "unknown"
	}
	status := alert.Status
	if status == "" {
		status = statusFiring
	}

	row := notificationhistory.Notification{
		AlertName:   alertName,
		Status:      status,
		Severity:    alert.Labels[labelSeverity],
		RuleGroup:   alert.Labels[labelRuleGroup],
		Fingerprint: alert.Fingerprint,
		DeviceID:    alert.Labels[labelDeviceID],
		Template:    alert.Labels[labelTemplate],
		Summary:     alert.Annotations["summary"],
		Labels:      alert.Labels,
		Annotations: alert.Annotations,
	}
	if !alert.StartsAt.IsZero() {
		t := alert.StartsAt.UTC()
		row.StartsAt = &t
	}
	if !alert.EndsAt.IsZero() {
		t := alert.EndsAt.UTC()
		row.EndsAt = &t
	}
	if orgID, ok := parseOrgID(alert.Labels[labelOrganizationID]); ok {
		row.OrganizationID = &orgID
	}
	return row
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

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorResponse{Error: message}); err != nil {
		slog.Error("alertmanager webhook: failed encode json", "error", err)
	}
}
