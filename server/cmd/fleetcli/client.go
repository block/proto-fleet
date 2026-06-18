package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	apikeyv1 "github.com/block/proto-fleet/server/generated/grpc/apikey/v1"
	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	fleetperformancev1 "github.com/block/proto-fleet/server/generated/grpc/fleetperformance/v1"
	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// APIError is returned when the server responds with a non-2xx status. It
// preserves the raw response body so callers can format it (e.g. colorized
// JSON) without parsing the error string.
type APIError struct {
	Method string
	Status string
	Body   []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s returned %s: %s", e.Method, e.Status, strings.TrimSpace(string(e.Body)))
}

type Options struct {
	Server   string
	APIKey   string
	Username string
	Password string
	Insecure bool
	Debug    bool
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	apiKey     string
	username   string
	password   string
	debug      bool
}

type authMode int

const (
	authNone authMode = iota
	authBearer
	authSession
)

func New(_ context.Context, opts Options) (*Client, error) {
	if opts.Server == "" {
		return nil, fmt.Errorf("server is required")
	}

	baseURL, err := normalizeBaseURL(opts.Server, opts.Insecure)
	if err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Jar: &loopbackSecureJar{inner: jar},
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					// #nosec G402 -- insecure TLS is an explicit user opt-in via --insecure.
					InsecureSkipVerify: opts.Insecure,
				},
			},
			Timeout: 30 * time.Second,
		},
		apiKey:   opts.APIKey,
		username: strings.TrimSpace(opts.Username),
		password: opts.Password,
		debug:    opts.Debug,
	}, nil
}

func (c *Client) Close() error {
	return nil
}

func (c *Client) CallBearer(ctx context.Context, method string, req proto.Message, resp proto.Message) error {
	return c.invoke(ctx, method, req, resp, authBearer)
}

func (c *Client) CallSession(ctx context.Context, method string, req proto.Message, resp proto.Message) error {
	return c.invoke(ctx, method, req, resp, authSession)
}

func (c *Client) CallAnonymous(ctx context.Context, method string, req proto.Message, resp proto.Message) error {
	return c.invoke(ctx, method, req, resp, authNone)
}

