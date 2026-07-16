package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func offlineConfig(name string, duration int32) RuleConfig {
	return RuleConfig{Name: name, DurationSeconds: duration, Offline: &OfflineRuleConfig{}}
}

func TestValidateRuleConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     RuleConfig
		wantErr bool
	}{
		{"offline ok", offlineConfig("Offline too long", 1800), false},
		{"name required", offlineConfig("   ", 1800), true},
		{"name too long", offlineConfig(strings.Repeat("n", maxRuleNameLength+1), 1800), true},
		// Length is counted in characters, not bytes (3-byte runes here).
		{"multibyte name at limit ok", offlineConfig(strings.Repeat("温", maxRuleNameLength), 1800), false},
		{"multibyte name over limit", offlineConfig(strings.Repeat("温", maxRuleNameLength+1), 1800), true},
		// Grafana's synthetic evaluation-failure alertnames are reserved.
		{"reserved name DatasourceError", offlineConfig("DatasourceError", 1800), true},
		{"reserved name DatasourceNoData case-insensitive", offlineConfig("datasourcenodata", 1800), true},
		{"duration below floor", offlineConfig("r", 59), true},
		{"duration above ceiling", offlineConfig("r", 86401), true},
		{"no template config", RuleConfig{Name: "r", DurationSeconds: 600}, true},
		{"two template configs", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Offline: &OfflineRuleConfig{}, Temperature: &TemperatureRuleConfig{MaxCelsius: 80},
		}, true},
		{"hashrate pct ok", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: 75},
		}, false},
		{"hashrate pct over 100", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: 101},
		}, true},
		{"hashrate pct zero", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: 0},
		}, true},
		// Sub-floor percents would render a HAVING < 0 threshold that never fires.
		{"hashrate pct below floor", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: 0.001},
		}, true},
		{"hashrate pct at floor ok", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: minHashratePercent},
		}, false},
		{"hashrate absolute ok", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 90, Unit: HashrateUnitTerahash},
		}, false},
		{"hashrate absolute missing unit", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 90},
		}, true},
		// PH→TH normalization must not overflow into an unrepresentable SQL literal.
		{"hashrate absolute overflows after unit conversion", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 1e308, Unit: HashrateUnitPetahash},
		}, true},
		{"hashrate absolute above cap", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: maxAbsoluteTerahash + 1, Unit: HashrateUnitTerahash},
		}, true},
		{"hashrate mode required", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Value: 90},
		}, true},
		{"temperature ok", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Temperature: &TemperatureRuleConfig{MaxCelsius: 85},
		}, false},
		{"temperature zero", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Temperature: &TemperatureRuleConfig{MaxCelsius: 0},
		}, true},
		{"temperature over ceiling", RuleConfig{
			Name: "r", DurationSeconds: 600,
			Temperature: &TemperatureRuleConfig{MaxCelsius: 151},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRuleConfig(tc.cfg)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, fleeterror.IsInvalidArgumentError(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func compiledSQL(t *testing.T, r GrafanaAlertRule) string {
	t.Helper()
	var data []struct {
		RefID string `json:"refId"`
		Model struct {
			RawSQL string `json:"rawSql"`
		} `json:"model"`
	}
	require.NoError(t, json.Unmarshal(r.Data, &data))
	require.Len(t, data, 2)
	assert.Equal(t, "A", data[0].RefID)
	return data[0].Model.RawSQL
}

func TestCompileUserRule(t *testing.T) {
	cases := []struct {
		name        string
		cfg         RuleConfig
		wantMetric  string
		wantSQLFrag string
		wantSummary string
	}{
		{
			name:        "offline",
			cfg:         offlineConfig("Offline too long", 1800),
			wantMetric:  "fleet_device_online",
			wantSQLFrag: "HAVING last(value, time) = 0",
			wantSummary: "Device is offline for at least 30 minutes.",
		},
		{
			name: "hashrate pct of expected",
			cfg: RuleConfig{
				Name: "Slow hashing", DurationSeconds: 1200,
				Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: 75},
			},
			wantMetric:  "fleet_device_hashing",
			wantSQLFrag: "HAVING last(value, time) < 0.75",
			wantSummary: "Device hashrate is below 75% of expected for at least 20 minutes.",
		},
		{
			name: "hashrate pct fractional percent formats without float drift",
			cfg: RuleConfig{
				Name: "Slow hashing", DurationSeconds: 1200,
				Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: 33.3},
			},
			wantMetric:  "fleet_device_hashing",
			wantSQLFrag: "HAVING last(value, time) < 0.333",
			wantSummary: "Device hashrate is below 33.3% of expected for at least 20 minutes.",
		},
		{
			name: "hashrate absolute petahash normalizes to terahash",
			cfg: RuleConfig{
				Name: "Slow hashing", DurationSeconds: 600,
				Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 1.5, Unit: HashrateUnitPetahash},
			},
			wantMetric:  "fleet_device_hashrate_terahash",
			wantSQLFrag: "AND obs.latest_value < 1500",
			wantSummary: "Device hashrate is below 1.5 PH/s for at least 10 minutes.",
		},
		{
			name: "temperature",
			cfg: RuleConfig{
				Name: "Running hot", DurationSeconds: 900,
				Temperature: &TemperatureRuleConfig{MaxCelsius: 85},
			},
			wantMetric:  "fleet_device_temperature_max_celsius",
			wantSQLFrag: "HAVING max(latest_temp) > 85",
			wantSummary: "Max sensor temperature for device is above 85C for at least 15 minutes.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, err := compileUserRule(7, "pfu-test", tc.cfg)
			require.NoError(t, err)

			assert.Equal(t, "pfu-test", rule.UID)
			assert.Equal(t, "proto-fleet-user-7", rule.FolderUID)
			// Per-rule Grafana group: group PUTs replace the whole group, so a
			// shared one would let concurrent creates erase siblings.
			assert.Equal(t, "pfu-test", rule.RuleGroup)
			assert.Equal(t, strings.TrimSpace(tc.cfg.Name), rule.Title)
			assert.Equal(t, "B", rule.Condition)
			assert.Equal(t, "OK", rule.NoDataState)
			assert.Equal(t, "Error", rule.ExecErrState)

			assert.Equal(t, "7", rule.Labels[ruleLabelOrganizationID])
			assert.Equal(t, ruleOriginUser, rule.Labels[ruleLabelOrigin])
			assert.Equal(t, "warning", rule.Labels[ruleLabelSeverity])
			assert.Equal(t, string(tc.cfg.Template()), rule.Labels[ruleLabelTemplate])
			assert.Equal(t, "proto-fleet-user-7", rule.Labels[ruleLabelRuleGroup])
			assert.NotContains(t, rule.Labels, ruleLabelScope)

			sql := compiledSQL(t, rule)
			assert.Contains(t, sql, "'"+tc.wantMetric+"'")
			assert.Contains(t, sql, "organization_id = '7'")
			assert.Contains(t, sql, tc.wantSQLFrag)

			assert.Equal(t, tc.wantSummary, rule.Annotations["summary"])

			var roundTripped RuleConfig
			require.NoError(t, json.Unmarshal([]byte(rule.Annotations[ruleAnnotationConfig]), &roundTripped))
			assert.Equal(t, tc.cfg, roundTripped)
		})
	}
}

