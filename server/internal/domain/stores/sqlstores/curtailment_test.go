package sqlstores

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestMapOrgConfigError(t *testing.T) {
	t.Parallel()

	const orgID = int64(42)
	fkErr := &pgconn.PgError{Code: pgErrCodeForeignKeyViolation, ConstraintName: "fk_curtailment_org_config_org"}
	uniqueErr := &pgconn.PgError{Code: "23505", ConstraintName: "curtailment_org_config_pkey"}
	plainErr := errors.New("connection reset by peer")

	t.Run("nil error returns nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, mapOrgConfigError(nil, orgID))
	})

	t.Run("FK violation surfaces as NotFound", func(t *testing.T) {
		t.Parallel()
		got := mapOrgConfigError(fkErr, orgID)
		require.Error(t, got)
		assert.True(t, fleeterror.IsNotFoundError(got),
			"FK violation must surface as NotFound; got %v", got)
		assert.Contains(t, got.Error(), "42", "error must echo the orgID")
	})

	t.Run("non-FK pg error wraps as Internal", func(t *testing.T) {
		t.Parallel()
		got := mapOrgConfigError(uniqueErr, orgID)
		require.Error(t, got)
		assert.False(t, fleeterror.IsNotFoundError(got),
			"non-FK pg error must not surface as NotFound; got %v", got)
		assert.Contains(t, got.Error(), "failed to get curtailment org config")
	})

	t.Run("plain non-pg error wraps as Internal", func(t *testing.T) {
		t.Parallel()
		got := mapOrgConfigError(plainErr, orgID)
		require.Error(t, got)
		assert.False(t, fleeterror.IsNotFoundError(got),
			"plain error must not surface as NotFound; got %v", got)
		assert.Contains(t, got.Error(), "failed to get curtailment org config")
	})
}
