package hostedaccounts

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestReportEvidenceCutoffTimeoutIsRetryable(t *testing.T) {
	for name, err := range map[string]error{
		"context deadline":    fmt.Errorf("cutoff: %w", context.DeadlineExceeded),
		"transaction timeout": &pgconn.PgError{Code: "25P04"},
		"query canceled":      &pgconn.PgError{Code: "57014"},
	} {
		t.Run(name, func(t *testing.T) {
			require.True(t, fleeterror.IsUnavailableError(reportEvidenceCutoffError(t.Context(), err)))
		})
	}

	expired, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()
	<-expired.Done()
	err := reportEvidenceCutoffError(expired, context.DeadlineExceeded)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "an exhausted preparation budget keeps its scope guidance")

	other := errors.New("connection reset")
	require.ErrorIs(t, reportEvidenceCutoffError(t.Context(), other), other)
}
