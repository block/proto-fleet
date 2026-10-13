package minerproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/authn"
	"golang.org/x/sync/semaphore"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

const (
	minerLoginPath        = "/api/v1/auth/login"
	proxyClientTimeout    = 30 * time.Second
	maxProxyBodyBytes     = 64 << 20
	maxLoginResponseBytes = 64 << 10
	defaultProtoURLScheme = "http"
	// Cached tokens are a latency optimization, not a source of truth: a 401
	// from the miner already forces a fresh login. The TTL caps how long a
	// stale entry lingers, and the size bound stops the cache from growing
	// without limit as devices churn over a long-running server's lifetime.
	tokenCacheTTL   = 30 * time.Minute
	maxCachedTokens = 8192
)

type cachedToken struct {
	token     string
	expiresAt time.Time
}

type errorResponse struct {
	Error string `json:"error"`
}

type loginRequest struct {
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
}

type Config struct {
	MaxConcurrentRequests int `help:"Maximum active miner proxy requests; zero uses sixteen, capped below the shared dispatch budget" default:"16" env:"MAX_CONCURRENT_REQUESTS"`
}

func (c Config) Validate() error {
	if c.MaxConcurrentRequests < 0 {
		return fmt.Errorf("MINER_PROXY_MAX_CONCURRENT_REQUESTS must be nonnegative")
	}
	return nil
}

type Handler struct {
	conn               *sql.DB
	config             Config
	proxySlotsOnce     sync.Once
	proxySlots         *semaphore.Weighted
	proxySlotsErr      error
	writeService       *devicewrite.Service
	queries            sqlc.Querier
	sessionService     *session.Service
	userStore          stores.UserStore
	permissionResolver *authz.PermissionResolver
	encryptService     *encrypt.Service
	httpClient         *http.Client
	httpsClient        *http.Client

	tokenMu sync.Mutex
	tokens  map[string]cachedToken
}

type proxyTarget struct {
	dispatch         func(context.Context, bool, func(context.Context) error) error
	binding          sqlc.GetDirectProtoMinerProxyTargetRow
	deviceIdentifier string
	baseURL          string
	siteID           *int64
	passwordEnc      sql.NullString
}

// hasCredentials reports whether the target has a usable stored password. An
// encrypted password that is present-but-empty counts as no credentials, so
// the proxy forwards unauthenticated rather than attempting a doomed login.
func (t proxyTarget) hasCredentials() bool {
	return t.passwordEnc.Valid && t.passwordEnc.String != ""
}

// cacheKey scopes a cached token to both the device and its resolved endpoint.
// If a discovery update changes the miner's address, the key changes too, so a
// token minted for the old endpoint is never replayed to (or credentials
// re-sent under the assumption of) a different address.
func (t proxyTarget) cacheKey() string {
	return t.deviceIdentifier + "\x00" + t.baseURL
}

func NewHandler(
	conn *sql.DB,
	sessionService *session.Service,
	userStore stores.UserStore,
	permissionResolver *authz.PermissionResolver,
	encryptService *encrypt.Service,
	config Config,
) http.Handler {
	return &Handler{
		conn:               conn,
		config:             config,
		writeService:       devicewrite.NewService(conn),
		queries:            db.NewFailoverResettingQuerier(db.NewRetryDB(conn)),
		sessionService:     sessionService,
		userStore:          userStore,
		permissionResolver: permissionResolver,
		encryptService:     encryptService,
		httpClient:         newProxyHTTPClient(false),
		httpsClient:        newProxyHTTPClient(true),
		tokens:             make(map[string]cachedToken),
	}
}