func (c *Client) Authenticate(ctx context.Context, username, password string) (*authv1.AuthenticateResponse, error) {
	req := &authv1.AuthenticateRequest{
		Username: username,
		Password: password,
	}
	resp := &authv1.AuthenticateResponse{}
	if err := c.invoke(ctx, "/auth.v1.AuthService/Authenticate", req, resp, authNone); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CreateAPIKey(ctx context.Context, req *apikeyv1.CreateApiKeyRequest) (*apikeyv1.CreateApiKeyResponse, error) {
	resp := &apikeyv1.CreateApiKeyResponse{}
	if err := c.invoke(ctx, "/apikey.v1.ApiKeyService/CreateApiKey", req, resp, authSession); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ListAPIKeys(ctx context.Context) (*apikeyv1.ListApiKeysResponse, error) {
	resp := &apikeyv1.ListApiKeysResponse{}
	if err := c.invoke(ctx, "/apikey.v1.ApiKeyService/ListApiKeys", &apikeyv1.ListApiKeysRequest{}, resp, authSession); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) RevokeAPIKey(ctx context.Context, keyID string) (*apikeyv1.RevokeApiKeyResponse, error) {
	resp := &apikeyv1.RevokeApiKeyResponse{}
	req := &apikeyv1.RevokeApiKeyRequest{KeyId: keyID}
	if err := c.invoke(ctx, "/apikey.v1.ApiKeyService/RevokeApiKey", req, resp, authSession); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ListMiners(ctx context.Context, pageSize int32, cursor string) (*fleetmanagementv1.ListMinerStateSnapshotsResponse, error) {
	req := &fleetmanagementv1.ListMinerStateSnapshotsRequest{
		PageSize: pageSize,
		Cursor:   cursor,
	}
	resp := &fleetmanagementv1.ListMinerStateSnapshotsResponse{}
	if err := c.invoke(ctx, "/fleetmanagement.v1.FleetManagementService/ListMinerStateSnapshots", req, resp, authBearer); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetFleetPerformance(ctx context.Context) (*fleetperformancev1.GetFleetPerformanceResponse, error) {
	resp := &fleetperformancev1.GetFleetPerformanceResponse{}
	if err := c.invoke(ctx, "/fleetperformance.v1.FleetPerformanceService/GetFleetPerformance", &fleetperformancev1.GetFleetPerformanceRequest{}, resp, authBearer); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetCombinedMetrics(ctx context.Context, req *telemetryv1.GetCombinedMetricsRequest) (*telemetryv1.GetCombinedMetricsResponse, error) {
	resp := &telemetryv1.GetCombinedMetricsResponse{}
	if err := c.invoke(ctx, "/telemetry.v1.TelemetryService/GetCombinedMetrics", req, resp, authBearer); err != nil {
		return nil, err
	}
	return resp, nil
}

// applyBearerAuth applies the same auth policy as bearer-mode JSON calls: the
// API key when present, otherwise a cookie session established from the
// global username/password.
func (c *Client) applyBearerAuth(ctx context.Context, header http.Header, method string) error {
	if c.apiKey != "" {
		header.Set("Authorization", "Bearer "+c.apiKey)
		return nil
	}
	if err := c.ensureSession(ctx); err != nil {
		return fmt.Errorf("api key or username/password is required for %s: %w", method, err)
	}
	return nil
}

// transferClient returns an HTTP client without an overall timeout, for
// long-running transfers (large firmware uploads, discovery scans) that can
// outlive the default per-request budget. It shares the cookie jar and
// transport so session auth and TLS settings still apply; cancellation comes
// from ctx.
func (c *Client) transferClient() *http.Client {
	return &http.Client{Jar: c.httpClient.Jar, Transport: c.httpClient.Transport}
}

func (c *Client) invoke(ctx context.Context, method string, req proto.Message, resp proto.Message, mode authMode) error {
	body, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", method, err)
	}

	endpoint := c.baseURL.JoinPath(strings.TrimPrefix(method, "/"))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	switch mode {
	case authNone:
		// No credentials attached.
	case authBearer:
		if err := c.applyBearerAuth(ctx, httpReq.Header, method); err != nil {
			return err
		}
	case authSession:
		if err := c.ensureSession(ctx); err != nil {
			return fmt.Errorf("session cookie or username/password is required for %s: %w", method, err)
		}
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return &APIError{Method: method, Status: httpResp.Status, Body: respBody}
	}

	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}

	unmarshalOptions := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshalOptions.Unmarshal(respBody, resp); err != nil {
		return fmt.Errorf("failed to decode %s response: %w", method, err)
	}

	return nil
}

func (c *Client) ensureSession(ctx context.Context) error {
	if c.hasSession() {
		return nil
	}
	if c.username == "" || c.password == "" {
		return fmt.Errorf("username and password must both be provided")
	}
	if _, err := c.Authenticate(ctx, c.username, c.password); err != nil {
		return err
	}
	if !c.hasSession() {
		return fmt.Errorf("authenticate did not yield a reusable session")
	}
	return nil
}

func (c *Client) hasSession() bool {
	// The jar's path matching needs a non-empty request path; a root base URL
	// has none, so probe with "/" the way real request URLs would.
	target := *c.baseURL
	if target.Path == "" {
		target.Path = "/"
	}
	return len(c.httpClient.Jar.Cookies(&target)) > 0
}

// loopbackSecureJar treats plain-HTTP loopback origins as secure contexts the
// way browsers do, so the Secure-flagged fleet session cookie works against a
// local fleet-api without weakening cookie handling for remote hosts.
type loopbackSecureJar struct {
	inner http.CookieJar
}

func (j *loopbackSecureJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.inner.SetCookies(loopbackAsHTTPS(u), cookies)
}

func (j *loopbackSecureJar) Cookies(u *url.URL) []*http.Cookie {
	return j.inner.Cookies(loopbackAsHTTPS(u))
}

func loopbackAsHTTPS(u *url.URL) *url.URL {
	if u.Scheme != "http" {
		return u
	}
	host := u.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return u
		}
	}
	clone := *u
	clone.Scheme = "https"
	return &clone
}

func normalizeBaseURL(server string, insecureOverride bool) (*url.URL, error) {
	value := strings.TrimSpace(server)
	if value == "" {
		return nil, fmt.Errorf("server is required")
	}

	if !strings.Contains(value, "://") {
		scheme := "https"
		if insecureOverride {
			scheme = "http"
		}
		value = scheme + "://" + value
	}

	u, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("server URL must include a host")
	}

	// A bare host defaults to the nginx proxy route, while an explicit
	// trailing slash ("http://localhost:4000/") targets the RPC root for
	// direct fleet-api access.
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" && u.Path == "" {
		path = "/api-proxy"
	}
	u.Path = path
	u.RawPath = ""
	return u, nil
}