func TestCompileUserRuleDurationAndDomainRoundTrip(t *testing.T) {
	cfg := RuleConfig{
		Name: "Slow hashing", DurationSeconds: 1200,
		Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: 80},
	}
	compiled, err := compileUserRule(7, "pfu-test", cfg)
	require.NoError(t, err)
	assert.Equal(t, "1200s", compiled.For)

	domain := grafanaRuleToDomain(7, compiled)
	assert.Equal(t, RuleOriginUser, domain.Origin)
	assert.Equal(t, int32(1200), domain.DurationSeconds)
	assert.Equal(t, RuleTemplateHashrate, domain.Template)
	// The UI groups by the per-org label, not the per-rule Grafana group.
	assert.Equal(t, "proto-fleet-user-7", domain.Group)
	require.NotNil(t, domain.Config)
	assert.Equal(t, cfg, *domain.Config)
}

// The curtailment gate: absolute-mode SQL joins the ratio metric and excludes
// its non-alerting 1.0 sentinel (not expected to hash) — but only when nothing
// is observed hashing, since real ratios also sit at 1.0 (at-nameplate devices,
// and no-nameplate devices where hashingRatio pins any positive reading to 1).
func TestCompileUserRuleAbsoluteHashrateGatesOnHashing(t *testing.T) {
	compiled, err := compileUserRule(7, "pfu-test", RuleConfig{
		Name: "Slow hashing", DurationSeconds: 600,
		Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 90, Unit: HashrateUnitTerahash},
	})
	require.NoError(t, err)
	sql := compiledSQL(t, compiled)
	assert.Contains(t, sql, "'fleet_device_hashing'")
	assert.Contains(t, sql, "(gate.latest_value < 1 OR obs.latest_value > 0)")
}

