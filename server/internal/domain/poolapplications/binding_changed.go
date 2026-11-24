package poolapplications

import (
	"errors"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// A changed reviewed binding cannot become valid again by retrying its old
// request. Keep this distinct from temporary preconditions such as maintenance.
type bindingChangedError struct{ error }

func newBindingChangedError(message string) error {
	return &bindingChangedError{error: fleeterror.NewFailedPreconditionError(message)}
}

func (e *bindingChangedError) Unwrap() error { return e.error }

func IsBindingChanged(err error) bool {
	var changed *bindingChangedError
	return errors.As(err, &changed)
}
