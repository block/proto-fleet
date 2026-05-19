package promqlshim

import (
	"context"
	"fmt"
	"time"
)

type resultRow struct {
	labels map[string]string
	ts     time.Time
	value  float64
}

func runRule(ctx context.Context, db DB, ruleID, orgFilter string, now time.Time) ([]resultRow, error) {
	rule, ok := ruleByID(ruleID)
	if !ok {
		return nil, fmt.Errorf("unknown rule_id %q", ruleID)
	}
	switch ruleID {
	case "device-offline-default":
		return runDeviceOffline(ctx, db, rule, orgFilter, now)
	case "device-temperature-default":
		return runDeviceTemperatureHigh(ctx, db, rule, orgFilter, now)
	case "telemetry-poll-failure-default":
		return runTelemetryPollFailureRate(ctx, db, rule, orgFilter, now)
	default:
		return nil, fmt.Errorf("rule_id %q has no SQL plan", ruleID)
	}
}

func ruleByID(id string) (Rule, bool) {
	for _, r := range BuiltinRules() {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

const sqlActiveOrgs = `
SELECT DISTINCT organization_id
  FROM notification_device_metrics
 WHERE time >= $1 - INTERVAL '24 hours'
 UNION
SELECT DISTINCT organization_id
  FROM notification_telemetry_poll_events
 WHERE time >= $1 - INTERVAL '24 hours'
`

const sqlDeviceOffline = `
SELECT device_id, COALESCE(device_group, '') AS device_group, COALESCE(driver, '') AS driver
  FROM device_online_v1
 WHERE organization_id = $1
   AND (online_bool = false OR last_seen_at < $2 - INTERVAL '10 minutes')
`

const sqlDeviceTemperatureHigh = `
WITH latest AS (
  SELECT DISTINCT ON (organization_id, device_id, sensor_kind)
         organization_id, device_id, sensor_kind, temperature_max_c, time,
         device_group, driver
    FROM notification_device_temperature
   WHERE organization_id = $1
     AND time >= $2 - INTERVAL '15 minutes'
ORDER BY organization_id, device_id, sensor_kind, time DESC
)
SELECT device_id, sensor_kind, temperature_max_c,
       COALESCE(device_group, '') AS device_group,
       COALESCE(driver, '')       AS driver
  FROM latest
 WHERE temperature_max_c > 90
`

const sqlTelemetryPollFailureRate = `
SELECT
    COUNT(*) FILTER (WHERE result = 'failure') AS failures,
    COUNT(*) FILTER (WHERE result = 'success') AS successes
  FROM notification_telemetry_poll_events
 WHERE organization_id = $1
   AND time >= $2 - INTERVAL '10 minutes'
   AND time <  $2
`

// resolveOrgs returns the orgs the rule should evaluate.
func resolveOrgs(ctx context.Context, db DB, orgFilter string, now time.Time) ([]int64, error) {
	if orgFilter != "" {
		var v int64
		if _, err := fmt.Sscanf(orgFilter, "%d", &v); err != nil {
			return nil, fmt.Errorf("organization_id %q must be a positive integer", orgFilter)
		}
		return []int64{v}, nil
	}
	rows, err := db.QueryContext(ctx, sqlActiveOrgs, now)
	if err != nil {
		return nil, fmt.Errorf("active orgs query: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("active orgs scan: %w", err)
		}
		out = append(out, v)
	}
	return out, fmt.Errorf("active orgs sql error: %w", rows.Err())
}

func runDeviceOffline(ctx context.Context, db DB, rule Rule, orgFilter string, now time.Time) ([]resultRow, error) {
	orgs, err := resolveOrgs(ctx, db, orgFilter, now)
	if err != nil {
		return nil, err
	}
	var out []resultRow
	for _, org := range orgs {
		rows, err := db.QueryContext(ctx, sqlDeviceOffline, org, now)
		if err != nil {
			return nil, fmt.Errorf("device-offline query: %w", err)
		}
		for rows.Next() {
			var deviceID, deviceGroup, driver string
			if err := rows.Scan(&deviceID, &deviceGroup, &driver); err != nil {
				defer rows.Close()
				return nil, fmt.Errorf("device-offline scan: %w", err)
			}
			out = append(out, resultRow{
				labels: alertLabels(rule, org, map[string]string{
					"device_id":    deviceID,
					"device_group": deviceGroup,
					"driver":       driver,
				}),
				ts:    now,
				value: 1,
			})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("active orgs sql error: %w", err)
		}
	}
	return out, nil
}

func runDeviceTemperatureHigh(ctx context.Context, db DB, rule Rule, orgFilter string, now time.Time) ([]resultRow, error) {
	orgs, err := resolveOrgs(ctx, db, orgFilter, now)
	if err != nil {
		return nil, err
	}
	var out []resultRow
	for _, org := range orgs {
		rows, err := db.QueryContext(ctx, sqlDeviceTemperatureHigh, org, now)
		if err != nil {
			return nil, fmt.Errorf("device-temperature query: %w", err)
		}
		for rows.Next() {
			var deviceID, sensorKind, deviceGroup, driver string
			var tempC float64
			if err := rows.Scan(&deviceID, &sensorKind, &tempC, &deviceGroup, &driver); err != nil {
				defer rows.Close()
				return nil, fmt.Errorf("device-temperature scan: %w", err)
			}
			out = append(out, resultRow{
				labels: alertLabels(rule, org, map[string]string{
					"device_id":    deviceID,
					"device_group": deviceGroup,
					"driver":       driver,
					"sensor_kind":  sensorKind,
				}),
				ts:    now,
				value: tempC,
			})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("active orgs sql error: %w", err)
		}
	}
	return out, nil
}

func runTelemetryPollFailureRate(ctx context.Context, db DB, rule Rule, orgFilter string, now time.Time) ([]resultRow, error) {
	orgs, err := resolveOrgs(ctx, db, orgFilter, now)
	if err != nil {
		return nil, err
	}
	var out []resultRow
	for _, org := range orgs {
		rows, err := db.QueryContext(ctx, sqlTelemetryPollFailureRate, org, now)
		if err != nil {
			return nil, fmt.Errorf("telemetry-poll-failure query: %w", err)
		}
		var failures, successes int64
		consumed := false
		for rows.Next() {
			if err := rows.Scan(&failures, &successes); err != nil {
				defer rows.Close()
				return nil, fmt.Errorf("telemetry-poll-failure scan: %w", err)
			}
			consumed = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("active orgs sql error: %w", err)
		}
		if !consumed || failures <= successes {
			continue
		}
		out = append(out, resultRow{
			labels: alertLabels(rule, org, nil),
			ts:     now,
			value:  float64(failures),
		})
	}
	return out, nil
}

func alertLabels(rule Rule, orgID int64, extras map[string]string) map[string]string {
	out := make(map[string]string, 3+len(extras))
	out["rule_id"] = rule.ID
	out["organization_id"] = fmt.Sprintf("%d", orgID)
	out["severity"] = rule.Severity
	for k, v := range extras {
		if v == "" {
			continue
		}
		out[k] = v
	}
	return out
}