func newProxyHTTPClient(skipVerify bool) *http.Client {
	// A reused connection can be retried internally by net/http without a new
	// binding or ownership check. One fresh HTTP/1 connection per request keeps
	// every physical send inside its explicit dispatch admission.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		DisableKeepAlives:     true,
		Protocols:             protocols,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: proxyClientTimeout,
	}
	if skipVerify {
		transport.TLSClientConfig = &tls.Config{
			// Proto rigs commonly present self-signed certs. This matches the
			// Proto plugin's transport behavior.
			InsecureSkipVerify: true, // #nosec G402 -- intentional for Proto rig HTTPS
			MinVersion:         tls.VersionTLS12,
		}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   proxyClientTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (h *Handler) clientFor(baseURL string) *http.Client {
	if strings.HasPrefix(baseURL, "https://") {
		return h.httpsClient
	}
	return h.httpClient
}

// Proxy requests use a smaller, non-waiting budget before durable admission.
// Keep one shared dispatch slot available for ordinary commands and curtailment.
func (h *Handler) acquireProxySlot() (func(), error) {
	h.proxySlotsOnce.Do(func() {
		capacity, err := db.DispatchCapacity(h.conn)
		if err != nil {
			h.proxySlotsErr = err
			return
		}
		limit := h.config.MaxConcurrentRequests
		if limit == 0 {
			limit = 16
		}
		if limit < 0 {
			h.proxySlotsErr = h.config.Validate()
			return
		}
		limit = min(limit, capacity-1)
		h.proxySlots = semaphore.NewWeighted(int64(max(limit, 0)))
	})
	if h.proxySlotsErr != nil {
		return nil, h.proxySlotsErr
	}
	if !h.proxySlots.TryAcquire(1) {
		return nil, fleeterror.NewUnavailableErrorf("miner proxy is busy; retry later")
	}
	return func() { h.proxySlots.Release(1) }, nil
}

func (h *Handler) responseTimeout(target proxyTarget) time.Duration {
	timeout := h.clientFor(target.baseURL).Timeout
	if timeout <= 0 {
		return proxyClientTimeout
	}
	return timeout
}

// Operation cancellation must not expire the downstream error response. The
// browser request's own cancellation still interrupts delivery immediately.
func writeProxyError(w http.ResponseWriter, r *http.Request, err error, timeout time.Duration) {
	slog.Warn("miner proxy request failed", "error", err)
	finish, boundErr := boundProxyResponse(r.Context(), w, timeout)
	if boundErr == nil {
		defer finish()
	}
	writeError(w, httpStatusForError(err), clientMessageForError(err))
	if boundErr == nil {
		if flushErr := http.NewResponseController(w).Flush(); flushErr != nil {
			slog.Warn("miner proxy error response did not flush", "error", flushErr)
		}
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for header := range r.Header {
		if strings.EqualFold(header, hostedaccounts.RequestAccountHeader) {
			writeError(w, http.StatusForbidden, "miner proxy is not available in an account workspace")
			return
		}
	}
	ctx, info, err := h.authenticate(r)
	if err != nil {
		slog.Warn("miner proxy authentication failed", "error", err)
		writeError(w, httpStatusForError(err), "authentication required")
		return
	}

	deviceIdentifier := r.PathValue("deviceIdentifier")
	apiVersion := r.PathValue("apiVersion")
	rest := r.PathValue("rest")
	if deviceIdentifier == "" || apiVersion == "" || rest == "" {
		writeError(w, http.StatusNotFound, "miner API route not found")
		return
	}

	// The only legitimate caller is the embedded ProtoOS client via fetch/XHR.
	// Reject browser navigations and framed loads so miner-controlled bytes are
	// never rendered as a document under the Fleet origin (defense in depth
	// alongside the nosniff/sandbox headers set on the response).
	if isRenderingNavigation(r) {
		writeError(w, http.StatusForbidden, "miner API is not directly navigable")
		return
	}

	// State-changing requests must originate from the same-origin embedded
	// client. The proxy authenticates with the Fleet session cookie, which a
	// same-site sibling origin (shared parent domain) can still send, so reject
	// cross-origin browser writes before acting on that cookie auth (CSRF).
	if isDisallowedCrossOriginWrite(r) {
		writeError(w, http.StatusForbidden, "cross-origin miner mutation rejected")
		return
	}

	proxyPath, ok := proxyPathFor(apiVersion, r.Method, rest)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid miner API path")
		return
	}
	if isUnproxyableEndpoint(r.Method, proxyPath) {
		writeError(w, http.StatusForbidden, "this miner endpoint is managed by Fleet and cannot be changed from the embedded view")
		return
	}

	target, err := h.resolveTarget(ctx, deviceIdentifier, info.OrganizationID)
	if err != nil {
		slog.Warn("miner proxy request rejected", "error", err)
		writeError(w, httpStatusForError(err), clientMessageForError(err))
		return
	}

	for _, permission := range permissionsFor(r.Method, proxyPath) {
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: target.siteID}); err != nil {
			writeError(w, httpStatusForError(err), clientMessageForError(err))
			return
		}
	}

	release, err := h.acquireProxySlot()
	if err != nil {
		slog.Warn("miner proxy capacity unavailable", "error", err)
		writeError(w, http.StatusServiceUnavailable, "miner proxy is busy; retry later")
		return
	}
	defer release()
	timeout := h.responseTimeout(target)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := readProxyBody(w, r)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}

	responseStarted := false
	call := func(ctx context.Context) error {
		return h.forwardAndCopy(ctx, w, r, target, proxyPath, body, &responseStarted)
	}
	if proxyMutatesMiner(r.Method, proxyPath) {
		// Reuse the typed command completion contract for known synchronous controls.
		err = h.writeService.WithHostAttempt(ctx, target.binding.OrgID, target.binding.ID, target.binding.WriteEpoch, proxyCommandKind(r.Method, proxyPath), call)
	} else {
		err = h.writeService.WithHostObservation(ctx, target.binding.OrgID, target.binding.ID, target.binding.WriteEpoch, call)
	}
	if err != nil && !responseStarted {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = ctx.Err()
		}
		writeProxyError(w, r, err, timeout)
	}
}

