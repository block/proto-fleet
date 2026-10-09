package devicewrite

import "errors"

// NotDispatched marks an error from preparation or admission that is known to
// have failed before any outbound device operation. Never use it for transport
// or acknowledgement errors, whose physical outcome may be ambiguous.
func NotDispatched(err error) error {
	if err == nil {
		return nil
	}
	return &notDispatchedError{err: err}
}

type notDispatchedError struct{ err error }

func (e *notDispatchedError) Error() string { return e.err.Error() }
func (e *notDispatchedError) Unwrap() error { return e.err }

// IsNotDispatched recognizes only explicitly classified pre-outbound errors.
func IsNotDispatched(err error) bool {
	var preflight *notDispatchedError
	return errors.As(err, &preflight)
}
