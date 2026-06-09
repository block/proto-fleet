package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
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
		return nil, err
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Jar: jar,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion:         tls.VersionTLS12,
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

func (c *Client) invoke(ctx context.Context, method string, req proto.Message, resp proto.Message, mode authMode) error {
	body, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}.Marshal(req)
	if err != nil {
		return err
	}

	endpoint := c.baseURL.JoinPath(strings.TrimPrefix(method, "/"))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	switch mode {
	case authBearer:
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
			break
		}
		if err := c.ensureSession(ctx); err != nil {
			return fmt.Errorf("api key or username/password is required for %s: %w", method, err)
		}
	case authSession:
		if err := c.ensureSession(ctx); err != nil {
			return fmt.Errorf("session cookie or username/password is required for %s: %w", method, err)
		}
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return err
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
	return len(c.httpClient.Jar.Cookies(c.baseURL)) > 0
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

	path := strings.TrimSuffix(u.Path, "/")
	if path == "" {
		path = "/api-proxy"
	}
	u.Path = path
	u.RawPath = ""
	return u, nil
}
