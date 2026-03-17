package interceptors

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/session"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"

	"connectrpc.com/authn"
)

type AuthInterceptor struct {
	sessionService *session.Service
	userStore      interfaces.UserStore
	allowList      map[string]struct{}
}

var _ connect.Interceptor = &AuthInterceptor{}

func NewAuthInterceptor(sessionService *session.Service, userStore interfaces.UserStore, allowedProcedures []string) *AuthInterceptor {
	allowList := make(map[string]struct{})
	for _, item := range allowedProcedures {
		allowList[item] = struct{}{}
	}

	return &AuthInterceptor{
		sessionService: sessionService,
		userStore:      userStore,
		allowList:      allowList,
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

	sessionID, err := i.parseSessionCookie(requestHeader)
	if err != nil {
		return ctx, err
	}

	sess, err := i.sessionService.Validate(ctx, sessionID)
	if err != nil {
		return ctx, err
	}

	_, err = i.userStore.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return ctx, fleeterror.NewUnauthenticatedErrorf("User with id %d not found", sess.UserID)
	}

	info := &session.Info{
		SessionID:      sess.SessionID,
		UserID:         sess.UserID,
		OrganizationID: sess.OrganizationID,
	}

	return authn.SetInfo(ctx, info), nil
}

func (i *AuthInterceptor) parseSessionCookie(requestHeader http.Header) (string, error) {
	cookieHeader := requestHeader.Get("Cookie")
	if cookieHeader == "" {
		return "", fleeterror.NewUnauthenticatedError("session cookie required but not provided")
	}

	// Parse cookies from header
	header := http.Header{}
	header.Add("Cookie", cookieHeader)
	request := http.Request{Header: header}

	cookie, err := request.Cookie(i.sessionService.CookieName())
	if err != nil {
		return "", fleeterror.NewUnauthenticatedError("session cookie not found")
	}

	if cookie.Value == "" {
		return "", fleeterror.NewUnauthenticatedError("session cookie is empty")
	}

	return cookie.Value, nil
}
