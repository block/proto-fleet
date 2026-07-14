package alerts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Must match infrastructure/metrics/contract.go, which cannot be imported
// here (it imports this package).
const (
	metricDeviceOnline                = "fleet_device_online"
	metricDeviceHashing               = "fleet_device_hashing"
	metricDeviceHashrateTerahash      = "fleet_device_hashrate_terahash"
	metricDeviceTemperatureMaxCelsius = "fleet_device_temperature_max_celsius"
)

const (
	ruleLabelOrigin = "proto_fleet_origin"
	ruleOriginUser  = "user"

	// Round-trips the RuleConfig so edits never parse compiled SQL back apart.
	ruleAnnotationConfig = "proto_fleet_config"

	timescaleDatasourceUID   = "protofleet-timescaledb"
	userRuleGroupInterval    = int64(30)
	userRuleEvalWindowMinute = 10

	// Grafana caps alert-rule titles at 190 characters.
	maxRuleNameLength = 190

	// Each rule is a recurring SQL query against the metrics hypertable.
	maxUserRulesPerOrg = 50

	// Bounds the absolute hashrate threshold after PH→TH normalization.
	maxAbsoluteTerahash = 1e9
)

// userRuleOrgSlug names the per-org folder and the rule_group label; each rule
// gets its own Grafana group (see compileUserRule) so group writes never race.
func userRuleOrgSlug(orgID int64) string {
	return "proto-fleet-user-" + strconv.FormatInt(orgID, 10)
}

func (s *Service) CreateRule(ctx context.Context, orgID int64, cfg RuleConfig) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateRuleConfig(cfg); err != nil {
		return nil, err
	}
	folderUID := userRuleOrgSlug(orgID)
	if err := s.grafana.EnsureFolder(ctx, folderUID, fmt.Sprintf("Proto Fleet User Rules (org %d)", orgID)); err != nil {
		return nil, err
	}
	uid, err := newUserRuleUID()
	if err != nil {
		return nil, err
	}
	body, err := compileUserRule(orgID, uid, cfg)
	if err != nil {
		return nil, err
	}
	created, err := s.createRuleWithinQuota(ctx, orgID, body)
	if err != nil {
		return nil, err
	}
	// Pin the fresh per-rule group's evaluation interval. Best-effort: a
	// default-interval group still evaluates; `for` carries the sustain semantics.
	group := GrafanaRuleGroup{
		Title:     created.RuleGroup,
		FolderUID: folderUID,
		Interval:  userRuleGroupInterval,
		Rules:     []GrafanaAlertRule{*created},
	}
	if err := s.grafana.SetRuleGroup(ctx, group); err != nil {
		slog.Warn("alerts.user_rule_group_interval", "org_id", orgID, "error", err)
	}
	out := grafanaRuleToDomain(orgID, *created)
	return &out, nil
}

// createRuleWithinQuota serializes the quota read-then-create — the only
// section where concurrent creates could exceed the cap.
func (s *Service) createRuleWithinQuota(ctx context.Context, orgID int64, body GrafanaAlertRule) (*GrafanaAlertRule, error) {
	s.userRuleMu.Lock()
	defer s.userRuleMu.Unlock()
	existing, err := s.grafana.ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatInt(orgID, 10)
	userCount := 0
	for _, gr := range existing {
		if ruleVisibleToOrg(gr, want) && gr.Labels[ruleLabelOrigin] == ruleOriginUser {
			userCount++
		}
	}
	if userCount >= maxUserRulesPerOrg {
		return nil, fleeterror.NewFailedPreconditionErrorf("rule limit reached (%d); delete a rule first", maxUserRulesPerOrg)
	}
	return s.grafana.CreateAlertRule(ctx, body)
}

func (s *Service) UpdateRule(ctx context.Context, orgID int64, id string, cfg RuleConfig) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateRuleConfig(cfg); err != nil {
		return nil, err
	}
	current, err := s.requireUserRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	body, err := compileUserRule(orgID, id, cfg)
	if err != nil {
		return nil, err
	}
	// Keep group/folder identity stable so pause silences (matched by UID) survive edits.
	body.FolderUID = current.FolderUID
	body.RuleGroup = current.RuleGroup
	body.IsPaused = current.IsPaused
	updated, err := s.grafana.UpdateAlertRule(ctx, body)
	if err != nil {
		return nil, err
	}
	out := grafanaRuleToDomain(orgID, *updated)
	// The write is committed; misreporting it as failed over a silence-read
	// hiccup invites confused retries, so degrade to the rule's own state.
	if paused, err := s.pauseSilencedRules(ctx, orgID); err != nil {
		slog.Warn("alerts.user_rule_update_pause_state", "org_id", orgID, "rule_id", id, "error", err)
	} else if paused[out.ID] {
		out.Enabled = false
	}
	return &out, nil
}

