package client

import (
	"context"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client/interceptors"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

// ContextWithAuth adds an auth token to the context for interceptor use
func ContextWithAuth(ctx context.Context, token *secrets.Text) context.Context {
	return context.WithValue(ctx, interceptors.AuthTokenContextKey, token.Value())
}
