package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, tc.policy)
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
		// A nil postBody means the caller expects no silence writes (e.g. DB-backed window paths).
		require.NotNil(t, postBody, "unexpected silence write")
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

// fakeWindowStore is an in-memory MaintenanceWindowStore honoring the store contract:
// write-once created_by/created_at, ErrNotFound for rows the org doesn't own.
type fakeWindowStore struct {
	next                int64
	rows                map[int64]MaintenanceWindowRecord
	listActiveErr       error
	lastPruneRetention  time.Duration
	lastPruneKeepNewest int64
}

func newFakeWindowStore() *fakeWindowStore {
	return &fakeWindowStore{rows: map[int64]MaintenanceWindowRecord{}}
}

func (f *fakeWindowStore) Insert(_ context.Context, rec MaintenanceWindowRecord) (MaintenanceWindowRecord, error) {
	f.next++
	rec.ID = f.next
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Unix(500, 0)
	}
	f.rows[rec.ID] = rec
	return rec, nil
}

func (f *fakeWindowStore) Update(_ context.Context, rec MaintenanceWindowRecord) (MaintenanceWindowRecord, error) {
	cur, ok := f.rows[rec.ID]
	if !ok || cur.OrganizationID != rec.OrganizationID {
		return MaintenanceWindowRecord{}, ErrNotFound
	}
	rec.CreatedBy, rec.CreatedAt = cur.CreatedBy, cur.CreatedAt
	f.rows[rec.ID] = rec
	return rec, nil
}

