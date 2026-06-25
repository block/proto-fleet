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
	"net/url"
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
	defaultProtoURLScheme = "http"
)

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
	tokens  map[string]string
}

type proxyTarget struct {
	deviceIdentifier string
	baseURL          string
	siteID           *int64
	passwordEnc      sql.NullString
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
		tokens:             make(map[string]string),
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

	target, err := h.resolveTarget(ctx, deviceIdentifier, info.OrganizationID)
	if err != nil {
		writeError(w, httpStatusForError(err), clientMessageForError(err))
		return
	}

	proxyPath := "/api/v1/" + rest
	requiredPermission := permissionFor(r.Method, proxyPath)
	if _, err := middleware.RequirePermission(ctx, requiredPermission, authz.ResourceContext{SiteID: target.siteID}); err != nil {
		writeError(w, httpStatusForError(err), clientMessageForError(err))
		return
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

	if resp.StatusCode == http.StatusUnauthorized && target.passwordEnc.Valid {
		_, _ = io.Copy(io.Discard, resp.Body)
		h.clearToken(target.deviceIdentifier)
		resp.Body.Close()

		resp, err = h.forward(ctx, r, target, proxyPath, body, true)
		if err != nil {
			writeError(w, httpStatusForError(err), clientMessageForError(err))
			return
		}
		defer resp.Body.Close()
	}

	copyResponseHeaders(w.Header(), resp.Header)
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

	host := row.IpAddress
	if row.Port != "" && row.Port != "0" {
		host = net.JoinHostPort(row.IpAddress, row.Port)
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
	if !target.passwordEnc.Valid || target.passwordEnc.String == "" {
		return "", nil
	}

	if !forceLogin {
		h.tokenMu.Lock()
		token := h.tokens[target.deviceIdentifier]
		h.tokenMu.Unlock()
		if token != "" {
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

	h.tokenMu.Lock()
	h.tokens[target.deviceIdentifier] = token
	h.tokenMu.Unlock()
	return token, nil
}

func (h *Handler) clearToken(deviceIdentifier string) {
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	delete(h.tokens, deviceIdentifier)
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
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
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

func permissionFor(method string, proxyPath string) string {
	if strings.HasPrefix(proxyPath, "/api/v1/system/logs") {
		return authz.PermMinerDownloadLogs
	}

	if method == http.MethodGet || method == http.MethodHead {
		return authz.PermMinerRead
	}

	switch {
	case proxyPath == "/api/v1/timeseries":
		return authz.PermMinerRead
	case strings.HasPrefix(proxyPath, "/api/v1/pools"):
		return authz.PermMinerUpdatePools
	case proxyPath == "/api/v1/cooling":
		return authz.PermMinerSetCoolingMode
	case proxyPath == "/api/v1/mining/target" || proxyPath == "/api/v1/mining/tuning":
		return authz.PermMinerSetPowerTarget
	case proxyPath == "/api/v1/mining/start":
		return authz.PermMinerStartMining
	case proxyPath == "/api/v1/mining/stop":
		return authz.PermMinerStopMining
	case proxyPath == "/api/v1/system/reboot":
		return authz.PermMinerReboot
	case proxyPath == "/api/v1/system/locate":
		return authz.PermMinerBlinkLED
	case strings.HasPrefix(proxyPath, "/api/v1/system/update"):
		return authz.PermMinerFirmwareUpdate
	case proxyPath == "/api/v1/power-supplies/update":
		return authz.PermMinerFirmwareUpdate
	case strings.HasPrefix(proxyPath, "/api/v1/system/tag"):
		return authz.PermMinerRename
	case strings.HasPrefix(proxyPath, "/api/v1/auth/"):
		return authz.PermMinerUpdatePassword
	case proxyPath == "/api/v1/system/ssh" ||
		proxyPath == "/api/v1/system/unlock" ||
		proxyPath == "/api/v1/network" ||
		proxyPath == "/api/v1/system/telemetry" ||
		strings.HasPrefix(proxyPath, "/api/v1/pairing/auth-key"):
		return authz.PermMinerUpdatePassword
	default:
		// Device settings still need finer-grained Fleet permissions. Until
		// then, require an elevated miner setting permission for any mutating
		// ProtoOS endpoint that is not explicitly classified above.
		return authz.PermMinerUpdatePassword
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
