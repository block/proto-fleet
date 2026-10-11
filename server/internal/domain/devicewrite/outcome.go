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

// UncertainDelivery retains recovery requirements when an intermediary may still
// queue or dispatch the request after its caller returns. Firmware API ordering
// cannot establish that a Fleet Node transport failure reached that boundary.
func UncertainDelivery(err error) error {
	if err == nil {
		return nil
	}
	return &uncertainDeliveryError{err: err}
}

type uncertainDeliveryError struct{ err error }

func (e *uncertainDeliveryError) Error() string { return e.err.Error() }
func (e *uncertainDeliveryError) Unwrap() error { return e.err }
func isUncertainDelivery(err error) bool {
	var uncertain *uncertainDeliveryError
	return errors.As(err, &uncertain)
}
