package alerts

import (
	"context"
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
		// Group and site scopes emit a matcher no provisioned alert carries, so they're
		// rejected until the alert queries label instances with group_id/site_id.
		{"group not yet supported", MaintenanceWindowScope{Kind: MaintenanceWindowScopeGroup, GroupID: "g1"}, true},
		{"site not yet supported", MaintenanceWindowScope{Kind: MaintenanceWindowScopeSite, SiteID: "s1"}, true},
		{"device with targets", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{"d1"}}, false},
		{"device without targets", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice}, true},
		{"device uuid and mac ids", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{
			"550e8400-e29b-41d4-a716-446655440000", "aa:bb:cc:dd:ee:ff", "SN.001",
		}}, false},
		{"device id regex wildcard rejected", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{".*"}}, true},
		{"device id regex alternation rejected", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{"a|b"}}, true},
		{"device id with anchors rejected", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{"^d1$"}}, true},
		{"device id at max length allowed", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{strings.Repeat("d", maxDeviceIDLength)}}, false},
		{"device id over max length rejected", MaintenanceWindowScope{Kind: MaintenanceWindowScopeDevice, DeviceIDs: []string{strings.Repeat("d", maxDeviceIDLength+1)}}, true},
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
	svc := NewService(nil, nil, nil, nil, DestinationPolicy{})
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
			svc := NewService(nil, nil, nil, nil, tc.policy)
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

func TestValidateChannelNameRejectsTransientPattern(t *testing.T) {
	require.NoError(t, validateChannelName("ops"))
	require.NoError(t, validateChannelName("test-pager"), "a test-* name that isn't a transient UUID is user-allowed")

	err := validateChannelName("test-550e8400-e29b-41d4-a716-446655440000")
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
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

func fakeGrafanaSilences(t *testing.T, listed []GrafanaSilence, postBody *[]byte) *Grafana {
	t.Helper()
	mux := http.NewServeMux()
	// rule-9 is a shared default (visible to every org) so the rule-scoped maintenance-window/
	// pause paths, which resolve the target through requireRule, find it.
	mux.HandleFunc("GET /api/v1/provisioning/alert-rules", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode([]GrafanaAlertRule{
			{UID: "rule-9", Title: "Rule 9", Labels: map[string]string{ruleLabelScope: ruleScopeShared}},
		}))
	})
	// The post-write target recheck resolves the rule by uid.
	mux.HandleFunc("GET /api/v1/provisioning/alert-rules/{uid}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("uid") != "rule-9" {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(GrafanaAlertRule{
			UID: "rule-9", Title: "Rule 9", Labels: map[string]string{ruleLabelScope: ruleScopeShared},
		}))
	})
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
		Comment:   maintenanceWindowCommentMarker + " old",
		Matchers: []GrafanaSilenceMatcher{
			{Name: "organization_id", Value: "7", IsEqual: true},
			{Name: "__alert_rule_uid__", Value: "rule-9", IsEqual: true},
		},
	}}
	var postBody []byte
	svc := NewService(fakeGrafanaSilences(t, existing, &postBody), nil, nil, nil, DestinationPolicy{})

	_, err := svc.UpdateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		ID:       "sil-1",
		Comment:  "updated",
		Scope:    MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule, RuleID: "rule-9"},
		StartsAt: time.Unix(1000, 0),
		EndsAt:   time.Unix(2000, 0),
	})
	require.NoError(t, err)

	var sent struct {
		CreatedBy string `json:"createdBy"`
	}
	require.NoError(t, json.Unmarshal(postBody, &sent))
	assert.Equal(t, "alice@example.com", sent.CreatedBy, "update must carry the original creator")
}

