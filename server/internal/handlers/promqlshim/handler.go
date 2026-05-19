// Package promqlshim is the narrow Prometheus query endpoint vmalert points at.
// It does NOT evaluate user-supplied PromQL.
// Every accepted query is the canonical synthetic-metric selector
//
//	fleet_alert{rule_id="<id>"[, organization_id="<org>"]}
//
// The shim parses the selector, looks up the matching Rule (rules.go), and
// runs the hard-coded SQL statement for that rule_id (queries.go).
package promqlshim

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DB is the small subset of *sql.DB the shim needs.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Handler struct{ db DB }

func New(db DB) *Handler { return &Handler{db: db} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/internal/promql/api/v1/query",
		"/internal/promql/api/v1/query_range":
		h.handleQuery(w, r)
	case "/internal/vmalert/rules.yml":
		h.handleRulesYAML(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writePromError(w, http.StatusMethodNotAllowed, "bad_method",
			"method "+r.Method+" not supported")
		return
	}
	if err := r.ParseForm(); err != nil {
		writePromError(w, http.StatusBadRequest, "bad_data",
			"parse form: "+err.Error())
		return
	}
	expr := r.Form.Get("query")
	if expr == "" {
		writePromError(w, http.StatusBadRequest, "bad_data", "missing query")
		return
	}

	ruleID, orgID, err := parseFleetAlertExpr(expr)
	if err != nil {
		writePromError(w, http.StatusBadRequest, "bad_data", err.Error())
		return
	}

	now := time.Now().UTC()
	if t := r.Form.Get("time"); t != "" {
		if parsed, err := parsePromTimestamp(t); err == nil {
			now = parsed
		}
	}

	if _, ok := ruleByID(ruleID); !ok {
		slog.Warn("promqlshim: unknown rule_id", "rule_id", ruleID, "expr", expr)
		writePromVector(w, nil)
		return
	}

	rows, err := runRule(r.Context(), h.db, ruleID, orgID, now)
	if err != nil {
		slog.Error("promqlshim: SQL execution failed",
			"rule_id", ruleID, "organization_id", orgID, "error", err)
		writePromError(w, http.StatusInternalServerError, "internal",
			"shim execution failed")
		return
	}
	writePromVector(w, rows)
}

// parseFleetAlertExpr extracts rule_id and (optional) organization_id from
// the canonical selector. Anything else — including any other label key,
// any regex matcher, any PromQL function — is rejected.
func parseFleetAlertExpr(expr string) (ruleID, orgID string, err error) {
	expr = strings.TrimSpace(expr)
	const prefix = "fleet_alert{"
	const suffix = "}"
	if !strings.HasPrefix(expr, prefix) || !strings.HasSuffix(expr, suffix) {
		return "", "", fmt.Errorf("shim only accepts fleet_alert{...} selectors, got %q", expr)
	}
	body := expr[len(prefix) : len(expr)-len(suffix)]

	labels, err := parseLabelList(body)
	if err != nil {
		return "", "", err
	}
	ruleID = labels["rule_id"]
	if ruleID == "" {
		return "", "", errors.New("missing rule_id label")
	}
	orgID = labels["organization_id"] // optional

	// The shim only recognises these two label keys. Anything else is a
	// signal that the caller wanted PromQL semantics the shim doesn't
	// implement — refuse rather than silently ignore.
	for k := range labels {
		if k != "rule_id" && k != "organization_id" {
			return "", "", fmt.Errorf("unsupported label %q", k)
		}
	}
	return ruleID, orgID, nil
}

// parseLabelList parses key="value", key2="value2" into a map.
func parseLabelList(s string) (map[string]string, error) {
	out := map[string]string{}
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ,")
		if s == "" {
			break
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("malformed label list at %q", s)
		}
		key := strings.TrimSpace(s[:eq])
		s = s[eq+1:]
		if len(s) == 0 || s[0] != '"' {
			return nil, fmt.Errorf("expected '\"' after %q=", key)
		}
		s = s[1:]
		end := strings.IndexByte(s, '"')
		if end < 0 {
			return nil, fmt.Errorf("unterminated value for label %q", key)
		}
		out[key] = s[:end]
		s = s[end+1:]
	}
	return out, nil
}

func parsePromTimestamp(v string) (time.Time, error) {
	// Prometheus accepts both unix-float and RFC3339; vmalert uses the float.
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		secs := int64(f)
		nanos := int64((f - float64(secs)) * 1e9)
		return time.Unix(secs, nanos).UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", v)
	}
	return t.UTC(), nil
}
