package devicewrite

import (
	"errors"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// ErrStaleBinding requires fresh device binding and admission before retrying.
var ErrStaleBinding = errors.New("device binding or write admission changed")

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
	return classifyOutcome(err).kind == sdk.WriteNotDispatched
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

// IsUncertainDelivery prevents automatic retries of possibly accepted work.
func IsUncertainDelivery(err error) bool {
	return classifyOutcome(err).kind == sdk.WriteUnknown
}

// IsCompleted requires explicit evidence that this invocation and all its work ended.
func IsCompleted(err error) bool {
	return classifyOutcome(err).kind == sdk.WriteCompleted
}

type writeOutcome struct {
	kind     string
	evidence *errdetails.ErrorInfo
}

func classifyOutcome(err error) writeOutcome {
	evidence := sdk.WriteEvidence(err)
	kind := evidence.GetReason()
	var uncertain *uncertainDeliveryError
	var preflight *notDispatchedError
	switch {
	case errors.As(err, &uncertain), kind == sdk.WriteUnknown:
		kind = sdk.WriteUnknown
	case errors.As(err, &preflight):
		kind = sdk.WriteNotDispatched
	}
	return writeOutcome{kind: kind, evidence: evidence}
}