func (f *fakeWindowStore) List(_ context.Context, orgID int64) ([]MaintenanceWindowRecord, error) {
	var out []MaintenanceWindowRecord
	for _, rec := range f.rows {
		if rec.OrganizationID == orgID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (f *fakeWindowStore) ListActive(_ context.Context, orgID int64, now time.Time) ([]MaintenanceWindowRecord, error) {
	if f.listActiveErr != nil {
		return nil, f.listActiveErr
	}
	var out []MaintenanceWindowRecord
	for _, rec := range f.rows {
		if rec.OrganizationID == orgID && !rec.StartsAt.After(now) && rec.EndsAt.After(now) {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (f *fakeWindowStore) CountUnexpired(_ context.Context, orgID int64, now time.Time, excludingID int64) (int64, error) {
	var n int64
	for _, rec := range f.rows {
		if rec.OrganizationID == orgID && rec.EndsAt.After(now) && rec.ID != excludingID {
			n++
		}
	}
	return n, nil
}

// PruneExpired applies only the age cutoff and records its arguments; the keep-newest count
// backstop is real SQL, exercised against the database in the sqlstore integration test.
func (f *fakeWindowStore) PruneExpired(_ context.Context, orgID int64, now time.Time, retention time.Duration, keepNewest int64) (int64, error) {
	f.lastPruneKeepNewest = keepNewest
	f.lastPruneRetention = retention
	before := now.Add(-retention)
	var n int64
	for id, rec := range f.rows {
		if rec.OrganizationID == orgID && rec.EndsAt.Before(before) {
			delete(f.rows, id)
			n++
		}
	}
	return n, nil
}

func (f *fakeWindowStore) Delete(_ context.Context, orgID, id int64) error {
	rec, ok := f.rows[id]
	if !ok || rec.OrganizationID != orgID {
		return ErrNotFound
	}
	delete(f.rows, id)
	return nil
}

// windowTestNow sits before the Unix(1000)…Unix(3000) fixture windows so they read as upcoming
// (writes require ends_at in the future).
var windowTestNow = time.Unix(500, 0)

// newMaintenanceWindowService wires a service whose Grafana knows one shared rule ("rule-9")
// and whose clock is pinned to windowTestNow.
func newMaintenanceWindowService(t *testing.T) (*Service, *fakeWindowStore, *fakeChannelStore) {
	t.Helper()
	windows := newFakeWindowStore()
	channels := newFakeChannelStore()
	svc := NewService(fakeGrafanaSilences(t, nil, nil), channels, nil, nil, windows, nil, nil, nil, DestinationPolicy{})
	svc.now = func() time.Time { return windowTestNow }
	return svc, windows, channels
}

// fillWindowsToCap inserts maxMaintenanceWindowsPerOrg active windows for org 7 and returns the
// last one, putting the org exactly at the write quota.
func fillWindowsToCap(t *testing.T, windows *fakeWindowStore, now time.Time) MaintenanceWindowRecord {
	t.Helper()
	var last MaintenanceWindowRecord
	for range maxMaintenanceWindowsPerOrg {
		var err error
		last, err = windows.Insert(context.Background(), MaintenanceWindowRecord{
			OrganizationID: 7, StartsAt: now, EndsAt: now.Add(time.Hour),
		})
		require.NoError(t, err)
	}
	return last
}

func TestCreateMaintenanceWindowResolvesTargets(t *testing.T) {
	svc, windows, channels := newMaintenanceWindowService(t)
	ch, err := channels.Insert(context.Background(), ChannelRecord{OrganizationID: 7, Name: "ops"})
	require.NoError(t, err)
	chID := strconv.FormatInt(ch.ID, 10)

	out, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		RuleIDs:    []string{"rule-9", "rule-9"},
		ChannelIDs: []string{chID, chID},
		StartsAt:   time.Unix(1000, 0),
		EndsAt:     time.Unix(2000, 0),
		Comment:    "planned work",
		CreatedBy:  "alice@example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"rule-9"}, out.RuleIDs, "duplicate rule ids collapse")
	assert.Equal(t, []string{chID}, out.ChannelIDs, "duplicate channel ids collapse")

	rec := windows.rows[1]
	assert.Equal(t, []string{"rule-9"}, rec.RuleUIDs)
	assert.Equal(t, []int64{ch.ID}, rec.ChannelIDs)
	assert.Equal(t, "alice@example.com", rec.CreatedBy)
}

// Empty target lists persist as "every rule / every channel" without reading Grafana or the
// channel table, so the all/all default works even mid-outage.
func TestCreateMaintenanceWindowEmptyTargetsMeanAll(t *testing.T) {
	windows := newFakeWindowStore()
	svc := NewService(nil, nil, nil, nil, windows, nil, nil, nil, DestinationPolicy{})
	svc.now = func() time.Time { return windowTestNow }

	out, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		StartsAt: time.Unix(1000, 0),
		EndsAt:   time.Unix(2000, 0),
	})
	require.NoError(t, err)
	assert.Empty(t, out.RuleIDs)
	assert.Empty(t, out.ChannelIDs)
	assert.Empty(t, windows.rows[1].RuleUIDs)
	assert.Empty(t, windows.rows[1].ChannelIDs)
}

func TestMaintenanceWindowRejectsUnknownChannel(t *testing.T) {
	svc, windows, _ := newMaintenanceWindowService(t)
	_, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		ChannelIDs: []string{"999"},
		StartsAt:   time.Unix(1000, 0),
		EndsAt:     time.Unix(2000, 0),
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "want InvalidArgument, got %v", err)
	assert.Empty(t, windows.rows, "window with an unknown channel must not persist")
}

func TestUpdateMaintenanceWindowPreservesCreator(t *testing.T) {
	svc, _, _ := newMaintenanceWindowService(t)
	created, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		StartsAt:  time.Unix(1000, 0),
		EndsAt:    time.Unix(2000, 0),
		CreatedBy: "alice@example.com",
	})
	require.NoError(t, err)

	updated, err := svc.UpdateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		ID:        created.ID,
		RuleIDs:   []string{"rule-9"},
		StartsAt:  time.Unix(1000, 0),
		EndsAt:    time.Unix(3000, 0),
		CreatedBy: "mallory@example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", updated.CreatedBy, "an edit must keep the original creator")
	assert.Equal(t, []string{"rule-9"}, updated.RuleIDs)
	assert.Equal(t, time.Unix(3000, 0), updated.EndsAt)
}

func TestMaintenanceWindowScopedToOwningOrg(t *testing.T) {
	svc, _, _ := newMaintenanceWindowService(t)
	created, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		StartsAt: time.Unix(1000, 0),
		EndsAt:   time.Unix(2000, 0),
	})
	require.NoError(t, err)

	_, err = svc.UpdateMaintenanceWindow(context.Background(), 8, MaintenanceWindow{
		ID:       created.ID,
		StartsAt: time.Unix(1000, 0),
		EndsAt:   time.Unix(2000, 0),
	})
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, svc.DeleteMaintenanceWindow(context.Background(), 8, created.ID), ErrNotFound)

	other, err := svc.ListMaintenanceWindows(context.Background(), 8)
	require.NoError(t, err)
	assert.Empty(t, other, "another org must not see the window")
}

