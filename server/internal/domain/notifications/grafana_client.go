package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Grafana provides typed access to the subset of Grafana's HTTP API
// the notifications domain uses:
//
//   - Provisioning API for contact points and alert rules
//     (/api/v1/provisioning/contact-points, /api/v1/provisioning/alert-rules)
//   - Alertmanager API for silences (/api/alertmanager/grafana/api/v2/silences)
//   - Provisioning test endpoint for synthetic deliveries
//     (/api/v1/provisioning/contact-points/test)
//
// The client deliberately doesn't model Grafana's full schema — only
// the fields fleet-api writes or reads. Anything else round-trips as
// `json.RawMessage` so a Grafana minor-version bump that adds fields
// doesn't break the call.
//
// Auth: in prod we inject a Grafana service-account token via
// `Authorization: Bearer <token>`. In dev, Grafana ships with admin
// auth only and no service account, so we fall back to basic auth
// against the admin user — the docker-compose defaults wire
// GRAFANA_ADMIN_USER / GRAFANA_ADMIN_PASSWORD through to this client
// so the local stack works out of the box. The token, if set, takes
// precedence over the basic-auth credentials.
type Grafana struct {
	baseURL    string
	token      string
	user       string
	password   string
	httpClient *http.Client
}

// GrafanaConfig is the operator-facing config for the Grafana client.
// Bound from environment variables under FLEET_METRICS_GRAFANA_*.
type GrafanaConfig struct {
	URL      string        `help:"Base URL of the Grafana sidecar (no trailing slash)" default:"http://grafana:3000" env:"URL"`
	Token    string        `help:"Service-account token with Editor permissions on org 1. Takes precedence over user/password when set." default:"" env:"TOKEN"`
	User     string        `help:"Grafana basic-auth username (dev fallback when no service-account token is available)." default:"admin" env:"USER"`
	Password string        `help:"Grafana basic-auth password (dev fallback when no service-account token is available)." default:"admin" env:"PASSWORD"`
	Timeout  time.Duration `help:"HTTP client timeout for Grafana calls" default:"10s" env:"TIMEOUT"`
}