func TestParseDurationSecondsPrometheusUnits(t *testing.T) {
	// Grafana echoes `for` back in Prometheus canonical form ("1d", "2w").
	cases := map[string]int32{
		"1800s": 1800,
		"30m":   1800,
		"1h30m": 5400,
		"1d":    86400,
		"1d2h":  93600,
		"2w":    1209600,
		"":      0,
		"bogus": 0,
	}
	for in, want := range cases {
		assert.Equalf(t, want, parseDurationSeconds(in), "parseDurationSeconds(%q)", in)
	}
}

// A parseable-but-invalid config annotation must not round-trip into the
// editor: the client hides Edit on nil Config, which is what prevents the
// modal's offline fallback from silently rewriting the rule.
func TestGrafanaRuleToDomainRejectsInvalidConfig(t *testing.T) {
	base := func(configJSON string) GrafanaAlertRule {
		return GrafanaAlertRule{
			UID: "pfu-x",
			Labels: map[string]string{
				ruleLabelOrigin:   ruleOriginUser,
				ruleLabelTemplate: "hashrate",
			},
			Annotations: map[string]string{ruleAnnotationConfig: configJSON},
		}
	}
	cases := map[string]string{
		"empty object":       `{}`,
		"no template branch": `{"name":"r","duration_seconds":600}`,
		"unknown mode":       `{"name":"r","duration_seconds":600,"hashrate":{"mode":"bogus","value":50}}`,
		"out-of-range value": `{"name":"r","duration_seconds":600,"hashrate":{"mode":"pct_expected","value":500}}`,
		"template mismatch":  `{"name":"r","duration_seconds":600,"temperature":{"max_celsius":85}}`,
		"not json":           `{"name":`,
	}
	for name, configJSON := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, grafanaRuleToDomain(7, base(configJSON)).Config)
		})
	}

	valid := base(`{"name":"r","duration_seconds":600,"hashrate":{"mode":"pct_expected","value":50}}`)
	require.NotNil(t, grafanaRuleToDomain(7, valid).Config)
}

func TestGrafanaRuleToDomainProvisionedOrigin(t *testing.T) {
	domain := grafanaRuleToDomain(7, GrafanaAlertRule{
		UID:    "protofleet-device-offline",
		Labels: map[string]string{ruleLabelScope: ruleScopeShared, "template": "offline"},
	})
	assert.Equal(t, RuleOriginProvisioned, domain.Origin)
	assert.Nil(t, domain.Config)
}

// fakeGrafanaRules serves the full rule-CRUD surface CreateRule/UpdateRule/DeleteRule touch.
type fakeGrafanaRules struct {
	listed          []GrafanaAlertRule
	created         *GrafanaAlertRule
	updated         *GrafanaAlertRule
	deletedUID      string
	folderEnsured   bool
	putGroup        *GrafanaRuleGroup
	deletedSilences []string
	silences        []GrafanaSilence
}

func (f *fakeGrafanaRules) server(t *testing.T) *Grafana {
	t.Helper()
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(v))
	}
	mux.HandleFunc("GET /api/v1/provisioning/alert-rules", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, f.listed)
	})
	mux.HandleFunc("GET /api/v1/provisioning/alert-rules/{uid}", func(w http.ResponseWriter, r *http.Request) {
		for _, rule := range f.listed {
			if rule.UID == r.PathValue("uid") {
				writeJSON(w, rule)
				return
			}
		}
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("POST /api/v1/provisioning/alert-rules", func(w http.ResponseWriter, r *http.Request) {
		var rule GrafanaAlertRule
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rule))
		f.created = &rule
		writeJSON(w, rule)
	})
	mux.HandleFunc("PUT /api/v1/provisioning/alert-rules/{uid}", func(w http.ResponseWriter, r *http.Request) {
		var rule GrafanaAlertRule
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rule))
		f.updated = &rule
		writeJSON(w, rule)
	})
	mux.HandleFunc("DELETE /api/v1/provisioning/alert-rules/{uid}", func(w http.ResponseWriter, r *http.Request) {
		f.deletedUID = r.PathValue("uid")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/folders/{uid}", func(w http.ResponseWriter, _ *http.Request) {
		if f.folderEnsured {
			writeJSON(w, GrafanaFolder{UID: "proto-fleet-user-7", Title: "t"})
			return
		}
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("POST /api/folders", func(w http.ResponseWriter, r *http.Request) {
		var folder GrafanaFolder
		require.NoError(t, json.NewDecoder(r.Body).Decode(&folder))
		f.folderEnsured = true
		writeJSON(w, folder)
	})
	mux.HandleFunc("PUT /api/v1/provisioning/folder/{uid}/rule-groups/{group}", func(w http.ResponseWriter, r *http.Request) {
		var group GrafanaRuleGroup
		require.NoError(t, json.NewDecoder(r.Body).Decode(&group))
		f.putGroup = &group
		writeJSON(w, group)
	})
	mux.HandleFunc("GET /api/alertmanager/grafana/api/v2/silences", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, f.silences)
	})
	mux.HandleFunc("DELETE /api/alertmanager/grafana/api/v2/silence/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.deletedSilences = append(f.deletedSilences, r.PathValue("id"))
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewGrafana(GrafanaConfig{URL: srv.URL})
}