func (s *Service) DeleteRule(ctx context.Context, orgID int64, id string) error {
	if err := requireOrg(orgID); err != nil {
		return err
	}
	if _, err := s.requireUserRule(ctx, orgID, id); err != nil {
		return err
	}
	if err := s.grafana.DeleteAlertRule(ctx, id); err != nil && !IsNotFound(err) {
		return err
	}
	// Pause silences and rule-scoped maintenance windows are inert once the
	// rule is gone; clean them up, but don't fail the delete over them.
	err := s.removeSilencesTargetingRule(ctx, orgID, id, func(sil GrafanaSilence) bool {
		return isPauseSilence(sil) || isMaintenanceWindowSilence(sil)
	})
	if err != nil {
		slog.Warn("alerts.user_rule_delete_silence_cleanup", "org_id", orgID, "rule_id", id, "error", err)
	}
	return nil
}

// requireUserRule resolves NotFound for missing rules, provisioned rules, other
// orgs' rules, and operator-hidden rules alike, so probing ids can't distinguish
// them and mutability never exceeds visibility (ruleVisibleToOrg).
func (s *Service) requireUserRule(ctx context.Context, orgID int64, id string) (*GrafanaAlertRule, error) {
	if id == "" {
		return nil, fleeterror.NewInvalidArgumentError("rule id is required")
	}
	rule, err := s.grafana.GetAlertRule(ctx, id)
	if err != nil {
		if IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if rule.Labels[ruleLabelOrigin] != ruleOriginUser {
		return nil, ErrNotFound
	}
	if rule.Labels[ruleLabelOrganizationID] != strconv.FormatInt(orgID, 10) {
		return nil, ErrNotFound
	}
	if !ruleVisibleToOrg(*rule, strconv.FormatInt(orgID, 10)) {
		return nil, ErrNotFound
	}
	return rule, nil
}

func newUserRuleUID() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate rule uid: %w", err)
	}
	return "pfu-" + hex.EncodeToString(b), nil
}

func validateRuleConfig(cfg RuleConfig) error {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return fleeterror.NewInvalidArgumentError("rule name is required")
	}
	if utf8.RuneCountInString(name) > maxRuleNameLength {
		return fleeterror.NewInvalidArgumentErrorf("rule name must be at most %d characters", maxRuleNameLength)
	}
	if cfg.DurationSeconds < 60 || cfg.DurationSeconds > 86400 {
		return fleeterror.NewInvalidArgumentError("duration must be between 60 seconds and 24 hours")
	}
	set := 0
	for _, present := range []bool{cfg.Offline != nil, cfg.Hashrate != nil, cfg.Temperature != nil} {
		if present {
			set++
		}
	}
	if set != 1 {
		return fleeterror.NewInvalidArgumentError("exactly one of offline, hashrate, or temperature must be set")
	}
	if h := cfg.Hashrate; h != nil {
		if math.IsNaN(h.Value) || math.IsInf(h.Value, 0) {
			return fleeterror.NewInvalidArgumentError("hashrate value must be a finite number")
		}
		switch h.Mode {
		case HashrateModePctExpected:
			if h.Value <= 0 || h.Value > 100 {
				return fleeterror.NewInvalidArgumentError("hashrate percent must be greater than 0 and at most 100")
			}
		case HashrateModeAbsolute:
			if h.Value <= 0 {
				return fleeterror.NewInvalidArgumentError("hashrate value must be greater than 0")
			}
			if h.Unit != HashrateUnitTerahash && h.Unit != HashrateUnitPetahash {
				return fleeterror.NewInvalidArgumentError("hashrate unit must be TH or PH")
			}
			if absoluteTerahash(*h) > maxAbsoluteTerahash {
				return fleeterror.NewInvalidArgumentError("hashrate threshold is too large")
			}
		default:
			return fleeterror.NewInvalidArgumentError("hashrate mode must be pct_expected or absolute")
		}
	}
	if t := cfg.Temperature; t != nil {
		if math.IsNaN(t.MaxCelsius) || t.MaxCelsius <= 0 || t.MaxCelsius > 150 {
			return fleeterror.NewInvalidArgumentError("temperature must be greater than 0 and at most 150 °C")
		}
	}
	return nil
}