func (h *Handler) forwardAndCopy(ctx context.Context, w http.ResponseWriter, r *http.Request, target proxyTarget, proxyPath string, body []byte, responseStarted *bool) error {
	return h.writeService.WithBindingTransaction(ctx, func(ctx context.Context, _ sqlc.Querier) (resultErr error) {
		// Verify deadline support before miner I/O, but arm it only when
		// delivering a response. An expired HTTP/2 write deadline closes the
		// stream permanently and cannot be reset to deliver an upstream 502.
		writeTimeout := h.responseTimeout(target)
		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
			return fleeterror.NewUnavailableErrorf("miner response write deadline could not be enforced")
		}
		resp, err := h.forward(ctx, r, target, proxyPath, body, false)
		if err != nil {
			return err
		}
		defer func() {
			if resp == nil {
				return
			}
			if err := resp.Body.Close(); err != nil && resultErr == nil {
				resultErr = fleeterror.NewUnavailableErrorf("miner response could not be closed")
			}
		}()
		if resp.StatusCode == http.StatusUnauthorized && target.hasCredentials() && requiresMinerAuthentication(proxyPath) {
			if _, err := io.Copy(io.Discard, resp.Body); err != nil {
				return fleeterror.NewUnavailableErrorf("miner response could not be consumed")
			}
			h.clearToken(target.cacheKey())
			if err := resp.Body.Close(); err != nil {
				return fleeterror.NewUnavailableErrorf("miner response could not be closed")
			}
			resp, err = h.forward(ctx, r, target, proxyPath, body, true)
			if err != nil {
				return err
			}
		}
		if resp.StatusCode == http.StatusAccepted && proxyMutatesMiner(r.Method, proxyPath) {
			// An explicit asynchronous acknowledgement overrides the endpoint's
			// ordinary synchronous contract, even if response copying fails.
			defer func() {
				if resultErr == nil {
					resultErr = fleeterror.NewUnavailableErrorf("miner operation is still pending")
				}
				resultErr = devicewrite.UncertainDelivery(resultErr)
			}()
		}
		// SQL waits, login and retries consume the operation budget, rather than
		// the independent downstream write budget.
		finishResponse, err := boundProxyResponse(r.Context(), w, writeTimeout)
		if err != nil {
			return err
		}
		defer finishResponse()
		if err := r.Context().Err(); err != nil {
			return fmt.Errorf("miner response delivery canceled: %w", err)
		}
		copyResponseHeaders(w.Header(), resp.Header)
		setResponseHardeningHeaders(w.Header())
		*responseStarted = true
		w.WriteHeader(resp.StatusCode)
		if r.Method != http.MethodHead {
			if _, err := io.Copy(w, resp.Body); err != nil {
				return fleeterror.NewUnavailableErrorf("miner response stream did not complete")
			}
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			return fleeterror.NewUnavailableErrorf("miner response could not be flushed")
		}
		return nil
	})
}

func boundProxyResponse(ctx context.Context, w http.ResponseWriter, timeout time.Duration) (func(), error) {
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fleeterror.NewUnavailableErrorf("miner response write deadline could not be enforced")
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(interrupted)
		_ = controller.SetWriteDeadline(time.Now())
	})
	return func() {
		// A stopped callback may already be setting the deadline. Join it before
		// returning the writer. The HTTP server clears the deadline after its
		// final buffered write, so retain it through that finalization.
		if !stop() {
			<-interrupted
		}
	}, nil
}

func (h *Handler) authenticate(r *http.Request) (context.Context, *session.Info, error) {
	cookie, err := r.Cookie(h.sessionService.CookieName())
	if err != nil || cookie.Value == "" {
		return r.Context(), nil, fleeterror.NewUnauthenticatedError("session cookie required")
	}

	sess, err := h.sessionService.Validate(r.Context(), cookie.Value)
	if err != nil {
		return r.Context(), nil, err
	}

	user, err := h.userStore.GetUserByID(r.Context(), sess.UserID)
	if err != nil {
		return r.Context(), nil, fleeterror.NewUnauthenticatedErrorf("user with id %d not found", sess.UserID)
	}

	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		SessionID:      sess.SessionID,
		UserID:         sess.UserID,
		OrganizationID: sess.OrganizationID,
		ExternalUserID: user.UserID,
		Username:       user.Username,
	}

	effectivePermissions, err := h.permissionResolver.LoadEffective(r.Context(), info.UserID, info.OrganizationID)
	if err != nil {
		return r.Context(), nil, fleeterror.NewInternalErrorf("authz: effective permissions lookup failed: %v", err)
	}

	ctx := middleware.WithEffectivePermissions(authn.SetInfo(r.Context(), info), effectivePermissions)
	return ctx, info, nil
}

func (h *Handler) resolveTarget(ctx context.Context, deviceIdentifier string, orgID int64) (proxyTarget, error) {
	row, err := h.queries.GetDirectProtoMinerProxyTarget(ctx, sqlc.GetDirectProtoMinerProxyTargetParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return proxyTarget{}, fleeterror.NewNotFoundError("miner not found or cannot be proxied")
		}
		return proxyTarget{}, fleeterror.NewInternalErrorf("failed to resolve miner proxy target: %v", err)
	}

	scheme := strings.ToLower(row.UrlScheme)
	if scheme == "" {
		scheme = defaultProtoURLScheme
	}
	if scheme != "http" && scheme != "https" {
		return proxyTarget{}, fleeterror.NewFailedPreconditionErrorf("miner URL scheme %q cannot be proxied", row.UrlScheme)
	}

	// The proxy dials this address and POSTs decrypted miner credentials to it,
	// so the discovery-sourced address must be a routable miner IP — never a
	// loopback/link-local/metadata/multicast target that would turn fleet-api
	// into an SSRF primitive or leak credentials to an unexpected host.
	addr, err := parseRoutableMinerAddr(row.IpAddress)
	if err != nil {
		return proxyTarget{}, err
	}

	host, err := minerHost(addr, row.Port)
	if err != nil {
		return proxyTarget{}, err
	}

	base := url.URL{Scheme: scheme, Host: host}
	var siteID *int64
	if row.SiteID.Valid {
		site := row.SiteID.Int64
		siteID = &site
	}

	return proxyTarget{
		binding: row,
		dispatch: func(ctx context.Context, mutation bool, call func(context.Context) error) error {
			validate := func(q sqlc.Querier) error { return h.revalidateTarget(ctx, q, row) }
			if mutation {
				return h.writeService.WithBindingDispatch(ctx, row.ID, validate, call)
			}
			return h.writeService.WithBindingObservation(ctx, row.ID, validate, call)
		},
		deviceIdentifier: row.DeviceIdentifier,
		baseURL:          base.String(),
		siteID:           siteID,
		passwordEnc:      row.PasswordEnc,
	}, nil
}