func userRuleFixture(uid string, org string) GrafanaAlertRule {
	return GrafanaAlertRule{
		UID:       uid,
		Title:     "User rule " + uid,
		FolderUID: "proto-fleet-user-" + org,
		RuleGroup: "proto-fleet-user-" + org,
		Labels: map[string]string{
			ruleLabelOrganizationID: org,
			ruleLabelOrigin:         ruleOriginUser,
			"template":              "offline",
		},
	}
}

func TestCreateRule(t *testing.T) {
	fake := &fakeGrafanaRules{}
	svc := NewService(fake.server(t), nil, nil, nil, DestinationPolicy{})

	rule, err := svc.CreateRule(context.Background(), 7, offlineConfig("Offline too long", 1800))
	require.NoError(t, err)

	require.NotNil(t, fake.created)
	assert.True(t, strings.HasPrefix(fake.created.UID, "pfu-"))
	assert.Equal(t, "proto-fleet-user-7", fake.created.FolderUID)
	assert.Equal(t, fake.created.UID, fake.created.RuleGroup)
	assert.True(t, fake.folderEnsured)
	// The interval pin PUT must carry the group's full contents: the one new rule.
	require.NotNil(t, fake.putGroup)
	assert.Equal(t, userRuleGroupInterval, fake.putGroup.Interval)
	assert.Equal(t, fake.created.RuleGroup, fake.putGroup.Title)
	require.Len(t, fake.putGroup.Rules, 1)
	assert.Equal(t, fake.created.UID, fake.putGroup.Rules[0].UID)

	assert.Equal(t, "Offline too long", rule.Name)
	assert.Equal(t, RuleOriginUser, rule.Origin)
	require.NotNil(t, rule.Config)
	assert.Equal(t, int32(1800), rule.Config.DurationSeconds)
}

func TestCreateRuleQuota(t *testing.T) {
	fake := &fakeGrafanaRules{}
	for i := range maxUserRulesPerOrg {
		fake.listed = append(fake.listed, userRuleFixture(fmt.Sprintf("pfu-%d", i), "7"))
	}
	svc := NewService(fake.server(t), nil, nil, nil, DestinationPolicy{})

	_, err := svc.CreateRule(context.Background(), 7, offlineConfig("One more", 1800))
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Nil(t, fake.created)
}

// Another org's user rules must not count against this org's quota.
func TestCreateRuleQuotaIsPerOrg(t *testing.T) {
	fake := &fakeGrafanaRules{}
	for i := range maxUserRulesPerOrg {
		fake.listed = append(fake.listed, userRuleFixture(fmt.Sprintf("pfu-%d", i), "8"))
	}
	svc := NewService(fake.server(t), nil, nil, nil, DestinationPolicy{})

	_, err := svc.CreateRule(context.Background(), 7, offlineConfig("First for org 7", 1800))
	require.NoError(t, err)
	require.NotNil(t, fake.created)
}

func TestUpdateRuleGuards(t *testing.T) {
	provisioned := GrafanaAlertRule{
		UID:    "protofleet-device-offline",
		Labels: map[string]string{ruleLabelScope: ruleScopeShared, ruleLabelTemplate: "offline"},
	}
	otherOrg := userRuleFixture("pfu-other", "8")
	// An operator-hidden user rule: mutability must not exceed visibility.
	hidden := userRuleFixture("pfu-hidden", "7")
	hidden.Labels[ruleLabelScope] = ruleScopeInternal
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{provisioned, otherOrg, hidden}}
	svc := NewService(fake.server(t), nil, nil, nil, DestinationPolicy{})

	ids := []string{"protofleet-device-offline", "pfu-other", "pfu-missing", "pfu-hidden"}
	for _, id := range ids {
		_, err := svc.UpdateRule(context.Background(), 7, id, offlineConfig("r", 1800))
		assert.ErrorIsf(t, err, ErrNotFound, "id %q must resolve NotFound", id)
	}
	assert.Nil(t, fake.updated)

	for _, id := range ids {
		err := svc.DeleteRule(context.Background(), 7, id)
		assert.ErrorIsf(t, err, ErrNotFound, "id %q must resolve NotFound", id)
	}
	assert.Empty(t, fake.deletedUID)
}

