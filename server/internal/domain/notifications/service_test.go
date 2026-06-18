package notifications

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestValidateMaintenanceWindowScope(t *testing.T) {
	cases := []struct {
		name    string
		scope   MaintenanceWindowScope
		wantErr bool
	}{
		{"rule with target", MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule, RuleID: "r1"}, false},
		{"rule without target", MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule}, true},
		{"group with target", MaintenanceWindowScope{Kind: MaintenanceWindowScopeGroup, GroupID: "g1"}, false},
		{"group without target", MaintenanceWindowScope{Kind: MaintenanceWindowScopeGroup}, true},
		{"site with target", MaintenanceWindowScope{Kind: MaintenanceWindowScopeSite, SiteID: "s1"}, false},
		{"site without target", MaintenanceWindowScope{Kind: MaintenanceWindowScopeSite}, true},
		{"device with targets", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{"d1"}}, false},
		{"device without targets", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice}, true},
		{"device uuid and mac ids", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{
			"550e8400-e29b-41d4-a716-446655440000", "aa:bb:cc:dd:ee:ff", "SN.001",
		}}, false},
		{"device id regex wildcard rejected", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{".*"}}, true},
		{"device id regex alternation rejected", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{"a|b"}}, true},
		{"device id with anchors rejected", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{"^d1$"}}, true},
		{"unknown kind", MaintenanceWindowScope{Kind: "everything"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMaintenanceWindowScope(tc.scope)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, fleeterror.IsInvalidArgumentError(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCreateMaintenanceWindowRejectsTargetlessScope(t *testing.T) {
	svc := NewService(nil, DestinationPolicy{})
	_, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		Scope: MaintenanceWindowScope{Kind: MaintenanceWindowScopeGroup},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestDeviceScopeRegexCompilation(t *testing.T) {
	sil := MaintenanceWindow{Scope: MaintenanceWindowScope{
		Kind:      MaintenanceWindowScopeDevice,
		DeviceIDs: []string{"dev-1", "SN.001"},
	}}
	gs := maintenanceWindowToGrafanaSilence(7, sil)

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
	assert.Equal(t, MaintenanceWindowScopeDevice, scope.Kind)
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

func TestCreateChannelRejectsDuplicateName(t *testing.T) {
	listed := []GrafanaContactPoint{{
		UID:      "cp-1",
		Name:     "org-7-pager",
		Type:     "webhook",
		Settings: json.RawMessage(`{"url": "https://hooks.example.com/x"}`),
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/contact-points", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(listed))
	})
	mux.HandleFunc("POST /api/v1/provisioning/contact-points", func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("must not create a contact point when the name already exists")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), DestinationPolicy{AllowPrivateDestinations: true})

	_, err := svc.CreateChannel(context.Background(), 7, Channel{
		Name:    "pager",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://hooks.example.com/y"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "duplicate channel name must be rejected as already-exists")
}

func TestCreateChannelAllowsDuplicateNameInDifferentOrg(t *testing.T) {
	// org 8 owns "pager"; org 7 creating "pager" is a different org-prefixed name.
	listed := []GrafanaContactPoint{{UID: "cp-1", Name: "org-8-pager", Type: "webhook", Settings: json.RawMessage(`{"url":"https://x"}`)}}
	var created bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/contact-points", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(listed))
	})
	mux.HandleFunc("POST /api/v1/provisioning/contact-points", func(w http.ResponseWriter, r *http.Request) {
		created = true
		var cp GrafanaContactPoint
		require.NoError(t, json.NewDecoder(r.Body).Decode(&cp))
		assert.Equal(t, "org-7-pager", cp.Name)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(cp))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), DestinationPolicy{AllowPrivateDestinations: true})

	_, err := svc.CreateChannel(context.Background(), 7, Channel{
		Name:    "pager",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "https://hooks.example.com/y"},
	})
	require.NoError(t, err)
	assert.True(t, created, "a name owned only by another org must not block creation")
}

func TestReceiverTestErrorScrubsDestinationURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /apis/notifications.alerting.grafana.app/v1beta1/namespaces/default/receivers/{name}/test",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Grafana can echo the outbound URL in the error string.
			_, _ = w.Write([]byte(`{"status":"failure","error":"Post \"https://hooks.slack.com/services/T1/B2/SECRET\": i/o timeout"}`))
		})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	g := NewGrafana(GrafanaConfig{URL: srv.URL})

	res, err := g.TestReceiverIntegration(context.Background(), "org-1-x", "slack", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.NotContains(t, res.Error, "hooks.slack.com", "the destination URL must be scrubbed from the test error")
	assert.Contains(t, res.Error, "[REDACTED-URL]")
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

func TestUpdateChannelRejectsRenameToExistingName(t *testing.T) {
	listed := []GrafanaContactPoint{
		{UID: "cp-1", Name: "org-7-pager", Type: "webhook", Settings: json.RawMessage(`{"url":"https://a.example.com"}`)},
		{UID: "cp-2", Name: "org-7-oncall", Type: "webhook", Settings: json.RawMessage(`{"url":"https://b.example.com"}`)},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/contact-points", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(listed))
	})
	mux.HandleFunc("PUT /api/v1/provisioning/contact-points/{uid}", func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("must not update when the renamed channel collides with another")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), DestinationPolicy{AllowPrivateDestinations: true})

	// Rename "oncall" (cp-2) to "pager", already owned by cp-1.
	_, err := svc.UpdateChannel(context.Background(), 7, Channel{
		ID: "cp-2", Name: "pager", Kind: ChannelKindWebhook, Webhook: &WebhookConfig{URL: "https://b.example.com"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "renaming onto another channel's name must be rejected")
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

func TestTestChannelReplaysStoredIntegrationForSavedChannel(t *testing.T) {
	// A read redacts the Slack url secret, so the saved-channel test can't rebuild
	// the body from it — it must replay the stored integration (uid + secureFields)
	// so Grafana reuses the secret. Sending the redacted value back fails delivery.
	// Name chosen so std base64 yields a '/' ("b3JnLTctYWE/") — URL-safe encoding
	// (which Grafana uses) must be applied so the receiver stays one path segment.
	const grafanaName = "org-7-aa?"
	// The contact-point uid the caller owns equals the integration uid.
	listed := []GrafanaContactPoint{{
		UID:      "int-1",
		Name:     grafanaName,
		Type:     "slack",
		Settings: json.RawMessage(`{"url": "[REDACTED]"}`),
	}}
	// Two integrations share this receiver (same display name); the requested one
	// ("int-1") is not first, so testing Integrations[0] would hit the wrong target.
	const storedIntegrations = `{"type":"webhook","version":"v1","uid":"int-2","settings":{"url":"http://other.example.com"},"secureFields":{}},` +
		`{"type":"slack","version":"v1","uid":"int-1","settings":{},"secureFields":{"url":true}}`
	name := base64.RawURLEncoding.EncodeToString([]byte(grafanaName))
	require.NotContains(t, name, "/", "receiver path segment must be URL-safe")

	var testedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/contact-points", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(listed))
	})
	mux.HandleFunc("GET /apis/notifications.alerting.grafana.app/v1beta1/namespaces/default/receivers/{name}",
		func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, name, r.PathValue("name"), "saved-channel test must address the receiver by base64(name)")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"spec":{"integrations":[` + storedIntegrations + `]}}`))
		})
	mux.HandleFunc("POST /apis/notifications.alerting.grafana.app/v1beta1/namespaces/default/receivers/{name}/test",
		func(w http.ResponseWriter, r *http.Request) {
			testedBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success"}`))
		})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), DestinationPolicy{})

	ok, _, _, err := svc.TestChannel(context.Background(), 7, Channel{ID: "int-1", Name: "pager", Kind: ChannelKindSlack})
	require.NoError(t, err)
	assert.True(t, ok)

	var sent struct {
		Integration struct {
			UID          string          `json:"uid"`
			SecureFields map[string]bool `json:"secureFields"`
			Settings     map[string]any  `json:"settings"`
		} `json:"integration"`
	}
	require.NoError(t, json.Unmarshal(testedBody, &sent))
	assert.Equal(t, "int-1", sent.Integration.UID, "must test the requested integration, not the first under the receiver")
	assert.True(t, sent.Integration.SecureFields["url"], "the secret must be reused via secureFields, not sent as a redacted value")
	assert.NotContains(t, sent.Integration.Settings, "url", "a redacted url must never be sent as the destination")
}

