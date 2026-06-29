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
	queries            *sqlc.Queries
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
		queries:            sqlc.New(db.NewRetryDB(conn)),
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

	target, err := h.resolveTarget(ctx, deviceIdentifier, info.OrganizationID)
	if err != nil {
		writeError(w, httpStatusForError(err), clientMessageForError(err))
		return
	}

	proxyPath := "/api/v1/" + rest
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

	host := addr.String()
	if row.Port != "" && row.Port != "0" {
		port, perr := strconv.Atoi(row.Port)
		if perr != nil || port < 1 || port > 65535 {
			return proxyTarget{}, fleeterror.NewFailedPreconditionErrorf("miner port %q cannot be proxied", row.Port)
		}
		host = net.JoinHostPort(addr.String(), row.Port)
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

// permissionsFor returns every permission a proxied request must hold. Most
// endpoints need a single permission, but some need more than one — firmware
// installs can reboot the miner, so they require both firmware-update and
// reboot, matching the direct Fleet command path (command/handler.go).
func permissionsFor(method string, proxyPath string) []string {
	if strings.HasPrefix(proxyPath, "/api/v1/system/logs") {
		return []string{authz.PermMinerDownloadLogs}
	}

	if method == http.MethodGet || method == http.MethodHead {
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

func copyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if shouldSkipResponseHeader(key) {
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

func shouldSkipResponseHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer",
		"transfer-encoding", "upgrade", "set-cookie":
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

func setResponseHardeningHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("X-Frame-Options", "DENY")
}

// parseRoutableMinerAddr validates a discovered miner address as a literal IP in
// a range we are willing to dial. Parsing as a literal (not a hostname) rules
// out DNS rebinding; rejecting loopback, link-local (including the
// 169.254.169.254 cloud-metadata endpoint), multicast, and the unspecified
// address blocks a stale or poisoned discovery record from steering fleet-api
// into an SSRF or leaking decrypted credentials to an unexpected host.
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
