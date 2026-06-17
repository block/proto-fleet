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
			name:    "webhook cgnat range rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "https://100.64.0.1/hook"}},
			wantErr: true,
		},
		{
			name:    "webhook benchmarking range rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "https://198.18.0.1/hook"}},
			wantErr: true,
		},
		{
			name:    "webhook localhost rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "http://localhost:9000/hook"}},
			wantErr: true,
		},
		{
			// .invalid never resolves (RFC 6761); unclassifiable hosts fail closed.
			name:    "webhook unresolvable host rejected",
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "https://definitely-not-real.invalid/hook"}},
			wantErr: true,
		},
		{
			name:    "webhook loopback allowed when policy opts in",
			policy:  DestinationPolicy{AllowPrivateDestinations: true},
			channel: Channel{Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "http://127.0.0.1:9000/hook"}},
		},
		{
			name:    "slack public ip allowed",
			channel: Channel{Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: "https://203.0.113.10/services/T00/B00/XXX"}},
		},
		{
			name:    "slack missing url",
			channel: Channel{Kind: ChannelKindSlack, Slack: &SlackConfig{}},
			wantErr: true,
		},
		{
			name:    "slack nil config",
			channel: Channel{Kind: ChannelKindSlack},
			wantErr: true,
		},
		{
			name:    "slack bad scheme",
			channel: Channel{Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: "ftp://203.0.113.10/services/x"}},
			wantErr: true,
		},
		{
			name:    "slack loopback rejected",
			channel: Channel{Kind: ChannelKindSlack, Slack: &SlackConfig{WebhookURL: "https://127.0.0.1/services/x"}},
			wantErr: true,
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
		ID:      "cp-1",
		Name:    "pager",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://hooks.example.com/old"},
	})
	require.NoError(t, err)
	assert.True(t, updated.HasSecret)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(putBody, &sent))
	assert.Equal(t, "[REDACTED]", sent.Settings["authorization_credentials"],
		"update without a new secret must carry the redacted placeholder so Grafana keeps the stored credential")
	assert.Equal(t, "https://hooks.example.com/old", sent.Settings["url"])
}

func TestUpdateChannelDropsSecretOnDestinationChange(t *testing.T) {
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
		Webhook: &WebhookConfig{URL: "https://hooks.example.com/new"},
	})
	require.NoError(t, err)
	assert.False(t, updated.HasSecret)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(putBody, &sent))
	assert.Empty(t, sent.Settings["authorization_credentials"],
		"a destination change without a fresh secret must wipe the stored credential, not carry the placeholder")
	assert.Equal(t, "https://hooks.example.com/new", sent.Settings["url"])
}

func TestTestChannelUsesStoredDestinationForSavedChannel(t *testing.T) {
	const fullURL = "https://hooks.slack.com/services/T1/B2/SECRET"
	listed := []GrafanaContactPoint{{
		UID:      "cp-1",
		Name:     "org-7-pager",
		Type:     "webhook",
		Settings: json.RawMessage(`{"url": "` + fullURL + `", "authorization_credentials": "[REDACTED]"}`),
	}}

	var testedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/contact-points", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(listed))
	})
	mux.HandleFunc("POST /api/v1/provisioning/contact-points/test", func(w http.ResponseWriter, r *http.Request) {
		testedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), DestinationPolicy{})

	ok, _, _, err := svc.TestChannel(context.Background(), 7, Channel{
		ID:      "cp-1",
		Name:    "pager",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://hooks.slack.com"},
	})
	require.NoError(t, err)
	assert.True(t, ok)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(testedBody, &sent))
	assert.Equal(t, fullURL, sent.Settings["url"], "saved-channel test must use the stored full URL, not the redacted host")
}

func TestTestChannelRejectsForeignSavedChannel(t *testing.T) {
	listed := []GrafanaContactPoint{{
		UID:      "cp-1",
		Name:     "org-8-pager",
		Type:     "webhook",
		Settings: json.RawMessage(`{"url": "https://hooks.example.com/x"}`),
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/contact-points", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(listed))
	})
	mux.HandleFunc("POST /api/v1/provisioning/contact-points/test", func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("test endpoint must not be called for a foreign channel")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), DestinationPolicy{})

	_, _, _, err := svc.TestChannel(context.Background(), 7, Channel{
		ID: "cp-1", Name: "pager", Kind: ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://hooks.example.com"},
	})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRedactWebhookURL(t *testing.T) {
	cases := map[string]string{
		"https://hooks.slack.com/services/T00/B00/XXXSECRETXXX": "https://hooks.slack.com",
		"https://events.pagerduty.com/x?token=abc":              "https://events.pagerduty.com",
		"http://relay.example.com:8443/path":                    "http://relay.example.com:8443",
		"https://user:pass@relay.example.com/h":                 "https://relay.example.com",
		"":                                                      "",
		"not a url":                                             "",
		"://bad":                                                "",
	}
	for in, want := range cases {
		assert.Equalf(t, want, redactWebhookURL(in), "redactWebhookURL(%q)", in)
	}
}