func TestTestChannelBeforeSaveUsesTransientReceiver(t *testing.T) {
	var createdName, testedName, deletedUID string
	var testedBody []byte

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/provisioning/contact-points", func(w http.ResponseWriter, r *http.Request) {
		var cp GrafanaContactPoint
		require.NoError(t, json.NewDecoder(r.Body).Decode(&cp))
		createdName = cp.Name
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uid":"tmp-uid","name":"` + cp.Name + `"}`))
	})
	mux.HandleFunc("POST /apis/notifications.alerting.grafana.app/v1beta1/namespaces/default/receivers/{name}/test",
		func(w http.ResponseWriter, r *http.Request) {
			testedName = r.PathValue("name")
			testedBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success"}`))
		})
	mux.HandleFunc("DELETE /api/v1/provisioning/contact-points/{uid}", func(w http.ResponseWriter, r *http.Request) {
		deletedUID = r.PathValue("uid")
		w.WriteHeader(http.StatusAccepted)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// Allow the loopback destination so the SSRF pre-flight doesn't reject the test URL.
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), DestinationPolicy{AllowPrivateDestinations: true})

	ok, code, _, err := svc.TestChannel(context.Background(), 7, Channel{
		Name:    "pager",
		Kind:    ChannelKindWebhook,
		Webhook: &WebhookConfig{URL: "http://127.0.0.1/hook"},
	})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 200, code)

	assert.True(t, strings.HasPrefix(createdName, "org-7-test-"),
		"transient receiver must keep the org prefix so isolation holds, got %q", createdName)
	assert.Equal(t, base64.RawURLEncoding.EncodeToString([]byte(createdName)), testedName,
		"test must address the transient receiver by base64(name)")
	assert.Equal(t, "tmp-uid", deletedUID, "the transient receiver must be torn down after the test")

	var sent struct {
		Integration struct {
			Settings map[string]any `json:"settings"`
		} `json:"integration"`
	}
	require.NoError(t, json.Unmarshal(testedBody, &sent))
	assert.Equal(t, "http://127.0.0.1/hook", sent.Integration.Settings["url"])
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
	mux.HandleFunc("POST /apis/notifications.alerting.grafana.app/v1beta1/namespaces/default/receivers/{name}/test",
		func(_ http.ResponseWriter, _ *http.Request) {
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

func fakeGrafanaSilences(t *testing.T, listed []GrafanaSilence, postBody *[]byte) *Grafana {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/alertmanager/grafana/api/v2/silences", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(listed))
	})
	mux.HandleFunc("POST /api/alertmanager/grafana/api/v2/silences", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		*postBody = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"silenceID":"sil-1"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewGrafana(GrafanaConfig{URL: srv.URL})
}

func TestUpdateMaintenanceWindowPreservesCreator(t *testing.T) {
	existing := []GrafanaSilence{{
		ID:        "sil-1",
		CreatedBy: "alice@example.com",
		Comment:   "old",
		Matchers: []GrafanaSilenceMatcher{
			{Name: "organization_id", Value: "7", IsEqual: true},
			{Name: "__alert_rule_uid__", Value: "rule-9", IsEqual: true},
		},
	}}
	var postBody []byte
	svc := NewService(fakeGrafanaSilences(t, existing, &postBody), DestinationPolicy{})

	_, err := svc.UpdateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		ID:      "sil-1",
		Comment: "updated",
		Scope:   MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule, RuleID: "rule-9"},
	})
	require.NoError(t, err)

	var sent struct {
		CreatedBy string `json:"createdBy"`
	}
	require.NoError(t, json.Unmarshal(postBody, &sent))
	assert.Equal(t, "alice@example.com", sent.CreatedBy, "update must carry the original creator")
}

