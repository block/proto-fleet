package alerts

import (
	"bytes"
	"context"
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

func (g *Grafana) GetAlertRule(ctx context.Context, uid string) (*GrafanaAlertRule, error) {
	var out GrafanaAlertRule
	if err := g.do(ctx, http.MethodGet, "/api/v1/provisioning/alert-rules/"+uid, nil, &out); err != nil {
		return nil, fmt.Errorf("get alert rule: %w", err)
	}
	return &out, nil
}

// Rule writes apply only to API-created rules: YAML-provisioned rules stay locked
// (Grafana 11.6+ blocks in-place edits of file-provenance rules; X-Disable-Provenance frees ours).
func (g *Grafana) CreateAlertRule(ctx context.Context, rule GrafanaAlertRule) (*GrafanaAlertRule, error) {
	var out GrafanaAlertRule
	if err := g.do(ctx, http.MethodPost, "/api/v1/provisioning/alert-rules", rule, &out); err != nil {
		return nil, fmt.Errorf("create alert rule: %w", err)
	}
	return &out, nil
}

func (g *Grafana) UpdateAlertRule(ctx context.Context, rule GrafanaAlertRule) (*GrafanaAlertRule, error) {
	var out GrafanaAlertRule
	if err := g.do(ctx, http.MethodPut, "/api/v1/provisioning/alert-rules/"+rule.UID, rule, &out); err != nil {
		return nil, fmt.Errorf("update alert rule: %w", err)
	}
	return &out, nil
}

func (g *Grafana) DeleteAlertRule(ctx context.Context, uid string) error {
	if err := g.do(ctx, http.MethodDelete, "/api/v1/provisioning/alert-rules/"+uid, nil, nil); err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	return nil
}

type GrafanaFolder struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
}

// EnsureFolder creates the folder if missing; a concurrent-create conflict resolves to the existing folder.
func (g *Grafana) EnsureFolder(ctx context.Context, uid, title string) error {
	var got GrafanaFolder
	err := g.do(ctx, http.MethodGet, "/api/folders/"+uid, nil, &got)
	if err == nil {
		return nil
	}
	if !IsNotFound(err) {
		return fmt.Errorf("get folder: %w", err)
	}
	createErr := g.do(ctx, http.MethodPost, "/api/folders", GrafanaFolder{UID: uid, Title: title}, &got)
	if createErr == nil || isConflict(createErr) {
		return nil
	}
	return fmt.Errorf("create folder: %w", createErr)
}

type GrafanaRuleGroup struct {
	Title     string             `json:"title"`
	FolderUID string             `json:"folderUid"`
	Interval  int64              `json:"interval"`
	Rules     []GrafanaAlertRule `json:"rules"`
}

// SetRuleGroup replaces the group's whole definition (Grafana's PUT semantics),
// so callers must supply the group's full rule list — trivial for user rules,
// which are one-per-group.
func (g *Grafana) SetRuleGroup(ctx context.Context, group GrafanaRuleGroup) error {
	path := "/api/v1/provisioning/folder/" + group.FolderUID + "/rule-groups/" + group.Title
	if err := g.do(ctx, http.MethodPut, path, group, nil); err != nil {
		return fmt.Errorf("set rule group: %w", err)
	}
	return nil
}

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

// The Alertmanager API takes the silence id in the body, not the URL.
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
			"alerts.grafana_error",
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

func isConflict(err error) bool {
	var ge *GrafanaError
	if errors.As(err, &ge) {
		return ge.StatusCode == http.StatusConflict || ge.StatusCode == http.StatusPreconditionFailed
	}
	return false
}