func TestListChannelsRedactsWebhookURL(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:      "cp-1",
		Name:     "org-7-pager",
		Type:     "webhook",
		Settings: json.RawMessage(`{"url": "https://hooks.slack.com/services/T1/B2/SECRET"}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{})

	channels, err := svc.ListChannels(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, "https://hooks.slack.com", channels[0].Webhook.URL)
}

func TestUpdateChannelPreservesWebhookURLOnRename(t *testing.T) {
	const fullURL = "https://hooks.slack.com/services/T1/B2/SECRET"
	existing := []GrafanaContactPoint{{
		UID:      "cp-1",
		Name:     "org-7-pager",
		Type:     "webhook",
		Settings: json.RawMessage(`{"url": "` + fullURL + `", "authorization_credentials": "[REDACTED]"}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{})

	_, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID:      "cp-1",
		Name:    "renamed-pager",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://hooks.slack.com"},
	})
	require.NoError(t, err)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(putBody, &sent))
	assert.Equal(t, fullURL, sent.Settings["url"], "rename must keep the stored full URL, not the redacted host")
}

func TestUpdateChannelPreservesSMTPSecret(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:  "cp-2",
		Name: "org-7-oncall-mail",
		Type: "email",
		Settings: json.RawMessage(`{
			"addresses": "oncall@example.com",
			"smtpHost": "smtp.example.com",
			"smtpPort": 587,
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

func TestListChannelsHidesSlackURL(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:      "cp-3",
		Name:     "org-7-oncall-slack",
		Type:     "slack",
		Settings: json.RawMessage(`{"url": "[REDACTED]"}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{})

	channels, err := svc.ListChannels(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, ChannelKindSlack, channels[0].Kind)
	require.NotNil(t, channels[0].Slack)
	assert.Empty(t, channels[0].Slack.WebhookURL)
	assert.True(t, channels[0].HasSecret)
}

func TestUpdateChannelPreservesSlackURLOnRename(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:      "cp-3",
		Name:     "org-7-oncall-slack",
		Type:     "slack",
		Settings: json.RawMessage(`{"url": "[REDACTED]"}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{})

	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID:    "cp-3",
		Name:  "renamed-slack",
		Kind:  ChannelKindSlack,
		Slack: &SlackConfig{},
	})
	require.NoError(t, err)
	assert.True(t, updated.HasSecret)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(putBody, &sent))
	assert.Equal(t, "[REDACTED]", sent.Settings["url"],
		"rename must carry the redacted placeholder so Grafana keeps the stored URL")
}

func TestUpdateChannelReplacesSlackURL(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:      "cp-3",
		Name:     "org-7-oncall-slack",
		Type:     "slack",
		Settings: json.RawMessage(`{"url": "[REDACTED]"}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{AllowPrivateDestinations: true})

	updated, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID:    "cp-3",
		Name:  "oncall-slack",
		Kind:  ChannelKindSlack,
		Slack: &SlackConfig{WebhookURL: "https://hooks.example.com/services/T1/B2/fresh"},
	})
	require.NoError(t, err)
	assert.True(t, updated.HasSecret)

	var sent struct {
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal(putBody, &sent))
	assert.Equal(t, "https://hooks.example.com/services/T1/B2/fresh", sent.Settings["url"])
}

func TestUpdateChannelChangingToWebhookRequiresFreshURL(t *testing.T) {
	existing := []GrafanaContactPoint{{
		UID:      "cp-3",
		Name:     "org-7-oncall-slack",
		Type:     "slack",
		Settings: json.RawMessage(`{"url": "https://hooks.slack.com/services/secret"}`),
	}}
	var putBody []byte
	svc := NewService(fakeGrafana(t, existing, &putBody), DestinationPolicy{AllowPrivateDestinations: true})

	_, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID:      "cp-3",
		Name:    "oncall-webhook",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{},
	})
	require.Error(t, err, "changing kind to webhook with no URL must not reuse the stored Slack URL")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Nil(t, putBody, "no contact point should be written")
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
