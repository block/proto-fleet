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

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	ruleLabelOrigin = "proto_fleet_origin"
	ruleOriginUser  = "user"

	// Round-trips the RuleConfig so edits never parse compiled SQL back apart.
	ruleAnnotationConfig = "proto_fleet_config"

	timescaleDatasourceUID   = "protofleet-timescaledb"
	userRuleGroupInterval    = int64(30)
	userRuleEvalWindowMinute = 10

	// Each rule is a recurring SQL query against the metrics hypertable.
	maxUserRulesPerOrg = 50
)

func userRuleFolderUID(orgID int64) string {
	return "proto-fleet-user-" + strconv.FormatInt(orgID, 10)
}

func userRuleGroup(orgID int64) string {
	return "proto-fleet-user-" + strconv.FormatInt(orgID, 10)
}

func (s *Service) CreateRule(ctx context.Context, orgID int64, cfg RuleConfig) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateRuleConfig(cfg); err != nil {
		return nil, err
	}
	existing, err := s.ListRules(ctx, orgID)
	if err != nil {
		return nil, err
	}
	userCount := 0
	for _, r := range existing {
		if r.Origin == RuleOriginUser {
			userCount++
		}
	}
	if userCount >= maxUserRulesPerOrg {
		return nil, fleeterror.NewFailedPreconditionErrorf("rule limit reached (%d); delete a rule first", maxUserRulesPerOrg)
	}
	folderUID := userRuleFolderUID(orgID)
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
	created, err := s.grafana.CreateAlertRule(ctx, body)
	if err != nil {
		return nil, err
	}
	// Best-effort: a default-interval group still evaluates; `for` carries the sustain semantics.
	if err := s.grafana.SetRuleGroupInterval(ctx, folderUID, userRuleGroup(orgID), userRuleGroupInterval); err != nil {
		slog.Warn("alerts.user_rule_group_interval", "org_id", orgID, "error", err)
	}
	out := grafanaRuleToDomain(orgID, *created)
	return &out, nil
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
	paused, err := s.pauseSilencedRules(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if paused[out.ID] {
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
	// A leftover pause silence is inert once the rule is gone; don't fail the delete over it.
	if err := s.removePauseSilences(ctx, orgID, id); err != nil {
		slog.Warn("alerts.user_rule_delete_silence_cleanup", "org_id", orgID, "rule_id", id, "error", err)
	}
	return nil
}

// requireUserRule resolves NotFound for missing rules, provisioned rules, and other
// orgs' rules alike, so probing ids can't distinguish the three.
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
	if len(name) > 255 {
		return fleeterror.NewInvalidArgumentError("rule name must be at most 255 characters")
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
		FolderUID: userRuleFolderUID(orgID),
		RuleGroup: userRuleGroup(orgID),
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
			"severity":              "warning",
			"template":              string(cfg.Template()),
			"rule_group":            userRuleGroup(orgID),
		},
		Annotations: map[string]string{
			"summary":            summary,
			"description":        description,
			ruleAnnotationConfig: string(configJSON),
		},
	}, nil
}

// compileTemplate renders the org-scoped SQL plus human summary/description.
// Every interpolated value is a server-validated number; the org id is taken
// from the session, so no request string ever reaches the SQL.
func compileTemplate(orgID int64, cfg RuleConfig) (sql, summary, description string) {
	org := strconv.FormatInt(orgID, 10)
	dur := humanizeDuration(cfg.DurationSeconds)
	switch {
	case cfg.Offline != nil:
		sql = fmt.Sprintf(`SELECT
    organization_id,
    device_id,
    1 AS value
FROM notification_metric_sample
WHERE metric = 'fleet_device_online'
  AND organization_id = '%s'
  AND time > NOW() - INTERVAL '10 minutes'
GROUP BY organization_id, device_id
HAVING last(value, time) = 0`, org)
		summary = fmt.Sprintf("Device is offline for at least %s.", dur)
		description = fmt.Sprintf("Device {{ $labels.device_id }} (org {{ $labels.organization_id }})\nhas been reporting fleet_device_online=0 for at least %s.", dur)
	case cfg.Hashrate != nil && cfg.Hashrate.Mode == HashrateModePctExpected:
		ratio := formatFloat(cfg.Hashrate.Value / 100)
		sql = fmt.Sprintf(`SELECT
    organization_id,
    device_id,
    1 AS value
FROM notification_metric_sample
WHERE metric = 'fleet_device_hashing'
  AND organization_id = '%s'
  AND time > NOW() - INTERVAL '10 minutes'
GROUP BY organization_id, device_id
HAVING last(value, time) < %s`, org, ratio)
		summary = fmt.Sprintf("Device hashrate is below %s%% of expected for at least %s.", formatFloat(cfg.Hashrate.Value), dur)
		description = fmt.Sprintf("Device {{ $labels.device_id }} (org {{ $labels.organization_id }})\nhas been hashing below %s%% of its expected rate for at least %s.", formatFloat(cfg.Hashrate.Value), dur)
	case cfg.Hashrate != nil:
		terahash := cfg.Hashrate.Value
		if cfg.Hashrate.Unit == HashrateUnitPetahash {
			terahash *= 1000
		}
		sql = fmt.Sprintf(`SELECT
    organization_id,
    device_id,
    1 AS value
FROM notification_metric_sample
WHERE metric = 'fleet_device_hashrate_terahash'
  AND organization_id = '%s'
  AND time > NOW() - INTERVAL '10 minutes'
GROUP BY organization_id, device_id
HAVING last(value, time) < %s`, org, formatFloat(terahash))
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
    WHERE metric = 'fleet_device_temperature_max_celsius'
      AND organization_id = '%s'
      AND time > NOW() - INTERVAL '10 minutes'
    GROUP BY organization_id, device_id, sensor_kind
)
SELECT
    organization_id,
    device_id,
    max(latest_temp) AS latest_temp
FROM latest_per_kind
WHERE last_sample_time > NOW() - INTERVAL '3 minutes'
GROUP BY organization_id, device_id
HAVING max(latest_temp) > %s`, org, limit)
		summary = fmt.Sprintf("Max sensor temperature for device is above %sC for at least %s.", limit, dur)
		description = fmt.Sprintf("Maximum sensor temperature for device {{ $labels.device_id }}\nhas been above %sC for at least %s.", limit, dur)
	}
	return sql, summary, description
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
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
