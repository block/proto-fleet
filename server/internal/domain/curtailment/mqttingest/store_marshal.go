package mqttingest

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// nullInt16FromTarget marshals a Target into sql.NullInt16 for the
// upsert path. Unknown is treated as no-write (null) to avoid
// clobbering a previously-known value.
func nullInt16FromTarget(t Target) sql.NullInt16 {
	switch t {
	case TargetOff:
		return sql.NullInt16{Int16: 0, Valid: true}
	case TargetOn:
		return sql.NullInt16{Int16: 100, Valid: true}
	case TargetUnknown:
		return sql.NullInt16{}
	default:
		return sql.NullInt16{}
	}
}

func targetFromNullInt16(n sql.NullInt16) Target {
	if !n.Valid {
		return TargetUnknown
	}
	return Target(n.Int16)
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
