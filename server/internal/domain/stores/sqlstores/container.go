package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

type SQLContainerStore struct{ SQLConnectionManager }

func NewSQLContainerStore(db *sql.DB) *SQLContainerStore {
	return &SQLContainerStore{NewSQLConnectionManager(db)}
}
func (s *SQLContainerStore) ListContainers(ctx context.Context, params sqlc.ListContainersParams) ([]sqlc.ListContainersRow, error) {
	return s.GetQueries(ctx).ListContainers(ctx, params)
}
