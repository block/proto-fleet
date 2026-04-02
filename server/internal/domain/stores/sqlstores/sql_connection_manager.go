package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// txContextKey is the key type for storing *sqlc.Queries in context
// We use this to ensure all repo calls share the same transaction when present.
type txContextKey struct{}

type SQLConnectionManager struct {
	conn *db.RetryDB
}

func NewSQLConnectionManager(conn *sql.DB) SQLConnectionManager {
	return SQLConnectionManager{conn: db.NewRetryDB(conn)}
}

// GetQueries retrieves or creates a sqlc.Queries instance based on the context
// If the context contains a transaction, it will use that transaction's queries
// Otherwise, it will create a new queries instance using the connection
func (b *SQLConnectionManager) GetQueries(ctx context.Context) *sqlc.Queries {
	if q := b.GetTxQueries(ctx); q != nil {
		return q
	}

	return sqlc.New(b.conn)
}

func (b *SQLConnectionManager) GetTxQueries(ctx context.Context) *sqlc.Queries {
	if q, ok := ctx.Value(txContextKey{}).(*sqlc.Queries); ok && q != nil {
		return q
	}
	return nil
}
