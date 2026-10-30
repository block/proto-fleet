package interceptors

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1/poolconfigurationsv1connect"

	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/hosted/v1/hostedv1connect"

	"connectrpc.com/authn"
	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// EffectivePermissionResolver loads the current permissions for an authenticated
// principal. Both Connect and raw HTTP requests use this same resolution path.
type EffectivePermissionResolver interface {
	LoadEffective(ctx context.Context, userID, organizationID int64) (*authz.EffectivePermissions, error)
}

type AccountKeyAuthorizer interface {
	Authorize(ctx context.Context, keyID, issuerID, orgID int64, targetID string) (hostedaccounts.Capabilities, error)
}

type AuthInterceptor struct {
	accountKeys        AccountKeyAuthorizer
	sessionService     *session.Service
	userStore          interfaces.UserStore
	userMgmtStore      interfaces.UserManagementStore
	apiKeyService      *apikey.Service
	permissionResolver EffectivePermissionResolver
	allowList          map[string]struct{}
	sessionOnlyList    map[string]struct{}
	agentAuthList      map[string]struct{}
}

var _ connect.Interceptor = &AuthInterceptor{}

func NewAuthInterceptor(
	sessionService *session.Service,
	userStore interfaces.UserStore,
	userMgmtStore interfaces.UserManagementStore,
	apiKeyService *apikey.Service,
	permissionResolver EffectivePermissionResolver,
	accountKeys AccountKeyAuthorizer,
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
		sessionService:     sessionService,
		userStore:          userStore,
		userMgmtStore:      userMgmtStore,
		apiKeyService:      apiKeyService,
		permissionResolver: permissionResolver,
		accountKeys:        accountKeys,
		allowList:          allowList,
		sessionOnlyList:    sessionOnlyList,
		agentAuthList:      agentAuthList,
	}
}

// loadEffectivePermissions consults the resolver for the authenticated
// user's effective permission set within their org, then stashes the
// result on the context so RequirePermission (and downstream handler
// code) can read it. Both auth branches call this after populating
// session.Info; the result is the same regardless of how the request
// authenticated, which is the point — API keys inherit the user's
// current set on every request rather than carrying a stale snapshot.
//
// Errors from the resolver are fatal for this request: returning the
// error rather than swallowing it preserves the fail-closed default.
// A deactivated user or a user with no live assignments gets a
// non-nil empty EffectivePermissions (the resolver does not error
// on no rows), so this path only errors on DB or wiring failures.
//
// A nil resolver is a constructor-wiring bug (the interceptor was
// built without one). Surface it as Internal rather than panicking
// with a nil dereference — easier to diagnose, and downstream
// RequirePermission's fail-closed default still applies when the
// EffectivePermissions value never makes it onto the context.
func (i *AuthInterceptor) loadEffectivePermissions(ctx context.Context, info *session.Info) (context.Context, error) {
	if i.permissionResolver == nil {
		return ctx, fleeterror.NewInternalError(
			"auth: permission resolver not wired into AuthInterceptor",
		)
	}
	eff, err := i.permissionResolver.LoadEffective(ctx, info.UserID, info.OrganizationID)
	if err != nil {
		return ctx, classifyLookupError(err, "auth: effective permissions lookup failed", info.UserID)
	}
	return middleware.WithEffectivePermissions(authn.SetInfo(ctx, info), eff), nil
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
	ctx, err := captureRequestAccount(ctx, requestHeader)
	if err != nil {
		return ctx, err
	}
	if _, selected := hostedaccounts.RequestAccountID(ctx); !selected && requiresSelectedAccount(procedure) {
		return ctx, fleeterror.NewForbiddenError("account endpoints require an explicitly selected account workspace")
	}
	if _, selected := hostedaccounts.RequestAccountID(ctx); selected && !allowsSelectedAccount(procedure) {
		return ctx, fleeterror.NewForbiddenError("this endpoint is not available in an account workspace")
	}
	// Only explicitly classified account operations accept bounded machine keys.
	// Exemption lists cannot widen account or interactive credential access.
	if strings.HasPrefix(procedure, hostedAccountProcedurePrefix) {
		authed, err := i.authenticateRequest(ctx, requestHeader, !allowsAccountKey(procedure))
		if err != nil {
			return ctx, err
		}
		info, err := session.GetInfo(authed)
		if err != nil {
			return ctx, err
		}
		if info.AuthMethod == session.AuthMethodAPIKey && !info.AccountKey {
			return ctx, fleeterror.NewForbiddenError("organization keys cannot access customer accounts")
		}
		return authed, nil
	}
	if _, ok := i.allowList[procedure]; ok {
		return ctx, nil
	}
	if _, ok := i.agentAuthList[procedure]; ok {
		return ctx, nil
	}
	_, sessionOnly := i.sessionOnlyList[procedure]
	return i.authenticateRequest(ctx, requestHeader, sessionOnly)
}

