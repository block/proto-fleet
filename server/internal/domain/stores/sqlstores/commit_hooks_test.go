package sqlstores_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestTransactionCallbacksRunOnlyForCommittedAttempt(t *testing.T) {
	if testing.Short() || os.Getenv("DB_PASSWORD") == "" {
		t.Skip("transaction callback integration tests need a database (DB_PASSWORD)")
	}
	for _, rollback := range []bool{false, true} {
		name := "retry then commit"
		if rollback {
			name = "outer rollback"
		}
		t.Run(name, func(t *testing.T) {
			conn := dbtest.GetTestDB(t)
			transactor := sqlstores.NewSQLTransactor(conn)
			type requestKey struct{}
			ctx := context.WithValue(t.Context(), requestKey{}, "request identity")
			var callbacks []int
			attempts := 0
			abort := errors.New("abort outer transaction")
			err := transactor.RunInTx(ctx, func(txCtx context.Context) error {
				attempts++
				attempt := attempts
				orgID, err := transactor.GetQueries(txCtx).CreateOrganization(txCtx, sqlc.CreateOrganizationParams{
					OrgID: "commit-hooks-org", Name: "Commit hooks test",
				})
				if err != nil {
					return err
				}
				require.True(t, db.AfterCommit(txCtx, func() {
					// Finalizers need the original identity but must read through a
					// fresh connection after the transaction has committed.
					postCommitCtx := db.WithoutTransaction(txCtx)
					require.Equal(t, "request identity", postCommitCtx.Value(requestKey{}))
					require.Nil(t, db.GetTxQueries(postCommitCtx))
					require.False(t, db.HasCommitHooks(postCommitCtx))
					saved, err := transactor.GetQueries(postCommitCtx).GetOrganizationByID(postCommitCtx, orgID)
					require.NoError(t, err)
					require.Equal(t, "commit-hooks-org", saved.OrgID)
					callbacks = append(callbacks, attempt)
				}))
				err = transactor.RunInTx(txCtx, func(innerCtx context.Context) error {
					require.True(t, db.AfterCommit(innerCtx, func() { callbacks = append(callbacks, attempt*10) }))
					return nil
				})
				if err != nil {
					return err
				}
				require.Empty(t, callbacks, "an inner transaction must not run callbacks before its outer commit")
				if rollback {
					return abort
				}
				if attempt == 1 {
					return &pgconn.PgError{Code: "40001", Message: "retry the transaction after registering callbacks"}
				}
				return nil
			})
			if rollback {
				require.ErrorIs(t, err, abort)
				require.Equal(t, 1, attempts)
				require.Empty(t, callbacks)
			} else {
				require.NoError(t, err)
				require.Equal(t, 2, attempts)
				require.Equal(t, []int{2, 20}, callbacks, "callbacks from the rolled-back attempt must be discarded")
			}
			var stored int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM organization WHERE org_id = 'commit-hooks-org'`).Scan(&stored))
			if rollback {
				require.Zero(t, stored)
			} else {
				require.Equal(t, 1, stored)
			}
		})
	}
}
