package control

import "errors"

// IsCommandNotEnqueued identifies a command rejected before it entered the
// outbound queue. A disconnect after enqueue can use the same underlying
// error, so callers must not infer this evidence from error codes or sentinels.
func IsCommandNotEnqueued(err error) bool {
	var rejected *commandNotEnqueuedError
	return errors.As(err, &rejected)
}

func commandNotEnqueued(err error) error {
	return &commandNotEnqueuedError{err: err}
}

type commandNotEnqueuedError struct{ err error }

func (e *commandNotEnqueuedError) Error() string { return e.err.Error() }
func (e *commandNotEnqueuedError) Unwrap() error { return e.err }
