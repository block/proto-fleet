package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestValidateSilenceScope(t *testing.T) {
	cases := []struct {
		name    string
		scope   SilenceScope
		wantErr bool
	}{
		{"rule with target", SilenceScope{Kind: SilenceScopeRule, RuleID: "r1"}, false},
		{"rule without target", SilenceScope{Kind: SilenceScopeRule}, true},
		{"group with target", SilenceScope{Kind: SilenceScopeGroup, GroupID: "g1"}, false},
		{"group without target", SilenceScope{Kind: SilenceScopeGroup}, true},
		{"site with target", SilenceScope{Kind: SilenceScopeSite, SiteID: "s1"}, false},
		{"site without target", SilenceScope{Kind: SilenceScopeSite}, true},
		{"device with targets", SilenceScope{Kind: SilenceScopeDevice, DeviceIDs: []string{"d1"}}, false},
		{"device without targets", SilenceScope{Kind: SilenceScopeDevice}, true},
		{"unknown kind", SilenceScope{Kind: "everything"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSilenceScope(tc.scope)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, fleeterror.IsInvalidArgumentError(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// CreateSilence must reject a targetless scope before anything reaches
// Grafana — an untargeted scope would compile to an org-wide silence.
func TestCreateSilenceRejectsTargetlessScope(t *testing.T) {
	svc := NewService(nil, DestinationPolicy{})
	_, err := svc.CreateSilence(context.Background(), 7, Silence{
		Scope: SilenceScope{Kind: SilenceScopeGroup},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestValidateDestination(t *testing.T) {
	cases := []struct {
		name    string
		policy  DestinationPolicy
		channel Channel
		wantErr bool
	}{
		{
			name:    "webhook public ip allowed",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "https://203.0.113.10/hook"}},
		},
		{
			name:    "webhook missing url",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{}},
			wantErr: true,
		},
		{
			name:    "webhook nil config",
			channel: Channel{Kind: ChannelKindWebhook},
			wantErr: true,
		},
		{
			name:    "webhook bad scheme",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "ftp://203.0.113.10/hook"}},
			wantErr: true,
		},
		{
			name:    "webhook loopback rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "http://127.0.0.1:9000/hook"}},
			wantErr: true,
		},
		{
			name:    "webhook ipv6 loopback rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "http://[::1]:9000/hook"}},
			wantErr: true,
		},
		{
			name:    "webhook private range rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "https://10.1.2.3/hook"}},
			wantErr: true,
		},
		{
			name:    "webhook metadata endpoint rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "http://169.254.169.254/latest/meta-data/"}},
			wantErr: true,
		},
		{
			name:    "webhook localhost rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "http://localhost:9000/hook"}},
			wantErr: true,
		},
		{
			name:    "webhook loopback allowed when policy opts in",
			policy:  DestinationPolicy{AllowPrivateDestinations: true},
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "http://127.0.0.1:9000/hook"}},
		},
		{
			name: "smtp public ip allowed",
			channel: Channel{Kind: ChannelKindSMTP, SMTP: &SMTPConfig{
				Host: "203.0.113.10", To: []string{"oncall@example.com"},
			}},
		},
		{
			name:    "smtp missing host",
			channel: Channel{Kind: ChannelKindSMTP, SMTP: &SMTPConfig{To: []string{"oncall@example.com"}}},
			wantErr: true,
		},
		{
			name:    "smtp missing recipients",
			channel: Channel{Kind: ChannelKindSMTP, SMTP: &SMTPConfig{Host: "203.0.113.10"}},
			wantErr: true,
		},
		{
			name:    "smtp private host rejected",
			channel: Channel{Kind: ChannelKindSMTP, SMTP: &SMTPConfig{Host: "192.168.1.5", To: []string{"oncall@example.com"}}},
			wantErr: true,
		},
		{
			name:   "smtp private host allowed when policy opts in",
			policy: DestinationPolicy{AllowPrivateDestinations: true},
			channel: Channel{Kind: ChannelKindSMTP, SMTP: &SMTPConfig{
				Host: "192.168.1.5", To: []string{"oncall@example.com"},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(nil, tc.policy)
			err := svc.validateDestination(context.Background(), &tc.channel)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, fleeterror.IsInvalidArgumentError(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// fakeGrafana stands in for the sidecar: serves a fixed contact-point
// list and captures the body of any PUT so tests can assert what
// fleet-api would have written.
func fakeGrafana(t *testing.T, listed []GrafanaContactPoint, putBody *[]byte) *Grafana {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/contact-points", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(listed))
	})
	mux.HandleFunc("PUT /api/v1/provisioning/contact-points/{uid}", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		*putBody = b
		w.Header().Set("Content-Type", "application/json")
		var cp GrafanaContactPoint
		require.NoError(t, json.Unmarshal(b, &cp))
		require.NoError(t, json.NewEncoder(w).Encode(cp))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewGrafana(GrafanaConfig{URL: srv.URL})
}

// An update without a fresh secret must carry the stored secret field
// into the PUT payload instead of overwriting it with an empty value.
// For webhooks Grafana redacts the secure field on reads, so the
// carried value is the "[REDACTED]" placeholder Grafana resolves back
// to the stored credential.
func TestUpdateChannelPreservesWebhookSecret(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:  "cp-1",
		Name: "org-7-pager",
		Type: "webhook",
		Settings: json.RawMessage(`{
			"url": "https://hooks.example.com/old",
			"authorization_scheme": "Bearer",
			"authorization_credentials": "[REDACTED]"
		}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{AllowPrivateDestinations: true})

	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID:   "cp-1",
		Name: "pager",
		Kind: ChannelKindWebhook,
		// Ordinary edit: the UI never has the secret to echo back.
		Webhook: &WebhookConfig{URL: "https://hooks.example.com/new"},
	})
	require.NoError(t, err)
	assert.True(t, updated.HasSecret)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(putBody, &sent))
	assert.Equal(t, "[REDACTED]", sent.Settings["authorization_credentials"],
		"update without a new secret must carry the redacted placeholder so Grafana keeps the stored credential")
	assert.Equal(t, "https://hooks.example.com/new", sent.Settings["url"])
}

func TestUpdateChannelPreservesSMTPSecret(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:  "cp-2",
		Name: "org-7-oncall-mail",
		Type: "email",
		Settings: json.RawMessage(`{
			"addresses": "oncall@example.com",
			"smtpHost": "smtp.example.com",
			"smtpPassword": "hunter2"
		}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{AllowPrivateDestinations: true})

	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID:   "cp-2",
		Name: "oncall-mail",
		Kind: ChannelKindSMTP,
		SMTP: &SMTPConfig{Host: "smtp.example.com", Port: 587, To: []string{"oncall@example.com"}},
	})
	require.NoError(t, err)
	assert.True(t, updated.HasSecret)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(putBody, &sent))
	assert.Equal(t, "hunter2", sent.Settings["smtpPassword"],
		"update without a new password must carry the stored one")
}

func TestUpdateChannelReplacesSecretWhenProvided(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:  "cp-1",
		Name: "org-7-pager",
		Type: "webhook",
		Settings: json.RawMessage(`{
			"url": "https://hooks.example.com/old",
			"authorization_scheme": "Bearer",
			"authorization_credentials": "[REDACTED]"
		}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{AllowPrivateDestinations: true})

	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID:      "cp-1",
		Name:    "pager",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://hooks.example.com/old", BearerHeader: "fresh-token"},
	})
	require.NoError(t, err)
	assert.True(t, updated.HasSecret)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(putBody, &sent))
	assert.Equal(t, "fresh-token", sent.Settings["authorization_credentials"])
}

// Cross-org updates must 404 before any write reaches Grafana.
func TestUpdateChannelRejectsForeignOrg(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:      "cp-1",
		Name:     "org-8-pager",
		Type:     "webhook",
		Settings: json.RawMessage(`{"url": "https://hooks.example.com/hook"}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{AllowPrivateDestinations: true})

	_, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID:      "cp-1",
		Name:    "pager",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://hooks.example.com/hook"},
	})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Nil(t, putBody, "no PUT should reach Grafana for a foreign org's channel")
}
