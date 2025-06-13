package testutil

import (
	"context"
	"time"

	"connectrpc.com/authn"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/golang-jwt/jwt/v5"
)

func MockAuthContextForTesting(ctx context.Context, userID, orgID int64) context.Context {
	claims := &token.ClientAuthClaims{
		UserID: userID,
		OrgID:  orgID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	return authn.SetInfo(ctx, claims)
}