func (h *Handler) forward(
	ctx context.Context,
	source *http.Request,
	target proxyTarget,
	proxyPath string,
	body []byte,
	forceLogin bool,
) (*http.Response, error) {
	var token string
	if requiresMinerAuthentication(proxyPath) {
		var err error
		token, err = h.tokenFor(ctx, target, forceLogin)
		if err != nil {
			return nil, err
		}
	}

	targetURL := target.baseURL + proxyPath
	if source.URL.RawQuery != "" {
		targetURL += "?" + source.URL.RawQuery
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, source.Method, targetURL, bodyReader)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("failed to create miner proxy request: %v", err)
	}
	copyRequestHeaders(req.Header, source.Header)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Del("Authorization")
	}

	var resp *http.Response
	var callErr error
	err = h.dispatchTarget(ctx, target, proxyMutatesMiner(source.Method, proxyPath), func(ctx context.Context) error {
		//nolint:bodyclose // forwardAndCopy closes the returned response within the admitted attempt.
		resp, callErr = h.clientFor(target.baseURL).Do(req.WithContext(ctx))
		if callErr != nil {
			return fmt.Errorf("miner proxy transport: %w", callErr)
		}
		return nil
	})
	if err != nil {
		if callErr != nil {
			// Dispatch records any explicit delivery uncertainty before the
			// transport error is sanitized for the HTTP client.
			return nil, fleeterror.NewUnavailableErrorf("failed to reach miner")
		}
		return nil, err
	}
	return resp, nil
}

func (h *Handler) tokenFor(ctx context.Context, target proxyTarget, forceLogin bool) (string, error) {
	if !target.hasCredentials() {
		return "", nil
	}

	if !forceLogin {
		if token, ok := h.lookupToken(target.cacheKey()); ok {
			return token, nil
		}
	}

	// Deliberately no device-identity preflight before sending credentials:
	// miner subnets are treated as trusted, and login itself fails closed
	// against a host that does not hold the stored password (resolveTarget has
	// already rejected non-routable addresses). An earlier serial/MAC preflight
	// was removed as disproportionate for this threat model; revisit if the
	// trust assumption changes.
	passwordBytes, err := h.encryptService.Decrypt(target.passwordEnc.String)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to decrypt miner credentials: %v", err)
	}

	token, err := h.loginTarget(ctx, target, string(passwordBytes))
	if err != nil {
		return "", err
	}

	h.storeToken(target.cacheKey(), token)
	return token, nil
}

func (h *Handler) lookupToken(key string) (string, bool) {
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	entry, ok := h.tokens[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(h.tokens, key)
		return "", false
	}
	return entry.token, true
}

func (h *Handler) storeToken(key string, token string) {
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	if _, exists := h.tokens[key]; !exists && len(h.tokens) >= maxCachedTokens {
		h.evictForRoomLocked()
	}
	h.tokens[key] = cachedToken{token: token, expiresAt: time.Now().Add(tokenCacheTTL)}
}

// evictForRoomLocked drops expired entries first; if the cache is still at
// capacity it removes a single arbitrary entry to make room. Re-login on the
// next request restores any token evicted prematurely. Callers hold tokenMu.
func (h *Handler) evictForRoomLocked() {
	now := time.Now()
	for id, entry := range h.tokens {
		if now.After(entry.expiresAt) {
			delete(h.tokens, id)
		}
	}
	if len(h.tokens) < maxCachedTokens {
		return
	}
	for id := range h.tokens {
		delete(h.tokens, id)
		return
	}
}

func (h *Handler) clearToken(key string) {
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	delete(h.tokens, key)
}

func (h *Handler) loginTarget(ctx context.Context, target proxyTarget, password string) (string, error) {
	var token string
	err := h.dispatchTarget(ctx, target, false, func(ctx context.Context) error {
		var err error
		token, err = h.login(ctx, target.baseURL, password)
		return err
	})
	return token, err
}

func (h *Handler) revalidateTarget(ctx context.Context, q sqlc.Querier, binding sqlc.GetDirectProtoMinerProxyTargetRow) error {
	fresh, err := q.GetDirectProtoMinerProxyTarget(ctx, sqlc.GetDirectProtoMinerProxyTargetParams{DeviceIdentifier: binding.DeviceIdentifier, OrgID: binding.OrgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewFailedPreconditionError("miner proxy connection changed")
		}
		return fleeterror.NewInternalError("miner proxy connection could not be revalidated")
	}
	if fresh != binding {
		return fleeterror.NewFailedPreconditionError("miner proxy connection changed")
	}
	return nil
}

func (h *Handler) dispatchTarget(ctx context.Context, target proxyTarget, mutation bool, call func(context.Context) error) error {
	if target.dispatch == nil {
		return fleeterror.NewFailedPreconditionError("miner proxy target admission missing")
	}
	return target.dispatch(ctx, mutation, call)
}

