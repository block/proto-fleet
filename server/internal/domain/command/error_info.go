package command

import (
	"database/sql"
	"errors"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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

// workerErrorInfo wraps boundedErrorInfo for callers that already hold a
// server-authored, trusted error string. It echoes err.Error() verbatim
// (bounded by maxErrorInfoRunes) and MUST NOT be used to persist raw errors
// that crossed the plugin or device gRPC boundary -- use sanitizedErrorInfo
// for those so the untrusted text can't be surfaced to operators. Today the
// only safe call site is the reap path (queue.sql writes a short,
// server-authored string before the reaper rehydrates it), which in
// practice goes through msg.ErrorInfo directly rather than this helper.
func workerErrorInfo(err error) sql.NullString {
	if err == nil {
		return sql.NullString{}
	}
	return boundedErrorInfo(err.Error())
}

// genericWorkerErrorMessage is the operator-safe placeholder stored in
// command_on_device_log.error_info when a worker error is not a FleetError
// (e.g. plugin-raised, device-raised, or transport errors). The raw
// err.Error() is expected to be logged server-side by the caller so
// admins can still debug via slog.
const genericWorkerErrorMessage = "command failed"

// sanitizedErrorInfo converts a worker error into an operator-safe
// sql.NullString suitable for persistence in command_on_device_log.error_info
// and later exposure to org members via GetCommandBatchDeviceResults.
//
// Only errors that unwrap to a fleeterror.FleetError are surfaced verbatim
// (truncated to maxErrorInfoRunes): those are server-authored and their
// DebugMessage is part of our controlled API surface. Any other error type --
// including anything that crosses the plugin or device boundary --
// collapses to a short generic marker so adversarial or noisy upstream
// text cannot be injected into the operator-visible result.
//
// A nil error yields an invalid NullString (column stays NULL on SUCCESS
// rows).
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
