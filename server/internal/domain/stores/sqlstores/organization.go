package sqlstores

import (
	"context"
	"database/sql"
	"fmt"
)

type SQLOrganizationStore struct {
	SQLConnectionManager
}

func NewSQLOrganizationStore(conn *sql.DB) *SQLOrganizationStore {
	return &SQLOrganizationStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLOrganizationStore) ListOrgIDsForSnapshots(ctx context.Context) ([]int64, error) {
	ids, err := s.GetQueries(ctx).ListOrgIDsForSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list org ids for snapshots: %w", err)
	}
	return ids, nil
}
