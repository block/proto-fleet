package alerts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	alertsv1 "github.com/block/proto-fleet/server/generated/grpc/alerts/v1"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestRuleConfigScopeMappingRoundTrip(t *testing.T) {
	in := offlineRuleConfig()
	in.Scope = &alertsv1.RuleScope{
		SiteIds:     []int64{3, 5},
		DeviceIds:   []string{"dev-a"},
		BuildingIds: []int64{2},
		RackIds:     []int64{4},
		GroupIds:    []int64{6},
	}

	dom, err := protoToRuleConfig(in)
	require.NoError(t, err)
	require.NotNil(t, dom.Scope)
	assert.Equal(t, &alerts.RuleScope{
		SiteIDs:     []int64{3, 5},
		DeviceIDs:   []string{"dev-a"},
		BuildingIDs: []int64{2},
		RackIDs:     []int64{4},
		GroupIDs:    []int64{6},
	}, dom.Scope)

	out := ruleConfigToProto(dom, true)
	require.NotNil(t, out.Scope)
	assert.Equal(t, in.Scope.SiteIds, out.Scope.SiteIds)
	assert.Equal(t, in.Scope.DeviceIds, out.Scope.DeviceIds)
	assert.Equal(t, in.Scope.BuildingIds, out.Scope.BuildingIds)
	assert.Equal(t, in.Scope.RackIds, out.Scope.RackIds)
	assert.Equal(t, in.Scope.GroupIds, out.Scope.GroupIds)
}

func TestRuleConfigAllSitesRoundTrip(t *testing.T) {
	in := offlineRuleConfig()
	in.Scope = &alertsv1.RuleScope{AllSites: true}

	dom, err := protoToRuleConfig(in)
	require.NoError(t, err)
	require.NotNil(t, dom.Scope)
	assert.True(t, dom.Scope.AllSites)

	out := ruleConfigToProto(dom, true)
	require.NotNil(t, out.Scope)
	assert.True(t, out.Scope.AllSites)
	// Redacted readers still see the placement half of the scope.
	assert.True(t, ruleConfigToProto(dom, false).Scope.GetAllSites())
}

// Scope device ids are device identity: read paths without miner:read omit the
// ids but flag the omission — an absent scope would misread as org-wide.
func TestRuleConfigScopeRedactsDeviceIDsWithoutMinerRead(t *testing.T) {
	in := offlineRuleConfig()
	in.Scope = &alertsv1.RuleScope{SiteIds: []int64{3}, DeviceIds: []string{"dev-a"}}
	dom, err := protoToRuleConfig(in)
	require.NoError(t, err)

	out := ruleConfigToProto(dom, false)
	require.NotNil(t, out.Scope)
	assert.Equal(t, []int64{3}, out.Scope.SiteIds)
	assert.Empty(t, out.Scope.DeviceIds)
	assert.True(t, out.Scope.DeviceIdsRedacted)

	dom.Scope = &alerts.RuleScope{DeviceIDs: []string{"dev-a"}}
	deviceOnly := ruleConfigToProto(dom, false).Scope
	require.NotNil(t, deviceOnly, "a redacted device-only scope must not read as org-wide")
	assert.Empty(t, deviceOnly.DeviceIds)
	assert.True(t, deviceOnly.DeviceIdsRedacted)

	// The flag is server-set only: a write echoing it back carries no device list to save, so
	// accepting it would silently rewrite the rule without its explicit miners.
	for name, scope := range map[string]*alertsv1.RuleScope{
		"redacted only":  {DeviceIdsRedacted: true},
		"redacted mixed": {SiteIds: []int64{3}, DeviceIdsRedacted: true},
	} {
		_, err := protoToRuleConfig(&alertsv1.RuleConfig{
			Name:            in.Name,
			DurationSeconds: in.DurationSeconds,
			TemplateConfig:  in.TemplateConfig,
			Scope:           scope,
		})
		require.Error(t, err, name)
		assert.True(t, fleeterror.IsInvalidArgumentError(err), name)
	}
}