// Ownership is proven by the Proto Fleet provenance marker, not the org matcher alone, so an
// operator-created Grafana silence that merely shares the org matcher is invisible and
// un-mutable through these RPCs.
func TestListMaintenanceWindowsIgnoresUnmarkedSilences(t *testing.T) {
	listed := []GrafanaSilence{
		{
			ID:       "ours",
			Comment:  maintenanceWindowCommentMarker + " planned",
			StartsAt: time.Unix(1000, 0),
			EndsAt:   time.Unix(2000, 0),
			Matchers: []GrafanaSilenceMatcher{
				{Name: "organization_id", Value: "7", IsEqual: true},
				{Name: "__alert_rule_uid__", Value: "rule-9", IsEqual: true},
			},
		},
		{
			ID:      "external",
			Comment: "operator silence, same org",
			Matchers: []GrafanaSilenceMatcher{
				{Name: "organization_id", Value: "7", IsEqual: true},
				{Name: "__alert_rule_uid__", Value: "rule-9", IsEqual: true},
			},
		},
	}
	var postBody []byte
	svc := NewService(fakeGrafanaSilences(t, listed, &postBody), nil, nil, nil, DestinationPolicy{})

	out, err := svc.ListMaintenanceWindows(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, out, 1, "only the Proto Fleet-marked silence is a maintenance window")
	assert.Equal(t, "ours", out[0].ID)
	assert.Equal(t, "planned", out[0].Comment, "the provenance marker is stripped for display")

	// The external silence isn't owned, so update/delete can't reach it.
	_, err = svc.UpdateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		ID:       "external",
		Scope:    MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule, RuleID: "rule-9"},
		StartsAt: time.Unix(1000, 0),
		EndsAt:   time.Unix(2000, 0),
	})
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, svc.DeleteMaintenanceWindow(context.Background(), 7, "external"), ErrNotFound)
}

// Without pause-silence state, a muted rule is indistinguishable from an enabled one, so
// ListRules must surface the error rather than render the rule as confidently enabled.
func TestListRulesFailsClosedWhenSilencesUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/alert-rules", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode([]GrafanaAlertRule{
			{UID: "rule-9", Title: "Rule 9", Labels: map[string]string{ruleLabelScope: ruleScopeShared}},
		}))
	})
	mux.HandleFunc("GET /api/alertmanager/grafana/api/v2/silences", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), nil, nil, nil, DestinationPolicy{})

	_, err := svc.ListRules(context.Background(), 7)
	require.Error(t, err, "ListRules must fail closed when pause-silence state can't be loaded")
}

// A lifted pause leaves an expired silence in the list that still carries the 2099 sentinel
// end time, so it looks active by timestamp. ListRules must treat it as gone, otherwise the
// rule reads as paused forever and PauseRule no-ops on a rule that is actually firing.
func TestListRulesIgnoresExpiredPauseSilence(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/alert-rules", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode([]GrafanaAlertRule{
			{UID: "rule-9", Title: "Rule 9", Labels: map[string]string{ruleLabelScope: ruleScopeShared}},
		}))
	})
	mux.HandleFunc("GET /api/alertmanager/grafana/api/v2/silences", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode([]GrafanaSilence{
			{
				ID:       "expired-pause",
				Comment:  pauseSilenceCommentMarker + " Paused via Proto Fleet UI",
				StartsAt: time.Unix(1000, 0),
				EndsAt:   pauseSilenceEndsAt,
				Status:   &GrafanaSilenceStatus{State: "expired"},
				Matchers: []GrafanaSilenceMatcher{
					{Name: "organization_id", Value: "7", IsEqual: true},
					{Name: "__alert_rule_uid__", Value: "rule-9", IsEqual: true},
				},
			},
		}))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), nil, nil, nil, DestinationPolicy{})

	out, err := svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.True(t, out[0].Enabled, "an expired pause silence must not report the rule as paused")
}

