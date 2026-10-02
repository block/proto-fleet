package sqlstores

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type discoveredDeviceErrorQuerier struct {
	sqlc.Querier
	upsertErr error
	fetchErr  error
}

func (q discoveredDeviceErrorQuerier) UpsertDiscoveredDevice(context.Context, sqlc.UpsertDiscoveredDeviceParams) (int64, error) {
	return 1, q.upsertErr
}

func (q discoveredDeviceErrorQuerier) GetDiscoveredDeviceByID(context.Context, sqlc.GetDiscoveredDeviceByIDParams) (sqlc.DiscoveredDevice, error) {
	return sqlc.DiscoveredDevice{}, q.fetchErr
}

func TestSaveDiscoveredDevicePreservesRetryablePostgresError(t *testing.T) {
	for _, stage := range []string{"upsert", "fetch"} {
		t.Run(stage, func(t *testing.T) {
			retryable := &pgconn.PgError{Code: db.PGDeadlockDetected}
			queries := discoveredDeviceErrorQuerier{upsertErr: retryable}
			if stage == "fetch" {
				queries = discoveredDeviceErrorQuerier{fetchErr: retryable}
			}
			ctx := db.WithTxQueries(t.Context(), queries)
			store := NewSQLDiscoveredDeviceStore(nil)

			_, err := store.Save(ctx, discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: "mac:retry", OrgID: 1}, &discoverymodels.DiscoveredDevice{})

			require.ErrorIs(t, err, retryable)
			require.True(t, db.IsRetryablePostgresError(err))
		})
	}
}
