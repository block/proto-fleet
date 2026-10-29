package hostedaccounts

import (
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/stretchr/testify/require"
)

func TestPoolStorageErrorsRedactValuesAndPreserveTransactionRetry(t *testing.T) {
	cause := &pgconn.PgError{Code: "40001", Message: "sensitive pool JSON"}
	err := poolStorageError(cause)
	require.NotContains(t, err.Error(), "sensitive pool JSON")
	require.True(t, db.IsRetryablePostgresError(err))
	var recovered *pgconn.PgError
	require.True(t, errors.As(err, &recovered))
	require.Same(t, cause, recovered)
}
