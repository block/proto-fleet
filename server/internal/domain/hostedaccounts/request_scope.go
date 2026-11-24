package hostedaccounts

import (
	"context"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
)

const RequestAccountHeader = "X-Fleet-Account-ID"

type requestAccountKey struct{}

// WithRequestAccountID captures the selected account independently of mutable
// transport headers. Capturing selection does not authorize access to the account.
func WithRequestAccountID(ctx context.Context, accountID string) (context.Context, error) {
	id, err := canonicalAccountID(accountID)
	if err != nil {
		return ctx, err
	}
	if previous, ok := RequestAccountID(ctx); ok && previous != id {
		return ctx, fleeterror.NewForbiddenError("request account cannot change during a request")
	}
	return context.WithValue(ctx, requestAccountKey{}, id), nil
}

func RequestAccountID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestAccountKey{}).(string)
	return id, ok
}

// RequireRequestAccount validates the requested account and, when customer mode
// is selected, binds it to that selection. Each handler must still resolve live
// account authority. An omitted header permits authenticated host administration.
func RequireRequestAccount(ctx context.Context, requestedAccountID string) error {
	id, err := canonicalAccountID(requestedAccountID)
	if err != nil {
		return err
	}
	if selected, ok := RequestAccountID(ctx); ok && selected != id {
		return fleeterror.NewForbiddenError("requested account does not match the selected account")
	}
	return nil
}

func canonicalAccountID(accountID string) (string, error) {
	id, err := uuid.Parse(accountID)
	if err != nil || id == uuid.Nil || id.String() != strings.ToLower(accountID) {
		return "", fleeterror.NewInvalidArgumentError("account ID must be a nonzero UUID")
	}
	return id.String(), nil
}
