package pools

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	stratumv1 "github.com/block/proto-fleet/server/internal/infrastructure/stratum/v1"
)

type Service struct{ cfg Config }

func NewService(cfg Config) *Service { return &Service{cfg: cfg} }

func (s *Service) ValidateConnection(ctx context.Context, url string, username string, password *secrets.Text, timeout *time.Duration) (bool, error) {
	to := s.cfg.Timeout
	if timeout != nil {
		to = *timeout
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	if sv2.IsSV2URL(url) {
		key, err := sv2.PoolNoiseKeyFromURL(url)
		if err != nil {
			return false, fleeterror.NewInvalidArgumentErrorf("%v", err)
		}
		ok, err := sv2.HandshakeProbe(ctx, url, key, to)
		if err != nil {
			return false, fleeterror.NewFailedPreconditionErrorf("validate sv2 pool connection: %v", err)
		}
		return ok, nil
	}
	ok, err := stratumv1.Authenticate(ctx, url, username, password)
	if err != nil {
		return false, fleeterror.NewFailedPreconditionErrorf("validate sv1 pool connection: %v", err)
	}
	return ok, nil
}
