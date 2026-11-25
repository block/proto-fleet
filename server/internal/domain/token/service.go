package token

import (
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const minClientSecretKeyLength = 32 // 32 bytes for HS256 security

type Service struct {
	cfg Config
}

// NewService validates and creates a Service instance
func NewService(cfg Config) (*Service, error) {
	if len(cfg.ClientToken.SecretKey) < minClientSecretKeyLength {
		return nil, fleeterror.NewInternalErrorf("secret key must be at least 32 bytes long: len=%d", len(cfg.ClientToken.SecretKey))
	}
	if cfg.ClientToken.ExpirationPeriod == 0 {
		return nil, fleeterror.NewInternalError("expiration period value is required. e.g. '30m'")
	}

	return &Service{cfg: cfg}, nil
}