// NewGrafana returns a Grafana client configured for the supplied
// sidecar.
func NewGrafana(cfg GrafanaConfig) *Grafana {
	return &Grafana{
		baseURL:  strings.TrimRight(cfg.URL, "/"),
		token:    cfg.Token,
		user:     cfg.User,
		password: cfg.Password,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// === Contact points ====================================================

// GrafanaContactPoint mirrors the provisioning API's contact-point
// shape. The Settings field is opaque JSON because every receiver
// kind has its own schema.
type GrafanaContactPoint struct {
	UID                   string          `json:"uid,omitempty"`
	Name                  string          `json:"name"`
	Type                  string          `json:"type"`
	Settings              json.RawMessage `json:"settings"`
	DisableResolveMessage bool            `json:"disableResolveMessage,omitempty"`
}

func (g *Grafana) ListContactPoints(ctx context.Context) ([]GrafanaContactPoint, error) {
	var out []GrafanaContactPoint
	if err := g.do(ctx, http.MethodGet, "/api/v1/provisioning/contact-points", nil, &out); err != nil {
		return nil, fmt.Errorf("list contact points: %w", err)
	}
	return out, nil
}

func (g *Grafana) CreateContactPoint(ctx context.Context, cp GrafanaContactPoint) (*GrafanaContactPoint, error) {
	var out GrafanaContactPoint
	if err := g.do(ctx, http.MethodPost, "/api/v1/provisioning/contact-points", cp, &out); err != nil {
		return nil, fmt.Errorf("create contact point: %w", err)
	}
	return &out, nil
}

func (g *Grafana) UpdateContactPoint(ctx context.Context, uid string, cp GrafanaContactPoint) (*GrafanaContactPoint, error) {
	var out GrafanaContactPoint
	if err := g.do(ctx, http.MethodPut, "/api/v1/provisioning/contact-points/"+uid, cp, &out); err != nil {
		return nil, fmt.Errorf("update contact point: %w", err)
	}
	return &out, nil
}

func (g *Grafana) DeleteContactPoint(ctx context.Context, uid string) error {
	if err := g.do(ctx, http.MethodDelete, "/api/v1/provisioning/contact-points/"+uid, nil, nil); err != nil {
		return fmt.Errorf("delete contact point: %w", err)
	}
	return nil
}

// TestContactPoint sends a synthetic alert through the supplied
// receiver definition. Grafana accepts the same body as the create
// endpoint plus an `alert` field; we let the caller pass the shaped
// payload so we don't double-marshal.
func (g *Grafana) TestContactPoint(ctx context.Context, payload any) (int, error) {
	resp, err := g.rawPost(ctx, "/api/v1/provisioning/contact-points/test", payload)
	if err != nil {
		return 0, fmt.Errorf("test contact point: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// === Alert rules =======================================================

// GrafanaAlertRule mirrors the subset of the provisioning alert-rule
// shape we read and write. The Data field is opaque JSON because
// Grafana's expression model is rich enough that round-tripping it as
// a strongly-typed struct would couple us to internal Grafana types.
type GrafanaAlertRule struct {
	UID          string            `json:"uid,omitempty"`
	OrgID        int64             `json:"orgID,omitempty"`
	FolderUID    string            `json:"folderUID,omitempty"`
	RuleGroup    string            `json:"ruleGroup"`
	Title        string            `json:"title"`
	Condition    string            `json:"condition"`
	Data         json.RawMessage   `json:"data"`
	For          string            `json:"for,omitempty"`
	NoDataState  string            `json:"noDataState,omitempty"`
	ExecErrState string            `json:"execErrState,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	IsPaused     bool              `json:"isPaused,omitempty"`
}

func (g *Grafana) ListAlertRules(ctx context.Context) ([]GrafanaAlertRule, error) {
	var out []GrafanaAlertRule
	if err := g.do(ctx, http.MethodGet, "/api/v1/provisioning/alert-rules", nil, &out); err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	return out, nil
}

// GetAlertRule fetches a single rule by UID. Used by pause / resume
// so we can round-trip the opaque Data + Annotations fields back
// through PUT without dropping anything.
func (g *Grafana) GetAlertRule(ctx context.Context, uid string) (*GrafanaAlertRule, error) {
	var out GrafanaAlertRule
	if err := g.do(ctx, http.MethodGet, "/api/v1/provisioning/alert-rules/"+uid, nil, &out); err != nil {
		return nil, fmt.Errorf("get alert rule: %w", err)
	}
	return &out, nil
}

// Note: there is intentionally no CreateAlertRule / UpdateAlertRule /
// DeleteAlertRule at this layer. The alert-rule set is owned by the
// provisioning YAML, and Grafana 11.6+ enforces a "cannot change
// provenance from 'file' to ''" guard that prevents the API from
// modifying YAML-provisioned rules in place anyway. Pause / resume
// is implemented at the silence layer (see PauseRule / ResumeRule
// in the service).

// === Silences ==========================================================

// GrafanaSilence mirrors the Alertmanager-compatible silence shape
// Grafana exposes at /api/alertmanager/grafana/api/v2/silences.
type GrafanaSilence struct {
	ID        string                  `json:"id,omitempty"`
	Status    *GrafanaSilenceStatus   `json:"status,omitempty"`
	StartsAt  time.Time               `json:"startsAt"`
	EndsAt    time.Time               `json:"endsAt"`
	CreatedBy string                  `json:"createdBy"`
	Comment   string                  `json:"comment"`
	Matchers  []GrafanaSilenceMatcher `json:"matchers"`
}

type GrafanaSilenceStatus struct {
	State string `json:"state"`
}

type GrafanaSilenceMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

const silencesPath = "/api/alertmanager/grafana/api/v2/silences"

func (g *Grafana) ListSilences(ctx context.Context) ([]GrafanaSilence, error) {
	var out []GrafanaSilence
	if err := g.do(ctx, http.MethodGet, silencesPath, nil, &out); err != nil {
		return nil, fmt.Errorf("list silences: %w", err)
	}
	return out, nil
}

// PutSilence creates a new silence or updates an existing one. The
// Alertmanager API takes the silence id in the body, not the URL.
func (g *Grafana) PutSilence(ctx context.Context, s GrafanaSilence) (string, error) {
	var out struct {
		SilenceID string `json:"silenceID"`
	}
	if err := g.do(ctx, http.MethodPost, silencesPath, s, &out); err != nil {
		return "", fmt.Errorf("put silence: %w", err)
	}
	return out.SilenceID, nil
}

func (g *Grafana) DeleteSilence(ctx context.Context, id string) error {
	path := "/api/alertmanager/grafana/api/v2/silence/" + id
	if err := g.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("delete silence: %w", err)
	}
	return nil
}

// === transport =========================================================

// do is the generic JSON request/response helper. Pass nil body for
// requests without one; pass nil out for responses you want to discard.
//
// On a non-2xx response, do() logs the request body and the response
// body at WARN, with secret-bearing fields redacted (see
// redactSecrets). The body field is opaque JSON from Grafana's
// perspective (`data` for alert rules is dozens of lines), so the
// only way to debug a 500 is to see what we sent and what came back.
// Operators can grep `notifications.grafana_error` in fleet-api's
// stdout.
func (g *Grafana) do(ctx context.Context, method, path string, body, out any) error {
	var reqJSON []byte
	if body != nil {
		var marshalErr error
		reqJSON, marshalErr = json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("marshal request body: %w", marshalErr)
		}
	}
	resp, err := g.requestWithBytes(ctx, method, path, reqJSON)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Warn(
			"notifications.grafana_error",
			"method", method,
			"path", path,
			"status", resp.StatusCode,
			"request_body", redactSecrets(reqJSON),
			"response_body", redactSecrets(respBody),
		)
		// The message rides back to the browser via err.Error(), and a
		// Grafana/proxy error can echo the request body — which on
		// update paths carries stored secrets. Redact before returning.
		msg := strings.TrimSpace(redactSecrets(respBody))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return &GrafanaError{StatusCode: resp.StatusCode, Message: msg}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (g *Grafana) rawPost(ctx context.Context, path string, body any) (*http.Response, error) {
	return g.request(ctx, http.MethodPost, path, body)
}

// redactedLogKeys are JSON field names whose values are secrets and
// must never reach the log stream. Channel payloads carry webhook
// bearer tokens and SMTP passwords inside the opaque settings object;
// "url" is included because webhook URLs routinely embed capability
// tokens in the path or query (Slack, PagerDuty, Teams); the extra
// keys cover credential fields of other Grafana receiver types this
// client may round-trip in the future.
var redactedLogKeys = map[string]bool{
	"authorization_credentials": true,
	"smtpPassword":              true,
	"password":                  true,
	"basicAuthPassword":         true,
	"bearerToken":               true,
	"token":                     true,
	"secureSettings":            true,
	"url":                       true,
}

// redactSecrets returns body with every redactedLogKeys value replaced
// by a placeholder, recursing through nested objects and arrays.
// Non-JSON input is returned verbatim — request bodies are always
// JSON we marshalled ourselves, so the redaction path always covers
// the payloads that carry secrets.
func redactSecrets(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	redacted, err := json.Marshal(redactValue(v))
	if err != nil {
		return "<failed to re-marshal redacted body>"
	}
	return string(redacted)
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if redactedLogKeys[k] {
				// Keep empty strings as-is so the log still shows
				// whether a secret was present at all.
				if s, ok := val.(string); ok && s == "" {
					continue
				}
				t[k] = "[REDACTED]"
				continue
			}
			t[k] = redactValue(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = redactValue(t[i])
		}
		return t
	}
	return v
}

// requestWithBytes is the same as request, but takes a pre-marshalled
// body so do() can log it on errors without re-marshalling.
func (g *Grafana) requestWithBytes(ctx context.Context, method, path string, bodyBytes []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	return g.send(ctx, method, path, bodyReader, bodyBytes != nil)
}

func (g *Grafana) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	hasBody := false
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
		hasBody = true
	}
	return g.send(ctx, method, path, bodyReader, hasBody)
}

func (g *Grafana) send(ctx context.Context, method, path string, bodyReader io.Reader, hasBody bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	// Token wins when set — prod uses a service-account token.
	// In dev there's no service account, so fall back to basic auth
	// against the Grafana admin user. We don't refuse to send the
	// request when neither is set; Grafana will 401 and the error
	// path will surface that to the operator.
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	} else if g.user != "" {
		req.SetBasicAuth(g.user, g.password)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	// The provisioning API requires this header on writes to disable
	// the "this object is provisioned, please edit the file" lock.
	if method != http.MethodGet {
		req.Header.Set("X-Disable-Provenance", "true")
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: %w", method, path, err)
	}
	return resp, nil
}

// GrafanaError carries a non-2xx Grafana response back to the
// handler layer.
type GrafanaError struct {
	StatusCode int
	Message    string
}

func (e *GrafanaError) Error() string {
	return fmt.Sprintf("grafana %d: %s", e.StatusCode, e.Message)
}

// IsNotFound reports whether err originated as a 404 from Grafana.
func IsNotFound(err error) bool {
	var ge *GrafanaError
	if errors.As(err, &ge) {
		return ge.StatusCode == http.StatusNotFound
	}
	return false
}