// org_wide alone maps to no domain scope with the explicit flag set (deliberate unscoping vs a
// stale pre-scope client); combined with placements it is contradictory and rejected.
func TestRuleConfigOrgWideScopeMapping(t *testing.T) {
	in := offlineRuleConfig()
	in.Scope = &alertsv1.RuleScope{OrgWide: true}
	dom, err := protoToRuleConfig(in)
	require.NoError(t, err)
	assert.Nil(t, dom.Scope)
	assert.True(t, dom.ScopeOrgWideExplicit)
	assert.Nil(t, ruleConfigToProto(dom, true).Scope, "reads never echo the marker")

	in.Scope = &alertsv1.RuleScope{OrgWide: true, SiteIds: []int64{3}}
	_, err = protoToRuleConfig(in)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// An empty scope message means org-wide: it maps to no domain scope so the
// stored config JSON stays clean, and reads omit the proto field entirely.
func TestRuleConfigEmptyScopeCollapses(t *testing.T) {
	in := offlineRuleConfig()
	in.Scope = &alertsv1.RuleScope{}

	dom, err := protoToRuleConfig(in)
	require.NoError(t, err)
	assert.Nil(t, dom.Scope)
	assert.Nil(t, ruleConfigToProto(dom, true).Scope)
}

// scopedConfigStore is a one-rule RuleConfigStore holding the scoped fixture's config.
type scopedConfigStore struct{ cfg alerts.RuleConfig }

func (s scopedConfigStore) UpsertConfig(context.Context, int64, string, alerts.RuleConfig) error {
	return nil
}

func (s scopedConfigStore) GetConfig(context.Context, int64, string) (*alerts.RuleConfig, error) {
	cfg := s.cfg
	return &cfg, nil
}

func (s scopedConfigStore) ListConfigs(context.Context, int64, []string) (map[string]alerts.RuleConfig, error) {
	return map[string]alerts.RuleConfig{"pfu-scoped": s.cfg}, nil
}

func (s scopedConfigStore) DeleteConfig(context.Context, int64, string) error { return nil }

func (s scopedConfigStore) SweepConfigs(context.Context, int64, []string) (int64, error) {
	return 0, nil
}

// scopedRuleGrafana serves one stored scoped user rule (org 1) so ListRules
// exercises the real handler path, including redaction.
func scopedRuleGrafana(t *testing.T) *alerts.Grafana {
	t.Helper()
	rule := alerts.GrafanaAlertRule{
		UID:       "pfu-scoped",
		FolderUID: "proto-fleet-user-1",
		RuleGroup: "pfu-scoped",
		Title:     "Scoped",
		For:       "300s",
		Labels: map[string]string{
			"organization_id":      "1",
			"proto_fleet_origin":   "user",
			"template":             "offline",
			"proto_fleet_rule_uid": "pfu-scoped",
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/provisioning/alert-rules", func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode([]alerts.GrafanaAlertRule{rule}))
	})
	mux.HandleFunc("GET /api/alertmanager/grafana/api/v2/silences", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[]"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return alerts.NewGrafana(alerts.GrafanaConfig{URL: srv.URL})
}

// Scope device ids are device identity: the wire response must carry them only
// for callers holding miner:read, without hiding the placement half.
func TestListRulesRedactsScopeDeviceIDsWithoutMinerRead(t *testing.T) {
	configs := scopedConfigStore{cfg: alerts.RuleConfig{
		Name:            "Scoped",
		DurationSeconds: 300,
		Offline:         &alerts.OfflineRuleConfig{},
		Scope:           &alerts.RuleScope{SiteIDs: []int64{3}, DeviceIDs: []string{"dev-a"}},
	}}
	h := NewHandler(alerts.NewService(scopedRuleGrafana(t), nil, nil, configs, nil, nil, nil, alerts.DestinationPolicy{}), nil)

	full, err := h.ListRules(ctxWithPerms(authz.PermAlertRead, authz.PermMinerRead), connect.NewRequest(&alertsv1.ListRulesRequest{}))
	require.NoError(t, err)
	require.Len(t, full.Msg.Rules, 1)
	fullScope := full.Msg.Rules[0].GetConfig().GetScope()
	require.NotNil(t, fullScope)
	assert.Equal(t, []int64{3}, fullScope.SiteIds)
	assert.Equal(t, []string{"dev-a"}, fullScope.DeviceIds)

	redacted, err := h.ListRules(ctxWithPerms(authz.PermAlertRead), connect.NewRequest(&alertsv1.ListRulesRequest{}))
	require.NoError(t, err)
	require.Len(t, redacted.Msg.Rules, 1)
	redactedScope := redacted.Msg.Rules[0].GetConfig().GetScope()
	require.NotNil(t, redactedScope)
	assert.Equal(t, []int64{3}, redactedScope.SiteIds)
	assert.Empty(t, redactedScope.DeviceIds)
}

// An org-level miner:read grant narrowed away at even one site must redact:
// scope device ids can reference miners at any site, including the narrowed one.
func TestListRulesRedactsScopeDeviceIDsWithSiteNarrowedMinerRead(t *testing.T) {
	configs := scopedConfigStore{cfg: alerts.RuleConfig{
		Name:            "Scoped",
		DurationSeconds: 300,
		Offline:         &alerts.OfflineRuleConfig{},
		Scope:           &alerts.RuleScope{SiteIDs: []int64{3}, DeviceIDs: []string{"dev-a"}},
	}}
	h := NewHandler(alerts.NewService(scopedRuleGrafana(t), nil, nil, configs, nil, nil, nil, alerts.DestinationPolicy{}), nil)

	site := int64(5)
	ctx := authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		Username:       "alice",
	})
	ctx = middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{
		{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermAlertRead, authz.PermMinerRead}},
		// The site-scope assignment narrows miner:read away at site 5.
		{AssignmentID: 2, ScopeType: authz.ScopeSite, SiteID: &site, Permissions: []string{authz.PermAlertRead}},
	}))

	res, err := h.ListRules(ctx, connect.NewRequest(&alertsv1.ListRulesRequest{}))
	require.NoError(t, err)
	require.Len(t, res.Msg.Rules, 1)
	scope := res.Msg.Rules[0].GetConfig().GetScope()
	require.NotNil(t, scope)
	assert.Empty(t, scope.DeviceIds)
	assert.True(t, scope.DeviceIdsRedacted)
}