// AuthenticateRequest authenticates raw HTTP requests using the same credentials,
// identity and live permissions as Connect RPCs. Procedure exemptions never apply.
func (i *AuthInterceptor) AuthenticateRequest(ctx context.Context, requestHeader http.Header) (context.Context, error) {
	ctx, err := captureRequestAccount(ctx, requestHeader)
	if err != nil {
		return ctx, err
	}
	if _, selected := hostedaccounts.RequestAccountID(ctx); selected {
		return ctx, fleeterror.NewForbiddenError("raw HTTP endpoints are not available in an account workspace")
	}
	return i.authenticateRequest(ctx, requestHeader, false)
}

const hostedAccountProcedurePrefix = "/hosted.v1.HostedAccountService/"

// Account workspace operations belong to a selected customer workspace. Host
// account administration deliberately continues to allow omitted selection.
func requiresSelectedAccount(procedure string) bool {
	switch procedure {
	case hostedv1connect.HostedAccountServiceListAccountActivitiesProcedure,
		hostedv1connect.HostedAccountServiceExportAccountActivitiesProcedure,
		hostedv1connect.HostedAccountServiceGetAccountDashboardProcedure,
		hostedv1connect.HostedAccountServiceGetAccountAssetDetailProcedure,
		hostedv1connect.HostedAccountServiceGetAccountMiningSummaryProcedure,
		hostedv1connect.HostedAccountServiceListAccountAssetsProcedure,
		hostedv1connect.HostedAccountServiceAssignAccountAssetsProcedure,
		hostedv1connect.HostedAccountServiceReplaceAccountHashboardProcedure,
		hostedv1connect.HostedAccountServiceCreatePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceRevisePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceGetPoolRevisionProcedure,
		hostedv1connect.HostedAccountServiceListPoolConfigurationsProcedure,
		hostedv1connect.HostedAccountServiceArchivePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceGetIncomingPoolProfileProcedure,
		hostedv1connect.HostedAccountServiceSetIncomingPoolProfileProcedure:
		return true
	default:
		return false
	}
}

func allowsAccountKey(procedure string) bool {
	switch procedure {
	case poolconfigurationsv1connect.PoolConfigurationServiceCreateConfigurationProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceGetConfigurationProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceListConfigurationsProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceDuplicateConfigurationProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceArchiveConfigurationProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceReviewEditProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceConfirmReviewProcedure:
		return true
	case hostedv1connect.HostedAccountServiceCreatePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceRevisePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceGetPoolRevisionProcedure,
		hostedv1connect.HostedAccountServiceListPoolConfigurationsProcedure,
		hostedv1connect.HostedAccountServiceArchivePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceGetIncomingPoolProfileProcedure,
		hostedv1connect.HostedAccountServiceGetAccountProcedure, hostedv1connect.HostedAccountServiceGetSitePolicyProcedure:
		return true
	default:
		return false
	}
}