func absoluteTerahash(h HashrateRuleConfig) float64 {
	if h.Unit == HashrateUnitPetahash {
		return h.Value * 1000
	}
	return h.Value
}

func compileUserRule(orgID int64, uid string, cfg RuleConfig) (GrafanaAlertRule, error) {
	sql, summary, description := compileTemplate(orgID, cfg)
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return GrafanaAlertRule{}, fmt.Errorf("marshal rule config: %w", err)
	}
	data, err := json.Marshal([]map[string]any{
		{
			"refId":             "A",
			"relativeTimeRange": map[string]any{"from": userRuleEvalWindowMinute * 60, "to": 0},
			"datasourceUid":     timescaleDatasourceUID,
			"model":             map[string]any{"refId": "A", "format": "table", "rawSql": sql},
		},
		{
			"refId":         "B",
			"datasourceUid": "__expr__",
			"model":         map[string]any{"refId": "B", "type": "math", "expression": "$A"},
		},
	})
	if err != nil {
		return GrafanaAlertRule{}, fmt.Errorf("marshal rule data: %w", err)
	}
	org := strconv.FormatInt(orgID, 10)
	return GrafanaAlertRule{
		UID:       uid,
		FolderUID: userRuleOrgSlug(orgID),
		// One Grafana group per rule: group PUTs (interval pinning) replace the
		// whole group, so sharing one would let concurrent creates erase siblings.
		RuleGroup: uid,
		Title:     strings.TrimSpace(cfg.Name),
		Condition: "B",
		Data:      data,
		For:       fmt.Sprintf("%ds", cfg.DurationSeconds),
		// Match the provisioned defaults: missing data is healthy; the unscoped
		// error row is operator-only (history is org-filtered), so orgs never see it.
		NoDataState:  "OK",
		ExecErrState: "Error",
		Labels: map[string]string{
			ruleLabelOrganizationID: org,
			ruleLabelOrigin:         ruleOriginUser,
			ruleLabelSeverity:       "warning",
			ruleLabelTemplate:       string(cfg.Template()),
			ruleLabelRuleGroup:      userRuleOrgSlug(orgID),
		},
		Annotations: map[string]string{
			"summary":            summary,
			"description":        description,
			ruleAnnotationConfig: string(configJSON),
		},
	}, nil
}

// latestValueSQL is the shared per-device skeleton: newest sample of one metric
// per device in the eval window, org-scoped, matching on the HAVING clause.
func latestValueSQL(org, metric, having string) string {
	return fmt.Sprintf(`SELECT
    organization_id,
    device_id,
    1 AS value
FROM notification_metric_sample
WHERE metric = '%s'
  AND organization_id = '%s'
  AND time > NOW() - INTERVAL '%d minutes'
GROUP BY organization_id, device_id
HAVING %s`, metric, org, userRuleEvalWindowMinute, having)
}

