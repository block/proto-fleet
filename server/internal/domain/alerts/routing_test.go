package alerts

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type fakeRouteStore struct {
	policies map[int64]map[string]RoutePolicy
	listErr  error
	setErr   error
}

func newFakeRouteStore() *fakeRouteStore {
	return &fakeRouteStore{policies: map[int64]map[string]RoutePolicy{}}
}

func (f *fakeRouteStore) SetPolicy(_ context.Context, orgID int64, policy RoutePolicy) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.policies[orgID] == nil {
		f.policies[orgID] = map[string]RoutePolicy{}
	}
	f.policies[orgID][policy.RuleUID] = policy
	return nil
}

func (f *fakeRouteStore) DeletePolicy(_ context.Context, orgID int64, ruleUID string) error {
	delete(f.policies[orgID], ruleUID)
	return nil
}

func (f *fakeRouteStore) ListPolicies(_ context.Context, orgID int64) ([]RoutePolicy, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]RoutePolicy, 0, len(f.policies[orgID]))
	for _, p := range f.policies[orgID] {
		out = append(out, p)
	}
	return out, nil
}

func TestRuleUIDFromGeneratorURL(t *testing.T) {
	cases := map[string]string{
		"http://grafana:3000/alerting/grafana/pfu-abc123/view":                                "pfu-abc123",
		"https://grafana.example.com/alerting/grafana/protofleet-device-offline/view?orgId=1": "protofleet-device-offline",
		"http://grafana:3000/sub/path/alerting/grafana/uid-1/view":                            "uid-1",
		"http://grafana:3000/alerting/list":                                                   "",
		"http://grafana:3000/alerting/grafana":                                                "",
		"not a url at all ::":                                                                 "",
		"":                                                                                    "",
	}
	for raw, want := range cases {
		assert.Equalf(t, want, RuleUIDFromGeneratorURL(raw), "generatorURL %q", raw)
	}
}

// routingService wires a Service against a shared provisioned rule, an internal rule, one org-7 user rule, two org-7 channels, and a fake route store.
func routingService(t *testing.T) (*Service, *fakeRouteStore, *fakeGrafanaRules) {
	t.Helper()
	provisioned := GrafanaAlertRule{
		UID:    "protofleet-device-offline",
		Title:  "Device Offline",
		Labels: map[string]string{ruleLabelScope: ruleScopeShared, ruleLabelTemplate: "offline"},
	}
	internal := GrafanaAlertRule{
		UID:    "protofleet-ingest-stalled",
		Title:  "Metric Ingest Stalled",
		Labels: map[string]string{ruleLabelScope: ruleScopeInternal},
	}
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{provisioned, internal, userRuleFixture("pfu-mine", "7")}}
	channels := newFakeChannelStore()
	for range 2 {
		_, err := channels.Insert(context.Background(), ChannelRecord{OrganizationID: 7, Kind: ChannelKindSlack})
		require.NoError(t, err)
	}
	routes := newFakeRouteStore()
	return NewService(fake.server(t), channels, routes, nil, nil, DestinationPolicy{}), routes, fake
}

func TestSetRuleRoutingPersistsCustomAndNone(t *testing.T) {
	svc, routes, _ := routingService(t)

	// Custom on a provisioned rule: routing is org-owned even for shared rules.
	rule, err := svc.SetRuleRouting(context.Background(), 7, "protofleet-device-offline", RouteModeCustom, []string{"2", "1", "2"})
	require.NoError(t, err)
	require.NotNil(t, rule.Routing)
	assert.Equal(t, RouteModeCustom, rule.Routing.Mode)
	assert.Equal(t, []int64{1, 2}, rule.Routing.ChannelIDs, "ids are deduped and sorted")
	assert.Equal(t, RouteModeCustom, routes.policies[7]["protofleet-device-offline"].Mode)

	// None on the user rule.
	rule, err = svc.SetRuleRouting(context.Background(), 7, "pfu-mine", RouteModeNone, nil)
	require.NoError(t, err)
	require.NotNil(t, rule.Routing)
	assert.Equal(t, RouteModeNone, rule.Routing.Mode)

	// Default clears the stored policy.
	rule, err = svc.SetRuleRouting(context.Background(), 7, "pfu-mine", RouteModeDefault, nil)
	require.NoError(t, err)
	assert.Nil(t, rule.Routing)
	_, still := routes.policies[7]["pfu-mine"]
	assert.False(t, still)
}

