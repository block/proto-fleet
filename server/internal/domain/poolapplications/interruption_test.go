package poolapplications

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestTemporaryPreparationFailureRequiresRecognizedCause(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		cause     error
		temporary bool
	}{
		{"cancellation", context.Canceled, true},
		{"deadline", context.DeadlineExceeded, true},
		{"lock unavailable", &pgconn.PgError{Code: "55P03"}, true},
		{"statement timeout", &pgconn.PgError{Code: "57014"}, true},
		{"transaction timeout", &pgconn.PgError{Code: "25P04"}, true},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, true},
		{"serialization conflict", &pgconn.PgError{Code: "40001"}, true},
		{"database failover", &pgconn.PgError{Code: "57P01"}, true},
		{"connection lost", driver.ErrBadConn, true},
		{"permission revoked", fleeterror.NewForbiddenError("revoked"), false},
		{"ownership changed", fleeterror.NewFailedPreconditionError("changed"), false},
		{"invalid configuration", fleeterror.NewInvalidArgumentError("invalid"), false},
		{"constraint violation", &pgconn.PgError{Code: "23514"}, false},
		{"unknown failure", errors.New("canceled busy timeout"), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// The transaction/repository boundaries wrap driver errors. Decisions
			// must use the preserved cause, never raw error text or generic status.
			err := fleeterror.NewInternalErrorf("preparation failed: %w", scenario.cause)
			require.Equal(t, scenario.temporary, temporaryPreparationFailure(err))
		})
	}
	require.False(t, temporaryPreparationFailure(nil))
}
