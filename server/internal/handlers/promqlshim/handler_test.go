package promqlshim

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeDB struct{}

func (fakeDB) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return nil, sql.ErrConnDone
}

func TestParseFleetAlertExpr(t *testing.T) {
	cases := []struct {
		expr        string
		wantRule    string
		wantOrg     string
		shouldError bool
	}{
		{`fleet_alert{rule_id="device-offline-default"}`, "device-offline-default", "", false},
		{`fleet_alert{rule_id="device-offline-default", organization_id="7"}`, "device-offline-default", "7", false},
		// no PromQL semantics — anything that isn't the canonical form is rejected.
		{`up`, "", "", true},
		{`fleet_alert{}`, "", "", true},
		{`fleet_alert{evil="x"}`, "", "", true},
		{`fleet_alert{rule_id="r", organization_id=~"7"}`, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			ruleID, orgID, err := parseFleetAlertExpr(tc.expr)
			if tc.shouldError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantRule, ruleID)
			require.Equal(t, tc.wantOrg, orgID)
		})
	}
}

func TestUnknownRuleIDReturnsEmptyVector(t *testing.T) {
	h := New(fakeDB{})
	req := httptest.NewRequest(http.MethodGet,
		`/internal/promql/api/v1/query?query=`+
			`fleet_alert%7Brule_id%3D%22not-a-real-rule%22%7D`,
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []any  `json:"result"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "success", body.Status)
	require.Equal(t, "vector", body.Data.ResultType)
	require.Empty(t, body.Data.Result)
}

func TestBuiltinRulesCoverDefaults(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range BuiltinRules() {
		ids[r.ID] = true
	}
	for _, want := range []string{
		"device-offline-default",
		"device-temperature-default",
		"telemetry-poll-failure-default",
	} {
		require.True(t, ids[want], "missing built-in rule %q", want)
	}
}

func TestMissingQueryParamReturns400(t *testing.T) {
	h := New(fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/internal/promql/api/v1/query", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestRulesYAMLContainsEveryBuiltin guards the YAML stub that vmalert
// loads from /internal/vmalert/rules.yml.
func TestRulesYAMLContainsEveryBuiltin(t *testing.T) {
	out := renderRulesYAML(BuiltinRules())
	require.Contains(t, out, "name: proto-fleet-builtins")
	for _, r := range BuiltinRules() {
		require.Contains(t, out, "alert: "+r.ID,
			"YAML stub must declare alert %q", r.ID)
		require.Contains(t, out, `expr: fleet_alert{rule_id="`+r.ID+`"}`,
			"alert %q must use the canonical selector", r.ID)
	}
	// And make sure handleRulesYAML actually serves it.
	h := New(fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/internal/vmalert/rules.yml", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.True(t, strings.Contains(body, "name: proto-fleet-builtins"))
}
