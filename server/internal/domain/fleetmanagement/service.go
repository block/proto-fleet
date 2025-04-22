package fleetmanagement

import (
	"context"
	"database/sql"
	"log/slog"
)

type Service struct {
	conn *sql.DB
}

type UpdateDefaultPoolRequest struct {
	URL        string
	Username   string
	Password   string
	WorkerName string
}

func NewService(conn *sql.DB) *Service {
	return &Service{
		conn: conn,
	}
}

func (s Service) UpdateDefaultPool(_ context.Context, r UpdateDefaultPoolRequest) error {
	slog.Debug("updating pool", "url", r.URL, "username", r.Username)

	// TODO actually store default pool in db
	return nil
}
