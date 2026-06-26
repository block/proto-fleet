package sqlstores

import (
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// emptyToNullString returns a NullString that's Valid only when s is
// non-empty.
func emptyToNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// ptrToNullString returns a NullString that's Valid when s is non-nil.
func ptrToNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// zeroToNullInt64 returns a NullInt64 that's Valid when v > 0. Used at
// the store boundary for "0 means unset" int64 fields.
func zeroToNullInt64(v int64) sql.NullInt64 {
	if v <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

// zeroToNullInt32 returns a NullInt32 that's Valid when v > 0. Used at
// the store boundary for "0 means unset" int32 fields.
func zeroToNullInt32(v int32) sql.NullInt32 {
	if v <= 0 {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: v, Valid: true}
}

// ptrToNullInt64 returns a NullInt64 that's Valid when v is non-nil.
func ptrToNullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

// nullInt64ToPtr returns a pointer to the underlying int64 when Valid,
// otherwise nil. Useful for converting nullable FK columns into the
// pointer-typed domain fields used by sites/buildings.
func nullInt64ToPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// ptrToNullInt32 returns a NullInt32 that's Valid when v is non-nil.
func ptrToNullInt32(v *int32) sql.NullInt32 {
	if v == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *v, Valid: true}
}

// ptrToNullTime returns a NullTime that's Valid when t is non-nil.
func ptrToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// numericFromFloat encodes a Go float64 as a NUMERIC-compatible
// NullString. Zero is treated as "not set" so the column stays NULL.
// Precision is preserved up to the FormatFloat default; the column's
// NUMERIC(10,3) clamp truncates beyond three decimals at write time.
func numericFromFloat(v float64) sql.NullString {
	if v == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: strconv.FormatFloat(v, 'f', -1, 64), Valid: true}
}

// floatFromNumeric decodes a NUMERIC NullString into a float64. Invalid
// or unparseable values yield 0 so callers don't need to thread errors
// for what is, contractually, a numeric column.
func floatFromNumeric(n sql.NullString) float64 {
	if !n.Valid {
		return 0
	}
	v, err := strconv.ParseFloat(n.String, 64)
	if err != nil {
		return 0
	}
	return v
}

// isUniqueViolation returns true when err wraps a Postgres
// unique-constraint violation. Used by store layers to translate
// driver errors into AlreadyExists domain errors.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation {
		return true
	}
	return false
}

func isUniqueViolationOn(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == db.PGUniqueViolation &&
		pgErr.ConstraintName == constraintName
}

func isForeignKeyViolationOn(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == pgErrCodeForeignKeyViolation &&
		pgErr.ConstraintName == constraintName
}
