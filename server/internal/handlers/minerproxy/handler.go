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

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

const (
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

type Handler struct {
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
) http.Handler {
	return &Handler{
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
	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: proxyClientTimeout,
		ForceAttemptHTTP2:     skipVerify,
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

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, info, err := h.authenticate(r)
	if err != nil {
		slog.Warn("miner proxy authentication failed", "error", err)
		writeError(w, httpStatusForError(err), "authentication required")
		return
	}

	deviceIdentifier := r.PathValue("deviceIdentifier")
	rest := r.PathValue("rest")
	if deviceIdentifier == "" || rest == "" {
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

	target, err := h.resolveTarget(ctx, deviceIdentifier, info.OrganizationID)
	if err != nil {
		writeError(w, httpStatusForError(err), clientMessageForError(err))
		return
	}

	proxyPath, ok := proxyPathFor(rest)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid miner API path")
		return
	}
	if isUnproxyableEndpoint(r.Method, proxyPath) {
		writeError(w, http.StatusForbidden, "this miner endpoint is managed by Fleet and cannot be changed from the embedded view")
		return
	}

	for _, permission := range permissionsFor(r.Method, proxyPath) {
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: target.siteID}); err != nil {
			writeError(w, httpStatusForError(err), clientMessageForError(err))
			return
		}
	}

	body, err := readProxyBody(w, r)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}

	resp, err := h.forward(ctx, r, target, proxyPath, body, false)
	if err != nil {
		writeError(w, httpStatusForError(err), clientMessageForError(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && target.hasCredentials() {
		_, _ = io.Copy(io.Discard, resp.Body)
		h.clearToken(target.cacheKey())
		resp.Body.Close()

		resp, err = h.forward(ctx, r, target, proxyPath, body, true)
		if err != nil {
			writeError(w, httpStatusForError(err), clientMessageForError(err))
			return
		}
		defer resp.Body.Close()
	}

	copyResponseHeaders(w.Header(), resp.Header)
	// Set after copying so a miner cannot override them: neutralize any
	// active content the miner might return on the same-origin Fleet URL.
	// sandbox + default-src 'none' stops scripts even if the body is HTML, and
	// nosniff stops MIME-sniffing a benign type into something executable.
	setResponseHardeningHeaders(w.Header())
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		slog.Warn("miner proxy response copy failed", "device_identifier", deviceIdentifier, "error", err)
	}
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
	token, err := h.tokenFor(ctx, target, forceLogin)
	if err != nil {
		return nil, err
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

	resp, err := h.clientFor(target.baseURL).Do(req)
	if err != nil {
		return nil, fleeterror.NewUnavailableErrorf("failed to reach miner: %v", err)
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

	token, err := h.login(ctx, target.baseURL, string(passwordBytes))
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

func (h *Handler) login(ctx context.Context, baseURL string, password string) (string, error) {
	body, err := json.Marshal(loginRequest{Password: password})
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create miner login request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create miner login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.clientFor(baseURL).Do(req)
	if err != nil {
		return "", fleeterror.NewUnavailableErrorf("failed to authenticate with miner: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fleeterror.NewUnauthenticatedError("stored miner credentials were rejected")
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fleeterror.NewFailedPreconditionErrorf("miner login failed with status %d", resp.StatusCode)
	}

	var tokens loginResponse
	// Bound the response: the login endpoint is dialed at a discovery-sourced
	// address, so a hostile/wrong host must not be able to stream an unbounded
	// body into the decoder.
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxLoginResponseBytes)).Decode(&tokens); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to decode miner login response: %v", err)
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

// proxyPathFor builds the upstream miner path from the {rest...} wildcard and
// reports whether it is canonical. The wildcard preserves percent-decoded
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
func proxyPathFor(rest string) (string, bool) {
	proxyPath := "/api/v1/" + rest
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
	if isWriteMethod(method) && strings.HasPrefix(proxyPath, "/api/v1/pools") {
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
	case proxyPath == "/api/v1/timeseries":
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
	default:
		return err.Error()
	}
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(errorResponse{Error: message}); err != nil {
		slog.Error("failed to encode miner proxy error response", "error", err)
	}
}
