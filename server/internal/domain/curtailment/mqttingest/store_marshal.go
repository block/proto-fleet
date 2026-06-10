package mqttingest

import (
	"database/sql"
	"time"
)

// nullStringFromTarget maps Unknown to NULL for partial upserts.
func nullStringFromTarget(t Target) sql.NullString {
	switch t {
	case TargetOff:
		return sql.NullString{String: "OFF", Valid: true}
	case TargetOn:
		return sql.NullString{String: "ON", Valid: true}
	case TargetUnknown:
		return sql.NullString{}
	default:
		return sql.NullString{}
	}
}

func targetFromNullString(n sql.NullString) Target {
	if !n.Valid {
		return TargetUnknown
	}
	switch n.String {
	case "OFF":
		return TargetOff
	case "ON":
		return TargetOn
	default:
		return TargetUnknown
	}
}

func stringsFromTargets(targets []Target) []string {
	if len(targets) == 0 {
		return nil
	}
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		if s := nullStringFromTarget(target); s.Valid {
			out = append(out, s.String)
		}
	}
	return out
}

func targetsFromStrings(values []string) []Target {
	if len(values) == 0 {
		return nil
	}
	out := make([]Target, 0, len(values))
	for _, value := range values {
		target := targetFromNullString(sql.NullString{String: value, Valid: true})
		if target != TargetUnknown {
			out = append(out, target)
		}
	}
	return out
}

func int32OrDefault(n sql.NullInt32, def int32) int32 {
	if !n.Valid {
		return def
	}
	return n.Int32
}

func nullTimeFrom(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

func timeFromNullTime(n sql.NullTime) time.Time {
	if !n.Valid {
		return time.Time{}
	}
	return n.Time.UTC()
}

func stringOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func nullStringFrom(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func edgeDirectionFromNullString(n sql.NullString) EdgeDirection {
	if !n.Valid {
		return EdgeNone
	}
	switch n.String {
	case EdgeOnToOff.String():
		return EdgeOnToOff
	case EdgeOffToOn.String():
		return EdgeOffToOn
	case EdgeWatchdogOff.String():
		return EdgeWatchdogOff
	default:
		return EdgeNone
	}
}

func pendingEdgeFromRow(
	direction sql.NullString,
	target sql.NullString,
	targetAt sql.NullTime,
	receivedAt sql.NullTime,
	receivedBroker sql.NullString,
	priorEdgeAt sql.NullTime,
	retryAt sql.NullTime,
) *PendingEdge {
	d := edgeDirectionFromNullString(direction)
	t := targetFromNullString(target)
	if d == EdgeNone || t == TargetUnknown {
		return nil
	}
	return &PendingEdge{
		Direction:      d,
		Target:         t,
		TargetAt:       timeFromNullTime(targetAt),
		ReceivedAt:     timeFromNullTime(receivedAt),
		ReceivedBroker: stringFromNullString(receivedBroker),
		PriorEdgeAt:    timeFromNullTime(priorEdgeAt),
		RetryAt:        timeFromNullTime(retryAt),
	}
}

func stringFromNullString(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}