func TestSetRuleRoutingValidation(t *testing.T) {
	svc, _, _ := routingService(t)
	ctx := context.Background()

	// Custom requires at least one channel.
	_, err := svc.SetRuleRouting(ctx, 7, "pfu-mine", RouteModeCustom, nil)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	// Default/none reject channel ids.
	_, err = svc.SetRuleRouting(ctx, 7, "pfu-mine", RouteModeNone, []string{"1"})
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	_, err = svc.SetRuleRouting(ctx, 7, "pfu-mine", RouteModeDefault, []string{"1"})
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	// Unknown mode.
	_, err = svc.SetRuleRouting(ctx, 7, "pfu-mine", RouteMode("weird"), nil)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	// A channel the org doesn't own (or a non-numeric id) is rejected.
	_, err = svc.SetRuleRouting(ctx, 7, "pfu-mine", RouteModeCustom, []string{"999"})
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	_, err = svc.SetRuleRouting(ctx, 7, "pfu-mine", RouteModeCustom, []string{"nope"})
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestSetRuleRoutingHiddenRulesAreNotFound(t *testing.T) {
	svc, _, _ := routingService(t)
	ctx := context.Background()

	// Another org's user rule and an operator-internal rule are invisible → NotFound.
	_, err := svc.SetRuleRouting(ctx, 8, "pfu-mine", RouteModeNone, nil)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.SetRuleRouting(ctx, 7, "protofleet-ingest-stalled", RouteModeNone, nil)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.SetRuleRouting(ctx, 7, "does-not-exist", RouteModeNone, nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestListRulesAttachesRouting(t *testing.T) {
	svc, routes, _ := routingService(t)
	require.NoError(t, routes.SetPolicy(context.Background(), 7, RoutePolicy{RuleUID: "pfu-mine", Mode: RouteModeCustom, ChannelIDs: []int64{1}}))

	rules, err := svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	byID := map[string]Rule{}
	for _, r := range rules {
		byID[r.ID] = r
	}
	require.NotNil(t, byID["pfu-mine"].Routing)
	assert.Equal(t, []int64{1}, byID["pfu-mine"].Routing.ChannelIDs)
	assert.Nil(t, byID["protofleet-device-offline"].Routing, "unrouted rules stay default")
}

func TestCreateRuleWithRoutingStoresPolicy(t *testing.T) {
	svc, routes, _ := routingService(t)

	rule, err := svc.CreateRule(context.Background(), 7, offlineConfig("Routed from birth", 1800), RouteModeCustom, []string{"1"})
	require.NoError(t, err)
	require.NotNil(t, rule.Routing)
	assert.Equal(t, RouteModeCustom, rule.Routing.Mode)
	assert.Equal(t, []int64{1}, rule.Routing.ChannelIDs)
	stored, ok := routes.policies[7][rule.ID]
	require.True(t, ok, "policy is keyed by the freshly created rule uid")
	assert.Equal(t, []int64{1}, stored.ChannelIDs)
}

// Routing is validated before the rule is created, so a bad channel can't leave an orphaned rule behind.
func TestCreateRuleRejectsBadRoutingBeforeCreating(t *testing.T) {
	svc, _, fake := routingService(t)

	_, err := svc.CreateRule(context.Background(), 7, offlineConfig("r", 1800), RouteModeCustom, []string{"999"})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Nil(t, fake.created, "no rule may be created when its routing is invalid")
}

// The policy is written before the rule, so a policy-write failure aborts the create with nothing to roll back.
func TestCreateRulePolicyWriteFailureAbortsCreate(t *testing.T) {
	svc, routes, fake := routingService(t)
	routes.setErr = errors.New("db down")

	_, err := svc.CreateRule(context.Background(), 7, offlineConfig("r", 1800), RouteModeNone, nil)
	require.Error(t, err)
	assert.Nil(t, fake.created, "the rule must not be created when its routing cannot be stored")
}

// When the rule create fails after the policy write (e.g. quota), the inert policy row is tidied up.
func TestCreateRuleCleansPolicyWhenCreateFails(t *testing.T) {
	fake := &fakeGrafanaRules{}
	for i := range maxUserRulesPerOrg {
		fake.listed = append(fake.listed, userRuleFixture(fmt.Sprintf("pfu-%d", i), "7"))
	}
	routes := newFakeRouteStore()
	svc := NewService(fake.server(t), nil, routes, nil, nil, DestinationPolicy{})

	_, err := svc.CreateRule(context.Background(), 7, offlineConfig("One more", 1800), RouteModeNone, nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Empty(t, routes.policies[7], "the pre-written policy is deleted when the create fails")
}

// The update response must carry the stored routing: reporting DEFAULT invites the client to overwrite the real policy.
func TestUpdateRuleResponseCarriesRouting(t *testing.T) {
	svc, routes, _ := routingService(t)
	require.NoError(t, routes.SetPolicy(context.Background(), 7, RoutePolicy{RuleUID: "pfu-mine", Mode: RouteModeCustom, ChannelIDs: []int64{1}}))

	updated, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", offlineConfig("Still routed", 1800))
	require.NoError(t, err)
	require.NotNil(t, updated.Routing)
	assert.Equal(t, RouteModeCustom, updated.Routing.Mode)
	assert.Equal(t, []int64{1}, updated.Routing.ChannelIDs)
}

// A delete probe against a provisioned rule resolves NotFound without touching the org's routing for it;
// only a rule that is genuinely gone from Grafana has its policy swept.
func TestDeleteRuleProbeKeepsProvisionedRulePolicy(t *testing.T) {
	svc, routes, _ := routingService(t)
	ctx := context.Background()
	require.NoError(t, routes.SetPolicy(ctx, 7, RoutePolicy{RuleUID: "protofleet-device-offline", Mode: RouteModeNone}))
	require.NoError(t, routes.SetPolicy(ctx, 7, RoutePolicy{RuleUID: "pfu-gone", Mode: RouteModeNone}))

	err := svc.DeleteRule(ctx, 7, "protofleet-device-offline")
	assert.ErrorIs(t, err, ErrNotFound)
	_, kept := routes.policies[7]["protofleet-device-offline"]
	assert.True(t, kept, "a probe delete on an existing provisioned rule must not clear its routing")

	err = svc.DeleteRule(ctx, 7, "pfu-gone")
	assert.ErrorIs(t, err, ErrNotFound)
	_, swept := routes.policies[7]["pfu-gone"]
	assert.False(t, swept, "a rule genuinely gone from Grafana has its orphaned policy swept")
}

type spyInvalidator struct {
	invalidated []int64
}

func (s *spyInvalidator) SendTest(context.Context, ChannelKind, string, string) (bool, string, error) {
	return true, "", nil
}

func (s *spyInvalidator) InvalidatePolicyCache(orgID int64) {
	s.invalidated = append(s.invalidated, orgID)
}

// Routing writes must invalidate the deliverer's policy snapshot so it can't serve stale pre-write routing.
func TestSetRuleRoutingInvalidatesDeliveryCache(t *testing.T) {
	spy := &spyInvalidator{}
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{userRuleFixture("pfu-mine", "7")}}
	svc := NewService(fake.server(t), nil, newFakeRouteStore(), nil, spy, DestinationPolicy{})

	_, err := svc.SetRuleRouting(context.Background(), 7, "pfu-mine", RouteModeNone, nil)
	require.NoError(t, err)
	_, err = svc.SetRuleRouting(context.Background(), 7, "pfu-mine", RouteModeDefault, nil)
	require.NoError(t, err)
	assert.Equal(t, []int64{7, 7}, spy.invalidated, "both the policy write and the policy delete invalidate the snapshot")
}

// A policy written concurrently with its rule's deletion is undone by the post-write recheck.
func TestSetRuleRoutingUndoneWhenRuleDeletedConcurrently(t *testing.T) {
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{userRuleFixture("pfu-mine", "7")}, getRuleGone: true}
	routes := newFakeRouteStore()
	svc := NewService(fake.server(t), nil, routes, nil, nil, DestinationPolicy{})

	_, err := svc.SetRuleRouting(context.Background(), 7, "pfu-mine", RouteModeNone, nil)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, routes.policies[7], "the freshly written policy is removed when the rule vanished mid-write")
}

func TestListRulesFailsClosedOnRouteReadError(t *testing.T) {
	svc, routes, _ := routingService(t)
	routes.listErr = errors.New("db down")

	_, err := svc.ListRules(context.Background(), 7)
	require.Error(t, err, "rendering every rule as default while policies are unreadable would mislead operators")
}