func TestMaintenanceWindowUnknownIDIsNotFound(t *testing.T) {
	svc, _, _ := newMaintenanceWindowService(t)
	_, err := svc.UpdateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		ID:       "42",
		StartsAt: time.Unix(1000, 0),
		EndsAt:   time.Unix(2000, 0),
	})
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, svc.DeleteMaintenanceWindow(context.Background(), 7, "42"), ErrNotFound)
	// A non-numeric id can't be a row, so it reads as the same uniform NotFound.
	require.ErrorIs(t, svc.DeleteMaintenanceWindow(context.Background(), 7, "sil-legacy"), ErrNotFound)
}

func TestListMaintenanceWindowsComputesActive(t *testing.T) {
	svc, windows, _ := newMaintenanceWindowService(t)
	svc.now = func() time.Time { return time.Unix(1500, 0) }
	windows.rows[1] = MaintenanceWindowRecord{ID: 1, OrganizationID: 7, StartsAt: time.Unix(1000, 0), EndsAt: time.Unix(2000, 0)}
	windows.rows[2] = MaintenanceWindowRecord{ID: 2, OrganizationID: 7, StartsAt: time.Unix(3000, 0), EndsAt: time.Unix(4000, 0)}

	out, err := svc.ListMaintenanceWindows(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, out, 2)
	byID := map[string]MaintenanceWindow{}
	for _, w := range out {
		byID[w.ID] = w
	}
	assert.True(t, byID["1"].Active, "a window covering now is active")
	assert.False(t, byID["2"].Active, "a future window is not active")
}

// Pre-migration windows lived as marked Grafana silences; the startup sweep must copy the
// representable live ones into the window store before deleting them (deleting alone would
// silently lift active suppression mid-maintenance), retain live ones the DB model can't
// represent until they expire on their own, and leave pause silences, operator silences, and
// already-expired legacy windows alone.
func TestMigrateLegacyMaintenanceWindowSilences(t *testing.T) {
	ruleMatchers := []GrafanaSilenceMatcher{
		{Name: "organization_id", Value: "7", IsEqual: true},
		{Name: "__alert_rule_uid__", Value: "rule-9", IsEqual: true},
	}
	fake := &fakeGrafanaRules{silences: []GrafanaSilence{
		{
			ID:        "legacy-rule",
			Comment:   legacyMaintenanceWindowCommentMarker + " planned",
			StartsAt:  time.Unix(1000, 0),
			EndsAt:    time.Unix(9000, 0),
			CreatedBy: "alice@example.com",
			Matchers:  ruleMatchers,
		},
		// Same rule and interval as legacy-rule but a different operator and comment: a distinct
		// window whose audit record must survive migration, not be deduped away.
		{
			ID:        "legacy-rule-twin",
			Comment:   legacyMaintenanceWindowCommentMarker + " other crew",
			StartsAt:  time.Unix(1000, 0),
			EndsAt:    time.Unix(9000, 0),
			CreatedBy: "bob@example.com",
			Matchers:  ruleMatchers,
		},
		// Device-scoped (no rule matcher): unrepresentable in the rule×channel model, so its live
		// suppression is left to expire in Grafana rather than lifted mid-maintenance.
		{ID: "legacy-device", Comment: legacyMaintenanceWindowCommentMarker, StartsAt: time.Unix(1000, 0), EndsAt: time.Unix(9000, 0), Matchers: []GrafanaSilenceMatcher{
			{Name: "organization_id", Value: "7", IsEqual: true},
			{Name: "device_id", Value: "miner-1", IsEqual: true},
		}},
		// Ended but not yet GC'd as expired: nothing to preserve, deleted only.
		{ID: "legacy-ended", Comment: legacyMaintenanceWindowCommentMarker, StartsAt: time.Unix(1000, 0), EndsAt: time.Unix(2000, 0), Matchers: ruleMatchers},
		{ID: "legacy-expired", Comment: legacyMaintenanceWindowCommentMarker, Status: &GrafanaSilenceStatus{State: "expired"}},
		{ID: "pause", Comment: pauseSilenceCommentMarker},
		{ID: "operator", Comment: "operator silence"},
	}}
	windows := newFakeWindowStore()
	svc := NewService(fake.server(t), nil, nil, nil, windows, nil, nil, nil, DestinationPolicy{})
	svc.now = func() time.Time { return time.Unix(5000, 0) }

	migrated, removed, err := svc.MigrateLegacyMaintenanceWindowSilences(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, migrated, "the same-interval twin is a distinct window, not a duplicate")
	assert.Equal(t, 3, removed)
	assert.ElementsMatch(t, []string{"legacy-rule", "legacy-rule-twin", "legacy-ended"}, fake.deletedSilences,
		"live silences the DB model can't represent must be retained, not deleted")

	require.Len(t, windows.rows, 2)
	assert.Equal(t, "bob@example.com", windows.rows[2].CreatedBy, "the twin keeps its own audit record")
	rec := windows.rows[1]
	assert.Equal(t, int64(7), rec.OrganizationID)
	assert.Equal(t, []string{"rule-9"}, rec.RuleUIDs)
	assert.Empty(t, rec.ChannelIDs, "a silence muted everywhere, so the window covers every channel")
	assert.True(t, rec.StartsAt.Equal(time.Unix(1000, 0)), "starts_at carries over")
	assert.True(t, rec.EndsAt.Equal(time.Unix(9000, 0)), "ends_at carries over")
	assert.Equal(t, "planned", rec.Comment, "the marker is stripped from the operator's comment")
	assert.Equal(t, "alice@example.com", rec.CreatedBy)

	// The fake's DELETE never mutates the served list, so a second sweep sees the same
	// silences — the shape of a first sweep whose insert landed but whose delete failed.
	migrated, _, err = svc.MigrateLegacyMaintenanceWindowSilences(context.Background())
	require.NoError(t, err)
	assert.Zero(t, migrated, "an already-migrated silence must not insert a duplicate window")
	assert.Len(t, windows.rows, 2)
}

