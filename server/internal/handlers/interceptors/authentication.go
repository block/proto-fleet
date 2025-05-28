package interceptors

import (
	"connectrpc.com/connect"
	"context"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"net/http"
	"strings"

	"github.com/btc-mining/proto-fleet/server/internal/domain/token"

	"connectrpc.com/authn"
)

type AuthInterceptor struct {
	tokenService *token.Service
	allowList    map[string]struct{}
}

var _ connect.Interceptor = &AuthInterceptor{}

func NewAuthInterceptor(tokenService *token.Service, allowedProcedures []string) *AuthInterceptor {
	allowList := make(map[string]struct{})
	for _, item := range allowedProcedures {
		allowList[item] = struct{}{}
	}

	return &AuthInterceptor{
		tokenService: tokenService,
		allowList:    allowList,
	}
}

func (i *AuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(
		ctx context.Context,
		request connect.AnyRequest,
	) (connect.AnyResponse, error) {
		ctx, err := i.authenticate(ctx, request.Spec().Procedure, request.Header())
		if err != nil {
			return nil, err
		}

		return next(ctx, request)
	}
}

func (i *AuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *AuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := i.authenticate(ctx, conn.Spec().Procedure, conn.RequestHeader())
		if err != nil {
			return err
		}

		return next(ctx, conn)
	}
}

func (i *AuthInterceptor) authenticate(ctx context.Context, procedure string, requestHeader http.Header) (context.Context, error) {
	if _, ok := i.allowList[procedure]; ok {
		return ctx, nil
	}

	bearerToken, err := parseBearerToken(requestHeader)
	if err != nil {
		return ctx, err
	}

	claims, err := i.tokenService.VerifyJWT(bearerToken)
	if err != nil {
		return ctx, err
	}

	return authn.SetInfo(ctx, claims), nil
}

func parseBearerToken(requestHeader http.Header) (string, error) {
	authHeader := requestHeader.Get("Authorization")
	if len(authHeader) == 0 {
		return "", fleeterror.NewUnauthenticatedError("bearer token required but not provided")
	}

	if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return "", fleeterror.NewUnauthenticatedError("Authorization header must contain a bearer token")
	}

	return authHeader[len("bearer "):], nil
}
