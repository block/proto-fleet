package mqttingest

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
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

func nullStringFrom(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func stringFromNullString(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

func nullUUIDFrom(s string) uuid.NullUUID {
	if s == "" {
		return uuid.NullUUID{}
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}
}

func stringFromNullUUID(n uuid.NullUUID) string {
	if !n.Valid {
		return ""
	}
	return n.UUID.String()
}