// compileTemplate renders the org-scoped SQL plus human summary/description.
// Every interpolated value is a server-validated number; the org id is taken
// from the session, so no request string ever reaches the SQL.
func compileTemplate(orgID int64, cfg RuleConfig) (sql, summary, description string) {
	org := strconv.FormatInt(orgID, 10)
	dur := humanizeDuration(cfg.DurationSeconds)
	switch {
	case cfg.Offline != nil:
		sql = latestValueSQL(org, metricDeviceOnline, "last(value, time) = 0")
		summary = fmt.Sprintf("Device is offline for at least %s.", dur)
		description = fmt.Sprintf("Device {{ $labels.device_id }} (org {{ $labels.organization_id }})\nhas been reporting %s=0 for at least %s.", metricDeviceOnline, dur)
	case cfg.Hashrate != nil && cfg.Hashrate.Mode == HashrateModePctExpected:
		ratio := formatRatio(cfg.Hashrate.Value)
		sql = latestValueSQL(org, metricDeviceHashing, "last(value, time) < "+ratio)
		summary = fmt.Sprintf("Device hashrate is below %s%% of expected for at least %s.", formatFloat(cfg.Hashrate.Value), dur)
		description = fmt.Sprintf("Device {{ $labels.device_id }} (org {{ $labels.organization_id }})\nhas been hashing below %s%% of its expected rate for at least %s.", formatFloat(cfg.Hashrate.Value), dur)
	case cfg.Hashrate != nil:
		threshold := formatFloat(absoluteTerahash(*cfg.Hashrate))
		// The observed-TH metric keeps reporting ~0 for curtailed/paused miners; the
		// ratio metric's 1.0 doubles as the "not expected to hash" sentinel, so only
		// suppress when the sentinel coincides with no observed hashing — a positive
		// reading (at-nameplate or no-nameplate devices also sit at ratio 1) still alerts.
		sql = fmt.Sprintf(`WITH latest AS (
    SELECT
        organization_id,
        device_id,
        metric,
        last(value, time) AS latest_value
    FROM notification_metric_sample
    WHERE metric IN ('%s', '%s')
      AND organization_id = '%s'
      AND time > NOW() - INTERVAL '%d minutes'
    GROUP BY organization_id, device_id, metric
)
SELECT
    obs.organization_id,
    obs.device_id,
    1 AS value
FROM latest AS obs
JOIN latest AS gate
  ON gate.organization_id = obs.organization_id
 AND gate.device_id = obs.device_id
 AND gate.metric = '%s'
WHERE obs.metric = '%s'
  AND (gate.latest_value < 1 OR obs.latest_value > 0)
  AND obs.latest_value < %s`,
			metricDeviceHashrateTerahash, metricDeviceHashing, org, userRuleEvalWindowMinute,
			metricDeviceHashing, metricDeviceHashrateTerahash, threshold)
		summary = fmt.Sprintf("Device hashrate is below %s %s/s for at least %s.", formatFloat(cfg.Hashrate.Value), cfg.Hashrate.Unit, dur)
		description = fmt.Sprintf("Device {{ $labels.device_id }} (org {{ $labels.organization_id }})\nhas been hashing below %s %s/s for at least %s.", formatFloat(cfg.Hashrate.Value), cfg.Hashrate.Unit, dur)
	case cfg.Temperature != nil:
		limit := formatFloat(cfg.Temperature.MaxCelsius)
		// Freshness gate mirrors the provisioned temperature rule: a device that
		// stopped reporting while hot must not keep firing on an unconfirmable reading.
		sql = fmt.Sprintf(`WITH latest_per_kind AS (
    SELECT
        organization_id,
        device_id,
        sensor_kind,
        last(value, time) AS latest_temp,
        max(time) AS last_sample_time
    FROM notification_metric_sample
    WHERE metric = '%s'
      AND organization_id = '%s'
      AND time > NOW() - INTERVAL '%d minutes'
    GROUP BY organization_id, device_id, sensor_kind
)
SELECT
    organization_id,
    device_id,
    max(latest_temp) AS latest_temp
FROM latest_per_kind
WHERE last_sample_time > NOW() - INTERVAL '3 minutes'
GROUP BY organization_id, device_id
HAVING max(latest_temp) > %s`, metricDeviceTemperatureMaxCelsius, org, userRuleEvalWindowMinute, limit)
		summary = fmt.Sprintf("Max sensor temperature for device is above %sC for at least %s.", limit, dur)
		description = fmt.Sprintf("Maximum sensor temperature for device {{ $labels.device_id }}\nhas been above %sC for at least %s.", limit, dur)
	}
	return sql, summary, description
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// formatRatio renders percent/100 without binary-division drift (33.3 → "0.333",
// not "0.33299999999999996") so the SQL matches what the summary claims.
func formatRatio(percent float64) string {
	s := strconv.FormatFloat(percent/100, 'f', 10, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

func humanizeDuration(seconds int32) string {
	switch {
	case seconds%3600 == 0:
		if seconds == 3600 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", seconds/3600)
	case seconds%60 == 0:
		if seconds == 60 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", seconds/60)
	default:
		return fmt.Sprintf("%d seconds", seconds)
	}
}
