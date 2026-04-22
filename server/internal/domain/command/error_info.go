package command

import (
	"database/sql"
	"unicode/utf8"
)

// maxErrorInfoRunes bounds per-device error strings persisted in
// command_on_device_log.error_info to avoid runaway row sizes when plugins
// return large upstream payloads or stack traces.
const maxErrorInfoRunes = 2048

const truncationSuffix = "... [truncated]"

// boundedErrorInfo returns a sql.NullString whose String is safely truncated
// to at most maxErrorInfoRunes runes. Empty input yields an invalid NullString
// so the column remains NULL on success.
func boundedErrorInfo(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	if utf8.RuneCountInString(s) <= maxErrorInfoRunes {
		return sql.NullString{String: s, Valid: true}
	}

	keep := maxErrorInfoRunes - utf8.RuneCountInString(truncationSuffix)
	if keep < 0 {
		keep = 0
	}

	runes := 0
	cut := len(s)
	for i := range s {
		if runes == keep {
			cut = i
			break
		}
		runes++
	}
	return sql.NullString{String: s[:cut] + truncationSuffix, Valid: true}
}

// workerErrorInfo wraps boundedErrorInfo with the convention that a nil error
// produces a NULL value (for SUCCESS rows) and any non-nil error is stored.
func workerErrorInfo(err error) sql.NullString {
	if err == nil {
		return sql.NullString{}
	}
	return boundedErrorInfo(err.Error())
}