// The authorization contract already distinguishes observations, including
// POST timeseries and downloadable logs. Login only establishes authentication;
// all other operations retain conservative physical-mutation evidence.
func proxyMutatesMiner(method, proxyPath string) bool {
	if method == http.MethodPost && proxyPath == minerLoginPath {
		return false
	}
	for _, permission := range permissionsFor(method, proxyPath) {
		if permission != authz.PermMinerRead && permission != authz.PermMinerDownloadLogs {
			return true
		}
	}
	return false
}

func (h *Handler) login(ctx context.Context, baseURL string, password string) (token string, resultErr error) {
	body, err := json.Marshal(loginRequest{Password: password})
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create miner login request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+minerLoginPath, bytes.NewReader(body))
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create miner login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.clientFor(baseURL).Do(req)
	if err != nil {
		return "", fleeterror.NewUnavailableErrorf("failed to authenticate with miner")
	}
	defer func() {
		if err := resp.Body.Close(); err != nil && resultErr == nil {
			token = ""
			resultErr = fleeterror.NewUnavailableErrorf("miner login response could not be closed")
		}
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return "", fleeterror.NewUnavailableErrorf("miner login response could not be consumed")
		}
		return "", fleeterror.NewUnauthenticatedError("stored miner credentials were rejected")
	}
	if resp.StatusCode != http.StatusOK {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return "", fleeterror.NewUnavailableErrorf("miner login response could not be consumed")
		}
		return "", fleeterror.NewFailedPreconditionErrorf("miner login failed with status %d", resp.StatusCode)
	}

	// Read the complete bounded body before accepting a token. Decoding a valid
	// JSON prefix alone can hide a later response read failure.
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxLoginResponseBytes+1))
	if err != nil || len(responseBody) > maxLoginResponseBytes {
		return "", fleeterror.NewUnavailableErrorf("miner login response did not complete")
	}
	var tokens loginResponse
	if err := json.Unmarshal(responseBody, &tokens); err != nil {
		return "", fleeterror.NewUnavailableErrorf("miner login response could not be decoded")
	}
	if tokens.AccessToken == "" {
		return "", fleeterror.NewInternalError("miner login response did not include an access token")
	}
	return tokens.AccessToken, nil
}

func readProxyBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	defer r.Body.Close()

	reader := http.MaxBytesReader(w, r.Body, maxProxyBodyBytes)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("proxied request body exceeds %d bytes", maxProxyBodyBytes)
	}
	if len(body) == 0 {
		return nil, nil
	}
	return body, nil
}

// proxyPathFor builds an allowlisted upstream miner path from the
// {apiVersion} and {rest...} wildcards and reports whether it is canonical.
// MDK v1 retains the existing generic proxy surface. MDK v2 exposes only
// explicitly listed endpoints used by the embedded CT1 settings and module view.
//
// The rest wildcard preserves percent-decoded
// dot-segments and slashes (Go's ServeMux collapses only *unencoded* ".."), and
// the handler both authorizes on and forwards this exact string — so a
// non-canonical path (".." traversal, encoded "%2f"/"%2e%2e", empty segments, a
// trailing slash) would let a caller classify one endpoint while the miner
// routes another. Anything path.Clean would rewrite is rejected, which keeps
// the classified path byte-identical to the forwarded path.
//
// path.Clean does not treat "?" or "#" as special, but they are URL delimiters:
// a decoded "?"/"#" survives path.Clean yet becomes a query/fragment when the
// path is built into the upstream URL, so "/api/v1/auth/password?x" would be
// authorized as a non-blocked path while the miner routes it to the password
// endpoint. Reject those bytes too — a real query string arrives via the
// request's own RawQuery, never through the path wildcard.
func proxyPathFor(apiVersion string, method string, rest string) (string, bool) {
	switch apiVersion {
	case "v1":
		// MDK v1 retains the existing generic proxy surface (any method/rest).
	case "v2":
		// MDK v2 accepts only its explicitly allowlisted endpoints
		// (keyed by method + rest) are accepted.
		if _, ok := lookupV2Endpoint(method, rest); !ok {
			return "", false
		}
	default:
		return "", false
	}

	proxyPath := "/api/" + apiVersion + "/" + rest
	// Reject query/fragment delimiters and any residual percent-encoding. The
	// mux already decoded the wildcard once, so a leftover "%" means a
	// double-encoded delimiter (e.g. "%252F" -> "%2F") that path.Clean won't
	// normalize but the miner may still decode to "/" — letting a crafted path
	// reach a protected endpoint under the wrong permission. No real ProtoOS
	// endpoint segment contains "%", "?", or "#".
	if strings.ContainsAny(proxyPath, "?#%") {
		return proxyPath, false
	}
	return proxyPath, path.Clean(proxyPath) == proxyPath
}

// v2Endpoint describes an allowed method and path. A single-segment wildcard
// is used only for hashboard/module identifiers, never for control names.
// Fleet authorization is required independently of miner bearer authentication.
type v2Endpoint struct {
	method       string
	rest         string
	requiresAuth bool
	// nil keeps unclassified operations conservative; zero is StartMining.
	commandKind *commandtype.Type
}

