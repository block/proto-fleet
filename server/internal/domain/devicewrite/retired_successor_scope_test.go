package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDeletionSuccessorChecksRetainOrganizationIsolation(t *testing.T) {
	for _, sameOrganization := range []bool{false, true} {
		label := "other organization"
		if sameOrganization {
			label = "same organization"
		}
		t.Run(label, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			owner := database.CreateSuperAdminUser()
			other := database.CreateSuperAdminUser2()
			retired := createDeletionBindingFixture(t, database, owner.OrganizationID)
			q := sqlc.New(database.DB)
			binding, err := q.GetDeviceWithCredentialsAndIPByDeviceIdentifier(t.Context(), retired.ID)
			require.NoError(t, err)
			hash, err := devicewrite.DirectBindingHash(binding)
			require.NoError(t, err)
			keys, err := q.GetDeviceWriteIdentityKeys(t.Context(), retired.DatabaseID)
			require.NoError(t, err)
			count, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{retired.ID}, owner.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, int64(1), count)
			successorOrganization := other.OrganizationID
			if sameOrganization {
				successorOrganization = owner.OrganizationID
			}
			successor := database.CreateDevice(successorOrganization, "proto")
			// Report the same physical identity in each organization. Organization keys
			// remain a hard boundary, while same-org successors supersede the permit.
			_, err = database.DB.ExecContext(t.Context(), `UPDATE device SET serial_number=$2::text,mac_address=substr($2::text,1,12) WHERE id=$1`, successor.DatabaseID, retired.ID)
			require.NoError(t, err)
			current, err := q.IsDeletionUnpairIdentityCurrent(t.Context(), sqlc.IsDeletionUnpairIdentityCurrentParams{OrganizationID: owner.OrganizationID, IdentityKeys: keys})
			require.NoError(t, err)
			require.Equal(t, !sameOrganization, current)
			calls := 0
			err = devicewrite.NewService(database.DB).RunDeletionUnpair(t.Context(), owner.OrganizationID, retired.DatabaseID, hash, func(ctx context.Context) error {
				return devicewrite.Dispatch(ctx, func(context.Context) error { calls++; return nil })
			})
			if sameOrganization {
				require.Error(t, err)
				require.Zero(t, calls)
			} else {
				require.NoError(t, err, "deleted-attempt recheck must also ignore other-org successors")
				require.Equal(t, 1, calls)
			}
		})
	}
}
