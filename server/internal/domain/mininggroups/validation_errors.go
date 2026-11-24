package mininggroups

import "errors"

type inventoryStaleError struct {
	message string
	cause   error
}

func (e *inventoryStaleError) Unwrap() error { return e.cause }

func (e *inventoryStaleError) Error() string { return e.message }

func newInventoryStaleError(message string) error {
	return &inventoryStaleError{message: message}
}

func isInventoryStale(err error) bool {
	var stale *inventoryStaleError
	return errors.As(err, &stale)
}

type workflowChangedError struct{ message string }

func (e *workflowChangedError) Error() string { return e.message }

func newWorkflowChangedError(message string) error {
	return &workflowChangedError{message: message}
}

func isWorkflowChanged(err error) bool {
	var changed *workflowChangedError
	return errors.As(err, &changed)
}
