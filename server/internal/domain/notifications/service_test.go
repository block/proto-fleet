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
		{"device uuid and mac ids", SilenceScope{Kind: SilenceScopeDevice, DeviceIDs: []string{
			"550e8400-e29b-41d4-a716-446655440000", "aa:bb:cc:dd:ee:ff", "SN.001",
		}}, false},
		{"device id regex wildcard rejected", SilenceScope{Kind: SilenceScopeDevice, DeviceIDs: []string{".*"}}, true},
		{"device id regex alternation rejected", SilenceScope{Kind: SilenceScopeDevice, DeviceIDs: []string{"a|b"}}, true},
		{"device id with anchors rejected", SilenceScope{Kind: SilenceScopeDevice, DeviceIDs: []string{"^d1$"}}, true},
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

// Multi-device silences compile to an anchored, escaped regex matcher
// and the matcher round-trips back to the plain id list on reads.
func TestDeviceScopeRegexCompilation(t *testing.T) {
	sil := Silence{Scope: SilenceScope{
		Kind:      SilenceScopeDevice,
		DeviceIDs: []string{"dev-1", "SN.001"},
	}}
	gs := domainSilenceToGrafana(7, sil)

	var matcher *GrafanaSilenceMatcher
	for i, m := range gs.Matchers {
		if m.Name == "device_id" {
			matcher = &gs.Matchers[i]
		}
	}
	require.NotNil(t, matcher)
	assert.True(t, matcher.IsRegex)
	assert.Equal(t, `^(?:dev-1|SN\.001)$`, matcher.Value)

	scope := matchersToScope(gs.Matchers)
	assert.Equal(t, SilenceScopeDevice, scope.Kind)
	assert.Equal(t, []string{"dev-1", "SN.001"}, scope.DeviceIDs)
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
			// DNS failures fail closed: a host we can't classify is
			// rejected. .invalid never resolves (RFC 6761).
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
		// Ordinary edit: same destination, and the UI never has the
		// secret to echo back.
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

// Changing the destination without supplying a fresh secret must drop
// the stored one — carrying it would deliver the old credential to
// whatever new destination the caller pointed the channel at.
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

// Testing a saved channel must probe the stored full webhook URL, not
// the host-only URL the UI echoes back from a redacted read.
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

	// UI sends the saved channel with the redacted host-only URL.
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

// Testing a saved channel the caller's org doesn't own is rejected
// before any outbound test fires.
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

// Reads expose webhook URLs host-only — the path/query where tokens
// live must never reach a notification:read holder.
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

// A rename echoes back the host-only URL the read returned; the stored
// full URL must be carried through rather than truncated.
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
		ID:   "cp-1",
		Name: "renamed-pager",
		Kind: ChannelKindWebhook,
		// UI echoes back the redacted host-only URL on a rename.
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

// A Slack channel read must expose only that a URL exists — the URL
// is the secret (it embeds the capability token).
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

// A rename without a fresh URL must carry Grafana's "[REDACTED]"
// placeholder through so the stored secure URL survives the PUT.
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
		ID:   "cp-3",
		Name: "renamed-slack",
		Kind: ChannelKindSlack,
		// Ordinary edit: reads never return the URL to echo back.
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

// A fresh Slack URL replaces the stored one outright.
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
