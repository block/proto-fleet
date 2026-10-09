package devicewrite

import "errors"

// NotDispatched marks explicit evidence that no outbound device operation began.
// Preparation/admission refusals and documented pre-execution transport refusals
// qualify; generic transport or acknowledgement errors do not.
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
