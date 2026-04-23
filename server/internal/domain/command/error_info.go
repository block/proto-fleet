package command

import (
	"database/sql"
	"errors"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// maxErrorInfoRunes caps per-device error strings persisted in
// command_on_device_log.error_info so plugin payloads can't grow the row
// indefinitely.
const maxErrorInfoRunes = 2048

const truncationSuffix = "... [truncated]"

// genericWorkerErrorMessage is the operator-safe placeholder written to
// command_on_device_log.error_info when a worker error is not a FleetError
// (plugin/device/transport errors). The raw err.Error() is still logged
// via slog so admins can debug.
const genericWorkerErrorMessage = "command failed"

// boundedErrorInfo returns a sql.NullString truncated to maxErrorInfoRunes
// runes. Empty input returns an invalid NullString so success rows stay NULL.
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

// sanitizedErrorInfo produces an operator-safe sql.NullString for persistence
// in command_on_device_log.error_info. Only server-authored FleetError values
// are surfaced verbatim; everything else (plugin/device/transport errors)
// collapses to the generic marker so untrusted text can't be injected into
// the operator-visible result.
func sanitizedErrorInfo(err error) sql.NullString {
	if err == nil {
		return sql.NullString{}
	}
	var fe fleeterror.FleetError
	if errors.As(err, &fe) {
		msg := fe.GRPCCode.String()
		if fe.DebugMessage != "" {
			msg = msg + ": " + fe.DebugMessage
		}
		return boundedErrorInfo(msg)
	}
	return boundedErrorInfo(genericWorkerErrorMessage)
}