// legacyWindowRecord must accept exactly the shape the old UI wrote — one org equality matcher
// plus one rule-UID equality matcher — and report every other shape unrepresentable (see its
// doc for why translating a recognizable subset would widen the suppression).
func TestLegacyWindowRecordShapes(t *testing.T) {
	org := GrafanaSilenceMatcher{Name: "organization_id", Value: "7", IsEqual: true}
	rule := GrafanaSilenceMatcher{Name: "__alert_rule_uid__", Value: "rule-9", IsEqual: true}
	cases := map[string]struct {
		matchers []GrafanaSilenceMatcher
		ok       bool
	}{
		"org and rule":            {[]GrafanaSilenceMatcher{org, rule}, true},
		"org and pre-uid rule":    {[]GrafanaSilenceMatcher{org, {Name: "alertname_uid", Value: "rule-9", IsEqual: true}}, true},
		"extra device matcher":    {[]GrafanaSilenceMatcher{org, rule, {Name: "device_id", Value: "miner-1", IsEqual: true}}, false},
		"regex rule matcher":      {[]GrafanaSilenceMatcher{org, {Name: "__alert_rule_uid__", Value: "rule-.*", IsEqual: true, IsRegex: true}}, false},
		"negated rule matcher":    {[]GrafanaSilenceMatcher{org, {Name: "__alert_rule_uid__", Value: "rule-9"}}, false},
		"duplicate rule matcher":  {[]GrafanaSilenceMatcher{org, rule, {Name: "alertname_uid", Value: "rule-8", IsEqual: true}}, false},
		"duplicate org matcher":   {[]GrafanaSilenceMatcher{org, org, rule}, false},
		"missing rule matcher":    {[]GrafanaSilenceMatcher{org}, false},
		"missing org matcher":     {[]GrafanaSilenceMatcher{rule}, false},
		"non-numeric org matcher": {[]GrafanaSilenceMatcher{{Name: "organization_id", Value: "abc", IsEqual: true}, rule}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, ok := legacyWindowRecord(GrafanaSilence{
				Comment:  legacyMaintenanceWindowCommentMarker,
				StartsAt: time.Unix(1000, 0),
				EndsAt:   time.Unix(2000, 0),
				Matchers: tc.matchers,
			})
			assert.Equal(t, tc.ok, ok)
		})
	}
}

