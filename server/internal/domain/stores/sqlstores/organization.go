package sqlstores

import (
	"context"
	"database/sql"
)

type SQLOrganizationStore struct {
	SQLConnectionManager
}

func NewSQLOrganizationStore(conn *sql.DB) *SQLOrganizationStore {
	return &SQLOrganizationStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLOrganizationStore) ListActiveOrganizationIDs(ctx context.Context) ([]int64, error) {
	orgs, err := s.GetQueries(ctx).ListOrganizations(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(orgs))
	for i := range orgs {
		if orgs[i].DeletedAt.Valid {
			continue
		}
		ids = append(ids, orgs[i].ID)
	}
	return ids, nil
}
