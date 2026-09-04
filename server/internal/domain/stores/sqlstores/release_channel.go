package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// SQLReleaseChannelStore gives the rollout domain access to the release
// channel and firmware rollout queries. The domain service works with sqlc
// types directly, so this store only manages the connection.
type SQLReleaseChannelStore struct {
	SQLConnectionManager
}

func NewSQLReleaseChannelStore(conn *sql.DB) *SQLReleaseChannelStore {
	return &SQLReleaseChannelStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

// Queries returns the querier bound to the current context (transaction-aware).
func (s *SQLReleaseChannelStore) Queries(ctx context.Context) sqlc.Querier {
	return s.GetQueries(ctx)
}

// IsUniqueViolation reports whether err is a PostgreSQL unique_violation.
func IsUniqueViolation(err error) bool {
	return isUniqueViolation(err)
}
