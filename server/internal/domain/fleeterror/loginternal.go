package fleeterror

import "log/slog"

// LogInternal records the raw error server-side and returns a generic
// client-safe internal error so backend details (table names, indexes,
// storage failure modes) don't leak to RPC callers.
func LogInternal(component, op, clientMsg string, err error) error {
	if err == nil {
		return NewInternalError(clientMsg)
	}
	slog.Error(component+" internal error", "op", op, "error", err)
	return NewInternalError(clientMsg)
}