// The pause marker must never be an alert matcher: Alertmanager ANDs every matcher
// against an alert's labels, and no rule emits a marker label, so a marker matcher
// would mute nothing while the rule still showed as paused.
func TestPauseSilenceMarkerIsNotAMatcher(t *testing.T) {
	sil := buildPauseSilence(7, "rule-9", time.Unix(0, 0).UTC())
	for _, m := range sil.Matchers {
		assert.Contains(t, []string{silenceLabelOrganizationID, alertRuleUIDMatcher}, m.Name,
			"pause silence may only carry org and alert-rule-UID matchers")
	}
	assert.True(t, isPauseSilence(sil), "comment marker must identify a pause silence")
	assert.True(t, isPauseSilenceFor(sil, "7", "rule-9"))
}

// A rule-scoped maintenance window is structurally identical to a pause silence, so a
// caller must not be able to smuggle the pause marker into the comment and have the
// window hidden from the list / overlaid as a paused rule.
func TestMaintenanceWindowRejectsPauseMarkerComment(t *testing.T) {
	var postBody []byte
	svc := NewService(fakeGrafanaSilences(t, nil, &postBody), DestinationPolicy{})

	_, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		Comment: pauseSilenceCommentMarker + " sneaky",
		Scope:   MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule, RuleID: "rule-9"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "want InvalidArgument, got %v", err)
	assert.Nil(t, postBody, "rejected window must not reach Grafana")
}

func TestRuleVisibleToOrg(t *testing.T) {
	const want = "7"
	cases := []struct {
		name    string
		labels  map[string]string
		visible bool
	}{
		{"no labels fails closed", nil, false},
		{"unlabeled tenant rule is hidden", map[string]string{"severity": "warning"}, false},
		{"explicit global marker visible to all", map[string]string{ruleLabelScope: ruleScopeGlobal}, true},
		{"matching org label visible", map[string]string{ruleLabelOrganizationID: "7"}, true},
		{"other org label hidden", map[string]string{ruleLabelOrganizationID: "9"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.visible, ruleVisibleToOrg(GrafanaAlertRule{Labels: tc.labels}, want))
		})
	}
}
