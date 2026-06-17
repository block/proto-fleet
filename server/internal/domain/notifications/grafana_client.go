package notifications

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Grafana struct {
	baseURL    string
	token      string
	user       string
	password   string
	httpClient *http.Client
}

type GrafanaConfig struct {
	URL      string        `help:"Base URL of the Grafana sidecar (no trailing slash)" default:"http://grafana:3000" env:"URL"`
	Token    string        `help:"Service-account token with Editor permissions on org 1. Takes precedence over user/password when set." default:"" env:"TOKEN"`
	User     string        `help:"Grafana basic-auth username (dev fallback when no service-account token is available)." default:"admin" env:"USER"`
	Password string        `help:"Grafana basic-auth password (dev fallback when no service-account token is available)." default:"admin" env:"PASSWORD"`
	Timeout  time.Duration `help:"HTTP client timeout for Grafana calls" default:"10s" env:"TIMEOUT"`
}

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

// Grafana answers PUT with a 202 Ack body, not the contact point; nothing useful to decode.
func (g *Grafana) UpdateContactPoint(ctx context.Context, uid string, cp GrafanaContactPoint) error {
	if err := g.do(ctx, http.MethodPut, "/api/v1/provisioning/contact-points/"+uid, cp, nil); err != nil {
		return fmt.Errorf("update contact point: %w", err)
	}
	return nil
}

func (g *Grafana) DeleteContactPoint(ctx context.Context, uid string) error {
	if err := g.do(ctx, http.MethodDelete, "/api/v1/provisioning/contact-points/"+uid, nil, nil); err != nil {
		return fmt.Errorf("delete contact point: %w", err)
	}
	return nil
}

// ReceiverTestResult is the outcome of a Grafana "test contact point" call. The
// receiver test endpoint answers HTTP 200 even when delivery to the destination
// fails, so the real result lives in the decoded status/error, not the status code.
type ReceiverTestResult struct {
	OK    bool
	Error string
}

// Grafana addresses an alerting receiver by base64(name) in its resource API.
const grafanaAlertingNamespace = "default"

// TestReceiverIntegration asks Grafana to deliver a synthetic alert to a single
// integration and reports whether the destination accepted it.
//
// Grafana 13 removed both the legacy provisioning route
// (POST /api/v1/provisioning/contact-points/test, now 404) and the old
// Alertmanager route (POST /api/alertmanager/grafana/config/api/v1/receivers/test,
// now 410). The live endpoint is the notifications.alerting.grafana.app resource
// API: it addresses an existing receiver by base64(name) in the path and reads the
// integration actually under test from the request body.
func (g *Grafana) TestReceiverIntegration(ctx context.Context, receiverName, integrationType string, settings json.RawMessage) (ReceiverTestResult, error) {
	// URL-safe base64 is how Grafana derives the receiver's k8s resource name, and
	// it keeps the value free of '/'/'+' so it stays a single, unescaped path segment.
	name := base64.RawURLEncoding.EncodeToString([]byte(receiverName))
	path := "/apis/notifications.alerting.grafana.app/v1beta1/namespaces/" + grafanaAlertingNamespace + "/receivers/" + name + "/test"
	body := map[string]any{
		"integration": map[string]any{
			"type":     integrationType,
			"version":  "",
			"settings": settings,
		},
	}
	var out struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := g.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return ReceiverTestResult{}, fmt.Errorf("test receiver: %w", err)
	}
	return ReceiverTestResult{OK: out.Status == "success", Error: out.Error}, nil
}

// TestStoredReceiver tests an already-saved receiver by replaying its stored
// integration verbatim. The integration carries a uid + secureFields, so Grafana
// reuses the stored secret values instead of whatever a read returned — necessary
// because reads redact secrets (the Slack webhook url, a webhook bearer token),
// and sending those redacted placeholders back makes delivery fail (e.g. an empty
// url surfaces as "unsupported protocol scheme").
func (g *Grafana) TestStoredReceiver(ctx context.Context, receiverName string) (ReceiverTestResult, error) {
	base := "/apis/notifications.alerting.grafana.app/v1beta1/namespaces/" + grafanaAlertingNamespace +
		"/receivers/" + base64.RawURLEncoding.EncodeToString([]byte(receiverName))

	var receiver struct {
		Spec struct {
			Integrations []json.RawMessage `json:"integrations"`
		} `json:"spec"`
	}
	if err := g.do(ctx, http.MethodGet, base, nil, &receiver); err != nil {
		return ReceiverTestResult{}, fmt.Errorf("load receiver: %w", err)
	}
	if len(receiver.Spec.Integrations) == 0 {
		return ReceiverTestResult{}, fmt.Errorf("receiver %q has no integration to test", receiverName)
	}

	body := map[string]any{"integration": receiver.Spec.Integrations[0]}
	var out struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := g.do(ctx, http.MethodPost, base+"/test", body, &out); err != nil {
		return ReceiverTestResult{}, fmt.Errorf("test receiver: %w", err)
	}
	return ReceiverTestResult{OK: out.Status == "success", Error: out.Error}, nil
}

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
		// Only surface a JSON body after redaction; non-JSON may carry unscrubbable secrets, so use status text.
		msg := http.StatusText(resp.StatusCode)
		if json.Valid(respBody) {
			if red := strings.TrimSpace(redactSecrets(respBody)); red != "" {
				msg = red
			}
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

// "url" is a secret: webhook URLs embed capability tokens.
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

func redactSecrets(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return fmt.Sprintf("<non-JSON response body omitted, %d bytes>", len(body))
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
				// Keep empty strings as-is so the log shows whether a secret was present at all.
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
	case string:
		// Scrub secrets a server may echo inside a generic string field, not just under a known key.
		return scrubSecretSubstrings(t)
	}
	return v
}

var (
	urlValuePattern    = regexp.MustCompile(`https?://[^\s"']+`)
	bearerValuePattern = regexp.MustCompile(`(?i)bearer\s+[^\s"']+`)
)

func scrubSecretSubstrings(s string) string {
	s = urlValuePattern.ReplaceAllString(s, "[REDACTED-URL]")
	s = bearerValuePattern.ReplaceAllString(s, "Bearer [REDACTED]")
	return s
}

// Pre-marshalled body so do() can log it on errors without re-marshalling.
func (g *Grafana) requestWithBytes(ctx context.Context, method, path string, bodyBytes []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	return g.send(ctx, method, path, bodyReader, bodyBytes != nil)
}

func (g *Grafana) send(ctx context.Context, method, path string, bodyReader io.Reader, hasBody bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	} else if g.user != "" {
		req.SetBasicAuth(g.user, g.password)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	// The provisioning API requires this header on writes to disable the file-provenance lock.
	if method != http.MethodGet {
		req.Header.Set("X-Disable-Provenance", "true")
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: %w", method, path, err)
	}
	return resp, nil
}

type GrafanaError struct {
	StatusCode int
	Message    string
}

func (e *GrafanaError) Error() string {
	return fmt.Sprintf("grafana %d: %s", e.StatusCode, e.Message)
}

func IsNotFound(err error) bool {
	var ge *GrafanaError
	if errors.As(err, &ge) {
		return ge.StatusCode == http.StatusNotFound
	}
	return false
}
