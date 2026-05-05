package interceptors

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"connectrpc.com/authn"
	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

type AuthInterceptor struct {
	sessionService  *session.Service
	userStore       interfaces.UserStore
	userMgmtStore   interfaces.UserManagementStore
	apiKeyService   *apikey.Service
	allowList       map[string]struct{}
	sessionOnlyList map[string]struct{}
	agentAuthList   map[string]struct{}
}

var _ connect.Interceptor = &AuthInterceptor{}

func NewAuthInterceptor(
	sessionService *session.Service,
	userStore interfaces.UserStore,
	userMgmtStore interfaces.UserManagementStore,
	apiKeyService *apikey.Service,
	allowedProcedures []string,
	sessionOnlyProcedures []string,
	agentAuthProcedures []string,
) *AuthInterceptor {
	allowList := make(map[string]struct{})
	for _, item := range allowedProcedures {
		allowList[item] = struct{}{}
	}

	sessionOnlyList := make(map[string]struct{})
	for _, item := range sessionOnlyProcedures {
		sessionOnlyList[item] = struct{}{}
	}

	agentAuthList := make(map[string]struct{})
	for _, item := range agentAuthProcedures {
		agentAuthList[item] = struct{}{}
	}

	return &AuthInterceptor{
		sessionService:  sessionService,
		userStore:       userStore,
		userMgmtStore:   userMgmtStore,
		apiKeyService:   apiKeyService,
		allowList:       allowList,
		sessionOnlyList: sessionOnlyList,
		agentAuthList:   agentAuthList,
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
	if _, ok := i.agentAuthList[procedure]; ok {
		return ctx, nil
	}

	hasAuthHeader := requestHeader.Get("Authorization") != ""
	hasSessionCookie := i.hasSessionCookie(requestHeader)

	if hasAuthHeader && hasSessionCookie {
		return ctx, fleeterror.NewUnauthenticatedError("ambiguous auth: provide either Authorization header or session cookie, not both")
	}

	// Session-only procedures reject API key auth before attempting validation.
	if _, sessionOnly := i.sessionOnlyList[procedure]; sessionOnly && hasAuthHeader {
		return ctx, fleeterror.NewForbiddenError("this endpoint requires session authentication; API key auth is not permitted")
	}
	if hasAuthHeader {
		return i.authenticateWithApiKey(ctx, requestHeader.Get("Authorization"))
	}
	if hasSessionCookie {
		return i.authenticateWithSession(ctx, requestHeader)
	}
	return ctx, fleeterror.NewUnauthenticatedError("authentication required")
}

func (i *AuthInterceptor) authenticateWithApiKey(ctx context.Context, authHeader string) (context.Context, error) {
	rawKey, ok := parseBearerToken(authHeader)
	if !ok {
		return ctx, fleeterror.NewUnauthenticatedError("invalid Authorization header format, expected: Bearer <key>")
	}

	apiKeyRecord, err := i.apiKeyService.Validate(ctx, rawKey)
	if err != nil {
		return ctx, err
	}

	userID, ok := apiKeyRecord.AsUser()
	if !ok {
		return ctx, fleeterror.NewUnauthenticatedError("invalid api key")
	}

	user, err := i.userStore.GetUserByID(ctx, userID)
	if err != nil {
		return ctx, classifyLookupError(err, "api key auth: user lookup failed", userID)
	}

	roleName, err := i.userMgmtStore.GetUserRoleName(ctx, userID, apiKeyRecord.OrganizationID)
	if err != nil {
		return ctx, classifyLookupError(err, "api key auth: role lookup failed", userID)
	}

	i.apiKeyService.RecordSuccessfulUse(ctx, apiKeyRecord)

	info := &session.Info{
		AuthMethod:     session.AuthMethodAPIKey,
		APIKeyID:       apiKeyRecord.KeyID,
		UserID:         userID,
		OrganizationID: apiKeyRecord.OrganizationID,
		ExternalUserID: user.UserID,
		Username:       user.Username,
		Role:           roleName,
	}

	return authn.SetInfo(ctx, info), nil
}

func parseBearerToken(authHeader string) (string, bool) {
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func (i *AuthInterceptor) authenticateWithSession(ctx context.Context, requestHeader http.Header) (context.Context, error) {
	sessionID, err := i.parseSessionCookie(requestHeader)
	if err != nil {
		return ctx, err
	}

	sess, err := i.sessionService.Validate(ctx, sessionID)
	if err != nil {
		return ctx, err
	}

	user, err := i.userStore.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return ctx, classifyLookupError(err, "session auth: user lookup failed", sess.UserID)
	}

	roleName, err := i.userMgmtStore.GetUserRoleName(ctx, sess.UserID, sess.OrganizationID)
	if err != nil {
		return ctx, classifyLookupError(err, "session auth: role lookup failed", sess.UserID)
	}

	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		SessionID:      sess.SessionID,
		UserID:         sess.UserID,
		OrganizationID: sess.OrganizationID,
		ExternalUserID: user.UserID,
		Username:       user.Username,
		Role:           roleName,
	}

	return authn.SetInfo(ctx, info), nil
}

func (i *AuthInterceptor) hasSessionCookie(requestHeader http.Header) bool {
	cookieHeader := requestHeader.Get("Cookie")
	if cookieHeader == "" {
		return false
	}
	header := http.Header{}
	header.Add("Cookie", cookieHeader)
	request := http.Request{Header: header}
	cookie, err := request.Cookie(i.sessionService.CookieName())
	return err == nil && cookie.Value != ""
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

// classifyLookupError returns Unauthenticated for genuine not-found (user/role deleted)
// and Internal for transient store failures, so callers can distinguish between invalid
// credentials and backend outages.
func classifyLookupError(err error, logMsg string, userID int64) error {
	if errors.Is(err, sql.ErrNoRows) {
		slog.Warn(logMsg, "user_id", userID, "error", err)
		return fleeterror.NewUnauthenticatedError("authentication failed")
	}
	slog.Error(logMsg, "user_id", userID, "error", err)
	return fleeterror.NewInternalErrorf("authentication lookup failed")
}
