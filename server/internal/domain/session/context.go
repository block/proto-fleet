package session

import (
	"context"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// GetInfo extracts session information from the context.
// This replaces token.GetClientAuthJWTClaims for session-based authentication.
func GetInfo(ctx context.Context) (*Info, error) {
	info, ok := authn.GetInfo(ctx).(*Info)
	if !ok {
		return nil, fleeterror.NewInternalError(
			"Context does not have session info. Likely cause is usage of GetInfo from an Endpoint without authentication.",
		)
	}
	return info, nil
}