func TestUpdateRuleKeepsIdentity(t *testing.T) {
	existing := userRuleFixture("pfu-mine", "7")
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{existing}}
	svc := NewService(fake.server(t), nil, nil, nil, DestinationPolicy{})

	updated, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", RuleConfig{
		Name: "Hotter", DurationSeconds: 600,
		Temperature: &TemperatureRuleConfig{MaxCelsius: 90},
	})
	require.NoError(t, err)

	require.NotNil(t, fake.updated)
	assert.Equal(t, "pfu-mine", fake.updated.UID)
	assert.Equal(t, existing.FolderUID, fake.updated.FolderUID)
	assert.Equal(t, existing.RuleGroup, fake.updated.RuleGroup)
	assert.Equal(t, RuleTemplateTemperature, updated.Template)

	// Edits re-pin the group interval so a pin that failed at create converges.
	require.NotNil(t, fake.putGroup)
	assert.Equal(t, userRuleGroupInterval, fake.putGroup.Interval)
	assert.Equal(t, existing.RuleGroup, fake.putGroup.Title)
}

// A delete retry after a half-failed earlier delete (rule gone, silences left)
// must re-sweep the caller's silences even though the rule 404s — while a
// provisioned rule's id must never reach cleanup, or a delete probe could lift
// the org's own pause silence on it.
func TestDeleteRuleSweepIsIdempotentButGuarded(t *testing.T) {
	pauseFor := func(uid string) GrafanaSilence {
		return GrafanaSilence{
			ID:      "sil-" + uid,
			Comment: pauseSilenceCommentMarker,
			Matchers: []GrafanaSilenceMatcher{
				{Name: silenceLabelOrganizationID, Value: "7", IsEqual: true},
				{Name: alertRuleUIDMatcher, Value: uid, IsEqual: true},
			},
		}
	}
	provisioned := GrafanaAlertRule{
		UID:    "protofleet-device-offline",
		Labels: map[string]string{ruleLabelScope: ruleScopeShared, ruleLabelTemplate: "offline"},
	}
	fake := &fakeGrafanaRules{
		listed:   []GrafanaAlertRule{provisioned},
		silences: []GrafanaSilence{pauseFor("pfu-gone"), pauseFor("protofleet-device-offline")},
	}
	svc := NewService(fake.server(t), nil, nil, nil, DestinationPolicy{})

	// Missing rule: uniform NotFound, but the orphaned silence is swept.
	err := svc.DeleteRule(context.Background(), 7, "pfu-gone")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, []string{"sil-pfu-gone"}, fake.deletedSilences)

	// Provisioned rule: NotFound and its pause silence is untouched.
	err = svc.DeleteRule(context.Background(), 7, "protofleet-device-offline")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, []string{"sil-pfu-gone"}, fake.deletedSilences)
}

func TestDeleteRuleCleansRuleScopedSilences(t *testing.T) {
	existing := userRuleFixture("pfu-mine", "7")
	ruleMatchers := []GrafanaSilenceMatcher{
		{Name: silenceLabelOrganizationID, Value: "7", IsEqual: true},
		{Name: alertRuleUIDMatcher, Value: "pfu-mine", IsEqual: true},
	}
	fake := &fakeGrafanaRules{
		listed: []GrafanaAlertRule{existing},
		silences: []GrafanaSilence{
			{ID: "sil-pause", Comment: pauseSilenceCommentMarker, Matchers: ruleMatchers},
			// Rule-scoped maintenance window: must not outlive the rule.
			{ID: "sil-mw", Comment: maintenanceWindowCommentMarker + " planned work", Matchers: ruleMatchers},
			// Device-scoped maintenance window: untouched (no rule matcher).
			{ID: "sil-device", Comment: maintenanceWindowCommentMarker, Matchers: []GrafanaSilenceMatcher{
				{Name: silenceLabelOrganizationID, Value: "7", IsEqual: true},
				{Name: "device_id", Value: "dev-1", IsEqual: true},
			}},
		},
	}
	svc := NewService(fake.server(t), nil, nil, nil, DestinationPolicy{})

	require.NoError(t, svc.DeleteRule(context.Background(), 7, "pfu-mine"))
	assert.Equal(t, "pfu-mine", fake.deletedUID)
	assert.ElementsMatch(t, []string{"sil-pause", "sil-mw"}, fake.deletedSilences)
}