func allowsSelectedAccount(procedure string) bool {
	// This is intentionally an exact list: new services or methods need an
	// explicit decision before they can be called from a customer workspace.
	// Invitation redemption precedes selection and is excluded.
	switch procedure {
	case poolconfigurationsv1connect.PoolConfigurationServiceCreateConfigurationProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceGetConfigurationProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceListConfigurationsProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceDuplicateConfigurationProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceArchiveConfigurationProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceReviewEditProcedure,
		poolconfigurationsv1connect.PoolConfigurationServiceConfirmReviewProcedure:
		return true
	case hostedv1connect.HostedAccountServiceListAccountActivitiesProcedure,
		hostedv1connect.HostedAccountServiceExportAccountActivitiesProcedure,
		hostedv1connect.HostedAccountServiceGetAccountDashboardProcedure,
		hostedv1connect.HostedAccountServiceGetAccountAssetDetailProcedure,
		hostedv1connect.HostedAccountServiceGetAccountMiningSummaryProcedure,
		hostedv1connect.HostedAccountServiceListAccountAssetsProcedure,
		hostedv1connect.HostedAccountServiceAssignAccountAssetsProcedure,
		hostedv1connect.HostedAccountServiceReplaceAccountHashboardProcedure,
		hostedv1connect.HostedAccountServiceCreatePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceRevisePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceGetPoolRevisionProcedure,
		hostedv1connect.HostedAccountServiceListPoolConfigurationsProcedure,
		hostedv1connect.HostedAccountServiceArchivePoolConfigurationProcedure,
		hostedv1connect.HostedAccountServiceGetIncomingPoolProfileProcedure,
		hostedv1connect.HostedAccountServiceSetIncomingPoolProfileProcedure,
		hostedv1connect.HostedAccountServiceCreateAccountKeyProcedure,
		hostedv1connect.HostedAccountServiceListAccountKeysProcedure,
		hostedv1connect.HostedAccountServiceRevokeAccountKeyProcedure,
		hostedv1connect.HostedAccountServiceListAccountsProcedure,
		hostedv1connect.HostedAccountServiceGetAccountProcedure,
		hostedv1connect.HostedAccountServiceListChildrenProcedure,
		hostedv1connect.HostedAccountServiceCreateAccountProcedure,
		hostedv1connect.HostedAccountServiceConfigureAccountProcedure,
		hostedv1connect.HostedAccountServiceRenameAccountProcedure,
		hostedv1connect.HostedAccountServiceListMembersProcedure,
		hostedv1connect.HostedAccountServiceSetMemberRoleProcedure,
		hostedv1connect.HostedAccountServiceRevokeMemberProcedure,
		hostedv1connect.HostedAccountServiceListAncestorGrantsProcedure,
		hostedv1connect.HostedAccountServiceSetAncestorGrantProcedure,
		hostedv1connect.HostedAccountServiceRevokeAncestorGrantProcedure,
		hostedv1connect.HostedAccountServiceSetSitePolicyProcedure,
		hostedv1connect.HostedAccountServiceGetSitePolicyProcedure,
		hostedv1connect.HostedAccountServiceListInvitationsProcedure,
		hostedv1connect.HostedAccountServiceCreateInvitationProcedure,
		hostedv1connect.HostedAccountServiceRevokeInvitationProcedure,
		authv1connect.AuthServiceGetUserAuditInfoProcedure, authv1connect.AuthServiceUpdatePasswordProcedure,
		authv1connect.AuthServiceLogoutProcedure, authv1connect.AuthServiceVerifyCredentialsProcedure:
		return true
	default:
		return false
	}
}

func captureRequestAccount(ctx context.Context, header http.Header) (context.Context, error) {
	var values []string
	for key, entries := range header {
		if strings.EqualFold(key, hostedaccounts.RequestAccountHeader) {
			// A present empty field is invalid too, rather than a host-mode fallback.
			if len(entries) == 0 {
				return ctx, fleeterror.NewInvalidArgumentError("account header must contain exactly one UUID")
			}
			values = append(values, entries...)
		}
	}
	if len(values) == 0 {
		return ctx, nil
	}
	if len(values) != 1 {
		return ctx, fleeterror.NewInvalidArgumentError("account header must contain exactly one UUID")
	}
	return hostedaccounts.WithRequestAccountID(ctx, values[0])
}

