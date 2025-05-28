package pools

import (
	"context"
	"database/sql"
	"time"

	stratumv1 "github.com/btc-mining/proto-fleet/server/internal/infrastructure/stratum/v1"
	"github.com/rsjethani/secret/v3"
)

type Service struct {
	comm *sql.DB
	cfg  Config
}

func NewService(db *sql.DB, cfg Config) *Service {
	return &Service{
		comm: db,
		cfg:  cfg,
	}
}

// ValidateConnection the connection to a pool server.
// It returns true if the connection is successful, otherwise false.
// We currently only support Stratum V1 connection pools, if you need V2
// support please use a proxy v1->v2 as described https://stratumprotocol.org/docs/#proxies
func (s *Service) ValidateConnection(ctx context.Context, url string, username string, password *secret.Text, timeout *time.Duration) (bool, error) {
	to := s.cfg.timeout
	if timeout != nil {
		to = *timeout
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	ok, err := stratumv1.Authenticate(ctx, url, username, password)

	if err != nil {
		return false, err
	}

	return ok, nil
}
