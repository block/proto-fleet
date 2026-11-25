package hostedaccounts_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

// Report preparation locks reporting inputs and then inserts a report whose
// foreign key takes KEY SHARE on the account row. Energy analysis takes the
// library lock and then the inputs lock, so the library lock must admit that
// KEY SHARE or the two transactions can deadlock.
func TestAccountAnalysisLibraryLockAdmitsForeignKeysAndSerializesWriters(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	library, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer library.Rollback()
	_, err = sqlc.New(library).LockAccountAnalysisLibrary(t.Context(), sqlc.LockAccountAnalysisLibraryParams{OrganizationID: f.org, ID: a.ID})
	require.NoError(t, err)

	other, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer other.Rollback()
	_, err = other.ExecContext(t.Context(), `SELECT id FROM hosted_account WHERE organization_id=$1 AND id=$2 FOR KEY SHARE NOWAIT`, f.org, a.ID)
	require.NoError(t, err, "a foreign-key check on the account must not wait for the analysis library lock")
	require.NoError(t, other.Rollback())

	writer, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = writer.ExecContext(t.Context(), `SET LOCAL lock_timeout='100ms'`)
	require.NoError(t, err)
	_, err = sqlc.New(writer).LockAccountAnalysisLibrary(t.Context(), sqlc.LockAccountAnalysisLibraryParams{OrganizationID: f.org, ID: a.ID})
	require.ErrorContains(t, err, "lock timeout", "library writers must still serialize")
}
