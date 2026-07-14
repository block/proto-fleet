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

// maxBodyBytes bounds the request body (guards memory); large enough for a fleet-wide outage
// batched into one org-grouped notification (~tens of thousands of alerts).
const maxBodyBytes = 32 << 20 // 32 MiB

// maxAlertsPerRequest caps alerts per batch well above any real fleet-wide outage, so a
// pathological or abusive payload of many tiny alert objects can't drive unbounded row-building,
// long transactions, or an OOM even within the byte cap.
const maxAlertsPerRequest = 100_000

// insertsTimeout bounds the batch persist; chunked multi-row INSERTs stay well under this even
// for a very large outage, with headroom for a slow database.
const insertsTimeout = 30 * time.Second

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
		slog.Warn("alertmanager webhook: alert batch exceeds cap; rejecting",
			"alerts", len(payload.Alerts),
			"cap", maxAlertsPerRequest,
		)
		writeError(w, http.StatusRequestEntityTooLarge, "alert batch exceeds limit")
		return
	}

	orgIDs := h.fanOutOrgIDs(r.Context())
	rows, overflowed := buildRows(payload.Alerts, orgIDs)
	if overflowed {
		slog.Warn("alertmanager webhook: batch expands beyond row cap; rejecting",
			"alerts", len(payload.Alerts),
			"cap", maxPersistRows,
		)
		writeError(w, http.StatusRequestEntityTooLarge, "alert batch expands beyond limit")
		return
	}

	persistCtx, cancel := context.WithTimeout(r.Context(), insertsTimeout)
	defer cancel()

	// One atomic batch: all rows land or none do, so on success every alert is in history and
	// the whole batch is safe to deliver; on failure we 5xx and Grafana retries the batch.
	if err := h.store.InsertBatch(persistCtx, rows); err != nil {
		slog.Error("alertmanager webhook: failed to persist alert batch",
			"error", err,
			"alerts", len(payload.Alerts),
			"rows", len(rows),
		)
		writeError(w, http.StatusInternalServerError, "failed to persist alerts")
		return
	}

	slog.Debug("alertmanager webhook delivered",
		"alerts", len(payload.Alerts),
		"rows", len(rows),
		"status", payload.Status,
	)

	// History is stored; fan out the whole batch. Delivery failures are logged inside the
	// deliverer, never surfaced here, so a bad destination can't trigger a Grafana retry.
	h.deliver(r.Context(), payload.Alerts)

	w.WriteHeader(http.StatusNoContent)
}

// maxFanOutOrgs bounds how many orgs a single global self-monitoring alert expands to, so one
// alert can't fan out to an arbitrarily large org count.
const maxFanOutOrgs = 2000

// maxPersistRows caps the total rows a batch expands to (after fan-out), so many self-monitoring
// alerts can't multiply into an unbounded write even under the per-request alert cap. Org-scoped
// alerts are 1 row each, so a real device outage stays well under this.
const maxPersistRows = 100_000

// buildRows converts a batch into history rows, expanding a global self-monitoring alert
// (no organization_id) into one row per active org. Returns overflowed=true if expansion would
// exceed maxPersistRows, in which case the returned rows are partial and the caller must reject.
func buildRows(alerts []alertmanagerAlert, orgIDs []int64) (rows []*notificationhistory.Notification, overflowed bool) {
	rows = make([]*notificationhistory.Notification, 0, len(alerts))
	add := func(n *notificationhistory.Notification) bool {
		if len(rows) >= maxPersistRows {
			return false
		}
		rows = append(rows, n)
		return true
	}
	for _, alert := range alerts {
		row := alertToRow(alert)
		if row.OrganizationID != nil {
			if !add(&row) {
				return rows, true
			}
			continue
		}
		if !isGlobalSelfMonitoringAlert(alert.Labels) || len(orgIDs) == 0 {
			if !add(&row) {
				return rows, true
			}
			continue
		}
		fanOut := orgIDs
		if len(fanOut) > maxFanOutOrgs {
			slog.Warn("alertmanager webhook: self-monitoring fan-out capped",
				"alertname", alert.Labels[labelAlertName],
				"active_orgs", len(fanOut),
				"cap", maxFanOutOrgs,
			)
			fanOut = fanOut[:maxFanOutOrgs]
		}
		for i := range fanOut {
			scoped := row
			scoped.OrganizationID = &fanOut[i]
			if !add(&scoped) {
				return rows, true
			}
		}
	}
	return rows, false
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
	// Detach from request cancellation so a client disconnect can't abort in-flight sends partway;
	// deliverTimeout still bounds the fan-out.
	deliverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliverTimeout)
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

func isGlobalSelfMonitoringAlert(labels map[string]string) bool {
	return labels[labelRuleGroup] == ruleGroupSelfMonitoring
}

// Grafana stamps datasource_uid only on its synthetic evaluation-failure
// alerts; real alerts merely named like them don't carry it.
func isSyntheticEvaluationAlert(labels map[string]string) bool {
	name := labels[labelAlertName]
	return (name == "DatasourceError" || name == "DatasourceNoData") && labels["datasource_uid"] != ""
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
	// Synthetic evaluation failures inherit a user rule's static org label;
	// persist them org-less so tenants never see them (operator triage only).
	if isSyntheticEvaluationAlert(alert.Labels) {
		return row
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
