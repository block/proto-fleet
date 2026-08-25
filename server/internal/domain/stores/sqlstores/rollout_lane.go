package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// SQLRolloutLaneStore gives the rollout domain access to the rollout-lane
// queries. The domain service works with sqlc types directly, so this store
// only manages the connection.
type SQLRolloutLaneStore struct {
	SQLConnectionManager
}

func NewSQLRolloutLaneStore(conn *sql.DB) *SQLRolloutLaneStore {
	return &SQLRolloutLaneStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

// Queries returns the querier bound to the current context (transaction-aware).
func (s *SQLRolloutLaneStore) Queries(ctx context.Context) sqlc.Querier {
	return s.GetQueries(ctx)
}
