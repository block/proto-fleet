package interceptors

import (
	"context"

	"connectrpc.com/connect"
)

// AuthTokenContextKey is the key used to store auth tokens in context
type contextKey string

const AuthTokenContextKey contextKey = "auth_token"

// AuthInterceptor handles Bearer token injection
type AuthInterceptor struct {
}

// NewAuthInterceptor creates a new auth interceptor
func NewAuthInterceptor() connect.Interceptor {
	return &AuthInterceptor{}
}

// WrapUnary implements the connect.Interceptor interface
func (i *AuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		// Extract auth token from context
		if token := getAuthTokenFromContext(ctx); token != "" {
			req.Header().Set("Authorization", "Bearer "+token)
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements the connect.Interceptor interface
func (i *AuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		// For streaming clients, we'll handle auth in the context
		// The actual header setting will be done in WrapUnary for individual calls
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements the connect.Interceptor interface
func (i *AuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next // No modification needed for server-side handlers
}

// getAuthTokenFromContext extracts the auth token from context
func getAuthTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(AuthTokenContextKey).(string); ok {
		return token
	}
	return ""
}
