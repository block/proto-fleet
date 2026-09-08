package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// SQLReleaseChannelStore hands the rollout domain the release channel and
// firmware rollout queries. The domain works with sqlc types directly, so the
// store only manages the connection.
type SQLReleaseChannelStore struct {
	SQLConnectionManager
}

func NewSQLReleaseChannelStore(conn *sql.DB) *SQLReleaseChannelStore {
	return &SQLReleaseChannelStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

// Queries returns the querier bound to ctx: the transaction's when ctx
// carries one, otherwise the pool's.
func (s *SQLReleaseChannelStore) Queries(ctx context.Context) sqlc.Querier {
	return s.GetQueries(ctx)
}

// IsUniqueViolation reports whether err is a PostgreSQL unique_violation.
func IsUniqueViolation(err error) bool {
	return isUniqueViolation(err)
}