func (i *AuthInterceptor) authenticateRequest(ctx context.Context, requestHeader http.Header, sessionOnly bool) (context.Context, error) {
	hasAuthHeader := requestHeader.Get("Authorization") != ""
	hasSessionCookie := i.hasSessionCookie(requestHeader)

	if hasAuthHeader && hasSessionCookie {
		return ctx, fleeterror.NewUnauthenticatedError("ambiguous auth: provide either Authorization header or session cookie, not both")
	}

	// Session-only procedures reject API key auth before attempting validation.
	if sessionOnly && hasAuthHeader {
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
	accountIssuer, isAccount := apiKeyRecord.AsAccount()
	if isAccount {
		userID = accountIssuer
		selected, present := hostedaccounts.RequestAccountID(ctx)
		if !present {
			return ctx, fleeterror.NewForbiddenError("account keys require an explicit selected account")
		}
		if i.accountKeys == nil {
			return ctx, fleeterror.NewInternalError("account credential authorizer is not configured")
		}
		capabilities, err := i.accountKeys.Authorize(ctx, apiKeyRecord.ID, userID, apiKeyRecord.OrganizationID, selected)
		if err != nil {
			return ctx, err
		}
		ctx, err = hostedaccounts.WithMachineScope(ctx, selected, capabilities)
		if err != nil {
			return ctx, err
		}
	} else {
		if !ok {
			return ctx, fleeterror.NewUnauthenticatedError("invalid api key")
		}
		if _, selected := hostedaccounts.RequestAccountID(ctx); selected {
			return ctx, fleeterror.NewForbiddenError("organization keys cannot access customer accounts")
		}
	}

	user, err := i.userStore.GetUserByID(ctx, userID)
	if err != nil {
		return ctx, classifyLookupError(err, "api key auth: user lookup failed", userID)
	}

	roleName, err := i.userMgmtStore.GetUserRoleName(ctx, userID, apiKeyRecord.OrganizationID)
	if err != nil {
		return ctx, classifyLookupError(err, "api key auth: role lookup failed", userID)
	}

	if isAccount {
		roleName = ""
	}
	info := &session.Info{
		AuthMethod:       session.AuthMethodAPIKey,
		APIKeyID:         apiKeyRecord.KeyID,
		APIKeyDatabaseID: apiKeyRecord.ID,
		APIKeyName:       apiKeyRecord.Name,
		AccountKey:       isAccount,
		UserID:           userID,
		OrganizationID:   apiKeyRecord.OrganizationID,
		ExternalUserID:   user.UserID,
		Username:         user.Username,
		Role:             roleName,
	}

	var authedCtx context.Context
	if isAccount {
		// Host roles belonging to the issuer never enter a customer credential's context.
		authedCtx = authn.SetInfo(ctx, info)
	} else {
		authedCtx, err = i.loadEffectivePermissions(ctx, info)
		if err != nil {
			return ctx, err
		}
	}

	// Only credit the key with a successful use once authentication is
	// fully resolved. Resolver failures (DB error, nil wiring) reject
	// the request, and recording success in that case would let the
	// last_used_at telemetry report rejected requests as successful.
	i.apiKeyService.RecordSuccessfulUse(ctx, apiKeyRecord)

	return authedCtx, nil
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

	return i.loadEffectivePermissions(ctx, info)
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
	if fleeterror.IsCanceledError(err) {
		return fleeterror.NewCanceledError()
	}
	if errors.Is(err, sql.ErrNoRows) {
		slog.Warn(logMsg, "user_id", userID, "error", err)
		return fleeterror.NewUnauthenticatedError("authentication failed")
	}
	slog.Error(logMsg, "user_id", userID, "error", err)
	return fleeterror.NewInternalErrorf("authentication lookup failed")
}
