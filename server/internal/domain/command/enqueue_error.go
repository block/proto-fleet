package command

import "errors"

// EnqueueUncertainError means the durable batch transaction was attempted but
// its commit outcome cannot be treated as definitely absent.
type EnqueueUncertainError struct {
	err error
}

func (e *EnqueueUncertainError) Error() string {
	return e.err.Error()
}

func (e *EnqueueUncertainError) Unwrap() error {
	return e.err
}

func enqueueUncertain(err error) error {
	if err == nil {
		return nil
	}
	return &EnqueueUncertainError{err: err}
}

func NewEnqueueUncertainError(err error) error {
	return enqueueUncertain(err)
}

func IsEnqueueUncertain(err error) bool {
	var target *EnqueueUncertainError
	return errors.As(err, &target)
}
