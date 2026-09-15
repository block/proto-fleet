package main

import "errors"

// EX_CONFIG; keep in sync with RestartPreventExitStatus in deployment-files/fleetnode/fleet-node.service.
const operatorActionExitCode = 78

type operatorActionError struct {
	err error
}

func (e operatorActionError) Error() string { return e.err.Error() }
func (e operatorActionError) Unwrap() error { return e.err }
func (e operatorActionError) ExitCode() int { return operatorActionExitCode }

func operatorActionRequired(err error) error {
	if err == nil {
		return nil
	}
	var existing operatorActionError
	if errors.As(err, &existing) {
		return err
	}
	return operatorActionError{err: err}
}