// The creation path bounds growth: unexpired windows count against a per-org cap, and rows
// expired past retention are pruned so neither the cap nor the list wedges shut over time.
func TestCreateMaintenanceWindowQuotaAndPrune(t *testing.T) {
	svc, windows, _ := newMaintenanceWindowService(t)
	now := time.Unix(1_000_000_000, 0)
	svc.now = func() time.Time { return now }

	fillWindowsToCap(t, windows, now)
	_, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		StartsAt: now, EndsAt: now.Add(time.Hour),
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "the unexpired-window cap must reject creation")

	// Expired history neither counts against the cap nor survives the retention prune.
	windows.rows = map[int64]MaintenanceWindowRecord{}
	ancient, err := windows.Insert(context.Background(), MaintenanceWindowRecord{
		OrganizationID: 7,
		StartsAt:       now.Add(-maintenanceWindowRetention - 2*time.Hour),
		EndsAt:         now.Add(-maintenanceWindowRetention - time.Hour),
	})
	require.NoError(t, err)
	created, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		StartsAt: now, EndsAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	_, stillThere := windows.rows[ancient.ID]
	assert.False(t, stillThere, "creation prunes windows expired past retention")
	// The count backstop itself is SQL (integration-tested); here just pin that creation asks
	// for it with the intended policy values.
	assert.Equal(t, maintenanceWindowRetention, windows.lastPruneRetention)
	assert.Equal(t, int64(maxRetainedExpiredWindowsPerOrg), windows.lastPruneKeepNewest)
}

// Every accepted write leaves the row unexpired, so an update can revive an expired row; it
// must be held to the same per-org cap as a create, while an edit of a window that already
// counts (excluded as the row being rewritten) still saves at cap.
func TestUpdateMaintenanceWindowQuota(t *testing.T) {
	svc, windows, _ := newMaintenanceWindowService(t)
	now := time.Unix(1_000_000_000, 0)
	svc.now = func() time.Time { return now }

	expired, err := windows.Insert(context.Background(), MaintenanceWindowRecord{
		OrganizationID: 7, StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(-time.Hour),
	})
	require.NoError(t, err)
	active := fillWindowsToCap(t, windows, now)

	_, err = svc.UpdateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		ID: strconv.FormatInt(expired.ID, 10), StartsAt: now, EndsAt: now.Add(time.Hour),
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err),
		"reviving an expired window past the cap must be rejected, got %v", err)

	updated, err := svc.UpdateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		ID: strconv.FormatInt(active.ID, 10), StartsAt: now, EndsAt: now.Add(2 * time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, now.Add(2*time.Hour), updated.EndsAt, "editing an at-cap active window must still save")
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
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), nil, nil, nil, nil, nil, nil, nil, DestinationPolicy{})

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
	svc := NewService(NewGrafana(GrafanaConfig{URL: srv.URL}), nil, nil, nil, nil, nil, nil, nil, DestinationPolicy{})

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

// A rule-scoped maintenance window must resolve its targets through the same visibility
// check as PauseRule, so a manage user can't silence a rule they can't list.
func TestMaintenanceWindowRequiresVisibleRule(t *testing.T) {
	svc, windows, _ := newMaintenanceWindowService(t)

	_, err := svc.CreateMaintenanceWindow(context.Background(), 7, MaintenanceWindow{
		RuleIDs:  []string{"rule-does-not-exist"},
		StartsAt: time.Unix(1000, 0),
		EndsAt:   time.Unix(2000, 0),
	})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, windows.rows, "window for an unknown rule must not persist")
}

// Maintenance windows are finite and forward-looking: the server must reject a missing or
// non-increasing time range even though the UI enforces it, so a direct RPC can't open a
// decades-long silence — and an already-ended range, which would occupy a row invisible to
// the unexpired-count quota. The service clock is pinned to Unix(500) by the helper.
func TestMaintenanceWindowRejectsInvalidTimes(t *testing.T) {
	cases := map[string]MaintenanceWindow{
		"missing ends_at":    {StartsAt: time.Unix(1000, 0)},
		"missing starts_at":  {EndsAt: time.Unix(2000, 0)},
		"ends before starts": {StartsAt: time.Unix(2000, 0), EndsAt: time.Unix(1000, 0)},
		"ends equals starts": {StartsAt: time.Unix(1000, 0), EndsAt: time.Unix(1000, 0)},
		"already ended":      {StartsAt: time.Unix(100, 0), EndsAt: time.Unix(400, 0)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc, windows, _ := newMaintenanceWindowService(t)
			_, err := svc.CreateMaintenanceWindow(context.Background(), 7, tc)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "want InvalidArgument, got %v", err)
			assert.Empty(t, windows.rows, "invalid window must not persist")
		})
	}
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