// proxyPath returns the canonical upstream path this endpoint forwards to.
func (e v2Endpoint) proxyPath() string {
	return "/api/v2/" + e.rest
}

// v2AllowlistedEndpoints is the single source of truth for which MDK v2
// endpoints the proxy accepts, their miner authentication, and any qualified
// synchronous command contract. Unclassified mutations retain recovery evidence.
// CT1 endpoints do not use MDK v1 login; Fleet still authenticates the caller,
// validates the miner target, and checks the operation permissions.
var v2AllowlistedEndpoints = []v2Endpoint{
	{method: http.MethodGet, rest: "system/info", requiresAuth: false},
	{method: http.MethodGet, rest: "system/status", requiresAuth: false},
	{method: http.MethodGet, rest: "container/map", requiresAuth: false},
	{method: http.MethodGet, rest: "mining/status", requiresAuth: false},
	{method: http.MethodGet, rest: "mining/target-power", requiresAuth: false},
	{method: http.MethodGet, rest: "telemetry/container", requiresAuth: false},
	{method: http.MethodGet, rest: "telemetry/timeseries", requiresAuth: false},
	{method: http.MethodGet, rest: "telemetry/shares/history", requiresAuth: false},
	{method: http.MethodGet, rest: "pools", requiresAuth: false},
	{method: http.MethodGet, rest: "pools/diagnostics", requiresAuth: false},
	{method: http.MethodGet, rest: "power/container/controls", requiresAuth: false},
	{method: http.MethodGet, rest: "hashboards/*/asics", requiresAuth: false},
	{method: http.MethodPut, rest: "mining/target-power", requiresAuth: false, commandKind: new(commandtype.SetPowerTarget)},
	{method: http.MethodPost, rest: "mining/start", requiresAuth: false, commandKind: new(commandtype.StartMining)},
	{method: http.MethodPost, rest: "mining/stop", requiresAuth: false, commandKind: new(commandtype.StopMining)},
	{method: http.MethodPost, rest: "mining/scoped/start", requiresAuth: false, commandKind: new(commandtype.SetTankMining)},
	{method: http.MethodPost, rest: "mining/scoped/stop", requiresAuth: false, commandKind: new(commandtype.SetTankMining)},
	{method: http.MethodPost, rest: "hashboards/identify-led", requiresAuth: false},
	{method: http.MethodPost, rest: "mining/isolation", requiresAuth: false},
	{method: http.MethodPost, rest: "modules/*/run-in", requiresAuth: false},
	{method: http.MethodPost, rest: "power/container/controls/coolant_pump", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/controls/dry_cooler", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/controls/dry_cooler_auto", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/controls/tank_led_light", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/controls/air_conditioner_1", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/controls/air_conditioner_2", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/setpoints/coolant_pump_speed_hz", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/setpoints/dry_cooler_auto_temp_c", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/setpoints/dry_cooler_manual_speed_percent", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/setpoints/air_conditioner_1_temp_c", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
	{method: http.MethodPost, rest: "power/container/setpoints/air_conditioner_2_temp_c", requiresAuth: false, commandKind: new(commandtype.SetContainerControl)},
}

// lookupV2Endpoint returns the allowlisted v2 endpoint for a {method, rest}, if
// any.
func lookupV2Endpoint(method string, rest string) (v2Endpoint, bool) {
	for _, e := range v2AllowlistedEndpoints {
		if matches, _ := path.Match(e.rest, rest); e.method == method && matches {
			return e, true
		}
	}
	return v2Endpoint{}, false
}

// proxyCommandKind qualifies only exact allowlisted v2 method/path pairs.
// Generic v1 writes and asynchronous or unclassified v2 endpoints default to
// unresolved; adding an endpoint does not opt it into synchronous completion.
func proxyCommandKind(method, proxyPath string) commandtype.Type {
	if rest, ok := strings.CutPrefix(proxyPath, "/api/v2/"); ok {
		if endpoint, ok := lookupV2Endpoint(method, rest); ok && endpoint.commandKind != nil {
			return *endpoint.commandKind
		}
	}
	return commandtype.Type(-1)
}

// requiresMinerAuthentication keeps the existing MDK v1 bearer-token behavior
// while honoring the per-endpoint auth flag for allowlisted MDK v2 endpoints.
// CT1 endpoints must not attempt the v1 login endpoint or send Fleet's stored
// miner password. Unknown paths retain the conservative authentication default.
func requiresMinerAuthentication(proxyPath string) bool {
	for _, e := range v2AllowlistedEndpoints {
		if matches, _ := path.Match(e.proxyPath(), proxyPath); matches {
			return e.requiresAuth
		}
	}
	return true
}

// isUnproxyableEndpoint rejects endpoints that must not be reverse-proxied
// because forwarding them blindly would either desync Fleet's own state or
// bypass a dedicated Fleet flow that carries controls the generic proxy can't:
//
//   - Password changes (/api/v1/auth/password, /api/v1/auth/change-password)
//     go through the Fleet UpdateMinerPassword command, which re-encrypts and
//     persists the new password, clears cached tokens, and reconciles pairing
//     state. Forwarded blindly they would leave Fleet on the stale password.
//   - Pool mutations go through the Fleet UpdateMiningPools command, which
//     requires step-up re-auth, runs SV2 preflight + worker-name composition,
//     and records dispatch/audit state. The proxy enforces none of that, so a
//     direct pool write would be an unaudited path to change payout addresses.
//
// Pool reads stay proxyable; only mutating methods are blocked there.
func isUnproxyableEndpoint(method string, proxyPath string) bool {
	switch proxyPath {
	case "/api/v1/auth/password", "/api/v1/auth/change-password":
		return true
	}
	if isWriteMethod(method) && (strings.HasPrefix(proxyPath, "/api/v1/pools") || strings.HasPrefix(proxyPath, "/api/v2/pools")) {
		return true
	}
	return false
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// permissionsFor returns every permission a proxied request must hold. Most
// endpoints need a single permission, but some need more than one — firmware
// installs can reboot the miner, so they require both firmware-update and
// reboot, matching the direct Fleet command path (command/handler.go).
func permissionsFor(method string, proxyPath string) []string {
	// Reads: log retrieval gets its own permission; everything else is a plain
	// read. Gate the logs prefix to read methods so a write under that prefix
	// falls through to the mutating switch below rather than being authorized
	// with only the download-logs permission.
	if method == http.MethodGet || method == http.MethodHead {
		if strings.HasPrefix(proxyPath, "/api/v1/system/logs") {
			return []string{authz.PermMinerDownloadLogs}
		}
		return []string{authz.PermMinerRead}
	}

	switch {
	case proxyPath == "/api/v2/mining/scoped/start" || proxyPath == "/api/v2/mining/start":
		return []string{authz.PermMinerStartMining}
	case proxyPath == "/api/v2/mining/scoped/stop" || proxyPath == "/api/v2/mining/stop":
		return []string{authz.PermMinerStopMining}
	case proxyPath == "/api/v2/hashboards/identify-led":
		return []string{authz.PermMinerBlinkLED}
	case proxyPath == "/api/v2/mining/isolation":
		// Clearing isolation re-aligns boards with the cluster's mining intent,
		// which can either start or stop mining.
		return []string{authz.PermMinerStartMining, authz.PermMinerStopMining}
	case proxyPath == "/api/v2/mining/target-power":
		return []string{authz.PermMinerSetPowerTarget}
	case strings.HasPrefix(proxyPath, "/api/v2/modules/") && strings.HasSuffix(proxyPath, "/run-in") && strings.Count(proxyPath, "/") == 5:
		return []string{authz.PermMinerStartMining, authz.PermMinerStopMining, authz.PermMinerSetPowerTarget}
	case proxyPath == "/api/v2/power/container/controls/tank_led_light":
		return []string{authz.PermMinerBlinkLED}
	case strings.HasPrefix(proxyPath, "/api/v2/power/container/controls/") ||
		strings.HasPrefix(proxyPath, "/api/v2/power/container/setpoints/"):
		return []string{authz.PermMinerSetCoolingMode}
	case method == http.MethodPost && proxyPath == "/api/v1/timeseries":
		return []string{authz.PermMinerRead}
	case strings.HasPrefix(proxyPath, "/api/v1/pools"):
		return []string{authz.PermMinerUpdatePools}
	case proxyPath == "/api/v1/cooling":
		return []string{authz.PermMinerSetCoolingMode}
	case proxyPath == "/api/v1/mining/target" || proxyPath == "/api/v1/mining/tuning":
		return []string{authz.PermMinerSetPowerTarget}
	case proxyPath == "/api/v1/mining/start":
		return []string{authz.PermMinerStartMining}
	case proxyPath == "/api/v1/mining/stop":
		return []string{authz.PermMinerStopMining}
	case proxyPath == "/api/v1/system/reboot":
		return []string{authz.PermMinerReboot}
	case proxyPath == "/api/v1/system/locate":
		return []string{authz.PermMinerBlinkLED}
	case proxyPath == "/api/v1/system/update/check":
		// Only checks whether a newer version exists — no download/install or
		// reboot — so it must not require miner:reboot like the install paths.
		return []string{authz.PermMinerFirmwareUpdate}
	case strings.HasPrefix(proxyPath, "/api/v1/system/update"):
		return []string{authz.PermMinerFirmwareUpdate, authz.PermMinerReboot}
	case proxyPath == "/api/v1/power-supplies/update":
		return []string{authz.PermMinerFirmwareUpdate, authz.PermMinerReboot}
	case strings.HasPrefix(proxyPath, "/api/v1/system/tag"):
		return []string{authz.PermMinerRename}
	case strings.HasPrefix(proxyPath, "/api/v1/auth/"):
		return []string{authz.PermMinerUpdatePassword}
	case proxyPath == "/api/v1/system/ssh" ||
		proxyPath == "/api/v1/system/unlock" ||
		proxyPath == "/api/v1/network" ||
		proxyPath == "/api/v1/system/telemetry" ||
		strings.HasPrefix(proxyPath, "/api/v1/pairing/auth-key"):
		return []string{authz.PermMinerUpdatePassword}
	default:
		// Device settings still need finer-grained Fleet permissions. Until
		// then, require an elevated miner setting permission for any mutating
		// ProtoOS endpoint that is not explicitly classified above.
		return []string{authz.PermMinerUpdatePassword}
	}
}

func copyRequestHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if shouldSkipRequestHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// responseHeaderAllowlist is the set of upstream (miner) response headers safe
// to surface from the Fleet origin. Default-deny is deliberate: the miner is
// untrusted and its bytes are served same-origin, so a compromised/misbehaving
// miner must not set origin-affecting headers — Set-Cookie, Clear-Site-Data,
// Strict-Transport-Security, or its own CSP/X-Frame-Options — that the browser
// would apply to Fleet. Only content-negotiation, caching, and range metadata
// pass through; the proxy sets its own security headers afterward.
var responseHeaderAllowlist = map[string]bool{
	"content-type":        true,
	"content-length":      true,
	"content-encoding":    true,
	"content-language":    true,
	"content-disposition": true,
	"content-range":       true,
	"accept-ranges":       true,
	"vary":                true,
	"date":                true,
	// Caching headers (cache-control, expires, etag, last-modified, age) are
	// deliberately not forwarded: these are authenticated, per-user, RBAC-gated
	// responses that must never be served from the browser cache after a
	// logout, user switch, or permission change. The proxy forces no-store
	// instead (see setResponseHardeningHeaders).
}

func copyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if !responseHeaderAllowlist[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func shouldSkipRequestHeader(key string) bool {
	switch strings.ToLower(key) {
	case "authorization", "cookie", "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailer", "transfer-encoding", "upgrade", "host":
		return true
	default:
		return false
	}
}

// isRenderingNavigation reports whether the request is a browser navigation or
// framed/embedded load rather than the embedded client's fetch/XHR (which
// carries Sec-Fetch-Dest: empty). Requests without the header — non-browser
// clients — are allowed; the sandbox/nosniff headers cover those.
func isRenderingNavigation(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Dest") {
	case "document", "iframe", "frame", "embed", "object":
		return true
	default:
		return false
	}
}

// isDisallowedCrossOriginWrite reports whether a state-changing request comes
// from a browser context that is not same-origin. Sec-Fetch-Site is set by the
// browser and cannot be forged by page JS, so for write methods only
// "same-origin" (the embedded client) is accepted; "same-site"/"cross-site"/
// "none" are rejected. Older browsers that omit Sec-Fetch-* fall back to an
// Origin host check. Non-browser callers send neither header and are not a CSRF
// vector, so they pass (the session-cookie auth still applies).
func isDisallowedCrossOriginWrite(r *http.Request) bool {
	if !isWriteMethod(r.Method) {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site != "same-origin"
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err != nil || u.Host != r.Host
	}
	return false
}

func setResponseHardeningHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("X-Frame-Options", "DENY")
	// Authenticated, per-user, RBAC-gated data must not linger in the browser
	// cache where a later session/user could read it without re-authorizing.
	h.Set("Cache-Control", "no-store")
}

// parseRoutableMinerAddr validates a discovered miner address as a literal IP in
// a range we are willing to dial. Parsing as a literal (not a hostname) rules
// out DNS rebinding; rejecting loopback, link-local (including the
// 169.254.169.254 cloud-metadata endpoint), multicast, and the unspecified
// address blocks a stale or poisoned discovery record from steering fleet-api
// into an SSRF or leaking decrypted credentials to an unexpected host.
// minerHost formats the dialable host for a validated miner address. IPv6
// literals are bracketed so url.URL parses them correctly — including the
// default-port case, where there is no port to trigger net.JoinHostPort's
// own bracketing and a bare "2001:db8::1" would be mis-read as host:port.
func minerHost(addr netip.Addr, port string) (string, error) {
	if port == "" || port == "0" {
		if addr.Is6() {
			return "[" + addr.String() + "]", nil
		}
		return addr.String(), nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fleeterror.NewFailedPreconditionErrorf("miner port %q cannot be proxied", port)
	}
	return net.JoinHostPort(addr.String(), port), nil
}

func parseRoutableMinerAddr(ipAddress string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(ipAddress)
	if err != nil {
		return netip.Addr{}, fleeterror.NewFailedPreconditionErrorf("miner address %q is not a valid IP", ipAddress)
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsInterfaceLocalMulticast() || addr.IsUnspecified() {
		return netip.Addr{}, fleeterror.NewFailedPreconditionErrorf("miner address %q is not a routable miner address", ipAddress)
	}
	return addr, nil
}

func httpStatusForError(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusBadGateway
	case fleeterror.IsAuthenticationError(err):
		return http.StatusUnauthorized
	case fleeterror.IsForbiddenError(err):
		return http.StatusForbidden
	case fleeterror.IsNotFoundError(err):
		return http.StatusNotFound
	case fleeterror.IsInvalidArgumentError(err):
		return http.StatusBadRequest
	case fleeterror.IsFailedPreconditionError(err):
		return http.StatusPreconditionFailed
	case fleeterror.IsUnavailableError(err):
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func clientMessageForError(err error) string {
	switch {
	case fleeterror.IsAuthenticationError(err):
		return "authentication required"
	case fleeterror.IsForbiddenError(err):
		return "permission denied"
	case fleeterror.IsNotFoundError(err):
		return "miner not found or cannot be proxied"
	case errors.Is(err, context.DeadlineExceeded):
		return "miner proxy request timed out"
	case fleeterror.IsInvalidArgumentError(err), fleeterror.IsFailedPreconditionError(err), fleeterror.IsUnavailableError(err):
		return err.Error()
	default:
		return "miner proxy request failed"
	}
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(errorResponse{Error: message}); err != nil {
		slog.Error("failed to encode miner proxy error response", "error", err)
	}
}