// The pause marker must never be an alert matcher: Alertmanager ANDs every matcher
// against an alert's labels, and no rule emits a marker label, so a marker matcher
// would mute nothing while the rule still showed as paused.
func TestPauseSilenceMarkerIsNotAMatcher(t *testing.T) {
	sil := buildPauseSilence(7, "rule-9", "alice@example.com", time.Unix(0, 0).UTC())
	for _, m := range sil.Matchers {
		assert.Contains(t, []string{silenceLabelOrganizationID, alertRuleUIDMatcher}, m.Name,
			"pause silence may only carry org and alert-rule-UID matchers")
	}
	assert.True(t, isPauseSilence(sil), "comment marker must identify a pause silence")
	assert.True(t, isPauseSilenceFor(sil, "7", "rule-9"))
}

// A rule pause is an indefinite mute of a (possibly critical) rule, so the silence must
// attribute it to the operator who paused rather than a generic app name.
func TestPauseSilenceRecordsActor(t *testing.T) {
	withActor := buildPauseSilence(7, "rule-9", "alice@example.com", time.Unix(0, 0).UTC())
	assert.Equal(t, "alice@example.com", withActor.CreatedBy)
	assert.Contains(t, withActor.Comment, "alice@example.com")
	assert.True(t, isPauseSilence(withActor), "actor in the comment must not break marker detection")

	anon := buildPauseSilence(7, "rule-9", "", time.Unix(0, 0).UTC())
	assert.Equal(t, "Proto Fleet", anon.CreatedBy, "fall back to app name when actor is unknown")
}

// A rule-scoped maintenance window must resolve its target through the same visibility
// check as PauseRule, so a manage user can't silence a rule they can't list.
func TestMaintenanceWindowRequiresVisibleRule(t *testing.T) {
	var postBody []byte
	svc := NewService(fakeGrafanaSilences(t, nil, &postBody), nil, nil, nil, DestinationPolicy{})

	_, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		Scope:    MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule, RuleID: "rule-does-not-exist"},
		StartsAt: time.Unix(1000, 0),
		EndsAt:   time.Unix(2000, 0),
	})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Nil(t, postBody, "window for an unknown rule must not reach Grafana")
}

// Maintenance windows are finite: the server must reject a missing or non-increasing time
// range even though the UI enforces it, so a direct RPC can't open a decades-long silence.
func TestMaintenanceWindowRejectsInvalidTimes(t *testing.T) {
	cases := map[string]MaintenanceWindow{
		"missing ends_at":    {StartsAt: time.Unix(1000, 0)},
		"missing starts_at":  {EndsAt: time.Unix(2000, 0)},
		"ends before starts": {StartsAt: time.Unix(2000, 0), EndsAt: time.Unix(1000, 0)},
		"ends equals starts": {StartsAt: time.Unix(1000, 0), EndsAt: time.Unix(1000, 0)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var postBody []byte
			svc := NewService(fakeGrafanaSilences(t, nil, &postBody), nil, nil, nil, DestinationPolicy{})
			tc.Scope = MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule, RuleID: "rule-9"}
			_, err := svc.CreateMaintenanceWindow(context.Background(), 7, tc)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "want InvalidArgument, got %v", err)
			assert.Nil(t, postBody, "invalid window must not reach Grafana")
		})
	}
}

// A rule-scoped maintenance window is structurally identical to a pause silence, so a
// caller must not be able to smuggle the pause marker into the comment and have the
// window hidden from the list / overlaid as a paused rule.
func TestMaintenanceWindowRejectsPauseMarkerComment(t *testing.T) {
	var postBody []byte
	svc := NewService(fakeGrafanaSilences(t, nil, &postBody), nil, nil, nil, DestinationPolicy{})

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
		{"unmarked unlabeled rule hidden", map[string]string{"severity": "warning"}, false},
		{"shared marker visible to all", map[string]string{ruleLabelScope: ruleScopeShared}, true},
		{"internal marker hidden from every org", map[string]string{ruleLabelScope: ruleScopeInternal}, false},
		{"matching org label visible", map[string]string{ruleLabelOrganizationID: "7"}, true},
		{"other org label hidden", map[string]string{ruleLabelOrganizationID: "9"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.visible, ruleVisibleToOrg(GrafanaAlertRule{Labels: tc.labels}, want))
		})
	}
}
